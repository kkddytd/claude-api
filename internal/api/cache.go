package api

import (
	"context"
	"math/rand"
	"claude-api/internal/database"
	"claude-api/internal/logger"
	"claude-api/internal/models"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// AccountPool 账号池，用于缓存启用的账号列表
// 避免每次请求都查询数据库，显著提升高并发性能
// @author ygw
type AccountPool struct {
	accounts        []*models.Account // 缓存的账号列表
	mu              sync.RWMutex      // 读写锁
	lastRefresh     time.Time         // 上次刷新时间
	refreshInterval time.Duration     // 刷新间隔
	db              *database.DB      // 数据库连接
	cfg             accountPoolConfig // 配置
	refreshing      atomic.Bool       // 是否正在刷新
	roundRobinIndex uint32            // 轮询索引（用于 round_robin 模式）
}

// accountPoolConfig 账号池配置
type accountPoolConfig struct {
	lazyEnabled   bool   // 是否启用懒加载模式
	lazyPoolSize  int    // 懒加载池大小
	lazyOrderBy   string // 懒加载排序字段
	lazyOrderDesc bool   // 懒加载是否降序
	selectionMode string // 账号选择方式: sequential, random, weighted_random, round_robin
}

// NewAccountPool 创建新的账号池
func NewAccountPool(db *database.DB, refreshInterval time.Duration) *AccountPool {
	if refreshInterval <= 0 {
		refreshInterval = 10 * time.Second // 默认 10 秒刷新
	}
	return &AccountPool{
		accounts:        make([]*models.Account, 0),
		refreshInterval: refreshInterval,
		db:              db,
	}
}

// SetConfig 设置配置
func (p *AccountPool) SetConfig(lazyEnabled bool, lazyPoolSize int, lazyOrderBy string, lazyOrderDesc bool, selectionMode string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// 验证选择模式，无效则使用默认值
	if selectionMode == "" {
		selectionMode = models.AccountSelectionSequential
	}
	p.cfg = accountPoolConfig{
		lazyEnabled:   lazyEnabled,
		lazyPoolSize:  lazyPoolSize,
		lazyOrderBy:   lazyOrderBy,
		lazyOrderDesc: lazyOrderDesc,
		selectionMode: selectionMode,
	}
}

// Start 启动后台刷新任务
func (p *AccountPool) Start(ctx context.Context) {
	// 立即刷新一次
	p.Refresh(ctx)

	go func() {
		ticker := time.NewTicker(p.refreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.Refresh(ctx)
			}
		}
	}()
}

// Refresh 刷新账号缓存
func (p *AccountPool) Refresh(ctx context.Context) {
	startTime := time.Now()
	
	// 防止并发刷新
	if !p.refreshing.CompareAndSwap(false, true) {
		return
	}
	defer p.refreshing.Store(false)

	// 从数据库配置更新最新配置（支持动态更新）
	dbCfg := p.db.GetConfig()
	p.mu.Lock()
	p.cfg.selectionMode = dbCfg.AccountSelectionMode
	if p.cfg.selectionMode == "" {
		p.cfg.selectionMode = models.AccountSelectionSequential
	}
	p.cfg.lazyEnabled = dbCfg.LazyAccountPoolEnabled
	p.cfg.lazyPoolSize = dbCfg.LazyAccountPoolSize
	p.cfg.lazyOrderBy = dbCfg.LazyAccountPoolOrderBy
	p.cfg.lazyOrderDesc = dbCfg.LazyAccountPoolOrderDesc
	cfg := p.cfg
	p.mu.Unlock()

	var accounts []*models.Account
	var err error

	// 只获取状态为 normal 的账号（优化：封控/用尽的账号不再进入池）
	if cfg.lazyEnabled {
		accounts, err = p.db.ListAccountsByStatus(ctx, models.AccountStatusNormal, cfg.lazyOrderBy, cfg.lazyOrderDesc)
		if err == nil && cfg.lazyPoolSize > 0 && len(accounts) > cfg.lazyPoolSize {
			accounts = accounts[:cfg.lazyPoolSize]
		}
	} else {
		accounts, err = p.db.ListAccountsByStatus(ctx, models.AccountStatusNormal, "created_at", true)
	}

	if err != nil {
		elapsed := time.Since(startTime)
		logger.Error("[账号池] 刷新失败 - 耗时: %.0fms, 错误: %v", elapsed.Seconds()*1000, err)
		return
	}

	p.mu.Lock()
	p.accounts = accounts
	p.lastRefresh = time.Now()
	p.mu.Unlock()

	elapsed := time.Since(startTime)
	logger.Debug("[账号池] 刷新完成 - 账号数: %d, 选择方式: %s, 耗时: %.0fms", len(accounts), cfg.selectionMode, elapsed.Seconds()*1000)
}

// GetAccount 获取一个账号（根据配置选择方式）
func (p *AccountPool) GetAccount() *models.Account {
	p.mu.RLock()
	accounts := p.accounts
	mode := p.cfg.selectionMode
	p.mu.RUnlock()

	if len(accounts) == 0 {
		return nil
	}

	return p.selectAccount(accounts, mode)
}

// selectAccount 根据选择模式选择账号
func (p *AccountPool) selectAccount(accounts []*models.Account, mode string) *models.Account {
	if len(accounts) == 0 {
		return nil
	}

	switch mode {
	case models.AccountSelectionRandom:
		// 随机选择
		return accounts[rand.Intn(len(accounts))]

	case models.AccountSelectionWeightedRandom:
		// 加权随机选择
		return p.selectWeightedRandom(accounts)

	case models.AccountSelectionRoundRobin:
		// 轮询选择
		idx := atomic.AddUint32(&p.roundRobinIndex, 1) - 1
		return accounts[idx%uint32(len(accounts))]

	default: // sequential 或其他
		// 顺序选择（返回第一个）
		return accounts[0]
	}
}

// selectWeightedRandom 加权随机选择账号
// 权重计算因素：
// 1. 配额剩余比例（权重最高）
// 2. 最近使用时间（越久未使用权重越高）
// 3. 成功率（成功次数越多权重越高）
func (p *AccountPool) selectWeightedRandom(accounts []*models.Account) *models.Account {
	if len(accounts) == 0 {
		return nil
	}
	if len(accounts) == 1 {
		return accounts[0]
	}

	type weightedAccount struct {
		account *models.Account
		weight  float64
	}

	weightedAccounts := make([]weightedAccount, 0, len(accounts))
	now := time.Now()

	for _, acc := range accounts {
		weight := 1.0

		// 1. 配额剩余权重（配额剩余越多，权重越高）
		if acc.UsageLimit > 0 {
			remaining := acc.UsageLimit - acc.UsageCurrent
			if remaining < 0 {
				remaining = 0
			}
			quotaRatio := remaining / acc.UsageLimit
			// 配额剩余比例 * 10 作为权重（最高加10分）
			weight += quotaRatio * 10
		} else {
			// 没有配额信息的账号给予中等权重
			weight += 5
		}

		// 2. 最近使用时间权重（越久未使用，权重越高）
		if acc.LastRefreshTime != nil && *acc.LastRefreshTime != "" {
			lastTime, err := time.Parse(models.TimeFormat, *acc.LastRefreshTime)
			if err == nil {
				hoursSinceUse := now.Sub(lastTime).Hours()
				// 每小时未使用增加 0.5 权重，最高加 5 分
				timeWeight := hoursSinceUse * 0.5
				if timeWeight > 5 {
					timeWeight = 5
				}
				weight += timeWeight
			}
		} else {
			// 从未使用过的账号给予较高权重
			weight += 3
		}

		// 3. 成功率权重（成功次数越多，说明账号越稳定）
		totalRequests := acc.SuccessCount + acc.ErrorCount
		if totalRequests > 0 {
			successRate := float64(acc.SuccessCount) / float64(totalRequests)
			// 成功率 * 3 作为权重（最高加3分）
			weight += successRate * 3
		} else {
			// 新账号给予中等权重
			weight += 1.5
		}

		weightedAccounts = append(weightedAccounts, weightedAccount{
			account: acc,
			weight:  weight,
		})
	}

	// 计算总权重
	totalWeight := 0.0
	for _, wa := range weightedAccounts {
		totalWeight += wa.weight
	}

	if totalWeight == 0 {
		return accounts[0]
	}

	// 加权随机选择
	r := rand.Float64() * totalWeight
	for _, wa := range weightedAccounts {
		r -= wa.weight
		if r <= 0 {
			return wa.account
		}
	}

	return accounts[0]
}

// GetAccountExcluding 获取一个账号，排除指定的账号 ID
func (p *AccountPool) GetAccountExcluding(excludeIDs []string) *models.Account {
	p.mu.RLock()
	accounts := p.accounts
	mode := p.cfg.selectionMode
	p.mu.RUnlock()

	if len(accounts) == 0 {
		return nil
	}

	// 构建排除集合
	excludeSet := make(map[string]bool)
	for _, id := range excludeIDs {
		excludeSet[id] = true
	}

	// 过滤可用账号
	var available []*models.Account
	for _, acc := range accounts {
		if !excludeSet[acc.ID] {
			available = append(available, acc)
		}
	}

	if len(available) == 0 {
		return nil
	}

	return p.selectAccount(available, mode)
}

// GetSelectionMode 获取当前选择模式（用于日志等）
func (p *AccountPool) GetSelectionMode() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cfg.selectionMode
}

// SortAccountsByWeight 按权重排序账号（用于调试和展示）
func (p *AccountPool) SortAccountsByWeight(accounts []*models.Account) []*models.Account {
	if len(accounts) <= 1 {
		return accounts
	}

	type weightedAccount struct {
		account *models.Account
		weight  float64
	}

	now := time.Now()
	weightedAccounts := make([]weightedAccount, 0, len(accounts))

	for _, acc := range accounts {
		weight := 1.0

		// 配额剩余权重
		if acc.UsageLimit > 0 {
			remaining := acc.UsageLimit - acc.UsageCurrent
			if remaining < 0 {
				remaining = 0
			}
			weight += (remaining / acc.UsageLimit) * 10
		} else {
			weight += 5
		}

		// 最近使用时间权重
		if acc.LastRefreshTime != nil && *acc.LastRefreshTime != "" {
			lastTime, err := time.Parse(models.TimeFormat, *acc.LastRefreshTime)
			if err == nil {
				hoursSinceUse := now.Sub(lastTime).Hours()
				timeWeight := hoursSinceUse * 0.5
				if timeWeight > 5 {
					timeWeight = 5
				}
				weight += timeWeight
			}
		} else {
			weight += 3
		}

		// 成功率权重
		totalRequests := acc.SuccessCount + acc.ErrorCount
		if totalRequests > 0 {
			successRate := float64(acc.SuccessCount) / float64(totalRequests)
			weight += successRate * 3
		} else {
			weight += 1.5
		}

		weightedAccounts = append(weightedAccounts, weightedAccount{
			account: acc,
			weight:  weight,
		})
	}

	// 按权重降序排序
	sort.Slice(weightedAccounts, func(i, j int) bool {
		return weightedAccounts[i].weight > weightedAccounts[j].weight
	})

	result := make([]*models.Account, len(weightedAccounts))
	for i, wa := range weightedAccounts {
		result[i] = wa.account
	}
	return result
}

// Count 返回缓存的账号数量
func (p *AccountPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.accounts)
}

// GetAll 获取所有缓存的账号（用于兼容需要完整列表的场景）
func (p *AccountPool) GetAll() []*models.Account {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 返回副本，避免外部修改
	result := make([]*models.Account, len(p.accounts))
	copy(result, p.accounts)
	return result
}

// IsStale 检查缓存是否过期
func (p *AccountPool) IsStale() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return time.Since(p.lastRefresh) > p.refreshInterval*2
}

// Invalidate 使缓存失效（用于账号变更时立即刷新）
func (p *AccountPool) Invalidate(ctx context.Context) {
	p.Refresh(ctx)
}

// SettingsCache 系统设置缓存
// @author ygw
type SettingsCache struct {
	settings    *models.Settings
	mu          sync.RWMutex
	lastRefresh time.Time
	ttl         time.Duration
	db          *database.DB
	refreshing  atomic.Bool
}

// NewSettingsCache 创建设置缓存
func NewSettingsCache(db *database.DB, ttl time.Duration) *SettingsCache {
	if ttl <= 0 {
		ttl = 30 * time.Second // 默认 30 秒
	}
	return &SettingsCache{
		db:  db,
		ttl: ttl,
	}
}

// Get 获取设置（自动刷新过期缓存）
func (c *SettingsCache) Get(ctx context.Context) (*models.Settings, error) {
	c.mu.RLock()
	if c.settings != nil && time.Since(c.lastRefresh) < c.ttl {
		settings := c.settings
		c.mu.RUnlock()
		return settings, nil
	}
	c.mu.RUnlock()

	// 需要刷新
	return c.refresh(ctx)
}

// refresh 刷新缓存
func (c *SettingsCache) refresh(ctx context.Context) (*models.Settings, error) {
	// 防止并发刷新
	if !c.refreshing.CompareAndSwap(false, true) {
		// 等待其他刷新完成
		c.mu.RLock()
		defer c.mu.RUnlock()
		if c.settings != nil {
			return c.settings, nil
		}
		return nil, nil
	}
	defer c.refreshing.Store(false)

	settings, err := c.db.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.settings = settings
	c.lastRefresh = time.Now()
	c.mu.Unlock()

	logger.Debug("设置缓存已刷新")
	return settings, nil
}

// Invalidate 使缓存失效
func (c *SettingsCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings = nil
	c.lastRefresh = time.Time{}
}

// Start 启动后台刷新任务
func (c *SettingsCache) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(c.ttl)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.refresh(ctx)
			}
		}
	}()
}

// TokenRefresher 令牌刷新器，使用 singleflight 模式避免重复刷新
// @author ygw - 高并发优化
type TokenRefresher struct {
	refreshing sync.Map // accountID -> *refreshState
}

// refreshState 刷新状态
type refreshState struct {
	mu       sync.Mutex
	done     bool
	err      error
	waiters  int       // 等待者数量
	doneTime time.Time // 完成时间
}

// NewTokenRefresher 创建令牌刷新器
func NewTokenRefresher() *TokenRefresher {
	return &TokenRefresher{}
}

// RefreshResult 刷新结果
type RefreshResult struct {
	Err      error
	Skipped  bool // 是否跳过（其他 goroutine 正在刷新）
	WasFirst bool // 是否是第一个发起刷新的
}

// TryRefresh 尝试刷新令牌
// doRefresh 是实际执行刷新的函数
// 返回: 刷新结果
// 如果同一账号正在被刷新，当前调用者会等待刷新完成并获得结果
func (r *TokenRefresher) TryRefresh(accountID string, doRefresh func() error) RefreshResult {
	// 加载或创建刷新状态
	stateI, loaded := r.refreshing.LoadOrStore(accountID, &refreshState{})
	state := stateI.(*refreshState)

	state.mu.Lock()

	// 检查是否刚刚完成刷新（5秒内不重复刷新）
	if state.done && time.Since(state.doneTime) < 5*time.Second {
		state.mu.Unlock()
		return RefreshResult{Err: state.err, Skipped: true, WasFirst: false}
	}

	// 判断是否需要执行刷新
	// 情况1: loaded=false（新创建）或 done=true（上次已完成，需要重新刷新）
	// 情况2: loaded=true 且 done=false（有人正在刷新，需要等待）
	if !loaded || state.done {
		// 我们来执行刷新（首次或重新刷新）
		state.done = false
		state.mu.Unlock()

		err := doRefresh()

		state.mu.Lock()
		state.done = true
		state.err = err
		state.doneTime = time.Now()
		state.mu.Unlock()

		return RefreshResult{Err: err, Skipped: false, WasFirst: true}
	}

	// 状态已存在且有人正在刷新，等待完成
	if !state.done {
		state.waiters++
		state.mu.Unlock()

		// 简单等待，最多等待 30 秒
		for i := 0; i < 300; i++ {
			time.Sleep(100 * time.Millisecond)
			state.mu.Lock()
			if state.done {
				state.waiters--
				err := state.err
				state.mu.Unlock()
				return RefreshResult{Err: err, Skipped: true, WasFirst: false}
			}
			state.mu.Unlock()
		}

		// 超时
		state.mu.Lock()
		state.waiters--
		state.mu.Unlock()
		return RefreshResult{Err: nil, Skipped: true, WasFirst: false}
	}

	state.mu.Unlock()
	return RefreshResult{Err: state.err, Skipped: true, WasFirst: false}
}

// ClearState 清除账号的刷新状态（用于账号被禁用等情况）
func (r *TokenRefresher) ClearState(accountID string) {
	r.refreshing.Delete(accountID)
}

// Cleanup 清理过期的刷新状态（超过 1 分钟）
func (r *TokenRefresher) Cleanup() {
	r.refreshing.Range(func(key, value interface{}) bool {
		state := value.(*refreshState)
		state.mu.Lock()
		defer state.mu.Unlock()

		if state.done && state.waiters == 0 && time.Since(state.doneTime) > time.Minute {
			r.refreshing.Delete(key)
		}
		return true
	})
}

// IPConfigCache IP配置缓存
// 用于缓存单个IP的频率限制配置，避免每次请求都查询数据库
// @author ygw
type IPConfigCache struct {
	cache sync.Map // key: IP (string), value: *ipConfigCacheEntry
	db    *database.DB
	ttl   time.Duration
}

// ipConfigCacheEntry IP配置缓存条目
type ipConfigCacheEntry struct {
	config   *models.IPConfig
	expireAt time.Time
}

// NewIPConfigCache 创建IP配置缓存
func NewIPConfigCache(db *database.DB, ttl time.Duration) *IPConfigCache {
	if ttl <= 0 {
		ttl = 60 * time.Second // 默认 60 秒
	}
	return &IPConfigCache{
		db:  db,
		ttl: ttl,
	}
}

// Get 获取IP配置（自动缓存）
// 返回值: config (可能为nil表示无配置), error
func (c *IPConfigCache) Get(ctx context.Context, ip string) (*models.IPConfig, error) {
	// 检查缓存
	if entry, ok := c.cache.Load(ip); ok {
		e := entry.(*ipConfigCacheEntry)
		if time.Now().Before(e.expireAt) {
			return e.config, nil
		}
	}

	// 从数据库加载
	config, err := c.db.GetIPConfig(ctx, ip)
	if err != nil {
		return nil, err
	}

	// 存入缓存
	c.cache.Store(ip, &ipConfigCacheEntry{
		config:   config,
		expireAt: time.Now().Add(c.ttl),
	})

	return config, nil
}

// Invalidate 使指定IP的缓存失效
func (c *IPConfigCache) Invalidate(ip string) {
	c.cache.Delete(ip)
}

// InvalidateAll 清空所有缓存
func (c *IPConfigCache) InvalidateAll() {
	c.cache.Range(func(key, value interface{}) bool {
		c.cache.Delete(key)
		return true
	})
}

// Cleanup 清理过期的缓存条目
func (c *IPConfigCache) Cleanup() {
	now := time.Now()
	c.cache.Range(func(key, value interface{}) bool {
		e := value.(*ipConfigCacheEntry)
		if now.After(e.expireAt) {
			c.cache.Delete(key)
		}
		return true
	})
}

// Start 启动后台清理任务
func (c *IPConfigCache) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute) // 每5分钟清理一次
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.Cleanup()
			}
		}
	}()
}
