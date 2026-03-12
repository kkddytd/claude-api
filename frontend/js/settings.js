// ==================== 设置管理模块 ====================

import * as API from './api.js';
import { authenticatedFetch } from './api.js';
import { showToast } from './ui.js';
import { downloadFile, generateTimestamp, generateClaudeAPIKey } from './utils.js';

/**
 * 设置管理Mixin
 */
export const settingsMixin = {
    data() {
        const cachedLayoutPref = (() => {
            const v = localStorage.getItem('layoutFullWidth');
            if (v === 'false') return false;
            if (v === 'true') return true;
            return true; // 默认铺满，避免初始闪烁
        })();
        return {
            announcementClosed: false,
            remoteAnnouncementClosed: false,
            settingsData: {
                adminPassword: '',
                apiKey: '',
                debugLog: false,
                enableRequestLog: false,
                logRetentionDays: 7,
                maxErrorCount: 30,
                port: 62311,
                layoutFullWidth: cachedLayoutPref,
                enableIPRateLimit: false,
                ipRateLimitWindow: 1,
                ipRateLimitMax: 100,
                blockedIPs: [],
                accountSelectionMode: 'sequential',
                supportedAccountSelectionModes: [],
                // 代理配置
                httpProxy: '',
                // 代理池配置
                proxyPoolEnabled: false,
                proxyPoolStrategy: 'round_robin',
                // 智能压缩配置
                compressionEnabled: false,
                compressionModel: 'claude-sonnet-4.5',
                supportedCompressionModels: [],
                        // 强制模型配置
                        forceModelEnabled: false,
                        forceModel: '',
                        supportedForceModels: [],
                        // 性能优化配置（合并了配额刷新和状态检查）
                        quotaRefreshConcurrency: 20,
                        quotaRefreshInterval: 120,
                        // 公告配置
                announcementEnabled: false,
                announcementText: '🎉 欢迎各位老板测试体验！免费用户如觉得好用，欢迎点击「添加账号」贡献账号，共享额度，让大家都能畅快使用～ 🚀',
                // 版本信息
                edition: 'free',
                maxAccounts: 1,
                currentAccountCount: 0,
                isFreeEdition: true,
                // 测试模式
                testMode: false
            },
            // 测试模式密码弹窗
            showTestPasswordModal: false,
            testPasswordInput: '',
            testPasswordCallback: null,
            testPasswordAction: '',
            showBackupImportModal: false,
            backupImportStatus: 'idle',
            backupImportTitle: '导入备份',
            backupImportDescription: '请选择要导入的备份文件',
            backupImportStatusText: '',
            backupImportData: null,
            // 版本详情弹窗
            showVersionModal: false,
            // 压缩模型下拉框状态
            compressionModelSelectOpen: false,
            // 强制模型下拉框状态
            forceModelSelectOpen: false,
            // 代理选择策略下拉框状态
            proxyStrategySelectOpen: false,
            // 账号选择方式下拉框状态
            accountSelectionModeSelectOpen: false,
            // 代理池管理
            showProxyPoolModal: false,
            proxyList: [],
            newProxyUrl: '',
            newProxyName: '',
            newProxyWeight: 1
        };
    },

    computed: {
        // 版本显示名称
        editionDisplayName() {
            const names = {
                'free': 'Free',
                'pro': 'Pro',
                'promax': 'Pro Max',
                'ultra': 'Ultra'
            };
            return names[this.settingsData.edition] || 'Free';
        },
        // 版本图标
        editionIcon() {
            const icons = {
                'free': 'ri-user-line',
                'pro': 'ri-medal-line',
                'promax': 'ri-flashlight-line',
                'ultra': 'ri-vip-crown-2-fill'
            };
            return icons[this.settingsData.edition] || 'ri-user-line';
        },
        // 版本徽章样式类
        editionBadgeClass() {
            return `edition-badge edition-badge--${this.settingsData.edition || 'free'}`;
        },
        // 账号配额使用情况
        accountQuotaText() {
            return `${this.settingsData.currentAccountCount}/${this.settingsData.maxAccounts}`;
        },
        // 账号配额百分比
        accountQuotaPercent() {
            if (this.settingsData.maxAccounts === 0) return 0;
            return Math.round((this.settingsData.currentAccountCount / this.settingsData.maxAccounts) * 100);
        },
        // 是否接近配额上限（>=80%）
        isNearQuotaLimit() {
            return this.accountQuotaPercent >= 80;
        },
        // 是否已达配额上限
        isAtQuotaLimit() {
            return this.settingsData.currentAccountCount >= this.settingsData.maxAccounts;
        },
        // 剩余可添加账号数
        remainingAccountQuota() {
            return Math.max(0, this.settingsData.maxAccounts - this.settingsData.currentAccountCount);
        },
        // 是否显示升级按钮（仅 pro 和 promax 显示）
        showUpgradeButton() {
            const edition = this.settingsData.edition;
            return edition === 'pro' || edition === 'promax';
        }
    },

    methods: {
        // 检查是否需要显示版本弹窗
        // 免费版：每次登录都显示（sessionStorage）
        // 付费版：只显示一次（localStorage）
        checkFirstVisit() {
            const isFree = this.settingsData.isFreeEdition;
            if (isFree) {
                // 免费版：每次会话都显示
                const visitedKey = 'claude-api_session_visited';
                if (!sessionStorage.getItem(visitedKey)) {
                    setTimeout(() => {
                        this.showVersionModal = true;
                        sessionStorage.setItem(visitedKey, 'true');
                    }, 500);
                }
            } else {
                // 付费版：只显示一次
                const visitedKey = 'claude-api_paid_visited';
                if (!localStorage.getItem(visitedKey)) {
                    setTimeout(() => {
                        this.showVersionModal = true;
                        localStorage.setItem(visitedKey, 'true');
                    }, 500);
                }
            }
        },
        // 打开版本详情弹窗
        handleOpenVersionModal() {
            this.showVersionModal = true;
        },
        // 关闭版本详情弹窗
        closeVersionModal() {
            this.showVersionModal = false;
        },

        async handleLoadSettings() {
            try {
                const data = await API.fetchSettings();
                if (data) {
                    this.settingsData = {
                        adminPassword: data.adminPassword || '',
                        apiKey: data.apiKey || generateClaudeAPIKey(),
                        debugLog: data.debugLog || false,
                        enableRequestLog: data.enableRequestLog !== undefined ? data.enableRequestLog : false,
                        logRetentionDays: data.logRetentionDays || 7,
                        maxErrorCount: Math.max(data.maxErrorCount || 30, 1),
                        port: data.port && data.port > 0 ? data.port : 62311,
                        layoutFullWidth: data.layoutFullWidth !== false,
                        enableIPRateLimit: data.enableIPRateLimit || false,
                        ipRateLimitWindow: data.ipRateLimitWindow || 1,
                        ipRateLimitMax: data.ipRateLimitMax || 100,
                        blockedIPs: data.blockedIPs || [],
                        accountSelectionMode: data.accountSelectionMode || 'sequential',
                        supportedAccountSelectionModes: data.supportedAccountSelectionModes || [],
                        // 代理配置
                        httpProxy: data.httpProxy || '',
                        // 代理池配置
                        proxyPoolEnabled: data.proxyPoolEnabled || false,
                        proxyPoolStrategy: data.proxyPoolStrategy || 'round_robin',
                        // 智能压缩配置
                        compressionEnabled: data.compressionEnabled || false,
                        compressionModel: data.compressionModel || 'claude-sonnet-4-5-20250929',
                        supportedCompressionModels: data.supportedCompressionModels || [],
                        // 强制模型配置
                        forceModelEnabled: data.forceModelEnabled || false,
                        forceModel: data.forceModel || '',
                        supportedForceModels: data.supportedForceModels || [],
                        // 性能优化配置（合并了配额刷新和状态检查）
                        quotaRefreshConcurrency: data.quotaRefreshConcurrency || 20,
                        quotaRefreshInterval: data.quotaRefreshInterval || 120,
                        // 公告配置
                        announcementEnabled: data.announcementEnabled || false,
                        announcementText: data.announcementText || '🎉 欢迎各位老板测试体验！免费用户如觉得好用，欢迎点击「添加账号」贡献账号，共享额度，让大家都能畅快使用～ 🚀',
                        // 远程公告
                        remoteAnnouncement: data.remoteAnnouncement || '',
                        // 版本信息
                        edition: data.edition || 'free',
                        maxAccounts: data.maxAccounts || 1,
                        currentAccountCount: data.currentAccountCount || 0,
                        isFreeEdition: data.isFreeEdition !== undefined ? data.isFreeEdition : true,
                        // 激活码状态
                        licenseInvalid: data.licenseInvalid || false,
                        licenseError: data.licenseError || '',
                        // 机器码
                        machineId: data.machineId || '',
                        // 测试模式
                        testMode: data.testMode || false
                    };
                    localStorage.setItem('layoutFullWidth', String(this.settingsData.layoutFullWidth));

                    // 如果激活码失效，显示提示
                    if (this.settingsData.licenseInvalid) {
                        this.showLicenseInvalidAlert();
                    }

                    // 如果代理池启用，加载代理列表
                    if (this.settingsData.proxyPoolEnabled) {
                        this.loadProxyList();
                    }
                }
            } catch (error) {
                console.error('加载设置失败:', error);
            }
        },

        /**
         * 生成符合 Claude API 格式的 API Key
         * @author ygw
         */
        handleGenerateAPIKey() {
            this.settingsData.apiKey = generateClaudeAPIKey();
            showToast(this, 'API Key 已生成', 'success');
        },

        async handleSaveSettings() {
            const port = Number(this.settingsData.port) || 0;
            if (port < 1 || port > 65535) {
                showToast(this, '端口号需在 1-65535 之间', 'error');
                return;
            }
            this.settingsData.port = Math.floor(port);

            // 排除 blockedIPs 字段，避免清空封禁IP数据
            const { blockedIPs, ...settingsToSave } = this.settingsData;
            
            // 测试模式需要密码
            const doSave = async (testPassword) => {
                await API.saveSettings(settingsToSave, testPassword);
                return true;
            };
            
            try {
                const result = await this.withTestPassword('保存设置', doSave);
                if (result === null) return; // 用户取消
                
                await this.handleLoadSettings(); // 保存后立即刷新，确保后端值生效
                localStorage.setItem('layoutFullWidth', String(this.settingsData.layoutFullWidth));
                showToast(this, '配置已保存', 'success');

                // 如果关闭了请求日志且当前在 logs tab，跳转到首页
                if (!this.settingsData.enableRequestLog && this.activeTab === 'logs') {
                    this.handleTabChange('home');
                }

                if (this.settingsData.adminPassword) {
                    localStorage.setItem('adminPassword', this.settingsData.adminPassword);
                }
            } catch (error) {
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '保存失败: ' + error.message, 'error');
                }
            }
        },

        async handleExportBackup() {
            // 测试模式需要密码
            const doExport = async (testPassword) => {
                return await API.exportBackup(testPassword);
            };
            
            try {
                const data = await this.withTestPassword('导出备份', doExport);
                if (data === null) return; // 用户取消
                
                const jsonStr = JSON.stringify(data, null, 2);
                const timestamp = generateTimestamp();
                downloadFile(jsonStr, `backup-${timestamp}.json`);
                showToast(this, '备份导出成功', 'success');
            } catch (error) {
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                } else {
                    showToast(this, '导出失败: ' + error.message, 'error');
                }
            }
        },

        handleImportBackup() {
            this.$refs.backupImportInput.click();
        },

        async handleBackupImportFileChange(event) {
            const file = event.target.files[0];
            if (!file) return;

            event.target.value = '';

            if (!file.name.endsWith('.json')) {
                showToast(this, '请选择 JSON 格式的文件', 'error');
                return;
            }

            try {
                const fileContent = await file.text();
                const backupData = JSON.parse(fileContent);

                // 验证备份数据格式（至少需要 accounts 或 settings）
                if (!backupData.accounts && !backupData.settings) {
                    throw new Error('备份文件格式错误：缺少有效数据');
                }
                if (backupData.accounts && !Array.isArray(backupData.accounts)) {
                    throw new Error('备份文件格式错误：accounts字段格式不正确');
                }

                // 统计备份内容
                const accountCount = backupData.accounts?.length || 0;
                // 同时支持驼峰格式（clientId）和下划线格式（client_id）
                const validAccountCount = (backupData.accounts || []).filter(acc =>
                    (acc.clientId || acc.client_id) && (acc.clientSecret || acc.client_secret)
                ).length;
                const userCount = backupData.users?.length || 0;
                const blockedIPCount = backupData.blocked_ips?.length || 0;
                const usageCount = backupData.user_token_usage?.length || 0;

                this.backupImportData = backupData;
                this.showBackupImportModal = true;
                this.backupImportStatus = 'confirm';
                this.backupImportTitle = '确认导入备份';

                // 构建描述信息
                const parts = [];
                if (accountCount > 0) parts.push(`${validAccountCount}/${accountCount} 个有效账号`);
                if (userCount > 0) parts.push(`${userCount} 个用户`);
                if (blockedIPCount > 0) parts.push(`${blockedIPCount} 个封禁IP`);
                if (usageCount > 0) parts.push(`${usageCount} 条使用记录`);
                if (backupData.settings) parts.push('系统设置');
                
                let description = parts.length > 0 ? `备份包含：${parts.join('、')}` : '备份文件为空';
                description += '。导入将覆盖所有现有数据';

                this.backupImportDescription = description;
                this.backupImportStatusText = '等待确认...';

            } catch (error) {
                showToast(this, '文件解析失败: ' + error.message, 'error');
            }
        },

        async confirmBackupImport() {
            if (!this.backupImportData) return;

            this.backupImportStatus = 'importing';
            this.backupImportTitle = '正在导入备份';
            this.backupImportDescription = '正在将备份数据写入数据库，请稍候...';
            this.backupImportStatusText = '导入中，请勿关闭页面...';

            try {
                // 过滤掉无效账号（缺少 clientId/client_id 或 clientSecret/client_secret）
                // 同时支持驼峰格式和下划线格式
                const validAccounts = this.backupImportData.accounts.filter(acc =>
                    (acc.clientId || acc.client_id) && (acc.clientSecret || acc.client_secret)
                );

                // 去重（根据clientId或client_id去重）
                const seenClientIds = new Set();
                const uniqueAccounts = validAccounts.filter(acc => {
                    const clientId = acc.clientId || acc.client_id;
                    if (seenClientIds.has(clientId)) {
                        return false;
                    }
                    seenClientIds.add(clientId);
                    return true;
                });

                // 构建清洁的备份数据
                const cleanBackupData = {
                    ...this.backupImportData,
                    accounts: uniqueAccounts
                };

                // 统计信息
                const totalCount = this.backupImportData.accounts.length;
                const validCount = validAccounts.length;
                const uniqueCount = uniqueAccounts.length;
                const filteredCount = totalCount - validCount;
                const duplicateCount = validCount - uniqueCount;

                console.log(`导入备份：总计 ${totalCount} 个账号，有效 ${validCount} 个，去重后 ${uniqueCount} 个，过滤 ${filteredCount} 个无效账号，${duplicateCount} 个重复账号`);

                await API.importBackup(cleanBackupData);

                this.backupImportStatus = 'success';
                this.backupImportTitle = '导入成功！';

                // 显示导入统计
                let successDescription = `成功导入 ${uniqueCount} 个有效账号`;
                if (filteredCount > 0) {
                    successDescription += `，已过滤 ${filteredCount} 个无效账号`;
                }
                if (duplicateCount > 0) {
                    successDescription += `，${duplicateCount} 个重复账号`;
                }
                successDescription += '，页面即将刷新';

                this.backupImportDescription = successDescription;
                this.backupImportStatusText = '导入成功';

                setTimeout(() => {
                    window.location.reload();
                }, 2000);

            } catch (error) {
                this.backupImportStatus = 'error';
                this.backupImportTitle = '导入失败';
                this.backupImportDescription = error.message;
                this.backupImportStatusText = '发生错误';
            }
        },

        closeBackupImportModal() {
            if (this.backupImportStatus === 'importing') {
                return;
            }
            this.showBackupImportModal = false;
            setTimeout(() => {
                this.backupImportStatus = 'idle';
                this.backupImportTitle = '导入备份';
                this.backupImportDescription = '请选择要导入的备份文件';
                this.backupImportStatusText = '';
                this.backupImportData = null;
            }, 300);
        },

        // ==================== 测试模式密码验证 ====================
        
        /**
         * 请求测试模式密码验证
         * @param {string} action - 操作描述（用于弹窗显示）
         * @returns {Promise<string|null>} - 返回密码或 null（用户取消）
         */
        requestTestPassword(action) {
            if (!this.settingsData.testMode) {
                return Promise.resolve(null); // 非测试模式，无需密码
            }
            
            return new Promise((resolve) => {
                this.testPasswordAction = action;
                this.testPasswordInput = '';
                this.testPasswordCallback = resolve;
                this.showTestPasswordModal = true;
            });
        },
        
        /**
         * 确认测试模式密码
         */
        confirmTestPassword() {
            const password = this.testPasswordInput.trim();
            if (!password) {
                showToast(this, '请输入操作密码', 'warning');
                return;
            }
            
            this.showTestPasswordModal = false;
            if (this.testPasswordCallback) {
                this.testPasswordCallback(password);
                this.testPasswordCallback = null;
            }
            this.testPasswordInput = '';
        },
        
        /**
         * 取消测试模式密码输入
         */
        cancelTestPassword() {
            this.showTestPasswordModal = false;
            if (this.testPasswordCallback) {
                this.testPasswordCallback(null);
                this.testPasswordCallback = null;
            }
            this.testPasswordInput = '';
        },

        /**
         * 带测试模式密码的请求包装器
         * @param {string} action - 操作描述
         * @param {Function} requestFn - 请求函数，接收密码参数
         */
        async withTestPassword(action, requestFn) {
            if (!this.settingsData.testMode) {
                // 非测试模式，直接执行
                return await requestFn(null);
            }
            
            const password = await this.requestTestPassword(action);
            if (password === null) {
                // 用户取消
                return null;
            }
            
            try {
                return await requestFn(password);
            } catch (error) {
                if (error.message && error.message.includes('TEST_MODE_PASSWORD_REQUIRED')) {
                    showToast(this, '操作密码错误', 'error');
                    return null;
                }
                throw error;
            }
        },

        // ==================== 代理池管理 ====================

        async loadProxyList() {
            try {
                const response = await authenticatedFetch('/v2/proxies');
                const data = await response.json();
                if (response.ok) {
                    this.proxyList = data.proxies || [];
                }
            } catch (error) {
                console.error('加载代理列表失败:', error);
            }
        },

        async handleAddProxy() {
            if (!this.newProxyUrl) return;
            try {
                const response = await authenticatedFetch('/v2/proxies', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        url: this.newProxyUrl,
                        name: this.newProxyName || '',
                        weight: this.newProxyWeight || 1
                    })
                });
                if (response.ok) {
                    showToast(this, '代理添加成功', 'success');
                    this.newProxyUrl = '';
                    this.newProxyName = '';
                    this.newProxyWeight = 1;
                    await this.loadProxyList();
                } else {
                    const data = await response.json();
                    showToast(this, data.error || '添加失败', 'error');
                }
            } catch (error) {
                showToast(this, '添加失败: ' + error.message, 'error');
            }
        },

        async handleToggleProxy(proxy) {
            try {
                const response = await authenticatedFetch(`/v2/proxies/${proxy.id}/toggle`, {
                    method: 'POST'
                });
                if (response.ok) {
                    await this.loadProxyList();
                }
            } catch (error) {
                showToast(this, '操作失败: ' + error.message, 'error');
            }
        },

        async handleDeleteProxy(proxy) {
            if (!confirm(`确定要删除代理 "${proxy.name || proxy.url}" 吗？`)) return;
            try {
                const response = await authenticatedFetch(`/v2/proxies/${proxy.id}`, {
                    method: 'DELETE'
                });
                if (response.ok) {
                    showToast(this, '代理已删除', 'success');
                    await this.loadProxyList();
                }
            } catch (error) {
                showToast(this, '删除失败: ' + error.message, 'error');
            }
        },

        closeProxyPoolModal() {
            this.showProxyPoolModal = false;
        },

        // 获取账号选择方式的显示标签
        getAccountSelectionModeLabel(mode) {
            const modeLabels = {
                'sequential': '顺序选择',
                'random': '随机选择',
                'weighted_random': '加权随机',
                'round_robin': '轮询选择'
            };
            return modeLabels[mode] || '顺序选择';
        }
    }
};
