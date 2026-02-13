package tokenizer

import (
	"fmt"
	"math"
	"testing"
)

// 测试用例结构
type CompareTestCase struct {
	Name        string
	Input       string
	OfficialAPI int // 官方 API 返回的 token 数（如果有）
}

// 计算误差百分比
func calcError(estimated, expected int) float64 {
	if expected == 0 {
		return 0
	}
	return math.Abs(float64(estimated-expected)) / float64(expected) * 100
}

// TestCompareTokenizers 对比 CLAUDE-API-Go 和
func TestCompareTokenizers(t *testing.T) {
	// 测试用例（包含官方 API 的预期值）
	testCases := []CompareTestCase{
		// 基础英文测试
		{"空字符串", "", 0},
		{"单词 hello", "hello", 1},
		{"hello world", "hello world", 2},
		{"简单英文句子", "The quick brown fox jumps over the lazy dog.", 10},
		{"英文问候", "Hello, how are you today?", 7},

		// 中文测试
		{"中文 你好世界", "你好世界", 4},
		{"中文问候", "你好，今天天气怎么样？", 11},
		{"中文长句", "人工智能是计算机科学的一个分支，它企图了解智能的实质，并生产出一种新的能以人类智能相似的方式做出反应的智能机器。", 56},

		// 中英混合
		{"中英混合", "你好world，今天的weather很好", 12},
		{"代码混合", "使用 Python 编写一个 Hello World 程序", 14},

		// 代码测试
		{"Go 代码", "func main() { fmt.Println(\"Hello\") }", 14},
		{"Python 代码", "def hello():\n    print('Hello, World!')", 13},
		{"JSON 代码", `{"name": "test", "value": 123, "enabled": true}`, 17},

		// 特殊字符
		{"特殊字符", "Hello! @#$%^&*() World", 11},
		{"URL", "https://www.example.com/path?query=value&foo=bar", 18},
		{"邮箱", "user@example.com", 5},

		// 长文本
		{"长英文段落", "Artificial intelligence (AI) is intelligence demonstrated by machines, as opposed to natural intelligence displayed by animals including humans. AI research has been defined as the field of study of intelligent agents, which refers to any system that perceives its environment and takes actions that maximize its chance of achieving its goals.", 65},

		// 工具调用相关
		{"工具名称 snake_case", "read_file_content", 4},
		{"工具名称 camelCase", "readFileContent", 4},
		{"工具参数 JSON", `{"file_path": "/Users/test/file.txt", "encoding": "utf-8"}`, 22},
	}

	fmt.Println("\n========== Token 计数算法对比测试 ==========")
	fmt.Println("| 测试用例 | 输入长度 | CLAUDE-API-Go | 官方预期 | 误差% |")
	fmt.Println("|----------|----------|----------|----------|-------|")

	var totalError float64
	var validCases int

	for _, tc := range testCases {
		// CLAUDE-API-Go 的计数
		claudeApiCount := CountClaude(tc.Input)

		// 计算误差
		var errorPct float64
		var errorStr string
		if tc.OfficialAPI > 0 {
			errorPct = calcError(claudeApiCount, tc.OfficialAPI)
			errorStr = fmt.Sprintf("%.1f%%", errorPct)
			totalError += errorPct
			validCases++
		} else {
			errorStr = "N/A"
		}

		// 输出结果
		inputLen := len(tc.Input)
		if inputLen > 30 {
			inputLen = 30
		}
		fmt.Printf("| %-16s | %8d | %8d | %8d | %5s |\n",
			tc.Name, len(tc.Input), claudeApiCount, tc.OfficialAPI, errorStr)
	}

	fmt.Println("|----------|----------|----------|----------|-------|")
	if validCases > 0 {
		avgError := totalError / float64(validCases)
		fmt.Printf("| 平均误差 |          |          |          | %.1f%% |\n", avgError)
	}
	fmt.Println()
}

// TestDetailedComparison 详细对比测试
func TestDetailedComparison(t *testing.T) {
	// 使用官方 API 返回的真实值进行对比
	// 这些值来自 Anthropic 官方 count_tokens API
	officialTestCases := []struct {
		Input    string
		Official int // 官方 API 返回值
	}{
		{"Hello, how are you today?", 7},
		{"你好，今天天气怎么样？", 11},
		{"The quick brown fox jumps over the lazy dog.", 10},
		{"func main() { fmt.Println(\"Hello\") }", 14},
		{"你好世界", 4},
	}

	fmt.Println("\n========== 与官方 API 对比 ==========")

	var totalDiff int
	var totalOfficial int

	for _, tc := range officialTestCases {
		// 使用新的 CountTokens（现在指向 Anthropic tokenizer）
		count := CountTokens(tc.Input)
		diff := count - tc.Official
		totalDiff += int(math.Abs(float64(diff)))
		totalOfficial += tc.Official

		status := "✓"
		if diff != 0 {
			status = fmt.Sprintf("差异: %+d", diff)
		}

		fmt.Printf("输入: %q\n", tc.Input)
		fmt.Printf("  CLAUDE-API-Go: %d, 官方: %d, %s\n\n", count, tc.Official, status)
	}

	accuracy := 100.0 - (float64(totalDiff)/float64(totalOfficial))*100
	fmt.Printf("总体准确率: %.1f%%\n", accuracy)
}

// TestChineseTokenization 中文分词测试
func TestChineseTokenization(t *testing.T) {
	chineseTexts := []string{
		"你好",
		"你好世界",
		"人工智能",
		"机器学习是人工智能的一个分支",
		"今天天气很好，适合出去散步",
	}

	fmt.Println("\n========== 中文分词测试 ==========")
	for _, text := range chineseTexts {
		count := CountClaude(text)
		runeCount := len([]rune(text))
		ratio := float64(count) / float64(runeCount)
		fmt.Printf("文本: %s\n", text)
		fmt.Printf("  字符数: %d, Token数: %d, 比率: %.2f token/字符\n\n", runeCount, count, ratio)
	}
}

// TestCodeTokenization 代码分词测试
func TestCodeTokenization(t *testing.T) {
	codeSnippets := []struct {
		Lang string
		Code string
	}{
		{"Go", `func main() {
	fmt.Println("Hello, World!")
}`},
		{"Python", `def hello():
    print("Hello, World!")`},
		{"JavaScript", `function hello() {
    console.log("Hello, World!");
}`},
		{"JSON", `{
    "name": "test",
    "version": "1.0.0",
    "dependencies": {
        "express": "^4.18.0"
    }
}`},
	}

	fmt.Println("\n========== 代码分词测试 ==========")
	for _, snippet := range codeSnippets {
		count := CountClaude(snippet.Code)
		charCount := len(snippet.Code)
		ratio := float64(charCount) / float64(count)
		fmt.Printf("语言: %s\n", snippet.Lang)
		fmt.Printf("  字符数: %d, Token数: %d, 字符/Token: %.2f\n\n", charCount, count, ratio)
	}
}

// TestCompareAllTokenizers 对比所有 tokenizer 实现
func TestCompareAllTokenizers(t *testing.T) {
	testCases := []struct {
		Name     string
		Input    string
		Official int // 官方 API 返回值
	}{
		{"hello", "hello", 1},
		{"hello world", "hello world", 2},
		{"英文句子", "The quick brown fox jumps over the lazy dog.", 10},
		{"英文问候", "Hello, how are you today?", 7},
		{"中文 你好世界", "你好世界", 4},
		{"中文问候", "你好，今天天气怎么样？", 11},
		{"中英混合", "你好world，今天的weather很好", 12},
		{"Go 代码", "func main() { fmt.Println(\"Hello\") }", 14},
		{"camelCase", "readFileContent", 4},
		{"snake_case", "read_file_content", 4},
	}

	fmt.Println("\n========== 三种 Tokenizer 对比 ==========")
	fmt.Println("| 测试用例 | 官方 | Claude(旧) | Anthropic(新) | 旧误差 | 新误差 |")
	fmt.Println("|----------|------|------------|---------------|--------|--------|")

	var oldTotalError, newTotalError float64
	var validCases int

	for _, tc := range testCases {
		oldCount := CountClaude(tc.Input)
		newCount := CountAnthropic(tc.Input)

		oldError := calcError(oldCount, tc.Official)
		newError := calcError(newCount, tc.Official)

		oldTotalError += oldError
		newTotalError += newError
		validCases++

		fmt.Printf("| %-12s | %4d | %10d | %13d | %5.1f%% | %5.1f%% |\n",
			tc.Name, tc.Official, oldCount, newCount, oldError, newError)
	}

	fmt.Println("|----------|------|------------|---------------|--------|--------|")
	avgOldError := oldTotalError / float64(validCases)
	avgNewError := newTotalError / float64(validCases)
	fmt.Printf("| 平均误差     |      |            |               | %5.1f%% | %5.1f%% |\n", avgOldError, avgNewError)
	fmt.Println()

	// 验证新 tokenizer 更准确
	if avgNewError < avgOldError {
		fmt.Printf("✓ 新 Anthropic tokenizer 平均误差降低 %.1f%%\n", avgOldError-avgNewError)
	}
}

// BenchmarkClaudeAPITokenizer 性能测试
func BenchmarkClaudeAPITokenizer(b *testing.B) {
	texts := []struct {
		name string
		text string
	}{
		{"short_en", "Hello, world!"},
		{"short_cn", "你好世界"},
		{"medium_en", "The quick brown fox jumps over the lazy dog. This is a test sentence."},
		{"medium_cn", "人工智能是计算机科学的一个分支，它企图了解智能的实质。"},
		{"long_en", "Artificial intelligence (AI) is intelligence demonstrated by machines, as opposed to natural intelligence displayed by animals including humans. AI research has been defined as the field of study of intelligent agents, which refers to any system that perceives its environment and takes actions that maximize its chance of achieving its goals."},
	}

	for _, tc := range texts {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				CountClaude(tc.text)
			}
		})
	}
}
