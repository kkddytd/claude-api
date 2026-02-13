package stream

import (
	"strings"
	"testing"
)

// =============================================================================
// ClaudeStreamHandler 基础测试
// =============================================================================

// TestNewClaudeStreamHandler 测试创建新的流处理器
func TestNewClaudeStreamHandler(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)

	if handler.Model != "claude-sonnet-4" {
		t.Errorf("期望 Model 为 claude-sonnet-4，实际为 %s", handler.Model)
	}
	if handler.InputTokens != 100 {
		t.Errorf("期望 InputTokens 为 100，实际为 %d", handler.InputTokens)
	}
	if handler.ContentBlockIndex != -1 {
		t.Errorf("期望 ContentBlockIndex 为 -1，实际为 %d", handler.ContentBlockIndex)
	}
	if handler.ResponseEnded {
		t.Error("期望 ResponseEnded 为 false")
	}
}

// =============================================================================
// assistantResponseEnd 事件测试 - 立即发送 message_stop
// =============================================================================

// TestHandleEvent_AssistantResponseEnd_SendsMessageStop 测试 assistantResponseEnd 立即发送 message_stop
func TestHandleEvent_AssistantResponseEnd_SendsMessageStop(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"
	handler.MessageStartSent = true
	handler.ContentBlockStarted = true
	handler.ContentBlockStartSent = true
	handler.ContentBlockIndex = 0

	events := handler.HandleEvent("assistantResponseEnd", map[string]interface{}{})

	// 验证返回的事件包含 message_stop
	hasMessageStop := false
	for _, event := range events {
		if strings.Contains(event, "message_stop") {
			hasMessageStop = true
			break
		}
	}

	if !hasMessageStop {
		t.Error("assistantResponseEnd 应该立即发送 message_stop 事件")
	}
}

// TestHandleEvent_AssistantResponseEnd_SetsResponseEnded 测试 assistantResponseEnd 设置 ResponseEnded 标志
func TestHandleEvent_AssistantResponseEnd_SetsResponseEnded(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"
	handler.MessageStartSent = true

	handler.HandleEvent("assistantResponseEnd", map[string]interface{}{})

	if !handler.ResponseEnded {
		t.Error("assistantResponseEnd 应该设置 ResponseEnded 为 true")
	}
}

// =============================================================================
// ResponseEnded 标志防止后续事件处理测试
// =============================================================================

// TestHandleEvent_ResponseEnded_BlocksSubsequentEvents 测试 ResponseEnded 阻止后续事件
func TestHandleEvent_ResponseEnded_BlocksSubsequentEvents(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"
	handler.MessageStartSent = true
	handler.ResponseEnded = true // 模拟已结束

	// 尝试处理 assistantResponseEvent
	events := handler.HandleEvent("assistantResponseEvent", map[string]interface{}{
		"content": "这段内容不应该被处理",
	})

	if len(events) != 0 {
		t.Errorf("ResponseEnded 为 true 时不应返回任何事件，实际返回 %d 个", len(events))
	}
}

// TestHandleEvent_ResponseEnded_BlocksToolUseEvent 测试 ResponseEnded 阻止 toolUseEvent
func TestHandleEvent_ResponseEnded_BlocksToolUseEvent(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"
	handler.MessageStartSent = true
	handler.ResponseEnded = true

	events := handler.HandleEvent("toolUseEvent", map[string]interface{}{
		"toolUseId": "tool_1",
		"name":      "read_file",
	})

	if len(events) != 0 {
		t.Errorf("ResponseEnded 为 true 时不应处理 toolUseEvent，实际返回 %d 个事件", len(events))
	}
}

// =============================================================================
// Finish() 方法在 ResponseEnded 为 true 时跳过测试
// =============================================================================

// TestFinish_SkipsWhenResponseEnded 测试 Finish 在 ResponseEnded 为 true 时跳过
func TestFinish_SkipsWhenResponseEnded(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"
	handler.ResponseEnded = true

	result := handler.Finish()

	if result != "" {
		t.Errorf("ResponseEnded 为 true 时 Finish() 应返回空字符串，实际返回: %s", result)
	}
}

// TestFinish_SendsMessageStopWhenNotEnded 测试 Finish 在未结束时发送 message_stop
func TestFinish_SendsMessageStopWhenNotEnded(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"
	handler.ResponseEnded = false
	handler.OutputDeltaCount = 50

	result := handler.Finish()

	if !strings.Contains(result, "message_stop") {
		t.Error("ResponseEnded 为 false 时 Finish() 应发送 message_stop")
	}
}

// =============================================================================
// 流处理完整流程测试
// =============================================================================

// TestHandleEvent_CompleteFlow 测试完整的事件处理流程
func TestHandleEvent_CompleteFlow(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"

	// 1. initial-response
	events := handler.HandleEvent("initial-response", map[string]interface{}{})
	if !handler.MessageStartSent {
		t.Error("initial-response 后 MessageStartSent 应为 true")
	}
	hasMessageStart := false
	for _, e := range events {
		if strings.Contains(e, "message_start") {
			hasMessageStart = true
			break
		}
	}
	if !hasMessageStart {
		t.Error("initial-response 应发送 message_start 事件")
	}

	// 2. assistantResponseEvent
	events = handler.HandleEvent("assistantResponseEvent", map[string]interface{}{
		"content": "Hello, world!",
	})
	if len(events) == 0 {
		t.Error("assistantResponseEvent 应返回事件")
	}

	// 3. assistantResponseEnd
	events = handler.HandleEvent("assistantResponseEnd", map[string]interface{}{})
	if !handler.ResponseEnded {
		t.Error("assistantResponseEnd 后 ResponseEnded 应为 true")
	}

	// 4. 验证后续事件被阻止
	events = handler.HandleEvent("assistantResponseEvent", map[string]interface{}{
		"content": "This should be blocked",
	})
	if len(events) != 0 {
		t.Error("ResponseEnded 后不应处理更多事件")
	}

	// 5. Finish 应跳过
	result := handler.Finish()
	if result != "" {
		t.Error("ResponseEnded 后 Finish() 应返回空字符串")
	}
}

// =============================================================================
// toolUseEvent 处理测试
// =============================================================================

// TestHandleEvent_ToolUseEvent_Start 测试工具调用开始
func TestHandleEvent_ToolUseEvent_Start(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"
	handler.MessageStartSent = true

	events := handler.HandleEvent("toolUseEvent", map[string]interface{}{
		"toolUseId": "tool_123",
		"name":      "read_file",
	})

	if handler.CurrentToolUse == nil {
		t.Error("应该设置 CurrentToolUse")
	}
	if !handler.ProcessedToolUseIDs["tool_123"] {
		t.Error("应该记录已处理的 toolUseId")
	}

	hasToolUseStart := false
	for _, e := range events {
		if strings.Contains(e, "tool_use") && strings.Contains(e, "tool_123") {
			hasToolUseStart = true
			break
		}
	}
	if !hasToolUseStart {
		t.Error("应发送 tool_use 开始事件")
	}
}

// TestHandleEvent_ToolUseEvent_Dedup 测试工具调用去重
func TestHandleEvent_ToolUseEvent_Dedup(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"
	handler.MessageStartSent = true
	handler.ProcessedToolUseIDs["tool_123"] = true // 已处理

	events := handler.HandleEvent("toolUseEvent", map[string]interface{}{
		"toolUseId": "tool_123",
		"name":      "read_file",
	})

	if len(events) != 0 {
		t.Error("重复的 toolUseId 不应产生事件")
	}
}

// TestHandleEvent_ToolUseEvent_Stop 测试工具调用停止
func TestHandleEvent_ToolUseEvent_Stop(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"
	handler.MessageStartSent = true
	handler.CurrentToolUse = map[string]interface{}{"toolUseId": "tool_123", "name": "read_file"}
	handler.ContentBlockStartSent = true
	handler.ContentBlockStarted = true
	handler.ContentBlockIndex = 0

	events := handler.HandleEvent("toolUseEvent", map[string]interface{}{
		"stop": true,
	})

	if handler.CurrentToolUse != nil {
		t.Error("stop 后 CurrentToolUse 应为 nil")
	}

	hasBlockStop := false
	for _, e := range events {
		if strings.Contains(e, "content_block_stop") {
			hasBlockStop = true
			break
		}
	}
	if !hasBlockStop {
		t.Error("应发送 content_block_stop 事件")
	}
}

// =============================================================================
// thinking 块处理测试
// =============================================================================

// TestProcessThinkBuffer_ThinkingTags 测试 thinking 标签处理
func TestProcessThinkBuffer_ThinkingTags(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"
	handler.MessageStartSent = true

	// 模拟包含 thinking 标签的内容
	handler.ThinkBuffer = "Hello <thinking>这是思考内容</thinking> World"
	events := handler.processThinkBuffer()

	// 验证产生了 thinking 相关事件
	hasThinkingStart := false
	hasThinkingDelta := false
	for _, e := range events {
		if strings.Contains(e, `"type":"thinking"`) {
			hasThinkingStart = true
		}
		if strings.Contains(e, "thinking_delta") {
			hasThinkingDelta = true
		}
	}

	if !hasThinkingStart {
		t.Error("应该产生 thinking 块开始事件")
	}
	if !hasThinkingDelta {
		t.Error("应该产生 thinking_delta 事件")
	}
}

// =============================================================================
// SSE 格式构建测试
// =============================================================================

// TestBuildMessageStart 测试 message_start 构建
func TestBuildMessageStart(t *testing.T) {
	result := BuildMessageStart("conv-123", "claude-sonnet-4", 100)

	if !strings.Contains(result, "event: message_start") {
		t.Error("应包含 event: message_start")
	}
	if !strings.Contains(result, "conv-123") {
		t.Error("应包含 conversation ID")
	}
	if !strings.Contains(result, "claude-sonnet-4") {
		t.Error("应包含 model")
	}
}

// TestBuildContentBlockStart 测试 content_block_start 构建
func TestBuildContentBlockStart(t *testing.T) {
	result := BuildContentBlockStart(0, "text")

	if !strings.Contains(result, "event: content_block_start") {
		t.Error("应包含 event: content_block_start")
	}
	if !strings.Contains(result, `"type":"text"`) {
		t.Error("应包含 type: text")
	}
}

// TestBuildContentBlockDelta 测试 content_block_delta 构建
func TestBuildContentBlockDelta(t *testing.T) {
	result := BuildContentBlockDelta(0, "Hello")

	if !strings.Contains(result, "event: content_block_delta") {
		t.Error("应包含 event: content_block_delta")
	}
	if !strings.Contains(result, "text_delta") {
		t.Error("应包含 text_delta")
	}
	if !strings.Contains(result, "Hello") {
		t.Error("应包含文本内容")
	}
}

// TestBuildThinkingBlockDelta 测试 thinking_delta 构建
func TestBuildThinkingBlockDelta(t *testing.T) {
	result := BuildThinkingBlockDelta(0, "思考中...")

	if !strings.Contains(result, "event: content_block_delta") {
		t.Error("应包含 event: content_block_delta")
	}
	if !strings.Contains(result, "thinking_delta") {
		t.Error("应包含 thinking_delta")
	}
}

// TestBuildContentBlockStop 测试 content_block_stop 构建
func TestBuildContentBlockStop(t *testing.T) {
	result := BuildContentBlockStop(0)

	if !strings.Contains(result, "event: content_block_stop") {
		t.Error("应包含 event: content_block_stop")
	}
}

// TestBuildMessageStop 测试 message_stop 构建
func TestBuildMessageStop(t *testing.T) {
	result := BuildMessageStop(100, 50, "end_turn")

	if !strings.Contains(result, "message_delta") {
		t.Error("应包含 message_delta")
	}
	if !strings.Contains(result, "message_stop") {
		t.Error("应包含 message_stop")
	}
	if !strings.Contains(result, "end_turn") {
		t.Error("应包含 stop_reason")
	}
}

// TestBuildMessageStop_DefaultStopReason 测试默认 stop_reason
func TestBuildMessageStop_DefaultStopReason(t *testing.T) {
	result := BuildMessageStop(100, 50, "")

	if !strings.Contains(result, "end_turn") {
		t.Error("空 stop_reason 应默认为 end_turn")
	}
}

// TestBuildPing 测试 ping 构建
func TestBuildPing(t *testing.T) {
	result := BuildPing()

	if !strings.Contains(result, "event: ping") {
		t.Error("应包含 event: ping")
	}
}

// TestBuildToolUseStart 测试工具调用开始事件构建
func TestBuildToolUseStart(t *testing.T) {
	result := BuildToolUseStart(0, "tool_123", "read_file")

	if !strings.Contains(result, "event: content_block_start") {
		t.Error("应包含 event: content_block_start")
	}
	if !strings.Contains(result, "tool_use") {
		t.Error("应包含 tool_use")
	}
	if !strings.Contains(result, "tool_123") {
		t.Error("应包含 tool ID")
	}
	if !strings.Contains(result, "read_file") {
		t.Error("应包含 tool name")
	}
}

// TestBuildToolUseInputDelta 测试工具输入增量事件构建
func TestBuildToolUseInputDelta(t *testing.T) {
	result := BuildToolUseInputDelta(0, `{"path":"/test"}`)

	if !strings.Contains(result, "event: content_block_delta") {
		t.Error("应包含 event: content_block_delta")
	}
	if !strings.Contains(result, "input_json_delta") {
		t.Error("应包含 input_json_delta")
	}
}

// =============================================================================
// pendingTagSuffix 函数测试
// =============================================================================

// TestPendingTagSuffix 测试标签前缀检测
func TestPendingTagSuffix(t *testing.T) {
	tests := []struct {
		name     string
		buffer   string
		tag      string
		expected int
	}{
		{"empty buffer", "", "<thinking>", 0},
		{"empty tag", "hello", "", 0},
		{"no match", "hello", "<thinking>", 0},
		{"partial match 1", "hello<", "<thinking>", 1},
		{"partial match 2", "hello<t", "<thinking>", 2},
		{"partial match 3", "hello<th", "<thinking>", 3},
		{"full prefix", "<thinkin", "<thinking>", 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pendingTagSuffix(tt.buffer, tt.tag)
			if result != tt.expected {
				t.Errorf("期望 %d，实际 %d", tt.expected, result)
			}
		})
	}
}

// =============================================================================
// OutputTokens 和 ResponseText 方法测试
// =============================================================================

// TestOutputTokens 测试输出 token 计数
func TestOutputTokens(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.OutputDeltaCount = 50

	tokens := handler.OutputTokens()
	if tokens != 50 {
		t.Errorf("期望 50，实际 %d", tokens)
	}
}

// TestOutputTokens_Fallback 测试输出 token 计数回退
func TestOutputTokens_Fallback(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.OutputDeltaCount = 0
	handler.ResponseBuffer = []string{"Hello", " ", "World"}

	tokens := handler.OutputTokens()
	if tokens < 1 {
		t.Error("回退计算应返回至少 1 个 token")
	}
}

// TestResponseText 测试响应文本获取
func TestResponseText(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ResponseBuffer = []string{"Hello", " ", "World"}

	text := handler.ResponseText()
	if text != "Hello World" {
		t.Errorf("期望 'Hello World'，实际 '%s'", text)
	}
}

// =============================================================================
// Error 方法测试
// =============================================================================

// TestError 测试错误事件构建
func TestError(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)

	result := handler.Error("Something went wrong")

	if !strings.Contains(result, "event: error") {
		t.Error("应包含 event: error")
	}
	if !strings.Contains(result, "api_error") {
		t.Error("应包含 api_error 类型")
	}
	if !strings.Contains(result, "Something went wrong") {
		t.Error("应包含错误消息")
	}
}

// =============================================================================
// 边界条件测试
// =============================================================================

// TestHandleEvent_InitialResponse_OnlyOnce 测试 initial-response 只处理一次
func TestHandleEvent_InitialResponse_OnlyOnce(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"

	// 第一次
	events1 := handler.HandleEvent("initial-response", map[string]interface{}{})
	// 第二次
	events2 := handler.HandleEvent("initial-response", map[string]interface{}{})

	if len(events1) == 0 {
		t.Error("第一次 initial-response 应返回事件")
	}
	if len(events2) != 0 {
		t.Error("第二次 initial-response 不应返回事件")
	}
}

// TestHandleEvent_AssistantResponseEvent_ClosesToolUse 测试文本事件关闭工具调用
func TestHandleEvent_AssistantResponseEvent_ClosesToolUse(t *testing.T) {
	handler := NewClaudeStreamHandler("claude-sonnet-4", 100)
	handler.ConversationID = "test-conv-id"
	handler.MessageStartSent = true
	handler.CurrentToolUse = map[string]interface{}{"toolUseId": "tool_1"}
	handler.ContentBlockStartSent = true
	handler.ContentBlockIndex = 0

	events := handler.HandleEvent("assistantResponseEvent", map[string]interface{}{
		"content": "继续回答",
	})

	if handler.CurrentToolUse != nil {
		t.Error("assistantResponseEvent 应关闭当前工具调用")
	}

	hasBlockStop := false
	for _, e := range events {
		if strings.Contains(e, "content_block_stop") {
			hasBlockStop = true
			break
		}
	}
	if !hasBlockStop {
		t.Error("应发送 content_block_stop 关闭工具块")
	}
}
