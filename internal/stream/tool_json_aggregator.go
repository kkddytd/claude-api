package stream

import (
	"bytes"
	"claude-api/internal/logger"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// ToolParamsUpdateCallback 工具参数更新回调函数类型
type ToolParamsUpdateCallback func(toolUseId string, fullParams string)

// ToolJSONAggregator 流式 JSON 聚合器
// 处理 AWS EventStream 分片传输时的 JSON 片段聚合
type ToolJSONAggregator struct {
	activeStreamers  map[string]*JSONStreamer
	mu               sync.RWMutex
	updateCallback   ToolParamsUpdateCallback
	currentToolUseID string // 跟踪当前活跃的工具调用 ID（用于处理独立的 input/stop 事件）
}

// JSONStreamer 单个工具调用的流式解析器
type JSONStreamer struct {
	toolUseID      string
	toolName       string
	buffer         *bytes.Buffer
	hasValidJSON   bool
	lastUpdate     time.Time
	isComplete     bool
	result         map[string]interface{}
	fragmentCount  int
	totalBytes     int
	incompleteUTF8 string // 用于存储跨片段的不完整UTF-8字符
}

// NewToolJSONAggregator 创建工具 JSON 聚合器
func NewToolJSONAggregator(callback ToolParamsUpdateCallback) *ToolJSONAggregator {
	logger.Debug("创建工具 JSON 聚合器 - has_callback: %v", callback != nil)
	return &ToolJSONAggregator{
		activeStreamers: make(map[string]*JSONStreamer),
		updateCallback:  callback,
	}
}

// ProcessToolData 处理工具调用数据片段
// AWS EventStream 按字节边界分片传输，导致 UTF-8 字符截断
// 只有在收到停止信号时才进行最终解析
func (tja *ToolJSONAggregator) ProcessToolData(toolUseID, name, input string, stop bool) (complete bool, fullInput string) {
	tja.mu.Lock()
	defer tja.mu.Unlock()

	// 处理独立的 input/stop 事件（没有 toolUseID）
	if toolUseID == "" {
		if tja.currentToolUseID != "" {
			toolUseID = tja.currentToolUseID
			logger.Debug("使用当前活跃的工具调用 ID 处理独立事件 - toolUseID: %s, hasInput: %v, stop: %v",
				toolUseID, input != "", stop)
		} else {
			logger.Warn("无法确定工具调用 ID，跳过事件 - name: %s, hasInput: %v, stop: %v",
				name, input != "", stop)
			return false, ""
		}
	}

	// 获取或创建流式解析器
	streamer, exists := tja.activeStreamers[toolUseID]
	if !exists {
		streamer = &JSONStreamer{
			toolUseID:  toolUseID,
			toolName:   name,
			buffer:     bytes.NewBuffer(nil),
			lastUpdate: time.Now(),
			result:     make(map[string]interface{}),
		}
		tja.activeStreamers[toolUseID] = streamer
		tja.currentToolUseID = toolUseID

		logger.Debug("创建 JSON 流式解析器 - toolUseID: %s, toolName: %s", toolUseID, name)
	}

	// 处理输入片段
	if input != "" {
		safeFragment := streamer.ensureUTF8Integrity(input)
		streamer.buffer.WriteString(safeFragment)
		streamer.lastUpdate = time.Now()
		streamer.fragmentCount++
		streamer.totalBytes += len(input)
	}

	// 只有在收到停止信号时才进行最终解析
	if !stop {
		return false, ""
	}

	// 尝试解析当前缓冲区
	parseResult := streamer.tryParse()

	logger.Debug("流式 JSON 解析完成 - toolUseID: %s, parseStatus: %s, hasValidJSON: %v, fragmentCount: %d, totalBytes: %d",
		toolUseID, parseResult, streamer.hasValidJSON, streamer.fragmentCount, streamer.totalBytes)

	streamer.isComplete = true

	if streamer.hasValidJSON && streamer.result != nil {
		jsonBytes, err := json.Marshal(streamer.result)
		if err != nil {
			logger.Error("JSON 序列化失败 - toolName: %s, error: %v", streamer.toolName, err)
			fullInput = "{}"
		} else {
			fullInput = string(jsonBytes)
		}
	} else {
		// 区分真正的错误和无参数工具
		if streamer.fragmentCount == 0 && streamer.totalBytes == 0 {
			logger.Debug("工具无参数，使用默认空对象 - toolName: %s", streamer.toolName)
		} else {
			logger.Error("流式解析失败，无有效 JSON 结果 - toolName: %s, toolUseID: %s, buffer: %s",
				streamer.toolName, streamer.toolUseID, streamer.buffer.String())
		}
		fullInput = "{}"
	}

	// 清理完成的流式解析器
	delete(tja.activeStreamers, toolUseID)

	// 清除当前活跃的工具调用 ID
	if tja.currentToolUseID == toolUseID {
		tja.currentToolUseID = ""
	}

	// 触发回调
	if tja.updateCallback != nil {
		tja.updateCallback(toolUseID, fullInput)
	}

	logger.Debug("流式 JSON 聚合完成 - toolUseID: %s, toolName: %s, totalFragments: %d, totalBytes: %d",
		toolUseID, name, streamer.fragmentCount, streamer.totalBytes)

	return true, fullInput
}

// ensureUTF8Integrity 确保 UTF-8 字符完整性
func (js *JSONStreamer) ensureUTF8Integrity(fragment string) string {
	if fragment == "" {
		return fragment
	}

	byteData := []byte(fragment)
	n := len(byteData)
	if n == 0 {
		return fragment
	}

	// 从末尾开始检查 UTF-8 字符边界
	for i := n - 1; i >= 0 && i >= n-4; i-- {
		b := byteData[i]

		if b&0x80 == 0 {
			// ASCII 字符，边界正确
			break
		} else if b&0xE0 == 0xC0 {
			// 2 字节 UTF-8 序列开始
			if n-i < 2 {
				logger.Debug("检测到截断的 UTF-8 字符(2字节) - toolUseID: %s", js.toolUseID)
				js.incompleteUTF8 = string(byteData[i:])
				return string(byteData[:i])
			}
			break
		} else if b&0xF0 == 0xE0 {
			// 3 字节 UTF-8 序列开始
			if n-i < 3 {
				logger.Debug("检测到截断的 UTF-8 字符(3字节) - toolUseID: %s", js.toolUseID)
				js.incompleteUTF8 = string(byteData[i:])
				return string(byteData[:i])
			}
			break
		} else if b&0xF8 == 0xF0 {
			// 4 字节 UTF-8 序列开始
			if n-i < 4 {
				logger.Debug("检测到截断的 UTF-8 字符(4字节) - toolUseID: %s", js.toolUseID)
				js.incompleteUTF8 = string(byteData[i:])
				return string(byteData[:i])
			}
			break
		}
	}

	// 检查是否有之前的不完整 UTF-8 字符需要拼接
	if js.incompleteUTF8 != "" {
		combined := js.incompleteUTF8 + fragment
		logger.Debug("恢复截断的 UTF-8 字符 - toolUseID: %s", js.toolUseID)
		js.incompleteUTF8 = ""
		return js.ensureUTF8Integrity(combined)
	}

	return fragment
}

// tryParse 尝试解析当前缓冲区
func (js *JSONStreamer) tryParse() string {
	content := js.buffer.Bytes()
	if len(content) == 0 {
		return "empty"
	}

	// 快速检测空对象/空数组（无参数工具）
	contentStr := strings.TrimSpace(string(content))
	if contentStr == "{}" || contentStr == "[]" {
		js.result = make(map[string]interface{})
		js.hasValidJSON = true
		return "complete"
	}

	// 尝试完整 JSON 解析
	var result map[string]interface{}
	if err := json.Unmarshal(content, &result); err == nil {
		js.result = result
		js.hasValidJSON = true
		logger.Debug("完整 JSON 解析成功 - toolUseID: %s, resultKeys: %d", js.toolUseID, len(result))
		return "complete"
	}

	return "invalid"
}

// GetActiveStreamerCount 获取活跃的流式解析器数量
func (tja *ToolJSONAggregator) GetActiveStreamerCount() int {
	tja.mu.RLock()
	defer tja.mu.RUnlock()
	return len(tja.activeStreamers)
}

// Reset 重置聚合器状态
func (tja *ToolJSONAggregator) Reset() {
	tja.mu.Lock()
	defer tja.mu.Unlock()
	tja.activeStreamers = make(map[string]*JSONStreamer)
	tja.currentToolUseID = ""
}
