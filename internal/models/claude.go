package models

// ClaudeMessage 表示 Claude API 格式的消息
type ClaudeMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // 可以是字符串或 []ContentBlock
}

// ClaudeTool 表示 Claude API 中的工具定义
type ClaudeTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ClaudeRequest 表示 Claude API 请求
type ClaudeRequest struct {
	Model          string          `json:"model"`
	Messages       []ClaudeMessage `json:"messages"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	Tools          []ClaudeTool    `json:"tools,omitempty"`
	ToolChoice     interface{}     `json:"tool_choice,omitempty"`     // 工具选择策略（与参考项目一致）
	Stream         bool            `json:"stream,omitempty"`
	System         interface{}     `json:"system,omitempty"`          // 可以是字符串或 []SystemBlock
	Thinking       interface{}     `json:"thinking,omitempty"`        // thinking 模式配置
	ConversationID *string         `json:"conversation_id,omitempty"` // 会话 ID（支持 conversation_id 和 conversationId）
}

// ContentBlock 表示内容块（text, image, tool_use, tool_result）
type ContentBlock struct {
	Type      string                 `json:"type"`
	Text      *string                `json:"text,omitempty"`
	Source    *ImageSource           `json:"source,omitempty"`
	ToolUseID *string                `json:"tool_use_id,omitempty"`
	ID        *string                `json:"id,omitempty"`
	Name      *string                `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	Content   interface{}            `json:"content,omitempty"` // 用于 tool_result
	IsError   *bool                  `json:"is_error,omitempty"`
}

// ImageSource 表示图片来源
type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

// SystemBlock 表示系统提示块
type SystemBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
