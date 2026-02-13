package tokenizer

import (
	"sync"

	tokenizer "github.com/qhenkart/anthropic-tokenizer-go"
)

var (
	anthropicTokenizer     *tokenizer.Tokenizer
	anthropicTokenizerOnce sync.Once
	anthropicTokenizerErr  error
)

// GetAnthropicTokenizer 返回官方 Anthropic tokenizer 单例
func GetAnthropicTokenizer() (*tokenizer.Tokenizer, error) {
	anthropicTokenizerOnce.Do(func() {
		anthropicTokenizer, anthropicTokenizerErr = tokenizer.New()
	})
	return anthropicTokenizer, anthropicTokenizerErr
}

// CountAnthropic 使用官方 Anthropic tokenizer 计算 token 数量
func CountAnthropic(text string) int {
	if text == "" {
		return 0
	}
	t, err := GetAnthropicTokenizer()
	if err != nil {
		// 回退到简单估算
		return fallbackEstimate(text)
	}
	return t.Tokens(text)
}

// fallbackEstimate 简单估算 token 数量
// 英文约 4 字符/token，中文约 1.5 字符/token
func fallbackEstimate(text string) int {
	if text == "" {
		return 0
	}

	var chineseChars, otherChars int
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			// CJK 统一汉字
			chineseChars++
		} else if r >= 0x3400 && r <= 0x4DBF {
			// CJK 扩展 A
			chineseChars++
		} else if r >= 0x20000 && r <= 0x2A6DF {
			// CJK 扩展 B
			chineseChars++
		} else {
			otherChars++
		}
	}

	// 中文约 1.5 字符/token，英文约 4 字符/token
	chineseTokens := int(float64(chineseChars) / 1.5)
	otherTokens := (otherChars + 3) / 4

	return chineseTokens + otherTokens
}
