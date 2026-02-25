package main
 
import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"claude-api/internal/api"
	"claude-api/internal/config"
	"claude-api/internal/database"
	"claude-api/internal/logger"
	"runtime"
	"syscall"
	"time"

	_ "time/tzdata" // 嵌入时区数据库，解决 Windows 下时区加载失败问题
)

// Version 版本号，通过 ldflags 注入
var Version = "dev"

func main() {
	// 解析命令行参数
	portFlag := flag.Int("port", 0, "服务器监听端口（优先级最高，0 表示使用系统配置或默认值 62311）")
	flag.IntVar(portFlag, "p", 0, "服务器监听端口（-port 的简写）")
	noBrowserFlag := flag.Bool("no-browser", false, "禁用启动时自动打开浏览器")
	dataDirFlag := flag.String("data-dir", "", "数据目录路径（存放数据库和日志，不指定则使用当前工作目录）")
	flag.Parse()

	// 设置时区为北京时间（UTC+8）
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		log.Printf("警告: 加载时区失败，使用 UTC+8: %v", err)
		loc = time.FixedZone("CST", 8*3600)
	}
	time.Local = loc

	// 确定数据目录（仅当通过 -data-dir 参数指定时才使用）
	// 命令行版本：不指定 -data-dir，使用当前工作目录
	// 桌面应用版本：通过 -data-dir 指定用户数据目录
	dataDir := *dataDirFlag
	if dataDir != "" {
		// 确保数据目录存在
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			log.Fatalf("创建数据目录失败: %v", err)
		}

		// 切换到数据目录，使数据库和日志文件都存放在此目录
		if err := os.Chdir(dataDir); err != nil {
			log.Fatalf("切换到数据目录失败: %v", err)
		}

		// 自动迁移旧位置的数据库（如果存在）
		migrateOldDatabase(dataDir)
	}

	// 初始化日志系统
	if err := logger.Init(); err != nil {
		log.Fatalf("初始化日志系统失败: %v", err)
	}
	logger.Info("=== Claude 无限畅享版 %s 服务器启动中 ===", Version)
	if dataDir != "" {
		logger.Info("数据目录: %s", dataDir)
	}
	logger.Info("系统时区: %s", time.Local.String())

	// 加载配置（优先 YAML，兼容 JSON，无配置文件则使用默认值）
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Warn("加载配置文件失败，使用默认配置: %v", err)
		cfg = config.Load()
	}

	// 记录配置来源
	filePort := cfg.Server.Port
	fileHost := cfg.Server.Host
	fileDebug := cfg.Debug
	if filePort > 0 && filePort != 62311 {
		logger.Info("从配置文件读取端口配置: %d", filePort)
	}
	if fileHost != "" && fileHost != "0.0.0.0" {
		logger.Info("从配置文件读取主机配置: %s", fileHost)
	}
	if fileDebug {
		logger.Info("从配置文件读取调试模式: 已开启")
	}

	// 设置调试日志（从配置文件读取，默认关闭）
	logger.SetDebugEnabled(fileDebug)

	// 保存命令行指定的端口，用于后续判断
	cliPort := *portFlag
	logger.Info("配置已加载 - 默认端口: %d, 配置文件端口: %d, 命令行端口: %d, 控制台: %v", cfg.Port, filePort, cliPort, cfg.EnableConsole)

	// 开源版本：最大账号数量为100
	maxAccounts := cfg.GetMaxAccounts()
	logger.Info("开源版本 - 最大账号数: %d", maxAccounts)

	// 初始化数据库
	db, err := database.New(cfg)
	if err != nil {
		logger.Error("初始化数据库失败: %v", err)
		log.Fatalf("数据库初始化失败: %v", err)
	}
	defer db.Close()
	logger.Info("数据库初始化成功")

	// 检查账号数量（仅记录警告，不阻止启动）
	accounts, err := db.ListAccounts(context.Background(), nil, "created_at", false)
	if err != nil {
		logger.Warn("检查账号数量失败: %v", err)
	} else {
		currentCount := len(accounts)
		if currentCount > maxAccounts {
			logger.Warn("账号数量超限警告 - 最大支持 %d 个账号，已有 %d 个", maxAccounts, currentCount)
			logger.Warn("超限账号将无法使用")
		} else {
			logger.Info("账号数量检查通过: %d/%d", currentCount, maxAccounts)
		}
	}

	// 从数据库加载设置并更新配置
	settings, err := db.GetSettings(context.Background())
	if err != nil {
		logger.Warn("从数据库加载设置失败，使用默认配置: %v", err)
	} else {
		// 使用数据库设置更新配置
		if settings.APIKey != "" {
			cfg.OpenAIKeys = []string{settings.APIKey}
			logger.Info("从数据库加载 API key 配置成功")
		}
		if settings.AdminPassword != "" {
			cfg.AdminPassword = settings.AdminPassword
		}
	}

	// 确定最终端口：命令行参数 > 配置文件 > 系统配置 > 默认值(62311)
	if cliPort > 0 && cliPort <= 65535 {
		cfg.Port = cliPort
		logger.Info("使用命令行指定端口: %d", cfg.Port)
	} else if filePort > 0 && filePort <= 65535 {
		cfg.Port = filePort
		logger.Info("使用配置文件端口: %d", cfg.Port)
	} else if settings != nil && settings.PortConfigured {
		// 用户已在界面配置过端口，使用数据库中的值（已在 GetSettings 中更新到 cfg.Port）
		logger.Info("使用系统配置端口: %d", cfg.Port)
	} else {
		logger.Info("使用默认端口: %d", cfg.Port)
	}

	// 确定最终主机：配置文件 > 默认值(0.0.0.0)
	if fileHost != "" {
		cfg.Host = fileHost
		logger.Info("使用配置文件主机: %s", cfg.Host)
	} else {
		logger.Info("使用默认主机: %s", cfg.Host)
	}

	// 创建 API 服务器
	server := api.NewServer(cfg, db, Version)

	// 启动 HTTP 服务器
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      server.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 300 * time.Second, // 流式响应需要较长超时
		IdleTimeout:  120 * time.Second,
	}

	// 在 goroutine 中启动服务器
	go func() {
		logger.Info("HTTP 服务器监听中 - 地址: http://%s:%d", cfg.Host, cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP 服务器启动失败: %v", err)
			log.Fatalf("服务器启动失败: %v", err)
		}
	}()

	// 等待服务器启动后自动打开浏览器（可通过 -no-browser 禁用）
	if !*noBrowserFlag {
		go func() {
			time.Sleep(500 * time.Millisecond) // 等待服务器启动
			openBrowser(cfg.Host, cfg.Port)
		}()
	} else {
		logger.Info("已禁用自动打开浏览器")
	}

	// 后台令牌刷新任务已关闭，改为被动刷新策略
	// 当请求进来时按需刷新令牌和配额，对用户无感知
	// go server.BackgroundTokenRefresh(context.Background())

	// 启动缓存系统（账号池、设置缓存的后台刷新）
	server.StartCaches(context.Background())

	// 启动后台检查任务（账号超限、日志清理、远程验证、在线IP清理）
	quit := make(chan os.Signal, 1)

	// 启动在线 IP 清理任务（每分钟清理一次）
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			server.CleanupOnlineIPs()
		}
	}()

	// 后台配额同步任务已关闭，改为被动刷新策略
	// 当请求进来时按需刷新配额，配额错误时刷新令牌，对用户无感知
	// logger.Info("启动后台配额同步任务")
	// go func() {
	// 	server.RefreshAllAccountsQuota(context.Background())
	// 	for {
	// 		settings, _ := db.GetSettings(context.Background())
	// 		interval := 120
	// 		if settings != nil && settings.QuotaRefreshInterval > 0 {
	// 			interval = settings.QuotaRefreshInterval
	// 		}
	// 		time.Sleep(time.Duration(interval) * time.Second)
	// 		server.RefreshAllAccountsQuota(context.Background())
	// 	}
	// }()

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			// 检查账号数量（仅记录警告，不关闭服务）
			accounts, err := db.ListAccounts(context.Background(), nil, "created_at", false)
			if err == nil {
				currentCount := len(accounts)
				maxAccounts := cfg.GetMaxAccounts()
				if currentCount > maxAccounts {
					logger.Warn("账号数量超限警告 - 已达账号数量上限 %d，已有 %d 个", maxAccounts, currentCount)
				}
			}
			// 自动清理过期日志
			settings, err := db.GetSettings(context.Background())
			if err == nil && settings.EnableRequestLog && settings.LogRetentionDays > 0 {
				deleted, err := db.CleanupOldLogs(context.Background(), settings.LogRetentionDays)
				if err == nil && deleted > 0 {
					logger.Info("自动清理过期日志完成，删除 %d 条记录（保留 %d 天）", deleted, settings.LogRetentionDays)
				}
			}
			// 恢复用尽超过30天的账号
			recovered, err := db.RecoverExhaustedAccounts(context.Background())
			if err != nil {
				logger.Error("恢复用尽账号失败: %v", err)
			} else if recovered > 0 {
				logger.Info("已恢复 %d 个用尽账号（超过30天自动恢复）", recovered)
				// 使账号缓存失效
				server.InvalidateAccountCache(context.Background())
			}
		}
	}()

	// 等待中断信号
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("收到关闭信号,正在优雅关闭服务器...")

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 停止日志worker
	server.StopLogWorker()

	// 先关闭 SSE 订阅者，让 SSE 连接能够正常结束
	logger.CloseSubscribers()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("服务器强制关闭: %v", err)
	}

	logger.Info("=== Claude 无限畅享版 %s 服务器已停止 ===", Version)
	logger.Close()
	log.Println("服务器已退出")
}

// openBrowser 自动打开浏览器访问管理页面
func openBrowser(host string, port int) {
	// 如果监听 0.0.0.0，使用 localhost 访问
	accessHost := host
	if host == "0.0.0.0" {
		accessHost = "localhost"
	}
	url := fmt.Sprintf("http://%s:%d", accessHost, port)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		logger.Info("请手动打开浏览器访问: %s", url)
		return
	}

	if err := cmd.Start(); err != nil {
		logger.Warn("自动打开浏览器失败: %v，请手动访问: %s", err, url)
	} else {
		logger.Info("已自动打开浏览器访问: %s", url)
	}
}

// getLocalIP 获取本机IP地址
// @author ygw
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

// getDefaultDataDir 获取默认数据目录
// macOS: ~/Library/Application Support/Claude-API-Server/
// Windows: %APPDATA%\Claude-API-Server\
// Linux: ~/.local/share/Claude-API-Server/
func getDefaultDataDir() string {
	var baseDir string

	switch runtime.GOOS {
	case "darwin":
		// macOS: ~/Library/Application Support/
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "."
		}
		baseDir = filepath.Join(homeDir, "Library", "Application Support")
	case "windows":
		// Windows: %APPDATA%
		baseDir = os.Getenv("APPDATA")
		if baseDir == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "."
			}
			baseDir = filepath.Join(homeDir, "AppData", "Roaming")
		}
	default:
		// Linux 和其他系统: ~/.local/share/
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "."
		}
		baseDir = filepath.Join(homeDir, ".local", "share")
	}

	return filepath.Join(baseDir, "Claude-API-Server")
}

// migrateOldDatabase 自动迁移旧位置的数据库到新位置
// 检查可执行文件所在目录是否有旧的 data.sqlite3，如果有则迁移到数据目录
func migrateOldDatabase(newDataDir string) {
	newDBPath := filepath.Join(newDataDir, "data.sqlite3")

	// 如果新位置已有数据库，不需要迁移
	if _, err := os.Stat(newDBPath); err == nil {
		return
	}

	// 收集所有可能的旧数据库位置
	var oldPaths []string

	// 1. 可执行文件所在目录
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		oldPaths = append(oldPaths, filepath.Join(exeDir, "data.sqlite3"))

		// macOS .app 包内的特殊处理：Contents/MacOS -> Contents/Resources
		if runtime.GOOS == "darwin" {
			oldPaths = append(oldPaths, filepath.Join(exeDir, "..", "Resources", "data.sqlite3"))
		}
	}

	// 2. Windows 临时目录（旧版桌面应用使用的位置）
	if runtime.GOOS == "windows" {
		tempDir := os.TempDir()
		oldPaths = append(oldPaths, filepath.Join(tempDir, "claude-api-server", "data.sqlite3"))
	}

	// 3. 当前工作目录（命令行版本可能使用）
	if cwd, err := os.Getwd(); err == nil && cwd != newDataDir {
		oldPaths = append(oldPaths, filepath.Join(cwd, "data.sqlite3"))
	}

	// 查找第一个存在的旧数据库
	var oldDBPath string
	for _, path := range oldPaths {
		if _, err := os.Stat(path); err == nil {
			oldDBPath = path
			break
		}
	}

	// 如果没有找到旧数据库，不需要迁移
	if oldDBPath == "" {
		return
	}

	// 获取旧数据库所在目录
	oldDir := filepath.Dir(oldDBPath)

	// 执行迁移
	log.Printf("发现旧版数据库，正在迁移: %s -> %s", oldDBPath, newDBPath)

	// 复制数据库文件
	if err := copyFile(oldDBPath, newDBPath); err != nil {
		log.Printf("警告: 数据库迁移失败: %v", err)
		return
	}

	// 同时迁移 WAL 和 SHM 文件（如果存在）
	for _, suffix := range []string{"-wal", "-shm"} {
		oldFile := oldDBPath + suffix
		if _, err := os.Stat(oldFile); err == nil {
			newFile := newDBPath + suffix
			copyFile(oldFile, newFile)
		}
	}

	// 迁移 config.json（如果存在）
	oldConfigPath := filepath.Join(oldDir, "config.json")
	if runtime.GOOS == "darwin" {
		resourcesConfig := filepath.Join(oldDir, "..", "Resources", "config.json")
		if _, err := os.Stat(resourcesConfig); err == nil {
			oldConfigPath = resourcesConfig
		}
	}
	if _, err := os.Stat(oldConfigPath); err == nil {
		newConfigPath := filepath.Join(newDataDir, "config.json")
		if _, err := os.Stat(newConfigPath); os.IsNotExist(err) {
			copyFile(oldConfigPath, newConfigPath)
			log.Printf("已迁移配置文件: %s -> %s", oldConfigPath, newConfigPath)
		}
	}

	log.Printf("数据库迁移完成！旧文件保留在原位置，确认无误后可手动删除")
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
