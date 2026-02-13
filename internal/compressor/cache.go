package compressor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-api/internal/logger"
	"claude-api/internal/models"
)

// CacheIndexEntry 缓存索引条目（轻量级，只存储查询所需的元数据）
// @author ygw
type CacheIndexEntry struct {
	PrefixHash         string    // 消息前缀 hash
	TotalCompressedMsg int       // 已压缩的消息数
	BlockIDs           []string  // 摘要块 ID 列表（用于冗余检测）
	UpdatedAt          time.Time // 最后更新时间
	FileName           string    // 缓存文件名
}

// SummaryCache 摘要文件缓存（带内存索引）
// @author ygw
type SummaryCache struct {
	cacheDir string
	ttl      time.Duration
	mu       sync.RWMutex

	// 内存索引：PrefixHash -> 索引条目
	index map[string]*CacheIndexEntry

	// 按消息数量分组的索引（用于快速查找最佳匹配）
	// key 是 TotalCompressedMsg，value 是该数量对应的所有缓存条目
	indexByMsgCount map[int][]*CacheIndexEntry

	// 已排序的消息数量列表（降序，用于从大到小查找）
	sortedMsgCounts []int
}

// SummaryBlock 单个摘要块
// @author ygw
type SummaryBlock struct {
	BlockID     string    `json:"block_id"`       // 块唯一标识
	MsgStartIdx int       `json:"msg_start_idx"`  // 起始消息索引
	MsgEndIdx   int       `json:"msg_end_idx"`    // 结束消息索引（不含）
	MsgHash     string    `json:"msg_hash"`       // 该块消息的 hash
	Summary     string    `json:"summary"`        // 摘要内容
	TokenCount  int       `json:"token_count"`    // 摘要 token 数
	CreatedAt   time.Time `json:"created_at"`     // 创建时间
}

// ConversationCache 对话级压缩缓存（分块模式）
// 使用消息内容 hash 作为匹配依据，支持多摘要块
// @author ygw
type ConversationCache struct {
	PrefixHash         string         `json:"prefix_hash"`          // 所有已压缩消息的前缀 hash
	TotalCompressedMsg int            `json:"total_compressed_msg"` // 已压缩的总消息数
	SummaryBlocks      []SummaryBlock `json:"summary_blocks"`       // 摘要块列表（无数量限制）
	CreatedAt          time.Time      `json:"created_at"`           // 缓存创建时间
	UpdatedAt          time.Time      `json:"updated_at"`           // 最后更新时间
}

// NewSummaryCache 创建缓存实例
func NewSummaryCache(cacheDir string, ttl time.Duration) *SummaryCache {
	cache := &SummaryCache{
		cacheDir:        cacheDir,
		ttl:             ttl,
		index:           make(map[string]*CacheIndexEntry),
		indexByMsgCount: make(map[int][]*CacheIndexEntry),
		sortedMsgCounts: make([]int, 0),
	}

	// 确保缓存目录存在
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		logger.Error("[智能压缩] 创建缓存目录失败: %v", err)
	}

	// 启动时加载索引
	cache.loadIndex()

	// 启动清理协程
	go cache.cleanupLoop()

	return cache
}

// loadIndex 从磁盘加载所有缓存的索引信息到内存
// @author ygw
func (c *SummaryCache) loadIndex() {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.cacheDir)
	if err != nil {
		logger.Debug("[智能压缩] 读取缓存目录失败: %v", err)
		return
	}

	now := time.Now()
	loadedCount := 0
	expiredCount := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(c.cacheDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var cache ConversationCache
		if err := json.Unmarshal(data, &cache); err != nil {
			// 删除损坏的缓存文件
			os.Remove(filePath)
			continue
		}

		// 检查是否过期
		if now.Sub(cache.UpdatedAt) > c.ttl {
			os.Remove(filePath)
			expiredCount++
			continue
		}

		// 提取摘要块 ID 列表
		blockIDs := make([]string, len(cache.SummaryBlocks))
		for i, block := range cache.SummaryBlocks {
			blockIDs[i] = block.BlockID
		}

		// 添加到索引
		indexEntry := &CacheIndexEntry{
			PrefixHash:         cache.PrefixHash,
			TotalCompressedMsg: cache.TotalCompressedMsg,
			BlockIDs:           blockIDs,
			UpdatedAt:          cache.UpdatedAt,
			FileName:           entry.Name(),
		}
		c.addToIndexLocked(indexEntry)
		loadedCount++
	}

	if loadedCount > 0 || expiredCount > 0 {
		logger.Info("[智能压缩] 索引加载完成 - 有效: %d, 过期清理: %d", loadedCount, expiredCount)
	}
}

// addToIndexLocked 添加条目到索引（需要持有写锁）
// @author ygw
func (c *SummaryCache) addToIndexLocked(entry *CacheIndexEntry) {
	// 添加到主索引
	c.index[entry.PrefixHash] = entry

	// 添加到消息数量索引
	msgCount := entry.TotalCompressedMsg
	if _, exists := c.indexByMsgCount[msgCount]; !exists {
		c.indexByMsgCount[msgCount] = make([]*CacheIndexEntry, 0)
		// 更新排序列表
		c.sortedMsgCounts = append(c.sortedMsgCounts, msgCount)
		sort.Sort(sort.Reverse(sort.IntSlice(c.sortedMsgCounts)))
	}
	c.indexByMsgCount[msgCount] = append(c.indexByMsgCount[msgCount], entry)
}

// removeFromIndexLocked 从索引中移除条目（需要持有写锁）
// @author ygw
func (c *SummaryCache) removeFromIndexLocked(prefixHash string) {
	entry, exists := c.index[prefixHash]
	if !exists {
		return
	}

	// 从主索引移除
	delete(c.index, prefixHash)

	// 从消息数量索引移除
	msgCount := entry.TotalCompressedMsg
	if entries, ok := c.indexByMsgCount[msgCount]; ok {
		newEntries := make([]*CacheIndexEntry, 0, len(entries)-1)
		for _, e := range entries {
			if e.PrefixHash != prefixHash {
				newEntries = append(newEntries, e)
			}
		}
		if len(newEntries) == 0 {
			delete(c.indexByMsgCount, msgCount)
			// 更新排序列表
			newSorted := make([]int, 0, len(c.sortedMsgCounts)-1)
			for _, count := range c.sortedMsgCounts {
				if count != msgCount {
					newSorted = append(newSorted, count)
				}
			}
			c.sortedMsgCounts = newSorted
		} else {
			c.indexByMsgCount[msgCount] = newEntries
		}
	}
}

// GenerateMessagesHash 生成消息列表的 hash（用于匹配检测）
func (c *SummaryCache) GenerateMessagesHash(messages []models.ClaudeMessage) string {
	var content strings.Builder
	for _, msg := range messages {
		content.WriteString(msg.Role)
		content.WriteString(":")
		if s, ok := msg.Content.(string); ok {
			content.WriteString(s)
		} else {
			jsonBytes, _ := json.Marshal(msg.Content)
			content.Write(jsonBytes)
		}
		content.WriteString("|")
	}

	hash := sha256.Sum256([]byte(content.String()))
	return hex.EncodeToString(hash[:])
}

// GeneratePrefixHash 生成消息前缀的 hash
// 用于检测新请求的前 N 条消息是否与之前压缩的消息匹配
func (c *SummaryCache) GeneratePrefixHash(messages []models.ClaudeMessage, count int) string {
	if count > len(messages) {
		count = len(messages)
	}
	if count <= 0 {
		return ""
	}
	return c.GenerateMessagesHash(messages[:count])
}

// FindMatchingCache 查找匹配的缓存（使用内存索引优化）
// 从消息数量最多的开始查找，找到第一个匹配的就是最佳匹配
// 返回匹配的缓存和匹配的消息数量
// @author ygw
func (c *SummaryCache) FindMatchingCache(messages []models.ClaudeMessage) (*ConversationCache, int) {
	c.mu.RLock()

	// 如果没有索引，直接返回
	if len(c.index) == 0 {
		c.mu.RUnlock()
		logger.Debug("[智能压缩] 无缓存索引 - 消息数: %d", len(messages))
		return nil, 0
	}

	msgLen := len(messages)
	now := time.Now()

	// 缓存前缀 hash，避免重复计算
	prefixHashCache := make(map[int]string)

	// 按消息数量从大到小遍历（优先匹配更多消息的缓存）
	var matchedEntry *CacheIndexEntry
	for _, msgCount := range c.sortedMsgCounts {
		// 跳过消息数超过当前请求的
		if msgCount > msgLen {
			continue
		}

		// 计算该长度的前缀 hash（使用缓存避免重复计算）
		prefixHash, exists := prefixHashCache[msgCount]
		if !exists {
			prefixHash = c.GeneratePrefixHash(messages, msgCount)
			prefixHashCache[msgCount] = prefixHash
		}

		// 在该消息数量的索引中查找
		entries := c.indexByMsgCount[msgCount]
		for _, entry := range entries {
			// 检查是否过期
			if now.Sub(entry.UpdatedAt) > c.ttl {
				continue
			}

			// 检查 hash 是否匹配
			if entry.PrefixHash == prefixHash {
				matchedEntry = entry
				break
			}
		}

		// 找到匹配就停止（因为是从大到小遍历，第一个匹配的就是最佳的）
		if matchedEntry != nil {
			break
		}
	}

	// 释放读锁
	c.mu.RUnlock()

	if matchedEntry == nil {
		logger.Debug("[智能压缩] 无匹配缓存 - 消息数: %d, 索引条目: %d", msgLen, len(c.index))
		return nil, 0
	}

	// 从文件加载完整缓存数据
	cache, err := c.loadCacheFromFile(matchedEntry.FileName)
	if err != nil {
		logger.Error("[智能压缩] 加载缓存文件失败: %v", err)
		return nil, 0
	}

	logger.Debug("[智能压缩] 索引命中 - 匹配 %d 条消息", matchedEntry.TotalCompressedMsg)
	return cache, matchedEntry.TotalCompressedMsg
}

// loadCacheFromFile 从文件加载完整缓存数据
// @author ygw
func (c *SummaryCache) loadCacheFromFile(fileName string) (*ConversationCache, error) {
	filePath := filepath.Join(c.cacheDir, fileName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var cache ConversationCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

// SaveCache 保存缓存（使用 hash 作为文件名）
// 保存新缓存时会删除被包含的旧缓存文件，并更新内存索引
// @author ygw
func (c *SummaryCache) SaveCache(cache *ConversationCache) {
	if cache == nil || cache.PrefixHash == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if cache.CreatedAt.IsZero() {
		cache.CreatedAt = now
	}
	cache.UpdatedAt = now

	// 删除被当前缓存包含的旧缓存文件
	c.removeObsoleteCachesLocked(cache)

	data, err := json.Marshal(cache)
	if err != nil {
		logger.Error("[智能压缩] 序列化缓存失败: %v", err)
		return
	}

	// 确保缓存目录存在
	if err := os.MkdirAll(c.cacheDir, 0755); err != nil {
		logger.Error("[智能压缩] 创建缓存目录失败: %v", err)
		return
	}

	// 使用 hash 前缀作为文件名
	fileName := cache.PrefixHash[:32] + ".json"
	filePath := filepath.Join(c.cacheDir, fileName)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		logger.Error("[智能压缩] 写入缓存失败: %v", err)
		return
	}

	// 更新内存索引
	// 先移除旧的（如果存在）
	c.removeFromIndexLocked(cache.PrefixHash)

	// 提取摘要块 ID 列表
	blockIDs := make([]string, len(cache.SummaryBlocks))
	for i, block := range cache.SummaryBlocks {
		blockIDs[i] = block.BlockID
	}

	// 添加新的索引条目
	indexEntry := &CacheIndexEntry{
		PrefixHash:         cache.PrefixHash,
		TotalCompressedMsg: cache.TotalCompressedMsg,
		BlockIDs:           blockIDs,
		UpdatedAt:          cache.UpdatedAt,
		FileName:           fileName,
	}
	c.addToIndexLocked(indexEntry)

	logger.Debug("[智能压缩] 缓存已保存 - %d 条消息, %d 个摘要块, 索引条目: %d",
		cache.TotalCompressedMsg, len(cache.SummaryBlocks), len(c.index))
}

// removeObsoleteCachesLocked 删除被新缓存包含的旧缓存文件（需要持有锁）
// 如果新缓存的摘要块包含了旧缓存的所有摘要块，则删除旧缓存
// 优化：使用内存索引而非遍历文件
// @author ygw
func (c *SummaryCache) removeObsoleteCachesLocked(newCache *ConversationCache) {
	// 构建新缓存的摘要块 ID 集合
	newBlockIDs := make(map[string]bool)
	for _, block := range newCache.SummaryBlocks {
		newBlockIDs[block.BlockID] = true
	}

	removedCount := 0
	toRemove := make([]string, 0)

	// 遍历内存索引而非文件系统
	for prefixHash, entry := range c.index {
		// 跳过自己（hash 相同）
		if prefixHash == newCache.PrefixHash {
			continue
		}

		// 检查旧缓存的所有摘要块是否都被新缓存包含
		allContained := true
		for _, blockID := range entry.BlockIDs {
			if !newBlockIDs[blockID] {
				allContained = false
				break
			}
		}

		// 如果旧缓存被完全包含，标记删除
		if allContained && len(entry.BlockIDs) > 0 {
			toRemove = append(toRemove, prefixHash)
		}
	}

	// 执行删除
	for _, prefixHash := range toRemove {
		entry := c.index[prefixHash]
		if entry != nil {
			filePath := filepath.Join(c.cacheDir, entry.FileName)
			if err := os.Remove(filePath); err == nil {
				c.removeFromIndexLocked(prefixHash)
				removedCount++
			}
		}
	}

	if removedCount > 0 {
		logger.Debug("[智能压缩] 清理 %d 个冗余缓存", removedCount)
	}
}

// cleanupLoop 定期清理过期缓存
func (c *SummaryCache) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanup()
	}
}

// cleanup 清理过期缓存
// 优化：使用内存索引进行清理
// @author ygw
func (c *SummaryCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	toRemove := make([]string, 0)

	// 遍历内存索引查找过期条目
	for prefixHash, entry := range c.index {
		if now.Sub(entry.UpdatedAt) > c.ttl {
			toRemove = append(toRemove, prefixHash)
		}
	}

	// 执行删除
	cleanedCount := 0
	for _, prefixHash := range toRemove {
		entry := c.index[prefixHash]
		if entry != nil {
			filePath := filepath.Join(c.cacheDir, entry.FileName)
			if err := os.Remove(filePath); err == nil {
				c.removeFromIndexLocked(prefixHash)
				cleanedCount++
			}
		}
	}

	if cleanedCount > 0 {
		logger.Debug("[智能压缩] 清理 %d 个过期缓存, 剩余索引: %d", cleanedCount, len(c.index))
	}
}

// GetIndexStats 获取索引统计信息（用于调试）
// @author ygw
func (c *SummaryCache) GetIndexStats() (totalEntries int, msgCountGroups int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.index), len(c.indexByMsgCount)
}
