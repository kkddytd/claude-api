package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"claude-api/internal/amazonq"
	"claude-api/internal/auth"
	"claude-api/internal/compressor"
	"claude-api/internal/config"
	"claude-api/internal/database"
	"claude-api/internal/logger"
	"claude-api/internal/models"
	"claude-api/internal/proxy"
	"claude-api/internal/ratelimit"
	syncpkg "claude-api/internal/sync"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Server 表示 API 服务器
type Server struct {
	cfg          *config.Config
	db           *database.DB
	aqClient     *amazonq.Client
	oidcClient   *auth.OIDCClient
	kiroClient   *auth.KiroClient       // Kiro 社交登录客户端
	compressor   *compressor.Compressor // 上下文压缩器
	proxyPool    *proxy.ProxyPool       // 代理池
	authSessions sync.Map               // 存储设备认证会话
	logChan      chan *models.RequestLog
	dbWriteChan  chan dbWriteOp // 数据库写操作队列
	logWg        sync.WaitGroup
	dbWriteWg    sync.WaitGroup
	version      string
	closing      atomic.Bool // 服务器是否正在关闭
	testMode     bool        // 测试模式（敏感操作需要密码验证）

	// IP 黑名单缓存
	blockedIPCache     sync.Map   // map[string]bool
	blockedIPCacheTime time.Time  // 缓存更新时间
	blockedIPCacheMu   sync.Mutex // 缓存更新锁

	// 双重限流器（IP + API Key）@author ygw
	rateLimiter *ratelimit.DualLimiter

	// 在线用户追踪
	onlineTracker *OnlineTracker // 在线 IP 追踪器

	// 高并发优化：内存缓存
	accountPool    *AccountPool    // 账号池缓存，避免每次请求查询数据库
	settingsCache  *SettingsCache  // 设置缓存
	tokenRefresher *TokenRefresher // 令牌刷新锁，避免重复刷新
	ipConfigCache  *IPConfigCache  // IP配置缓存，用于单独IP频率限制 @author ygw

	// 账号封控状态缓存（免费版使用）
	suspendedCache    sync.Map // map[accountID]suspendedCacheEntry
	suspendedCacheTTL time.Duration
	suspendedCacheMu  sync.Mutex
}

// suspendedCacheEntry 账号封控状态缓存条目
// @author ygw
type suspendedCacheEntry struct {
	suspended bool
	expireAt  time.Time
}

// OnlineTracker 在线用户追踪器
// @author ygw
type OnlineTracker struct {
	ips sync.Map // key: IP (string), value: 最后访问时间 (time.Time)
}

// RecordIP 记录 IP 访问
// @author ygw
func (ot *OnlineTracker) RecordIP(ip string) {
	ot.ips.Store(ip, time.Now())
}

// GetOnlineCount 获取当前在线用户数（5分钟内活跃的 IP 数量）
// @author ygw
func (ot *OnlineTracker) GetOnlineCount() int {
	count := 0
	now := time.Now()
	ot.ips.Range(func(key, value interface{}) bool {
		lastTime := value.(time.Time)
		if now.Sub(lastTime) <= 5*time.Minute {
			count++
		}
		return true
	})
	return count
}

// CleanupExpired 清理过期的 IP（超过 5 分钟）
// @author ygw
func (ot *OnlineTracker) CleanupExpired() {
	now := time.Now()
	ot.ips.Range(func(key, value interface{}) bool {
		lastTime := value.(time.Time)
		if now.Sub(lastTime) > 5*time.Minute {
			ot.ips.Delete(key)
		}
		return true
	})
}

// CleanupOnlineIPs 清理过期的在线 IP（供外部调用）
// @author ygw
func (s *Server) CleanupOnlineIPs() {
	s.onlineTracker.CleanupExpired()
}

// dbWriteOp 数据库写操作
type dbWriteOp struct {
	opType string
	data   interface{}
}

// statsUpdate 统计更新数据
type statsUpdate struct {
	accountID string
	success   bool
}

// tokenUsageUpdate token使用量更新数据
type tokenUsageUpdate struct {
	userID       string
	inputTokens  int
	outputTokens int
}

// NewServer 创建新的 API 服务器
func NewServer(cfg *config.Config, db *database.DB, version string) *Server {
	// 从配置文件读取测试模式
	testMode := false
	if fileConfig, err := config.LoadFromFile(); err == nil {
		testMode = fileConfig.Test
		if testMode {
			logger.Info("测试模式已启用 - 敏感操作需要密码验证")
		}
	}

	// 创建账号池缓存（30秒刷新间隔）
	accountPool := NewAccountPool(db, 30*time.Second)
	accountPool.SetConfig(
		cfg.LazyAccountPoolEnabled,
		cfg.LazyAccountPoolSize,
		cfg.LazyAccountPoolOrderBy,
		cfg.LazyAccountPoolOrderDesc,
		cfg.AccountSelectionMode,
	)

	// 创建设置缓存（30秒 TTL）
	settingsCache := NewSettingsCache(db, 30*time.Second)
	// 创建IP配置缓存（60秒 TTL）@author ygw
	ipConfigCache := NewIPConfigCache(db, 60*time.Second)

	s := &Server{
		cfg:               cfg,
		db:                db,
		aqClient:          amazonq.NewClient(cfg),
		oidcClient:        auth.NewOIDCClient(cfg),
		kiroClient:        auth.NewKiroClient(cfg),             // Kiro 社交登录客户端
		compressor:        compressor.New(nil),                 // 使用默认配置初始化压缩器
		logChan:           make(chan *models.RequestLog, 5000), // 扩容日志队列
		dbWriteChan:       make(chan dbWriteOp, 10000),         // 扩容数据库写队列
		version:           version,
		testMode:          testMode,
		onlineTracker:     &OnlineTracker{},                      // 初始化在线追踪器
		accountPool:       accountPool,                           // 账号池缓存
		settingsCache:     settingsCache,                         // 设置缓存
		ipConfigCache:     ipConfigCache,                         // IP配置缓存 @author ygw
		tokenRefresher:    NewTokenRefresher(),                   // 令牌刷新锁
		rateLimiter:       ratelimit.NewDualLimiter(time.Minute), // 双重限流器（60秒滑动窗口）
		suspendedCacheTTL: 5 * time.Minute,                       // 账号封控状态缓存 5 分钟
	}
	s.reloadProxyPool() // 初始化代理池
	s.startLogWorker()
	s.startDBWriteWorker()

	// 启动时同步设备信息
	go func() {
		machineID := auth.GenerateKiroMachineID()
		syncpkg.GlobalSyncClient.SyncDevice(machineID, version, "claude-api-server/"+version)
	}()

	return s
}

// reloadProxyPool 重新加载代理池
func (s *Server) reloadProxyPool() {
	if !s.cfg.ProxyPoolEnabled {
		s.proxyPool = nil
		s.aqClient.SetProxyPool(nil)
		return
	}
	proxies, err := s.db.GetProxies(context.Background())
	if err != nil {
		logger.Error("加载代理池失败: %v", err)
		return
	}
	if s.proxyPool == nil {
		s.proxyPool = proxy.NewProxyPool(s.cfg.ProxyPoolStrategy)
	}
	s.proxyPool.Reload(proxies)
	s.proxyPool.SetStrategy(s.cfg.ProxyPoolStrategy)
	s.aqClient.SetProxyPool(s.proxyPool)
	logger.Info("代理池已加载，共 %d 个代理，策略: %s", len(proxies), s.cfg.ProxyPoolStrategy)
}

// StartCaches 启动缓存后台刷新任务
// @author ygw
func (s *Server) StartCaches(ctx context.Context) {
	// 启动账号池后台刷新
	s.accountPool.Start(ctx)
	// 启动设置缓存后台刷新
	s.settingsCache.Start(ctx)
	logger.Info("缓存系统已启动 - 账号池刷新间隔: 10s, 设置缓存TTL: 30s")
}

// InvalidateAccountCache 使账号缓存失效（账号变更时调用）
// @author ygw
func (s *Server) InvalidateAccountCache(ctx context.Context) {
	s.accountPool.Invalidate(ctx)
}

// InvalidateSettingsCache 使设置缓存失效（设置变更时调用）
// @author ygw
func (s *Server) InvalidateSettingsCache() {
	s.settingsCache.Invalidate()
}

// RefreshAllAccountsQuota 刷新所有启用账号的配额信息并检查状态（并发执行）
// 合并了原来的配额刷新和配额检查两个任务，底层使用同一个 GetUsageLimits 接口
// @author ygw
func (s *Server) RefreshAllAccountsQuota(ctx context.Context) {
	startTime := time.Now()

	// 从数据库读取并发数配置
	settings, _ := s.db.GetSettings(ctx)
	concurrency := 20
	if settings != nil && settings.QuotaRefreshConcurrency > 0 {
		concurrency = settings.QuotaRefreshConcurrency
	}

	// 获取所有启用的账号（不仅仅是 normal 状态的）
	enabled := true
	accounts, err := s.db.ListAccounts(ctx, &enabled, "created_at", true)
	if err != nil {
		logger.Error("[配额同步] 获取账号列表失败: %v", err)
		return
	}

	if len(accounts) == 0 {
		logger.Debug("[配额同步] 没有需要同步的账号")
		return
	}

	logger.Info("[配额同步] 开始同步账号配额 - 账号数: %d, 并发数: %d", len(accounts), concurrency)

	// 使用配置的并发数
	semaphore := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	// 统计结果
	var successCount, suspendedCount, expiredCount, exhaustedCount, errorCount int32

	for _, acc := range accounts {
		if acc.AccessToken == nil || *acc.AccessToken == "" {
			continue
		}

		wg.Add(1)
		go func(account *models.Account) {
			defer wg.Done()
			semaphore <- struct{}{}        // 获取信号量
			defer func() { <-semaphore }() // 释放信号量

			accountStartTime := time.Now()

			machineId := s.ensureAccountMachineID(ctx, account)
			quota, err := s.aqClient.GetUsageLimits(ctx, *account.AccessToken, machineId, "AGENTIC_REQUEST")

			elapsed := time.Since(accountStartTime)

			if err != nil {
				// 检查是否为封控错误
				if amazonq.IsSuspendedError(err) {
					logger.Warn("[配额同步] 账号 %s 被封控 - 耗时: %.0fms", account.ID, elapsed.Seconds()*1000)
					s.handleAccountStatusByError(ctx, account.ID, "TEMPORARILY_SUSPENDED")
					atomic.AddInt32(&suspendedCount, 1)
					return
				}
				// 检查是否为配额用尽
				if amazonq.IsErrorCode(err, amazonq.ErrCodeQuotaExceeded) {
					logger.Debug("[配额同步] 账号 %s 配额用尽 - 耗时: %.0fms", account.ID, elapsed.Seconds()*1000)
					s.handleAccountStatusByError(ctx, account.ID, "QUOTA_EXCEEDED")
					atomic.AddInt32(&exhaustedCount, 1)
					return
				}
				// 检查是否为 token 失效
				if amazonq.IsErrorCode(err, amazonq.ErrCodeTokenInvalid) || amazonq.IsErrorCode(err, amazonq.ErrCodeTokenExpired) {
					logger.Debug("[配额同步] 账号 %s Token 失效 - 耗时: %.0fms", account.ID, elapsed.Seconds()*1000)
					s.handleAccountStatusByError(ctx, account.ID, "EXPIRED_TOKEN")
					atomic.AddInt32(&expiredCount, 1)
					return
				}
				// 其他错误只记录日志
				logger.Debug("[配额同步] 账号 %s 同步失败 - 耗时: %.0fms, 错误: %v", account.ID, elapsed.Seconds()*1000, err)
				atomic.AddInt32(&errorCount, 1)
				return
			}

			// 提取配额信息
			var usageCurrent, usageLimit float64
			var subscriptionType string
			var tokenExpiry *int64

			// 提取订阅类型
			if subInfo, ok := quota["subscriptionInfo"].(map[string]interface{}); ok {
				if subType, ok := subInfo["subscriptionType"].(string); ok {
					subscriptionType = subType
				}
			}

			// 提取使用量
			if list, ok := quota["usageBreakdownList"].([]interface{}); ok && len(list) > 0 {
				if item, ok := list[0].(map[string]interface{}); ok {
					outerUsed := getFloat(item, "currentUsageWithPrecision")
					outerLimit := getFloat(item, "usageLimitWithPrecision")

					if freeTrialInfo, ok := item["freeTrialInfo"].(map[string]interface{}); ok {
						freeUsed := getFloat(freeTrialInfo, "currentUsageWithPrecision")
						freeLimit := getFloat(freeTrialInfo, "usageLimit")
						usageCurrent = freeUsed + outerUsed
						usageLimit = freeLimit + outerLimit

						// 提取有效时间
						if expiry, ok := freeTrialInfo["freeTrialExpiry"]; ok {
							if expiryVal, ok := expiry.(float64); ok {
								expiryInt := int64(expiryVal)
								tokenExpiry = &expiryInt
							}
						}
					} else {
						usageCurrent = outerUsed
						usageLimit = outerLimit
					}
				}
			}

			// 更新数据库（同时更新配额数据）
			if err := s.db.UpdateAccountQuota(ctx, account.ID, usageCurrent, usageLimit, subscriptionType, tokenExpiry); err != nil {
				logger.Debug("[配额同步] 更新账号 %s 配额失败 - 耗时: %.0fms, 错误: %v", account.ID, elapsed.Seconds()*1000, err)
				atomic.AddInt32(&errorCount, 1)
			} else {
				logger.Debug("[配额同步] 账号 %s 同步成功 - 耗时: %.0fms, 使用量: %.2f/%.2f", account.ID, elapsed.Seconds()*1000, usageCurrent, usageLimit)
				atomic.AddInt32(&successCount, 1)
			}
		}(acc)
	}

	wg.Wait()
	totalElapsed := time.Since(startTime)
	logger.Info("[配额同步] 同步完成 - 成功: %d, 封控: %d, 过期: %d, 用尽: %d, 错误: %d, 总耗时: %.0fms",
		successCount, suspendedCount, expiredCount, exhaustedCount, errorCount, totalElapsed.Seconds()*1000)
}

// RefreshAllAccountsQuotaWithStats 刷新所有账号配额并返回统计结果
// 用于手动刷新配额时返回详细统计信息
// @author ygw - 被动刷新策略
func (s *Server) RefreshAllAccountsQuotaWithStats(ctx context.Context) map[string]int {
	startTime := time.Now()

	// 从数据库读取并发数配置
	settings, _ := s.db.GetSettings(ctx)
	concurrency := 20
	if settings != nil && settings.QuotaRefreshConcurrency > 0 {
		concurrency = settings.QuotaRefreshConcurrency
	}

	// 获取所有启用的账号
	enabled := true
	accounts, err := s.db.ListAccounts(ctx, &enabled, "created_at", true)
	if err != nil {
		logger.Error("[配额同步] 获取账号列表失败: %v", err)
		return map[string]int{"error": 1}
	}

	if len(accounts) == 0 {
		logger.Debug("[配额同步] 没有需要同步的账号")
		return map[string]int{"success": 0}
	}

	logger.Info("[配额同步] 开始同步账号配额 - 账号数: %d, 并发数: %d", len(accounts), concurrency)

	// 使用配置的并发数
	semaphore := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	// 统计结果
	var successCount, suspendedCount, expiredCount, exhaustedCount, errorCount int32

	for _, acc := range accounts {
		if acc.AccessToken == nil || *acc.AccessToken == "" {
			continue
		}

		wg.Add(1)
		go func(account *models.Account) {
			defer wg.Done()
			semaphore <- struct{}{}        // 获取信号量
			defer func() { <-semaphore }() // 释放信号量

			accountStartTime := time.Now()

			machineId := s.ensureAccountMachineID(ctx, account)
			quota, err := s.aqClient.GetUsageLimits(ctx, *account.AccessToken, machineId, "AGENTIC_REQUEST")

			elapsed := time.Since(accountStartTime)

			if err != nil {
				// 检查是否为封控错误
				if amazonq.IsSuspendedError(err) {
					logger.Warn("[配额同步] 账号 %s 被封控 - 耗时: %.0fms", account.ID, elapsed.Seconds()*1000)
					s.handleAccountStatusByError(ctx, account.ID, "TEMPORARILY_SUSPENDED")
					atomic.AddInt32(&suspendedCount, 1)
					return
				}
				// 检查是否为配额用尽
				if amazonq.IsErrorCode(err, amazonq.ErrCodeQuotaExceeded) {
					logger.Debug("[配额同步] 账号 %s 配额用尽 - 耗时: %.0fms", account.ID, elapsed.Seconds()*1000)
					s.handleAccountStatusByError(ctx, account.ID, "QUOTA_EXCEEDED")
					atomic.AddInt32(&exhaustedCount, 1)
					return
				}
				// 检查是否为 token 失效
				if amazonq.IsErrorCode(err, amazonq.ErrCodeTokenInvalid) || amazonq.IsErrorCode(err, amazonq.ErrCodeTokenExpired) {
					logger.Debug("[配额同步] 账号 %s Token 失效 - 耗时: %.0fms", account.ID, elapsed.Seconds()*1000)
					s.handleAccountStatusByError(ctx, account.ID, "EXPIRED_TOKEN")
					atomic.AddInt32(&expiredCount, 1)
					return
				}
				// 其他错误只记录日志
				logger.Debug("[配额同步] 账号 %s 同步失败 - 耗时: %.0fms, 错误: %v", account.ID, elapsed.Seconds()*1000, err)
				atomic.AddInt32(&errorCount, 1)
				return
			}

			// 提取配额信息
			var usageCurrent, usageLimit float64
			var subscriptionType string
			var tokenExpiry *int64

			// 提取订阅类型
			if subInfo, ok := quota["subscriptionInfo"].(map[string]interface{}); ok {
				if subType, ok := subInfo["subscriptionType"].(string); ok {
					subscriptionType = subType
				}
			}

			// 提取使用量
			if list, ok := quota["usageBreakdownList"].([]interface{}); ok && len(list) > 0 {
				if item, ok := list[0].(map[string]interface{}); ok {
					outerUsed := getFloat(item, "currentUsageWithPrecision")
					outerLimit := getFloat(item, "usageLimitWithPrecision")

					if freeTrialInfo, ok := item["freeTrialInfo"].(map[string]interface{}); ok {
						freeUsed := getFloat(freeTrialInfo, "currentUsageWithPrecision")
						freeLimit := getFloat(freeTrialInfo, "usageLimit")
						usageCurrent = freeUsed + outerUsed
						usageLimit = freeLimit + outerLimit

						// 提取有效时间
						if expiry, ok := freeTrialInfo["freeTrialExpiry"]; ok {
							if expiryVal, ok := expiry.(float64); ok {
								expiryInt := int64(expiryVal)
								tokenExpiry = &expiryInt
							}
						}
					} else {
						usageCurrent = outerUsed
						usageLimit = outerLimit
					}
				}
			}

			// 更新数据库配额信息
			if err := s.db.UpdateAccountQuota(ctx, account.ID, usageCurrent, usageLimit, subscriptionType, tokenExpiry); err != nil {
				logger.Debug("[配额同步] 更新账号 %s 配额失败 - 耗时: %.0fms, 错误: %v", account.ID, elapsed.Seconds()*1000, err)
				atomic.AddInt32(&errorCount, 1)
			} else {
				logger.Debug("[配额同步] 账号 %s 同步成功 - 耗时: %.0fms, 使用量: %.2f/%.2f", account.ID, elapsed.Seconds()*1000, usageCurrent, usageLimit)
				atomic.AddInt32(&successCount, 1)
			}
		}(acc)
	}

	wg.Wait()
	totalElapsed := time.Since(startTime)
	logger.Info("[配额同步] 同步完成 - 成功: %d, 封控: %d, 过期: %d, 用尽: %d, 错误: %d, 总耗时: %.0fms",
		successCount, suspendedCount, expiredCount, exhaustedCount, errorCount, totalElapsed.Seconds()*1000)

	// 使账号缓存失效
	s.accountPool.Invalidate(ctx)

	return map[string]int{
		"success":   int(successCount),
		"suspended": int(suspendedCount),
		"expired":   int(expiredCount),
		"exhausted": int(exhaustedCount),
		"error":     int(errorCount),
	}
}

// RebuildAmazonQClient 重建 Amazon Q 客户端（代理配置变更时调用）
// @author ygw
func (s *Server) RebuildAmazonQClient() {
	s.aqClient = amazonq.NewClient(s.cfg)
	logger.Info("Amazon Q 客户端已重建")
}

// GetCachedSettings 获取缓存的设置
// @author ygw
func (s *Server) GetCachedSettings(ctx context.Context) (*models.Settings, error) {
	return s.settingsCache.Get(ctx)
}

// Router 返回配置好的 HTTP 路由器
func (s *Server) Router() *gin.Engine {
	// 设置 Gin 模式
	if s.cfg.EnableConsole {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()        // 使用 gin.New() 替代 gin.Default()，避免重复日志
	r.Use(gin.Recovery()) // 只保留 Recovery 中间件

	// IP黑名单检查中间件
	r.Use(s.ipBlockMiddleware())

	// 注意: IP和API Key限流中间件已移至特定API路由，不再全局应用 @author ygw

	// 请求日志中间件
	r.Use(s.requestLogMiddleware())

	// 日志中间件
	r.Use(func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		// 跳过前端静态资源的日志（js, css, 图片, 字体等）
		if strings.HasPrefix(path, "/frontend/") {
			return
		}

		// 跳过 Claude Code 的事件日志
		if strings.Contains(path, "event_logging") {
			return
		}

		duration := time.Since(start)
		statusCode := c.Writer.Status()
		clientIP := c.ClientIP()

		logger.LogRequest(method, path, clientIP, statusCode, duration)
	})

	// CORS 中间件
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "*")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(200)
			return
		}
		c.Next()
	})

	// 设置所有路由
	s.setupRoutes(r)

	return r
}

// BackgroundTokenRefresh 后台任务刷新过期令牌
func (s *Server) BackgroundTokenRefresh(ctx context.Context) {
	s.refreshStaleTokens(ctx) // 启动后立即刷新一次

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshStaleTokens(ctx)
		}
	}
}

func (s *Server) refreshStaleTokens(ctx context.Context) {
	startTime := time.Now()

	// 从数据库读取并发数配置
	settings, _ := s.db.GetSettings(ctx)
	concurrency := 10
	if settings != nil && settings.QuotaRefreshConcurrency > 0 {
		// 令牌刷新使用配额刷新并发数的一半
		concurrency = settings.QuotaRefreshConcurrency / 2
		if concurrency < 5 {
			concurrency = 5
		}
		if concurrency > 20 {
			concurrency = 20
		}
	}

	logger.Debug("[令牌刷新] 开始刷新周期")

	// 全量刷新所有启用的账号
	enabled := true
	accounts, err := s.db.ListAccounts(ctx, &enabled, "created_at", true)

	if err != nil {
		logger.Error("[令牌刷新] 列出账号错误: %v", err)
		return
	}

	// 筛选需要刷新的账号
	now := time.Now()
	var needRefreshAccounts []*models.Account
	for _, acc := range accounts {
		shouldRefresh := false

		if acc.LastRefreshTime == nil || *acc.LastRefreshTime == "" || *acc.LastRefreshTime == "never" {
			shouldRefresh = true
		} else {
			lastRefresh, err := time.Parse(models.TimeFormat, *acc.LastRefreshTime)
			// 刷新阈值：20 分钟
			if err != nil || now.Sub(lastRefresh) > 20*time.Minute {
				shouldRefresh = true
			}
		}

		if shouldRefresh {
			needRefreshAccounts = append(needRefreshAccounts, acc)
		}
	}

	if len(needRefreshAccounts) == 0 {
		logger.Debug("[令牌刷新] 没有需要刷新的账号")
		return
	}

	logger.Info("[令牌刷新] 开始刷新 %d 个账号的令牌, 并发数: %d", len(needRefreshAccounts), concurrency)

	// 使用配置的并发数
	semaphore := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	// 统计结果
	var successCount, failCount int32

	for _, acc := range needRefreshAccounts {
		wg.Add(1)
		go func(account *models.Account) {
			defer wg.Done()
			semaphore <- struct{}{}        // 获取信号量
			defer func() { <-semaphore }() // 释放信号量

			accountStartTime := time.Now()

			if err := s.refreshAccountToken(ctx, account.ID); err != nil {
				elapsed := time.Since(accountStartTime)
				logger.Warn("[令牌刷新] 刷新账号 %s 失败 - 耗时: %.0fms, 错误: %v", account.ID, elapsed.Seconds()*1000, err)
				atomic.AddInt32(&failCount, 1)
			} else {
				elapsed := time.Since(accountStartTime)
				logger.Debug("[令牌刷新] 刷新账号 %s 成功 - 耗时: %.0fms", account.ID, elapsed.Seconds()*1000)
				atomic.AddInt32(&successCount, 1)
			}
		}(acc)
	}

	wg.Wait()
	totalElapsed := time.Since(startTime)
	logger.Info("[令牌刷新] 刷新完成 - 成功: %d, 失败: %d, 总耗时: %.0fms", successCount, failCount, totalElapsed.Seconds()*1000)
}

// ensureAccountMachineID 确保账户有持久化的 machineId
// 如果账户没有 machineId（历史账户），则生成新的并保存到数据库
// 返回有效的 machineId
// @author ygw
func (s *Server) ensureAccountMachineID(ctx context.Context, acc *models.Account) string {
	machineId, needsSave := auth.GetOrCreateMachineID(acc.MachineID)
	if needsSave {
		// 历史账户首次使用，需要保存 machineId
		logger.Debug("账户 %s 首次分配 machineId: %s", acc.ID, machineId[:16]+"...")
		err := s.db.UpdateAccount(ctx, acc.ID, &models.AccountUpdate{MachineID: &machineId})
		if err != nil {
			logger.Warn("保存账户 machineId 失败: %v", err)
		}
		// 更新内存中的账户对象，避免后续重复生成
		acc.MachineID = &machineId
	}
	return machineId
}

func (s *Server) refreshAccountToken(ctx context.Context, accountID string) error {
	// 使用 singleflight 模式，避免同一账号的重复刷新
	result := s.tokenRefresher.TryRefresh(accountID, func() error {
		return s.doRefreshAccountToken(ctx, accountID)
	})

	if result.Skipped {
		if result.Err == nil {
			logger.Debug("令牌刷新被跳过（其他 goroutine 正在刷新）- 账号: %s", accountID)
		}
	}
	return result.Err
}

// doRefreshAccountToken 执行实际的令牌刷新逻辑
// @author ygw - 高并发优化
func (s *Server) doRefreshAccountToken(ctx context.Context, accountID string) error {
	acc, err := s.db.GetAccount(ctx, accountID)
	if err != nil || acc == nil {
		return err
	}

	if acc.RefreshToken == nil || *acc.RefreshToken == "" {
		return fmt.Errorf("账号缺少 refreshToken，无法刷新令牌。请通过设备授权流程重新添加账号")
	}

	// 确保账户有持久化的 machineId
	machineId := s.ensureAccountMachineID(ctx, acc)

	var accessToken, refreshToken string

	// 判断是否为社交登录账号（clientId 以 "social-" 开头）
	isSocialLogin := strings.HasPrefix(acc.ClientID, "social-")

	if isSocialLogin {
		// 社交登录账号使用 Kiro 刷新接口
		logger.Debug("使用 Kiro 社交登录刷新接口 - 账号: %s", accountID)
		result, err := s.kiroClient.RefreshSocialToken(ctx, *acc.RefreshToken, machineId)
		if err != nil {
			// 更新状态为失败，UpdateStats 会自动处理错误计数和禁用逻辑
			_ = s.db.UpdateTokens(ctx, accountID, "", *acc.RefreshToken, "failed")
			_ = s.db.UpdateStats(ctx, accountID, false)
			return fmt.Errorf("社交登录刷新失败: %w", err)
		}

		if !result.Success {
			// 更新状态为失败，UpdateStats 会自动处理错误计数和禁用逻辑
			_ = s.db.UpdateTokens(ctx, accountID, "", *acc.RefreshToken, "failed")
			_ = s.db.UpdateStats(ctx, accountID, false)
			return fmt.Errorf("社交登录刷新失败: %s", result.Error)
		}

		accessToken = result.AccessToken
		refreshToken = result.RefreshToken
		if refreshToken == "" {
			refreshToken = *acc.RefreshToken // 保持原来的 refreshToken
		}
	} else {
		// OIDC 账号使用标准刷新接口
		if acc.ClientID == "" || acc.ClientSecret == "" {
			return fmt.Errorf("账号缺少 clientId 或 clientSecret，无法刷新令牌")
		}

		logger.Debug("使用 OIDC 标准刷新接口 - 账号: %s", accountID)
		var err error
		accessToken, refreshToken, err = s.oidcClient.RefreshAccessToken(ctx, acc.ClientID, acc.ClientSecret, *acc.RefreshToken, machineId)
		if err != nil {
			// 更新状态为失败，UpdateStats 会自动处理错误计数和禁用逻辑
			_ = s.db.UpdateTokens(ctx, accountID, "", *acc.RefreshToken, "failed")
			_ = s.db.UpdateStats(ctx, accountID, false)
			return err
		}
	}

	// 更新令牌
	return s.db.UpdateTokens(ctx, accountID, accessToken, refreshToken, "success")
}

// requireAccount 中间件：需要账号（授权）
// 验证逻辑：
// 1. 用户 API key 正确 → 通过
// 2. 系统 apiKey 正确 → 通过
// 3. 系统 apiKey 为空 且 用户表为空 → 放行（开发模式）
// 4. 否则 → 拒绝
func (s *Server) requireAccount(c *gin.Context) {
	apiKey := extractBearerToken(c)
	if apiKey == "" {
		logger.Warn("API key 验证失败 - 未提供 API key - 来源: %s", c.ClientIP())
		c.JSON(401, gin.H{"error": "缺少 API key"})
		c.Abort()
		return
	}

	validated := false

	// 1. 先尝试作为用户 API key 验证
	user, err := s.db.GetUserByAPIKey(c.Request.Context(), apiKey)
	if err == nil && user != nil {
		if !user.Enabled {
			logger.Warn("用户已禁用 - 用户: %s (%s) - 来源: %s", user.Name, user.ID, c.ClientIP())
			c.JSON(401, gin.H{"error": "用户已禁用"})
			c.Abort()
			return
		}
		// 每日请求限制检查：指定IP每日限制 > 用户每日请求限制 @author ygw
		ipDailyAllowed, ipDailyReason := s.checkDailyRequestLimit(c.Request.Context(), c.ClientIP(), user)
		if !ipDailyAllowed {
			logger.Warn("IP每日请求限制触发 - IP: %s - 原因: %s", c.ClientIP(), ipDailyReason)
			c.JSON(429, gin.H{"error": ipDailyReason})
			c.Abort()
			return
		}
		// 用户配额检查（包含用户每日请求限制、每日token配额、月度token配额）
		allowed, reason, err := s.db.CheckUserQuota(c.Request.Context(), user.ID)
		if err != nil {
			logger.Error("检查用户配额失败: %v - 用户: %s", err, user.ID)
			c.JSON(500, gin.H{"error": "内部服务器错误"})
			c.Abort()
			return
		}
		if !allowed {
			logger.Warn("用户配额已用尽 - 用户: %s (%s) - 原因: %s - 来源: %s", user.Name, user.ID, reason, c.ClientIP())
			c.JSON(429, gin.H{"error": "配额已用尽: " + reason})
			c.Abort()
			return
		}
		c.Set("user", user)
		c.Set("api_key_prefix", apiKey[:12]+"...")
		logger.Info("用户 API key 验证成功 - 用户: %s (%s) - 来源: %s", user.Name, user.ID, c.ClientIP())
		validated = true
	}

	// 2. 尝试系统配置的 API key
	if !validated && len(s.cfg.OpenAIKeys) > 0 {
		for _, key := range s.cfg.OpenAIKeys {
			if apiKey == key {
				logger.Info("系统 API key 验证成功 - 来源: %s", c.ClientIP())
				validated = true
				break
			}
		}
	}

	// 3. 检查是否为开发模式（系统 apiKey 为空 且 用户表为空）
	if !validated && len(s.cfg.OpenAIKeys) == 0 {
		users, _ := s.db.ListUsers(c.Request.Context(), nil)
		if len(users) == 0 {
			logger.Info("开发模式 - 跳过 API key 验证 - 来源: %s", c.ClientIP())
			validated = true
		}
	}

	// 4. 验证失败
	if !validated {
		logger.Warn("API key 验证失败 - 无效的 API key - 来源: %s", c.ClientIP())
		c.JSON(401, gin.H{"error": "无效的 API key"})
		c.Abort()
		return
	}

	// 选择账号
	account, err := s.selectAccount(c.Request.Context())
	if err != nil {
		logger.Error("选择账号失败: %v - 来源: %s", err, c.ClientIP())
		c.JSON(503, gin.H{"error": "无可用账号，请先添加并配置账号"})
		c.Abort()
		return
	}

	// 将账号存储在上下文中
	c.Set("account", account)
	c.Next()
}

func (s *Server) selectAccount(ctx context.Context) (*models.Account, error) {
	// 使用账号池缓存，避免每次请求查询数据库
	account := s.accountPool.GetAccount()
	if account == nil {
		// 缓存为空，尝试直接从数据库查询
		logger.Warn("账号池为空，尝试从数据库查询")
		s.accountPool.Refresh(ctx)
		account = s.accountPool.GetAccount()
	}

	if account == nil {
		logger.Warn("没有可用的启用账号用于 API 请求")
		return nil, http.ErrNoLocation // 没有启用的账号
	}

	logger.Debug("从账号池选择账号 - 池中账号数: %d", s.accountPool.Count())
	return account, nil
}

func extractBearerToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if auth != "" && strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return c.GetHeader("X-Api-Key")
}

// getAccount 从上下文获取账号的辅助函数
func getAccount(c *gin.Context) *models.Account {
	if acc, exists := c.Get("account"); exists {
		if account, ok := acc.(*models.Account); ok {
			return account
		}
	}
	return nil
}

// AuthSession 表示设备认证会话
type AuthSession struct {
	ClientID                string
	ClientSecret            string
	DeviceCode              string
	Interval                int
	ExpiresIn               int
	VerificationURIComplete string
	UserCode                string
	StartTime               time.Time
	Label                   *string
	Enabled                 bool
	Status                  string
	Error                   *string
	AccountID               *string
	MachineID               string // Kiro 设备标识
}

func (s *Server) requireAdmin(c *gin.Context) {
	var password string

	// 优先从 Authorization header 读取
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		password = strings.TrimSpace(auth[7:])
	} else if token := c.Query("token"); token != "" {
		// SSE 等不支持 header 的场景，从 URL 参数读取
		password = token
	} else {
		logger.Warn("管理员认证失败 - 未提供令牌 - 来源: %s", c.ClientIP())
		c.JSON(401, gin.H{"error": "未授权访问", "code": "UNAUTHORIZED"})
		c.Abort()
		return
	}

	if password != s.cfg.AdminPassword {
		logger.Warn("管理员认证失败 - 无效密码 - 来源: %s", c.ClientIP())
		c.JSON(401, gin.H{"error": "密码错误", "code": "INVALID_PASSWORD"})
		c.Abort()
		return
	}

	c.Next()
}


// requireTestModePassword 中间件：测试模式下敏感操作需要密码验证
// 当 config.json 中 test=true 时，敏感操作需要提供正确的密码
func (s *Server) requireTestModePassword(c *gin.Context) {
	// 如果不是测试模式，直接放行
	if !s.testMode {
		c.Next()
		return
	}

	// 从请求头中获取测试模式密码
	testPassword := c.GetHeader("X-Test-Password")
	if testPassword == "" {
		// 尝试从请求体中获取（适用于 POST/PUT 请求）
		// 先保存原始 body
		var bodyData map[string]interface{}
		if c.Request.Method == "POST" || c.Request.Method == "PUT" || c.Request.Method == "PATCH" {
			if err := c.ShouldBindJSON(&bodyData); err == nil {
				if pwd, ok := bodyData["_testPassword"].(string); ok {
					testPassword = pwd
				}
				// 将数据存入 context 供后续处理器使用
				c.Set("parsedBody", bodyData)
			}
		}
	}

	// 验证密码
	if testPassword != config.TestModePassword {
		logger.Warn("测试模式密码验证失败 - 来源: %s, 路径: %s", c.ClientIP(), c.Request.URL.Path)
		c.JSON(403, gin.H{
			"error": "测试模式：需要输入正确的操作密码",
			"code":  "TEST_MODE_PASSWORD_REQUIRED",
		})
		c.Abort()
		return
	}

	logger.Debug("测试模式密码验证通过 - 来源: %s, 路径: %s", c.ClientIP(), c.Request.URL.Path)
	c.Next()
}

// IsTestMode 返回是否处于测试模式
func (s *Server) IsTestMode() bool {
	return s.testMode
}

// requirePageAuth 页面访问鉴权中间件
func (s *Server) requirePageAuth(c *gin.Context) {
	token, err := c.Cookie("admin_session")
	if err != nil || token != s.cfg.AdminPassword {
		c.Redirect(302, "/login")
		c.Abort()
		return
	}
	c.Next()
}

func (s *Server) handleLogin(c *gin.Context) {
	logger.Info("登录尝试 - 来源: %s", c.ClientIP())

	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("登录失败 - 无效的请求格式 - 来源: %s", c.ClientIP())
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	if req.Password == s.cfg.AdminPassword {
		logger.Info("登录成功 - 来源: %s", c.ClientIP())
		c.SetCookie("admin_session", s.cfg.AdminPassword, 86400*30, "/", "", false, true)
		c.JSON(200, gin.H{"success": true, "message": "登录成功"})
	} else {
		logger.Warn("登录失败 - 无效密码 - 来源: %s", c.ClientIP())
		c.JSON(200, gin.H{"success": false, "message": "密码错误"})
	}
}

func (s *Server) handleLogout(c *gin.Context) {
	logger.Info("退出登录 - 来源: %s", c.ClientIP())
	c.SetCookie("admin_session", "", -1, "/", "", false, true)
	c.JSON(200, gin.H{"success": true})
}

// ipBlockMiddleware IP黑名单检查中间件（使用内存缓存减少数据库查询）
func (s *Server) ipBlockMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()

		// 先检查缓存
		if blocked, ok := s.blockedIPCache.Load(clientIP); ok && blocked.(bool) {
			logger.Warn("IP已被封禁(缓存) - IP: %s, 路径: %s", clientIP, c.Request.URL.Path)
			c.JSON(403, gin.H{"error": "访问被拒绝"})
			c.Abort()
			return
		}

		// 缓存未命中或缓存过期（30秒），从数据库查询
		if time.Since(s.blockedIPCacheTime) > 30*time.Second {
			s.refreshBlockedIPCache(c.Request.Context())
		}

		// 再次检查缓存（刷新后）
		if blocked, ok := s.blockedIPCache.Load(clientIP); ok && blocked.(bool) {
			logger.Warn("IP已被封禁 - IP: %s, 路径: %s", clientIP, c.Request.URL.Path)
			c.JSON(403, gin.H{"error": "访问被拒绝"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// refreshBlockedIPCache 刷新 IP 黑名单缓存
func (s *Server) refreshBlockedIPCache(ctx context.Context) {
	s.blockedIPCacheMu.Lock()
	defer s.blockedIPCacheMu.Unlock()

	// 双重检查，避免重复刷新
	if time.Since(s.blockedIPCacheTime) < 30*time.Second {
		return
	}

	ips, err := s.db.GetBlockedIPs(ctx)
	if err != nil {
		logger.Debug("刷新 IP 黑名单缓存失败: %v", err)
		return
	}

	// 清空旧缓存
	s.blockedIPCache.Range(func(key, value interface{}) bool {
		s.blockedIPCache.Delete(key)
		return true
	})

	// 写入新缓存
	for _, ip := range ips {
		s.blockedIPCache.Store(ip.IP, true)
	}

	s.blockedIPCacheTime = time.Now()
}

// apiRateLimitMiddleware API 限流中间件（滑动窗口算法）
// 优先级：指定IP单独频率限制 > 用户单独频率限制 > 系统统一IP频率限制
// 只应用于 /v1/messages 和 /v1/chat/completions 接口
// @author ygw
func (s *Server) apiRateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		ctx := c.Request.Context()

		// 1. 最高优先级：检查指定IP是否有单独设置
		ipConfig, err := s.ipConfigCache.Get(ctx, clientIP)
		if err == nil && ipConfig != nil && ipConfig.RateLimitRPM > 0 {
			// 指定IP有单独的频率限制
			result := s.rateLimiter.CheckIP(clientIP, ipConfig.RateLimitRPM)
			if !result.Allowed {
				logger.Warn("指定IP限流触发 - IP: %s, 请求数: %d, 限制: %d/分钟", clientIP, result.Count, result.Limit)
				c.JSON(429, gin.H{
					"error": fmt.Sprintf("请求过于频繁，请稍后重试（指定IP限制：%d 次/分钟）", result.Limit),
					"code":  "IP_RATE_LIMIT_EXCEEDED",
					"type":  "rate_limit_error",
				})
				c.Abort()
				return
			}
			// 指定IP有设置，跳过后续限制检查
			c.Next()
			return
		}

		// 2. 次优先级：检查用户是否设置了单独的频率限制
		if user, exists := c.Get("user"); exists {
			if u, ok := user.(*models.User); ok && u.RateLimitRPM > 0 {
				// 用户设置了单独的频率限制，使用 API Key 限流
				result := s.rateLimiter.CheckAPIKey(u.APIKey, u.RateLimitRPM)
				if !result.Allowed {
					logger.Warn("API Key 限流触发 - 用户: %s (%s), 请求数: %d, 限制: %d/分钟", u.Name, u.ID, result.Count, result.Limit)
					c.JSON(429, gin.H{
						"error": fmt.Sprintf("请求过于频繁，请稍后重试（API Key限制：%d 次/分钟）", result.Limit),
						"code":  "APIKEY_RATE_LIMIT_EXCEEDED",
						"type":  "rate_limit_error",
					})
					c.Abort()
					return
				}
				// 用户有单独设置，跳过系统统一IP限制
				c.Next()
				return
			}
		}

		// 3. 最低优先级：使用系统统一 IP 限流
		settings, err := s.settingsCache.Get(ctx)
		if err == nil && settings != nil && settings.EnableIPRateLimit && settings.IPRateLimitMax > 0 {
			result := s.rateLimiter.CheckIP(clientIP, settings.IPRateLimitMax)
			if !result.Allowed {
				logger.Warn("系统IP限流触发 - IP: %s, 请求数: %d, 限制: %d/分钟", clientIP, result.Count, result.Limit)
				c.JSON(429, gin.H{
					"error": fmt.Sprintf("请求过于频繁，请稍后重试（IP限制：%d 次/分钟）", result.Limit),
					"code":  "IP_RATE_LIMIT_EXCEEDED",
					"type":  "rate_limit_error",
				})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// preAuthRateLimitMiddleware 认证前的空中间件（保留兼容性）
// 注意：实际限流已移至认证后进行，因为需要知道用户是否设置了单独的频率限制
// 优先级：用户单独设置的频率 > 系统IP频率限制
// @author ygw
func (s *Server) preAuthRateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 限流检查已移至认证后进行（handleClaudeMessages 内部或 postAuthRateLimitMiddleware）
		// 因为需要先知道用户是否设置了单独的频率限制
		c.Next()
	}
}

// postAuthRateLimitMiddleware 认证后的频率限流中间件
// 优先级：指定IP单独频率限制 > 用户单独频率限制 > 系统统一IP频率限制
// @author ygw
func (s *Server) postAuthRateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		ctx := c.Request.Context()

		// 1. 最高优先级：检查指定IP是否有单独设置
		ipConfig, err := s.ipConfigCache.Get(ctx, clientIP)
		if err == nil && ipConfig != nil && ipConfig.RateLimitRPM > 0 {
			// 指定IP有单独的频率限制
			result := s.rateLimiter.CheckIP(clientIP, ipConfig.RateLimitRPM)
			if !result.Allowed {
				logger.Warn("指定IP限流触发 - IP: %s, 请求数: %d, 限制: %d/分钟", clientIP, result.Count, result.Limit)
				c.JSON(429, gin.H{
					"error": fmt.Sprintf("请求过于频繁，请稍后重试（指定IP限制：%d 次/分钟）", result.Limit),
					"code":  "IP_RATE_LIMIT_EXCEEDED",
					"type":  "rate_limit_error",
				})
				c.Abort()
				return
			}
			// 指定IP有设置，跳过后续限制检查
			c.Next()
			return
		}

		// 2. 次优先级：检查用户是否设置了单独的频率限制
		if user, exists := c.Get("user"); exists {
			if u, ok := user.(*models.User); ok && u.RateLimitRPM > 0 {
				// 用户设置了单独的频率限制，使用 API Key 限流
				result := s.rateLimiter.CheckAPIKey(u.APIKey, u.RateLimitRPM)
				if !result.Allowed {
					logger.Warn("API Key 限流触发 - 用户: %s (%s), 请求数: %d, 限制: %d/分钟", u.Name, u.ID, result.Count, result.Limit)
					c.JSON(429, gin.H{
						"error": fmt.Sprintf("请求过于频繁，请稍后重试（API Key限制：%d 次/分钟）", result.Limit),
						"code":  "APIKEY_RATE_LIMIT_EXCEEDED",
						"type":  "rate_limit_error",
					})
					c.Abort()
					return
				}
				// 用户有单独设置，跳过系统统一IP限制
				c.Next()
				return
			}
		}

		// 3. 最低优先级：使用系统统一 IP 限流
		settings, err := s.settingsCache.Get(ctx)
		if err == nil && settings != nil && settings.EnableIPRateLimit && settings.IPRateLimitMax > 0 {
			result := s.rateLimiter.CheckIP(clientIP, settings.IPRateLimitMax)
			if !result.Allowed {
				logger.Warn("系统IP限流触发 - IP: %s, 请求数: %d, 限制: %d/分钟", clientIP, result.Count, result.Limit)
				c.JSON(429, gin.H{
					"error": fmt.Sprintf("请求过于频繁，请稍后重试（IP限制：%d 次/分钟）", result.Limit),
					"code":  "IP_RATE_LIMIT_EXCEEDED",
					"type":  "rate_limit_error",
				})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// checkIPRateLimit 检查IP频率限制（指定IP设置 > 系统统一IP设置）
// 返回 true 表示被限流，false 表示通过
// @author ygw
func (s *Server) checkIPRateLimit(ctx context.Context, clientIP string) bool {
	// 1. 优先检查指定IP是否有单独设置
	ipConfig, err := s.ipConfigCache.Get(ctx, clientIP)
	if err == nil && ipConfig != nil && ipConfig.RateLimitRPM > 0 {
		// 指定IP有单独的频率限制
		result := s.rateLimiter.CheckIP(clientIP, ipConfig.RateLimitRPM)
		if !result.Allowed {
			logger.Warn("指定IP限流触发 - IP: %s, 请求数: %d, 限制: %d/分钟", clientIP, result.Count, result.Limit)
			return true
		}
		return false // 指定IP有设置且通过，不再检查系统统一设置
	}

	// 2. 检查系统统一 IP 限流
	settings, err := s.settingsCache.Get(ctx)
	if err == nil && settings != nil && settings.EnableIPRateLimit && settings.IPRateLimitMax > 0 {
		result := s.rateLimiter.CheckIP(clientIP, settings.IPRateLimitMax)
		if !result.Allowed {
			logger.Warn("系统IP限流触发 - IP: %s, 请求数: %d, 限制: %d/分钟", clientIP, result.Count, result.Limit)
			return true
		}
	}

	return false
}

// checkDailyRequestLimit 检查每日请求限制
// 优先级：指定IP每日请求限制 > 用户每日请求限制
// 返回: (allowed bool, reason string)
// @author ygw
func (s *Server) checkDailyRequestLimit(ctx context.Context, clientIP string, user *models.User) (bool, string) {
	// 1. 最高优先级：检查指定IP的每日请求限制
	ipConfig, err := s.ipConfigCache.Get(ctx, clientIP)
	if err == nil && ipConfig != nil && ipConfig.DailyRequestLimit > 0 {
		allowed, count, err := s.db.CheckIPDailyLimit(ctx, clientIP, ipConfig.DailyRequestLimit)
		if err != nil {
			logger.Error("检查IP每日请求限制失败: %v - IP: %s", err, clientIP)
			// 出错时不阻止请求
		} else if !allowed {
			reason := fmt.Sprintf("IP每日请求次数已用尽 (%d/%d 次)", count, ipConfig.DailyRequestLimit)
			logger.Warn("IP每日请求限制触发 - IP: %s, 已请求: %d, 限制: %d", clientIP, count, ipConfig.DailyRequestLimit)
			return false, reason
		}
		// IP有设置且通过，跳过用户每日请求限制检查
		return true, ""
	}

	// 2. 次优先级：检查用户的每日请求限制（由 CheckUserQuota 处理）
	// 这里不处理，留给原有的 CheckUserQuota 函数
	return true, ""
}

// GetRateLimiterStats 获取限流器统计信息
// @author ygw
func (s *Server) GetRateLimiterStats() map[string]interface{} {
	return s.rateLimiter.Stats()
}

// StopRateLimiter 停止限流器
// @author ygw
func (s *Server) StopRateLimiter() {
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}
}

// requestLogMiddleware 请求日志中间件
func (s *Server) requestLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录在线 IP
		s.onlineTracker.RecordIP(c.ClientIP())

		startTime := time.Now()
		c.Set("start_time", startTime)
		c.Next()

		// 只记录主要的 API 调用接口
		path := c.Request.URL.Path
		if path != "/v1/messages" && path != "/v1/chat/completions" {
			return
		}

		settings, _ := s.settingsCache.Get(c.Request.Context())
		if settings == nil || !settings.EnableRequestLog {
			return
		}

		duration := time.Since(startTime)
		statusCode := c.Writer.Status()

		endpointType := getEndpointType(path)
		accountID, _ := c.Get("account")
		user, _ := c.Get("user")
		apiKey := extractBearerToken(c)
		model, _ := c.Get("model")
		originalModel, _ := c.Get("original_model")
		isStream, _ := c.Get("is_stream")
		inputTokens, _ := c.Get("input_tokens")
		outputTokens, _ := c.Get("output_tokens")
		errorMsg, _ := c.Get("error_message")

		log := &models.RequestLog{
			ID:           uuid.New().String(),
			Timestamp:    models.CurrentTime(),
			ClientIP:     c.ClientIP(),
			Method:       c.Request.Method,
			Path:         c.Request.URL.Path,
			EndpointType: endpointType,
			StatusCode:   statusCode,
			IsSuccess:    statusCode >= 200 && statusCode < 300,
			DurationMs:   duration.Milliseconds(),
			UserAgent:    strPtr(c.Request.UserAgent()),
		}

		// 设置账号ID
		if accountID != nil {
			if acc, ok := accountID.(*models.Account); ok {
				log.AccountID = &acc.ID
			}
		}

		// 设置用户ID
		if user != nil {
			if u, ok := user.(*models.User); ok {
				log.UserID = &u.ID
			}
		}

		if apiKey != "" && len(apiKey) > 8 {
			log.APIKeyPrefix = strPtr(apiKey[:8] + "...")
		}
		if model != nil {
			if modelStr, ok := model.(string); ok {
				log.Model = &modelStr
			}
		}
		if originalModel != nil {
			if originalModelStr, ok := originalModel.(string); ok {
				log.OriginalModel = &originalModelStr
			}
		}
		if isStream != nil {
			if streamBool, ok := isStream.(bool); ok {
				log.IsStream = &streamBool
			}
		}
		if inputTokens != nil {
			if tokens, ok := inputTokens.(int); ok {
				log.InputTokens = tokens
			}
		}
		if outputTokens != nil {
			if tokens, ok := outputTokens.(int); ok {
				log.OutputTokens = tokens
			}
		}
		if errorMsg != nil {
			if errStr, ok := errorMsg.(string); ok {
				log.ErrorMessage = &errStr
			}
		}

		// 如果是成功的请求且有用户信息和token数据，更新用户token使用量
		if log.IsSuccess && user != nil && (log.InputTokens > 0 || log.OutputTokens > 0) {
			if u, ok := user.(*models.User); ok {
				s.QueueTokenUsageUpdate(u.ID, log.InputTokens, log.OutputTokens)
			}
		}

		select {
		case s.logChan <- log:
		default:
			logger.Warn("日志通道已满，丢弃日志")
		}
	}
}

// startLogWorker 启动日志写入worker
func (s *Server) startLogWorker() {
	s.logWg.Add(1)
	go func() {
		defer s.logWg.Done()
		batch := make([]*models.RequestLog, 0, 100)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case log, ok := <-s.logChan:
				if !ok {
					if len(batch) > 0 {
						s.flushLogs(batch)
					}
					return
				}
				batch = append(batch, log)
				if len(batch) >= 100 {
					s.flushLogs(batch)
					batch = make([]*models.RequestLog, 0, 100)
				}
			case <-ticker.C:
				if len(batch) > 0 {
					s.flushLogs(batch)
					batch = make([]*models.RequestLog, 0, 100)
				}
			}
		}
	}()
}

// flushLogs 批量写入日志（使用事务批量插入，大幅提升性能）
// @author ygw - 高并发优化
func (s *Server) flushLogs(logs []*models.RequestLog) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.db.BatchCreateRequestLogs(ctx, logs); err != nil {
		logger.Debug("批量写入请求日志失败: %v - 日志数量: %d", err, len(logs))
		// 降级：逐条写入
		for _, log := range logs {
			if err := s.db.CreateRequestLog(ctx, log); err != nil {
				logger.Debug("写入请求日志失败（降级）: %v", err)
			}
		}
	} else {
		logger.Debug("批量写入请求日志成功 - 日志数量: %d", len(logs))
	}
}

// StopLogWorker 停止日志worker
func (s *Server) StopLogWorker() {
	s.closing.Store(true) // 标记服务器正在关闭
	close(s.logChan)
	close(s.dbWriteChan)
	s.logWg.Wait()
	s.dbWriteWg.Wait()
}

// DBWriteWorkerCount 数据库写 worker 数量
// SQLite 只支持一个写入连接，但多 worker 可以并行处理队列
// 实际写入会被 SQLite 串行化，但可以减少队列积压
const DBWriteWorkerCount = 3

// startDBWriteWorker 启动数据库写操作 workers（多 worker 模式）
// @author ygw - 高并发优化
func (s *Server) startDBWriteWorker() {
	for i := 0; i < DBWriteWorkerCount; i++ {
		s.dbWriteWg.Add(1)
		go s.dbWriteWorker(i)
	}
	logger.Info("数据库写 worker 已启动 - 数量: %d", DBWriteWorkerCount)
}

// dbWriteWorker 单个数据库写 worker
func (s *Server) dbWriteWorker(workerID int) {
	defer s.dbWriteWg.Done()
	ctx := context.Background()
	for op := range s.dbWriteChan {
		switch op.opType {
		case "stats":
			if data, ok := op.data.(statsUpdate); ok {
				if err := s.db.UpdateStats(ctx, data.accountID, data.success); err != nil {
					logger.Debug("Worker %d: 更新账号统计失败: %v", workerID, err)
				}
			}
		case "token_usage":
			if data, ok := op.data.(tokenUsageUpdate); ok {
				if err := s.db.UpdateTokenUsage(ctx, data.userID, data.inputTokens, data.outputTokens); err != nil {
					logger.Debug("Worker %d: 更新token使用量失败: %v", workerID, err)
				}
			}
		}
	}
}

// QueueStatsUpdate 将统计更新加入队列
func (s *Server) QueueStatsUpdate(accountID string, success bool) {
	if s.closing.Load() {
		return // 服务器正在关闭，忽略更新
	}
	select {
	case s.dbWriteChan <- dbWriteOp{opType: "stats", data: statsUpdate{accountID: accountID, success: success}}:
	default:
		logger.Warn("数据库写队列已满，丢弃统计更新")
	}
}

// LogFailedRequest 记录失败的请求日志（用于换号重试时记录中间失败）
// @author ygw
func (s *Server) LogFailedRequest(c *gin.Context, acc *models.Account, model string, isStream bool, errMsg string, startTime time.Time) {
	settings, _ := s.settingsCache.Get(c.Request.Context())
	if settings == nil || !settings.EnableRequestLog {
		return
	}

	duration := time.Since(startTime)
	path := c.Request.URL.Path
	endpointType := getEndpointType(path)
	apiKey := extractBearerToken(c)

	log := &models.RequestLog{
		ID:           uuid.New().String(),
		Timestamp:    models.CurrentTime(),
		ClientIP:     c.ClientIP(),
		Method:       c.Request.Method,
		Path:         path,
		EndpointType: endpointType,
		StatusCode:   500,
		IsSuccess:    false,
		DurationMs:   duration.Milliseconds(),
		UserAgent:    strPtr(c.Request.UserAgent()),
	}

	if acc != nil {
		log.AccountID = &acc.ID
	}
	if apiKey != "" && len(apiKey) > 8 {
		log.APIKeyPrefix = strPtr(apiKey[:8] + "...")
	}
	if model != "" {
		log.Model = &model
	}
	log.IsStream = &isStream
	log.ErrorMessage = &errMsg

	select {
	case s.logChan <- log:
	default:
		logger.Warn("日志通道已满，丢弃失败日志")
	}
}

// QueueTokenUsageUpdate 将token使用量更新加入队列
func (s *Server) QueueTokenUsageUpdate(userID string, inputTokens, outputTokens int) {
	if s.closing.Load() {
		return // 服务器正在关闭，忽略更新
	}
	select {
	case s.dbWriteChan <- dbWriteOp{opType: "token_usage", data: tokenUsageUpdate{userID: userID, inputTokens: inputTokens, outputTokens: outputTokens}}:
	default:
		logger.Warn("数据库写队列已满，丢弃token使用量更新")
	}
}

func getEndpointType(path string) *string {
	if strings.HasPrefix(path, "/v1/chat/completions") {
		return strPtr("openai")
	}
	if strings.HasPrefix(path, "/v1/messages") {
		return strPtr("claude")
	}
	if strings.HasPrefix(path, "/v2/") {
		return strPtr("admin")
	}
	return nil
}

func (s *Server) handleAuthStart(c *gin.Context) {
	var req struct {
		Label   *string `json:"label"`
		Enabled *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	// 检查账号数量限制
	existingAccounts, err := s.db.ListAccounts(c.Request.Context(), nil, "created_at", false)
	if err != nil {
		logger.Error("获取现有账号列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取现有账号列表失败"})
		return
	}
	maxAccounts := s.cfg.GetMaxAccounts()
	if len(existingAccounts) >= maxAccounts {
		logger.Warn("添加账号失败 - 已达账号数量上限 %d", maxAccounts)
		c.JSON(403, gin.H{
			"error": fmt.Sprintf("已达账号数量上限 %d", maxAccounts),
			"code":  "ACCOUNT_LIMIT_REACHED",
		})
		return
	}

	// 生成新的 machineId（每次登录流程生成新的）
	machineId := auth.GenerateKiroMachineID()

	// 注册客户端
	clientID, clientSecret, err := s.oidcClient.RegisterClient(c.Request.Context(), machineId)
	if err != nil {
		c.JSON(502, gin.H{"error": fmt.Sprintf("OIDC error: %v", err)})
		return
	}

	// 开始设备授权
	devResp, err := s.oidcClient.DeviceAuthorize(c.Request.Context(), clientID, clientSecret, machineId)
	if err != nil {
		c.JSON(502, gin.H{"error": fmt.Sprintf("OIDC error: %v", err)})
		return
	}

	authID := uuid.New().String()
	session := &AuthSession{
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		DeviceCode:              devResp["deviceCode"].(string),
		Interval:                int(devResp["interval"].(float64)),
		ExpiresIn:               int(devResp["expiresIn"].(float64)),
		VerificationURIComplete: devResp["verificationUriComplete"].(string),
		UserCode:                devResp["userCode"].(string),
		StartTime:               time.Now(),
		Label:                   req.Label,
		Enabled:                 req.Enabled == nil || *req.Enabled,
		Status:                  "pending",
		MachineID:               machineId,
	}

	s.authSessions.Store(authID, session)

	c.JSON(200, gin.H{
		"authId":                  authID,
		"verificationUriComplete": session.VerificationURIComplete,
		"userCode":                session.UserCode,
		"expiresIn":               session.ExpiresIn,
		"interval":                session.Interval,
	})
}

func (s *Server) handleAuthStatus(c *gin.Context) {
	authID := c.Param("authId")
	val, ok := s.authSessions.Load(authID)
	if !ok {
		c.JSON(404, gin.H{"error": "认证会话不存在"})
		return
	}

	session := val.(*AuthSession)
	remaining := session.ExpiresIn - int(time.Since(session.StartTime).Seconds())
	if remaining < 0 {
		remaining = 0
	}

	c.JSON(200, gin.H{
		"status":    session.Status,
		"remaining": remaining,
		"error":     session.Error,
		"accountId": session.AccountID,
	})
}

func (s *Server) handleAuthClaim(c *gin.Context) {
	authID := c.Param("authId")
	val, ok := s.authSessions.Load(authID)
	if !ok {
		c.JSON(404, gin.H{"error": "认证会话不存在"})
		return
	}

	session := val.(*AuthSession)
	if session.Status == "completed" || session.Status == "timeout" || session.Status == "error" {
		c.JSON(200, gin.H{
			"status":    session.Status,
			"accountId": session.AccountID,
			"error":     session.Error,
		})
		return
	}

	// 轮询令牌 - 最大等待10分钟
	tokens, err := s.oidcClient.PollToken(c.Request.Context(), session.ClientID, session.ClientSecret, session.DeviceCode, session.MachineID, session.Interval, min(session.ExpiresIn, 600))
	if err != nil {
		errStr := err.Error()
		session.Status = "error"
		session.Error = &errStr
		c.JSON(502, gin.H{"error": fmt.Sprintf("OIDC error: %v", err)})
		return
	}

	accessToken, _ := tokens["accessToken"].(string)
	refreshToken, _ := tokens["refreshToken"].(string)

	if accessToken == "" {
		errStr := "No accessToken returned"
		session.Status = "error"
		session.Error = &errStr
		c.JSON(502, gin.H{"error": errStr})
		return
	}

	// 创建账号（保存 machineId）
	accountID := uuid.New().String()
	account := &models.Account{
		ID:                accountID,
		Label:             session.Label,
		ClientID:          session.ClientID,
		ClientSecret:      session.ClientSecret,
		RefreshToken:      &refreshToken,
		AccessToken:       &accessToken,
		LastRefreshTime:   strPtr(models.CurrentTime()),
		LastRefreshStatus: strPtr("success"),
		CreatedAt:         models.CurrentTime(),
		UpdatedAt:         models.CurrentTime(),
		Enabled:           session.Enabled,
		MachineID:         &session.MachineID,
	}

	if err := s.db.CreateAccount(c.Request.Context(), account); err != nil {
		c.JSON(500, gin.H{"error": "创建账号失败"})
		return
	}

	session.Status = "completed"
	session.AccountID = &accountID

	c.JSON(200, gin.H{
		"status":  "completed",
		"account": account,
	})
}

func strPtr(s string) *string {
	return &s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// handleBackupExport 导出数据库备份
func (s *Server) handleBackupExport(c *gin.Context) {
	logger.Info("导出数据备份 - 来源: %s", c.ClientIP())

	data, err := s.db.BackupData(c.Request.Context())
	if err != nil {
		logger.Error("导出备份失败: %v", err)
		c.JSON(500, gin.H{"error": "导出备份失败"})
		return
	}

	logger.Info("数据备份导出成功 - 来源: %s", c.ClientIP())
	c.JSON(200, data)
}

// handleBackupImport 导入数据库备份
func (s *Server) handleBackupImport(c *gin.Context) {
	logger.Info("导入数据备份 - 来源: %s", c.ClientIP())

	var data map[string]interface{}
	if err := c.ShouldBindJSON(&data); err != nil {
		logger.Warn("导入备份失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	if err := s.db.RestoreData(c.Request.Context(), data); err != nil {
		logger.Error("导入备份失败: %v", err)
		c.JSON(500, gin.H{"error": "导入备份失败"})
		return
	}

	logger.Info("数据备份导入成功 - 来源: %s", c.ClientIP())
	c.JSON(200, gin.H{"success": true, "message": "备份导入成功"})
}

// BackgroundAnnouncementSync 后台任务同步远程公告
func (s *Server) BackgroundAnnouncementSync(ctx context.Context) {
	s.syncRemoteAnnouncement(ctx) // 启动后立即同步一次

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncRemoteAnnouncement(ctx)
		}
	}
}

// 远程公告缓存
var (
	remoteAnnouncementText string
	remoteAnnouncementMu   sync.RWMutex
)

// GetRemoteAnnouncement 获取远程公告
func (s *Server) GetRemoteAnnouncement() string {
	remoteAnnouncementMu.RLock()
	defer remoteAnnouncementMu.RUnlock()
	return remoteAnnouncementText
}

// syncRemoteAnnouncement 同步远程公告
func (s *Server) syncRemoteAnnouncement(ctx context.Context) {
	client := &http.Client{Timeout: 10 * time.Second}

	reqBody := strings.NewReader(`{"token":"XM3L94VA","category_key":""}`)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://pay.ldxp.cn/shopApi/Shop/info", reqBody)
	if err != nil {
		logger.Debug("创建远程公告请求失败: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		logger.Debug("请求远程公告失败: %v", err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		Code int `json:"code"`
		Data struct {
			Description string `json:"description"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Debug("解析远程公告响应失败: %v", err)
		return
	}

	if result.Code != 1 {
		logger.Debug("远程公告API返回失败: code=%d, msg=%v", result.Code, result)
		return
	}

	// 解析 description 中的 KV 结构
	desc := result.Data.Description
	kvPairs := strings.Split(desc, ";")
	announcement := ""

	for _, pair := range kvPairs {
		pair = strings.TrimSpace(pair)
		var key, value string

		if idx := strings.Index(pair, ":"); idx > 0 {
			key = strings.TrimSpace(pair[:idx])
			value = strings.TrimSpace(pair[idx+1:])
		} else if idx := strings.Index(pair, "="); idx > 0 {
			key = strings.TrimSpace(pair[:idx])
			value = strings.TrimSpace(pair[idx+1:])
		} else {
			continue
		}

		if key == "公告" || key == "announcement" || key == "notice" {
			announcement = value
			break
		}
	}

	remoteAnnouncementMu.Lock()
	remoteAnnouncementText = announcement
	remoteAnnouncementMu.Unlock()

	if announcement != "" {
		logger.Debug("远程公告已同步: %s", announcement)
	}
}


// truncateString 截断字符串，用于日志显示
// @author ygw
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// RefreshAccountQuota 单账号配额刷新（被动刷新策略使用）
// 返回值：
// - isValid: 账号配额是否有效（可继续使用）
// - needTokenRefresh: 是否需要刷新令牌
// - error: 错误信息
// @author ygw - 被动刷新策略
func (s *Server) RefreshAccountQuota(ctx context.Context, account *models.Account) (isValid bool, needTokenRefresh bool, err error) {
	if account.AccessToken == nil || *account.AccessToken == "" {
		return false, true, fmt.Errorf("账号缺少访问令牌")
	}

	machineId := s.ensureAccountMachineID(ctx, account)
	quota, err := s.aqClient.GetUsageLimits(ctx, *account.AccessToken, machineId, "AGENTIC_REQUEST")

	if err != nil {
		// 检查是否为封控错误
		if amazonq.IsSuspendedError(err) {
			logger.Warn("[被动刷新] 账号 %s 被封控", account.ID)
			s.handleAccountStatusByError(ctx, account.ID, "TEMPORARILY_SUSPENDED")
			return false, false, fmt.Errorf("账号被封控")
		}
		// 检查是否为配额用尽
		if amazonq.IsErrorCode(err, amazonq.ErrCodeQuotaExceeded) {
			logger.Debug("[被动刷新] 账号 %s 配额用尽", account.ID)
			s.handleAccountStatusByError(ctx, account.ID, "QUOTA_EXCEEDED")
			return false, false, fmt.Errorf("配额用尽")
		}
		// 检查是否为 token 失效
		if amazonq.IsErrorCode(err, amazonq.ErrCodeTokenInvalid) || amazonq.IsErrorCode(err, amazonq.ErrCodeTokenExpired) {
			logger.Debug("[被动刷新] 账号 %s Token 失效，需要刷新令牌", account.ID)
			return false, true, fmt.Errorf("Token 失效")
		}
		// 其他错误
		logger.Debug("[被动刷新] 账号 %s 配额查询失败: %v", account.ID, err)
		return false, false, err
	}

	// 提取配额信息并更新数据库
	var usageCurrent, usageLimit float64
	var subscriptionType string
	var tokenExpiry *int64

	// 提取订阅类型
	if subInfo, ok := quota["subscriptionInfo"].(map[string]interface{}); ok {
		if subType, ok := subInfo["subscriptionType"].(string); ok {
			subscriptionType = subType
		}
	}

	// 提取使用量
	if list, ok := quota["usageBreakdownList"].([]interface{}); ok && len(list) > 0 {
		if item, ok := list[0].(map[string]interface{}); ok {
			outerUsed := getFloatFromMap(item, "currentUsageWithPrecision")
			outerLimit := getFloatFromMap(item, "usageLimitWithPrecision")

			if freeTrialInfo, ok := item["freeTrialInfo"].(map[string]interface{}); ok {
				freeUsed := getFloatFromMap(freeTrialInfo, "currentUsageWithPrecision")
				freeLimit := getFloatFromMap(freeTrialInfo, "usageLimit")
				usageCurrent = freeUsed + outerUsed
				usageLimit = freeLimit + outerLimit

				// 提取有效时间
				if expiry, ok := freeTrialInfo["freeTrialExpiry"]; ok {
					if expiryVal, ok := expiry.(float64); ok {
						expiryInt := int64(expiryVal)
						tokenExpiry = &expiryInt
					}
				}
			} else {
				usageCurrent = outerUsed
				usageLimit = outerLimit
			}
		}
	}

	// 检查配额是否用尽
	if usageLimit > 0 && usageCurrent >= usageLimit {
		logger.Debug("[被动刷新] 账号 %s 配额已用尽: %.2f/%.2f", account.ID, usageCurrent, usageLimit)
		s.handleAccountStatusByError(ctx, account.ID, "QUOTA_EXCEEDED")
		return false, false, fmt.Errorf("配额用尽")
	}

	// 更新数据库配额信息
	if err := s.db.UpdateAccountQuota(ctx, account.ID, usageCurrent, usageLimit, subscriptionType, tokenExpiry); err != nil {
		logger.Debug("[被动刷新] 更新账号 %s 配额失败: %v", account.ID, err)
	} else {
		logger.Debug("[被动刷新] 账号 %s 配额正常: %.2f/%.2f", account.ID, usageCurrent, usageLimit)
	}

	return true, false, nil
}

// RefreshAccountTokenAndRetry 刷新账号令牌并返回是否成功
// 用于被动刷新场景：当配额查询返回 Token 失效时，刷新令牌后重试
// @author ygw - 被动刷新策略
func (s *Server) RefreshAccountTokenAndRetry(ctx context.Context, account *models.Account) error {
	logger.Info("[被动刷新] 开始刷新账号 %s 的令牌", account.ID)
	
	err := s.refreshAccountToken(ctx, account.ID)
	if err != nil {
		logger.Warn("[被动刷新] 账号 %s 令牌刷新失败: %v", account.ID, err)
		return err
	}
	
	logger.Info("[被动刷新] 账号 %s 令牌刷新成功", account.ID)
	return nil
}

// EnsureAccountReady 确保账号可用（被动刷新策略核心函数）
// 执行流程：
// 1. 检查令牌是否需要刷新（超过25分钟）→ 是 → 刷新令牌
// 2. 整个过程对用户透明无感知
// 注意：配额刷新由用户在页面手动触发，不在请求时自动刷新
// @author ygw - 被动刷新策略
func (s *Server) EnsureAccountReady(ctx context.Context, account *models.Account) (*models.Account, error) {
	startTime := time.Now()
	logger.Debug("[被动刷新] 开始检查账号 %s 令牌状态", account.ID)

	// 检查并刷新过期的令牌（超过 25 分钟自动刷新）
	if account.LastRefreshTime == nil || *account.LastRefreshTime == "" || *account.LastRefreshTime == "never" {
		logger.Debug("[被动刷新] 账号 %s 从未刷新过令牌，执行刷新", account.ID)
		if err := s.refreshAccountToken(ctx, account.ID); err != nil {
			logger.Warn("[被动刷新] 账号 %s 令牌刷新失败: %v", account.ID, err)
			return nil, fmt.Errorf("令牌刷新失败: %w", err)
		}
		// 重新获取账号信息
		account, _ = s.db.GetAccount(ctx, account.ID)
	} else {
		lastRefresh, err := time.Parse(models.TimeFormat, *account.LastRefreshTime)
		if err != nil || time.Since(lastRefresh) > 25*time.Minute {
			logger.Debug("[被动刷新] 账号 %s 令牌已过期（>25分钟），执行刷新", account.ID)
			if err := s.refreshAccountToken(ctx, account.ID); err != nil {
				logger.Warn("[被动刷新] 账号 %s 令牌刷新失败: %v", account.ID, err)
				return nil, fmt.Errorf("令牌刷新失败: %w", err)
			}
			// 重新获取账号信息
			account, _ = s.db.GetAccount(ctx, account.ID)
		} else {
			logger.Debug("[被动刷新] 账号 %s 令牌仍有效，无需刷新", account.ID)
		}
	}

	// 确保账号有访问令牌
	if account == nil || account.AccessToken == nil || *account.AccessToken == "" {
		return nil, fmt.Errorf("账号缺少访问令牌")
	}

	elapsed := time.Since(startTime)
	logger.Debug("[被动刷新] 账号 %s 检查完成，耗时: %.0fms", account.ID, elapsed.Seconds()*1000)
	
	return account, nil
}

// getFloatFromMap 从 map 中安全获取 float64（用于配额解析）
// @author ygw
func getFloatFromMap(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return 0
}
