// ==================== IP管理模块 ====================

import * as API from './api.js';
import { showToast } from './ui.js';

export const ipsMixin = {
    data() {
        return {
            blockedIPs: [],
            visitorIPs: [],
            ipsLoading: false,
            newBlockIP: '',
            newBlockReason: '',
            ipViewMode: 'visitors',
            showBlockIPModal: false,
            blockIPTarget: '',
            showUnblockIPModal: false,
            unblockIPTarget: '',
            // IP列表排序状态
            ipSortField: 'request_count_hour',  // 默认按最近1小时请求数排序
            ipSortOrder: 'desc',                // 默认倒序
            // IP备注编辑状态
            editingIPNotes: null,               // 正在编辑备注的IP
            editingIPNotesValue: '',            // 编辑中的备注值
            // IP配置编辑弹窗
            showIPConfigModal: false,
            editingIPConfig: null,
            ipConfigForm: {
                notes: '',
                rateLimitRPM: 0,
                dailyRequestLimit: 0
            }
        };
    },

    computed: {
        // 排序后的访问IP列表
        sortedVisitorIPs() {
            if (!this.visitorIPs || this.visitorIPs.length === 0) return [];
            return [...this.visitorIPs].sort((a, b) => {
                let aVal, bVal;
                if (this.ipSortField === 'request_count') {
                    aVal = a.request_count || 0;
                    bVal = b.request_count || 0;
                } else if (this.ipSortField === 'request_count_day') {
                    aVal = a.request_count_day || 0;
                    bVal = b.request_count_day || 0;
                } else if (this.ipSortField === 'request_count_hour') {
                    aVal = a.request_count_hour || 0;
                    bVal = b.request_count_hour || 0;
                } else {
                    // 默认按 last_visit 排序
                    aVal = a.last_visit || '';
                    bVal = b.last_visit || '';
                }
                if (this.ipSortOrder === 'asc') {
                    return aVal > bVal ? 1 : (aVal < bVal ? -1 : 0);
                } else {
                    return aVal < bVal ? 1 : (aVal > bVal ? -1 : 0);
                }
            });
        }
    },

    methods: {
        async handleLoadAllIPs() {
            await Promise.all([
                this.handleLoadBlockedIPs(),
                this.handleLoadVisitorIPs()
            ]);
        },

        async handleLoadBlockedIPs() {
            this.ipsLoading = true;
            try {
                const response = await fetch('/v2/ips/blocked', {
                    headers: { 'Authorization': `Bearer ${localStorage.getItem('adminPassword')}` }
                });
                const data = await response.json();
                this.blockedIPs = data.blocked_ips || [];
            } catch (error) {
                console.error('加载封禁IP失败:', error);
                showToast(this, '加载封禁IP失败', 'error');
            } finally {
                this.ipsLoading = false;
            }
        },

        async handleLoadVisitorIPs() {
            this.ipsLoading = true;
            try {
                const response = await fetch('/v2/ips/visitors', {
                    headers: { 'Authorization': `Bearer ${localStorage.getItem('adminPassword')}` }
                });
                const data = await response.json();
                this.visitorIPs = data.visitor_ips || [];
            } catch (error) {
                console.error('加载访问IP失败:', error);
                showToast(this, '加载访问IP失败', 'error');
            } finally {
                this.ipsLoading = false;
            }
        },

        async handleBlockIP() {
            if (!this.newBlockIP.trim()) {
                showToast(this, '请输入IP地址', 'warning');
                return;
            }

            // 测试模式需要密码
            const doBlock = async (testPassword) => {
                const headers = {
                    'Authorization': `Bearer ${localStorage.getItem('adminPassword')}`,
                    'Content-Type': 'application/json'
                };
                if (testPassword) {
                    headers['X-Test-Password'] = testPassword;
                }
                const response = await fetch('/v2/ips/block', {
                    method: 'POST',
                    headers,
                    body: JSON.stringify({
                        ip: this.newBlockIP.trim(),
                        reason: this.newBlockReason.trim() || '手动封禁'
                    })
                });
                if (!response.ok) {
                    const text = await response.text();
                    throw new Error(text);
                }
                return await response.json();
            };

            try {
                const data = await this.withTestPassword('封禁 IP', doBlock);
                if (data === null) return; // 用户取消
                showToast(this, data.message || 'IP封禁成功', 'success');
                this.newBlockIP = '';
                this.newBlockReason = '';
                await this.handleLoadAllIPs();
            } catch (error) {
                console.error('封禁IP失败:', error);
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '封禁IP失败', 'error');
                }
            }
        },

        handleQuickBlockIP(ip) {
            this.blockIPTarget = ip;
            this.showBlockIPModal = true;
        },

        closeBlockIPModal() {
            this.showBlockIPModal = false;
            this.blockIPTarget = '';
        },

        async confirmBlockIP() {
            if (!this.blockIPTarget) return;

            const targetIP = this.blockIPTarget;
            
            // 测试模式需要密码
            const doBlock = async (testPassword) => {
                const headers = {
                    'Authorization': `Bearer ${localStorage.getItem('adminPassword')}`,
                    'Content-Type': 'application/json'
                };
                if (testPassword) {
                    headers['X-Test-Password'] = testPassword;
                }
                const response = await fetch('/v2/ips/block', {
                    method: 'POST',
                    headers,
                    body: JSON.stringify({
                        ip: targetIP,
                        reason: '从访问列表封禁'
                    })
                });
                if (!response.ok) {
                    const text = await response.text();
                    throw new Error(text);
                }
                return await response.json();
            };

            try {
                this.closeBlockIPModal(); // 先关闭确认弹窗
                const data = await this.withTestPassword('封禁 IP', doBlock);
                if (data === null) return; // 用户取消
                showToast(this, data.message || 'IP封禁成功', 'success');
                await this.handleLoadAllIPs();
            } catch (error) {
                console.error('封禁IP失败:', error);
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '封禁IP失败', 'error');
                }
            }
        },

        handleUnblockIP(ip) {
            this.unblockIPTarget = ip;
            this.showUnblockIPModal = true;
        },

        closeUnblockIPModal() {
            this.showUnblockIPModal = false;
            this.unblockIPTarget = '';
        },

        async confirmUnblockIP() {
            if (!this.unblockIPTarget) return;

            const targetIP = this.unblockIPTarget;
            
            // 测试模式需要密码
            const doUnblock = async (testPassword) => {
                const headers = {
                    'Authorization': `Bearer ${localStorage.getItem('adminPassword')}`,
                    'Content-Type': 'application/json'
                };
                if (testPassword) {
                    headers['X-Test-Password'] = testPassword;
                }
                const response = await fetch('/v2/ips/unblock', {
                    method: 'POST',
                    headers,
                    body: JSON.stringify({ ip: targetIP })
                });
                if (!response.ok) {
                    const text = await response.text();
                    throw new Error(text);
                }
                return await response.json();
            };

            try {
                this.closeUnblockIPModal(); // 先关闭确认弹窗
                const data = await this.withTestPassword('解封 IP', doUnblock);
                if (data === null) return; // 用户取消
                showToast(this, data.message || 'IP解封成功', 'success');
                await this.handleLoadAllIPs();
            } catch (error) {
                console.error('解封IP失败:', error);
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '解封IP失败', 'error');
                }
            }
        },

        isIPBlocked(ip) {
            return this.blockedIPs.some(blocked => blocked.ip === ip);
        },

        handleViewIPLogs(ip) {
            // 切换到日志Tab
            this.activeTab = 'logs';
            // 设置IP筛选条件
            this.$nextTick(() => {
                this.logsFilters.clientIP = ip;
                this.handleLoadLogs(true);
            });
        },

        formatIPTime(timestamp) {
            if (!timestamp) return '-';
            return new Date(timestamp).toLocaleString('zh-CN');
        },

        // 切换IP列表排序
        // @author ygw
        toggleIPSort(field) {
            if (this.ipSortField === field) {
                // 同一字段，切换排序方向
                this.ipSortOrder = this.ipSortOrder === 'asc' ? 'desc' : 'asc';
            } else {
                // 不同字段，设置新字段并默认倒序
                this.ipSortField = field;
                this.ipSortOrder = 'desc';
            }
        },

        // ==================== IP备注编辑功能 ====================
        
        // 开始编辑IP备注
        startEditIPNotes(ip) {
            this.editingIPNotes = ip.ip;
            this.editingIPNotesValue = ip.notes || '';
            this.$nextTick(() => {
                const input = document.querySelector('.ip-notes-input');
                if (input) input.focus();
            });
        },

        // 取消编辑IP备注
        cancelEditIPNotes() {
            this.editingIPNotes = null;
            this.editingIPNotesValue = '';
        },

        // 保存IP备注
        async saveIPNotes(ip) {
            try {
                const response = await fetch(`/v2/ips/config/${encodeURIComponent(ip)}`, {
                    method: 'PUT',
                    headers: {
                        'Authorization': `Bearer ${localStorage.getItem('adminPassword')}`,
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify({
                        notes: this.editingIPNotesValue.trim() || null
                    })
                });

                if (!response.ok) {
                    throw new Error('保存失败');
                }

                // 更新本地数据
                const ipData = this.visitorIPs.find(i => i.ip === ip);
                if (ipData) {
                    ipData.notes = this.editingIPNotesValue.trim() || null;
                }

                this.editingIPNotes = null;
                this.editingIPNotesValue = '';
                showToast(this, '备注保存成功', 'success');
            } catch (error) {
                console.error('保存IP备注失败:', error);
                showToast(this, '保存备注失败', 'error');
            }
        },

        // ==================== IP配置编辑弹窗 ====================

        // 打开IP配置编辑弹窗
        openIPConfigModal(ip) {
            this.editingIPConfig = ip;
            this.ipConfigForm = {
                notes: ip.notes || '',
                rateLimitRPM: ip.rate_limit_rpm || 0,
                dailyRequestLimit: ip.daily_request_limit || 0
            };
            this.showIPConfigModal = true;
        },

        // 关闭IP配置编辑弹窗
        closeIPConfigModal() {
            this.showIPConfigModal = false;
            this.editingIPConfig = null;
        },

        // 保存IP配置
        async saveIPConfig() {
            if (!this.editingIPConfig) return;

            try {
                const response = await fetch(`/v2/ips/config/${encodeURIComponent(this.editingIPConfig.ip)}`, {
                    method: 'PUT',
                    headers: {
                        'Authorization': `Bearer ${localStorage.getItem('adminPassword')}`,
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify({
                        notes: this.ipConfigForm.notes.trim() || null,
                        rate_limit_rpm: parseInt(this.ipConfigForm.rateLimitRPM) || 0,
                        daily_request_limit: parseInt(this.ipConfigForm.dailyRequestLimit) || 0
                    })
                });

                if (!response.ok) {
                    throw new Error('保存失败');
                }

                // 更新本地数据
                const ipData = this.visitorIPs.find(i => i.ip === this.editingIPConfig.ip);
                if (ipData) {
                    ipData.notes = this.ipConfigForm.notes.trim() || null;
                    ipData.rate_limit_rpm = parseInt(this.ipConfigForm.rateLimitRPM) || 0;
                    ipData.daily_request_limit = parseInt(this.ipConfigForm.dailyRequestLimit) || 0;
                }

                this.closeIPConfigModal();
                showToast(this, 'IP配置保存成功', 'success');
            } catch (error) {
                console.error('保存IP配置失败:', error);
                showToast(this, '保存配置失败', 'error');
            }
        }
    }
};

