package compressor

import (
	"fmt"
	"strings"
	"testing"
)

// SimulatedBlock 模拟摘要块
type SimulatedBlock struct {
	ID       int
	StartIdx int
	EndIdx   int
	Summary  string
	Tokens   int
}

// SimulationResult 模拟结果
type SimulationResult struct {
	Round           int
	TotalMessages   int
	CacheHit        bool
	ReusedBlocks    int
	NewBlockCreated bool
	NewBlockRange   string
	TotalBlocks     int
	KeepMessages    int
	FinalStructure  string
	TotalSummaryTokens int
}

// RunSimulation 运行压缩模拟
// @author ygw
func RunSimulation(totalRounds int, messagesPerRound int, keepCount int) []SimulationResult {
	var results []SimulationResult
	var blocks []SimulatedBlock
	compressedCount := 0

	for round := 1; round <= totalRounds; round++ {
		totalMessages := round * messagesPerRound
		result := SimulationResult{
			Round:         round,
			TotalMessages: totalMessages,
		}

		// 检查是否命中缓存
		if compressedCount > 0 {
			result.CacheHit = true
			result.ReusedBlocks = len(blocks)
		}

		// 计算需要压缩的消息范围
		startIdx := compressedCount
		remainingMessages := totalMessages - startIdx

		// 如果剩余消息数超过阈值，需要压缩
		if remainingMessages > keepCount {
			// 计算新摘要块的范围
			newBlockEnd := totalMessages - keepCount
			newBlockStart := startIdx

			if newBlockEnd > newBlockStart {
				// 创建新摘要块
				newBlock := SimulatedBlock{
					ID:       len(blocks) + 1,
					StartIdx: newBlockStart,
					EndIdx:   newBlockEnd,
					Summary:  fmt.Sprintf("摘要块%d: 消息%d-%d的内容概要", len(blocks)+1, newBlockStart+1, newBlockEnd),
					Tokens:   500, // 假设每个摘要块约500 tokens
				}
				blocks = append(blocks, newBlock)
				compressedCount = newBlockEnd

				result.NewBlockCreated = true
				result.NewBlockRange = fmt.Sprintf("%d-%d", newBlockStart+1, newBlockEnd)
			}
		}

		result.TotalBlocks = len(blocks)
		result.KeepMessages = min(keepCount, totalMessages-compressedCount)

		// 计算总摘要 tokens
		totalTokens := 0
		for _, b := range blocks {
			totalTokens += b.Tokens
		}
		result.TotalSummaryTokens = totalTokens

		// 构建最终结构描述
		result.FinalStructure = buildStructureDescription(blocks, compressedCount, totalMessages, keepCount)

		results = append(results, result)
	}

	return results
}

func buildStructureDescription(blocks []SimulatedBlock, compressedCount, totalMessages, keepCount int) string {
	var sb strings.Builder

	if len(blocks) > 0 {
		sb.WriteString("[摘要区]\n")
		for _, b := range blocks {
			sb.WriteString(fmt.Sprintf("  块%d: 消息%d-%d\n", b.ID, b.StartIdx+1, b.EndIdx))
		}
	}

	keepStart := compressedCount + 1
	keepEnd := totalMessages
	if keepEnd >= keepStart {
		sb.WriteString(fmt.Sprintf("[保留区] 消息%d-%d (%d条)\n", keepStart, keepEnd, keepEnd-keepStart+1))
	}

	return sb.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestSimulateChunkedCompression 测试分块压缩模拟
func TestSimulateChunkedCompression(t *testing.T) {
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println("                    分块压缩模拟演示")
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println()
	fmt.Println("配置参数:")
	fmt.Println("  - 每轮新增消息: 100 条")
	fmt.Println("  - 保留最近消息: 6 条")
	fmt.Println("  - 每个摘要块约: 500 tokens")
	fmt.Println()

	results := RunSimulation(10, 100, 6)

	for _, r := range results {
		fmt.Printf("【第 %d 轮】总消息数: %d\n", r.Round, r.TotalMessages)
		fmt.Println(strings.Repeat("-", 60))

		if r.CacheHit {
			fmt.Printf("  ✓ 缓存命中 - 复用 %d 个摘要块\n", r.ReusedBlocks)
		} else {
			fmt.Println("  ✗ 无缓存")
		}

		if r.NewBlockCreated {
			fmt.Printf("  ✓ 新建摘要块 - 压缩消息 %s\n", r.NewBlockRange)
		}

		fmt.Printf("  → 当前摘要块数: %d\n", r.TotalBlocks)
		fmt.Printf("  → 摘要总 tokens: %d\n", r.TotalSummaryTokens)
		fmt.Printf("  → 保留消息数: %d\n", r.KeepMessages)
		fmt.Println()
		fmt.Println("  请求结构:")
		for _, line := range strings.Split(r.FinalStructure, "\n") {
			if line != "" {
				fmt.Printf("    %s\n", line)
			}
		}
		fmt.Println()
	}

	// 展示最终请求格式
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println("                    最终请求格式示例 (第10轮)")
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println()
	fmt.Println(`[user] 历史对话摘要 - 分块模式

=== 摘要块 1 (消息 1-94) ===
用户讨论了项目初始化，确定了技术栈选型...

=== 摘要块 2 (消息 95-194) ===
实现了用户认证模块，包括登录、注册功能...

=== 摘要块 3 (消息 195-294) ===
开发了订单管理系统，支持创建、查询、取消订单...

=== 摘要块 4 (消息 295-394) ===
添加了支付集成，对接了支付宝和微信支付...

=== 摘要块 5 (消息 395-494) ===
实现了数据统计和报表导出功能...

=== 摘要块 6 (消息 495-594) ===
优化了系统性能，添加了缓存层...

=== 摘要块 7 (消息 595-694) ===
完成了单元测试和集成测试...

=== 摘要块 8 (消息 695-794) ===
部署到生产环境，配置了监控告警...

=== 摘要块 9 (消息 795-894) ===
处理了线上问题，优化了日志系统...

=== 摘要块 10 (消息 895-994) ===
添加了新功能需求，重构了部分代码...

[摘要结束，以下是最近的对话]

[assistant] 好的，我已了解之前的对话上下文（共 10 个摘要块）。请继续。

[user] 消息 995 的内容...
[assistant] 消息 996 的内容...
[user] 消息 997 的内容...
[assistant] 消息 998 的内容...
[user] 消息 999 的内容...
[assistant] 消息 1000 的内容...`)

	fmt.Println()
	fmt.Println()

	// 展示摘要块增长趋势
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println("                    摘要块增长趋势")
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println()
	fmt.Println("轮次  消息数   摘要块数  摘要Tokens  保留消息")
	fmt.Println(strings.Repeat("-", 50))
	for _, r := range results {
		fmt.Printf(" %2d    %4d      %2d        %5d        %d\n",
			r.Round, r.TotalMessages, r.TotalBlocks, r.TotalSummaryTokens, r.KeepMessages)
	}

	fmt.Println()
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println("                    关键特性说明")
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println()
	fmt.Println("1. 【增量压缩】每次只压缩新增的消息，不重复处理已压缩内容")
	fmt.Println("2. 【摘要块独立】每个摘要块保持独立，不会被重新压缩")
	fmt.Println("3. 【缓存复用】命中缓存时直接复用所有历史摘要块")
	fmt.Println("4. 【无数量限制】摘要块可以无限增长，保留完整历史信息")
	fmt.Println("5. 【信息完整】每个摘要块保留原始摘要内容，不会信息衰减")
	fmt.Println()
}
