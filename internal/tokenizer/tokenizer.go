package tokenizer

import (
	"encoding/json"
)

// CountTokens 使用官方 Anthropic tokenizer 计算 token 数量
// 基于 github.com/qhenkart/anthropic-tokenizer-go 库，准确率约 88%+
func CountTokens(text string) int {
	return CountAnthropic(text)
}

// CountTokensForClaude 是 CountTokens 的别名，保持向后兼容
func CountTokensForClaude(text string) int {
	return CountTokens(text)
}

// CountMessageTokens counts tokens for Claude API messages including system prompts
func CountMessageTokens(messages []interface{}, systemPrompt string) int {
	total := 0

	// Count system prompt
	if systemPrompt != "" {
		total += CountTokens(systemPrompt)
	}

	// Count messages
	for _, msg := range messages {
		if msgMap, ok := msg.(map[string]interface{}); ok {
			// Count role (+ formatting overhead)
			if role, ok := msgMap["role"].(string); ok {
				total += CountTokens(role) + 4 // Role + formatting tokens
			}

			// Count content
			if content, ok := msgMap["content"].(string); ok {
				total += CountTokens(content)
			} else if contentList, ok := msgMap["content"].([]interface{}); ok {
				// Handle content blocks (for multi-modal messages)
				for _, block := range contentList {
					if blockMap, ok := block.(map[string]interface{}); ok {
						if blockType, ok := blockMap["type"].(string); ok && blockType == "text" {
							if text, ok := blockMap["text"].(string); ok {
								total += CountTokens(text)
							}
						}
					}
				}
			}
		}
	}

	return total
}

// CountToolTokens counts tokens for tool definitions
func CountToolTokens(tools []interface{}) int {
	if len(tools) == 0 {
		return 0
	}

	// Serialize tools to JSON and count tokens
	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return 0
	}

	return CountTokens(string(toolsJSON))
}
