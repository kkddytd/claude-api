package stream

import (
	"claude-api/internal/tokenizer"
	"encoding/json"
	"fmt"
	"strings"
)

// thinking 块标签常量
const (
	ThinkingStartTag = "<thinking>"
	ThinkingEndTag   = "</thinking>"
)

// SSE 事件格式化
func sseFormat(eventType string, data interface{}) string {
	jsonData, _ := json.Marshal(data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(jsonData))
}

// BuildMessageStart 构建 message_start 事件
func BuildMessageStart(conversationID, model string, inputTokens int) string {
	data := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            conversationID,
			"type":          "message",
			"role":          "assistant",
			"content":       []interface{}{},
			"model":         model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]int{"input_tokens": inputTokens, "output_tokens": 0},
		},
	}
	return sseFormat("message_start", data)
}

// BuildContentBlockStart 构建 content_block_start 事件
func BuildContentBlockStart(index int, blockType string) string {
	contentBlock := map[string]interface{}{"type": blockType}
	if blockType == "text" {
		contentBlock["text"] = ""
	} else if blockType == "thinking" {
		contentBlock["thinking"] = ""
	}
	data := map[string]interface{}{
		"type":          "content_block_start",
		"index":         index,
		"content_block": contentBlock,
	}
	return sseFormat("content_block_start", data)
}

// BuildContentBlockDelta 构建 content_block_delta 事件
func BuildContentBlockDelta(index int, text string) string {
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]interface{}{"type": "text_delta", "text": text},
	}
	return sseFormat("content_block_delta", data)
}

// BuildThinkingBlockDelta 构建 thinking_delta 事件
func BuildThinkingBlockDelta(index int, thinking string) string {
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]interface{}{"type": "thinking_delta", "thinking": thinking},
	}
	return sseFormat("content_block_delta", data)
}

// BuildContentBlockStop 构建 content_block_stop 事件
func BuildContentBlockStop(index int) string {
	data := map[string]interface{}{
		"type":  "content_block_stop",
		"index": index,
	}
	return sseFormat("content_block_stop", data)
}

// BuildPing 构建 ping 事件
func BuildPing() string {
	return sseFormat("ping", map[string]string{"type": "ping"})
}

// BuildMessageStop 构建 message_delta 和 message_stop 事件
func BuildMessageStop(inputTokens, outputTokens int, stopReason string) string {
	if stopReason == "" {
		stopReason = "end_turn"
	}
	deltaData := map[string]interface{}{
		"type":  "message_delta",
		"delta": map[string]interface{}{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]int{"output_tokens": outputTokens},
	}
	stopData := map[string]interface{}{"type": "message_stop"}
	return sseFormat("message_delta", deltaData) + sseFormat("message_stop", stopData)
}

// BuildToolUseStart 构建工具调用开始事件
func BuildToolUseStart(index int, toolUseID, toolName string) string {
	data := map[string]interface{}{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]interface{}{
			"type":  "tool_use",
			"id":    toolUseID,
			"name":  toolName,
			"input": map[string]interface{}{},
		},
	}
	return sseFormat("content_block_start", data)
}

// BuildToolUseInputDelta 构建工具输入增量事件
func BuildToolUseInputDelta(index int, inputJSONDelta string) string {
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]interface{}{
			"type":         "input_json_delta",
			"partial_json": inputJSONDelta,
		},
	}
	return sseFormat("content_block_delta", data)
}

// pendingTagSuffix 检查缓冲区末尾是否有标签的部分匹配
func pendingTagSuffix(buffer string, tag string) int {
	if buffer == "" || tag == "" {
		return 0
	}
	maxLen := len(buffer)
	if maxLen > len(tag)-1 {
		maxLen = len(tag) - 1
	}
	for length := maxLen; length > 0; length-- {
		if buffer[len(buffer)-length:] == tag[:length] {
			return length
		}
	}
	return 0
}

// ClaudeStreamHandler 处理 Amazon Q 事件并转换为 Claude SSE 格式
// 用状态管理器确保事件序列符合 Claude 规范
type ClaudeStreamHandler struct {
	Model                 string
	InputTokens           int
	ResponseBuffer        []string
	ContentBlockIndex     int
	ContentBlockStarted   bool
	ContentBlockStartSent bool
	ContentBlockStopSent  bool
	MessageStartSent      bool
	ConversationID        string
	CurrentToolUse        map[string]interface{}
	ToolInputBuffer       []string
	ProcessedToolUseIDs   map[string]bool
	AllToolInputs         []string
	// thinking 块状态
	InThinkBlock         bool
	ThinkBuffer          string
	PendingStartTagChars int
	// token 计数（基于流式 delta 事件，更准确）
	// 根据 anthropic-tokenizer 项目，每个流式 delta 对应一个 token
	OutputDeltaCount int
	// 响应结束标志，防止后续事件处理
	ResponseEnded       bool
	CreditUsage         float64
	ContextUsagePercent float64
	// 状态管理器（用于验证事件序列）
	stateManager *SSEStateManager
}

// NewClaudeStreamHandler 创建 Claude 流处理器
func NewClaudeStreamHandler(model string, inputTokens int) *ClaudeStreamHandler {
	return &ClaudeStreamHandler{
		Model:               model,
		InputTokens:         inputTokens,
		ContentBlockIndex:   -1,
		ProcessedToolUseIDs: make(map[string]bool),
		stateManager:        NewSSEStateManager(false), // 非严格模式
	}
}

// HandleEvent 处理单个 Amazon Q 事件并返回 Claude SSE 事件
func (h *ClaudeStreamHandler) HandleEvent(eventType string, payload map[string]interface{}) []string {
	var events []string

	// 如果响应已结束，提前返回（与 Python 版本保持一致）
	if h.ResponseEnded {
		return events
	}

	switch eventType {
	case "initial-response":
		if !h.MessageStartSent {
			// 直接使用初始化时生成的随机 ID，不从 AWS 响应获取（AWS 返回的可能为空或重复）
			events = append(events, BuildMessageStart(h.ConversationID, h.Model, h.InputTokens))
			h.MessageStartSent = true
			events = append(events, BuildPing())
		}

	case "assistantResponseEvent":
		content, _ := payload["content"].(string)

		// 关闭任何打开的工具调用块（与 Python 版本保持一致，不重置 ContentBlockStartSent）
		if h.CurrentToolUse != nil && !h.ContentBlockStopSent {
			events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
			h.ContentBlockStopSent = true
			h.CurrentToolUse = nil
		}

		// 处理带有 thinking 标签检测的内容
		if content != "" {
			// 使用 tokenizer 计算实际 token 数，而不是简单 +1
			h.OutputDeltaCount += tokenizer.CountTokens(content)
			h.ThinkBuffer += content
			thinkEvents := h.processThinkBuffer()
			events = append(events, thinkEvents...)
		}

	case "toolUseEvent":
		toolUseID, _ := payload["toolUseId"].(string)
		toolName, _ := payload["name"].(string)
		toolInput := payload["input"]
		isStop, _ := payload["stop"].(bool)

		// 与 kiro2api 一致：处理独立的 input/stop 事件（没有 name 和 toolUseId）
		// AWS EventStream 分片传输时可能出现：
		// 1. {"name":"xxx","toolUseId":"xxx"} - 开始
		// 2. {"input":"..."} - input 数据（没有 name 和 toolUseId）
		// 3. {"stop":true} - 结束（没有 name 和 toolUseId）
		if toolUseID == "" && toolName == "" {
			hasInput := toolInput != nil
			hasStop := isStop

			// 完全无效的事件，直接跳过
			if !hasInput && !hasStop {
				break
			}

			// 如果有当前工具调用，继续处理独立事件
			if h.CurrentToolUse != nil {
				// 累积独立的 input 事件
				if hasInput {
					var fragment string
					switch v := toolInput.(type) {
					case string:
						fragment = v
					default:
						b, _ := json.Marshal(v)
						fragment = string(b)
					}
					if fragment != "" {
						h.ToolInputBuffer = append(h.ToolInputBuffer, fragment)
						events = append(events, BuildToolUseInputDelta(h.ContentBlockIndex, fragment))
					}
				}

				// 处理独立的 stop 事件
				if hasStop {
					fullInput := ""
					for _, s := range h.ToolInputBuffer {
						fullInput += s
					}
					h.AllToolInputs = append(h.AllToolInputs, fullInput)

					events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
					h.ContentBlockStopSent = true
					h.ContentBlockStarted = false
					h.ContentBlockStartSent = false
					h.CurrentToolUse = nil
					h.ToolInputBuffer = nil
				}
			}
			// 独立事件处理完毕，跳出
			break
		}

		// 开始新的工具调用（有 toolUseId 和 name）
		if toolUseID != "" && toolName != "" && h.CurrentToolUse == nil {
			// 检查是否已处理过此工具调用ID（去重）
			if h.ProcessedToolUseIDs[toolUseID] {
				break
			}

			// 关闭之前的文本块
			if h.ContentBlockStartSent && !h.ContentBlockStopSent {
				events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
				h.ContentBlockStopSent = true
				h.ContentBlockStartSent = false
			}

			h.ProcessedToolUseIDs[toolUseID] = true
			h.ContentBlockIndex++

			events = append(events, BuildToolUseStart(h.ContentBlockIndex, toolUseID, toolName))

			h.CurrentToolUse = map[string]interface{}{"toolUseId": toolUseID, "name": toolName}
			h.ToolInputBuffer = nil
			h.ContentBlockStopSent = false
			h.ContentBlockStartSent = true
			h.ContentBlockStarted = true

			// 如果开始事件同时包含 input，立即处理
			if toolInput != nil {
				var fragment string
				switch v := toolInput.(type) {
				case string:
					fragment = v
				default:
					b, _ := json.Marshal(v)
					fragment = string(b)
				}
				if fragment != "" {
					h.ToolInputBuffer = append(h.ToolInputBuffer, fragment)
					events = append(events, BuildToolUseInputDelta(h.ContentBlockIndex, fragment))
				}
			}

			// 如果开始事件同时包含 stop，立即结束（一次性完整数据）
			if isStop {
				fullInput := ""
				for _, s := range h.ToolInputBuffer {
					fullInput += s
				}
				h.AllToolInputs = append(h.AllToolInputs, fullInput)

				events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
				h.ContentBlockStopSent = true
				h.ContentBlockStarted = false
				h.ContentBlockStartSent = false
				h.CurrentToolUse = nil
				h.ToolInputBuffer = nil
			}
			break
		}

		// 累积输入（后续的带 toolUseId 的 input 事件）
		if h.CurrentToolUse != nil && toolInput != nil {
			var fragment string
			switch v := toolInput.(type) {
			case string:
				fragment = v
			default:
				b, _ := json.Marshal(v)
				fragment = string(b)
			}
			if fragment != "" {
				h.ToolInputBuffer = append(h.ToolInputBuffer, fragment)
				events = append(events, BuildToolUseInputDelta(h.ContentBlockIndex, fragment))
			}
		}

		// 停止工具调用（后续的带 toolUseId 的 stop 事件）
		if isStop && h.CurrentToolUse != nil {
			fullInput := ""
			for _, s := range h.ToolInputBuffer {
				fullInput += s
			}
			h.AllToolInputs = append(h.AllToolInputs, fullInput)

			events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
			h.ContentBlockStopSent = true
			h.ContentBlockStarted = false
			h.ContentBlockStartSent = false
			h.CurrentToolUse = nil
			h.ToolInputBuffer = nil
		}

	case "assistantResponseEnd":
		// 关闭任何打开的块
		if h.ContentBlockStarted && !h.ContentBlockStopSent {
			events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
			h.ContentBlockStopSent = true
		}

		// 标记响应已结束，防止后续事件处理（与 Python 版本保持一致）
		h.ResponseEnded = true

		// 立即发送 message_stop（与 Python 版本保持一致，不等待 Finish()）
		outputTokens := h.OutputDeltaCount
		if outputTokens < 1 {
			fullText := strings.Join(h.ResponseBuffer, "")
			fullToolInput := strings.Join(h.AllToolInputs, "")
			outputTokens = tokenizer.CountTokens(fullText + fullToolInput)
			if outputTokens < 1 {
				outputTokens = 1
			}
		}
		events = append(events, BuildMessageStop(h.InputTokens, outputTokens, "end_turn"))

	case "meteringEvent":
		// 提取 credit usage
		if creditUsage, ok := payload["creditUsage"].(float64); ok {
			h.CreditUsage = creditUsage
		} else if usage, ok := payload["usage"].(float64); ok {
			h.CreditUsage = usage
		}
		// metering 事件不需要发送到客户端

	case "contextUsageEvent":
		// 提取 context usage percentage
		if percent, ok := payload["contextUsagePercentage"].(float64); ok {
			h.ContextUsagePercent = percent
		}
		// context usage 事件不需要发送到客户端
	}

	return events
}

// processThinkBuffer 处理 think 缓冲区，检测 thinking 标签
func (h *ClaudeStreamHandler) processThinkBuffer() []string {
	var events []string

	for h.ThinkBuffer != "" {
		// 处理待定的开始标签字符
		if h.PendingStartTagChars > 0 {
			if len(h.ThinkBuffer) < h.PendingStartTagChars {
				h.PendingStartTagChars -= len(h.ThinkBuffer)
				h.ThinkBuffer = ""
				break
			} else {
				h.ThinkBuffer = h.ThinkBuffer[h.PendingStartTagChars:]
				h.PendingStartTagChars = 0
				if h.ThinkBuffer == "" {
					break
				}
				continue
			}
		}

		if !h.InThinkBlock {
			// 查找 thinking 开始标签
			thinkStart := strings.Index(h.ThinkBuffer, ThinkingStartTag)
			if thinkStart == -1 {
				// 没有找到完整标签，检查是否有部分匹配
				pending := pendingTagSuffix(h.ThinkBuffer, ThinkingStartTag)
				if pending == len(h.ThinkBuffer) && pending > 0 {
					// 整个缓冲区是标签前缀，等待更多数据
					// 但如果当前是文本块，需要先关闭它
					if h.ContentBlockStartSent && !h.ContentBlockStopSent {
						events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
						h.ContentBlockStopSent = true
						h.ContentBlockStartSent = false
					}

					// 开始 thinking 块
					h.ContentBlockIndex++
					events = append(events, BuildContentBlockStart(h.ContentBlockIndex, "thinking"))
					h.ContentBlockStartSent = true
					h.ContentBlockStarted = true
					h.ContentBlockStopSent = false
					h.InThinkBlock = true
					h.PendingStartTagChars = len(ThinkingStartTag) - pending
					h.ThinkBuffer = ""
					break
				}

				// 发送非标签前缀部分作为文本
				emitLen := len(h.ThinkBuffer) - pending
				if emitLen <= 0 {
					break
				}
				textChunk := h.ThinkBuffer[:emitLen]
				if textChunk != "" {
					if !h.ContentBlockStartSent {
						h.ContentBlockIndex++
						events = append(events, BuildContentBlockStart(h.ContentBlockIndex, "text"))
						h.ContentBlockStartSent = true
						h.ContentBlockStarted = true
						h.ContentBlockStopSent = false
					}
					h.ResponseBuffer = append(h.ResponseBuffer, textChunk)
					events = append(events, BuildContentBlockDelta(h.ContentBlockIndex, textChunk))
				}
				h.ThinkBuffer = h.ThinkBuffer[emitLen:]
			} else {
				// 找到开始标签
				beforeText := h.ThinkBuffer[:thinkStart]
				if beforeText != "" {
					if !h.ContentBlockStartSent {
						h.ContentBlockIndex++
						events = append(events, BuildContentBlockStart(h.ContentBlockIndex, "text"))
						h.ContentBlockStartSent = true
						h.ContentBlockStarted = true
						h.ContentBlockStopSent = false
					}
					h.ResponseBuffer = append(h.ResponseBuffer, beforeText)
					events = append(events, BuildContentBlockDelta(h.ContentBlockIndex, beforeText))
				}
				h.ThinkBuffer = h.ThinkBuffer[thinkStart+len(ThinkingStartTag):]

				// 关闭当前文本块
				if h.ContentBlockStartSent && !h.ContentBlockStopSent {
					events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
					h.ContentBlockStopSent = true
					h.ContentBlockStartSent = false
				}

				// 开始 thinking 块
				h.ContentBlockIndex++
				events = append(events, BuildContentBlockStart(h.ContentBlockIndex, "thinking"))
				h.ContentBlockStartSent = true
				h.ContentBlockStarted = true
				h.ContentBlockStopSent = false
				h.InThinkBlock = true
				h.PendingStartTagChars = 0
			}
		} else {
			// 在 thinking 块中，查找结束标签
			thinkEnd := strings.Index(h.ThinkBuffer, ThinkingEndTag)
			if thinkEnd == -1 {
				// 没有找到结束标签，检查部分匹配
				pending := pendingTagSuffix(h.ThinkBuffer, ThinkingEndTag)
				emitLen := len(h.ThinkBuffer) - pending
				if emitLen <= 0 {
					break
				}
				thinkingChunk := h.ThinkBuffer[:emitLen]
				if thinkingChunk != "" {
					events = append(events, BuildThinkingBlockDelta(h.ContentBlockIndex, thinkingChunk))
				}
				h.ThinkBuffer = h.ThinkBuffer[emitLen:]
			} else {
				// 找到结束标签
				thinkingChunk := h.ThinkBuffer[:thinkEnd]
				if thinkingChunk != "" {
					events = append(events, BuildThinkingBlockDelta(h.ContentBlockIndex, thinkingChunk))
				}
				h.ThinkBuffer = h.ThinkBuffer[thinkEnd+len(ThinkingEndTag):]

				// 关闭 thinking 块
				events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
				h.ContentBlockStopSent = true
				h.ContentBlockStartSent = false
				h.InThinkBlock = false
			}
		}
	}

	return events
}

// Finish 返回最终的 SSE 事件
func (h *ClaudeStreamHandler) Finish() string {
	// 如果响应已结束（message_stop 已在 assistantResponseEnd 中发送），跳过
	if h.ResponseEnded {
		return ""
	}

	var result string

	// 确保最后一个块已关闭
	if h.ContentBlockStarted && !h.ContentBlockStopSent {
		result += BuildContentBlockStop(h.ContentBlockIndex)
		h.ContentBlockStopSent = true
	}

	// 使用流式 delta 计数作为输出 token 数
	// 根据 anthropic-tokenizer 项目，每个流式事件对应一个 token，这是最准确的计数方式
	outputTokens := h.OutputDeltaCount
	if outputTokens < 1 {
		// 如果没有 delta 事件，回退到文本估算（使用 Claude tokenizer）
		fullText := strings.Join(h.ResponseBuffer, "")
		fullToolInput := strings.Join(h.AllToolInputs, "")
		outputTokens = tokenizer.CountTokens(fullText + fullToolInput)
		if outputTokens < 1 {
			outputTokens = 1
		}
	}

	result += BuildMessageStop(h.InputTokens, outputTokens, "end_turn")
	return result
}

// OutputTokens 返回基于流式事件的输出 token 数
// @author ygw
func (h *ClaudeStreamHandler) OutputTokens() int {
	var tokens int
	if h.OutputDeltaCount > 0 {
		tokens = h.OutputDeltaCount
	} else {
		// 回退到文本估算（使用 Claude tokenizer）
		fullText := strings.Join(h.ResponseBuffer, "")
		fullToolInput := strings.Join(h.AllToolInputs, "")
		tokens = tokenizer.CountTokens(fullText + fullToolInput)
		if tokens < 1 {
			tokens = 1
		}
	}
	return tokens
}

// ResponseText 返回已累计的回答文本
func (h *ClaudeStreamHandler) ResponseText() string {
	return strings.Join(h.ResponseBuffer, "")
}

// Error 构造错误事件（Claude 格式）
func (h *ClaudeStreamHandler) Error(msg string) string {
	data := map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": msg,
		},
	}
	return sseFormat("error", data)
}
