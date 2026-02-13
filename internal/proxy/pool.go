package proxy

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"net/url"
	"claude-api/internal/models"
	"strings"
	"sync"
	"sync/atomic"
)

// ProxyPool 代理池管理器
// @author ygw
type ProxyPool struct {
	proxies  []*models.Proxy
	mu       sync.RWMutex
	index    uint32
	strategy string
}

// NewProxyPool 创建代理池
// @param strategy 选择策略: round_robin, random, weighted
// @return *ProxyPool 代理池实例
// @author ygw
func NewProxyPool(strategy string) *ProxyPool {
	if strategy == "" {
		strategy = "round_robin"
	}
	return &ProxyPool{strategy: strategy}
}

// GetProxy 获取代理地址
// @param accountID 账号ID，用于 Session 派生
// @return string 派生后的代理地址，空字符串表示无可用代理
// @author ygw
func (p *ProxyPool) GetProxy(accountID string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 过滤启用的代理
	var enabled []*models.Proxy
	for _, proxy := range p.proxies {
		if proxy.Enabled {
			enabled = append(enabled, proxy)
		}
	}
	if len(enabled) == 0 {
		return ""
	}

	var selected *models.Proxy
	switch p.strategy {
	case "random":
		selected = enabled[rand.Intn(len(enabled))]
	case "weighted":
		selected = p.selectWeighted(enabled)
	default: // round_robin
		idx := atomic.AddUint32(&p.index, 1) - 1
		selected = enabled[idx%uint32(len(enabled))]
	}

	return DeriveProxyURL(selected.URL, accountID)
}

// selectWeighted 加权随机选择
// @param proxies 代理列表
// @return *models.Proxy 选中的代理
// @author ygw
func (p *ProxyPool) selectWeighted(proxies []*models.Proxy) *models.Proxy {
	totalWeight := 0
	for _, proxy := range proxies {
		totalWeight += proxy.Weight
	}
	if totalWeight == 0 {
		return proxies[0]
	}
	r := rand.Intn(totalWeight)
	for _, proxy := range proxies {
		r -= proxy.Weight
		if r < 0 {
			return proxy
		}
	}
	return proxies[0]
}

// Reload 重新加载代理列表
// @param proxies 代理列表
// @author ygw
func (p *ProxyPool) Reload(proxies []*models.Proxy) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.proxies = proxies
}

// SetStrategy 设置选择策略
// @param strategy 选择策略
// @author ygw
func (p *ProxyPool) SetStrategy(strategy string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.strategy = strategy
}

// Count 获取代理数量
// @return int 代理数量
// @author ygw
func (p *ProxyPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.proxies)
}

// EnabledCount 获取启用的代理数量
// @return int 启用的代理数量
// @author ygw
func (p *ProxyPool) EnabledCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, proxy := range p.proxies {
		if proxy.Enabled {
			count++
		}
	}
	return count
}

// DeriveProxyURL 派生代理地址
// 将代理 URL 中的 % 占位符替换为基于账号 ID 的 Hash 值
// @param proxyURL 原始代理地址（可能包含 % 占位符）
// @param accountID 账号ID
// @return string 派生后的代理地址
// @author ygw
func DeriveProxyURL(proxyURL, accountID string) string {
	if !strings.Contains(proxyURL, "%") {
		return proxyURL
	}
	h := fnv.New32a()
	h.Write([]byte(accountID))
	sessionID := uint32ToString(h.Sum32())
	return strings.ReplaceAll(proxyURL, "%", sessionID)
}

// uint32ToString 将 uint32 转换为字符串
// @param n uint32 数值
// @return string 字符串
// @author ygw
func uint32ToString(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// ValidateProxyURL 验证代理 URL 格式
// @param proxyURL 代理地址
// @return error 验证错误，nil 表示验证通过
// @author ygw
func ValidateProxyURL(proxyURL string) error {
	if proxyURL == "" {
		return fmt.Errorf("代理地址不能为空")
	}

	// 临时替换 % 占位符以便解析
	testURL := strings.ReplaceAll(proxyURL, "%", "session")
	
	parsed, err := url.Parse(testURL)
	if err != nil {
		return fmt.Errorf("代理地址格式错误: %v", err)
	}

	// 检查协议
	if parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "socks5" {
		return fmt.Errorf("不支持的代理协议: %s (仅支持 http/https/socks5)", parsed.Scheme)
	}

	// 检查主机名
	if parsed.Hostname() == "" {
		return fmt.Errorf("代理地址缺少主机名")
	}

	// 检查端口
	if parsed.Port() == "" {
		return fmt.Errorf("代理地址缺少端口")
	}

	return nil
}
