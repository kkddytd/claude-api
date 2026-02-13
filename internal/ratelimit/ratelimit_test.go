// Package ratelimit 滑动窗口限流器测试
// @author ygw
package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// TestSlidingWindowLimiter_Basic 测试基本的限流功能
func TestSlidingWindowLimiter_Basic(t *testing.T) {
	limiter := NewSlidingWindowLimiter(time.Second * 2) // 2秒窗口
	defer limiter.Stop()

	key := "test-ip-1"
	limit := 5

	// 前5次请求应该允许
	for i := 0; i < 5; i++ {
		allowed, count, remaining := limiter.Allow(key, limit)
		if !allowed {
			t.Errorf("第%d次请求应该被允许，但被拒绝了", i+1)
		}
		if count != i+1 {
			t.Errorf("第%d次请求后计数应为%d，实际为%d", i+1, i+1, count)
		}
		if remaining != limit-i-1 {
			t.Errorf("第%d次请求后剩余应为%d，实际为%d", i+1, limit-i-1, remaining)
		}
	}

	// 第6次请求应该被拒绝
	allowed, count, remaining := limiter.Allow(key, limit)
	if allowed {
		t.Error("第6次请求应该被拒绝，但被允许了")
	}
	if count != 5 {
		t.Errorf("被拒绝时计数应为5，实际为%d", count)
	}
	if remaining != 0 {
		t.Errorf("被拒绝时剩余应为0，实际为%d", remaining)
	}
}

// TestSlidingWindowLimiter_WindowExpiry 测试窗口过期后重置
func TestSlidingWindowLimiter_WindowExpiry(t *testing.T) {
	limiter := NewSlidingWindowLimiter(time.Millisecond * 100) // 100毫秒窗口
	defer limiter.Stop()

	key := "test-ip-2"
	limit := 3

	// 先发送3次请求
	for i := 0; i < 3; i++ {
		allowed, _, _ := limiter.Allow(key, limit)
		if !allowed {
			t.Errorf("第%d次请求应该被允许", i+1)
		}
	}

	// 第4次应该被拒绝
	allowed, _, _ := limiter.Allow(key, limit)
	if allowed {
		t.Error("第4次请求应该被拒绝")
	}

	// 等待窗口过期
	time.Sleep(time.Millisecond * 150)

	// 窗口过期后应该允许新请求
	allowed, count, _ := limiter.Allow(key, limit)
	if !allowed {
		t.Error("窗口过期后应该允许新请求")
	}
	if count != 1 {
		t.Errorf("窗口过期后计数应为1，实际为%d", count)
	}
}

// TestSlidingWindowLimiter_ZeroLimit 测试limit=0时不限制
func TestSlidingWindowLimiter_ZeroLimit(t *testing.T) {
	limiter := NewSlidingWindowLimiter(time.Second)
	defer limiter.Stop()

	key := "test-ip-3"

	// limit=0时不限制
	for i := 0; i < 100; i++ {
		allowed, _, remaining := limiter.Allow(key, 0)
		if !allowed {
			t.Errorf("limit=0时第%d次请求应该被允许", i+1)
		}
		if remaining != -1 {
			t.Errorf("limit=0时remaining应为-1，实际为%d", remaining)
		}
	}
}

// TestSlidingWindowLimiter_MultipleKeys 测试多个key独立限流
func TestSlidingWindowLimiter_MultipleKeys(t *testing.T) {
	limiter := NewSlidingWindowLimiter(time.Second * 2)
	defer limiter.Stop()

	limit := 2

	// 两个不同的key应该独立限流
	for i := 0; i < 2; i++ {
		allowed1, _, _ := limiter.Allow("key1", limit)
		allowed2, _, _ := limiter.Allow("key2", limit)
		if !allowed1 || !allowed2 {
			t.Errorf("第%d次请求：key1和key2都应该被允许", i+1)
		}
	}

	// 两个key都应该达到限制
	allowed1, _, _ := limiter.Allow("key1", limit)
	allowed2, _, _ := limiter.Allow("key2", limit)
	if allowed1 || allowed2 {
		t.Error("第3次请求：key1和key2都应该被拒绝")
	}
}

// TestSlidingWindowLimiter_Concurrent 测试并发安全
func TestSlidingWindowLimiter_Concurrent(t *testing.T) {
	limiter := NewSlidingWindowLimiter(time.Second * 5)
	defer limiter.Stop()

	key := "concurrent-test"
	limit := 100
	goroutines := 50
	requestsPerGoroutine := 10

	var wg sync.WaitGroup
	allowedCount := int64(0)
	var mu sync.Mutex

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				allowed, _, _ := limiter.Allow(key, limit)
				if allowed {
					mu.Lock()
					allowedCount++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	// 总共500次请求，限制100，应该正好有100次被允许
	if allowedCount != int64(limit) {
		t.Errorf("并发测试：允许的请求数应为%d，实际为%d", limit, allowedCount)
	}
}

// TestSlidingWindowLimiter_GetCount 测试获取当前计数
func TestSlidingWindowLimiter_GetCount(t *testing.T) {
	limiter := NewSlidingWindowLimiter(time.Second * 2)
	defer limiter.Stop()

	key := "count-test"
	limit := 10

	// 初始计数应该为0
	if count := limiter.GetCount(key); count != 0 {
		t.Errorf("初始计数应为0，实际为%d", count)
	}

	// 发送5次请求
	for i := 0; i < 5; i++ {
		limiter.Allow(key, limit)
	}

	// 计数应该为5
	if count := limiter.GetCount(key); count != 5 {
		t.Errorf("发送5次请求后计数应为5，实际为%d", count)
	}
}

// TestSlidingWindowLimiter_Reset 测试重置功能
func TestSlidingWindowLimiter_Reset(t *testing.T) {
	limiter := NewSlidingWindowLimiter(time.Second * 2)
	defer limiter.Stop()

	key := "reset-test"
	limit := 5

	// 发送5次请求达到限制
	for i := 0; i < 5; i++ {
		limiter.Allow(key, limit)
	}

	// 应该被拒绝
	allowed, _, _ := limiter.Allow(key, limit)
	if allowed {
		t.Error("达到限制后应该被拒绝")
	}

	// 重置
	limiter.Reset(key)

	// 重置后应该允许
	allowed, count, _ := limiter.Allow(key, limit)
	if !allowed {
		t.Error("重置后应该允许")
	}
	if count != 1 {
		t.Errorf("重置后计数应为1，实际为%d", count)
	}
}

// TestDualLimiter_Basic 测试双重限流器
func TestDualLimiter_Basic(t *testing.T) {
	limiter := NewDualLimiter(time.Second * 2)
	defer limiter.Stop()

	ip := "192.168.1.1"
	apiKey := "sk-test-key"
	ipLimit := 10
	apiKeyLimit := 5

	// IP限流测试
	for i := 0; i < 10; i++ {
		result := limiter.CheckIP(ip, ipLimit)
		if !result.Allowed {
			t.Errorf("IP第%d次请求应该被允许", i+1)
		}
		if result.Type != "ip" {
			t.Errorf("类型应为ip，实际为%s", result.Type)
		}
	}

	// IP第11次应该被拒绝
	result := limiter.CheckIP(ip, ipLimit)
	if result.Allowed {
		t.Error("IP第11次请求应该被拒绝")
	}

	// API Key限流测试（独立于IP）
	for i := 0; i < 5; i++ {
		result := limiter.CheckAPIKey(apiKey, apiKeyLimit)
		if !result.Allowed {
			t.Errorf("API Key第%d次请求应该被允许", i+1)
		}
		if result.Type != "apikey" {
			t.Errorf("类型应为apikey，实际为%s", result.Type)
		}
	}

	// API Key第6次应该被拒绝
	result = limiter.CheckAPIKey(apiKey, apiKeyLimit)
	if result.Allowed {
		t.Error("API Key第6次请求应该被拒绝")
	}
}

// TestDualLimiter_Stats 测试统计信息
func TestDualLimiter_Stats(t *testing.T) {
	limiter := NewDualLimiter(time.Second)
	defer limiter.Stop()

	// 添加一些数据
	limiter.CheckIP("ip1", 10)
	limiter.CheckIP("ip2", 10)
	limiter.CheckAPIKey("key1", 10)

	stats := limiter.Stats()
	if stats == nil {
		t.Error("统计信息不应为nil")
	}

	ipStats := stats["ip_limiter"].(map[string]interface{})
	if ipStats["active_keys"].(int) != 2 {
		t.Errorf("IP活跃key数应为2，实际为%d", ipStats["active_keys"])
	}

	apiKeyStats := stats["apikey_limiter"].(map[string]interface{})
	if apiKeyStats["active_keys"].(int) != 1 {
		t.Errorf("API Key活跃key数应为1，实际为%d", apiKeyStats["active_keys"])
	}
}

// TestSlidingWindowLimiter_SlidingBehavior 测试滑动窗口行为（非固定窗口）
func TestSlidingWindowLimiter_SlidingBehavior(t *testing.T) {
	windowSize := time.Millisecond * 200
	limiter := NewSlidingWindowLimiter(windowSize)
	defer limiter.Stop()

	key := "sliding-test"
	limit := 4

	// 在窗口开始时发送2次请求
	limiter.Allow(key, limit)
	limiter.Allow(key, limit)

	// 等待半个窗口时间
	time.Sleep(time.Millisecond * 100)

	// 再发送2次请求
	limiter.Allow(key, limit)
	limiter.Allow(key, limit)

	// 此时窗口内有4次请求，第5次应该被拒绝
	allowed, _, _ := limiter.Allow(key, limit)
	if allowed {
		t.Error("窗口内达到4次请求后，第5次应该被拒绝")
	}

	// 再等待半个窗口时间，最初的2次请求应该过期
	time.Sleep(time.Millisecond * 120)

	// 现在窗口内只有2次请求，应该允许新请求
	allowed, count, _ := limiter.Allow(key, limit)
	if !allowed {
		t.Error("部分请求过期后应该允许新请求")
	}
	// 滑动窗口应该有3次请求（2次未过期 + 1次新请求）
	if count != 3 {
		t.Errorf("滑动窗口内计数应为3，实际为%d", count)
	}
}
