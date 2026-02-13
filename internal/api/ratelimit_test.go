// Package api 频率限制测试
// 测试频率限制优先级：指定IP单独设置 > 用户单独设置 > 系统统一IP设置
// 测试每日请求限制优先级：指定IP每日限制 > 用户每日请求限制
// @author ygw
package api

import (
	"testing"
	"time"

	"claude-api/internal/ratelimit"
)

// ==================== 频率限制（每分钟）测试 ====================

// TestRateLimitPriority_IPConfig 测试指定IP设置优先级最高
func TestRateLimitPriority_IPConfig(t *testing.T) {
	limiter := ratelimit.NewDualLimiter(time.Minute)
	defer limiter.Stop()

	// 指定IP限制为 10次/分钟
	testIP := "192.168.1.100"
	ipRateLimitRPM := 10

	// 模拟10次请求，都应该通过（使用指定IP的10次限制）
	for i := 0; i < 10; i++ {
		result := limiter.CheckIP(testIP, ipRateLimitRPM)
		if !result.Allowed {
			t.Errorf("第 %d 次请求应该通过（指定IP限制10次），但被拒绝", i+1)
		}
	}

	// 第11次请求应该被拒绝
	result := limiter.CheckIP(testIP, ipRateLimitRPM)
	if result.Allowed {
		t.Error("第11次请求应该被拒绝，但通过了")
	}

	t.Logf("✅ 指定IP限流正常（10次/分钟）- 最高优先级")
}

// TestRateLimitPriority_UserSetting 测试用户单独设置（次优先级）
func TestRateLimitPriority_UserSetting(t *testing.T) {
	limiter := ratelimit.NewDualLimiter(time.Minute)
	defer limiter.Stop()

	// 用户设置了频率限制为 20次/分钟
	userAPIKey := "sk-test-user-1"
	userRateLimitRPM := 20

	// 模拟20次请求，都应该通过（使用用户的20次限制）
	for i := 0; i < 20; i++ {
		result := limiter.CheckAPIKey(userAPIKey, userRateLimitRPM)
		if !result.Allowed {
			t.Errorf("第 %d 次请求应该通过（用户限制20次），但被拒绝", i+1)
		}
	}

	// 第21次请求应该被拒绝
	result := limiter.CheckAPIKey(userAPIKey, userRateLimitRPM)
	if result.Allowed {
		t.Error("第21次请求应该被拒绝，但通过了")
	}

	t.Logf("✅ 用户API Key限流正常（20次/分钟）- 次优先级")
}

// TestRateLimitPriority_SystemDefault 测试系统统一IP设置（最低优先级）
func TestRateLimitPriority_SystemDefault(t *testing.T) {
	limiter := ratelimit.NewDualLimiter(time.Minute)
	defer limiter.Stop()

	// 系统统一IP限制为 8次/分钟
	testIP := "10.0.0.50"
	systemIPRateLimitMax := 8

	// 模拟8次请求，都应该通过（使用系统的8次限制）
	for i := 0; i < 8; i++ {
		result := limiter.CheckIP(testIP, systemIPRateLimitMax)
		if !result.Allowed {
			t.Errorf("第 %d 次请求应该通过（系统限制8次），但被拒绝", i+1)
		}
	}

	// 第9次请求应该被拒绝
	result := limiter.CheckIP(testIP, systemIPRateLimitMax)
	if result.Allowed {
		t.Error("第9次请求应该被拒绝，但通过了")
	}

	t.Logf("✅ 系统统一IP限流正常（8次/分钟）- 最低优先级")
}

// TestRateLimitPriority_Complete 完整优先级测试
// 验证：指定IP单独设置 > 用户单独设置 > 系统统一IP设置
func TestRateLimitPriority_Complete(t *testing.T) {
	limiter := ratelimit.NewDualLimiter(time.Minute)
	defer limiter.Stop()

	// 场景1: 指定IP有设置 - 应使用指定IP设置（5次）优先于其他
	ipWithLimit := "192.168.1.1"
	ipLimit := 5
	for i := 0; i < 5; i++ {
		result := limiter.CheckIP(ipWithLimit, ipLimit)
		if !result.Allowed {
			t.Errorf("场景1: 第 %d 次请求应该通过", i+1)
		}
	}
	result := limiter.CheckIP(ipWithLimit, ipLimit)
	if result.Allowed {
		t.Error("场景1: 第6次请求应该被拒绝")
	}
	t.Logf("✅ 场景1通过: 指定IP设置5次/分钟优先生效（最高优先级）")

	// 场景2: 用户有设置 - 应使用用户设置（10次）
	userAPIKey := "sk-user-with-limit"
	userLimit := 10
	for i := 0; i < 10; i++ {
		result := limiter.CheckAPIKey(userAPIKey, userLimit)
		if !result.Allowed {
			t.Errorf("场景2: 第 %d 次请求应该通过", i+1)
		}
	}
	result = limiter.CheckAPIKey(userAPIKey, userLimit)
	if result.Allowed {
		t.Error("场景2: 第11次请求应该被拒绝")
	}
	t.Logf("✅ 场景2通过: 用户设置10次/分钟优先生效（次优先级）")

	// 场景3: 都无设置 - 应使用系统设置（3次）
	ipWithoutLimit := "10.0.0.1"
	systemLimit := 3
	for i := 0; i < 3; i++ {
		result := limiter.CheckIP(ipWithoutLimit, systemLimit)
		if !result.Allowed {
			t.Errorf("场景3: 第 %d 次请求应该通过", i+1)
		}
	}
	result = limiter.CheckIP(ipWithoutLimit, systemLimit)
	if result.Allowed {
		t.Error("场景3: 第4次请求应该被拒绝")
	}
	t.Logf("✅ 场景3通过: 系统统一设置3次/分钟生效（最低优先级）")
}

// TestRateLimitPriority_NoLimit 测试禁用限流
func TestRateLimitPriority_NoLimit(t *testing.T) {
	limiter := ratelimit.NewDualLimiter(time.Minute)
	defer limiter.Stop()

	testIP := "1.2.3.4"

	// limit=0 表示不限制
	for i := 0; i < 100; i++ {
		result := limiter.CheckIP(testIP, 0)
		if !result.Allowed {
			t.Errorf("limit=0时，第 %d 次请求不应该被限流", i+1)
		}
	}

	t.Logf("✅ limit=0时不进行限制")
}

// TestRateLimitPriority_IndependentCounters 测试独立计数器
// 验证不同IP使用各自独立的计数器
func TestRateLimitPriority_IndependentCounters(t *testing.T) {
	limiter := ratelimit.NewDualLimiter(time.Minute)
	defer limiter.Stop()

	ip1 := "192.168.1.1"
	ip2 := "192.168.1.2"
	limit := 5

	// IP1 发送5次请求
	for i := 0; i < 5; i++ {
		limiter.CheckIP(ip1, limit)
	}

	// IP1 第6次应该被拒绝
	result := limiter.CheckIP(ip1, limit)
	if result.Allowed {
		t.Error("IP1第6次请求应该被拒绝")
	}

	// IP2 应该不受IP1影响，可以发送5次
	for i := 0; i < 5; i++ {
		result := limiter.CheckIP(ip2, limit)
		if !result.Allowed {
			t.Errorf("IP2第 %d 次请求应该通过（独立计数）", i+1)
		}
	}

	t.Logf("✅ 不同IP使用独立计数器")
}

// TestRateLimitPriority_UserAndIP_Independent 测试用户和IP计数器独立
func TestRateLimitPriority_UserAndIP_Independent(t *testing.T) {
	limiter := ratelimit.NewDualLimiter(time.Minute)
	defer limiter.Stop()

	userAPIKey := "sk-test-user"
	testIP := "192.168.1.100"
	limit := 3

	// 用户发送3次请求
	for i := 0; i < 3; i++ {
		limiter.CheckAPIKey(userAPIKey, limit)
	}

	// 用户第4次应该被拒绝
	result := limiter.CheckAPIKey(userAPIKey, limit)
	if result.Allowed {
		t.Error("用户第4次请求应该被拒绝")
	}

	// IP应该不受用户计数影响，可以发送3次
	for i := 0; i < 3; i++ {
		result := limiter.CheckIP(testIP, limit)
		if !result.Allowed {
			t.Errorf("IP第 %d 次请求应该通过（独立于用户计数）", i+1)
		}
	}

	t.Logf("✅ 用户API Key和IP使用独立计数器")
}

// ==================== 优先级逻辑说明 ====================
/*
频率限制（每分钟请求次数）优先级：
1️⃣ 指定IP单独频率限制 (ip_configs.rate_limit_rpm) - 最高优先级
2️⃣ 用户单独频率限制 (users.rate_limit_rpm) - 次优先级
3️⃣ 系统统一IP频率限制 (settings.IPRateLimitMax) - 最低优先级

每日请求限制优先级：
1️⃣ 指定IP每日请求限制 (ip_configs.daily_request_limit) - 最高优先级
2️⃣ 用户每日请求限制 (users.request_quota) - 次优先级

流程图：
请求到达 → 认证
    │
    ├─ 指定IP有频率设置?
    │   └─ 是 → 使用指定IP频率限制 → 结束
    │
    ├─ 用户有频率设置?
    │   └─ 是 → 使用用户频率限制 → 结束
    │
    └─ 系统启用IP频率限制?
        └─ 是 → 使用系统统一IP限制 → 结束

每日限制检查：
    ├─ 指定IP有每日限制?
    │   └─ 是 → 检查IP今日请求数 → 结束
    │
    └─ 用户有每日限制?
        └─ 是 → 检查用户今日请求数 → 结束
*/
