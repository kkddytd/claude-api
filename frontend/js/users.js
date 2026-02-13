// ==================== Users Management Module ====================

import * as API from './api.js';
import { showToast } from './ui.js';

export const usersMixin = {
    data() {
        return {
            users: [],
            selectedUser: null,
            showCreateUserModal: false,
            showEditUserModal: false,
            showDeleteUserModal: false,
            showUserStatsModal: false,
            showUserIPsModal: false, // 用户 IP 列表弹窗 @author ygw
            showRegenerateKeyModal: false,
            userToDelete: null,
            userToRegenerate: null,
            userStats: null,
            userIPs: [], // 用户关联的 IP 列表 @author ygw
            userIPsSortField: 'request_count_hour', // 默认按最近一小时排序 @author ygw
            userIPsSortOrder: 'desc', // 默认降序 @author ygw
            newAPIKey: null,
            createUserForm: {
                name: '',
                dailyQuota: 0,
                monthlyQuota: 0,
                requestQuota: 0,
                rateLimitRPM: 0,
                enabled: true,
                isVip: false,
                notes: ''
            },
            editUserForm: {},
            // 视图模式：'table' 或 'card' @author ygw
            userViewMode: localStorage.getItem('userViewMode') || 'table',
            // 排序字段和顺序 @author ygw
            userSortField: 'created_at',
            userSortOrder: 'desc',
        };
    },

    computed: {
        enabledUsersCount() {
            return this.users.filter(u => u.enabled).length;
        },
        // 排序后的用户IP列表 @author ygw
        sortedUserIPs() {
            if (!this.userIPs || this.userIPs.length === 0) return [];
            const sorted = [...this.userIPs];
            const field = this.userIPsSortField;
            const order = this.userIPsSortOrder;

            sorted.sort((a, b) => {
                let valA = a[field];
                let valB = b[field];

                // 处理空值
                if (valA === null || valA === undefined) valA = 0;
                if (valB === null || valB === undefined) valB = 0;

                // 时间字段特殊处理
                if (field === 'last_visit') {
                    valA = valA ? new Date(valA).getTime() : 0;
                    valB = valB ? new Date(valB).getTime() : 0;
                }

                // 数值比较
                if (order === 'asc') {
                    return valA - valB;
                } else {
                    return valB - valA;
                }
            });

            return sorted;
        },
        // 排序后的用户列表 @author ygw
        sortedUsers() {
            if (!this.users || this.users.length === 0) return [];
            const sorted = [...this.users];
            const field = this.userSortField;
            const order = this.userSortOrder;

            sorted.sort((a, b) => {
                let valA = a[field];
                let valB = b[field];

                // 处理空值
                if (valA === null || valA === undefined) valA = '';
                if (valB === null || valB === undefined) valB = '';

                // 字符串比较
                if (typeof valA === 'string' && typeof valB === 'string') {
                    const cmp = valA.localeCompare(valB, 'zh-CN');
                    return order === 'asc' ? cmp : -cmp;
                }

                // 数值比较
                if (order === 'asc') {
                    return valA - valB;
                } else {
                    return valB - valA;
                }
            });

            return sorted;
        }
    },

    methods: {
        /**
         * 设置用户视图模式
         * @author ygw
         * @param {string} mode - 视图模式：'table' 或 'card'
         */
        setUserViewMode(mode) {
            this.userViewMode = mode;
            localStorage.setItem('userViewMode', mode);
        },

        /**
         * 切换用户视图模式
         * @author ygw
         */
        toggleUserViewMode() {
            const newMode = this.userViewMode === 'table' ? 'card' : 'table';
            this.setUserViewMode(newMode);
        },

        /**
         * 处理用户排序
         * @author ygw
         * @param {string} field - 排序字段
         */
        handleUserSort(field) {
            if (this.userSortField === field) {
                // 同一字段，切换排序顺序
                this.userSortOrder = this.userSortOrder === 'asc' ? 'desc' : 'asc';
            } else {
                // 不同字段，默认降序
                this.userSortField = field;
                this.userSortOrder = 'desc';
            }
        },

        async handleLoadUsers() {
            try {
                const response = await API.listUsers();
                this.users = response || [];
            } catch (error) {
                console.error('Failed to load users:', error);
                showToast(this, '加载用户列表失败', 'error');
            }
        },

        handleCreateUser() {
            this.createUserForm = {
                name: '',
                dailyQuota: 0,
                monthlyQuota: 0,
                requestQuota: 0,
                rateLimitRPM: 0,
                enabled: true,
                isVip: false,
                notes: ''
            };
            this.newAPIKey = null;
            this.showCreateUserModal = true;
        },

        /**
         * 批量创建VIP用户
         * 默认创建10个VIP用户，用户名为"待分配"，每日请求限制1000次，频率限制5次/分钟
         * @author ygw
         */
        async handleBatchCreateVIPUsers() {
            if (!confirm('确定要批量创建10个VIP用户吗？\n\n默认配置：\n- 用户名：待分配\n- VIP：是\n- 每日请求限制：1000次\n- 频率限制：10次/分钟')) {
                return;
            }

            // 测试模式需要密码
            const doBatchCreate = async (testPassword) => {
                return await API.batchCreateVIPUsers(10, testPassword);
            };

            try {
                const result = await this.withTestPassword('批量创建VIP用户', doBatchCreate);
                if (result === null) return; // 用户取消

                showToast(this, `成功创建 ${result.count} 个VIP用户`, 'success');
                // 刷新用户列表
                await this.handleLoadUsers();
            } catch (error) {
                console.error('Failed to batch create VIP users:', error);
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '批量创建VIP用户失败', 'error');
                }
            }
        },

        async confirmCreateUser() {
            if (!this.createUserForm.name.trim()) {
                showToast(this, '请输入用户名', 'error');
                return;
            }

            // 转换为后端期望的字段名格式 @author ygw
            const formData = {
                name: this.createUserForm.name,
                daily_quota: this.createUserForm.dailyQuota || 0,
                monthly_quota: this.createUserForm.monthlyQuota || 0,
                request_quota: this.createUserForm.requestQuota || 0,
                rate_limit_rpm: this.createUserForm.rateLimitRPM || 0,
                enabled: this.createUserForm.enabled,
                is_vip: this.createUserForm.isVip || false,
                notes: this.createUserForm.notes || null
            };
            this.closeCreateUserModal(); // 先关闭创建弹窗

            // 测试模式需要密码
            const doCreate = async (testPassword) => {
                return await API.createUser(formData, testPassword);
            };

            try {
                const user = await this.withTestPassword('创建用户', doCreate);
                if (user === null) return; // 用户取消
                this.users.unshift(user);
                this.newAPIKey = user.api_key;
                showToast(this, `用户 ${user.name} 创建成功`, 'success');

                // 重新打开弹窗显示 API Key
                this.showCreateUserModal = true;
                setTimeout(() => {
                    if (!this.showCreateUserModal) return;
                    this.closeCreateUserModal();
                }, 5000);
            } catch (error) {
                console.error('Failed to create user:', error);
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '创建用户失败', 'error');
                }
            }
        },

        handleEditUser(user) {
            this.selectedUser = user;
            this.editUserForm = {
                name: user.name,
                dailyQuota: user.daily_quota,
                monthlyQuota: user.monthly_quota,
                requestQuota: user.request_quota || 0,
                rateLimitRPM: user.rate_limit_rpm || 0,
                enabled: user.enabled,
                isVip: user.is_vip || false,
                notes: user.notes || ''
            };
            this.showEditUserModal = true;
        },

        async confirmEditUser() {
            const updates = {};
            if (this.editUserForm.name !== this.selectedUser.name) {
                updates.name = this.editUserForm.name;
            }
            if (this.editUserForm.dailyQuota !== this.selectedUser.daily_quota) {
                updates.daily_quota = this.editUserForm.dailyQuota;
            }
            if (this.editUserForm.monthlyQuota !== this.selectedUser.monthly_quota) {
                updates.monthly_quota = this.editUserForm.monthlyQuota;
            }
            // 请求次数限制 @author ygw
            if (this.editUserForm.requestQuota !== (this.selectedUser.request_quota || 0)) {
                updates.request_quota = this.editUserForm.requestQuota;
            }
            // 频率限制（次/分钟）@author ygw
            if (this.editUserForm.rateLimitRPM !== (this.selectedUser.rate_limit_rpm || 0)) {
                updates.rate_limit_rpm = this.editUserForm.rateLimitRPM;
            }
            if (this.editUserForm.enabled !== this.selectedUser.enabled) {
                updates.enabled = this.editUserForm.enabled;
            }
            // VIP用户标识 @author ygw
            if (this.editUserForm.isVip !== (this.selectedUser.is_vip || false)) {
                updates.is_vip = this.editUserForm.isVip;
            }
            if (this.editUserForm.notes !== (this.selectedUser.notes || '')) {
                updates.notes = this.editUserForm.notes || null;
            }

            const userId = this.selectedUser.id;
            this.closeEditUserModal(); // 先关闭编辑弹窗
            
            // 测试模式需要密码
            const doUpdate = async (testPassword) => {
                return await API.updateUser(userId, updates, testPassword);
            };
            
            try {
                const updatedUser = await this.withTestPassword('更新用户', doUpdate);
                if (updatedUser === null) return; // 用户取消
                
                const index = this.users.findIndex(u => u.id === userId);
                if (index !== -1) {
                    this.users.splice(index, 1, updatedUser);
                }

                showToast(this, '用户信息已更新', 'success');
            } catch (error) {
                console.error('Failed to update user:', error);
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '更新用户失败', 'error');
                }
            }
        },

        handleDeleteUser(user) {
            this.userToDelete = user;
            this.showDeleteUserModal = true;
        },

        async confirmDeleteUser() {
            const userId = this.userToDelete.id;
            this.closeDeleteUserModal(); // 先关闭删除弹窗
            
            // 测试模式需要密码
            const doDelete = async (testPassword) => {
                return await API.deleteUser(userId, testPassword);
            };
            
            try {
                const result = await this.withTestPassword('删除用户', doDelete);
                if (result === null) return; // 用户取消
                
                this.users = this.users.filter(u => u.id !== userId);
                showToast(this, '用户已删除', 'success');
            } catch (error) {
                console.error('Failed to delete user:', error);
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '删除用户失败', 'error');
                }
            }
        },

        handleRegenerateAPIKey(user) {
            this.userToRegenerate = user;
            this.newAPIKey = null;
            this.showRegenerateKeyModal = true;
        },

        async confirmRegenerateAPIKey() {
            try {
                const response = await API.regenerateUserAPIKey(this.userToRegenerate.id);
                this.newAPIKey = response.api_key;

                // Update user in list
                const index = this.users.findIndex(u => u.id === this.userToRegenerate.id);
                if (index !== -1) {
                    this.users[index].api_key = response.api_key;
                }

                showToast(this, 'API Key 已重新生成', 'success');
            } catch (error) {
                console.error('Failed to regenerate API key:', error);
                showToast(this, '重新生成 API Key 失败', 'error');
            }
        },

        async handleShowUserStats(user) {
            try {
                this.selectedUser = user;
                this.userStats = await API.getUserStats(user.id, 30);
                this.showUserStatsModal = true;
            } catch (error) {
                console.error('Failed to load user stats:', error);
                showToast(this, '加载用户统计失败', 'error');
            }
        },

        /**
         * 显示用户关联的 IP 列表
         * @author ygw
         */
        async handleShowUserIPs(user) {
            try {
                this.selectedUser = user;
                const result = await API.getUserIPs(user.id);
                this.userIPs = result.ips || [];
                this.showUserIPsModal = true;
            } catch (error) {
                console.error('Failed to load user IPs:', error);
                showToast(this, '加载用户 IP 列表失败', 'error');
            }
        },

        /**
         * 点击 IP 跳转到日志查询
         * @author ygw
         */
        handleIPClick(ip) {
            this.showUserIPsModal = false;
            this.activeTab = 'logs';
            this.$nextTick(() => {
                this.logsFilters.clientIP = ip;
                this.handleLoadLogs(true);
            });
        },

        /**
         * 用户IP列表排序 @author ygw
         */
        handleUserIPsSort(field) {
            if (this.userIPsSortField === field) {
                this.userIPsSortOrder = this.userIPsSortOrder === 'asc' ? 'desc' : 'asc';
            } else {
                this.userIPsSortField = field;
                this.userIPsSortOrder = 'desc';
            }
        },

        closeUserIPsModal() {
            this.showUserIPsModal = false;
            this.userIPs = [];
        },

        closeCreateUserModal() {
            this.showCreateUserModal = false;
            this.newAPIKey = null;
        },

        closeEditUserModal() {
            this.showEditUserModal = false;
            this.selectedUser = null;
        },

        closeDeleteUserModal() {
            this.showDeleteUserModal = false;
            this.userToDelete = null;
        },

        closeRegenerateKeyModal() {
            this.showRegenerateKeyModal = false;
            this.userToRegenerate = null;
            this.newAPIKey = null;
        },

        closeUserStatsModal() {
            this.showUserStatsModal = false;
            this.selectedUser = null;
            this.userStats = null;
        },

        async copyAPIKey(apiKey) {
            try {
                if (navigator.clipboard && window.isSecureContext) {
                    await navigator.clipboard.writeText(apiKey);
                } else {
                    const textarea = document.createElement('textarea');
                    textarea.value = apiKey;
                    textarea.style.position = 'fixed';
                    textarea.style.opacity = '0';
                    document.body.appendChild(textarea);
                    textarea.select();
                    document.execCommand('copy');
                    document.body.removeChild(textarea);
                }
                showToast(this, 'API Key 已复制到剪贴板', 'success');
            } catch (err) {
                showToast(this, '复制失败', 'error');
            }
        },

        formatTokenCount(count) {
            if (!count) return '0';
            if (count >= 1000000) {
                return (count / 1000000).toFixed(2) + 'M';
            } else if (count >= 1000) {
                return (count / 1000).toFixed(2) + 'K';
            }
            return count.toLocaleString();
        },

        formatCostUSD(cost) {
            if (!cost || cost === 0) return '$0.00';
            if (cost < 0.01) {
                return '$' + cost.toFixed(4);
            }
            return '$' + cost.toFixed(2);
        },

        formatDate(dateStr) {
            if (!dateStr) return '-';
            try {
                return new Date(dateStr).toLocaleDateString('zh-CN');
            } catch {
                return dateStr;
            }
        }
    }
};
