package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	InfoLogger   *log.Logger
	WarnLogger   *log.Logger
	ErrorLogger  *log.Logger
	DebugLogger  *log.Logger
	debugEnabled bool
	logFile      *os.File

	// 日志广播
	subscribers   = make(map[chan string]struct{})
	subscribersMu sync.RWMutex
)

// broadcastWriter 将日志同时写入原始 writer 和广播给订阅者
type broadcastWriter struct {
	original io.Writer
}

func (w *broadcastWriter) Write(p []byte) (n int, err error) {
	n, err = w.original.Write(p)
	// 广播给所有订阅者
	msg := string(p)
	subscribersMu.RLock()
	for ch := range subscribers {
		select {
		case ch <- msg:
		default:
			// 如果 channel 满了就跳过，避免阻塞
		}
	}
	subscribersMu.RUnlock()
	return
}

// Subscribe 订阅日志流
func Subscribe() chan string {
	ch := make(chan string, 100)
	subscribersMu.Lock()
	subscribers[ch] = struct{}{}
	subscribersMu.Unlock()
	return ch
}

// Unsubscribe 取消订阅
func Unsubscribe(ch chan string) {
	subscribersMu.Lock()
	_, exists := subscribers[ch]
	if exists {
		delete(subscribers, ch)
		close(ch)
	}
	subscribersMu.Unlock()
}

// Init 初始化日志系统
func Init() error {
	// 创建日志目录
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("创建日志目录失败: %v", err)
	}

	// 创建日志文件（按日期命名）
	logFileName := filepath.Join(logDir, fmt.Sprintf("server_%s.log", time.Now().Format("2006-01-02")))
	var err error
	logFile, err = os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("创建日志文件失败: %v", err)
	}

	// 同时输出到控制台和文件，并广播
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	broadcastW := &broadcastWriter{original: multiWriter}

	InfoLogger = log.New(broadcastW, "[INFO] ", log.Ldate|log.Ltime|log.Lshortfile)
	WarnLogger = log.New(broadcastW, "[WARN] ", log.Ldate|log.Ltime|log.Lshortfile)
	ErrorLogger = log.New(broadcastW, "[ERROR] ", log.Ldate|log.Ltime|log.Lshortfile)
	DebugLogger = log.New(broadcastW, "[DEBUG] ", log.Ldate|log.Ltime|log.Lshortfile)

	InfoLogger.Println("日志系统初始化成功，日志文件: " + logFileName)
	return nil
}

// CloseSubscribers 关闭所有订阅者的 channel（用于优雅关闭时先断开 SSE 连接）
func CloseSubscribers() {
	subscribersMu.Lock()
	for ch := range subscribers {
		delete(subscribers, ch)
		close(ch)
	}
	subscribersMu.Unlock()
}

// Close 关闭日志文件并断开所有订阅者
func Close() {
	CloseSubscribers()

	if logFile != nil {
		logFile.Close()
	}
}

// SetDebugEnabled 设置调试日志开关
func SetDebugEnabled(enabled bool) {
	debugEnabled = enabled
	if enabled {
		InfoLogger.Println("调试日志已启用")
	} else {
		InfoLogger.Println("调试日志已禁用")
	}
}

// IsDebugEnabled 返回调试模式是否开启
func IsDebugEnabled() bool {
	return debugEnabled
}

// Info 记录信息级别日志
func Info(format string, v ...interface{}) {
	if InfoLogger != nil {
		InfoLogger.Output(2, fmt.Sprintf(format, v...))
	}
}

// Warn 记录警告级别日志
func Warn(format string, v ...interface{}) {
	if WarnLogger != nil {
		WarnLogger.Output(2, fmt.Sprintf(format, v...))
	}
}

// Error 记录错误级别日志
func Error(format string, v ...interface{}) {
	if ErrorLogger != nil {
		ErrorLogger.Output(2, fmt.Sprintf(format, v...))
	}
}

// Debug 记录调试级别日志
func Debug(format string, v ...interface{}) {
	if DebugLogger != nil && debugEnabled {
		DebugLogger.Output(2, fmt.Sprintf(format, v...))
	}
}

// LogRequest 记录 HTTP 请求详情
func LogRequest(method, path, ip string, statusCode int, duration time.Duration) {
	Info("%s %s from %s - Status: %d - Duration: %v", method, path, ip, statusCode, duration)
}
