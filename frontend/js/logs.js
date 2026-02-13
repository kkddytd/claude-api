// ==================== 请求日志模块 ====================

import * as API from './api.js';
import { showToast } from './ui.js';

export const logsMixin = {
    data() {
        return {
            logs: [],
            logsTotal: 0,
            logsStats: null,
            showLogsStatsPanel: false,
            logsLoading: false,
            logsFilters: {
                startTime: '',
                endTime: '',
                clientIP: '',
                accountID: '',
                endpointType: '',
                isSuccess: ''
            },
            logsLimit: 50,
            logsOffset: 0,
            logsJumpPage: '', // 跳转页码输入（旧版）
            logsPageJump: '', // 跳转页码输入
            logsAutoRefresh: false,
            logsAutoRefreshTimer: null,
            showLogDetailModal: false,
            selectedLog: null,
            showCleanupLogsModal: false,
            // 下拉选择器状态
            accountSelectOpen: false,
            endpointSelectOpen: false,
            statusSelectOpen: false
        };
    },

    computed: {
        // 当前页码
        logsCurrentPage() {
            return Math.floor(this.logsOffset / this.logsLimit) + 1;
        },
        // 总页数
        logsTotalPages() {
            return Math.ceil(this.logsTotal / this.logsLimit) || 1;
        },
        // 页码数组（最多显示5个）
        logsPageNumbers() {
            const total = this.logsTotalPages;
            const current = this.logsCurrentPage;
            if (total <= 5) return Array.from({length: total}, (_, i) => i + 1);
            if (current <= 3) return [1, 2, 3, 4, 5];
            if (current >= total - 2) return [total - 4, total - 3, total - 2, total - 1, total];
            return [current - 2, current - 1, current, current + 1, current + 2];
        }
    },

    methods: {
        async handleLoadLogs(resetPage = false) {
            if (resetPage) {
                this.logsOffset = 0;
            }
            this.logsLoading = true;
            try {
                const params = new URLSearchParams();
                params.append('limit', this.logsLimit);
                params.append('offset', this.logsOffset);

                // 转换本地时间为 ISO8601 格式
                if (this.logsFilters.startTime) {
                    const startISO = new Date(this.logsFilters.startTime).toISOString();
                    params.append('start_time', startISO);
                }
                if (this.logsFilters.endTime) {
                    const endISO = new Date(this.logsFilters.endTime).toISOString();
                    params.append('end_time', endISO);
                }
                if (this.logsFilters.clientIP) params.append('client_ip', this.logsFilters.clientIP);
                if (this.logsFilters.accountID) params.append('account_id', this.logsFilters.accountID);
                if (this.logsFilters.endpointType) params.append('endpoint_type', this.logsFilters.endpointType);
                if (this.logsFilters.isSuccess !== '') params.append('is_success', this.logsFilters.isSuccess);

                const response = await fetch(`/v2/logs?${params}`, {
                    headers: { 'Authorization': `Bearer ${localStorage.getItem('adminPassword')}` }
                });
                const data = await response.json();
                this.logs = data.logs || [];
                this.logsTotal = data.total || 0;
            } catch (error) {
                console.error('加载日志失败:', error);
                showToast(this, '加载日志失败', 'error');
            } finally {
                this.logsLoading = false;
            }
        },

        async handleLoadLogsStats() {
            try {
                const params = new URLSearchParams();
                // 与日志列表使用相同的筛选条件 @author ygw
                if (this.logsFilters.startTime) {
                    const startISO = new Date(this.logsFilters.startTime).toISOString();
                    params.append('start_time', startISO);
                }
                if (this.logsFilters.endTime) {
                    const endISO = new Date(this.logsFilters.endTime).toISOString();
                    params.append('end_time', endISO);
                }
                if (this.logsFilters.clientIP) params.append('client_ip', this.logsFilters.clientIP);
                if (this.logsFilters.accountID) params.append('account_id', this.logsFilters.accountID);
                if (this.logsFilters.endpointType) params.append('endpoint_type', this.logsFilters.endpointType);
                if (this.logsFilters.isSuccess !== '') params.append('is_success', this.logsFilters.isSuccess);

                const response = await fetch(`/v2/logs/stats?${params}`, {
                    headers: { 'Authorization': `Bearer ${localStorage.getItem('adminPassword')}` }
                });
                this.logsStats = await response.json();
            } catch (error) {
                console.error('加载统计失败:', error);
            }
        },

        async handleToggleLogsStatsPanel() {
            const willOpen = !this.showLogsStatsPanel;
            if (willOpen) {
                await this.handleLoadLogsStats();
            }
            this.showLogsStatsPanel = willOpen;
            localStorage.setItem('showLogsStatsPanel', willOpen);
        },

        async handleCleanupLogs() {
            this.showCleanupLogsModal = true;
        },

        closeCleanupLogsModal() {
            this.showCleanupLogsModal = false;
        },

        async confirmCleanupLogs() {
            this.showCleanupLogsModal = false;
            
            const daysToKeep = this.settingsData.logRetentionDays;
            
            // 测试模式需要密码
            const doCleanup = async (testPassword) => {
                const headers = {
                    'Authorization': `Bearer ${localStorage.getItem('adminPassword')}`,
                    'Content-Type': 'application/json'
                };
                if (testPassword) {
                    headers['X-Test-Password'] = testPassword;
                }
                const response = await fetch('/v2/logs/cleanup', {
                    method: 'POST',
                    headers,
                    body: JSON.stringify({ days_to_keep: daysToKeep })
                });
                if (!response.ok) {
                    const text = await response.text();
                    throw new Error(text);
                }
                return await response.json();
            };
            
            try {
                const data = await this.withTestPassword('清理日志', doCleanup);
                if (data === null) return; // 用户取消
                showToast(this, data.message || '清理成功', 'success');
                await this.handleLoadLogs();
            } catch (error) {
                console.error('清理日志失败:', error);
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '清理日志失败', 'error');
                }
            }
        },

        async handleCopyLogErrorMessage(message) {
            if (!message) return;
            try {
                await navigator.clipboard.writeText(message);
                showToast(this, '错误信息已复制', 'success');
            } catch (err) {
                // 回退方案，兼容不支持 clipboard API 的环境
                try {
                    const textarea = document.createElement('textarea');
                    textarea.value = message;
                    textarea.style.position = 'fixed';
                    textarea.style.left = '-9999px';
                    document.body.appendChild(textarea);
                    textarea.select();
                    document.execCommand('copy');
                    document.body.removeChild(textarea);
                    showToast(this, '错误信息已复制', 'success');
                } catch (fallbackErr) {
                    console.error('复制错误信息失败:', fallbackErr);
                    showToast(this, '复制失败，请手动复制', 'error');
                }
            }
        },

        handleSetLogsTimeRange(range) {
            const now = new Date();
            // 使用本地时间格式 YYYY-MM-DDTHH:mm
            const formatLocalTime = (date) => {
                const year = date.getFullYear();
                const month = String(date.getMonth() + 1).padStart(2, '0');
                const day = String(date.getDate()).padStart(2, '0');
                const hours = String(date.getHours()).padStart(2, '0');
                const minutes = String(date.getMinutes()).padStart(2, '0');
                return `${year}-${month}-${day}T${hours}:${minutes}`;
            };

            const end = formatLocalTime(now);
            let start;

            switch(range) {
                case '1h':
                    start = new Date(now.getTime() - 60 * 60 * 1000);
                    break;
                case 'today': // 当天 @author ygw
                    start = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 0, 0, 0);
                    break;
                case '24h':
                    start = new Date(now.getTime() - 24 * 60 * 60 * 1000);
                    break;
                case '7d':
                    start = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
                    break;
                default:
                    start = new Date(now.getTime() - 24 * 60 * 60 * 1000);
            }

            this.logsFilters.startTime = formatLocalTime(start);
            this.logsFilters.endTime = end;
            this.handleLoadLogs(true); // 重置到第一页
            this.handleLoadLogsStats();
        },

        /**
         * 点击IP直接筛选 @author ygw
         */
        handleFilterByIP(ip) {
            this.logsFilters.clientIP = ip;
            this.handleLoadLogs(true);
            this.handleLoadLogsStats();
        },

        handleResetLogsFilters() {
            this.logsFilters = {
                startTime: '',
                endTime: '',
                clientIP: '',
                accountID: '',
                endpointType: '',
                isSuccess: ''
            };
            this.handleLoadLogs(true); // 重置到第一页
            this.handleLoadLogsStats();
        },

        handleLogsNextPage() {
            if (this.logs.length >= this.logsLimit) {
                this.logsOffset += this.logsLimit;
                this.handleLoadLogs();
            }
        },

        handleLogsPrevPage() {
            if (this.logsOffset > 0) {
                this.logsOffset = Math.max(0, this.logsOffset - this.logsLimit);
                this.handleLoadLogs();
            }
        },

        /**
         * 跳转到指定页码
         * @author ygw
         */
        handleLogsJumpToPage() {
            const page = parseInt(this.logsJumpPage);
            if (isNaN(page) || page < 1 || page > this.logsTotalPages) {
                this.logsJumpPage = '';
                return;
            }
            this.logsOffset = (page - 1) * this.logsLimit;
            this.logsJumpPage = '';
            this.handleLoadLogs();
        },

        // 跳转到指定页
        handleLogsGoToPage(page) {
            if (page < 1 || page > this.logsTotalPages) return;
            this.logsOffset = (page - 1) * this.logsLimit;
            this.handleLoadLogs();
        },

        // 跳转到输入的页码
        handleLogsJumpToPage() {
            const page = parseInt(this.logsPageJump);
            if (isNaN(page) || page < 1 || page > this.logsTotalPages) {
                showToast(this, '请输入有效的页码', 'warning');
                return;
            }
            this.logsOffset = (page - 1) * this.logsLimit;
            this.logsPageJump = '';
            this.handleLoadLogs();
        },

        formatLogTime(timestamp) {
            if (!timestamp) return '-';
            return new Date(timestamp).toLocaleString('zh-CN');
        },

        formatDuration(ms) {
            return `${ms}ms`;
        },

        handleToggleLogsAutoRefresh() {
            this.logsAutoRefresh = !this.logsAutoRefresh;
            if (this.logsAutoRefresh) {
                this.startLogsAutoRefresh();
            } else {
                this.stopLogsAutoRefresh();
            }
        },

        startLogsAutoRefresh() {
            this.stopLogsAutoRefresh();
            this.logsAutoRefreshTimer = setInterval(() => {
                this.handleLoadLogs();
            }, 3000);
        },

        stopLogsAutoRefresh() {
            if (this.logsAutoRefreshTimer) {
                clearInterval(this.logsAutoRefreshTimer);
                this.logsAutoRefreshTimer = null;
            }
        },

        handleShowLogDetail(log) {
            this.selectedLog = log;
            this.showLogDetailModal = true;
        },

        handleCloseLogDetail() {
            this.showLogDetailModal = false;
            this.selectedLog = null;
        },

        parseUserAgent(ua) {
            if (!ua) return { browser: '未知', os: '未知', device: '未知' };

            let browser = '未知浏览器';
            let os = '未知系统';
            let device = '桌面设备';

            // 解析浏览器
            if (ua.includes('Edg/')) browser = 'Edge ' + (ua.match(/Edg\/([\d.]+)/) || [])[1];
            else if (ua.includes('Chrome/')) browser = 'Chrome ' + (ua.match(/Chrome\/([\d.]+)/) || [])[1];
            else if (ua.includes('Firefox/')) browser = 'Firefox ' + (ua.match(/Firefox\/([\d.]+)/) || [])[1];
            else if (ua.includes('Safari/') && !ua.includes('Chrome')) browser = 'Safari ' + (ua.match(/Version\/([\d.]+)/) || [])[1];
            else if (ua.includes('curl/')) browser = 'curl ' + (ua.match(/curl\/([\d.]+)/) || [])[1];
            else if (ua.includes('python-requests')) browser = 'Python Requests';
            else if (ua.includes('PostmanRuntime')) browser = 'Postman';

            // 解析操作系统
            if (ua.includes('Windows NT 10.0')) os = 'Windows 10/11';
            else if (ua.includes('Windows NT 6.3')) os = 'Windows 8.1';
            else if (ua.includes('Windows NT 6.2')) os = 'Windows 8';
            else if (ua.includes('Windows NT 6.1')) os = 'Windows 7';
            else if (ua.includes('Mac OS X')) os = 'macOS ' + ((ua.match(/Mac OS X ([\d_]+)/) || [])[1] || '').replace(/_/g, '.');
            else if (ua.includes('Linux')) os = 'Linux';
            else if (ua.includes('Android')) os = 'Android ' + (ua.match(/Android ([\d.]+)/) || [])[1];
            else if (ua.includes('iPhone')) os = 'iOS ' + ((ua.match(/OS ([\d_]+)/) || [])[1] || '').replace(/_/g, '.');

            // 解析设备类型
            if (ua.includes('Mobile') || ua.includes('Android') || ua.includes('iPhone')) device = '移动设备';
            else if (ua.includes('Tablet') || ua.includes('iPad')) device = '平板设备';

            return { browser, os, device };
        }
    },

    beforeUnmount() {
        this.stopLogsAutoRefresh();
    }
};

