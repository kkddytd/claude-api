package claude

import (
	"encoding/json"
	"claude-api/internal/models"
	"strings"
	"testing"
)

// TestConvertClaudeToAmazonQ_BasicMessage 测试基本消息转换
func TestConvertClaudeToAmazonQ_BasicMessage(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{
				Role:    "user",
				Content: "Hello, how are you?",
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证 conversationId 不为空
	if result.ConversationState.ConversationID == "" {
		t.Error("conversationId 不应为空")
	}

	// 验证 content 直接使用原始文本（与参考项目一致）
	content := result.ConversationState.CurrentMessage.UserInputMessage.Content
	if content != "Hello, how are you?" {
		t.Errorf("content 应为原始文本，got: %s", content)
	}

	// 验证 origin 为 "AI_EDITOR"（与参考项目一致）
	origin := result.ConversationState.CurrentMessage.UserInputMessage.Origin
	if origin != "AI_EDITOR" {
		t.Errorf("origin 应为 'AI_EDITOR', got: %s", origin)
	}

	// 验证 chatTriggerType 为 "MANUAL"
	if result.ConversationState.ChatTriggerType != "MANUAL" {
		t.Errorf("chatTriggerType 应为 'MANUAL', got: %s", result.ConversationState.ChatTriggerType)
	}
}

// TestConvertClaudeToAmazonQ_WithHistory 测试带历史消息的转换
func TestConvertClaudeToAmazonQ_WithHistory(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "What is 2+2?"},
			{Role: "assistant", Content: "2+2 equals 4."},
			{Role: "user", Content: "What about 3+3?"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证历史消息存在
	if len(result.ConversationState.History) == 0 {
		t.Error("历史消息不应为空")
	}

	// 验证历史消息有 messageId（与参考项目一致）
	for i, msg := range result.ConversationState.History {
		if msg.MessageID == "" {
			t.Errorf("历史消息 %d 应有 messageId", i)
		}
	}

	// 验证当前消息
	if result.ConversationState.CurrentMessage.UserInputMessage.Content != "What about 3+3?" {
		t.Errorf("当前消息内容不正确")
	}
}

// TestConvertClaudeToAmazonQ_WithSystemPrompt 测试带 system prompt 的转换
func TestConvertClaudeToAmazonQ_WithSystemPrompt(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System: []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "You are a helpful assistant.",
			},
		},
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证 system prompt 转换为 user-assistant 消息对（与参考项目一致）
	if len(result.ConversationState.History) < 2 {
		t.Error("system prompt 应转换为至少 2 条历史消息")
	}

	// 第一条应该是 user（包含 system prompt）
	firstMsg := result.ConversationState.History[0]
	if firstMsg.UserInputMessage == nil {
		t.Error("第一条历史消息应为 userInputMessage")
	}
	if !strings.Contains(firstMsg.UserInputMessage.Content, "helpful assistant") {
		t.Error("第一条历史消息应包含 system prompt 内容")
	}

	// 第二条应该是 assistant（回复 "OK"）
	if len(result.ConversationState.History) >= 2 {
		secondMsg := result.ConversationState.History[1]
		if secondMsg.AssistantResponseMessage == nil {
			t.Error("第二条历史消息应为 assistantResponseMessage")
		}
		if secondMsg.AssistantResponseMessage.Content != "OK" {
			t.Errorf("第二条历史消息内容应为 'OK', got: %s", secondMsg.AssistantResponseMessage.Content)
		}
	}
}

// TestConvertClaudeToAmazonQ_WithTools 测试带工具的转换
func TestConvertClaudeToAmazonQ_WithTools(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "What's the weather?"},
		},
		Tools: []models.ClaudeTool{
			{
				Name:        "get_weather",
				Description: "Get weather information",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{"type": "string"},
					},
					"required": []interface{}{"location"},
				},
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证工具存在
	tools := result.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools
	if len(tools) != 1 {
		t.Errorf("应有 1 个工具, got: %d", len(tools))
	}

	// 验证工具名称
	if tools[0].ToolSpecification.Name != "get_weather" {
		t.Errorf("工具名称应为 'get_weather', got: %s", tools[0].ToolSpecification.Name)
	}
}

// TestConvertClaudeToAmazonQ_WithToolResults 测试带工具结果的转换
func TestConvertClaudeToAmazonQ_WithToolResults(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "tool_123",
						"content":     "Temperature is 25°C",
					},
				},
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证 toolResults 存在
	toolResults := result.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults
	if len(toolResults) != 1 {
		t.Errorf("应有 1 个 toolResult, got: %d", len(toolResults))
	}

	// 验证 tool_use_id
	if toolResults[0].ToolUseID != "tool_123" {
		t.Errorf("toolUseId 应为 'tool_123', got: %s", toolResults[0].ToolUseID)
	}

	// 验证 content 为默认值（与参考项目一致）
	content := result.ConversationState.CurrentMessage.UserInputMessage.Content
	if content != "Tool results provided." {
		t.Errorf("content 应为 'Tool results provided.', got: %s", content)
	}
}

// TestConvertClaudeToAmazonQ_ToolResultWithUserText 测试 toolResult 同时带用户文本
func TestConvertClaudeToAmazonQ_ToolResultWithUserText(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "tool_123",
						"content":     "Sunny, 25°C",
					},
					map[string]interface{}{
						"type": "text",
						"text": "Please summarize this weather info.",
					},
				},
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证 content 为用户文本（与参考项目一致）
	content := result.ConversationState.CurrentMessage.UserInputMessage.Content
	if content != "Please summarize this weather info." {
		t.Errorf("content 应为用户文本, got: %s", content)
	}
}

// TestDetermineChatTriggerType 测试 chatTriggerType 动态判断
func TestDetermineChatTriggerType(t *testing.T) {
	tests := []struct {
		name     string
		req      *models.ClaudeRequest
		expected string
	}{
		{
			name: "无工具 - MANUAL",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{{Role: "user", Content: "Hello"}},
			},
			expected: "MANUAL",
		},
		{
			name: "有工具但无 tool_choice - MANUAL",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{{Role: "user", Content: "Hello"}},
				Tools:    []models.ClaudeTool{{Name: "test_tool"}},
			},
			expected: "MANUAL",
		},
		{
			name: "有工具且 tool_choice=any - AUTO",
			req: &models.ClaudeRequest{
				Messages:   []models.ClaudeMessage{{Role: "user", Content: "Hello"}},
				Tools:      []models.ClaudeTool{{Name: "test_tool"}},
				ToolChoice: map[string]interface{}{"type": "any"},
			},
			expected: "AUTO",
		},
		{
			name: "有工具且 tool_choice=tool - AUTO",
			req: &models.ClaudeRequest{
				Messages:   []models.ClaudeMessage{{Role: "user", Content: "Hello"}},
				Tools:      []models.ClaudeTool{{Name: "test_tool"}},
				ToolChoice: map[string]interface{}{"type": "tool", "name": "test_tool"},
			},
			expected: "AUTO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineChatTriggerType(tt.req)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestConvertClaudeToAmazonQ_ConsecutiveUserMessages 测试连续 user 消息处理
func TestConvertClaudeToAmazonQ_ConsecutiveUserMessages(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Question 1"},
			{Role: "user", Content: "Question 2"},
			{Role: "user", Content: "Question 3"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证历史消息交替（连续 user 之间应插入 "OK" 的 assistant）
	history := result.ConversationState.History
	var prevType string
	for i, msg := range history {
		var currentType string
		if msg.UserInputMessage != nil {
			currentType = "user"
		} else {
			currentType = "assistant"
		}
		if i > 0 && prevType == currentType {
			t.Errorf("历史消息 %d 和 %d 应交替, 都是 %s", i-1, i, currentType)
		}
		prevType = currentType
	}
}

// TestConvertClaudeToAmazonQ_HistoryEndsWithAssistant 测试历史以 assistant 结尾
func TestConvertClaudeToAmazonQ_HistoryEndsWithAssistant(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Question 1"},
			{Role: "user", Content: "Question 2"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证历史以 assistant 结尾
	history := result.ConversationState.History
	if len(history) > 0 {
		lastMsg := history[len(history)-1]
		if lastMsg.AssistantResponseMessage == nil {
			t.Error("历史应以 assistant 消息结尾")
		}
	}
}

// TestConvertClaudeToAmazonQ_OutputFormat 测试输出 JSON 格式
func TestConvertClaudeToAmazonQ_OutputFormat(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证可以正确序列化为 JSON
	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	t.Logf("输出 JSON:\n%s", string(jsonBytes))

	// 验证结构完整性
	if result.ConversationState.ConversationID == "" {
		t.Error("conversationId 不应为空")
	}
	if result.ConversationState.ChatTriggerType == "" {
		t.Error("chatTriggerType 不应为空")
	}
}

// TestConvertClaudeToAmazonQ_WithToolUseHistory 测试带 tool_use 的历史消息
func TestConvertClaudeToAmazonQ_WithToolUseHistory(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "What's the weather?"},
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "Let me check..."},
					map[string]interface{}{
						"type":  "tool_use",
						"id":    "tool_123",
						"name":  "get_weather",
						"input": map[string]interface{}{"city": "Beijing"},
					},
				},
			},
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "tool_123",
						"content":     "Sunny, 25°C",
					},
				},
			},
			{Role: "user", Content: "Thanks!"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证历史中有 assistant 消息带 toolUses
	foundToolUse := false
	for _, msg := range result.ConversationState.History {
		if msg.AssistantResponseMessage != nil && len(msg.AssistantResponseMessage.ToolUses) > 0 {
			foundToolUse = true
			// 验证 toolUse 格式
			tu := msg.AssistantResponseMessage.ToolUses[0]
			if tu.ToolUseID != "tool_123" {
				t.Errorf("toolUseId 应为 'tool_123', got: %s", tu.ToolUseID)
			}
			if tu.Name != "get_weather" {
				t.Errorf("name 应为 'get_weather', got: %s", tu.Name)
			}
			break
		}
	}
	if !foundToolUse {
		t.Error("历史中应有包含 toolUses 的 assistant 消息")
	}

	// 验证历史中有 user 消息带 toolResults
	foundToolResult := false
	for _, msg := range result.ConversationState.History {
		if msg.UserInputMessage != nil && len(msg.UserInputMessage.UserInputMessageContext.ToolResults) > 0 {
			foundToolResult = true
			tr := msg.UserInputMessage.UserInputMessageContext.ToolResults[0]
			if tr.ToolUseID != "tool_123" {
				t.Errorf("toolUseId 应为 'tool_123', got: %s", tr.ToolUseID)
			}
			break
		}
	}
	if !foundToolResult {
		t.Error("历史中应有包含 toolResults 的 user 消息")
	}
}

// TestConvertClaudeToAmazonQ_HistoryMessageIdFormat 测试历史消息 messageId 格式
func TestConvertClaudeToAmazonQ_HistoryMessageIdFormat(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System: []interface{}{
			map[string]interface{}{"type": "text", "text": "You are helpful."},
		},
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi!"},
			{Role: "user", Content: "Bye"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证所有历史消息都有 messageId，且格式为 msg-XXX
	for i, msg := range result.ConversationState.History {
		if msg.MessageID == "" {
			t.Errorf("历史消息 %d 应有 messageId", i)
		}
		if !strings.HasPrefix(msg.MessageID, "msg-") {
			t.Errorf("messageId 应以 'msg-' 开头, got: %s", msg.MessageID)
		}
	}
}

// TestConvertClaudeToAmazonQ_NoContentWrapping 测试 content 不被包装
func TestConvertClaudeToAmazonQ_NoContentWrapping(t *testing.T) {
	testContent := "This is a test message without any special formatting."
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: testContent},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	content := result.ConversationState.CurrentMessage.UserInputMessage.Content

	// 验证 content 不包含旧的包装格式
	unwantedPatterns := []string{
		"--- CONTEXT ENTRY BEGIN ---",
		"--- CONTEXT ENTRY END ---",
		"--- USER MESSAGE BEGIN ---",
		"--- USER MESSAGE END ---",
		"--- SYSTEM PROMPT BEGIN ---",
		"--- SYSTEM PROMPT END ---",
		"<system-reminder>",
	}

	for _, pattern := range unwantedPatterns {
		if strings.Contains(content, pattern) {
			t.Errorf("content 不应包含 '%s', got: %s", pattern, content)
		}
	}

	// 验证 content 为原始文本
	if content != testContent {
		t.Errorf("content 应为原始文本 '%s', got: %s", testContent, content)
	}
}
