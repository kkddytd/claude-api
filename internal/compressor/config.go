package compressor

import "time"

// 压缩摘要相关常量
const (
	// SummaryPreamble 压缩摘要的开篇语
	SummaryPreamble = `上下文已使用结构化9节算法压缩。所有关键技术细节、代码模式、架构决策和用户意图已保留，可无缝继续对话。`

	// 工具结果裁剪配置
	MaxToolResultLength = 2000 // 单个工具结果最大字符数
	KeepHeadChars       = 800  // 保留头部字符数
	KeepTailChars       = 800  // 保留尾部字符数
)

// CompressConfig 上下文压缩配置
// @author ygw
type CompressConfig struct {
	// 触发条件
	TokenThreshold   int // 触发压缩的 token 阈值，默认 150000
	MessageThreshold int // 触发压缩的消息数阈值，默认 100

	// 保留策略
	KeepMessageCount int // 保留最近的消息数量，默认 6
	MaxToolLookback  int // 工具调用最大回溯距离，默认 10

	// 摘要生成
	MaxBatchChars          int    // 单批摘要最大字符数，默认 80000
	SummaryModel           string // 用于生成摘要的模型
	MaxSingleSummaryTokens int    // 单个摘要块的最大 token，默认 25000

	// 缓存配置
	CacheEnabled bool          // 是否启用缓存，默认 true
	CacheDir     string        // 缓存目录
	CacheTTL     time.Duration // 缓存过期时间，默认 24h
}

// DefaultConfig 返回默认配置
func DefaultConfig() *CompressConfig {
	return &CompressConfig{
		TokenThreshold:         180000,
		MessageThreshold:       100,
		KeepMessageCount:       6,
		MaxToolLookback:        10,
		MaxBatchChars:          80000,
		SummaryModel:           "claude-sonnet-4-5-20250929",
		MaxSingleSummaryTokens: 25000,
		CacheEnabled:           true,
		CacheDir:               "cache/summaries",
		CacheTTL:               24 * time.Hour,
	}
}
