package claude

import (
	"claude-api/internal/logger"
	"claude-api/internal/models"
	"claude-api/internal/utils"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

// getOperatingSystem 获取当前操作系统名称（Amazon Q 格式）
func getOperatingSystem() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

// thinking 模式相关常量
// 注意：ThinkingHint 只需要一次，与 Python 参考项目 (claude-api) 保持一致
const (
	ThinkingHint     = "<thinking_mode>interleaved</thinking_mode><max_thinking_length>16000</max_thinking_length>"
	ThinkingStartTag = "<thinking>"
	ThinkingEndTag   = "</thinking>"
)

// 有效的模型名称集合
var validModels = map[string]bool{
	"auto":              true,
	"claude-sonnet-4":   true,
	"claude-sonnet-4.5": true,
	"claude-haiku-4.5":  true,
	"claude-opus-4.5":   true,
}

// 规范模型名称到短名称的映射
var canonicalToShort = map[string]string{
	"claude-sonnet-4-20250514":   "claude-sonnet-4",
	"claude-sonnet-4-5-20250929": "claude-sonnet-4.5",
	"claude-sonnet-4-5":          "claude-sonnet-4.5",
	"claude-haiku-4-5-20251001":  "claude-haiku-4.5",
	"claude-opus-4-5-20251101":   "claude-opus-4.5",
	// Claude 3.5 Sonnet 旧版映射
	"claude-3-5-sonnet-20241022": "claude-sonnet-4.5",
	"claude-3-5-sonnet-20240620": "claude-sonnet-4.5",
}

// MapModelName 将 Claude 模型名称映射到 Amazon Q 模型 ID
// 支持短名称（如 claude-sonnet-4）和规范名称（如 claude-sonnet-4-20250514）
func MapModelName(claudeModel string) string {
	const defaultModel = "claude-sonnet-4.5"

	modelLower := strings.ToLower(claudeModel)

	// 检查是否是有效的短名称
	if validModels[modelLower] {
		return modelLower
	}

	// 检查是否是规范名称
	if shortName, ok := canonicalToShort[modelLower]; ok {
		return shortName
	}

	// 未知模型，返回默认模型
	return defaultModel
}

// getCurrentTimestamp 获取当前时间戳
func getCurrentTimestamp() string {
	now := time.Now()
	return now.Format("Monday, 2006-01-02T15:04:05.000-07:00")
}

// wrapThinkingContent 将 thinking 内容包装成 XML 标签
func wrapThinkingContent(text string) string {
	return ThinkingStartTag + text + ThinkingEndTag
}

// isThinkingModeEnabled 检测是否启用了 thinking 模式
func isThinkingModeEnabled(thinking interface{}) bool {
	if thinking == nil {
		return false
	}
	switch t := thinking.(type) {
	case bool:
		return t
	case string:
		return strings.ToLower(t) == "enabled"
	case map[string]interface{}:
		// 检查 type 字段
		if typeVal, ok := t["type"].(string); ok {
			if strings.ToLower(typeVal) == "enabled" {
				return true
			}
		}
		// 检查 enabled 字段
		if enabled, ok := t["enabled"].(bool); ok {
			return enabled
		}
		// 检查 budget_tokens 字段
		if budget, ok := t["budget_tokens"].(float64); ok && budget > 0 {
			return true
		}
		if budget, ok := t["budget_tokens"].(int); ok && budget > 0 {
			return true
		}
	}
	return false
}

// appendThinkingHint 在文本末尾添加 thinking 提示
func appendThinkingHint(text string) string {
	if text == "" {
		return ThinkingHint
	}
	normalized := strings.TrimRight(text, " \t\r\n")
	if strings.HasSuffix(normalized, ThinkingHint) {
		return text
	}
	separator := ""
	if !strings.HasSuffix(text, "\n") && !strings.HasSuffix(text, "\r") {
		separator = "\n"
	}
	return text + separator + ThinkingHint
}

// detectToolCallLoop 检测是否存在无限工具调用循环
// 只有当同一工具在连续的 assistant 消息中被调用 threshold 次且输入相同时才触发
func detectToolCallLoop(messages []models.ClaudeMessage, threshold int) error {
	if threshold <= 0 {
		threshold = 3
	}

	var lastToolCall struct {
		name  string
		input string
	}
	consecutiveCount := 0

	// 检查最近 10 条消息
	startIdx := len(messages) - 10
	if startIdx < 0 {
		startIdx = 0
	}

	for _, msg := range messages[startIdx:] {
		if msg.Role == "assistant" {
			if blocks, ok := msg.Content.([]interface{}); ok {
				for _, block := range blocks {
					if m, ok := block.(map[string]interface{}); ok {
						if m["type"] == "tool_use" {
							toolName, _ := m["name"].(string)
							toolInput, _ := m["input"].(map[string]interface{})
							inputJSON, _ := json.Marshal(toolInput)
							currentInput := string(inputJSON)

							currentCall := struct {
								name  string
								input string
							}{toolName, currentInput}

							if currentCall.name == lastToolCall.name && currentCall.input == lastToolCall.input {
								consecutiveCount++
							} else {
								consecutiveCount = 1
								lastToolCall = currentCall
							}
						}
					}
				}
			}
		} else if msg.Role == "user" {
			// 用户消息打断连续链
			consecutiveCount = 0
			lastToolCall = struct {
				name  string
				input string
			}{}
		}
	}

	// 只有连续相同调用才触发
	if consecutiveCount >= threshold {
		return fmt.Errorf("检测到无限循环: 工具 '%s' 连续调用 %d 次且输入相同", lastToolCall.name, consecutiveCount)
	}

	return nil
}

func extractSystemText(system interface{}) string {
	switch s := system.(type) {
	case string:
		return s
	case []interface{}:
		var parts []string
		for _, block := range s {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if blockType, _ := blockMap["type"].(string); blockType == "text" {
					if text, ok := blockMap["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// determineChatTriggerType 根据 tool_choice 动态判断触发类型
func determineChatTriggerType(req *models.ClaudeRequest) string {
	if len(req.Tools) == 0 {
		return "MANUAL"
	}

	// 检查 tool_choice 配置
	if req.ToolChoice != nil {
		if tc, ok := req.ToolChoice.(map[string]interface{}); ok {
			if tcType, exists := tc["type"].(string); exists {
				if tcType == "any" || tcType == "tool" {
					return "AUTO"
				}
			}
		}
	}

	return "MANUAL"
}

// ConvertClaudeToAmazonQ 将 Claude 请求转换为 Amazon Q 格式
// 返回 AmazonQRequest 结构体，使用自定义 MarshalJSON 确保字段顺序与参考项目一致
// 如果检测到无限循环，返回 nil 和错误信息
func ConvertClaudeToAmazonQ(req *models.ClaudeRequest, conversationID string, _ bool) (*models.AmazonQRequest, error) {
	if conversationID == "" {
		conversationID = uuid.New().String()
	}

	// 检测无限工具调用循环
	if err := detectToolCallLoop(req.Messages, 3); err != nil {
		return nil, err
	}

	// 检测 thinking 模式
	thinkingEnabled := isThinkingModeEnabled(req.Thinking)

	// 1. 转换工具
	var aqTools []models.Tool
	for _, t := range req.Tools {
		desc := t.Description
		// 处理空描述（Amazon Q 不接受空的 description）
		if desc == "" {
			desc = "..."
		}
		// 限制 description 长度为 10000 字符（与参考项目一致）
		if len(desc) > 10000 {
			desc = desc[:10000]
			logger.Debug("[消息转换] 工具描述超长已截断: %s", t.Name)
		}
		aqTools = append(aqTools, models.Tool{
			ToolSpecification: models.ToolSpecification{
				Name:        t.Name,
				Description: desc,
				InputSchema: models.ToolInputSchema{
					JSON: t.InputSchema,
				},
			},
		})
	}

	// 2. 处理最后一条消息
	var promptContent string
	var toolResults []models.ToolResult
	var images []models.AmazonQImage
	hasToolResult := false

	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		if lastMsg.Role == "user" {
			promptContent = extractClaudeTextContent(lastMsg.Content)
			toolResults = extractClaudeToolResults(lastMsg.Content)
			images = extractImagesFromContent(lastMsg.Content)
			hasToolResult = len(toolResults) > 0
		}
	}

	// 从上一个 assistant 消息中获取 tool_use 顺序，用于重新排序当前消息的 tool_results
	if len(toolResults) > 0 && len(req.Messages) >= 2 {
		// 查找当前 user 消息之前的最后一个 assistant 消息
		for i := len(req.Messages) - 2; i >= 0; i-- {
			if req.Messages[i].Role == "assistant" {
				lastToolUseOrder := extractToolUseOrder(req.Messages[i].Content)
				if len(lastToolUseOrder) > 0 {
					toolResults = reorderToolResultsByToolUses(toolResults, lastToolUseOrder)
					logger.Debug("[消息转换] 重新排序 %d 个当前消息 tool_results 以匹配 tool_use 顺序", len(toolResults))
				}
				break
			}
		}
	}

	// 如果启用了 thinking 模式，添加 thinking 提示
	if thinkingEnabled && promptContent != "" {
		promptContent = appendThinkingHint(promptContent)
	}

	// 3. 构建上下文
	userCtx := models.UserInputMessageContext{
		EnvState: models.EnvState{
			OperatingSystem:         getOperatingSystem(),
			CurrentWorkingDirectory: "/",
		},
	}
	if len(aqTools) > 0 {
		userCtx.Tools = aqTools
	}
	if len(toolResults) > 0 {
		userCtx.ToolResults = toolResults
	}

	// 4. 格式化内容
	formattedContent := promptContent
	hasImages := len(images) > 0
	hasTools := len(aqTools) > 0

	if hasToolResult && promptContent == "" {
		// 与参考项目一致：当 content 为空但有 toolResults 时，使用默认提示
		formattedContent = "Tool results provided."
	} else if promptContent == "" && !hasImages && hasTools {
		//  如果没有内容但有工具，注入占位内容
		formattedContent = "执行工具任务"
		logger.Debug("[消息转换] 注入占位内容以触发工具调用 - 工具数: %d", len(aqTools))
	} else if promptContent == "" && !hasImages && !hasToolResult {
		// 与 AIClient-2-API 一致：使用 "Continue" 作为占位符，而不是返回错误
		formattedContent = "Continue"
		logger.Debug("[消息转换] 内容为空，使用默认占位符 'Continue'")
	}

	// 5. 模型映射（支持规范名称和短名称）
	modelID := MapModelName(req.Model)

	// 6. 用户输入消息（
	userInputMsg := models.UserInputMessage{
		Content:                 formattedContent,
		UserInputMessageContext: userCtx,
		Origin:                  "AI_EDITOR", // 与参考项目一致
		ModelID:                 modelID,
		Images:                  images,
	}

	// 7. 构建历史消息
	var historyMsgs []models.ClaudeMessage
	if len(req.Messages) > 1 {
		historyMsgs = req.Messages[:len(req.Messages)-1]
	}

	// 处理 system prompt（与参考项目一致：转换为 user-assistant 消息对）
	var systemHistory []models.HistoryMessage
	if req.System != nil {
		sysText := extractSystemText(req.System)
		if sysText != "" {
			// 与参考项目一致：system prompt 转换为 user-assistant 消息对
			systemHistory = append(systemHistory, models.HistoryMessage{
				MessageID: "msg-001",
				UserInputMessage: &models.UserInputMessage{
					Content: sysText,
					Origin:  "AI_EDITOR",
					ModelID: modelID,
				},
			})
			systemHistory = append(systemHistory, models.HistoryMessage{
				MessageID: "msg-002",
				AssistantResponseMessage: &models.AssistantResponseMessage{
					Content: "OK",
				},
			})
		}
	}

	// 处理常规历史消息（从 msg-003 开始编号）
	aqHistory := processClaudeHistoryWithMessageID(historyMsgs, thinkingEnabled, modelID, len(systemHistory)+1)

	// 合并 system 历史和常规历史
	fullHistory := append(systemHistory, aqHistory...)

	// 后处理：移除与 currentMessage 内容相同的尾部 userInputMessage
	if len(fullHistory) > 0 && formattedContent != "" {
		lastItem := fullHistory[len(fullHistory)-1]
		if lastItem.UserInputMessage != nil {
			lastContent := strings.TrimSpace(lastItem.UserInputMessage.Content)
			currentContent := strings.TrimSpace(formattedContent)
			if lastContent != "" && lastContent == currentContent {
				fullHistory = fullHistory[:len(fullHistory)-1]
				logger.Info("[消息转换] 移除与 currentMessage 内容相同的尾部 userInputMessage，防止重复响应")
			}
		}
	}

	// 8. 构建最终负载
	result := &models.AmazonQRequest{
		ConversationState: models.ConversationState{
			ConversationID:  conversationID,
			History:         fullHistory,
			CurrentMessage:  models.CurrentMessage{UserInputMessage: userInputMsg},
			ChatTriggerType: determineChatTriggerType(req), // 动态判断
		},
	}

	// 调试日志
	logger.Debug("[消息转换] 原始消息数: %d, 历史消息数: %d, chatTriggerType: %s",
		len(req.Messages), len(fullHistory), result.ConversationState.ChatTriggerType)

	return result, nil
}

// countToolUses 统计消息中的 tool_use 数量
func countToolUses(content interface{}) int {
	blocks, ok := content.([]interface{})
	if !ok {
		return 0
	}
	count := 0
	for _, block := range blocks {
		if m, ok := block.(map[string]interface{}); ok && m["type"] == "tool_use" {
			count++
		}
	}
	return count
}

// extractToolUseOrder 从 assistant 消息内容中提取 tool_use ID 的顺序
// 用于在下一个 user 消息中重新排序 tool_results 以匹配
func extractToolUseOrder(content interface{}) []string {
	blocks, ok := content.([]interface{})
	if !ok {
		return nil
	}
	var order []string
	for _, block := range blocks {
		if m, ok := block.(map[string]interface{}); ok && m["type"] == "tool_use" {
			if tid, ok := m["id"].(string); ok && tid != "" {
				order = append(order, tid)
			}
		}
	}
	return order
}

// reorderToolResultsByToolUses 根据 tool_use 顺序重新排序 tool_results
// 这对于防止并行工具调用时结果顺序不一致导致模型混淆至关重要
func reorderToolResultsByToolUses(toolResults []models.ToolResult, toolUseOrder []string) []models.ToolResult {
	if len(toolUseOrder) == 0 || len(toolResults) == 0 {
		return toolResults
	}

	resultByID := make(map[string]models.ToolResult)
	for _, r := range toolResults {
		resultByID[r.ToolUseID] = r
	}

	var orderedResults []models.ToolResult
	// 按 tool_use 顺序添加结果
	for _, toolUseID := range toolUseOrder {
		if r, ok := resultByID[toolUseID]; ok {
			orderedResults = append(orderedResults, r)
			delete(resultByID, toolUseID)
		}
	}

	// 添加不在原始顺序中的剩余结果（正常情况下不应该发生）
	for _, r := range resultByID {
		orderedResults = append(orderedResults, r)
	}

	return orderedResults
}

// mergeToolResultIntoMap 将 tool_result 合并到去重 map 中
// 如果 toolUseId 已存在，合并 content 数组（与 Python 版本 _merge_tool_result_into_dict 保持一致）
func mergeToolResultIntoMap(resultsByID map[string]*models.ToolResult, order *[]string, tr models.ToolResult) {
	if tr.ToolUseID == "" {
		return
	}

	if existing, ok := resultsByID[tr.ToolUseID]; ok {
		// 合并 content 数组，按 text 值去重
		existingTexts := make(map[string]bool)
		for _, c := range existing.Content {
			existingTexts[c.Text] = true
		}
		for _, c := range tr.Content {
			if c.Text != "" && !existingTexts[c.Text] {
				existing.Content = append(existing.Content, c)
				existingTexts[c.Text] = true
			}
		}
		// 如果任何结果有错误状态，保持错误
		if tr.Status == "error" {
			existing.Status = "error"
		}
		logger.Debug("[消息合并] 合并重复的 toolUseId: %s", tr.ToolUseID)
	} else {
		// 新的 toolUseId，添加到 map
		trCopy := tr
		resultsByID[tr.ToolUseID] = &trCopy
		*order = append(*order, tr.ToolUseID)
	}
}

// extractClaudeTextContent 从 Claude 内容中提取文本（包括 thinking 内容）
func extractClaudeTextContent(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var parts []string
		for _, block := range c {
			if m, ok := block.(map[string]interface{}); ok {
				blockType, _ := m["type"].(string)
				if blockType == "text" {
					if text, ok := m["text"].(string); ok {
						parts = append(parts, text)
					}
				} else if blockType == "thinking" {
					// 将 thinking 内容包装成 XML 标签
					if thinking, ok := m["thinking"].(string); ok {
						parts = append(parts, wrapThinkingContent(thinking))
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// extractImagesFromContent 从 Claude 内容中提取图片并转换为 Amazon Q 格式
// 支持两种格式：
// 1. Anthropic 原生格式: {"type": "image", "source": {"type": "base64", "media_type": "...", "data": "..."}}
// 2. OpenAI 格式: {"type": "image_url", "image_url": {"url": "data:image/...;base64,..."}}
func extractImagesFromContent(content interface{}) []models.AmazonQImage {
	blocks, ok := content.([]interface{})
	if !ok {
		return nil
	}

	var images []models.AmazonQImage
	for _, block := range blocks {
		m, ok := block.(map[string]interface{})
		if !ok {
			continue
		}

		blockType, _ := m["type"].(string)

		switch blockType {
		case "image":
			// Anthropic 原生格式
			source, ok := m["source"].(map[string]interface{})
			if !ok {
				continue
			}

			// 只支持 base64 类型的图片
			if source["type"] != "base64" {
				continue
			}

			mediaType, _ := source["media_type"].(string)
			if mediaType == "" {
				mediaType = "image/png"
			}

			// 从 media_type 提取格式（如 image/png -> png）
			format := "png"
			if idx := strings.LastIndex(mediaType, "/"); idx != -1 {
				format = mediaType[idx+1:]
			}

			data, _ := source["data"].(string)
			if data == "" {
				continue
			}

			images = append(images, models.AmazonQImage{
				Format: format,
				Source: models.AmazonQImageSource{
					Bytes: data,
				},
			})

		case "image_url":
			// OpenAI 格式: {"type": "image_url", "image_url": {"url": "data:..."}}
			imageURL, ok := m["image_url"].(map[string]interface{})
			if !ok {
				continue
			}

			// 使用工具函数转换
			aqImage, err := utils.ConvertImageURLToAmazonQImage(imageURL)
			if err != nil {
				logger.Warn("[消息转换] 转换 image_url 失败: %v", err)
				continue
			}

			images = append(images, *aqImage)
		}
	}

	if len(images) == 0 {
		return nil
	}
	return images
}

// extractClaudeToolResults 从 Claude 内容中提取工具结果
func extractClaudeToolResults(content interface{}) []models.ToolResult {
	blocks, ok := content.([]interface{})
	if !ok {
		return nil
	}

	resultMap := make(map[string]*models.ToolResult)
	var order []string

	for _, block := range blocks {
		m, ok := block.(map[string]interface{})
		if !ok || m["type"] != "tool_result" {
			continue
		}

		toolUseID, _ := m["tool_use_id"].(string)
		rawContent := m["content"]

		// 检查 status 和 is_error 字段（与参考项目一致）
		status, _ := m["status"].(string)
		isError, _ := m["is_error"].(bool)
		if status == "" {
			// 如果 status 未设置，从 is_error 推断
			if isError {
				status = "error"
			} else {
				status = "success"
			}
		}

		var aqContent []models.ToolResultContent
		switch c := rawContent.(type) {
		case string:
			aqContent = []models.ToolResultContent{{Text: c}}
		case []interface{}:
			for _, item := range c {
				if im, ok := item.(map[string]interface{}); ok {
					if im["type"] == "text" {
						if text, ok := im["text"].(string); ok {
							aqContent = append(aqContent, models.ToolResultContent{Text: text})
						}
					} else if text, ok := im["text"].(string); ok {
						aqContent = append(aqContent, models.ToolResultContent{Text: text})
					}
				} else if s, ok := item.(string); ok {
					aqContent = append(aqContent, models.ToolResultContent{Text: s})
				}
			}
		}

		hasContent := false
		for _, c := range aqContent {
			if c.Text != "" {
				hasContent = true
				break
			}
		}
		if !hasContent {
			// 根据状态返回不同的默认消息（与参考项目一致）
			if status != "error" && !isError {
				aqContent = []models.ToolResultContent{{Text: "Command executed successfully"}}
			} else {
				aqContent = []models.ToolResultContent{{Text: "Tool use was cancelled by the user"}}
			}
		}

		if existing, ok := resultMap[toolUseID]; ok {
			existing.Content = append(existing.Content, aqContent...)
			// 如果当前是错误状态，更新状态
			if status == "error" {
				existing.Status = "error"
			}
		} else {
			resultMap[toolUseID] = &models.ToolResult{
				ToolUseID: toolUseID,
				Content:   aqContent,
				Status:    status,
			}
			order = append(order, toolUseID)
		}
	}

	var results []models.ToolResult
	for _, id := range order {
		results = append(results, *resultMap[id])
	}
	return results
}

// processClaudeHistoryWithMessageID 处理 Claude 历史消息，转换为 Amazon Q 格式
func processClaudeHistoryWithMessageID(messages []models.ClaudeMessage, thinkingEnabled bool, modelID string, startMsgID int) []models.HistoryMessage {
	var history []models.HistoryMessage
	msgCounter := startMsgID
	seenToolUseIDs := make(map[string]bool)
	var lastToolUseOrder []string
	var lastMsgType string // "user", "assistant"
	toolUseIdToName := make(map[string]string)

	for _, msg := range messages {
		if msg.Role == "user" {
			// 检测连续 user 消息，插入 "OK" 的 assistant
			if lastMsgType == "user" {
				history = append(history, models.HistoryMessage{
					MessageID: fmt.Sprintf("msg-%03d", msgCounter),
					AssistantResponseMessage: &models.AssistantResponseMessage{
						Content: "OK",
					},
				})
				msgCounter++
			}

			textContent := extractClaudeTextContent(msg.Content)
			toolResults := extractClaudeToolResults(msg.Content)
			images := extractImagesFromContent(msg.Content)

			// 根据上一个 assistant 消息中的 tool_use 顺序重新排序 tool_results
			if len(toolResults) > 0 && len(lastToolUseOrder) > 0 {
				toolResults = reorderToolResultsByToolUses(toolResults, lastToolUseOrder)
			}

			// 如果启用了 thinking 模式，添加 thinking 提示
			if thinkingEnabled && textContent != "" {
				textContent = appendThinkingHint(textContent)
			}

			userCtx := &models.UserInputMessageContext{
				EnvState: models.EnvState{
					OperatingSystem:         getOperatingSystem(),
					CurrentWorkingDirectory: "/",
				},
			}
			if len(toolResults) > 0 {
				userCtx.ToolResults = toolResults
			}

			// 与参考项目一致：content 为空但有 toolResults 时使用默认值
			if textContent == "" && len(toolResults) > 0 {
				textContent = "Tool results provided."
			}

			history = append(history, models.HistoryMessage{
				MessageID: fmt.Sprintf("msg-%03d", msgCounter),
				UserInputMessage: &models.UserInputMessage{
					Content:                 textContent,
					UserInputMessageContext: *userCtx,
					Origin:                  "AI_EDITOR",
					ModelID:                 modelID,
					Images:                  images,
				},
			})
			msgCounter++
			lastMsgType = "user"

		} else if msg.Role == "assistant" {
			textContent := extractClaudeTextContent(msg.Content)

			// 跳过包含错误信息的助手消息
			if strings.HasPrefix(textContent, "❌ 请求失败:") {
				continue
			}

			entry := models.HistoryMessage{
				MessageID: fmt.Sprintf("msg-%03d", msgCounter),
				AssistantResponseMessage: &models.AssistantResponseMessage{
					Content: textContent,
				},
			}
			msgCounter++

			// 追踪 tool_use 顺序
			lastToolUseOrder = nil
			if blocks, ok := msg.Content.([]interface{}); ok {
				var toolUses []models.ToolUse
				for _, block := range blocks {
					m, ok := block.(map[string]interface{})
					if !ok || m["type"] != "tool_use" {
						continue
					}
					tid, _ := m["id"].(string)
					if tid == "" {
						continue
					}
					if seenToolUseIDs[tid] {
						continue
					}
					seenToolUseIDs[tid] = true
					lastToolUseOrder = append(lastToolUseOrder, tid)

					name, _ := m["name"].(string)
					input, _ := m["input"].(map[string]interface{})
					toolUseIdToName[tid] = name

					toolUses = append(toolUses, models.ToolUse{
						ToolUseID: tid,
						Name:      name,
						Input:     input,
					})
				}
				if len(toolUses) > 0 {
					entry.AssistantResponseMessage.ToolUses = toolUses
				}
			}

			history = append(history, entry)
			lastMsgType = "assistant"
		}
	}

	// 确保历史以 assistant 消息结尾（与参考项目一致）
	if len(history) > 0 {
		lastItem := history[len(history)-1]
		if lastItem.UserInputMessage != nil {
			// 最后是 user 消息，补充一个 "OK" 的 assistant
			history = append(history, models.HistoryMessage{
				MessageID: fmt.Sprintf("msg-%03d", msgCounter),
				AssistantResponseMessage: &models.AssistantResponseMessage{
					Content: "OK",
				},
			})
		}
	}

	return history
}

// processClaudeHistory 处理 Claude 历史消息，转换为 Amazon Q 格式（交替的 user/assistant 消息）
// 双模式检测：
// - 如果消息已正确交替（无连续的相同角色消息），跳过合并逻辑（快速路径）
// - 如果检测到连续的相同角色消息，应用合并逻辑
// 关键修复：追踪 assistant 消息中的 tool_use 顺序，并在下一个 user 消息中重新排序 tool_results 以匹配
// Deprecated: 建议使用 processClaudeHistoryWithMessageID
func processClaudeHistory(messages []models.ClaudeMessage, thinkingEnabled bool) []models.HistoryMessage {
	var rawHistory []models.HistoryMessage
	seenToolUseIDs := make(map[string]bool)
	var lastToolUseOrder []string // 追踪上一个 assistant 消息中的 tool_use 顺序

	// 第一遍：转换单个消息
	for _, msg := range messages {
		if msg.Role == "user" {
			textContent := extractClaudeTextContent(msg.Content)
			toolResults := extractClaudeToolResults(msg.Content)
			images := extractImagesFromContent(msg.Content)

			// 根据上一个 assistant 消息中的 tool_use 顺序重新排序 tool_results
			if len(toolResults) > 0 && len(lastToolUseOrder) > 0 {
				toolResults = reorderToolResultsByToolUses(toolResults, lastToolUseOrder)
				logger.Debug("[历史处理] 重新排序 %d 个 tool_results 以匹配 tool_use 顺序", len(toolResults))
			}

			// 如果启用了 thinking 模式，添加 thinking 提示
			if thinkingEnabled && textContent != "" {
				textContent = appendThinkingHint(textContent)
			}

			userCtx := &models.UserInputMessageContext{
				EnvState: models.EnvState{
					OperatingSystem:         getOperatingSystem(),
					CurrentWorkingDirectory: "/",
				},
			}
			if len(toolResults) > 0 {
				userCtx.ToolResults = toolResults
			}

			// 当 content 为空但有 toolResults 时，添加默认提示
			if textContent == "" && len(toolResults) > 0 {
				textContent = "<system-reminder>user said something to assistant, but report the tools result. follow tool results.</system-reminder>"
			}

			uMsg := &models.UserInputMessage{
				Content:                 textContent,
				UserInputMessageContext: *userCtx,
				Origin:                  "KIRO_CLI",
				Images:                  images,
			}

			rawHistory = append(rawHistory, models.HistoryMessage{UserInputMessage: uMsg})

		} else if msg.Role == "assistant" {
			textContent := extractClaudeTextContent(msg.Content)

			// 跳过包含错误信息的助手消息，避免发送给 Amazon Q 导致验证失败
			// 注意：只检查特定的错误前缀，不要使用宽泛的 "error": 匹配
			// 因为正常消息中可能包含 JSON 示例或代码讨论中的 "error": 字符串
			if strings.HasPrefix(textContent, "❌ 请求失败:") {
				continue
			}

			entry := models.HistoryMessage{
				AssistantResponseMessage: &models.AssistantResponseMessage{
					MessageID: uuid.New().String(),
					Content:   textContent,
				},
			}

			// 追踪 tool_use 顺序，用于在下一个 user 消息中重新排序 tool_results
			lastToolUseOrder = nil
			if blocks, ok := msg.Content.([]interface{}); ok {
				var toolUses []models.ToolUse
				for _, block := range blocks {
					m, ok := block.(map[string]interface{})
					if !ok || m["type"] != "tool_use" {
						continue
					}
					tid, _ := m["id"].(string)
					if tid == "" {
						continue
					}
					if seenToolUseIDs[tid] {
						logger.Debug("[历史处理] 跳过重复的 tool_use: %s", tid)
						continue
					}
					seenToolUseIDs[tid] = true
					lastToolUseOrder = append(lastToolUseOrder, tid) // 追踪顺序

					name, _ := m["name"].(string)
					input, _ := m["input"].(map[string]interface{})
					toolUses = append(toolUses, models.ToolUse{
						ToolUseID: tid,
						Name:      name,
						Input:     input,
					})
				}
				if len(toolUses) > 0 {
					entry.AssistantResponseMessage.ToolUses = toolUses
				}
			}

			rawHistory = append(rawHistory, entry)
		}
	}

	// 双模式检测：检查消息是否已正确交替
	hasConsecutiveSameRole := false
	var prevRole string
	for _, item := range rawHistory {
		var currentRole string
		if item.UserInputMessage != nil {
			currentRole = "user"
		} else {
			currentRole = "assistant"
		}
		if prevRole == currentRole {
			hasConsecutiveSameRole = true
			break
		}
		prevRole = currentRole
	}

	// 如果消息已正确交替，跳过合并逻辑（快速路径）
	if !hasConsecutiveSameRole {
		logger.Debug("[历史处理] 消息已正确交替，跳过合并，返回 %d 条历史", len(rawHistory))
		return rawHistory
	}

	logger.Debug("[历史处理] 检测到连续相同角色消息，执行合并逻辑")

	// 第二遍：合并连续的用户消息（仅在需要时执行）
	// 重要：包含 toolResults 的用户消息应该保持独立，不与普通文本消息合并
	// 这样可以确保 AI 能正确看到工具执行结果，避免无限循环
	var history []models.HistoryMessage
	var pendingUserMsgs []*models.UserInputMessage

	for _, item := range rawHistory {
		if item.UserInputMessage != nil {
			msg := item.UserInputMessage
			hasToolResults := len(msg.UserInputMessageContext.ToolResults) > 0

			if hasToolResults {
				// 包含 toolResults 的消息：先处理待合并的普通消息，然后独立添加此消息
				if len(pendingUserMsgs) > 0 {
					merged := mergeClaudeUserMessages(pendingUserMsgs)
					history = append(history, models.HistoryMessage{UserInputMessage: merged})
					pendingUserMsgs = nil
				}
				// toolResults 消息保持独立
				history = append(history, models.HistoryMessage{UserInputMessage: msg})
			} else {
				// 普通文本消息：加入待合并队列
				pendingUserMsgs = append(pendingUserMsgs, msg)
			}
		} else if item.AssistantResponseMessage != nil {
			if len(pendingUserMsgs) > 0 {
				merged := mergeClaudeUserMessages(pendingUserMsgs)
				history = append(history, models.HistoryMessage{UserInputMessage: merged})
				pendingUserMsgs = nil
			}
			history = append(history, item)
		}
	}

	if len(pendingUserMsgs) > 0 {
		merged := mergeClaudeUserMessages(pendingUserMsgs)
		history = append(history, models.HistoryMessage{UserInputMessage: merged})
	}

	// 后处理：确保历史消息严格交替（user-assistant-user-assistant）
	history = ensureAlternatingMessages(history)

	return history
}

// ensureAlternatingMessages 确保历史消息严格交替（user-assistant-user-assistant）
// 处理三种情况：
// 1. 第一条消息是 assistant，在前面插入空 user
// 2. 连续 user 消息，在中间插入空 assistant
// 3. 连续 assistant 消息，在中间插入空 user
func ensureAlternatingMessages(history []models.HistoryMessage) []models.HistoryMessage {
	if len(history) == 0 {
		return history
	}

	var result []models.HistoryMessage
	var prevRole string

	for _, item := range history {
		var currentRole string
		if item.UserInputMessage != nil {
			currentRole = "user"
		} else {
			currentRole = "assistant"
		}

		// 第一条消息必须是 user，如果是 assistant 则在前面插入空 user
		if len(result) == 0 && currentRole == "assistant" {
			logger.Debug("[历史处理] 第一条是 assistant，插入空 user 占位消息")
			result = append(result, models.HistoryMessage{
				UserInputMessage: &models.UserInputMessage{
					Content: "...",
				},
			})
			prevRole = "user"
		}

		// 连续 user 消息，插入空 assistant
		if prevRole == "user" && currentRole == "user" {
			logger.Debug("[历史处理] 检测到连续 user 消息，插入空 assistant 占位消息")
			result = append(result, models.HistoryMessage{
				AssistantResponseMessage: &models.AssistantResponseMessage{
					MessageID: uuid.New().String(),
					Content:   "...",
				},
			})
		}

		// 连续 assistant 消息，插入空 user
		if prevRole == "assistant" && currentRole == "assistant" {
			logger.Debug("[历史处理] 检测到连续 assistant 消息，插入空 user 占位消息")
			result = append(result, models.HistoryMessage{
				UserInputMessage: &models.UserInputMessage{
					Content: "...",
				},
			})
		}

		result = append(result, item)
		prevRole = currentRole
	}

	return result
}

// mergeClaudeUserMessages 合并用户消息（只保留最后 2 条消息的图片）
// 重要：此函数正确合并所有消息的 toolResults，以防止丢失工具执行历史导致无限循环
// 关键修复：按 toolUseId 去重 toolResults，防止重复的 tool_result 导致模型重复响应
// 合并时移除重复的 thinking hints，确保只在末尾出现一次
func mergeClaudeUserMessages(msgs []*models.UserInputMessage) *models.UserInputMessage {
	if len(msgs) == 0 {
		return nil
	}
	if len(msgs) == 1 {
		return msgs[0]
	}

	var allContents []string
	var baseCtx *models.UserInputMessageContext
	var baseOrigin string
	var allImages [][]models.AmazonQImage
	// 使用 map 按 toolUseId 去重 toolResults（与 Python 版本保持一致）
	toolResultsByID := make(map[string]*models.ToolResult)
	var toolResultOrder []string // 保持顺序

	// 检查是否有消息包含 thinking hint
	hadThinkingHint := false

	for i, m := range msgs {
		// 初始化基础上下文（从第一个消息）
		if i == 0 {
			// 复制第一个消息的上下文
			ctxCopy := m.UserInputMessageContext
			baseCtx = &ctxCopy
			baseOrigin = m.Origin

			// 从基础上下文中提取 toolResults 并清空（单独合并）
			if len(baseCtx.ToolResults) > 0 {
				for _, tr := range baseCtx.ToolResults {
					mergeToolResultIntoMap(toolResultsByID, &toolResultOrder, tr)
				}
				baseCtx.ToolResults = nil
			}
		} else {
			// 从后续消息收集 toolResults（按 toolUseId 去重合并）
			if len(m.UserInputMessageContext.ToolResults) > 0 {
				for _, tr := range m.UserInputMessageContext.ToolResults {
					mergeToolResultIntoMap(toolResultsByID, &toolResultOrder, tr)
				}
			}
		}

		// 处理内容：移除 thinking hint 以避免重复
		content := m.Content
		if content != "" {
			if strings.Contains(content, ThinkingHint) {
				hadThinkingHint = true
				content = strings.ReplaceAll(content, ThinkingHint, "")
				content = strings.TrimSpace(content)
			}
			if content != "" {
				allContents = append(allContents, content)
			}
		}

		// 收集图片
		if len(m.Images) > 0 {
			allImages = append(allImages, m.Images)
		}
	}

	// 合并内容
	mergedContent := strings.Join(allContents, "\n\n")

	// 如果原始消息有 thinking hint，在合并内容末尾添加一次
	if hadThinkingHint && mergedContent != "" {
		mergedContent = appendThinkingHint(mergedContent)
	}

	result := &models.UserInputMessage{
		Content:                 mergedContent,
		UserInputMessageContext: *baseCtx,
		Origin:                  baseOrigin,
	}

	// 将去重后的 toolResults 按顺序添加到结果
	if len(toolResultsByID) > 0 {
		var mergedToolResults []models.ToolResult
		for _, id := range toolResultOrder {
			if tr, ok := toolResultsByID[id]; ok {
				mergedToolResults = append(mergedToolResults, *tr)
			}
		}
		result.UserInputMessageContext.ToolResults = mergedToolResults
	}

	// 只保留最后 2 条消息的图片（与 Python 参考项目一致）
	if len(allImages) > 0 {
		var keptImages []models.AmazonQImage
		start := len(allImages) - 2
		if start < 0 {
			start = 0
		}
		for _, imgList := range allImages[start:] {
			keptImages = append(keptImages, imgList...)
		}
		if len(keptImages) > 0 {
			result.Images = keptImages
		}
	}

	return result
}
