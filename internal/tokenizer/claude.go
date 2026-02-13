package tokenizer

import (
	"embed"
	"encoding/json"
	"sync"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

//go:embed claude_vocab.json
var claudeVocabFS embed.FS

// ClaudeTokenizer 实现基于 ai-tokenizer 的 Claude 专用分词器
type ClaudeTokenizer struct {
	encoder  map[string]int
	decoder  map[int]string
	pattern  *regexp2.Regexp
	initOnce sync.Once
	initErr  error
}

type claudeVocab struct {
	Name    string         `json:"name"`
	PatStr  string         `json:"pat_str"`
	Encoder map[string]int `json:"encoder"`
}

var (
	claudeTokenizer     *ClaudeTokenizer
	claudeTokenizerOnce sync.Once
)

// GetClaudeTokenizer 返回单例的 Claude tokenizer
func GetClaudeTokenizer() (*ClaudeTokenizer, error) {
	claudeTokenizerOnce.Do(func() {
		claudeTokenizer = &ClaudeTokenizer{}
		claudeTokenizer.initOnce.Do(func() {
			claudeTokenizer.initErr = claudeTokenizer.load()
		})
	})
	if claudeTokenizer.initErr != nil {
		return nil, claudeTokenizer.initErr
	}
	return claudeTokenizer, nil
}

func (t *ClaudeTokenizer) load() error {
	data, err := claudeVocabFS.ReadFile("claude_vocab.json")
	if err != nil {
		return err
	}

	var vocab claudeVocab
	if err := json.Unmarshal(data, &vocab); err != nil {
		return err
	}

	t.encoder = vocab.Encoder
	t.decoder = make(map[int]string, len(vocab.Encoder))
	for k, v := range vocab.Encoder {
		t.decoder[v] = k
	}

	pattern := `'s|'t|'re|'ve|'m|'ll|'d| ?\p{L}+| ?\p{N}+| ?[^\s\p{L}\p{N}]+|\s+(?!\S)|\s+`
	t.pattern, err = regexp2.Compile(pattern, regexp2.Unicode)
	return err
}

// Count 计算文本的 token 数量
func (t *ClaudeTokenizer) Count(text string) int {
	if text == "" {
		return 0
	}

	tokens := t.tokenize(text)
	return len(tokens)
}

// tokenize 将文本分割成 tokens
func (t *ClaudeTokenizer) tokenize(text string) []int {
	if t.pattern == nil {
		return nil
	}

	var tokens []int
	match, _ := t.pattern.FindStringMatch(text)
	for match != nil {
		token := match.String()
		if id, ok := t.encoder[token]; ok {
			tokens = append(tokens, id)
		} else {
			// 未知 token，按字节/字符分割
			tokens = append(tokens, t.encodeUnknown(token)...)
		}
		match, _ = t.pattern.FindNextMatch(match)
	}
	return tokens
}

// encodeUnknown 处理词表中不存在的 token
func (t *ClaudeTokenizer) encodeUnknown(token string) []int {
	var tokens []int
	for _, r := range token {
		s := string(r)
		if id, ok := t.encoder[s]; ok {
			tokens = append(tokens, id)
		} else {
			// 按 UTF-8 字节编码
			for _, b := range []byte(s) {
				if id, ok := t.encoder[string(b)]; ok {
					tokens = append(tokens, id)
				} else {
					tokens = append(tokens, int(b))
				}
			}
		}
	}
	if len(tokens) == 0 {
		return []int{utf8.RuneCountInString(token)}
	}
	return tokens
}

// CountClaude 使用 Claude tokenizer 计算 token 数量
// 如果 Claude tokenizer 加载失败，使用简单估算（每4字符约1个token）
func CountClaude(text string) int {
	if text == "" {
		return 0
	}
	t, err := GetClaudeTokenizer()
	if err != nil {
		// 简单估算：英文约4字符/token，中文约1.5字符/token
		// 使用保守估算：每3个字符约1个token
		return (len(text) + 2) / 3
	}
	return t.Count(text)
}
