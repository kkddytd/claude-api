package stream

import (
	"testing"
)

// TestToolJSONAggregator_BasicFragment 测试基本 JSON 片段聚合
func TestToolJSONAggregator_BasicFragment(t *testing.T) {
	var callbackResult string
	aggregator := NewToolJSONAggregator(func(toolUseID, fullParams string) {
		callbackResult = fullParams
	})

	toolID := "tool_123"
	toolName := "search"

	// 发送完整 JSON
	complete, result := aggregator.ProcessToolData(toolID, toolName, `{"pattern":"**/*.go"}`, true)

	if !complete {
		t.Error("完整 JSON 应该标记为完成")
	}

	if result != `{"pattern":"**/*.go"}` {
		t.Errorf("期望完整 JSON，实际: %s", result)
	}

	if callbackResult != result {
		t.Errorf("回调结果不匹配 - 期望: %s, 实际: %s", result, callbackResult)
	}
}

// TestToolJSONAggregator_FragmentedJSON 测试分片 JSON 聚合
func TestToolJSONAggregator_FragmentedJSON(t *testing.T) {
	aggregator := NewToolJSONAggregator(nil)

	toolID := "tool_456"
	toolName := "read_file"

	// 发送第一个片段
	complete, _ := aggregator.ProcessToolData(toolID, toolName, `{"pattern"`, false)
	if complete {
		t.Error("第一个片段不应该标记为完成")
	}

	// 发送第二个片段
	complete, _ = aggregator.ProcessToolData(toolID, toolName, `:"**/*.go"`, false)
	if complete {
		t.Error("第二个片段不应该标记为完成")
	}

	// 发送停止信号
	complete, result := aggregator.ProcessToolData(toolID, toolName, `}`, true)
	if !complete {
		t.Error("停止信号后应该标记为完成")
	}

	if result != `{"pattern":"**/*.go"}` {
		t.Errorf("期望聚合后的完整 JSON，实际: %s", result)
	}
}

// TestToolJSONAggregator_EmptyInput 测试空输入（无参数工具）
func TestToolJSONAggregator_EmptyInput(t *testing.T) {
	aggregator := NewToolJSONAggregator(nil)

	toolID := "tool_789"
	toolName := "no_params_tool"

	// 直接发送停止信号（无参数工具）
	complete, result := aggregator.ProcessToolData(toolID, toolName, "", true)

	if !complete {
		t.Error("空输入工具应该标记为完成")
	}

	if result != "{}" {
		t.Errorf("空输入工具应该返回空对象，实际: %s", result)
	}
}

// TestToolJSONAggregator_EmptyObject 测试空对象
func TestToolJSONAggregator_EmptyObject(t *testing.T) {
	aggregator := NewToolJSONAggregator(nil)

	toolID := "tool_abc"
	toolName := "empty_params"

	// 发送空对象
	complete, result := aggregator.ProcessToolData(toolID, toolName, `{}`, true)

	if !complete {
		t.Error("空对象应该标记为完成")
	}

	if result != "{}" {
		t.Errorf("空对象应该保持为空对象，实际: %s", result)
	}
}

// TestToolJSONAggregator_MultipleTools 测试多个工具调用
func TestToolJSONAggregator_MultipleTools(t *testing.T) {
	results := make(map[string]string)
	aggregator := NewToolJSONAggregator(func(toolUseID, fullParams string) {
		results[toolUseID] = fullParams
	})

	// 第一个工具
	complete1, result1 := aggregator.ProcessToolData("tool_1", "search", `{"query":"test"}`, true)
	if !complete1 || result1 != `{"query":"test"}` {
		t.Errorf("第一个工具聚合失败 - complete: %v, result: %s", complete1, result1)
	}

	// 第二个工具
	complete2, result2 := aggregator.ProcessToolData("tool_2", "read", `{"path":"file.txt"}`, true)
	if !complete2 || result2 != `{"path":"file.txt"}` {
		t.Errorf("第二个工具聚合失败 - complete: %v, result: %s", complete2, result2)
	}

	// 验证回调结果
	if results["tool_1"] != `{"query":"test"}` {
		t.Errorf("tool_1 回调结果不匹配: %s", results["tool_1"])
	}
	if results["tool_2"] != `{"path":"file.txt"}` {
		t.Errorf("tool_2 回调结果不匹配: %s", results["tool_2"])
	}
}

// TestToolJSONAggregator_OrphanInput 测试孤立的输入事件（使用当前活跃 ID）
func TestToolJSONAggregator_OrphanInput(t *testing.T) {
	aggregator := NewToolJSONAggregator(nil)

	toolID := "tool_orphan"
	toolName := "orphan_tool"

	// 先注册工具
	aggregator.ProcessToolData(toolID, toolName, `{"key":`, false)

	// 发送孤立的输入事件（没有 toolUseID）
	complete, _ := aggregator.ProcessToolData("", "", `"value"`, false)
	if complete {
		t.Error("孤立输入不应该标记为完成")
	}

	// 发送停止信号
	complete, result := aggregator.ProcessToolData(toolID, toolName, `}`, true)
	if !complete {
		t.Error("停止后应该标记为完成")
	}

	if result != `{"key":"value"}` {
		t.Errorf("期望聚合后的 JSON，实际: %s", result)
	}
}

// TestToolJSONAggregator_UTF8Handling 测试 UTF-8 字符处理
func TestToolJSONAggregator_UTF8Handling(t *testing.T) {
	aggregator := NewToolJSONAggregator(nil)

	toolID := "tool_utf8"
	toolName := "utf8_tool"

	// 发送包含中文的 JSON
	complete, result := aggregator.ProcessToolData(toolID, toolName, `{"message":"你好世界"}`, true)

	if !complete {
		t.Error("UTF-8 JSON 应该标记为完成")
	}

	if result != `{"message":"你好世界"}` {
		t.Errorf("UTF-8 JSON 应该正确保留，实际: %s", result)
	}
}

// TestToolJSONAggregator_Reset 测试重置功能
func TestToolJSONAggregator_Reset(t *testing.T) {
	aggregator := NewToolJSONAggregator(nil)

	// 添加一些活跃的流式解析器
	aggregator.ProcessToolData("tool_1", "test", `{"key":`, false)
	aggregator.ProcessToolData("tool_2", "test", `{"key":`, false)

	if aggregator.GetActiveStreamerCount() != 2 {
		t.Errorf("期望 2 个活跃的流式解析器，实际 %d", aggregator.GetActiveStreamerCount())
	}

	// 重置
	aggregator.Reset()

	if aggregator.GetActiveStreamerCount() != 0 {
		t.Errorf("重置后期望 0 个活跃的流式解析器，实际 %d", aggregator.GetActiveStreamerCount())
	}
}

// TestToolJSONAggregator_ComplexJSON 测试复杂嵌套 JSON
func TestToolJSONAggregator_ComplexJSON(t *testing.T) {
	aggregator := NewToolJSONAggregator(nil)

	toolID := "tool_complex"
	toolName := "complex_tool"

	// 分片发送复杂 JSON
	aggregator.ProcessToolData(toolID, toolName, `{"options":{`, false)
	aggregator.ProcessToolData(toolID, toolName, `"recursive":true,`, false)
	aggregator.ProcessToolData(toolID, toolName, `"include":["*.go","*.ts"]`, false)
	complete, result := aggregator.ProcessToolData(toolID, toolName, `}}`, true)

	if !complete {
		t.Error("复杂 JSON 应该标记为完成")
	}

	// 验证解析结果
	if result == "{}" {
		t.Error("复杂 JSON 不应该解析为空对象")
	}
}
