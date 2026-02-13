package stream

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// BuildOpenAIChunk 构建 OpenAI 流式响应块
func BuildOpenAIChunk(id, model, content string, finishReason string) string {
	chunk := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]interface{}{},
			},
		},
	}

	if content != "" {
		chunk["choices"].([]map[string]interface{})[0]["delta"] = map[string]interface{}{
			"content": content,
		}
	}

	if finishReason != "" {
		chunk["choices"].([]map[string]interface{})[0]["finish_reason"] = finishReason
		chunk["choices"].([]map[string]interface{})[0]["delta"] = map[string]interface{}{}
	}

	jsonData, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", string(jsonData))
}

// BuildOpenAIDone 构建 OpenAI 流结束标记
func BuildOpenAIDone() string {
	return "data: [DONE]\n\n"
}

// OpenAIStreamHandler 处理 Amazon Q 事件并转换为 OpenAI SSE 格式
type OpenAIStreamHandler struct {
	ID                  string
	Model               string
	ResponseBuffer      []string
	Started             bool
	ToolCalls           []map[string]interface{}
	ToolCallIndex       int
	CurrentToolCall     *ToolCallState
	ProcessedToolUseIDs map[string]bool
	// token 计数（基于流式 delta 事件，更准确）
	// 根据 anthropic-tokenizer 项目，每个流式 delta 对应一个 token
	OutputDeltaCount int
}

// buildChunk 构建 OpenAI 流式响应块
// @author ygw
func (h *OpenAIStreamHandler) buildChunk(delta map[string]interface{}) string {
	chunk := map[string]interface{}{
		"id":      h.ID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   h.Model,
		"choices": []map[string]interface{}{
			{"index": 0, "delta": delta},
		},
	}
	jsonData, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", string(jsonData))
}

type ToolCallState struct {
	ID            string
	Name          string
	Index         int
	ArgumentsJSON []string
}

// NewOpenAIStreamHandler 创建 OpenAI 流处理器
func NewOpenAIStreamHandler(id, model string) *OpenAIStreamHandler {
	return &OpenAIStreamHandler{
		ID:                  id,
		Model:               model,
		ProcessedToolUseIDs: make(map[string]bool),
	}
}

// HandleEvent 处理单个 Amazon Q 事件并返回 OpenAI SSE 事件
func (h *OpenAIStreamHandler) HandleEvent(eventType string, payload map[string]interface{}) []string {
	var events []string

	switch eventType {
	case "initial-response":
		// 发送带角色的初始块
		if !h.Started {
			events = append(events, h.buildChunk(map[string]interface{}{"role": "assistant"}))
			h.Started = true
		}

	case "assistantResponseEvent":
		content, _ := payload["content"].(string)
		if content != "" {
			// 每个 assistantResponseEvent 对应一个 token（参考 anthropic-tokenizer 项目）
			h.OutputDeltaCount++
			h.ResponseBuffer = append(h.ResponseBuffer, content)
			events = append(events, BuildOpenAIChunk(h.ID, h.Model, content, ""))
		}

	case "toolUseEvent":
		toolCallID, _ := payload["toolUseId"].(string)
		toolName, _ := payload["name"].(string)
		toolInput := payload["input"]
		isStop, _ := payload["stop"].(bool)

		// 开始新的工具调用
		if toolCallID != "" && toolName != "" && h.CurrentToolCall == nil {
			if !h.ProcessedToolUseIDs[toolCallID] {
				h.ProcessedToolUseIDs[toolCallID] = true
				h.CurrentToolCall = &ToolCallState{
					ID:            toolCallID,
					Name:          toolName,
					Index:         h.ToolCallIndex,
					ArgumentsJSON: []string{},
				}

				// 发送工具调用开始事件
				events = append(events, h.buildChunk(map[string]interface{}{
					"tool_calls": []map[string]interface{}{
						{
							"index": h.ToolCallIndex,
							"id":    toolCallID,
							"type":  "function",
							"function": map[string]interface{}{
								"name":      toolName,
								"arguments": "",
							},
						},
					},
				}))
			}
		}

		// 累积工具输入
		if h.CurrentToolCall != nil && toolInput != nil {
			var fragment string
			switch v := toolInput.(type) {
			case string:
				fragment = v
			default:
				b, _ := json.Marshal(v)
				fragment = string(b)
			}

			if fragment != "" {
				h.CurrentToolCall.ArgumentsJSON = append(h.CurrentToolCall.ArgumentsJSON, fragment)

				// 发送增量参数
				events = append(events, h.buildChunk(map[string]interface{}{
					"tool_calls": []map[string]interface{}{
						{
							"index": h.CurrentToolCall.Index,
							"function": map[string]interface{}{
								"arguments": fragment,
							},
						},
					},
				}))
			}
		}

		// 结束工具调用
		if isStop && h.CurrentToolCall != nil {
			fullArgs := ""
			for _, s := range h.CurrentToolCall.ArgumentsJSON {
				fullArgs += s
			}

			h.ToolCalls = append(h.ToolCalls, map[string]interface{}{
				"id":   h.CurrentToolCall.ID,
				"name": h.CurrentToolCall.Name,
				"args": fullArgs,
			})

			h.ToolCallIndex++
			h.CurrentToolCall = nil
		}

	case "codeReferenceEvent":
		if toolUses, ok := payload["toolUses"].([]interface{}); ok {
			for _, tu := range toolUses {
				if toolUse, ok := tu.(map[string]interface{}); ok {
					toolCallID, _ := toolUse["toolUseId"].(string)
					toolName, _ := toolUse["name"].(string)
					toolInput, _ := toolUse["input"].(map[string]interface{})

					if toolCallID != "" && toolName != "" {
						inputJSON, _ := json.Marshal(toolInput)

						toolCall := map[string]interface{}{
							"index": h.ToolCallIndex,
							"id":    toolCallID,
							"type":  "function",
							"function": map[string]interface{}{
								"name":      toolName,
								"arguments": string(inputJSON),
							},
						}

						h.ToolCalls = append(h.ToolCalls, toolCall)
						events = append(events, h.buildChunk(map[string]interface{}{
							"tool_calls": []map[string]interface{}{toolCall},
						}))
						h.ToolCallIndex++
					}
				}
			}
		}

	case "assistantResponseEnd":
		// 发送结束块
		finishReason := "stop"
		if len(h.ToolCalls) > 0 {
			finishReason = "tool_calls"
		}
		events = append(events, BuildOpenAIChunk(h.ID, h.Model, "", finishReason))
	}

	return events
}

// Finish 返回最终的 SSE 事件
func (h *OpenAIStreamHandler) Finish() string {
	return BuildOpenAIDone()
}

// OutputTokens 返回基于流式事件的输出 token 数（乘以2倍系数）
// @author ygw
func (h *OpenAIStreamHandler) OutputTokens() int {
	var tokens int
	if h.OutputDeltaCount > 0 {
		tokens = h.OutputDeltaCount
	} else {
		// 回退到文本估算（使用 tiktoken）
		responseText := strings.Join(h.ResponseBuffer, "")
		tokens = len(responseText) / 4 // 简单估算：4 字符约等于 1 token
	}
	return tokens * 2
}

// ResponseText 返回已累计的响应文本
func (h *OpenAIStreamHandler) ResponseText() string {
	return strings.Join(h.ResponseBuffer, "")
}

