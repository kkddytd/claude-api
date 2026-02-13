package compressor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"claude-api/internal/logger"
	"claude-api/internal/models"
	"claude-api/internal/tokenizer"
)

// Compressor 上下文压缩器
// @author ygw
type Compressor struct {
	config *CompressConfig
	cache  *SummaryCache
}

// SummarizerFunc 摘要生成函数类型
type SummarizerFunc func(ctx context.Context, content, model string) (string, error)

// New 创建压缩器实例
func New(config *CompressConfig) *Compressor {
	if config == nil {
		config = DefaultConfig()
	}
	return &Compressor{
		config: config,
		cache:  NewSummaryCache(config.CacheDir, config.CacheTTL),
	}
}

// SetSummaryModel 设置摘要模型（用于动态更新配置）
func (c *Compressor) SetSummaryModel(model string) {
	if model != "" {
		c.config.SummaryModel = model
	}
}

// GetSummaryModel 获取当前摘要模型
func (c *Compressor) GetSummaryModel() string {
	return c.config.SummaryModel
}

// NeedsCompression 检查是否需要压缩
// 返回: needsCompress bool, tokenCount int, messageCount int
func (c *Compressor) NeedsCompression(messages []models.ClaudeMessage, systemPrompt interface{}) (bool, int, int) {
	tokenCount := c.countTokens(messages, systemPrompt)
	messageCount := len(messages)

	needsCompress := tokenCount > c.config.TokenThreshold ||
		messageCount > c.config.MessageThreshold

	return needsCompress, tokenCount, messageCount
}

// countTokens 计算消息的 token 数量
func (c *Compressor) countTokens(messages []models.ClaudeMessage, systemPrompt interface{}) int {
	total := 0

	// 计算 system prompt
	if system, ok := systemPrompt.(string); ok && system != "" {
		total += tokenizer.CountTokens(system)
		total += 3 // system prompt 格式开销
	}

	// 计算消息内容
	for _, msg := range messages {
		total += 4 // role + 格式开销
		if content, ok := msg.Content.(string); ok {
			total += tokenizer.CountTokens(content)
		} else if contentList, ok := msg.Content.([]interface{}); ok {
			for _, block := range contentList {
				if blockMap, ok := block.(map[string]interface{}); ok {
					if blockMap["type"] == "text" {
						if text, ok := blockMap["text"].(string); ok {
							total += tokenizer.CountTokens(text)
						}
					}
				}
			}
		}
	}
	return total
}

// CompressIfNeeded 如果需要则执行压缩（分块模式）
// 使用消息内容 hash 匹配缓存，支持增量压缩和多摘要块复用
// @author ygw
func (c *Compressor) CompressIfNeeded(ctx context.Context, req *models.ClaudeRequest,
	summarizer SummarizerFunc) (*models.ClaudeRequest, error) {

	// 1. 尝试查找匹配的缓存
	var existingBlocks []SummaryBlock
	var matchedCount int

	if c.config.CacheEnabled {
		cached, count := c.cache.FindMatchingCache(req.Messages)
		if cached != nil && count > 0 {
			existingBlocks = cached.SummaryBlocks
			matchedCount = count
			logger.Info("[智能压缩] 命中缓存 - 复用 %d 条消息的 %d 个摘要块",
				matchedCount, len(existingBlocks))
		}
	}

	// 2. 如果有缓存，构建带多摘要块的请求
	var workingReq *models.ClaudeRequest
	if len(existingBlocks) > 0 {
		remainingMsgs := req.Messages[matchedCount:]
		workingReq = c.buildCompressedRequestWithBlocks(req, existingBlocks, remainingMsgs)

		// 检查复用后是否仍需压缩
		needsCompress, tokenCount, msgCount := c.NeedsCompression(workingReq.Messages, req.System)
		if !needsCompress {
			logger.Info("[智能压缩] 复用后 Token=%d 消息=%d 无需再压缩", tokenCount, msgCount)
			return workingReq, nil
		}
		logger.Info("[智能压缩] 复用后仍超阈值 Token=%d 消息=%d 继续压缩", tokenCount, msgCount)
	} else {
		workingReq = req
	}

	// 3. 检查是否需要压缩
	needsCompress, tokenCount, msgCount := c.NeedsCompression(workingReq.Messages, req.System)
	if !needsCompress {
		if len(existingBlocks) > 0 {
			return workingReq, nil
		}
		return nil, nil // 不需要压缩，返回 nil 表示使用原请求
	}

	logger.Info("[智能压缩] 触发 - Token=%d/%d 消息=%d/%d",
		tokenCount, c.config.TokenThreshold, msgCount, c.config.MessageThreshold)

	// 4. 执行增量压缩
	return c.compressIncremental(ctx, req, existingBlocks, matchedCount, summarizer)
}

// compressIncremental 增量压缩（只压缩新增消息，生成新摘要块）
// @author ygw
func (c *Compressor) compressIncremental(ctx context.Context, originalReq *models.ClaudeRequest,
	existingBlocks []SummaryBlock, alreadyCompressed int, summarizer SummarizerFunc) (*models.ClaudeRequest, error) {

	// 计算需要压缩的消息范围
	// 如果有已压缩的消息，从已压缩位置开始；否则从头开始
	startIdx := alreadyCompressed
	messages := originalReq.Messages[startIdx:]

	// 分割消息：待压缩 vs 保留
	historyMsgs, keepMsgs := c.splitMessages(messages)

	if len(historyMsgs) == 0 {
		logger.Debug("[智能压缩] 无可压缩历史")
		if len(existingBlocks) > 0 {
			return c.buildCompressedRequestWithBlocks(originalReq, existingBlocks, messages), nil
		}
		return nil, nil
	}

	logger.Info("[智能压缩] 压缩 %d 条新消息（索引 %d-%d），保留 %d 条最新",
		len(historyMsgs), startIdx, startIdx+len(historyMsgs), len(keepMsgs))

	// 生成新摘要块
	summary, err := c.generateSummary(ctx, historyMsgs, summarizer)
	if err != nil {
		return nil, fmt.Errorf("生成摘要失败: %w", err)
	}

	// 创建新摘要块
	newBlockEndIdx := startIdx + len(historyMsgs)
	newBlock := SummaryBlock{
		BlockID:     generateBlockID(),
		MsgStartIdx: startIdx,
		MsgEndIdx:   newBlockEndIdx,
		MsgHash:     c.cache.GeneratePrefixHash(originalReq.Messages[startIdx:newBlockEndIdx], len(historyMsgs)),
		Summary:     summary,
		TokenCount:  tokenizer.CountTokens(summary),
		CreatedAt:   time.Now(),
	}

	// 合并所有摘要块
	allBlocks := append(existingBlocks, newBlock)

	// 构建压缩后的请求
	compressedReq := c.buildCompressedRequestWithBlocks(originalReq, allBlocks, keepMsgs)

	// 保存缓存
	totalCompressed := newBlockEndIdx
	if c.config.CacheEnabled && totalCompressed > 0 {
		prefixHash := c.cache.GeneratePrefixHash(originalReq.Messages, totalCompressed)
		c.cache.SaveCache(&ConversationCache{
			PrefixHash:         prefixHash,
			TotalCompressedMsg: totalCompressed,
			SummaryBlocks:      allBlocks,
		})
	}

	logger.Info("[智能压缩] 完成 %d→%d 条消息，共 %d 个摘要块",
		len(originalReq.Messages), len(compressedReq.Messages), len(allBlocks))

	return compressedReq, nil
}

// generateBlockID 生成摘要块唯一标识
func generateBlockID() string {
	return fmt.Sprintf("blk_%d", time.Now().UnixNano())
}

// splitMessages 分割消息为历史消息和保留消息
func (c *Compressor) splitMessages(messages []models.ClaudeMessage) (
	history []models.ClaudeMessage, keep []models.ClaudeMessage) {

	if len(messages) <= c.config.KeepMessageCount {
		return nil, messages
	}

	// 从末尾开始，找到安全的分割点
	splitIdx := len(messages) - c.config.KeepMessageCount

	// 向前扫描，确保不拆分工具调用对
	splitIdx = c.findSafeSplitPoint(messages, splitIdx)

	return messages[:splitIdx], messages[splitIdx:]
}

// findSafeSplitPoint 找到安全的分割点，保护工具调用完整性
func (c *Compressor) findSafeSplitPoint(messages []models.ClaudeMessage, proposedIdx int) int {
	// 收集保留区域内的 tool_result 的 tool_use_id
	pendingToolUseIDs := make(map[string]bool)

	for i := proposedIdx; i < len(messages); i++ {
		toolResults := extractToolResultIDs(messages[i].Content)
		for _, id := range toolResults {
			pendingToolUseIDs[id] = true
		}
	}

	if len(pendingToolUseIDs) == 0 {
		return proposedIdx
	}

	// 向前扫描，找到所有相关的 tool_use
	maxLookback := c.config.MaxToolLookback
	for i := proposedIdx - 1; i >= 0 && proposedIdx-i <= maxLookback; i-- {
		toolUseIDs := extractToolUseIDs(messages[i].Content)
		hasMatch := false
		for _, id := range toolUseIDs {
			if pendingToolUseIDs[id] {
				hasMatch = true
				delete(pendingToolUseIDs, id)
			}
		}
		if hasMatch {
			proposedIdx = i // 将分割点前移
		}
		if len(pendingToolUseIDs) == 0 {
			break
		}
	}

	return proposedIdx
}

// extractToolResultIDs 从消息内容中提取 tool_result 的 tool_use_id
func extractToolResultIDs(content interface{}) []string {
	blocks, ok := content.([]interface{})
	if !ok {
		return nil
	}
	var ids []string
	for _, block := range blocks {
		if m, ok := block.(map[string]interface{}); ok {
			if m["type"] == "tool_result" {
				if id, ok := m["tool_use_id"].(string); ok {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

// extractToolUseIDs 从消息内容中提取 tool_use 的 id
func extractToolUseIDs(content interface{}) []string {
	blocks, ok := content.([]interface{})
	if !ok {
		return nil
	}
	var ids []string
	for _, block := range blocks {
		if m, ok := block.(map[string]interface{}); ok {
			if m["type"] == "tool_use" {
				if id, ok := m["id"].(string); ok {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

// generateSummary 生成摘要
// @author ygw
func (c *Compressor) generateSummary(ctx context.Context,
	messages []models.ClaudeMessage, summarizer SummarizerFunc) (string, error) {

	// 将消息转换为文本
	content := c.messagesToText(messages)

	logger.Info("[智能压缩] 生成摘要 - %d 字符", len(content))

	// 如果内容过长，分批处理
	if len(content) > c.config.MaxBatchChars {
		return c.generateBatchSummary(ctx, content, summarizer)
	}

	// 计算目标摘要字数（基于 token 限制，中文约 1.5 字符/token）
	maxChars := c.config.MaxSingleSummaryTokens * 3 / 2

	// 构建摘要提示（9节结构化XML格式）
	prompt := fmt.Sprintf(`你的任务是创建迄今为止对话的详细摘要，密切关注用户的明确请求和之前的操作。
该摘要应全面捕获技术细节、代码模式和架构决策，这些对于在不丢失上下文的情况下继续开发工作至关重要。

请按照以下 XML 结构生成摘要（控制在 %d 字以内）：

<summary>
  <primary_request>
    【主要请求和意图】
    详细描述用户的所有明确请求和意图，包括任何修订或澄清。
    按时间顺序列出，标注意图的变化。
  </primary_request>

  <technical_concepts>
    【关键技术概念】
    列出讨论的所有重要技术概念、技术栈和框架：
    - 概念名称: 说明及其在项目中的作用
  </technical_concepts>

  <files_and_code>
    【文件和代码部分】
    列举检查、修改或创建的具体文件，包含关键代码片段：
    - 文件路径: path/to/file
      操作: 创建/修改/删除/读取
      重要性: 说明
      关键代码: [代码片段，保留完整的函数签名和核心逻辑]
  </files_and_code>

  <errors_and_fixes>
    【错误和修复】
    列出遇到的所有错误及其修复方法：
    - 错误描述: 具体错误信息
      原因分析: 为什么发生
      修复方法: 如何解决
      用户反馈: 用户的评价或后续问题
  </errors_and_fixes>

  <problem_solving>
    【问题解决】
    记录已解决的问题和正在进行的故障排除工作，包括尝试过的方法。
  </problem_solving>

  <user_messages>
    【所有用户消息】
    按时间顺序列出所有用户消息的要点（排除工具结果），以理解意图变化：
    1. [消息要点]
    2. [消息要点]
  </user_messages>

  <pending_tasks>
    【待处理任务】
    概述明确要求但尚未完成的任务：
    - [ ] 任务描述
  </pending_tasks>

  <current_work>
    【当前工作】
    详细描述摘要请求前正在进行的具体工作，特别关注最近的消息：
    - 正在处理的文件和代码
    - 当前的实现状态
    - 遇到的阻塞点
  </current_work>

  <next_steps>
    【可选的下一步】
    列出与当前工作相关的建议后续步骤：
    1. 步骤描述
    2. 步骤描述
  </next_steps>
</summary>

【对话历史】
%s

请直接输出 XML 格式的摘要，确保结构完整。`, maxChars, content)

	// 设置超时
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	return summarizer(ctx, prompt, c.config.SummaryModel)
}

// generateBatchSummary 分批生成摘要
// @author ygw
func (c *Compressor) generateBatchSummary(ctx context.Context,
	content string, summarizer SummarizerFunc) (string, error) {

	// 按字符数分割
	var batches []string
	for len(content) > 0 {
		end := c.config.MaxBatchChars
		if end > len(content) {
			end = len(content)
		}
		batches = append(batches, content[:end])
		content = content[end:]
	}

	logger.Debug("[智能压缩] 分批处理 %d 批", len(batches))

	// 计算每批的目标字数
	maxCharsPerBatch := c.config.MaxSingleSummaryTokens * 3 / 2 / len(batches)
	if maxCharsPerBatch < 500 {
		maxCharsPerBatch = 500
	}

	// 逐批生成摘要
	var summaries []string
	for i, batch := range batches {
		prompt := fmt.Sprintf(`请将以下对话片段（第 %d/%d 部分）压缩为结构化的 XML 摘要。

【输出要求】
1. 使用 XML 格式，保持结构清晰
2. 保留关键技术细节、代码片段和用户意图
3. 控制在 %d 字以内

请按以下结构输出：
<batch_summary part="%d">
  <key_points>关键要点和决策</key_points>
  <files_changed>涉及的文件和代码变更</files_changed>
  <user_requests>用户的请求和反馈</user_requests>
  <progress>完成的工作和当前状态</progress>
</batch_summary>

【对话片段】
%s

请直接输出 XML 格式摘要。`, i+1, len(batches), maxCharsPerBatch, i+1, batch)

		// 设置超时
		batchCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		summary, err := summarizer(batchCtx, prompt, c.config.SummaryModel)
		cancel()

		if err != nil {
			return "", fmt.Errorf("批次 %d 摘要失败: %w", i+1, err)
		}
		summaries = append(summaries, summary)
		logger.Debug("[智能压缩] 批次 %d/%d 完成", i+1, len(batches))
	}

	// 合并摘要（使用清晰的分隔）
	var sb strings.Builder
	for i, summary := range summaries {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("【第 %d 部分】\n", i+1))
		sb.WriteString(summary)
	}
	return sb.String(), nil
}

// messagesToText 将消息转换为文本（单独标注用户消息便于摘要识别）
// @author ygw
func (c *Compressor) messagesToText(messages []models.ClaudeMessage) string {
	var sb strings.Builder
	userMsgCount := 0

	for _, msg := range messages {
		if msg.Role == "user" {
			userMsgCount++
			sb.WriteString(fmt.Sprintf("[用户消息 #%d]: ", userMsgCount))
		} else {
			sb.WriteString(fmt.Sprintf("[%s]: ", msg.Role))
		}

		if s, ok := msg.Content.(string); ok {
			sb.WriteString(s)
		} else {
			// 提取文本内容（包含工具结果）
			sb.WriteString(extractTextContent(msg.Content))
		}
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// truncateToolResult 智能裁剪工具结果
// 保留头尾，省略中间，确保关键信息不丢失
// @author ygw
func truncateToolResult(content string) string {
	if len(content) <= MaxToolResultLength {
		return content
	}

	head := content[:KeepHeadChars]
	tail := content[len(content)-KeepTailChars:]
	omittedChars := len(content) - KeepHeadChars - KeepTailChars

	return fmt.Sprintf("%s\n\n... [省略 %d 字符] ...\n\n%s",
		head, omittedChars, tail)
}

// extractToolResultContent 从工具结果中提取文本内容
// @author ygw
func extractToolResultContent(content interface{}) string {
	if content == nil {
		return ""
	}

	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var texts []string
		for _, item := range c {
			if m, ok := item.(map[string]interface{}); ok {
				if m["type"] == "text" {
					if text, ok := m["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			} else if s, ok := item.(string); ok {
				texts = append(texts, s)
			}
		}
		return strings.Join(texts, "\n")
	default:
		if data, err := json.Marshal(content); err == nil {
			return string(data)
		}
		return ""
	}
}

// extractTextContent 从复杂内容中提取文本（保留工具结果内容）
// @author ygw
func extractTextContent(content interface{}) string {
	blocks, ok := content.([]interface{})
	if !ok {
		// 尝试 JSON 序列化
		if data, err := json.Marshal(content); err == nil {
			return string(data)
		}
		return ""
	}

	var texts []string
	for _, block := range blocks {
		if m, ok := block.(map[string]interface{}); ok {
			switch m["type"] {
			case "text":
				if text, ok := m["text"].(string); ok {
					texts = append(texts, text)
				}
			case "tool_use":
				name, _ := m["name"].(string)
				id, _ := m["id"].(string)
				// 提取工具调用的输入参数摘要
				var inputSummary string
				if input, ok := m["input"].(map[string]interface{}); ok {
					// 只保留关键参数名
					var keys []string
					for k := range input {
						keys = append(keys, k)
					}
					if len(keys) > 0 {
						inputSummary = fmt.Sprintf("(%s)", strings.Join(keys, ", "))
					}
				}
				texts = append(texts, fmt.Sprintf("[调用工具: %s%s id=%s]", name, inputSummary, id))
			case "tool_result":
				toolUseID, _ := m["tool_use_id"].(string)
				resultContent := extractToolResultContent(m["content"])
				truncated := truncateToolResult(resultContent)
				texts = append(texts, fmt.Sprintf("[工具结果 %s]:\n%s", toolUseID, truncated))
			}
		}
	}
	return strings.Join(texts, "\n")
}

// buildCompressedRequestWithBlocks 构建带多摘要块的压缩请求
// @author ygw
func (c *Compressor) buildCompressedRequestWithBlocks(original *models.ClaudeRequest,
	blocks []SummaryBlock, keepMsgs []models.ClaudeMessage) *models.ClaudeRequest {

	// 构建多摘要块内容（添加开篇语）
	var sb strings.Builder
	sb.WriteString("[历史对话摘要 - 结构化压缩]\n")
	sb.WriteString(SummaryPreamble)
	sb.WriteString("\n\n")

	for i, block := range blocks {
		sb.WriteString(fmt.Sprintf("=== 摘要块 %d (消息 %d-%d) ===\n",
			i+1, block.MsgStartIdx+1, block.MsgEndIdx))
		sb.WriteString(block.Summary)
		sb.WriteString("\n\n")
	}
	sb.WriteString("[摘要结束，以下是最近的对话]")

	// 构建新消息列表
	newMessages := make([]models.ClaudeMessage, 0, len(keepMsgs)+2)
	newMessages = append(newMessages, models.ClaudeMessage{
		Role:    "user",
		Content: sb.String(),
	})
	newMessages = append(newMessages, models.ClaudeMessage{
		Role:    "assistant",
		Content: fmt.Sprintf("好的，我已了解之前的对话上下文（共 %d 个摘要块）。请继续。", len(blocks)),
	})
	newMessages = append(newMessages, keepMsgs...)

	// 复制原请求并替换消息
	return &models.ClaudeRequest{
		Model:          original.Model,
		Messages:       newMessages,
		MaxTokens:      original.MaxTokens,
		Temperature:    original.Temperature,
		Tools:          original.Tools,
		Stream:         original.Stream,
		System:         original.System,
		Thinking:       original.Thinking,
		ConversationID: original.ConversationID,
	}
}

// buildCompressedRequest 构建压缩后的请求（保留向后兼容）
// @author ygw
func (c *Compressor) buildCompressedRequest(original *models.ClaudeRequest,
	summary string, keepMsgs []models.ClaudeMessage) *models.ClaudeRequest {

	// 创建摘要消息
	summaryContent := fmt.Sprintf(`[历史对话摘要]
%s
[摘要结束，以下是最近的对话]`, summary)

	// 构建新消息列表
	// 摘要作为 user 消息，后面跟一个 assistant 确认消息
	newMessages := make([]models.ClaudeMessage, 0, len(keepMsgs)+2)
	newMessages = append(newMessages, models.ClaudeMessage{
		Role:    "user",
		Content: summaryContent,
	})
	newMessages = append(newMessages, models.ClaudeMessage{
		Role:    "assistant",
		Content: "好的，我已了解之前的对话上下文。请继续。",
	})
	newMessages = append(newMessages, keepMsgs...)

	// 复制原请求并替换消息
	return &models.ClaudeRequest{
		Model:          original.Model,
		Messages:       newMessages,
		MaxTokens:      original.MaxTokens,
		Temperature:    original.Temperature,
		Tools:          original.Tools,
		Stream:         original.Stream,
		System:         original.System,
		Thinking:       original.Thinking,
		ConversationID: original.ConversationID,
	}
}
