// Package ratelimit 提供滑动窗口限流器实现
// 用于控制IP和API Key的请求频率
// @author ygw
package ratelimit

import (
	"sync"
	"time"
)

// SlidingWindowLimiter 滑动窗口限流器
// 使用滑动日志算法，记录每个请求的时间戳
// 统计窗口内的请求数，实现精确的频率限制
type SlidingWindowLimiter struct {
	mu           sync.RWMutex
	windowSize   time.Duration           // 滑动窗口大小（默认60秒）
	entries      map[string]*windowEntry // key -> 窗口条目（IP或API Key）
	cleanupTick  time.Duration           // 清理间隔
	stopCleanup  chan struct{}           // 停止清理信号
}

// windowEntry 滑动窗口条目
type windowEntry struct {
	mu         sync.Mutex
	timestamps []int64 // 请求时间戳列表（Unix纳秒）
}

// NewSlidingWindowLimiter 创建新的滑动窗口限流器
// windowSize: 滑动窗口大小，默认60秒
func NewSlidingWindowLimiter(windowSize time.Duration) *SlidingWindowLimiter {
	if windowSize <= 0 {
		windowSize = 60 * time.Second
	}

	limiter := &SlidingWindowLimiter{
		windowSize:  windowSize,
		entries:     make(map[string]*windowEntry),
		cleanupTick: 5 * time.Minute, // 每5分钟清理一次过期数据
		stopCleanup: make(chan struct{}),
	}

	// 启动后台清理协程
	go limiter.cleanupLoop()

	return limiter
}

// Allow 检查是否允许请求
// key: 限流键（IP地址或API Key）
// limit: 窗口内允许的最大请求数
// 返回: (是否允许, 当前窗口内请求数, 窗口内剩余配额)
func (l *SlidingWindowLimiter) Allow(key string, limit int) (allowed bool, count int, remaining int) {
	if limit <= 0 {
		// limit为0表示不限制
		return true, 0, -1
	}

	now := time.Now().UnixNano()
	windowStart := now - int64(l.windowSize)

	// 获取或创建条目
	l.mu.Lock()
	entry, exists := l.entries[key]
	if !exists {
		entry = &windowEntry{
			timestamps: make([]int64, 0, limit),
		}
		l.entries[key] = entry
	}
	l.mu.Unlock()

	// 操作条目
	entry.mu.Lock()
	defer entry.mu.Unlock()

	// 清理过期的时间戳
	validTimestamps := make([]int64, 0, len(entry.timestamps))
	for _, ts := range entry.timestamps {
		if ts > windowStart {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	entry.timestamps = validTimestamps

	// 检查是否超限
	count = len(entry.timestamps)
	remaining = limit - count

	if count >= limit {
		return false, count, 0
	}

	// 添加当前请求时间戳
	entry.timestamps = append(entry.timestamps, now)
	return true, count + 1, remaining - 1
}

// GetCount 获取指定key在当前窗口内的请求数
func (l *SlidingWindowLimiter) GetCount(key string) int {
	l.mu.RLock()
	entry, exists := l.entries[key]
	l.mu.RUnlock()

	if !exists {
		return 0
	}

	now := time.Now().UnixNano()
	windowStart := now - int64(l.windowSize)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	count := 0
	for _, ts := range entry.timestamps {
		if ts > windowStart {
			count++
		}
	}
	return count
}

// Reset 重置指定key的计数
func (l *SlidingWindowLimiter) Reset(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

// cleanupLoop 后台清理过期数据
func (l *SlidingWindowLimiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanupTick)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.cleanup()
		case <-l.stopCleanup:
			return
		}
	}
}

// cleanup 清理过期的条目
func (l *SlidingWindowLimiter) cleanup() {
	now := time.Now().UnixNano()
	windowStart := now - int64(l.windowSize)
	// 扩展清理时间，保留2倍窗口时间的数据
	expireThreshold := now - int64(l.windowSize)*2

	l.mu.Lock()
	defer l.mu.Unlock()

	for key, entry := range l.entries {
		entry.mu.Lock()

		// 检查是否所有时间戳都已过期
		allExpired := true
		for _, ts := range entry.timestamps {
			if ts > expireThreshold {
				allExpired = false
				break
			}
		}

		if allExpired {
			entry.mu.Unlock()
			delete(l.entries, key)
			continue
		}

		// 清理过期的时间戳
		validTimestamps := make([]int64, 0, len(entry.timestamps))
		for _, ts := range entry.timestamps {
			if ts > windowStart {
				validTimestamps = append(validTimestamps, ts)
			}
		}
		entry.timestamps = validTimestamps
		entry.mu.Unlock()
	}
}

// Stop 停止限流器的后台清理
func (l *SlidingWindowLimiter) Stop() {
	close(l.stopCleanup)
}

// Stats 返回限流器的统计信息
func (l *SlidingWindowLimiter) Stats() map[string]interface{} {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return map[string]interface{}{
		"window_size_seconds": l.windowSize.Seconds(),
		"active_keys":         len(l.entries),
	}
}

// RateLimitResult 限流检查结果
type RateLimitResult struct {
	Allowed   bool   // 是否允许
	Count     int    // 当前请求数
	Limit     int    // 限制数
	Remaining int    // 剩余配额
	Key       string // 限流键
	Type      string // 限流类型（ip/apikey）
}

// DualLimiter 双重限流器（IP + API Key）
type DualLimiter struct {
	ipLimiter     *SlidingWindowLimiter
	apiKeyLimiter *SlidingWindowLimiter
}

// NewDualLimiter 创建双重限流器
func NewDualLimiter(windowSize time.Duration) *DualLimiter {
	return &DualLimiter{
		ipLimiter:     NewSlidingWindowLimiter(windowSize),
		apiKeyLimiter: NewSlidingWindowLimiter(windowSize),
	}
}

// CheckIP 检查IP限流
func (d *DualLimiter) CheckIP(ip string, limit int) RateLimitResult {
	allowed, count, remaining := d.ipLimiter.Allow(ip, limit)
	return RateLimitResult{
		Allowed:   allowed,
		Count:     count,
		Limit:     limit,
		Remaining: remaining,
		Key:       ip,
		Type:      "ip",
	}
}

// CheckAPIKey 检查API Key限流
func (d *DualLimiter) CheckAPIKey(apiKey string, limit int) RateLimitResult {
	allowed, count, remaining := d.apiKeyLimiter.Allow(apiKey, limit)
	return RateLimitResult{
		Allowed:   allowed,
		Count:     count,
		Limit:     limit,
		Remaining: remaining,
		Key:       apiKey,
		Type:      "apikey",
	}
}

// GetIPCount 获取IP当前请求数
func (d *DualLimiter) GetIPCount(ip string) int {
	return d.ipLimiter.GetCount(ip)
}

// GetAPIKeyCount 获取API Key当前请求数
func (d *DualLimiter) GetAPIKeyCount(apiKey string) int {
	return d.apiKeyLimiter.GetCount(apiKey)
}

// ResetIP 重置IP计数
func (d *DualLimiter) ResetIP(ip string) {
	d.ipLimiter.Reset(ip)
}

// ResetAPIKey 重置API Key计数
func (d *DualLimiter) ResetAPIKey(apiKey string) {
	d.apiKeyLimiter.Reset(apiKey)
}

// Stop 停止双重限流器
func (d *DualLimiter) Stop() {
	d.ipLimiter.Stop()
	d.apiKeyLimiter.Stop()
}

// Stats 返回双重限流器的统计信息
func (d *DualLimiter) Stats() map[string]interface{} {
	return map[string]interface{}{
		"ip_limiter":     d.ipLimiter.Stats(),
		"apikey_limiter": d.apiKeyLimiter.Stats(),
	}
}
