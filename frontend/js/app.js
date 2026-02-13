// ==================== 主应用入口 ====================

import * as API from './api.js';
import { showToast, getToastIconClass, applyTheme } from './ui.js';
import { initWatermark, highlightCode, formatClientIP } from './utils.js';
import { accountsMixin } from './accounts.js';
import { usersMixin } from './users.js';
import { settingsMixin } from './settings.js';
import { chatMixin } from './chat.js';
import { logsMixin } from './logs.js';
import { ipsMixin } from './ips.js';
import { serverLogsMixin } from './serverLogs.js';
import { devtoolsMixin } from './devtools.js';

const { createApp } = Vue;

createApp({
    // 混入所有功能模块
    mixins: [accountsMixin, usersMixin, settingsMixin, chatMixin, logsMixin, ipsMixin, serverLogsMixin, devtoolsMixin],

    // ==================== 数据定义 ====================
    data() {
        return {
            // UI 状态
            theme: 'light',
            activeTab: 'home',
            // 所有可用的 Tab 定义
            allTabs: [
                { id: 'home', label: '首页', freeAllowed: true },
                { id: 'accounts', label: '账号管理', freeAllowed: true },
                { id: 'users', label: '用户管理', freeAllowed: false },
                { id: 'logs', label: '请求日志', freeAllowed: false },
                { id: 'ips', label: 'IP管理', freeAllowed: false },
                { id: 'settings', label: '系统配置', freeAllowed: true },
                { id: 'serverLogs', label: '服务日志', freeAllowed: true },
                { id: 'chat', label: 'Chat 会话', freeAllowed: true },
                { id: 'devtools', label: '开发工具配置', freeAllowed: true }
            ],

            // Toast 状态
            toastVisible: false,
            toastMessage: '',
            toastType: 'info',

            // 弹窗状态
            showLogoutModal: false,

            // 版本号
            appVersion: '',

            // 在线用户数
            onlineCount: 0,
            onlineCountTimer: null,

            // AWS 延迟检测 @author ygw
            awsLatency: null,        // 延迟值（毫秒），null 表示未检测
            awsLatencyStatus: 'unknown', // 状态：good/medium/poor/error/unknown
            awsLatencyTimer: null,
            awsLatencyChecking: false,

            // 已加载过数据的tab（切换时不重复加载）
            loadedTabs: new Set()
        };
    },

    // ==================== 计算属性 ====================
    computed: {
        // 根据版本过滤可见的 Tab
        tabs() {
            // 如果是免费版，只显示 freeAllowed: true 的 Tab
            if (this.settingsData && this.settingsData.isFreeEdition) {
                return this.allTabs.filter(tab => tab.freeAllowed);
            }
            // 付费版显示所有 Tab（除了请求日志需要检查是否启用）
            return this.allTabs;
        }
    },

    // ==================== 生命周期 ====================
    mounted() {
        this.initializeApp();
        const watermarkBgGenerator = initWatermark(this.theme);

        // 监听主题变化更新水印
        this.$watch('theme', () => {
            const container = document.getElementById('watermark-container');
            if (container) {
                container.style.backgroundImage = `url(${watermarkBgGenerator()})`;
            }
        });

        // 点击外部关闭下拉框 - 保存引用以便清理
        this._handleGlobalClick = (e) => {
            if (!e.target.closest('.custom-select')) {
                this.modelSelectOpen = false;
                this.accountSelectOpen = false;
                this.endpointSelectOpen = false;
                this.statusSelectOpen = false;
                this.logLevelSelectOpen = false;
                this.compressionModelSelectOpen = false;
                this.accountSelectionModeSelectOpen = false;
                this.accountStatusSelectOpen = false; // 账号状态筛选下拉框
            }
        };
        document.addEventListener('click', this._handleGlobalClick);

        // Tooltip 自动定位：根据按钮位置决定显示在上方还是下方 @author ygw
        this._handleTooltipPosition = (e) => {
            // 检查 e.target 是否是元素节点（文本节点没有 closest 方法）
            if (!e.target || typeof e.target.closest !== 'function') return;

            const btn = e.target.closest('.btn--mini[data-tooltip]');
            if (!btn) return;

            const rect = btn.getBoundingClientRect();
            const container = btn.closest('.account-table-container, .user-table-container');
            if (!container) return;

            const containerRect = container.getBoundingClientRect();
            const spaceBelow = containerRect.bottom - rect.bottom;
            const spaceAbove = rect.top - containerRect.top;

            // 如果下方空间不足（< 50px）且上方空间充足，则显示在上方
            if (spaceBelow < 50 && spaceAbove > 50) {
                btn.classList.add('tooltip-top');
            } else {
                btn.classList.remove('tooltip-top');
            }
        };
        document.addEventListener('mouseenter', this._handleTooltipPosition, true);
    },

    beforeUnmount() {
        // 清理全局事件监听器
        if (this._handleGlobalClick) {
            document.removeEventListener('click', this._handleGlobalClick);
        }
        // 清理 tooltip 定位监听器 @author ygw
        if (this._handleTooltipPosition) {
            document.removeEventListener('mouseenter', this._handleTooltipPosition, true);
        }
        // 清理定时器
        if (this.oauthPollingTimer) {
            clearInterval(this.oauthPollingTimer);
        }
        // 清理日志自动刷新
        if (this.logsAutoRefreshTimer) {
            clearInterval(this.logsAutoRefreshTimer);
        }
        // 清理在线数定时器
        this.stopOnlineCountTimer();
        // 清理 AWS 延迟检测定时器 @author ygw
        this.stopAwsLatencyTimer();
    },

    // ==================== 监听器 ====================
    watch: {
        showDeleteModal(newVal) {
            if (newVal) {
                this.$nextTick(() => {
                    const overlay = document.querySelector('.modal-overlay.active');
                    if (overlay) {
                        overlay.focus();
                    }
                });
            }
        }
    },

    // ==================== 方法定义 ====================
    methods: {
        // ========== 应用初始化 ==========
        async initializeApp() {
            // 初始化主题
            const savedTheme = localStorage.getItem('theme') ||
                (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
            this.theme = savedTheme;
            this.applyTheme();

            // 初始化 Tab（默认首页，若有有效的缓存则保持）
            // 注意：使用 allTabs 而不是 tabs，因为此时 settingsData 还未加载，tabs 会错误地返回免费版 tab 列表
            const savedTab = localStorage.getItem('current_tab');
            const allowedTabs = this.allTabs.map(t => t.id);
            this.activeTab = savedTab && allowedTabs.includes(savedTab) ? savedTab : 'home';

            // 检查登录
            const password = API.getStoredPassword();
            if (!password) {
                window.location.href = '/login';
                return;
            }

            // 必须加载的基础数据：settings（包含版本号和功能开关）
            await this.handleLoadSettings();
            // 从 settings 中获取版本号
            this.appVersion = this.settingsData.version || '';

            // 首次进入系统时显示版本信息弹窗（已禁用自动弹窗，用户可点击版本号手动查看）
            // this.checkFirstVisit();

            // 如果缓存的 tab 是 logs 但请求日志已关闭，跳转到首页
            if (this.activeTab === 'logs' && !this.settingsData.enableRequestLog) {
                this.activeTab = 'home';
                localStorage.setItem('current_tab', 'home');
            }

            // 免费版检查：如果缓存的 tab 不在允许列表中，跳转到首页
            if (this.settingsData.isFreeEdition) {
                const allowedTabIds = this.tabs.map(t => t.id);
                if (!allowedTabIds.includes(this.activeTab)) {
                    this.activeTab = 'home';
                    localStorage.setItem('current_tab', 'home');
                }
            }

            // 恢复面板状态
            this.showAccountsStatsPanel = localStorage.getItem('showAccountsStatsPanel') === 'true';
            this.showLogsStatsPanel = localStorage.getItem('showLogsStatsPanel') === 'true';

            // 根据当前 tab 按需加载数据
            await this.loadTabData(this.activeTab);

            // 启动在线用户数定时器
            this.startOnlineCountTimer();

            // 启动 AWS 延迟检测定时器 @author ygw
            this.startAwsLatencyTimer();

            // 配置 Markdown
            if (typeof marked !== 'undefined') {
                marked.setOptions({
                    breaks: true,
                    gfm: true,
                    highlight: function(code, lang) {
                        if (typeof hljs !== 'undefined' && lang && hljs.getLanguage(lang)) {
                            try {
                                return hljs.highlight(code, { language: lang }).value;
                            } catch (e) {
                                console.error('Highlight error:', e);
                            }
                        }
                        return code;
                    }
                });
            }

            // 初次加载时高亮现有代码块
            this.$nextTick(() => {
                highlightCode();
            });

            // 自动连接服务日志（Tab 切换不影响连接）
            this.connectServerLogs();
        },

        // ========== 按需加载 Tab 数据 ==========
        async loadTabData(tabId, forceReload = false) {
            // 如果已加载过且不强制刷新，跳过
            if (!forceReload && this.loadedTabs.has(tabId)) {
                return;
            }

            switch (tabId) {
                case 'home':
                    // 首页需要账号数据显示统计（不加载配额）
                    await this.handleLoadAccounts();
                    break;
                case 'accounts':
                    // 账号管理需要账号数据（配额从账号列表数据中获取，不再单独调用 API）
                    await this.handleLoadAccounts();
                    break;
                case 'users':
                    await this.handleLoadUsers();
                    break;
                case 'logs':
                    await this.handleLoadLogs();
                    if (this.showLogsStatsPanel) {
                        await this.handleLoadLogsStats();
                    }
                    break;
                case 'ips':
                    await this.handleLoadAllIPs();
                    break;
                case 'settings':
                    // settings 已在初始化时加载
                    break;
                case 'serverLogs':
                    // WebSocket 连接已在 connectServerLogs 处理
                    break;
                case 'chat':
                    // chat 需要账号列表用于选择（不加载配额）
                    await this.handleLoadAccounts();
                    this.initializeChatSessions();
                    break;
                case 'devtools':
                    await this.loadClaudeCodeConfig();
                    break;
            }

            // 标记该tab已加载
            this.loadedTabs.add(tabId);
        },

        formatClientIP,

        // ========== 主题管理 ==========
        handleToggleTheme() {
            this.theme = this.theme === 'dark' ? 'light' : 'dark';
            localStorage.setItem('theme', this.theme);
            this.applyTheme();
        },

        applyTheme() {
            applyTheme(this.theme);

            // 重新高亮所有代码块
            this.$nextTick(() => {
                highlightCode();
            });
        },

        // ========== Tab 管理 ==========
        handleTabChange(tabId) {
            this.activeTab = tabId;
            localStorage.setItem('current_tab', tabId);

            // 按需加载对应 tab 的数据
            this.loadTabData(tabId);

            if (tabId === 'chat') {
                this.$nextTick(() => {
                    this.scrollChatToBottom();
                    this.highlightChatCode();
                });
            }
        },

        // ========== 认证相关 ==========
        handleLogout() {
            this.showLogoutModal = true;
        },

        confirmLogout() {
            this.showLogoutModal = false;
            localStorage.removeItem('adminPassword');
            localStorage.removeItem('current_tab');
            window.location.href = '/login';
        },

        // ========== Toast 通知 ==========
        getToastIconClass() {
            return getToastIconClass(this.toastType);
        },

        // ========== 在线用户统计 ==========
        async fetchOnlineCount() {
            try {
                const data = await API.getOnlineStats();
                this.onlineCount = data.online_count || 0;
            } catch (error) {
                console.error('获取在线数失败:', error);
            }
        },

        startOnlineCountTimer() {
            // 立即获取一次
            this.fetchOnlineCount();
            // 每30秒刷新一次
            this.onlineCountTimer = setInterval(() => {
                this.fetchOnlineCount();
            }, 30000);
        },

        stopOnlineCountTimer() {
            if (this.onlineCountTimer) {
                clearInterval(this.onlineCountTimer);
                this.onlineCountTimer = null;
            }
        },

        // ========== AWS 延迟检测 @author ygw ==========
        /**
         * 检测服务器到 AWS 的网络延迟
         * 通过后端 API 测量，反映服务器到 AWS 的真实延迟
         */
        async checkAwsLatency() {
            if (this.awsLatencyChecking) return;
            this.awsLatencyChecking = true;

            try {
                const data = await API.getAwsLatency();

                if (data.latency >= 0) {
                    this.awsLatency = data.latency;
                    this.awsLatencyStatus = data.status;
                } else {
                    // 后端返回 -1 表示检测失败
                    this.awsLatency = null;
                    this.awsLatencyStatus = 'error';
                }
            } catch (error) {
                this.awsLatency = null;
                this.awsLatencyStatus = 'error';
                console.warn('AWS 延迟检测失败:', error.message);
            } finally {
                this.awsLatencyChecking = false;
            }
        },

        /**
         * 启动 AWS 延迟检测定时器
         */
        startAwsLatencyTimer() {
            // 立即检测一次
            this.checkAwsLatency();
            // 每 60 秒检测一次
            this.awsLatencyTimer = setInterval(() => {
                this.checkAwsLatency();
            }, 60000);
        },

        /**
         * 停止 AWS 延迟检测定时器
         */
        stopAwsLatencyTimer() {
            if (this.awsLatencyTimer) {
                clearInterval(this.awsLatencyTimer);
                this.awsLatencyTimer = null;
            }
        },

        /**
         * 获取延迟状态的显示文本
         */
        getAwsLatencyText() {
            if (this.awsLatencyChecking && this.awsLatency === null) {
                return '检测中...';
            }
            if (this.awsLatencyStatus === 'error' || this.awsLatency === null) {
                return '超时';
            }
            return `${this.awsLatency}ms`;
        },

        /**
         * 获取延迟状态的 CSS 类
         */
        getAwsLatencyClass() {
            return `latency-${this.awsLatencyStatus}`;
        }
    }
}).mount('#app');
