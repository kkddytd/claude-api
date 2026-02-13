package api

import (
	"io/fs"
	"net/http"
	"claude-api/frontend"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// noCacheMiddleware 禁用浏览器缓存，确保每次都获取最新的静态文件（仅用于 HTML）
func noCacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Next()
	}
}

// staticCacheMiddleware 根据文件类型设置不同的缓存策略
// - 带哈希的 JS/CSS: 1年缓存（内容变化时哈希自动变化）
// - 普通 JS/CSS: 1小时缓存
// - 图片/字体: 7-30天缓存
// - vendor 第三方库: 30天缓存
// @author ygw
var hashedFilePattern = regexp.MustCompile(`\.[a-f0-9]{8}\.(js|css)$`)

func staticCacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		switch {
		case hashedFilePattern.MatchString(path):
			// 带哈希的文件，长期缓存 1 年，immutable 表示内容不会变
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		case strings.Contains(path, "/vendor/"):
			// 第三方库，长期缓存 30 天
			c.Header("Cache-Control", "public, max-age=2592000")
		case strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css"):
			// 普通 JS/CSS 文件，缓存 1 小时
			c.Header("Cache-Control", "public, max-age=3600")
		case strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") ||
			strings.HasSuffix(path, ".gif") || strings.HasSuffix(path, ".ico") ||
			strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".webp"):
			// 图片文件，缓存 7 天
			c.Header("Cache-Control", "public, max-age=604800")
		case strings.HasSuffix(path, ".woff") || strings.HasSuffix(path, ".woff2") ||
			strings.HasSuffix(path, ".ttf") || strings.HasSuffix(path, ".eot"):
			// 字体文件，缓存 30 天
			c.Header("Cache-Control", "public, max-age=2592000")
		default:
			// 其他文件不缓存
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		}

		c.Next()
	}
}

// setupRoutes 配置所有 HTTP 路由
func (s *Server) setupRoutes(r *gin.Engine) {
	// 健康检查
	r.GET("/healthz", s.handleHealthCheck)

	// 版本信息
	r.GET("/version", s.handleVersion)

	// Claude API 端点（带限流中间件和黑名单检查）
	// 中间件顺序: IP限流(预检) -> 业务处理(含用户认证) -> API Key限流(后检)
	// 注意: handleClaudeMessages 内部会进行用户认证并设置 user 到上下文
	r.POST("/v1/messages", s.preAuthRateLimitMiddleware(), s.handleClaudeMessages)
	r.POST("/v1/messages/count_tokens", s.handleCountTokens)

	// OpenAI API 端点（带限流中间件和黑名单检查）
	// 中间件顺序: IP限流(预检) -> 用户认证 -> API Key限流(后检) -> 业务处理
	r.POST("/v1/chat/completions", s.preAuthRateLimitMiddleware(), s.requireAccount, s.postAuthRateLimitMiddleware(), s.handleChatCompletions)

	// 管理控制台端点（如果启用）
	if s.cfg.EnableConsole {
		s.setupConsoleRoutes(r)
	}
}

// setupConsoleRoutes 注册控制台管理路由
func (s *Server) setupConsoleRoutes(r *gin.Engine) {
	embeddedFS, _ := fs.Sub(frontend.StaticFiles, ".")
	r.Group("/frontend").Use(staticCacheMiddleware()).StaticFS("/", http.FS(embeddedFS))

	// 登录页面（无需鉴权）
	r.GET("/login", s.handleLoginPage)
	r.POST("/api/login", s.handleLogin)
	r.POST("/api/logout", s.handleLogout)

	// 需要鉴权的页面
	r.GET("/", s.requirePageAuth, s.handleAccountsPage)
	r.GET("/accounts", s.requirePageAuth, s.handleAccountsPage)

	// 设备授权
	authGroup := r.Group("/v2/auth")
	authGroup.Use(s.requireAdmin)
	{
		authGroup.POST("/start", s.handleAuthStart)
		authGroup.GET("/status/:authId", s.handleAuthStatus)
		authGroup.POST("/claim/:authId", s.handleAuthClaim)
	}

	// 账号管理
	accountsGroup := r.Group("/v2/accounts")
	accountsGroup.Use(s.requireAdmin)
	{
		accountsGroup.POST("", s.handleCreateAccount)
		accountsGroup.POST("/feed", s.handleFeedAccounts)
		accountsGroup.POST("/import", s.handleImportAccounts)
		accountsGroup.POST("/import-by-token", s.handleImportByToken)
		accountsGroup.POST("/direct-import", s.requireTestModePassword, s.handleDirectImportAccounts) // 直接导入账号（需要密码）
		accountsGroup.POST("/reset-stats", s.requireTestModePassword, s.handleResetAllAccountStats)   // 测试模式需要密码
		accountsGroup.GET("", s.handleListAccounts)
		accountsGroup.GET("/export", s.requireTestModePassword, s.handleExportAccounts) // 测试模式需要密码
		accountsGroup.GET("/incomplete", s.handleListIncompleteAccounts)
		accountsGroup.DELETE("/incomplete", s.handleDeleteIncompleteAccounts)
		accountsGroup.DELETE("/suspended", s.requireTestModePassword, s.handleDeleteSuspendedAccounts) // 删除封控账号 @author ygw
		accountsGroup.POST("/enable-all", s.requireTestModePassword, s.handleEnableAllAccounts)        // 批量启用所有账号 @author ygw
		accountsGroup.POST("/disable-all", s.requireTestModePassword, s.handleDisableAllAccounts)      // 批量禁用所有账号 @author ygw
		accountsGroup.GET("/:id", s.handleGetAccount)
		accountsGroup.GET("/:id/quota", s.handleGetAccountQuota)
		accountsGroup.PATCH("/:id", s.handleUpdateAccount)
		accountsGroup.DELETE("/:id", s.requireTestModePassword, s.handleDeleteAccount) // 测试模式需要密码
		accountsGroup.POST("/:id/refresh", s.handleRefreshAccount)
		accountsGroup.POST("/sync-emails", s.handleSyncAccountEmails)   // 同步所有账号邮箱
		accountsGroup.POST("/refresh-quotas", s.handleRefreshAllQuotas) // 手动刷新所有账号配额 @author ygw - 被动刷新策略
	}

	// 控制台 API 测试（绕过 API key 检查）
	r.POST("/v2/test/chat/completions", s.requireAdmin, s.handleConsoleChatTest)
	r.POST("/v2/test/messages", s.requireAdmin, s.handleConsoleClaudeTest)

	// 设置管理
	settingsGroup := r.Group("/v2/settings")
	settingsGroup.Use(s.requireAdmin)
	{
		settingsGroup.GET("", s.handleGetSettings)
		settingsGroup.PUT("", s.requireTestModePassword, s.handleUpdateSettings) // 测试模式需要密码
	}

	// 代理池管理
	proxiesGroup := r.Group("/v2/proxies")
	proxiesGroup.Use(s.requireAdmin)
	{
		proxiesGroup.GET("", s.handleListProxies)
		proxiesGroup.POST("", s.handleCreateProxy)
		proxiesGroup.PUT("/:id", s.handleUpdateProxy)
		proxiesGroup.DELETE("/:id", s.handleDeleteProxy)
		proxiesGroup.POST("/:id/toggle", s.handleToggleProxy)
	}

	// 模型列表
	r.GET("/v2/models", s.requireAdmin, s.handleGetModels)

	// 数据备份和恢复
	backupGroup := r.Group("/v2/backup")
	backupGroup.Use(s.requireAdmin)
	{
		backupGroup.GET("/export", s.requireTestModePassword, s.handleBackupExport) // 测试模式需要密码
		backupGroup.POST("/import", s.handleBackupImport)
	}

	// 请求日志管理
	logsGroup := r.Group("/v2/logs")
	logsGroup.Use(s.requireAdmin)
	{
		logsGroup.GET("", s.handleGetLogs)
		logsGroup.GET("/stats", s.handleGetStats)
		logsGroup.POST("/cleanup", s.requireTestModePassword, s.handleCleanupLogs) // 测试模式需要密码
	}

	// 服务日志流（SSE）
	r.GET("/v2/server-logs/stream", s.requireAdmin, s.handleServerLogsStream)

	// IP黑名单管理
	ipGroup := r.Group("/v2/ips")
	ipGroup.Use(s.requireAdmin)
	{
		ipGroup.GET("/blocked", s.handleGetBlockedIPs)
		ipGroup.GET("/visitors", s.handleGetVisitorIPs)
		ipGroup.POST("/block", s.requireTestModePassword, s.handleBlockIP)     // 测试模式需要密码
		ipGroup.POST("/unblock", s.requireTestModePassword, s.handleUnblockIP) // 测试模式需要密码
		ipGroup.GET("/config/:ip", s.handleGetIPConfig)                        // 获取IP配置
		ipGroup.PUT("/config/:ip", s.handleUpdateIPConfig)                     // 更新IP配置（备注、限制等）
	}

	// 激活码/机器码黑名单管理（用于白嫖模式）@author ygw
	// 用户管理
	usersGroup := r.Group("/v2/users")
	usersGroup.Use(s.requireAdmin)
	{
		usersGroup.POST("", s.requireTestModePassword, s.handleCreateUser)                    // 测试模式需要密码
		usersGroup.POST("/batch-vip", s.requireTestModePassword, s.handleBatchCreateVIPUsers) // 批量创建VIP用户 @author ygw
		usersGroup.GET("", s.handleListUsers)
		usersGroup.GET("/:id", s.handleGetUser)
		usersGroup.PATCH("/:id", s.requireTestModePassword, s.handleUpdateUser)  // 测试模式需要密码（包含禁用用户）
		usersGroup.DELETE("/:id", s.requireTestModePassword, s.handleDeleteUser) // 测试模式需要密码
		usersGroup.POST("/:id/regenerate-key", s.handleRegenerateAPIKey)
		usersGroup.GET("/:id/stats", s.handleGetUserStats)
		usersGroup.GET("/:id/ips", s.handleGetUserIPs) // 获取用户关联的 IP 列表 @author ygw
	}

	// 用户统计总览
	r.GET("/v2/stats/users", s.requireAdmin, s.handleGetAllUsersStats)

	// 在线用户统计
	r.GET("/v2/stats/online", s.requireAdmin, s.handleGetOnlineStats)

	// AWS 延迟检测 @author ygw
	r.GET("/v2/health/aws-latency", s.requireAdmin, s.handleAwsLatency)

	// 开发工具
	devtoolsGroup := r.Group("/v2/devtools")
	devtoolsGroup.Use(s.requireAdmin)
	{
		devtoolsGroup.GET("/claude-code/config", s.handleGetClaudeCodeConfig)
		devtoolsGroup.POST("/claude-code/config", s.handleSaveClaudeCodeConfig)
		devtoolsGroup.GET("/droid/config", s.handleGetDroidConfig)
		devtoolsGroup.POST("/droid/config", s.handleSaveDroidConfig)
	}
}

// handleHealthCheck 返回服务健康状态
func (s *Server) handleHealthCheck(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

// handleVersion 返回版本信息
func (s *Server) handleVersion(c *gin.Context) {
	c.JSON(200, gin.H{"version": s.version})
}

// handleLoginPage 提供登录页面
func (s *Server) handleLoginPage(c *gin.Context) {
	data, err := s.readFrontendFile("login.html")
	if err != nil {
		c.String(500, "加载页面失败")
		return
	}
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Data(200, "text/html; charset=utf-8", data)
}

// handleAccountsPage 提供账号管理页面
func (s *Server) handleAccountsPage(c *gin.Context) {
	data, err := s.readFrontendFile("index.html")
	if err != nil {
		c.String(500, "加载页面失败")
		return
	}
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Data(200, "text/html; charset=utf-8", data)
}

func (s *Server) readFrontendFile(name string) ([]byte, error) {
	return frontend.StaticFiles.ReadFile(name)
}
