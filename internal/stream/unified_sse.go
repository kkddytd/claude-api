package stream

import (
	"encoding/json"
	"fmt"
	"claude-api/internal/tokenizer"
	"strings"
)

// 统一 SSE 事件构造
func buildUnifiedEvent(eventType string, data interface{}) string {
	payload, _ := json.Marshal(data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(payload))
}

// UnifiedStreamHandler 将 Amazon Q 事件转换为统一的 SSE 协议
// 支持双通道输出：thinking_delta 与 answer_delta
type UnifiedStreamHandler struct {
	Model          string
	ConversationID string
	ResponseID     string
	InputTokens    int

	// 内部状态
	outputBuffer         []string
	thinkingBuffer       string
	inThinking           bool
	metaSent             bool
	pendingStartTagChars int
	doneSent             bool
	// token 计数（基于流式 delta 事件，更准确）
	// 根据 anthropic-tokenizer 项目，每个流式 delta 对应一个 token
	outputDeltaCount int
}

func NewUnifiedStreamHandler(model string, conversationID string, inputTokens int) *UnifiedStreamHandler {
	return &UnifiedStreamHandler{
		Model:          model,
		ConversationID: conversationID,
		InputTokens:    inputTokens,
		outputBuffer:   []string{},
	}
}

// HandleEvent 将 AWS EventStream 事件映射为统一 SSE 事件
func (h *UnifiedStreamHandler) HandleEvent(eventType string, payload map[string]interface{}) []string {
	var events []string

	switch eventType {
	case "initial-response":
		if !h.metaSent {
			// 直接使用初始化时生成的随机 ID，不从 AWS 响应获取（AWS 返回的可能为空或重复）
			events = append(events, buildUnifiedEvent("meta", map[string]interface{}{
				"type":            "meta",
				"conversation_id": h.ConversationID,
				"model":           h.Model,
				"input_tokens":    h.InputTokens,
			}))
			h.metaSent = true
		}

	case "assistantResponseEvent":
		content, _ := payload["content"].(string)
		if content != "" {
			// 使用 tokenizer 计算实际 token 数，而不是简单 +1
			h.outputDeltaCount += tokenizer.CountTokens(content)
			h.thinkingBuffer += content
			events = append(events, h.flushThinkingBuffer()...)
		}

	case "assistantResponseEnd":
		if !h.doneSent {
			events = append(events, h.done("stop"))
		}
	}

	return events
}

// done 完成流时输出 done 事件
func (h *UnifiedStreamHandler) done(reason string) string {
	if h.doneSent {
		return ""
	}
	h.doneSent = true

	// 使用流式 delta 计数作为输出 token 数
	// 根据 anthropic-tokenizer 项目，每个流式事件对应一个 token
	outputTokens := h.outputDeltaCount
	if outputTokens < 1 {
		// 如果没有 delta 事件，回退到文本估算（使用 Claude tokenizer）
		outputText := strings.Join(h.outputBuffer, "")
		outputTokens = tokenizer.CountTokens(outputText)
		if outputTokens < 1 {
			outputTokens = 1
		}
	}

	return buildUnifiedEvent("done", map[string]interface{}{
		"type":          "done",
		"finish_reason": reason,
		"usage": map[string]int{
			"input_tokens":  h.InputTokens,
			"output_tokens": outputTokens,
		},
	})
}

// OutputTokens 返回基于流式事件的输出 token 数
// @author ygw
func (h *UnifiedStreamHandler) OutputTokens() int {
	var tokens int
	if h.outputDeltaCount > 0 {
		tokens = h.outputDeltaCount
	} else {
		// 回退到文本估算（使用 Claude tokenizer）
		outputText := strings.Join(h.outputBuffer, "")
		tokens = tokenizer.CountTokens(outputText)
		if tokens < 1 {
			tokens = 1
		}
	}
	return tokens
}

// Error 构造错误事件
func (h *UnifiedStreamHandler) Error(msg string) string {
	return buildUnifiedEvent("error", map[string]interface{}{
		"type":    "error",
		"message": msg,
	})
}

// ResponseText 返回已累计的回答文本
func (h *UnifiedStreamHandler) ResponseText() string {
	return strings.Join(h.outputBuffer, "")
}

// Finish 输出最终 done 事件
func (h *UnifiedStreamHandler) Finish(reason string) string {
	return h.done(reason)
}

// flushThinkingBuffer 解析 thinking 标签，输出对应事件
func (h *UnifiedStreamHandler) flushThinkingBuffer() []string {
	var events []string

	for h.thinkingBuffer != "" {
		// 处理待定的开始标签字符
		if h.pendingStartTagChars > 0 {
			if len(h.thinkingBuffer) < h.pendingStartTagChars {
				h.pendingStartTagChars -= len(h.thinkingBuffer)
				h.thinkingBuffer = ""
				break
			}
			h.thinkingBuffer = h.thinkingBuffer[h.pendingStartTagChars:]
			h.pendingStartTagChars = 0
			if h.thinkingBuffer == "" {
				break
			}
		}

		if !h.inThinking {
			start := strings.Index(h.thinkingBuffer, ThinkingStartTag)
			if start == -1 {
				// 检查是否有部分起始标签
				pending := pendingTagSuffix(h.thinkingBuffer, ThinkingStartTag)
				if pending == len(h.thinkingBuffer) && pending > 0 {
					h.pendingStartTagChars = len(ThinkingStartTag) - pending
					h.thinkingBuffer = ""
					break
				}
				emitLen := len(h.thinkingBuffer) - pending
				if emitLen <= 0 {
					break
				}
				textChunk := h.thinkingBuffer[:emitLen]
				if textChunk != "" {
					h.outputBuffer = append(h.outputBuffer, textChunk)
					events = append(events, buildUnifiedEvent("answer_delta", map[string]interface{}{
						"type": "answer_delta",
						"text": textChunk,
					}))
				}
				h.thinkingBuffer = h.thinkingBuffer[emitLen:]
			} else {
				before := h.thinkingBuffer[:start]
				if before != "" {
					h.outputBuffer = append(h.outputBuffer, before)
					events = append(events, buildUnifiedEvent("answer_delta", map[string]interface{}{
						"type": "answer_delta",
						"text": before,
					}))
				}
				h.thinkingBuffer = h.thinkingBuffer[start+len(ThinkingStartTag):]
				// 进入 thinking 块
				h.inThinking = true
			}
		} else {
			end := strings.Index(h.thinkingBuffer, ThinkingEndTag)
			if end == -1 {
				// 检查未完整的结束标签
				pending := pendingTagSuffix(h.thinkingBuffer, ThinkingEndTag)
				emitLen := len(h.thinkingBuffer) - pending
				if emitLen <= 0 {
					break
				}
				thinkChunk := h.thinkingBuffer[:emitLen]
				if thinkChunk != "" {
					events = append(events, buildUnifiedEvent("thinking_delta", map[string]interface{}{
						"type": "thinking_delta",
						"text": thinkChunk,
					}))
				}
				h.thinkingBuffer = h.thinkingBuffer[emitLen:]
				if pending > 0 {
					h.pendingStartTagChars = pending
				}
			} else {
				thinkChunk := h.thinkingBuffer[:end]
				if thinkChunk != "" {
					events = append(events, buildUnifiedEvent("thinking_delta", map[string]interface{}{
						"type": "thinking_delta",
						"text": thinkChunk,
					}))
				}
				h.thinkingBuffer = h.thinkingBuffer[end+len(ThinkingEndTag):]
				h.inThinking = false
				h.pendingStartTagChars = 0
			}
		}
	}

	return events
}
