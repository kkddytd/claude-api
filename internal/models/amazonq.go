package models

import (
	"bytes"
	"encoding/json"
)

// ============================================================================
// Amazon Q 请求数据结构
// 重要：所有结构体都实现了自定义 MarshalJSON 方法以确保 JSON 字段顺序
// 与 Python 参考项目 (claude-api) 完全一致
// ============================================================================

// AmazonQRequest 表示 Amazon Q 对话请求
type AmazonQRequest struct {
	ConversationState ConversationState `json:"conversationState"`
	ProfileArn        string            `json:"profileArn,omitempty"`
}

// MarshalJSON 自定义序列化，确保字段顺序与参考项目一致
func (r AmazonQRequest) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"conversationState":`)
	cs, err := json.Marshal(r.ConversationState)
	if err != nil {
		return nil, err
	}
	buf.Write(cs)
	if r.ProfileArn != "" {
		buf.WriteString(`,"profileArn":`)
		pa, _ := json.Marshal(r.ProfileArn)
		buf.Write(pa)
	}
	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// ConversationState 表示对话状态
// 字段顺序: conversationId -> history -> currentMessage -> chatTriggerType
type ConversationState struct {
	ConversationID  string           `json:"conversationId"`
	History         []HistoryMessage `json:"history"`
	CurrentMessage  CurrentMessage   `json:"currentMessage"`
	ChatTriggerType string           `json:"chatTriggerType"`
}

// MarshalJSON 自定义序列化，确保字段顺序: conversationId -> history -> currentMessage -> chatTriggerType
func (c ConversationState) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"conversationId":`)
	cid, _ := json.Marshal(c.ConversationID)
	buf.Write(cid)

	buf.WriteString(`,"history":`)
	if c.History == nil {
		buf.WriteString(`[]`)
	} else {
		h, err := json.Marshal(c.History)
		if err != nil {
			return nil, err
		}
		buf.Write(h)
	}

	buf.WriteString(`,"currentMessage":`)
	cm, err := json.Marshal(c.CurrentMessage)
	if err != nil {
		return nil, err
	}
	buf.Write(cm)

	buf.WriteString(`,"chatTriggerType":`)
	ctt, _ := json.Marshal(c.ChatTriggerType)
	buf.Write(ctt)

	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// HistoryMessage 表示对话历史中的消息
type HistoryMessage struct {
	MessageID                string                    `json:"messageId,omitempty"`
	UserInputMessage         *UserInputMessage         `json:"userInputMessage,omitempty"`
	AssistantResponseMessage *AssistantResponseMessage `json:"assistantResponseMessage,omitempty"`
}

// MarshalJSON 自定义序列化，=
func (h HistoryMessage) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{`)
	first := true

	// 输出 messageId（如果存在）
	if h.MessageID != "" {
		buf.WriteString(`"messageId":`)
		mid, _ := json.Marshal(h.MessageID)
		buf.Write(mid)
		first = false
	}

	if h.UserInputMessage != nil {
		if !first {
			buf.WriteString(`,`)
		}
		buf.WriteString(`"userInputMessage":`)
		um, err := json.Marshal(h.UserInputMessage)
		if err != nil {
			return nil, err
		}
		buf.Write(um)
	}
	if h.AssistantResponseMessage != nil {
		if !first {
			buf.WriteString(`,`)
		}
		buf.WriteString(`"assistantResponseMessage":`)
		am, err := json.Marshal(h.AssistantResponseMessage)
		if err != nil {
			return nil, err
		}
		buf.Write(am)
	}
	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// CurrentMessage 封装当前用户消息
type CurrentMessage struct {
	UserInputMessage UserInputMessage `json:"userInputMessage"`
}

// MarshalJSON 自定义序列化
func (c CurrentMessage) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"userInputMessage":`)
	um, err := json.Marshal(c.UserInputMessage)
	if err != nil {
		return nil, err
	}
	buf.Write(um)
	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// UserInputMessage 表示用户的输入消息
// 字段顺序: content -> userInputMessageContext -> origin -> modelId -> images
type UserInputMessage struct {
	Content                 string                  `json:"content"`
	UserInputMessageContext UserInputMessageContext `json:"userInputMessageContext"`
	Origin                  string                  `json:"origin,omitempty"`
	ModelID                 string                  `json:"modelId,omitempty"`
	Images                  []AmazonQImage          `json:"images,omitempty"`
}

// MarshalJSON 自定义序列化，确保字段顺序: content -> userInputMessageContext -> origin -> modelId -> images
func (u UserInputMessage) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"content":`)
	c, _ := json.Marshal(u.Content)
	buf.Write(c)

	buf.WriteString(`,"userInputMessageContext":`)
	ctx, err := json.Marshal(u.UserInputMessageContext)
	if err != nil {
		return nil, err
	}
	buf.Write(ctx)

	if u.Origin != "" {
		buf.WriteString(`,"origin":`)
		o, _ := json.Marshal(u.Origin)
		buf.Write(o)
	}

	if u.ModelID != "" {
		buf.WriteString(`,"modelId":`)
		m, _ := json.Marshal(u.ModelID)
		buf.Write(m)
	}

	if len(u.Images) > 0 {
		buf.WriteString(`,"images":`)
		img, err := json.Marshal(u.Images)
		if err != nil {
			return nil, err
		}
		buf.Write(img)
	}

	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// UserInputMessageContext 包含用户消息的上下文
// 字段顺序: envState -> tools -> toolResults
type UserInputMessageContext struct {
	EnvState    EnvState     `json:"envState"`
	Tools       []Tool       `json:"tools,omitempty"`
	ToolResults []ToolResult `json:"toolResults,omitempty"`
}

// MarshalJSON 自定义序列化，确保字段顺序: envState -> tools -> toolResults
func (u UserInputMessageContext) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{`)

	// 检查是否有任何内容
	hasContent := false

	// envState - 只有当有 tools 时才输出
	if len(u.Tools) > 0 {
		buf.WriteString(`"envState":`)
		env, err := json.Marshal(u.EnvState)
		if err != nil {
			return nil, err
		}
		buf.Write(env)
		hasContent = true
	}

	// tools
	if len(u.Tools) > 0 {
		if hasContent {
			buf.WriteString(`,`)
		}
		buf.WriteString(`"tools":`)
		t, err := json.Marshal(u.Tools)
		if err != nil {
			return nil, err
		}
		buf.Write(t)
		hasContent = true
	}

	// toolResults
	if len(u.ToolResults) > 0 {
		if hasContent {
			buf.WriteString(`,`)
		}
		buf.WriteString(`"toolResults":`)
		tr, err := json.Marshal(u.ToolResults)
		if err != nil {
			return nil, err
		}
		buf.Write(tr)
	}

	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// EnvState 表示环境状态
// 字段顺序: operatingSystem -> currentWorkingDirectory
type EnvState struct {
	OperatingSystem         string `json:"operatingSystem"`
	CurrentWorkingDirectory string `json:"currentWorkingDirectory"`
}

// MarshalJSON 自定义序列化，确保字段顺序: operatingSystem -> currentWorkingDirectory
func (e EnvState) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"operatingSystem":`)
	os, _ := json.Marshal(e.OperatingSystem)
	buf.Write(os)
	buf.WriteString(`,"currentWorkingDirectory":`)
	cwd, _ := json.Marshal(e.CurrentWorkingDirectory)
	buf.Write(cwd)
	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// Tool 表示工具规范
type Tool struct {
	ToolSpecification ToolSpecification `json:"toolSpecification"`
}

// MarshalJSON 自定义序列化
func (t Tool) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"toolSpecification":`)
	ts, err := json.Marshal(t.ToolSpecification)
	if err != nil {
		return nil, err
	}
	buf.Write(ts)
	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// ToolSpecification 定义工具
// 字段顺序: name -> description -> inputSchema
type ToolSpecification struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema ToolInputSchema `json:"inputSchema"`
}

// MarshalJSON 自定义序列化，确保字段顺序: name -> description -> inputSchema
func (t ToolSpecification) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"name":`)
	n, _ := json.Marshal(t.Name)
	buf.Write(n)
	buf.WriteString(`,"description":`)
	d, _ := json.Marshal(t.Description)
	buf.Write(d)
	buf.WriteString(`,"inputSchema":`)
	is, err := json.Marshal(t.InputSchema)
	if err != nil {
		return nil, err
	}
	buf.Write(is)
	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// ToolInputSchema 封装 JSON schema
// 字段顺序: type -> properties -> required (JSON Schema 标准顺序)
type ToolInputSchema struct {
	JSON map[string]interface{} `json:"json"`
}

// MarshalJSON 自定义序列化，确保 JSON Schema 字段顺序: type -> properties -> required
func (t ToolInputSchema) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"json":`)
	orderedJSON := marshalJSONSchemaOrdered(t.JSON)
	buf.Write(orderedJSON)
	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// marshalJSONSchemaOrdered 按照 JSON Schema 标准顺序序列化
// 顺序: type -> properties -> required -> 其他字段
func marshalJSONSchemaOrdered(schema map[string]interface{}) []byte {
	if schema == nil || len(schema) == 0 {
		return []byte(`{}`)
	}

	var buf bytes.Buffer
	buf.WriteString(`{`)
	first := true

	// 定义字段输出顺序
	orderedKeys := []string{"type", "properties", "required"}

	// 获取 required 数组用于排序 properties
	var requiredOrder []string
	if req, ok := schema["required"]; ok {
		if reqArr, ok := req.([]string); ok {
			requiredOrder = reqArr
		} else if reqArr, ok := req.([]interface{}); ok {
			for _, r := range reqArr {
				if s, ok := r.(string); ok {
					requiredOrder = append(requiredOrder, s)
				}
			}
		}
	}

	// 先输出有序字段
	for _, key := range orderedKeys {
		if val, ok := schema[key]; ok {
			if !first {
				buf.WriteString(`,`)
			}
			first = false
			buf.WriteString(`"`)
			buf.WriteString(key)
			buf.WriteString(`":`)

			if key == "properties" {
				// properties 需要特殊处理，使用 required 顺序
				buf.Write(marshalPropertiesOrdered(val, requiredOrder))
			} else {
				// 其他字段直接序列化
				v, _ := json.Marshal(val)
				buf.Write(v)
			}
		}
	}

	// 输出其他未在有序列表中的字段
	for key, val := range schema {
		if key == "type" || key == "properties" || key == "required" {
			continue
		}
		if !first {
			buf.WriteString(`,`)
		}
		first = false
		buf.WriteString(`"`)
		buf.WriteString(key)
		buf.WriteString(`":`)
		v, _ := json.Marshal(val)
		buf.Write(v)
	}

	buf.WriteString(`}`)
	return buf.Bytes()
}

// marshalPropertiesOrdered 序列化 properties，使用 required 顺序
func marshalPropertiesOrdered(props interface{}, requiredOrder []string) []byte {
	propsMap, ok := props.(map[string]interface{})
	if !ok {
		v, _ := json.Marshal(props)
		return v
	}

	if len(propsMap) == 0 {
		return []byte(`{}`)
	}

	var buf bytes.Buffer
	buf.WriteString(`{`)
	first := true

	// 使用 required 顺序，然后是其他属性（按字母顺序）
	outputted := make(map[string]bool)

	// 先按 required 顺序输出
	for _, key := range requiredOrder {
		if val, ok := propsMap[key]; ok {
			if !first {
				buf.WriteString(`,`)
			}
			first = false
			buf.WriteString(`"`)
			buf.WriteString(key)
			buf.WriteString(`":`)

			if propSchema, ok := val.(map[string]interface{}); ok {
				buf.Write(marshalPropertySchemaOrdered(propSchema))
			} else {
				v, _ := json.Marshal(val)
				buf.Write(v)
			}
			outputted[key] = true
		}
	}

	// 输出其他属性（按字母顺序）
	keys := make([]string, 0, len(propsMap))
	for k := range propsMap {
		if !outputted[k] {
			keys = append(keys, k)
		}
	}
	sortStrings(keys)

	for _, key := range keys {
		val := propsMap[key]
		if !first {
			buf.WriteString(`,`)
		}
		first = false
		buf.WriteString(`"`)
		buf.WriteString(key)
		buf.WriteString(`":`)

		if propSchema, ok := val.(map[string]interface{}); ok {
			buf.Write(marshalPropertySchemaOrdered(propSchema))
		} else {
			v, _ := json.Marshal(val)
			buf.Write(v)
		}
	}

	buf.WriteString(`}`)
	return buf.Bytes()
}

// marshalPropertySchemaOrdered 序列化单个属性的 schema，字段顺序: type -> description -> 其他
func marshalPropertySchemaOrdered(schema map[string]interface{}) []byte {
	if len(schema) == 0 {
		return []byte(`{}`)
	}

	var buf bytes.Buffer
	buf.WriteString(`{`)
	first := true

	// 属性 schema 的字段顺序: type -> description
	orderedKeys := []string{"type", "description"}

	for _, key := range orderedKeys {
		if val, ok := schema[key]; ok {
			if !first {
				buf.WriteString(`,`)
			}
			first = false
			buf.WriteString(`"`)
			buf.WriteString(key)
			buf.WriteString(`":`)
			v, _ := json.Marshal(val)
			buf.Write(v)
		}
	}

	// 输出其他字段
	for key, val := range schema {
		if key == "type" || key == "description" {
			continue
		}
		if !first {
			buf.WriteString(`,`)
		}
		first = false
		buf.WriteString(`"`)
		buf.WriteString(key)
		buf.WriteString(`":`)
		v, _ := json.Marshal(val)
		buf.Write(v)
	}

	buf.WriteString(`}`)
	return buf.Bytes()
}

// sortStrings 简单的字符串排序
func sortStrings(s []string) {
	for i := 0; i < len(s)-1; i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// ToolResult 表示工具使用的结果
// 字段顺序: toolUseId -> content -> status
type ToolResult struct {
	ToolUseID string              `json:"toolUseId"`
	Content   []ToolResultContent `json:"content"`
	Status    string              `json:"status,omitempty"`
}

// MarshalJSON 自定义序列化，确保字段顺序: toolUseId -> content -> status
func (t ToolResult) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"toolUseId":`)
	tid, _ := json.Marshal(t.ToolUseID)
	buf.Write(tid)
	buf.WriteString(`,"content":`)
	c, err := json.Marshal(t.Content)
	if err != nil {
		return nil, err
	}
	buf.Write(c)
	if t.Status != "" {
		buf.WriteString(`,"status":`)
		s, _ := json.Marshal(t.Status)
		buf.Write(s)
	}
	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// ToolResultContent 表示工具结果中的内容
type ToolResultContent struct {
	Text string `json:"text"`
}

// AssistantResponseMessage 表示助手的响应
// 字段顺序: messageId -> content -> toolUses
type AssistantResponseMessage struct {
	MessageID string    `json:"messageId"`
	Content   string    `json:"content"`
	ToolUses  []ToolUse `json:"toolUses,omitempty"`
}

// MarshalJSON 自定义序列化，确保字段顺序: messageId -> content -> toolUses
func (a AssistantResponseMessage) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"messageId":`)
	mid, _ := json.Marshal(a.MessageID)
	buf.Write(mid)
	buf.WriteString(`,"content":`)
	c, _ := json.Marshal(a.Content)
	buf.Write(c)
	if len(a.ToolUses) > 0 {
		buf.WriteString(`,"toolUses":`)
		tu, err := json.Marshal(a.ToolUses)
		if err != nil {
			return nil, err
		}
		buf.Write(tu)
	}
	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// ToolUse 表示助手的工具使用
// 字段顺序: toolUseId -> name -> input
type ToolUse struct {
	ToolUseID string                 `json:"toolUseId"`
	Name      string                 `json:"name"`
	Input     map[string]interface{} `json:"input"`
}

// MarshalJSON 自定义序列化，确保字段顺序: toolUseId -> name -> input
func (t ToolUse) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"toolUseId":`)
	tid, _ := json.Marshal(t.ToolUseID)
	buf.Write(tid)
	buf.WriteString(`,"name":`)
	n, _ := json.Marshal(t.Name)
	buf.Write(n)
	buf.WriteString(`,"input":`)
	i, err := json.Marshal(t.Input)
	if err != nil {
		return nil, err
	}
	buf.Write(i)
	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// AmazonQImage 表示 Amazon Q 格式的图片
type AmazonQImage struct {
	Format string             `json:"format"`
	Source AmazonQImageSource `json:"source"`
}

// MarshalJSON 自定义序列化，确保字段顺序: format -> source
func (a AmazonQImage) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"format":`)
	f, _ := json.Marshal(a.Format)
	buf.Write(f)
	buf.WriteString(`,"source":`)
	s, err := json.Marshal(a.Source)
	if err != nil {
		return nil, err
	}
	buf.Write(s)
	buf.WriteString(`}`)
	return buf.Bytes(), nil
}

// AmazonQImageSource 包含图片数据
type AmazonQImageSource struct {
	Bytes string `json:"bytes"`
}
