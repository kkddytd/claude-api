package stream

import (
	"claude-api/internal/logger"
	"fmt"
	"sync"
)

// BlockState 内容块状态
type BlockState struct {
	Index     int    `json:"index"`
	Type      string `json:"type"` // "text" | "tool_use" | "thinking"
	Started   bool   `json:"started"`
	Stopped   bool   `json:"stopped"`
	ToolUseID string `json:"tool_use_id,omitempty"` // 仅用于工具块
}

// SSEStateManager SSE事件状态管理器，确保事件序列符合Claude规范
type SSEStateManager struct {
	mu               sync.Mutex
	messageStarted   bool
	messageDeltaSent bool // 跟踪message_delta是否已发送
	activeBlocks     map[int]*BlockState
	messageEnded     bool
	nextBlockIndex   int
	strictMode       bool
}

// NewSSEStateManager 创建SSE状态管理器
func NewSSEStateManager(strictMode bool) *SSEStateManager {
	return &SSEStateManager{
		activeBlocks: make(map[int]*BlockState),
		strictMode:   strictMode,
	}
}

// Reset 重置状态管理器
func (ssm *SSEStateManager) Reset() {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	ssm.messageStarted = false
	ssm.messageDeltaSent = false
	ssm.messageEnded = false
	ssm.activeBlocks = make(map[int]*BlockState)
	ssm.nextBlockIndex = 0
}

// ValidateAndSendEvent 验证并发送事件，返回修正后的事件列表
func (ssm *SSEStateManager) ValidateAndSendEvent(eventType string, eventData map[string]interface{}) ([]string, error) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()

	switch eventType {
	case "message_start":
		return ssm.handleMessageStart(eventData)
	case "content_block_start":
		return ssm.handleContentBlockStart(eventData)
	case "content_block_delta":
		return ssm.handleContentBlockDelta(eventData)
	case "content_block_stop":
		return ssm.handleContentBlockStop(eventData)
	case "message_delta":
		return ssm.handleMessageDelta(eventData)
	case "message_stop":
		return ssm.handleMessageStop(eventData)
	default:
		// 其他事件直接返回
		return []string{sseFormat(eventType, eventData)}, nil
	}
}

// handleMessageStart 处理消息开始事件
func (ssm *SSEStateManager) handleMessageStart(eventData map[string]interface{}) ([]string, error) {
	if ssm.messageStarted {
		logger.Error("违规：message_start只能出现一次")
		if ssm.strictMode {
			return nil, fmt.Errorf("违规：message_start只能出现一次")
		}
		return nil, nil // 非严格模式下跳过重复的message_start
	}

	ssm.messageStarted = true
	return []string{sseFormat("message_start", eventData)}, nil
}

// handleContentBlockStart 处理内容块开始事件
func (ssm *SSEStateManager) handleContentBlockStart(eventData map[string]interface{}) ([]string, error) {
	var events []string

	if !ssm.messageStarted {
		logger.Error("违规：content_block_start必须在message_start之后")
		if ssm.strictMode {
			return nil, fmt.Errorf("违规：content_block_start必须在message_start之后")
		}
	}

	if ssm.messageEnded {
		logger.Error("违规：message已结束，不能发送content_block_start")
		if ssm.strictMode {
			return nil, fmt.Errorf("违规：message已结束，不能发送content_block_start")
		}
		return nil, nil
	}

	// 提取块索引
	index := ssm.nextBlockIndex
	if idx, ok := eventData["index"].(int); ok {
		index = idx
	} else if idx, ok := eventData["index"].(float64); ok {
		index = int(idx)
	}

	// 检查是否重复启动同一块
	if block, exists := ssm.activeBlocks[index]; exists && block.Started && !block.Stopped {
		// 自动关闭上一个未完成的 block
		logger.Warn("自动关闭未完成的content_block - index: %d, type: %s", index, block.Type)
		stopEvent := map[string]interface{}{
			"type":  "content_block_stop",
			"index": index,
		}
		events = append(events, sseFormat("content_block_stop", stopEvent))
		block.Stopped = true
	}

	// 确定块类型
	blockType := "text"
	if contentBlock, ok := eventData["content_block"].(map[string]interface{}); ok {
		if cbType, ok := contentBlock["type"].(string); ok {
			blockType = cbType
		}
	}

	// 关键修复：在启动新工具块前，自动关闭文本块（防止事件交织）
	if blockType == "tool_use" {
		for blockIndex, block := range ssm.activeBlocks {
			if block.Type == "text" && block.Started && !block.Stopped {
				stopEvent := map[string]interface{}{
					"type":  "content_block_stop",
					"index": blockIndex,
				}
				logger.Debug("工具块启动前自动关闭文本块 - text_index: %d, tool_index: %d", blockIndex, index)
				events = append(events, sseFormat("content_block_stop", stopEvent))
				block.Stopped = true
			}
		}
	}

	// 提取工具使用ID
	toolUseID := ""
	if blockType == "tool_use" {
		if contentBlock, ok := eventData["content_block"].(map[string]interface{}); ok {
			if id, ok := contentBlock["id"].(string); ok {
				toolUseID = id
			}
		}
	}

	// 关键修复：为 thinking 块添加 signature 字段（Claude 标准要求）
	if blockType == "thinking" {
		if contentBlock, ok := eventData["content_block"].(map[string]interface{}); ok {
			if _, hasSignature := contentBlock["signature"]; !hasSignature {
				contentBlock["signature"] = ""
				logger.Debug("为 thinking 块添加缺失的 signature 字段 - index: %d", index)
			}
		}
	}

	ssm.activeBlocks[index] = &BlockState{
		Index:     index,
		Type:      blockType,
		Started:   true,
		Stopped:   false,
		ToolUseID: toolUseID,
	}

	if index >= ssm.nextBlockIndex {
		ssm.nextBlockIndex = index + 1
	}

	events = append(events, sseFormat("content_block_start", eventData))
	return events, nil
}

// handleContentBlockDelta 处理内容块增量事件
func (ssm *SSEStateManager) handleContentBlockDelta(eventData map[string]interface{}) ([]string, error) {
	var events []string

	index := 0
	if idx, ok := eventData["index"].(int); ok {
		index = idx
	} else if idx, ok := eventData["index"].(float64); ok {
		index = int(idx)
	}

	// 检查块是否已启动，如果没有则自动启动
	block, exists := ssm.activeBlocks[index]
	if !exists || !block.Started {
		logger.Debug("检测到content_block_delta但块未启动，自动生成content_block_start - index: %d", index)

		// 推断块类型
		blockType := "text"
		if delta, ok := eventData["delta"].(map[string]interface{}); ok {
			if deltaType, ok := delta["type"].(string); ok {
				switch deltaType {
				case "input_json_delta":
					blockType = "tool_use"
				case "thinking_delta":
					blockType = "thinking"
				}
			}
		}

		// 自动生成并发送content_block_start事件
		startEvent := map[string]interface{}{
			"type":  "content_block_start",
			"index": index,
			"content_block": map[string]interface{}{
				"type": blockType,
			},
		}

		switch blockType {
		case "text":
			startEvent["content_block"].(map[string]interface{})["text"] = ""
		case "thinking":
			startEvent["content_block"].(map[string]interface{})["thinking"] = ""
			startEvent["content_block"].(map[string]interface{})["signature"] = ""
		case "tool_use":
			startEvent["content_block"].(map[string]interface{})["id"] = fmt.Sprintf("tooluse_auto_%d", index)
			startEvent["content_block"].(map[string]interface{})["name"] = "auto_detected"
			startEvent["content_block"].(map[string]interface{})["input"] = map[string]interface{}{}
		}

		events = append(events, sseFormat("content_block_start", startEvent))

		// 更新状态
		ssm.activeBlocks[index] = &BlockState{
			Index:   index,
			Type:    blockType,
			Started: true,
			Stopped: false,
		}
		if index >= ssm.nextBlockIndex {
			ssm.nextBlockIndex = index + 1
		}
		block = ssm.activeBlocks[index]
	}

	// 检查块是否已停止
	if block != nil && block.Stopped {
		// thinking 块特殊处理：标记为未停止并继续发送 delta
		if block.Type == "thinking" {
			logger.Debug("thinking块已停止但收到新delta，继续发送（不重启）- index: %d", index)
			block.Stopped = false
			events = append(events, sseFormat("content_block_delta", eventData))
			return events, nil
		}

		// 其他类型的块，重新启动
		logger.Warn("content_block已停止但收到新delta，重新启动 - index: %d, type: %s", index, block.Type)
		startEvent := map[string]interface{}{
			"type":  "content_block_start",
			"index": index,
			"content_block": map[string]interface{}{
				"type": block.Type,
			},
		}
		switch block.Type {
		case "text":
			startEvent["content_block"].(map[string]interface{})["text"] = ""
		case "tool_use":
			startEvent["content_block"].(map[string]interface{})["id"] = block.ToolUseID
			startEvent["content_block"].(map[string]interface{})["name"] = "continued"
			startEvent["content_block"].(map[string]interface{})["input"] = map[string]interface{}{}
		}
		events = append(events, sseFormat("content_block_start", startEvent))
		block.Stopped = false
	}

	events = append(events, sseFormat("content_block_delta", eventData))
	return events, nil
}

// handleContentBlockStop 处理内容块停止事件
func (ssm *SSEStateManager) handleContentBlockStop(eventData map[string]interface{}) ([]string, error) {
	index := 0
	if idx, ok := eventData["index"].(int); ok {
		index = idx
	} else if idx, ok := eventData["index"].(float64); ok {
		index = int(idx)
	}

	block, exists := ssm.activeBlocks[index]
	if !exists || !block.Started {
		logger.Error("违规：索引%d的content_block未启动就发送stop", index)
		if ssm.strictMode {
			return nil, fmt.Errorf("违规：索引%d的content_block未启动就发送stop", index)
		}
		return nil, nil
	}

	if block.Stopped {
		logger.Error("违规：索引%d的content_block重复停止", index)
		if ssm.strictMode {
			return nil, fmt.Errorf("违规：索引%d的content_block重复停止", index)
		}
		return nil, nil
	}

	block.Stopped = true
	return []string{sseFormat("content_block_stop", eventData)}, nil
}

// handleMessageDelta 处理消息增量事件
func (ssm *SSEStateManager) handleMessageDelta(eventData map[string]interface{}) ([]string, error) {
	var events []string

	if !ssm.messageStarted {
		logger.Error("违规：message_delta必须在message_start之后")
		if ssm.strictMode {
			return nil, fmt.Errorf("违规：message_delta必须在message_start之后")
		}
	}

	// 关键修复：防止重复的message_delta事件
	if ssm.messageDeltaSent {
		logger.Error("违规：message_delta只能出现一次 - message_started: %v, message_delta_sent: %v, message_ended: %v",
			ssm.messageStarted, ssm.messageDeltaSent, ssm.messageEnded)
		if ssm.strictMode {
			return nil, fmt.Errorf("违规：message_delta只能出现一次")
		}
		logger.Debug("跳过重复的message_delta事件")
		return nil, nil
	}

	// 关键修复：在发送message_delta之前，确保非thinking的content_block都已关闭
	var unclosedBlocks []int
	for index, block := range ssm.activeBlocks {
		if block.Started && !block.Stopped {
			// 跳过 thinking 块
			if block.Type == "thinking" {
				logger.Debug("message_delta前跳过关闭thinking块 - index: %d", index)
				continue
			}
			unclosedBlocks = append(unclosedBlocks, index)
		}
	}

	if len(unclosedBlocks) > 0 {
		logger.Debug("message_delta前自动关闭未关闭的content_block - unclosed_blocks: %v", unclosedBlocks)
		if !ssm.strictMode {
			for _, index := range unclosedBlocks {
				stopEvent := map[string]interface{}{
					"type":  "content_block_stop",
					"index": index,
				}
				events = append(events, sseFormat("content_block_stop", stopEvent))
				ssm.activeBlocks[index].Stopped = true
				logger.Debug("自动关闭未关闭的content_block（message_delta前）- index: %d", index)
			}
		}
	}

	ssm.messageDeltaSent = true
	events = append(events, sseFormat("message_delta", eventData))
	return events, nil
}

// handleMessageStop 处理消息停止事件
func (ssm *SSEStateManager) handleMessageStop(eventData map[string]interface{}) ([]string, error) {
	if !ssm.messageStarted {
		logger.Error("违规：message_stop必须在message_start之后")
		if ssm.strictMode {
			return nil, fmt.Errorf("违规：message_stop必须在message_start之后")
		}
	}

	if ssm.messageEnded {
		logger.Error("违规：message_stop只能出现一次")
		if ssm.strictMode {
			return nil, fmt.Errorf("违规：message_stop只能出现一次")
		}
		return nil, nil
	}

	ssm.messageEnded = true
	return []string{sseFormat("message_stop", eventData)}, nil
}

// GetActiveBlocks 获取所有活跃块
func (ssm *SSEStateManager) GetActiveBlocks() map[int]*BlockState {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	return ssm.activeBlocks
}

// IsMessageStarted 检查消息是否已开始
func (ssm *SSEStateManager) IsMessageStarted() bool {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	return ssm.messageStarted
}

// IsMessageEnded 检查消息是否已结束
func (ssm *SSEStateManager) IsMessageEnded() bool {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	return ssm.messageEnded
}

// IsMessageDeltaSent 检查message_delta是否已发送
func (ssm *SSEStateManager) IsMessageDeltaSent() bool {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	return ssm.messageDeltaSent
}

// GetNextBlockIndex 获取下一个块索引
func (ssm *SSEStateManager) GetNextBlockIndex() int {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	return ssm.nextBlockIndex
}
