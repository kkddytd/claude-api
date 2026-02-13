// ==================== API 请求函数 ====================

/**
 * 获取存储的密码
 */
export function getStoredPassword() {
    return localStorage.getItem('adminPassword');
}

/**
 * 获取认证头
 */
export function getAuthHeaders() {
    const password = getStoredPassword();
    return password ? { 'Authorization': `Bearer ${password}` } : {};
}

/**
 * 带认证的fetch请求
 */
export async function authenticatedFetch(url, options = {}) {
    const headers = { ...getAuthHeaders(), ...options.headers };
    const response = await fetch(url, { ...options, headers });

    if (response.status === 401) {
        localStorage.removeItem('adminPassword');
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }

    return response;
}

/**
 * 获取账号列表（支持分页）
 * @param {Object} options - 分页选项
 * @param {number} options.page - 页码（从1开始，默认1）
 * @param {number} options.pageSize - 每页数量（默认100）
 * @param {string} options.orderBy - 排序字段（默认created_at）
 * @param {boolean} options.orderDesc - 是否降序（默认true）
 * @param {boolean|null} options.enabled - 启用状态过滤（null表示不过滤）
 * @returns {Promise<{accounts: Array, pagination: Object}>}
 * @author ygw
 */
export async function fetchAccounts(options = {}) {
    const {
        page = 1,
        pageSize = 100,
        orderBy = 'created_at',
        orderDesc = true,
        enabled = null,
        status = 'all' // 状态筛选 @author ygw
    } = options;

    const params = new URLSearchParams();
    params.append('page', page);
    params.append('pageSize', pageSize);
    params.append('orderBy', orderBy);
    params.append('orderDesc', orderDesc);
    if (enabled !== null) {
        params.append('enabled', enabled);
    }
    // 状态筛选参数 @author ygw
    if (status && status !== 'all') {
        params.append('status', status);
    }

    const response = await authenticatedFetch(`/v2/accounts?${params}`);
    const data = await response.json();
    return {
        accounts: data.accounts || [],
        pagination: data.pagination || { total: 0, page: 1, pageSize: 100, pages: 1 },
        quotaStats: data.quotaStats || { totalUsage: 0, totalLimit: 0, count: 0, percent: 0 },
        accountStats: data.accountStats || { totalCount: 0, enabledCount: 0, successTotal: 0, errorTotal: 0 },
        statusStats: data.statusStats || { normal: 0, disabled: 0, suspended: 0, exhausted: 0, expired: 0 } // 状态统计 @author ygw
    };
}

/**
 * 导出账号完整信息
 * @param {string|null} testPassword - 测试模式密码（可选）
 */
export async function exportAccounts(testPassword = null) {
    const headers = { ...getAuthHeaders() };
    if (testPassword) {
        headers['X-Test-Password'] = testPassword;
    }
    const response = await fetch('/v2/accounts/export', { headers });
    if (response.status === 401) {
        localStorage.removeItem('adminPassword');
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }
    if (!response.ok) throw new Error(await response.text());
    return response;
}

/**
 * 导入账号（后端处理验证和去重）
 */
export async function importAccounts(accounts) {
    const response = await authenticatedFetch('/v2/accounts/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(accounts)
    });
    if (!response.ok) throw new Error(await response.text());
    return await response.json();
}

/**
 * 通过 RefreshToken 导入账号
 * 支持两种格式:
 * 1. 仅 refreshToken: [{refreshToken: "xxx"}]
 * 2. 完整格式: [{authMethod: "IdC", clientId: "xxx", clientSecret: "xxx", email: "xxx", refreshToken: "xxx", region: "xxx"}]
 * 自动获取账号信息（邮箱、用户ID等），并保存到 accounts 表和 imported_accounts 备份表
 * @param {Array} tokens - 包含账号信息的对象数组
 * @returns {Promise<Object>} 导入结果
 */
export async function importAccountsByToken(tokens) {
    const response = await authenticatedFetch('/v2/accounts/import-by-token', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(tokens)
    });
    if (!response.ok) throw new Error(await response.text());
    return await response.json();
}

/**
 * 同步所有账号的邮箱
 * 通过 API 获取缺少邮箱的账号的邮箱，并更新 label 为邮箱
 * @returns {Promise<Object>} 同步结果
 */
export async function syncAccountEmails() {
    const response = await authenticatedFetch('/v2/accounts/sync-emails', {
        method: 'POST'
    });
    if (!response.ok) throw new Error(await response.text());
    return await response.json();
}

/**
 * 更新账号
 */
export async function updateAccount(accountId, patchData) {
    const response = await authenticatedFetch(
        `/v2/accounts/${encodeURIComponent(accountId)}`,
        {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(patchData)
        }
    );
    if (!response.ok) throw new Error(await response.text());
    return response;
}

/**
 * 删除账号
 * @param {string} accountId - 账号ID
 * @param {string|null} testPassword - 测试模式密码（可选）
 */
export async function deleteAccount(accountId, testPassword = null) {
    const headers = { ...getAuthHeaders() };
    if (testPassword) {
        headers['X-Test-Password'] = testPassword;
    }
    const response = await fetch(
        `/v2/accounts/${encodeURIComponent(accountId)}`,
        { method: 'DELETE', headers }
    );
    if (response.status === 401) {
        localStorage.removeItem('adminPassword');
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }
    if (!response.ok) throw new Error(await response.text());
    return response;
}

/**
 * 刷新账号Token
 */
export async function refreshAccountToken(accountId) {
    const response = await authenticatedFetch(
        `/v2/accounts/${encodeURIComponent(accountId)}/refresh`,
        { method: 'POST' }
    );
    if (!response.ok) throw new Error(await response.text());
    return response;
}

/**
 * 批量刷新所有账号Token（后台异步）
 */
export async function refreshAllAccounts() {
    const response = await authenticatedFetch('/v2/accounts/refresh-all', { method: 'POST' });
    if (!response.ok) throw new Error(await response.text());
    return response.json();
}

/**
 * 刷新所有账号配额（手动触发）
 * 由于改为被动刷新策略，配额刷新需要用户手动触发
 * @author ygw - 被动刷新策略
 */
export async function refreshAllQuotas() {
    const response = await authenticatedFetch('/v2/accounts/refresh-quotas', { method: 'POST' });
    if (!response.ok) throw new Error(await response.text());
    return response.json();
}

/**
 * 获取信息不全的账号列表
 */
export async function fetchIncompleteAccounts() {
    const response = await authenticatedFetch('/v2/accounts/incomplete');
    const data = await response.json();
    return data;
}

/**
 * 删除信息不全的账号
 */
export async function deleteIncompleteAccounts() {
    const response = await authenticatedFetch('/v2/accounts/incomplete', {
        method: 'DELETE'
    });
    if (!response.ok) throw new Error(await response.text());
    const data = await response.json();
    return data;
}

/**
 * 删除所有封控状态的账号
 * @param {string|null} testPassword - 测试模式密码（可选）
 * @returns {Promise<{success: boolean, count: number, message: string}>}
 * @author ygw
 */
export async function deleteSuspendedAccounts(testPassword = null) {
    const headers = { ...getAuthHeaders() };
    if (testPassword) {
        headers['X-Test-Password'] = testPassword;
    }
    const response = await fetch('/v2/accounts/suspended', {
        method: 'DELETE',
        headers
    });
    if (response.status === 401) {
        localStorage.removeItem('adminPassword');
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }
    if (!response.ok) throw new Error(await response.text());
    return await response.json();
}

/**
 * 创建账号
 */
export async function createAccount(accountData) {
    const response = await authenticatedFetch('/v2/accounts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(accountData)
    });
    return response;
}

/**
 * 获取设置
 */
export async function fetchSettings() {
    const response = await authenticatedFetch('/v2/settings');
    if (!response.ok) return null;
    const text = await response.text();
    if (!text || text.trim() === '') return null;
    return JSON.parse(text);
}

/**
 * 获取可用模型列表
 * @author ygw
 */
export async function fetchModels() {
    const response = await authenticatedFetch('/v2/models');
    if (!response.ok) return null;
    const data = await response.json();
    return data.models || [];
}

/**
 * 保存设置
 * @param {Object} settingsData - 设置数据
 * @param {string|null} testPassword - 测试模式密码（可选）
 */
export async function saveSettings(settingsData, testPassword = null) {
    const headers = { ...getAuthHeaders(), 'Content-Type': 'application/json' };
    if (testPassword) {
        headers['X-Test-Password'] = testPassword;
    }
    const response = await fetch('/v2/settings', {
        method: 'PUT',
        headers,
        body: JSON.stringify(settingsData)
    });
    if (response.status === 401) {
        localStorage.removeItem('adminPassword');
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }
    if (!response.ok) throw new Error(await response.text());
    return response;
}

/**
 * 导出备份
 * @param {string|null} testPassword - 测试模式密码（可选）
 */
export async function exportBackup(testPassword = null) {
    const headers = { ...getAuthHeaders() };
    if (testPassword) {
        headers['X-Test-Password'] = testPassword;
    }
    const response = await fetch('/v2/backup/export', { headers });
    if (response.status === 401) {
        localStorage.removeItem('adminPassword');
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }
    if (!response.ok) throw new Error(await response.text());
    return await response.json();
}

/**
 * 导入备份
 */
export async function importBackup(backupData) {
    const response = await authenticatedFetch('/v2/backup/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(backupData)
    });
    if (!response.ok) throw new Error(await response.text());
    return response;
}

/**
 * 开始OAuth授权
 */
export async function startOAuth(label, enabled) {
    const response = await authenticatedFetch('/v2/auth/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ label, enabled })
    });
    if (!response.ok) throw new Error(await response.text());
    return await response.json();
}

/**
 * 认领OAuth账号
 */
export async function claimOAuthAccount(authId) {
    const response = await authenticatedFetch(
        `/v2/auth/claim/${encodeURIComponent(authId)}`,
        { method: 'POST' }
    );
    return response;
}

/**
 * Chat请求
 */
export async function chatCompletions(model, messages) {
    const response = await authenticatedFetch('/v2/test/chat/completions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            model,
            messages,
            stream: true
        })
    });
    if (!response.ok) throw new Error(await response.text());
    return response;
}

// ==================== Chat 配置缓存 ====================
// @author ygw
let chatConfigCache = null;
let chatConfigCacheTime = 0;
const CHAT_CONFIG_CACHE_TTL = 5 * 60 * 1000; // 缓存有效期 5 分钟

/**
 * 获取 Chat 配置（从 settings 接口获取，带缓存）
 * @author ygw
 */
export async function getChatConfig() {
    const now = Date.now();
    // 检查缓存是否有效
    if (chatConfigCache && (now - chatConfigCacheTime) < CHAT_CONFIG_CACHE_TTL) {
        return chatConfigCache;
    }
    try {
        const settings = await fetchSettings();
        if (settings && settings.chatConfig) {
            chatConfigCache = settings.chatConfig;
            chatConfigCacheTime = now;
            return chatConfigCache;
        }
    } catch (error) {
        console.error('获取 chatConfig 失败:', error);
    }
    // 返回默认值
    return {
        budgetTokens: 2000,
        maxTokens: 4096,
        timeoutMs: 120000
    };
}

/**
 * 清除 Chat 配置缓存
 * @author ygw
 */
export function clearChatConfigCache() {
    chatConfigCache = null;
    chatConfigCacheTime = 0;
}

/**
 * Chat请求 with thinking support
 */
export async function chatCompletionsWithThinking(model, messages, thinkModeEnabled) {
    const chatConfig = await getChatConfig();

    const requestBody = {
        model,
        messages,
        stream: true
    };

    // Add thinking configuration if enabled
    if (thinkModeEnabled) {
        requestBody.thinking = {
            type: "enabled",
            budget_tokens: chatConfig.budgetTokens
        };
    }

    const response = await authenticatedFetch('/v2/test/chat/completions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody)
    });
    if (!response.ok) throw new Error(await response.text());
    return response;
}

/**
 * Claude Messages API for console (with thinking support)
 * @author ygw
 * @param {string} model - Model name (e.g., 'claude-sonnet-4-5')
 * @param {Array} messages - Array of message objects with {role, content}
 * @param {boolean} thinkModeEnabled - Whether to enable think mode
 * @param {number} maxTokens - Maximum tokens for response (可选，默认从配置获取)
 * @returns {Promise<Response>} Streaming response
 */
export async function claudeMessagesConsole(model, messages, thinkModeEnabled, maxTokens) {
    const chatConfig = await getChatConfig();
    const actualMaxTokens = maxTokens || chatConfig.maxTokens;

    const requestBody = {
        model,
        messages,
        max_tokens: actualMaxTokens,
        stream: true
    };

    // Add thinking configuration if enabled
    if (thinkModeEnabled) {
        requestBody.thinking = {
            type: "enabled",
            budget_tokens: chatConfig.budgetTokens
        };
    }

    const response = await authenticatedFetch('/v2/test/messages', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'anthropic-version': '2023-06-01'
        },
        body: JSON.stringify(requestBody)
    });

    if (!response.ok) {
        throw new Error(await response.text());
    }

    return response;
}

/**
 * 通用 SSE 流解析器
 * @param {Response} response - fetch 返回的响应
 * @param {Object} handlers - 回调集合
 * @param {Function} handlers.onMeta - 收到 meta 事件回调
 * @param {Function} handlers.onThinking - thinking_delta 回调
 * @param {Function} handlers.onAnswer - answer_delta 回调
 * @param {Function} handlers.onDone - done 事件回调
 * @param {Function} handlers.onError - error 事件回调
 * @param {Object} options - 额外选项
 * @param {AbortSignal} options.signal - 取消信号
 * @param {number} options.timeoutMs - 超时时间
 */
export async function consumeSSEStream(response, handlers = {}, options = {}) {
    const { signal, timeoutMs = 120000 } = options;
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    let aborted = false;

    const safeCall = (fn, payload) => {
        if (typeof fn === 'function') {
            fn(payload);
        }
    };

    const handleAbort = () => {
        aborted = true;
        reader.cancel();
    };

    if (signal) {
        if (signal.aborted) {
            handleAbort();
            return;
        }
        signal.addEventListener('abort', handleAbort, { once: true });
    }

    let timeoutId = null;
    const resetTimeout = () => {
        if (timeoutId) clearTimeout(timeoutId);
        timeoutId = setTimeout(() => {
            handleAbort();
        }, timeoutMs);
    };
    resetTimeout();

    try {
        while (true) {
            if (aborted) {
                throw new Error('请求已取消');
            }
            const { done, value } = await reader.read();
            if (done) break;
            resetTimeout();

            buffer += decoder.decode(value, { stream: true });

            let separatorIndex;
            while ((separatorIndex = buffer.indexOf('\n\n')) !== -1) {
                const rawEvent = buffer.slice(0, separatorIndex);
                buffer = buffer.slice(separatorIndex + 2);

                const parsed = parseSSEBlock(rawEvent);
                if (!parsed) continue;

                switch (parsed.event) {
                    case 'meta':
                        safeCall(handlers.onMeta, parsed.data);
                        break;
                    case 'thinking_delta':
                        safeCall(handlers.onThinking, parsed.data);
                        break;
                    case 'answer_delta':
                        safeCall(handlers.onAnswer, parsed.data);
                        break;
                    case 'done':
                        safeCall(handlers.onDone, parsed.data);
                        break;
                    case 'error':
                        safeCall(handlers.onError, parsed.data);
                        break;
                    default:
                        safeCall(handlers.onEvent, parsed);
                        break;
                }
            }
        }
    } finally {
        if (timeoutId) clearTimeout(timeoutId);
        if (signal) {
            signal.removeEventListener('abort', handleAbort);
        }
    }
}

// 解析单个 SSE 事件块
function parseSSEBlock(block) {
    if (!block || typeof block !== 'string') return null;

    const lines = block.split('\n');
    let eventName = 'message';
    const dataLines = [];

    for (const line of lines) {
        if (line.startsWith('event:')) {
            eventName = line.replace('event:', '').trim() || 'message';
        } else if (line.startsWith('data:')) {
            dataLines.push(line.replace('data:', '').trim());
        }
    }

    const dataStr = dataLines.join('\n');
    let data;
    try {
        data = dataStr ? JSON.parse(dataStr) : null;
    } catch (err) {
        data = { raw: dataStr };
    }

    return { event: eventName, data };
}

// ==================== 用户管理 API ====================

/**
 * 获取用户列表
 */
export async function listUsers() {
    const response = await authenticatedFetch('/v2/users');
    if (!response.ok) throw new Error('获取用户列表失败');
    return await response.json();
}

/**
 * 创建用户
 * @param {Object} userData - 用户数据
 * @param {string|null} testPassword - 测试模式密码（可选）
 */
export async function createUser(userData, testPassword = null) {
    const headers = { ...getAuthHeaders(), 'Content-Type': 'application/json' };
    if (testPassword) {
        headers['X-Test-Password'] = testPassword;
    }
    const response = await fetch('/v2/users', {
        method: 'POST',
        headers,
        body: JSON.stringify(userData)
    });
    if (response.status === 401) {
        localStorage.removeItem('adminPassword');
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }
    if (!response.ok) throw new Error('创建用户失败');
    return await response.json();
}

/**
 * 批量创建VIP用户
 * @param {number} count - 创建数量（默认10，最大100）
 * @param {string|null} testPassword - 测试模式密码（可选）
 * @author ygw
 */
export async function batchCreateVIPUsers(count = 10, testPassword = null) {
    const headers = { ...getAuthHeaders() };
    if (testPassword) {
        headers['X-Test-Password'] = testPassword;
    }
    const response = await fetch(`/v2/users/batch-vip?count=${count}`, {
        method: 'POST',
        headers
    });
    if (response.status === 401) {
        localStorage.removeItem('adminPassword');
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }
    if (!response.ok) throw new Error('批量创建VIP用户失败');
    return await response.json();
}

/**
 * 获取用户信息
 */
export async function getUser(userId) {
    const response = await authenticatedFetch(`/v2/users/${userId}`);
    if (!response.ok) throw new Error('获取用户信息失败');
    return await response.json();
}

/**
 * 更新用户信息
 * @param {string} userId - 用户ID
 * @param {Object} updates - 更新数据
 * @param {string|null} testPassword - 测试模式密码（可选）
 */
export async function updateUser(userId, updates, testPassword = null) {
    const headers = { ...getAuthHeaders(), 'Content-Type': 'application/json' };
    if (testPassword) {
        headers['X-Test-Password'] = testPassword;
    }
    const response = await fetch(`/v2/users/${userId}`, {
        method: 'PATCH',
        headers,
        body: JSON.stringify(updates)
    });
    if (response.status === 401) {
        localStorage.removeItem('adminPassword');
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }
    if (!response.ok) throw new Error('更新用户失败');
    return await response.json();
}

/**
 * 删除用户
 * @param {string} userId - 用户ID
 * @param {string|null} testPassword - 测试模式密码（可选）
 */
export async function deleteUser(userId, testPassword = null) {
    const headers = { ...getAuthHeaders() };
    if (testPassword) {
        headers['X-Test-Password'] = testPassword;
    }
    const response = await fetch(`/v2/users/${userId}`, {
        method: 'DELETE',
        headers
    });
    if (response.status === 401) {
        localStorage.removeItem('adminPassword');
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }
    if (!response.ok) throw new Error('删除用户失败');
    return await response.json();
}

/**
 * 重新生成用户 API Key
 */
export async function regenerateUserAPIKey(userId) {
    const response = await authenticatedFetch(`/v2/users/${userId}/regenerate-key`, {
        method: 'POST'
    });
    if (!response.ok) throw new Error('重新生成 API Key 失败');
    return await response.json();
}

/**
 * 获取用户统计信息
 */
export async function getUserStats(userId, days = 30) {
    const response = await authenticatedFetch(`/v2/users/${userId}/stats?days=${days}`);
    if (!response.ok) throw new Error('获取用户统计失败');
    return await response.json();
}

/**
 * 获取用户关联的 IP 列表
 * @param {string} userId - 用户ID
 */
export async function getUserIPs(userId) {
    const response = await authenticatedFetch(`/v2/users/${userId}/ips`);
    if (!response.ok) throw new Error('获取用户IP列表失败');
    return await response.json();
}

/**
 * 获取账号配额
 * @param {string} accountId - 账号ID
 * @param {boolean} refresh - 是否强制从API刷新（默认false，从数据库缓存读取）
 */
export async function getAccountQuota(accountId, refresh = false) {
    const url = `/v2/accounts/${encodeURIComponent(accountId)}/quota${refresh ? '?refresh=true' : ''}`;
    const response = await authenticatedFetch(url);
    if (!response.ok) {
        const text = await response.text();
        throw new Error(text || 'quota request failed');
    }
    return await response.json();
}

// ==================== 开发工具 API ====================

/**
 * 获取 Claude Code 配置
 */
export async function getClaudeCodeConfig() {
    const response = await authenticatedFetch('/v2/devtools/claude-code/config');
    if (!response.ok) throw new Error('获取配置失败');
    return await response.json();
}

/**
 * 保存 Claude Code 配置
 */
export async function saveClaudeCodeConfig(baseUrl, apiKey) {
    const response = await authenticatedFetch('/v2/devtools/claude-code/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ baseUrl, apiKey })
    });
    if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || '保存配置失败');
    }
    return await response.json();
}

/**
 * 获取 Droid 配置
 * @author ygw
 */
export async function getDroidConfig() {
    const response = await authenticatedFetch('/v2/devtools/droid/config');
    if (!response.ok) throw new Error('获取配置失败');
    return await response.json();
}

/**
 * 保存 Droid 配置
 * @author ygw
 */
export async function saveDroidConfig(baseUrl, apiKey) {
    const response = await authenticatedFetch('/v2/devtools/droid/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ baseUrl, apiKey })
    });
    if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || '保存配置失败');
    }
    return await response.json();
}

/**
 * 获取在线用户统计
 */
export async function getOnlineStats() {
    const response = await authenticatedFetch('/v2/stats/online');
    if (!response.ok) throw new Error('获取在线统计失败');
    return await response.json();
}

/**
 * 获取 AWS 延迟检测结果
 * @returns {Promise<{latency: number, status: string, timestamp: number}>}
 * @author ygw
 */
export async function getAwsLatency() {
    const response = await authenticatedFetch('/v2/health/aws-latency');
    if (!response.ok) throw new Error('获取 AWS 延迟失败');
    return await response.json();
}
