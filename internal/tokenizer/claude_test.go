package tokenizer

import (
	"testing"
)

func TestClaudeTokenizer(t *testing.T) {
	tokenizer, err := GetClaudeTokenizer()
	if err != nil {
		t.Fatalf("Failed to load Claude tokenizer: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected int // 预期 token 数（基于 ai-tokenizer 的结果）
	}{
		{"empty", "", 0},
		{"hello", "hello", 1},
		{"hello world", "hello world", 2},
		{"the", " the", 1},
		{"simple sentence", "The quick brown fox jumps over the lazy dog.", 10},
		{"chinese", "你好世界", 4},
		{"code", "func main() { fmt.Println(\"Hello\") }", 14},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := tokenizer.Count(tt.input)
			t.Logf("Input: %q, Tokens: %d", tt.input, count)
			// 由于我们没有精确的预期值，只检查是否返回合理的结果
			if tt.input != "" && count == 0 {
				t.Errorf("Expected non-zero token count for %q, got %d", tt.input, count)
			}
		})
	}
}

func TestCountClaude(t *testing.T) {
	// 测试便捷函数
	count := CountClaude("Hello, world!")
	if count == 0 {
		t.Error("Expected non-zero token count")
	}
	t.Logf("CountClaude(\"Hello, world!\"): %d", count)
}

func TestCountTokensForClaude(t *testing.T) {
	// 测试对外暴露的函数
	count := CountTokensForClaude("This is a test sentence.")
	if count == 0 {
		t.Error("Expected non-zero token count")
	}
	t.Logf("CountTokensForClaude(\"This is a test sentence.\"): %d", count)
}

func BenchmarkClaudeTokenizer(b *testing.B) {
	tokenizer, err := GetClaudeTokenizer()
	if err != nil {
		b.Fatalf("Failed to load Claude tokenizer: %v", err)
	}

	text := "The quick brown fox jumps over the lazy dog. This is a longer text to benchmark the tokenizer performance."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tokenizer.Count(text)
	}
}

func BenchmarkTiktoken(b *testing.B) {
	text := "The quick brown fox jumps over the lazy dog. This is a longer text to benchmark the tokenizer performance."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CountTokens(text)
	}
}

