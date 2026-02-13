// ==================== 账号管理模块 ====================

import * as API from './api.js';
import { showToast } from './ui.js';
import { getAccountShortId, maskEmail, downloadFile, generateTimestamp } from './utils.js';

/**
 * 账号管理Mixin
 */
export const accountsMixin = {
    data() {
        return {
            accounts: [],
            accountQuotas: {}, // 账号配额缓存 { accountId: { used, limit, loading, error } }
            isLoadingAccounts: false,
            isRefreshingAll: false,
            isRefreshingQuotas: false, // 刷新所有配额状态
            isCheckingIncomplete: false,
            isTogglingAll: false,
            currentOAuthData: null,
            oauthPollingTimer: null,
            showOAuthModal: false,
            oauthUrl: '',
            oauthStatus: 'init',
            oauthTitle: '添加账号',
            oauthDescription: '即将打开授权窗口，请按步骤完成操作',
            oauthStatusText: '准备中...',
            showImportModal: false,
            importStatus: 'idle',
            importTitle: '导入账号',
            importDescription: '请选择要导入的账号文件（JSON 格式）',
            importStatusText: '准备导入...',
            importSuccessCount: 0,
            importFailCount: 0,
            importDuplicateCount: 0,
            importTotalCount: 0,
            importProgressPercent: 0,
            showDeleteModal: false,
            deleteAccountId: null,
            deleteAccountLabel: '',
            // 删除无效账号弹窗
            showDeleteIncompleteModal: false,
            incompleteAccountCount: 0,
            // 删除封控账号弹窗
            showDeleteSuspendedModal: false,
            suspendedAccountCount: 0,
            isDeletingSuspended: false,
            // 账号导入确认弹窗
            showAccountImportConfirmModal: false,
            accountImportData: null,
            accountImportPreviewCount: 0,
            accountImportValidCount: 0,
            accountImportDuplicateCount: 0,
            // 清除账号统计弹窗
            showResetStatsModal: false,
            // 账号日志弹窗
            showAccountLogsModal: false,
            accountLogs: [],
            accountLogsTotal: 0,
            accountLogsLoading: false,
            accountLogsLimit: 50,
            accountLogsOffset: 0,
            accountLogsJumpPage: '',
            currentViewAccountId: null,
            currentViewAccountLabel: '',
            accountLogsAutoRefresh: false,
            accountLogsRefreshTimer: null,
            // 清除账号统计
            isResettingAccountStats: false,
            // 账号统计面板
            showAccountsStatsPanel: false,
            // 添加账号提示弹窗
            showAddAccountTipModal: false,
            // Token 导入相关
            showTokenImportModal: false,
            tokenImportStatus: 'idle', // idle, importing, success, error
            tokenImportTitle: '通过 Token 导入',
            tokenImportDescription: '支持两种格式：① 仅 refreshToken（社交登录）② 完整 IdC 格式（含 clientId/clientSecret）',
            tokenImportStatusText: '准备导入...',
            tokenImportSuccessCount: 0,
            tokenImportFailCount: 0,
            tokenImportDuplicateCount: 0,
            tokenImportTotalCount: 0,
            tokenImportResults: [],
            // 同步邮箱相关
            isSyncingEmails: false,
            // 批量选择相关
            // @author ygw
            selectedAccountIds: [],
            // 视图模式：'table' 或 'card'
            // @author ygw
            accountViewMode: localStorage.getItem('accountViewMode') || 'table',
            // 排序相关
            // @author ygw
            accountSortField: 'created',  // 默认按创建时间排序
            accountSortOrder: 'desc',      // 默认倒序
            // 分页相关 @author ygw
            accountPagination: {
                total: 0,
                page: 1,
                pageSize: 100,
                pages: 1
            },
            accountPageJump: '',  // 跳转页码输入
            // 总配额统计（从后端获取）@author ygw
            accountQuotaStats: {
                totalUsage: 0,
                totalLimit: 0,
                count: 0,
                percent: 0
            },
            // 账号统计（从后端获取，全局统计不受分页影响）@author ygw
            accountStats: {
                totalCount: 0,
                enabledCount: 0,
                successTotal: 0,
                errorTotal: 0
            },
            // 各状态账号数量统计 @author ygw
            statusStats: {
                normal: 0,
                disabled: 0,
                suspended: 0,
                exhausted: 0,
                expired: 0
            },
            // 状态筛选 @author ygw
            accountStatusFilter: 'all',
            accountStatusSelectOpen: false, // 状态选择下拉框开关
            // 支持的状态选项
            supportedAccountStatuses: [
                { value: 'all', label: '全部状态' },
                { value: 'normal', label: '正常' },
                { value: 'disabled', label: '已禁用' },
                { value: 'suspended', label: '已封控' },
                { value: 'exhausted', label: '额度用尽' },
                { value: 'expired', label: '已过期' }
            ]
        };
    },

    computed: {
        // 账号总数（使用后端统计）@author ygw
        accountsTotalCount() {
            return this.accountStats.totalCount || this.accountPagination.total || this.accounts.length;
        },
        // 启用账号数（使用后端统计）@author ygw
        accountsEnabledCount() {
            return this.accountStats.enabledCount || this.accounts.filter(acc => acc.enabled).length;
        },
        accountsDisabledCount() {
            return this.accountsTotalCount - this.accountsEnabledCount;
        },
        // 封控账号数量（从状态统计中获取）
        accountsSuspendedCount() {
            return this.accountStatsByStatus && this.accountStatsByStatus.suspended ? this.accountStatsByStatus.suspended : 0;
        },
        // 成功请求总数（使用后端统计）@author ygw
        accountsSuccessTotal() {
            return this.accountStats.successTotal || this.accounts.reduce((sum, acc) => sum + (acc.success_count || 0), 0);
        },
        // 失败请求总数（使用后端统计）@author ygw
        accountsErrorTotal() {
            return this.accountStats.errorTotal || this.accounts.reduce((sum, acc) => sum + (acc.error_count || 0), 0);
        },
        accountsSuccessRate() {
            const total = this.accountsSuccessTotal + this.accountsErrorTotal;
            return total > 0 ? ((this.accountsSuccessTotal / total) * 100).toFixed(2) : '0.00';
        },
        // 整体配额统计（使用后端返回的数据）@author ygw
        totalQuotaStats() {
            const totalUsage = this.accountQuotaStats.totalUsage || 0;
            const totalLimit = this.accountQuotaStats.totalLimit || 0;
            // 计算百分比，保留两位小数精度
            const percent = totalLimit > 0 ? (totalUsage / totalLimit) * 100 : 0;
            return {
                totalUsage: totalUsage,
                totalLimit: totalLimit,
                percent: percent,
                loadedCount: this.accountQuotaStats.count || 0
            };
        },
        // 账号日志当前页码
        accountLogsCurrentPage() {
            return Math.floor(this.accountLogsOffset / this.accountLogsLimit) + 1;
        },
        // 账号日志总页数
        accountLogsTotalPages() {
            return Math.ceil(this.accountLogsTotal / this.accountLogsLimit) || 1;
        },
        // 账号日志页码数组（最多显示5个）
        accountLogsPageNumbers() {
            const total = this.accountLogsTotalPages;
            const current = this.accountLogsCurrentPage;
            if (total <= 5) return Array.from({length: total}, (_, i) => i + 1);
            if (current <= 3) return [1, 2, 3, 4, 5];
            if (current >= total - 2) return [total - 4, total - 3, total - 2, total - 1, total];
            return [current - 2, current - 1, current, current + 1, current + 2];
        },
        // 是否全选账号
        // @author ygw
        isAllAccountsSelected() {
            return this.accounts.length > 0 &&
                   this.selectedAccountIds.length === this.accounts.length;
        },
        // 排序后的账号列表（排序已由后端完成，直接返回）
        // @author ygw
        sortedAccounts() {
            return this.accounts || [];
        }
    },

    methods: {
        getAccountShortId,
        maskEmail,

        /**
         * 格式化数字为友好显示（如 1234 -> 1.2K, 1234567 -> 1.2M）
         * @author ygw
         * @param {number} num - 要格式化的数字
         * @returns {string} 格式化后的字符串
         */
        formatNumber(num) {
            if (num === null || num === undefined) return '0';
            if (num < 1000) return num.toString();
            if (num < 10000) return (num / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
            if (num < 1000000) return Math.round(num / 1000) + 'K';
            if (num < 10000000) return (num / 1000000).toFixed(1).replace(/\.0$/, '') + 'M';
            return Math.round(num / 1000000) + 'M';
        },

        // 显示添加账号提示弹窗（每次都显示）
        showAddAccountTip() {
            this.showAddAccountTipModal = true;
            return false; // 返回false，等待用户确认
        },

        // 关闭添加账号提示弹窗并继续添加
        closeAddAccountTipAndContinue() {
            this.showAddAccountTipModal = false;
            // 继续执行添加账号流程
            this.proceedWithOAuth();
        },

        handleToggleAccountsStatsPanel() {
            this.showAccountsStatsPanel = !this.showAccountsStatsPanel;
            localStorage.setItem('showAccountsStatsPanel', this.showAccountsStatsPanel);
            // 自动刷新已关闭
        },

        // 批量启用所有账号（调用后端 API，排除封控账号）@author ygw
        async handleEnableAllAccounts() {
            if (this.isTogglingAll) return;
            this.isTogglingAll = true;

            const doEnable = async (testPassword) => {
                const headers = {
                    'Authorization': `Bearer ${localStorage.getItem('adminPassword')}`,
                    'Content-Type': 'application/json'
                };
                if (testPassword) {
                    headers['X-Test-Password'] = testPassword;
                }
                const response = await fetch('/v2/accounts/enable-all', {
                    method: 'POST',
                    headers
                });
                if (!response.ok) {
                    const text = await response.text();
                    throw new Error(text);
                }
                return await response.json();
            };

            try {
                const result = await this.withTestPassword('启用所有账号', doEnable);
                if (result === null) {
                    this.isTogglingAll = false;
                    return;
                }
                showToast(this, result.message || `已启用 ${result.count} 个账号`, 'success');
                await this.handleLoadAccounts();
            } catch (error) {
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '启用失败: ' + error.message, 'error');
                }
            } finally {
                this.isTogglingAll = false;
            }
        },

        // 批量禁用所有账号（调用后端 API）@author ygw
        async handleDisableAllAccounts() {
            if (this.isTogglingAll) return;
            this.isTogglingAll = true;

            const doDisable = async (testPassword) => {
                const headers = {
                    'Authorization': `Bearer ${localStorage.getItem('adminPassword')}`,
                    'Content-Type': 'application/json'
                };
                if (testPassword) {
                    headers['X-Test-Password'] = testPassword;
                }
                const response = await fetch('/v2/accounts/disable-all', {
                    method: 'POST',
                    headers
                });
                if (!response.ok) {
                    const text = await response.text();
                    throw new Error(text);
                }
                return await response.json();
            };

            try {
                const result = await this.withTestPassword('禁用所有账号', doDisable);
                if (result === null) {
                    this.isTogglingAll = false;
                    return;
                }
                showToast(this, result.message || `已禁用 ${result.count} 个账号`, 'warning');
                await this.handleLoadAccounts();
            } catch (error) {
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '禁用失败: ' + error.message, 'error');
                }
            } finally {
                this.isTogglingAll = false;
            }
        },

        async handleLoadAccounts(resetPage = false) {
            if (this.isLoadingAccounts) return;

            // 重置页码时回到第一页
            if (resetPage) {
                this.accountPagination.page = 1;
            }

            this.isLoadingAccounts = true;
            try {
                const result = await API.fetchAccounts({
                    page: this.accountPagination.page,
                    pageSize: this.accountPagination.pageSize,
                    orderBy: this.getBackendSortField(this.accountSortField),
                    orderDesc: this.accountSortOrder === 'desc',
                    status: this.accountStatusFilter // 状态筛选 @author ygw
                });
                // 只有当数据真的改变时才赋值，或者直接赋值，Vue 的 diff 会处理
                // 不要置空 accounts，防止页面内容消失导致闪烁
                this.accounts = result.accounts;
                this.accountPagination = result.pagination;
                this.accountQuotaStats = result.quotaStats; // 保存总配额统计 @author ygw
                // 保存账号统计（全局统计，不受分页影响）@author ygw
                if (result.accountStats) {
                    this.accountStats = result.accountStats;
                }
                // 保存各状态账号数量统计 @author ygw
                if (result.statusStats) {
                    this.statusStats = result.statusStats;
                }

                // 从账号列表数据初始化配额信息（不再单独调用 API）
                // @author ygw
                for (const acc of result.accounts) {
                    // 只有当有配额数据时才初始化
                    if (acc.usage_limit > 0 || acc.usage_current > 0 || acc.quota_refreshed_at) {
                        const percent = acc.usage_limit > 0 ? Math.round((acc.usage_current / acc.usage_limit) * 100) : 0;
                        this.accountQuotas[acc.id] = {
                            loading: false,
                            percent: percent,
                            currentUsage: acc.usage_current || 0,
                            usageLimit: acc.usage_limit || 0,
                            subscriptionType: acc.subscription_type,
                            quotaRefreshedAt: acc.quota_refreshed_at,
                            lastFetchTime: Date.now(),
                            fromCache: true
                        };
                    }
                }
            } catch (error) {
                showToast(this, '加载账号失败: ' + error.message, 'error');
            } finally {
                this.isLoadingAccounts = false;
            }
        },

        // ==================== 账号分页功能 ====================
        // @author ygw

        /**
         * 账号列表上一页
         * @author ygw
         */
        handleAccountsPrevPage() {
            if (this.accountPagination.page > 1) {
                this.accountPagination.page--;
                this.handleLoadAccounts();
                this.selectedAccountIds = []; // 切换页面时清除选择
            }
        },

        /**
         * 账号列表下一页
         * @author ygw
         */
        handleAccountsNextPage() {
            if (this.accountPagination.page < this.accountPagination.pages) {
                this.accountPagination.page++;
                this.handleLoadAccounts();
                this.selectedAccountIds = []; // 切换页面时清除选择
            }
        },

        /**
         * 账号列表跳转到指定页
         * @author ygw
         * @param {number} page - 目标页码
         */
        handleAccountsGoToPage(page) {
            if (page < 1 || page > this.accountPagination.pages || page === this.accountPagination.page) return;
            this.accountPagination.page = page;
            this.handleLoadAccounts();
            this.selectedAccountIds = []; // 切换页面时清除选择
        },

        /**
         * 账号列表跳转页码（输入框）
         * @author ygw
         */
        handleAccountsJumpToPage() {
            const page = parseInt(this.accountPageJump);
            if (isNaN(page) || page < 1 || page > this.accountPagination.pages) {
                this.accountPageJump = '';
                return;
            }
            this.handleAccountsGoToPage(page);
            this.accountPageJump = '';
        },

        /**
         * 获取账号分页页码数组（最多显示5个）
         * @author ygw
         * @returns {Array<number>} 页码数组
         */
        getAccountsPageNumbers() {
            const total = this.accountPagination.pages;
            const current = this.accountPagination.page;
            if (total <= 5) return Array.from({length: total}, (_, i) => i + 1);
            if (current <= 3) return [1, 2, 3, 4, 5];
            if (current >= total - 2) return [total - 4, total - 3, total - 2, total - 1, total];
            return [current - 2, current - 1, current, current + 1, current + 2];
        },

        // 异步加载账号配额（带延迟，避免请求过快）
        async loadAccountQuotas() {
            const delay = (ms) => new Promise(resolve => setTimeout(resolve, ms));
            for (let i = 0; i < this.accounts.length; i++) {
                this.loadSingleAccountQuota(this.accounts[i].id);
                // 每个请求间隔 200ms，避免触发限流
                if (i < this.accounts.length - 1) {
                    await delay(200);
                }
            }
        },

        // 加载单个账号配额（带自动重试和缓存）
        // @author ygw
        // @param {string} accountId - 账号ID
        // @param {number} retryCount - 重试次数
        // @param {boolean} forceRefresh - 是否强制刷新（跳过缓存）
        async loadSingleAccountQuota(accountId, retryCount = 0, forceRefresh = false) {
            const maxRetries = 3;
            const retryDelay = 2000; // 重试延迟 2 秒
            const CACHE_TTL = 5 * 60 * 1000; // 5分钟缓存有效期

            // 检查缓存是否有效（非强制刷新时）
            if (!forceRefresh && retryCount === 0) {
                const cached = this.accountQuotas[accountId];
                if (cached &&
                    !cached.loading &&
                    !cached.error &&
                    cached.lastFetchTime &&
                    (Date.now() - cached.lastFetchTime) < CACHE_TTL) {
                    // 缓存有效，直接返回
                    return;
                }
            }

            // 设置加载状态
            this.accountQuotas[accountId] = { loading: true, percent: null, error: null };
            try {
                // 调用 API 时始终带 refresh=true，强制从 AWS API 刷新配额
                const data = await API.getAccountQuota(accountId, true);

                // 处理后端返回的特殊状态
                if (data.status === 'suspended') {
                    this.accountQuotas[accountId] = { loading: false, percent: 100, error: '已封控', lastFetchTime: Date.now() };
                    return;
                }
                if (data.status === 'refreshing') {
                    // Token 正在后台刷新中，延迟后自动重试
                    if (retryCount < maxRetries) {
                        this.accountQuotas[accountId] = { loading: true, percent: null, error: null };
                        await new Promise(resolve => setTimeout(resolve, retryDelay * (retryCount + 1)));
                        return this.loadSingleAccountQuota(accountId, retryCount + 1, forceRefresh);
                    }
                    this.accountQuotas[accountId] = { loading: false, percent: null, error: '刷新中' };
                    return;
                }
                if (data.status === 'token_invalid') {
                    // Token 无效，后台已触发刷新，延迟后重试
                    if (retryCount < maxRetries) {
                        this.accountQuotas[accountId] = { loading: true, percent: null, error: null };
                        await new Promise(resolve => setTimeout(resolve, retryDelay * (retryCount + 1)));
                        return this.loadSingleAccountQuota(accountId, retryCount + 1, forceRefresh);
                    }
                    this.accountQuotas[accountId] = { loading: false, percent: null, error: 'Token无效' };
                    return;
                }

                if (data && data.usedPercent !== undefined) {
                    this.accountQuotas[accountId] = {
                        loading: false,
                        percent: Math.round(data.usedPercent),
                        currentUsage: data.currentUsage || 0,
                        usageLimit: data.usageLimit || 0,
                        daysUntilReset: data.daysUntilReset,
                        nextDateReset: data.nextDateReset,
                        freeTrialExpiry: data.freeTrialExpiry,      // 试用到期时间 @author ygw
                        freeTrialStatus: data.freeTrialStatus,      // 试用状态 @author ygw
                        userId: data.userId,
                        isDuplicate: data.isDuplicate || false,
                        duplicateAccountId: data.duplicateAccountId,
                        error: null,
                        lastFetchTime: Date.now()  // 记录获取时间 @author ygw
                    };
                    // 标签为空时用 userId 填充
                    if (data.userId) {
                        const account = this.accounts.find(acc => acc.id === accountId);
                        if (account && !account.label) account.label = data.userId;
                    }
                } else {
                    this.accountQuotas[accountId] = { loading: false, percent: null, error: '无数据' };
                }
            } catch (error) {
                const errMsg = error.message || '';

                // 可重试的错误（限流、网络错误等）
                const isRetriable = errMsg.includes('429') ||
                                   errMsg.includes('rate') ||
                                   errMsg.includes('limit') ||
                                   errMsg.includes('timeout') ||
                                   errMsg.includes('network') ||
                                   errMsg.includes('failed to fetch');

                if (isRetriable && retryCount < maxRetries) {
                    // 延迟后重试
                    this.accountQuotas[accountId] = { loading: true, percent: null, error: null };
                    await new Promise(resolve => setTimeout(resolve, retryDelay * (retryCount + 1)));
                    return this.loadSingleAccountQuota(accountId, retryCount + 1, forceRefresh);
                }

                this.accountQuotas[accountId] = { loading: false, percent: null, error: errMsg };
            }
        },

        // 获取账号配额显示文本
        getQuotaDisplay(accountId) {
            const quota = this.accountQuotas[accountId];
            if (!quota) return '-';
            if (quota.loading) return '...';
            if (quota.error) return '-';
            if (quota.percent !== null) {
                if (quota.percent >= 100) return '已用尽';
                return `${quota.percent}%`;
            }
            return '-';
        },

        /**
         * 获取配额悬浮提示（显示具体使用量）
         * @author ygw
         * @param {string} accountId - 账号ID
         * @returns {string} 悬浮提示文本
         */
        getQuotaTooltip(accountId) {
            const quota = this.accountQuotas[accountId];
            if (!quota || quota.loading) return '点击刷新';
            if (quota.error) return quota.error;
            if (quota.currentUsage !== undefined && quota.usageLimit !== undefined) {
                return `${Math.round(quota.currentUsage)} / ${Math.round(quota.usageLimit)}`;
            }
            return '点击刷新';
        },

        // 获取配额百分比（用于样式）
        getQuotaPercent(accountId) {
            const quota = this.accountQuotas[accountId];
            if (!quota || quota.loading || quota.error) return 0;
            return quota.percent || 0;
        },

        // 获取配额圆环颜色
        getQuotaColor(accountId) {
            const percent = this.getQuotaPercent(accountId);
            if (percent >= 90) return '#f44336';  // 红色
            if (percent >= 70) return '#ff9800';  // 橙色
            return '#4caf50';  // 绿色
        },

        // 获取配额圆环样式
        getQuotaCircleStyle(accountId) {
            const percent = this.getQuotaPercent(accountId);
            const color = this.getQuotaColor(accountId);
            const circumference = 2 * Math.PI * 16; // r=16
            const offset = circumference - (percent / 100) * circumference;
            return {
                strokeDasharray: `${circumference}`,
                strokeDashoffset: `${offset}`,
                stroke: color
            };
        },

        /**
         * 获取有效时间标签（原重置时间）
         * @author ygw - 优先从账号列表读取 token_expiry
         * @param {string} accountId - 账号ID
         * @returns {string} 有效时间显示文本
         */
        getQuotaResetLabel(accountId) {
            const account = this.accounts.find(acc => acc.id === accountId);
            const quota = this.accountQuotas[accountId];

            // 封控/已用尽账号显示 -
            if (account && (account.status === 'suspended' || account.status === 'exhausted')) {
                return '-';
            }
            if (quota && quota.error === '已封控') return '-';
            if (quota && quota.isDuplicate) return '⚠️ 重复';

            // 优先从账号列表读取 token_expiry（数据库缓存）
            // @author ygw
            let tokenExpiry = null;
            if (account && account.token_expiry) {
                tokenExpiry = account.token_expiry;
            } else if (quota && quota.freeTrialExpiry) {
                // 兼容：从 quota 缓存读取
                tokenExpiry = quota.freeTrialExpiry;
            }

            if (tokenExpiry) {
                const d = new Date(Number(tokenExpiry) * 1000);
                const now = new Date();
                const diffDays = Math.ceil((d - now) / (1000 * 60 * 60 * 24));

                // 格式化日期
                const dateStr = `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`;

                // 根据剩余天数显示不同样式
                if (diffDays <= 0) {
                    return '已过期';
                } else if (diffDays <= 7) {
                    return `${diffDays}天后到期`;
                } else {
                    return dateStr;
                }
            }

            // 无试用信息时显示重置时间
            if (quota && quota.nextDateReset) {
                const d = new Date(Number(quota.nextDateReset) * 1000);
                return `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`;
            }

            return '-';
        },

        // 检查账号是否重复
        isAccountDuplicate(accountId) {
            const quota = this.accountQuotas[accountId];
            return quota && quota.isDuplicate;
        },

        // 检查账号是否被封控
        // @author ygw - 优先使用 status 字段判断
        isAccountSuspended(accountId) {
            const account = this.accounts.find(acc => acc.id === accountId);
            if (account && account.status === 'suspended') {
                return true;
            }
            // 兼容：如果 status 字段为空，回退到 quota 错误判断
            const quota = this.accountQuotas[accountId];
            return quota && quota.error === '已封控';
        },

        // 检查账号配额是否已用尽
        // @author ygw - 优先使用 status 字段判断
        isAccountExhausted(accountId) {
            const account = this.accounts.find(acc => acc.id === accountId);
            if (account && account.status === 'exhausted') {
                return true;
            }
            // 兼容：如果 status 字段为空，回退到 quota 百分比判断
            const quota = this.accountQuotas[accountId];
            return quota && quota.percent !== null && quota.percent >= 100;
        },

        /**
         * 检查账号是否即将过期（7天内）
         * @author ygw - 优先从账号列表读取 token_expiry
         * @param {string} accountId - 账号ID
         * @returns {boolean} 是否即将过期
         */
        isAccountExpiringSoon(accountId) {
            const account = this.accounts.find(acc => acc.id === accountId);
            const quota = this.accountQuotas[accountId];

            // 优先从账号列表读取 token_expiry
            let tokenExpiry = null;
            if (account && account.token_expiry) {
                tokenExpiry = account.token_expiry;
            } else if (quota && quota.freeTrialExpiry) {
                tokenExpiry = quota.freeTrialExpiry;
            }

            if (!tokenExpiry) return false;

            const expiry = new Date(Number(tokenExpiry) * 1000);
            const now = new Date();
            const diffDays = Math.ceil((expiry - now) / (1000 * 60 * 60 * 24));

            return diffDays > 0 && diffDays <= 7;
        },

        /**
         * 检查账号是否已过期
         * @author ygw - 优先从账号列表读取 token_expiry
         * @param {string} accountId - 账号ID
         * @returns {boolean} 是否已过期
         */
        isAccountExpired(accountId) {
            const account = this.accounts.find(acc => acc.id === accountId);
            const quota = this.accountQuotas[accountId];

            // 优先从账号列表读取 token_expiry
            let tokenExpiry = null;
            if (account && account.token_expiry) {
                tokenExpiry = account.token_expiry;
            } else if (quota && quota.freeTrialExpiry) {
                tokenExpiry = quota.freeTrialExpiry;
            }

            if (!tokenExpiry) return false;

            const expiry = new Date(Number(tokenExpiry) * 1000);
            return expiry < new Date();
        },

        /**
         * 刷新所有账号（Token + 列表）
         * @author ygw
         */
        async handleRefreshAllAccounts() {
            if (this.isRefreshingAll) return;

            this.isRefreshingAll = true;
            try {
                const enabledAccounts = this.accounts.filter(acc => acc.enabled);

                // 如果有启用的账号，先刷新 Token
                if (enabledAccounts.length > 0) {
                    let success = 0, fail = 0;
                    for (const acc of enabledAccounts) {
                        try {
                            await API.refreshAccountToken(acc.id);
                            success++;
                        } catch {
                            fail++;
                        }
                    }

                    if (fail === 0) {
                        showToast(this, `Token 刷新成功：${success} 个账号`, 'success');
                    } else {
                        showToast(this, `Token 刷新：成功 ${success} 个，失败 ${fail} 个`, 'warning');
                    }
                }

                // 刷新账号列表
                await this.handleLoadAccounts();
            } catch (error) {
                showToast(this, '刷新失败: ' + error.message, 'error');
            } finally {
                this.isRefreshingAll = false;
            }
        },

        // 刷新所有账号配额
        // @author ygw - 被动刷新策略：配额刷新改为用户手动触发
        async handleRefreshAllQuotas() {
            if (this.isRefreshingQuotas) return;

            this.isRefreshingQuotas = true;
            try {
                showToast(this, '正在刷新所有账号配额...', 'info');
                const result = await API.refreshAllQuotas();
                
                if (result.success) {
                    const stats = result.stats || {};
                    const successCount = stats.success || 0;
                    const suspendedCount = stats.suspended || 0;
                    const expiredCount = stats.expired || 0;
                    const exhaustedCount = stats.exhausted || 0;
                    const errorCount = stats.error || 0;
                    
                    let message = `配额刷新完成：成功 ${successCount} 个`;
                    if (suspendedCount > 0) message += `，封控 ${suspendedCount} 个`;
                    if (expiredCount > 0) message += `，过期 ${expiredCount} 个`;
                    if (exhaustedCount > 0) message += `，用尽 ${exhaustedCount} 个`;
                    if (errorCount > 0) message += `，错误 ${errorCount} 个`;
                    
                    showToast(this, message, errorCount > 0 ? 'warning' : 'success');
                } else {
                    showToast(this, result.error || '刷新配额失败', 'error');
                }

                // 刷新账号列表以显示最新配额
                await this.handleLoadAccounts();
            } catch (error) {
                showToast(this, '刷新配额失败: ' + error.message, 'error');
            } finally {
                this.isRefreshingQuotas = false;
            }
        },

        async handleToggleAccount(accountId, enabled) {
            try {
                await API.updateAccount(accountId, { enabled });
                showToast(this, `账号已${enabled ? '启用' : '禁用'}`, enabled ? 'success' : 'error');
                // 只更新单个账号状态，不刷新所有配额
                const account = this.accounts.find(acc => acc.id === accountId);
                if (account) {
                    account.enabled = enabled;
                    // 启用时加载该账号配额
                    if (enabled) {
                        this.loadSingleAccountQuota(accountId);
                    }
                }
            } catch (error) {
                showToast(this, '更新失败: ' + error.message, 'error');
                await this.handleLoadAccounts();
            }
        },

        async handleRefreshAccount(accountId) {
            try {
                await API.refreshAccountToken(accountId);
                showToast(this, 'Token 刷新成功', 'success');
                // 只刷新该账号的配额，不刷新全部（强制刷新，跳过缓存）
                this.loadSingleAccountQuota(accountId, 0, true);
            } catch (error) {
                showToast(this, '刷新失败: ' + error.message, 'error');
            }
        },

        handleDeleteAccount(accountId) {
            const account = this.accounts.find(acc => acc.id === accountId);
            this.deleteAccountLabel = account?.label || `账号 #${accountId.substring(0, 8)}`;
            this.deleteAccountId = accountId;
            this.showDeleteModal = true;
        },

        closeDeleteModal() {
            this.showDeleteModal = false;
            this.deleteAccountId = null;
            this.deleteAccountLabel = '';
        },

        async confirmDelete() {
            if (!this.deleteAccountId) return;

            const accountId = this.deleteAccountId;
            this.closeDeleteModal(); // 先关闭确认弹窗
            
            // 测试模式需要密码
            const doDelete = async (testPassword) => {
                await API.deleteAccount(accountId, testPassword);
                return true;
            };
            
            try {
                const result = await this.withTestPassword('删除账号', doDelete);
                if (result === null) return; // 用户取消
                showToast(this, '账号已删除', 'success');
                // 从本地列表移除，不刷新全部
                this.accounts = this.accounts.filter(acc => acc.id !== accountId);
                delete this.accountQuotas[accountId];
            } catch (error) {
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '删除失败: ' + error.message, 'error');
                }
            }
        },

        async handleUpdateAccountLabel(accountId, label) {
            try {
                await API.updateAccount(accountId, { label: label.trim() || null });
                showToast(this, '标签已更新', 'success');
            } catch (error) {
                showToast(this, '更新标签失败: ' + error.message, 'error');
                await this.handleLoadAccounts();
            }
        },

        async handleViewLogs(accountId) {
            // 获取账号标签
            const account = this.accounts.find(acc => acc.id === accountId);
            this.currentViewAccountLabel = account?.label || '';
            this.currentViewAccountId = accountId;
            this.showAccountLogsModal = true;
            await this.loadAccountLogs();
            // 启动自动刷新
            this.startAccountLogsAutoRefresh();
        },

        async loadAccountLogs(resetPage = false) {
            if (this.accountLogsLoading) return;
            if (resetPage) {
                this.accountLogsOffset = 0;
            }
            this.accountLogsLoading = true;
            try {
                const params = new URLSearchParams();
                params.append('limit', this.accountLogsLimit);
                params.append('offset', this.accountLogsOffset);
                params.append('account_id', this.currentViewAccountId);

                const response = await fetch(`/v2/logs?${params}`, {
                    headers: { 'Authorization': `Bearer ${localStorage.getItem('adminPassword')}` }
                });
                const data = await response.json();

                this.accountLogs = data.logs || [];
                this.accountLogsTotal = data.total || 0;
            } catch (error) {
                console.error('加载账号日志失败:', error);
            } finally {
                this.accountLogsLoading = false;
            }
        },

        handleAccountLogsPrevPage() {
            if (this.accountLogsOffset > 0) {
                this.accountLogsOffset = Math.max(0, this.accountLogsOffset - this.accountLogsLimit);
                this.loadAccountLogs();
            }
        },

        handleAccountLogsNextPage() {
            if (this.accountLogs.length >= this.accountLogsLimit) {
                this.accountLogsOffset += this.accountLogsLimit;
                this.loadAccountLogs();
            }
        },

        handleAccountLogsJumpToPage() {
            const page = parseInt(this.accountLogsJumpPage);
            if (isNaN(page) || page < 1 || page > this.accountLogsTotalPages) {
                this.accountLogsJumpPage = '';
                return;
            }
            this.accountLogsOffset = (page - 1) * this.accountLogsLimit;
            this.accountLogsJumpPage = '';
            this.loadAccountLogs();
        },

        // 跳转到指定页
        handleAccountLogsGoToPage(page) {
            if (page < 1 || page > this.accountLogsTotalPages) return;
            this.accountLogsOffset = (page - 1) * this.accountLogsLimit;
            this.loadAccountLogs();
        },

        startAccountLogsAutoRefresh() {
            this.stopAccountLogsAutoRefresh();
            this.accountLogsAutoRefresh = true;
            this.accountLogsRefreshTimer = setInterval(() => {
                if (this.showAccountLogsModal && this.currentViewAccountId) {
                    this.loadAccountLogs();
                }
            }, 5000);
        },

        stopAccountLogsAutoRefresh() {
            this.accountLogsAutoRefresh = false;
            if (this.accountLogsRefreshTimer) {
                clearInterval(this.accountLogsRefreshTimer);
                this.accountLogsRefreshTimer = null;
            }
        },

        closeAccountLogsModal() {
            // 停止自动刷新
            this.stopAccountLogsAutoRefresh();
            this.showAccountLogsModal = false;
            setTimeout(() => {
                this.accountLogs = [];
                this.accountLogsTotal = 0;
                this.accountLogsOffset = 0;
                this.accountLogsJumpPage = '';
                this.currentViewAccountId = null;
                this.currentViewAccountLabel = '';
            }, 300);
        },

        // 格式化日志时间为友好格式
        formatLogTimeShort(timestamp) {
            if (!timestamp) return '-';
            const date = new Date(timestamp);
            const now = new Date();
            const isToday = date.toDateString() === now.toDateString();
            
            if (isToday) {
                return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
            }
            return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
        },

        // 清除所有账号的成功/失败计数和请求日志
        async handleResetAllAccountStats() {
            if (this.isResettingAccountStats) return;
            this.showResetStatsModal = true;
        },

        closeResetStatsModal() {
            this.showResetStatsModal = false;
        },

        async confirmResetStats() {
            this.showResetStatsModal = false;
            
            // 测试模式需要密码
            const doReset = async (testPassword) => {
                const headers = { 'Authorization': `Bearer ${localStorage.getItem('adminPassword')}` };
                if (testPassword) {
                    headers['X-Test-Password'] = testPassword;
                }
                const response = await fetch('/v2/accounts/reset-stats', {
                    method: 'POST',
                    headers
                });

                if (!response.ok) {
                    const text = await response.text();
                    throw new Error(text);
                }
                return true;
            };
            
            this.isResettingAccountStats = true;
            try {
                const result = await this.withTestPassword('清除账号数据', doReset);
                if (result === null) {
                    this.isResettingAccountStats = false;
                    return; // 用户取消
                }

                showToast(this, '已清除所有账号数据和日志', 'success');
                await this.handleLoadAccounts();
            } catch (error) {
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '清除失败: ' + error.message, 'error');
                }
            } finally {
                this.isResettingAccountStats = false;
            }
        },

        async handleDeleteIncompleteAccounts() {
            if (this.isCheckingIncomplete) return;

            this.isCheckingIncomplete = true;

            try {
                const listData = await API.fetchIncompleteAccounts();
                const incompleteCount = listData.count || 0;

                if (incompleteCount === 0) {
                    showToast(this, '没有信息不全的账号', 'info');
                    this.isCheckingIncomplete = false;
                    return;
                }

                // 显示确认弹窗
                this.incompleteAccountCount = incompleteCount;
                this.showDeleteIncompleteModal = true;
                this.isCheckingIncomplete = false;
            } catch (error) {
                showToast(this, '检查失败: ' + error.message, 'error');
                this.isCheckingIncomplete = false;
            }
        },

        closeDeleteIncompleteModal() {
            this.showDeleteIncompleteModal = false;
            this.incompleteAccountCount = 0;
        },

        async confirmDeleteIncomplete() {
            this.closeDeleteIncompleteModal();
            this.isCheckingIncomplete = true;

            try {
                const deleteData = await API.deleteIncompleteAccounts();
                showToast(this, `成功删除 ${deleteData.count} 个信息不全的账号`, 'success');
                await this.handleLoadAccounts();
            } catch (error) {
                showToast(this, '删除失败: ' + error.message, 'error');
            } finally {
                this.isCheckingIncomplete = false;
            }
        },

        /**
         * 获取所有封控账号列表
         * @author ygw
         * @returns {Array} 封控账号数组
         */
        getSuspendedAccounts() {
            return this.accounts.filter(acc => this.isAccountSuspended(acc.id));
        },

        /**
         * 处理删除封控账号按钮点击（直接删除，无确认弹窗）
         * @author ygw
         */
        async handleDeleteSuspendedAccounts() {
            if (this.isDeletingSuspended) return;

            this.isDeletingSuspended = true;

            const doDelete = async (testPassword) => {
                const result = await API.deleteSuspendedAccounts(testPassword);
                return result;
            };

            try {
                const result = await this.withTestPassword('删除封控账号', doDelete);
                if (result === null) {
                    this.isDeletingSuspended = false;
                    return; // 用户取消
                }

                if (result.count > 0) {
                    showToast(this, `成功删除 ${result.count} 个封控账号`, 'success');
                    await this.handleLoadAccounts(); // 刷新列表
                } else {
                    showToast(this, '没有封控的账号', 'info');
                }
            } catch (error) {
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '删除失败: ' + error.message, 'error');
                }
            } finally {
                this.isDeletingSuspended = false;
            }
        },

        /**
         * 关闭删除封控账号弹窗
         * @author ygw
         */
        closeDeleteSuspendedModal() {
            this.showDeleteSuspendedModal = false;
            this.suspendedAccountCount = 0;
        },

        /**
         * 确认删除所有封控账号（调用后端API）
         * @author ygw
         * @deprecated 已废弃，直接在 handleDeleteSuspendedAccounts 中执行
         */
        async confirmDeleteSuspended() {
            this.handleDeleteSuspendedAccounts();
        },

        // OAuth相关
        async handleStartOAuth() {
            // 检查账号配额
            if (this.isAtQuotaLimit) {
                showToast(this, `已达账号上限（${this.settingsData.maxAccounts}个），无法添加更多账号`, 'warning');
                return;
            }

            // 付费版检查账号配额限制
            if (!this.settingsData.isFreeEdition && this.isAtQuotaLimit) {
                showToast(this, `当前 ${this.editionDisplayName} 限制 ${this.settingsData.maxAccounts} 个账号，已达上限`, 'error');
                this.handleOpenVersionModal();
                return;
            }

            // 首次添加账号时显示提示弹窗
            if (!this.showAddAccountTip()) {
                return; // 等待用户确认提示后再继续
            }

            // 继续执行OAuth流程
            this.proceedWithOAuth();
        },

        // 实际执行OAuth流程
        async proceedWithOAuth() {
            const label = `账号-${new Date().toLocaleString()}`;
            const enabled = true;

            try {
                this.showOAuthModal = true;
                this.oauthStatus = 'init';
                this.oauthTitle = '添加账号';
                this.oauthDescription = '正在准备授权流程...';
                this.oauthStatusText = '初始化中...';

                const data = await API.startOAuth(label, enabled);
                this.currentOAuthData = data;
                this.oauthUrl = data.verificationUriComplete;

                this.oauthStatus = 'waiting';
                this.oauthTitle = '等待授权';
                this.oauthDescription = '已打开授权窗口和临时邮箱，请完成以下步骤';
                this.oauthStatusText = '等待授权完成...';

                try {
                    window.open(data.verificationUriComplete, '_blank');
                    setTimeout(() => {
                        window.open('https://tempmail.hk/zh', '_blank');
                    }, 500);
                } catch (e) {
                    console.error('Failed to open popup', e);
                }

                this.startOAuthPolling();

            } catch (error) {
                this.showOAuthModal = false;
                showToast(this, '启动失败: ' + error.message, 'error');
            }
        },

        startOAuthPolling() {
            // 先清除旧的轮询
            if (this.oauthPollingTimer) {
                clearInterval(this.oauthPollingTimer);
                this.oauthPollingTimer = null;
            }

            let attempts = 0;
            const maxAttempts = 60;

            this.oauthPollingTimer = setInterval(async () => {
                // 如果已成功或 authId 已清除，停止轮询
                if (this.oauthStatus === 'success' || !this.currentOAuthData?.authId) {
                    clearInterval(this.oauthPollingTimer);
                    this.oauthPollingTimer = null;
                    return;
                }

                attempts++;
                if (attempts >= maxAttempts) {
                    clearInterval(this.oauthPollingTimer);
                    this.oauthPollingTimer = null;
                    this.oauthStatus = 'init';
                    this.oauthTitle = '授权超时';
                    this.oauthDescription = '授权已超时，请重新打开窗口完成授权';
                    this.oauthStatusText = '已超时';
                    return;
                }

                try {
                    await this.claimOAuthAccount(true);
                } catch (error) {
                    // Continue polling
                }
            }, 5000);
        },

        async claimOAuthAccount(silent = false) {
            if (!this.currentOAuthData?.authId) return;

            const authId = this.currentOAuthData.authId;
            
            try {
                await API.claimOAuthAccount(authId);

                // 成功后立即清除，防止重复请求
                this.currentOAuthData = null;
                this.oauthUrl = '';
                if (this.oauthPollingTimer) {
                    clearInterval(this.oauthPollingTimer);
                    this.oauthPollingTimer = null;
                }

                this.oauthStatus = 'success';
                this.oauthTitle = '添加成功';
                this.oauthDescription = '账号已成功添加，可以开始使用了';
                this.oauthStatusText = '完成';

                await this.handleLoadAccounts();
                // 刷新设置以更新账号计数
                await this.handleLoadSettings();

                setTimeout(() => {
                    this.closeOAuthModal();
                }, 2000);

            } catch (error) {
                if (!silent) {
                    this.showOAuthModal = false;
                    showToast(this, '授权失败: ' + error.message, 'error');
                }
                throw error;
            }
        },

        reopenOAuthWindows() {
            if (this.currentOAuthData?.verificationUriComplete) {
                window.open(this.currentOAuthData.verificationUriComplete, '_blank');
                setTimeout(() => {
                    window.open('https://tempmail.hk/zh', '_blank');
                }, 500);
            } else if (this.oauthUrl) {
                window.open(this.oauthUrl, '_blank');
                setTimeout(() => {
                    window.open('https://tempmail.hk/zh', '_blank');
                }, 500);
            }
        },

        async copyOAuthUrl() {
            if (!this.oauthUrl) return;
            try {
                await this.copyToClipboard(this.oauthUrl);
                showToast(this, '链接已复制，请在无痕窗口打开', 'success');
            } catch (err) {
                showToast(this, '复制失败', 'error');
            }
        },

        async copyTempMailUrl() {
            try {
                await this.copyToClipboard('https://tempmail.hk/zh');
                showToast(this, '临时邮箱地址已复制', 'success');
            } catch (err) {
                showToast(this, '复制失败', 'error');
            }
        },

        async copyToClipboard(text) {
            if (navigator.clipboard && window.isSecureContext) {
                await navigator.clipboard.writeText(text);
            } else {
                const textarea = document.createElement('textarea');
                textarea.value = text;
                textarea.style.position = 'fixed';
                textarea.style.opacity = '0';
                document.body.appendChild(textarea);
                textarea.select();
                document.execCommand('copy');
                document.body.removeChild(textarea);
            }
        },

        closeOAuthModal() {
            if (this.oauthPollingTimer) {
                clearInterval(this.oauthPollingTimer);
                this.oauthPollingTimer = null;
            }
            this.currentOAuthData = null;
            this.showOAuthModal = false;
            setTimeout(() => {
                this.oauthUrl = '';
                this.oauthStatus = 'init';
                this.oauthTitle = '添加账号';
                this.oauthDescription = '即将打开授权窗口，请按步骤完成操作';
                this.oauthStatusText = '准备中...';
            }, 300);
        },

        // 导入导出
        async handleExportAccounts() {

            if (!this.accounts || this.accounts.length === 0) {
                showToast(this, '没有账号可导出', 'warning');
                return;
            }

            // 测试模式需要密码
            const doExport = async (testPassword) => {
                const response = await API.exportAccounts(testPassword);
                return await response.json();
            };
            
            try {
                const exportData = await this.withTestPassword('导出账号', doExport);
                if (exportData === null) return; // 用户取消

                const jsonStr = JSON.stringify(exportData, null, 2);
                const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, -5);
                downloadFile(jsonStr, `claude-accounts-${timestamp}.json`);

                showToast(this, `成功导出 ${exportData.length} 个账号`, 'success');
            } catch (error) {
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '导出失败: ' + error.message, 'error');
                }
            }
        },

        handleImportAccounts() {

            // 检查账号配额限制
            if (this.isAtQuotaLimit) {
                showToast(this, `当前 ${this.editionDisplayName} 限制 ${this.settingsData.maxAccounts} 个账号，已达上限`, 'error');
                this.handleOpenVersionModal();
                return;
            }
            this.$refs.accountImportInput.click();
        },

        async handleAccountImportFileChange(event) {
            const file = event.target.files[0];
            if (!file) return;

            event.target.value = '';

            if (!file.name.endsWith('.json')) {
                showToast(this, '请选择 JSON 格式的文件', 'error');
                return;
            }

            try {
                const fileContent = await file.text();
                const importData = JSON.parse(fileContent);

                if (!Array.isArray(importData)) {
                    throw new Error('文件格式错误：应该是账号数组');
                }

                if (importData.length === 0) {
                    showToast(this, '文件中没有账号数据', 'warning');
                    return;
                }

                // 检查剩余配额
                const remaining = this.remainingAccountQuota;
                if (remaining <= 0) {
                    showToast(this, `当前 ${this.editionDisplayName} 限制 ${this.settingsData.maxAccounts} 个账号，已达上限`, 'error');
                    this.handleOpenVersionModal();
                    return;
                }

                // 提示可能只能导入部分账号
                if (importData.length > remaining) {
                    showToast(this, `文件包含 ${importData.length} 个账号，但当前仅剩 ${remaining} 个配额，将导入前 ${remaining} 个`, 'warning');
                }

                // 直接调用后端导入接口
                this.showImportModal = true;
                this.importStatus = 'importing';
                this.importTitle = '正在导入账号';
                this.importDescription = '正在处理账号数据，请稍候...';
                this.importStatusText = '导入中...';
                this.importTotalCount = importData.length;
                this.importSuccessCount = 0;
                this.importFailCount = 0;
                this.importDuplicateCount = 0;

                const result = await API.importAccounts(importData);

                // 更新统计数据
                // 成功数 = imported
                // 重复数 = duplicate
                // 失败数 = invalid（格式无效）+ (valid - duplicate - imported)（有效但导入失败，如达到限制）
                this.importSuccessCount = result.imported || 0;
                this.importDuplicateCount = result.duplicate || 0;
                const validButFailed = (result.valid || 0) - (result.duplicate || 0) - (result.imported || 0);
                this.importFailCount = (result.invalid || 0) + Math.max(0, validButFailed);
                this.importTotalCount = result.total || 0;

                this.importStatus = 'success';
                this.importTitle = '导入完成！';
                this.importDescription = `总计 ${result.total} 个，有效 ${result.valid} 个，重复 ${result.duplicate} 个，无效 ${result.invalid} 个，成功导入 ${result.imported} 个`;
                this.importStatusText = '导入成功';

                // 刷新设置以更新账号计数
                await this.handleLoadSettings();

                setTimeout(async () => {
                    this.showImportModal = false;
                    await this.handleLoadAccounts();
                }, 2000);

            } catch (error) {
                this.importStatus = 'error';
                this.importTitle = '导入失败';
                this.importDescription = error.message;
                this.importStatusText = '导入失败';
            }
        },

        closeAccountImportConfirmModal() {
            this.showAccountImportConfirmModal = false;
            this.accountImportData = null;
            this.accountImportPreviewCount = 0;
            this.accountImportValidCount = 0;
            this.accountImportDuplicateCount = 0;
        },

        async confirmAccountImport() {
            // 已废弃，现在直接在 handleAccountImportFileChange 中处理
            const importData = this.accountImportData;
            this.closeAccountImportConfirmModal();

            // 开始导入
            this.showImportModal = true;
            this.importStatus = 'idle';
            this.importTitle = '导入账号';
            this.importDescription = `准备导入 ${importData.length} 个账号`;
            this.importStatusText = '准备导入...';
            this.importSuccessCount = 0;
            this.importFailCount = 0;
            this.importTotalCount = importData.length;
            this.importProgressPercent = 0;

            await new Promise(resolve => setTimeout(resolve, 500));

            this.importStatus = 'importing';
            this.importTitle = '正在导入账号';
            this.importDescription = '正在逐个导入账号，请稍候...';
            this.importStatusText = '正在导入...';

            for (let i = 0; i < importData.length; i++) {
                const acc = importData[i];

                try {
                    const response = await API.createAccount({
                        label: acc.label || null,
                        clientId: acc.clientId,
                        clientSecret: acc.clientSecret,
                        refreshToken: acc.refreshToken || null,
                        accessToken: acc.accessToken || null,
                        enabled: acc.enabled !== false,
                        errorCount: acc.errorCount || acc.error_count || 0,
                        successCount: acc.successCount || acc.success_count || 0
                    });

                    if (response.ok) {
                        this.importSuccessCount++;
                    } else {
                        this.importFailCount++;
                    }
                } catch (error) {
                    this.importFailCount++;
                }

                this.importProgressPercent = Math.round(((i + 1) / importData.length) * 100);
            }

            if (this.importFailCount === 0) {
                this.importStatus = 'success';
                this.importTitle = '导入成功！';
                this.importDescription = `成功导入所有账号，共 ${this.importSuccessCount} 个`;
                this.importStatusText = '导入成功';
            } else if (this.importSuccessCount === 0) {
                this.importStatus = 'error';
                this.importTitle = '导入失败';
                this.importDescription = `所有账号导入失败，共 ${this.importFailCount} 个`;
                this.importStatusText = '导入失败';
            } else {
                this.importStatus = 'success';
                this.importTitle = '导入完成';
                this.importDescription = `导入完成，成功 ${this.importSuccessCount} 个，失败 ${this.importFailCount} 个`;
                this.importStatusText = '部分成功';
            }

            await this.handleLoadAccounts();

            setTimeout(() => {
                this.closeImportModal();
            }, 3000);
        },

        closeImportModal() {
            this.showImportModal = false;
            setTimeout(() => {
                this.importStatus = 'idle';
                this.importTitle = '导入账号';
                this.importDescription = '请选择要导入的账号文件（JSON 格式）';
                this.importStatusText = '准备导入...';
                this.importSuccessCount = 0;
                this.importFailCount = 0;
                this.importDuplicateCount = 0;
                this.importTotalCount = 0;
                this.importProgressPercent = 0;
            }, 300);
        },

        // ==================== Token 导入功能 ====================

        // 打开 Token 导入文件选择
        handleImportByToken() {

            // 检查账号配额限制
            if (this.isAtQuotaLimit) {
                showToast(this, `当前 ${this.editionDisplayName} 限制 ${this.settingsData.maxAccounts} 个账号，已达上限`, 'error');
                this.handleOpenVersionModal();
                return;
            }

            this.$refs.tokenImportInput.click();
        },

        // 处理 Token 导入文件选择
        async handleTokenImportFileChange(event) {
            const file = event.target.files[0];
            if (!file) return;

            event.target.value = '';

            if (!file.name.endsWith('.json')) {
                showToast(this, '请选择 JSON 格式的文件', 'error');
                return;
            }

            try {
                const fileContent = await file.text();
                const importData = JSON.parse(fileContent);

                if (!Array.isArray(importData)) {
                    throw new Error('文件格式错误：应该是包含 refreshToken 的数组');
                }

                if (importData.length === 0) {
                    showToast(this, '文件中没有 Token 数据', 'warning');
                    return;
                }

                // 验证格式：确保每个元素都有 refreshToken 或完整的 clientId/clientSecret
                const validTokens = importData.filter(item => 
                    (item.refreshToken && typeof item.refreshToken === 'string') ||
                    (item.clientId && item.clientSecret)
                );
                if (validTokens.length === 0) {
                    showToast(this, '文件中没有有效的 Token 数据', 'error');
                    return;
                }

                // 检查剩余配额
                const remaining = this.remainingAccountQuota;
                if (remaining <= 0) {
                    showToast(this, `当前 ${this.editionDisplayName} 限制 ${this.settingsData.maxAccounts} 个账号，已达上限`, 'error');
                    this.handleOpenVersionModal();
                    return;
                }

                // 提示可能只能导入部分账号
                if (validTokens.length > remaining) {
                    showToast(this, `文件包含 ${validTokens.length} 个 Token，但当前仅剩 ${remaining} 个配额，将导入前 ${remaining} 个`, 'warning');
                }

                // 开始导入
                this.showTokenImportModal = true;
                this.tokenImportStatus = 'importing';
                this.tokenImportTitle = '正在导入账号';
                this.tokenImportDescription = `正在验证 ${validTokens.length} 个 Token 并获取账号信息，请稍候...`;
                this.tokenImportStatusText = '验证中...';
                this.tokenImportTotalCount = validTokens.length;
                this.tokenImportSuccessCount = 0;
                this.tokenImportFailCount = 0;
                this.tokenImportDuplicateCount = 0;
                this.tokenImportResults = [];

                // 调用后端 API 进行批量导入
                const result = await API.importAccountsByToken(validTokens);

                // 更新结果
                this.tokenImportSuccessCount = result.imported || 0;
                this.tokenImportFailCount = result.failed || 0;
                this.tokenImportDuplicateCount = result.duplicate || 0;
                this.tokenImportResults = result.results || [];

                if (result.success) {
                    this.tokenImportStatus = 'success';
                    this.tokenImportTitle = '导入完成！';
                    this.tokenImportDescription = result.message;
                    this.tokenImportStatusText = '导入成功';
                } else if (this.tokenImportSuccessCount > 0) {
                    this.tokenImportStatus = 'success';
                    this.tokenImportTitle = '部分导入成功';
                    this.tokenImportDescription = result.message;
                    this.tokenImportStatusText = '部分成功';
                } else {
                    this.tokenImportStatus = 'error';
                    this.tokenImportTitle = '导入失败';
                    this.tokenImportDescription = result.message || '所有 Token 验证失败';
                    this.tokenImportStatusText = '导入失败';
                }

                // 刷新设置以更新账号计数
                await this.handleLoadSettings();

                // 延迟关闭弹窗
                setTimeout(async () => {
                    if (this.tokenImportSuccessCount > 0) {
                        await this.handleLoadAccounts();
                    }
                    this.closeTokenImportModal();
                }, 3000);

            } catch (error) {
                this.tokenImportStatus = 'error';
                this.tokenImportTitle = '导入失败';
                this.tokenImportDescription = error.message;
                this.tokenImportStatusText = '导入失败';
                this.showTokenImportModal = true;
            }
        },

        // 关闭 Token 导入弹窗
        closeTokenImportModal() {
            this.showTokenImportModal = false;
            setTimeout(() => {
                this.tokenImportStatus = 'idle';
                this.tokenImportTitle = '通过 Token 导入';
                this.tokenImportDescription = '支持两种格式：① 仅 refreshToken（社交登录）② 完整 IdC 格式（含 clientId/clientSecret）';
                this.tokenImportStatusText = '准备导入...';
                this.tokenImportSuccessCount = 0;
                this.tokenImportFailCount = 0;
                this.tokenImportDuplicateCount = 0;
                this.tokenImportTotalCount = 0;
                this.tokenImportResults = [];
            }, 300);
        },

        // 同步所有账号的邮箱
        async handleSyncEmails() {
            if (this.isSyncingEmails) return;

            this.isSyncingEmails = true;
            showToast(this, '正在同步账号邮箱...', 'info');

            try {
                const result = await API.syncAccountEmails();

                if (result.synced > 0) {
                    showToast(this, `同步完成: 成功 ${result.synced} 个, 跳过 ${result.skipped} 个（已有邮箱）`, 'success');
                    await this.handleLoadAccounts(); // 刷新列表
                } else if (result.skipped > 0) {
                    showToast(this, `所有账号都已有邮箱（共 ${result.skipped} 个）`, 'info');
                } else {
                    showToast(this, '没有需要同步的账号', 'info');
                }
            } catch (error) {
                console.error('同步邮箱失败:', error);
                showToast(this, `同步失败: ${error.message}`, 'error');
            } finally {
                this.isSyncingEmails = false;
            }
        },

        // ==================== 批量选择功能 ====================
        // @author ygw

        /**
         * 切换全选/取消全选
         * @author ygw
         */
        toggleSelectAllAccounts() {
            if (this.isAllAccountsSelected) {
                this.selectedAccountIds = [];
            } else {
                this.selectedAccountIds = this.accounts.map(acc => acc.id);
            }
        },

        /**
         * 切换单个账号选择
         * @author ygw
         * @param {string} accountId - 账号ID
         */
        toggleSelectAccount(accountId) {
            const index = this.selectedAccountIds.indexOf(accountId);
            if (index > -1) {
                this.selectedAccountIds.splice(index, 1);
            } else {
                this.selectedAccountIds.push(accountId);
            }
        },

        /**
         * 清除所有选择
         * @author ygw
         */
        clearSelectedAccounts() {
            this.selectedAccountIds = [];
        },

        /**
         * 批量启用账号
         * @author ygw
         */
        async handleBatchEnable() {
            if (this.selectedAccountIds.length === 0) return;

            const count = this.selectedAccountIds.length;
            try {
                for (const id of this.selectedAccountIds) {
                    await API.updateAccount(id, { enabled: true });
                }
                showToast(this, `已启用 ${count} 个账号`, 'success');
                this.selectedAccountIds = [];
                await this.handleLoadAccounts();
            } catch (error) {
                showToast(this, '批量启用失败: ' + error.message, 'error');
            }
        },

        /**
         * 批量禁用账号
         * @author ygw
         */
        async handleBatchDisable() {
            if (this.selectedAccountIds.length === 0) return;

            const count = this.selectedAccountIds.length;
            try {
                for (const id of this.selectedAccountIds) {
                    await API.updateAccount(id, { enabled: false });
                }
                showToast(this, `已禁用 ${count} 个账号`, 'warning');
                this.selectedAccountIds = [];
                await this.handleLoadAccounts();
            } catch (error) {
                showToast(this, '批量禁用失败: ' + error.message, 'error');
            }
        },

        /**
         * 批量删除账号
         * @author ygw
         */
        async handleBatchDelete() {
            if (this.selectedAccountIds.length === 0) return;

            const count = this.selectedAccountIds.length;

            const doDelete = async (testPassword) => {
                let successCount = 0;
                let failCount = 0;

                for (const id of this.selectedAccountIds) {
                    try {
                        await API.deleteAccount(id, testPassword);
                        successCount++;
                        // 从本地列表移除
                        this.accounts = this.accounts.filter(acc => acc.id !== id);
                        delete this.accountQuotas[id];
                    } catch (error) {
                        failCount++;
                    }
                }

                return { successCount, failCount };
            };

            try {
                const result = await this.withTestPassword(`批量删除 ${count} 个账号`, doDelete);
                if (result === null) return; // 用户取消

                this.selectedAccountIds = [];

                if (result.failCount === 0) {
                    showToast(this, `成功删除 ${result.successCount} 个账号`, 'success');
                } else {
                    showToast(this, `删除完成：成功 ${result.successCount} 个，失败 ${result.failCount} 个`, 'warning');
                }
            } catch (error) {
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '批量删除失败: ' + error.message, 'error');
                }
            }
        },

        // ==================== 视图切换功能 ====================
        // @author ygw

        /**
         * 设置账号视图模式
         * @author ygw
         * @param {string} mode - 视图模式：'table' 或 'card'
         */
        setAccountViewMode(mode) {
            this.accountViewMode = mode;
            localStorage.setItem('accountViewMode', mode);
            // 切换到卡片视图时清除选择（卡片视图不支持批量选择）
            if (mode === 'card') {
                this.selectedAccountIds = [];
            }
        },

        /**
         * 切换账号视图模式
         * @author ygw
         */
        toggleAccountViewMode() {
            const newMode = this.accountViewMode === 'table' ? 'card' : 'table';
            this.setAccountViewMode(newMode);
        },

        // ==================== 排序功能 ====================
        // @author ygw

        /**
         * 按指定字段排序账号（调用后端API重新获取数据）
         * @author ygw
         * @param {string} field - 排序字段：'status', 'success', 'error', 'created', 'quota'
         */
        sortAccountsBy(field) {
            if (this.accountSortField === field) {
                // 同一字段，切换排序方向
                this.accountSortOrder = this.accountSortOrder === 'asc' ? 'desc' : 'asc';
            } else {
                // 不同字段，设置新字段，默认降序
                this.accountSortField = field;
                this.accountSortOrder = 'desc';
            }
            // 重新加载数据（回到第一页）
            this.handleLoadAccounts(true);
        },

        /**
         * 获取后端排序字段名
         * @author ygw
         * @param {string} field - 前端字段名
         * @returns {string} 后端字段名
         */
        getBackendSortField(field) {
            const fieldMap = {
                'created': 'created_at',
                'success': 'success_count',
                'error': 'error_count',
                'status': 'status',
                'quota': 'usage_current'
            };
            return fieldMap[field] || 'created_at';
        },

        /**
         * 格式化创建时间
         * @author ygw
         * @param {string} timestamp - ISO 时间字符串
         * @returns {string} 格式化后的时间
         */
        formatCreatedTime(timestamp) {
            if (!timestamp) return '-';
            try {
                const date = new Date(timestamp);
                const now = new Date();
                const isThisYear = date.getFullYear() === now.getFullYear();

                if (isThisYear) {
                    return date.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' }) +
                           ' ' + date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
                }
                return date.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' });
            } catch (e) {
                return '-';
            }
        },

        /**
         * 设置账号状态筛选并刷新列表
         * @author ygw
         * @param {string} status - 状态筛选值：all, normal, disabled, suspended, exhausted, expired
         */
        setAccountStatusFilter(status) {
            this.accountStatusFilter = status;
            this.handleLoadAccounts(true); // 重置页码
        },

        /**
         * 获取状态筛选标签显示文字
         * @author ygw
         * @param {string} status - 状态值
         * @returns {string} 状态显示文字
         */
        getStatusFilterLabel(status) {
            const labels = {
                'all': '全部',
                'normal': '正常',
                'disabled': '已禁用',
                'suspended': '已封控',
                'exhausted': '额度用尽',
                'expired': '已过期'
            };
            return labels[status] || '全部';
        }
    },

    // 组件销毁时清理定时器
    beforeUnmount() {
        // 清理 OAuth 轮询定时器
        if (this.oauthPollingTimer) {
            clearInterval(this.oauthPollingTimer);
            this.oauthPollingTimer = null;
        }
        // 清理账号日志刷新定时器
        if (this.accountLogsRefreshTimer) {
            clearInterval(this.accountLogsRefreshTimer);
            this.accountLogsRefreshTimer = null;
        }
    }
};
