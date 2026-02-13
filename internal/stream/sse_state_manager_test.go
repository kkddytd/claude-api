package stream

import (
	"strings"
	"testing"
)

// TestSSEStateManager_MessageStart 测试 message_start 事件处理
func TestSSEStateManager_MessageStart(t *testing.T) {
	ssm := NewSSEStateManager(false)

	// 第一次 message_start 应该成功
	events, err := ssm.ValidateAndSendEvent("message_start", map[string]interface{}{
		"type": "message_start",
	})
	if err != nil {
		t.Fatalf("第一次 message_start 失败: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("期望 1 个事件，实际 %d", len(events))
	}

	// 第二次 message_start 应该被跳过（非严格模式）
	events, err = ssm.ValidateAndSendEvent("message_start", map[string]interface{}{
		"type": "message_start",
	})
	if err != nil {
		t.Fatalf("第二次 message_start 不应返回错误: %v", err)
	}
	if events != nil {
		t.Errorf("重复的 message_start 应该返回 nil events")
	}
}

// TestSSEStateManager_MessageStartStrict 测试严格模式下的 message_start
func TestSSEStateManager_MessageStartStrict(t *testing.T) {
	ssm := NewSSEStateManager(true) // 严格模式

	// 第一次成功
	_, err := ssm.ValidateAndSendEvent("message_start", map[string]interface{}{
		"type": "message_start",
	})
	if err != nil {
		t.Fatalf("第一次 message_start 失败: %v", err)
	}

	// 第二次应该返回错误
	_, err = ssm.ValidateAndSendEvent("message_start", map[string]interface{}{
		"type": "message_start",
	})
	if err == nil {
		t.Error("严格模式下重复的 message_start 应该返回错误")
	}
}

// TestSSEStateManager_ContentBlockSequence 测试内容块序列
func TestSSEStateManager_ContentBlockSequence(t *testing.T) {
	ssm := NewSSEStateManager(false)

	// 先发送 message_start
	ssm.ValidateAndSendEvent("message_start", map[string]interface{}{
		"type": "message_start",
	})

	// 发送 content_block_start
	events, err := ssm.ValidateAndSendEvent("content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	})
	if err != nil {
		t.Fatalf("content_block_start 失败: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("期望 1 个事件，实际 %d", len(events))
	}

	// 发送 content_block_delta
	events, err = ssm.ValidateAndSendEvent("content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]interface{}{
			"type": "text_delta",
			"text": "Hello",
		},
	})
	if err != nil {
		t.Fatalf("content_block_delta 失败: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("期望 1 个事件，实际 %d", len(events))
	}

	// 发送 content_block_stop
	events, err = ssm.ValidateAndSendEvent("content_block_stop", map[string]interface{}{
		"type":  "content_block_stop",
		"index": 0,
	})
	if err != nil {
		t.Fatalf("content_block_stop 失败: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("期望 1 个事件，实际 %d", len(events))
	}
}

// TestSSEStateManager_AutoCloseBlockBeforeMessageDelta 测试 message_delta 前自动关闭块
func TestSSEStateManager_AutoCloseBlockBeforeMessageDelta(t *testing.T) {
	ssm := NewSSEStateManager(false)

	// 发送 message_start
	ssm.ValidateAndSendEvent("message_start", map[string]interface{}{
		"type": "message_start",
	})

	// 发送 content_block_start
	ssm.ValidateAndSendEvent("content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	})

	// 发送 content_block_delta（不关闭）
	ssm.ValidateAndSendEvent("content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]interface{}{
			"type": "text_delta",
			"text": "Hello",
		},
	})

	// 发送 message_delta（应该自动关闭之前的块）
	events, err := ssm.ValidateAndSendEvent("message_delta", map[string]interface{}{
		"type":  "message_delta",
		"delta": map[string]interface{}{"stop_reason": "end_turn"},
		"usage": map[string]int{"output_tokens": 10},
	})
	if err != nil {
		t.Fatalf("message_delta 失败: %v", err)
	}

	// 应该有 2 个事件：content_block_stop + message_delta
	if len(events) != 2 {
		t.Errorf("期望 2 个事件（自动关闭块 + message_delta），实际 %d", len(events))
	}

	// 第一个应该是 content_block_stop
	if len(events) >= 1 && !strings.Contains(events[0], "content_block_stop") {
		t.Errorf("第一个事件应该是 content_block_stop，实际: %s", events[0])
	}
}

// TestSSEStateManager_PreventDuplicateMessageDelta 测试防止重复 message_delta
func TestSSEStateManager_PreventDuplicateMessageDelta(t *testing.T) {
	ssm := NewSSEStateManager(false)

	// 发送 message_start
	ssm.ValidateAndSendEvent("message_start", map[string]interface{}{
		"type": "message_start",
	})

	// 第一次 message_delta
	events, _ := ssm.ValidateAndSendEvent("message_delta", map[string]interface{}{
		"type":  "message_delta",
		"delta": map[string]interface{}{"stop_reason": "end_turn"},
		"usage": map[string]int{"output_tokens": 10},
	})
	if len(events) == 0 {
		t.Error("第一次 message_delta 应该成功发送")
	}

	// 第二次 message_delta 应该被跳过
	events, _ = ssm.ValidateAndSendEvent("message_delta", map[string]interface{}{
		"type":  "message_delta",
		"delta": map[string]interface{}{"stop_reason": "end_turn"},
		"usage": map[string]int{"output_tokens": 20},
	})
	if events != nil {
		t.Error("重复的 message_delta 应该被跳过")
	}
}

// TestSSEStateManager_ThinkingBlockSignature 测试 thinking 块自动添加 signature
func TestSSEStateManager_ThinkingBlockSignature(t *testing.T) {
	ssm := NewSSEStateManager(false)

	// 发送 message_start
	ssm.ValidateAndSendEvent("message_start", map[string]interface{}{
		"type": "message_start",
	})

	// 发送 thinking 块（不带 signature）
	contentBlock := map[string]interface{}{
		"type":     "thinking",
		"thinking": "",
	}
	events, err := ssm.ValidateAndSendEvent("content_block_start", map[string]interface{}{
		"type":          "content_block_start",
		"index":         0,
		"content_block": contentBlock,
	})
	if err != nil {
		t.Fatalf("thinking 块 content_block_start 失败: %v", err)
	}

	// 检查是否添加了 signature
	if _, hasSignature := contentBlock["signature"]; !hasSignature {
		t.Error("thinking 块应该自动添加 signature 字段")
	}

	// 检查事件是否包含 signature
	if len(events) > 0 && !strings.Contains(events[0], "signature") {
		t.Error("输出的 SSE 事件应该包含 signature 字段")
	}
}

// TestSSEStateManager_AutoStartBlockOnDelta 测试 delta 事件自动启动块
func TestSSEStateManager_AutoStartBlockOnDelta(t *testing.T) {
	ssm := NewSSEStateManager(false)

	// 发送 message_start
	ssm.ValidateAndSendEvent("message_start", map[string]interface{}{
		"type": "message_start",
	})

	// 直接发送 content_block_delta（没有 content_block_start）
	events, err := ssm.ValidateAndSendEvent("content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]interface{}{
			"type": "text_delta",
			"text": "Hello",
		},
	})
	if err != nil {
		t.Fatalf("content_block_delta 失败: %v", err)
	}

	// 应该有 2 个事件：自动生成的 content_block_start + content_block_delta
	if len(events) != 2 {
		t.Errorf("期望 2 个事件（自动启动块 + delta），实际 %d", len(events))
	}

	// 第一个应该是 content_block_start
	if len(events) >= 1 && !strings.Contains(events[0], "content_block_start") {
		t.Errorf("第一个事件应该是 content_block_start，实际: %s", events[0])
	}
}

// TestSSEStateManager_AutoCloseTextBlockBeforeToolBlock 测试工具块前自动关闭文本块
func TestSSEStateManager_AutoCloseTextBlockBeforeToolBlock(t *testing.T) {
	ssm := NewSSEStateManager(false)

	// 发送 message_start
	ssm.ValidateAndSendEvent("message_start", map[string]interface{}{
		"type": "message_start",
	})

	// 发送文本块
	ssm.ValidateAndSendEvent("content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	})

	// 发送工具块（应该自动关闭之前的文本块）
	events, err := ssm.ValidateAndSendEvent("content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": 1,
		"content_block": map[string]interface{}{
			"type":  "tool_use",
			"id":    "tool_123",
			"name":  "search",
			"input": map[string]interface{}{},
		},
	})
	if err != nil {
		t.Fatalf("工具块 content_block_start 失败: %v", err)
	}

	// 应该有 2 个事件：content_block_stop（关闭文本块）+ content_block_start（工具块）
	if len(events) != 2 {
		t.Errorf("期望 2 个事件（关闭文本块 + 启动工具块），实际 %d", len(events))
	}

	// 第一个应该是 content_block_stop
	if len(events) >= 1 && !strings.Contains(events[0], "content_block_stop") {
		t.Errorf("第一个事件应该是 content_block_stop，实际: %s", events[0])
	}
}
