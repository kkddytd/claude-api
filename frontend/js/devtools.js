// ==================== 开发工具模块 ====================

import * as API from './api.js';
import { showToast } from './ui.js';

/**
 * 开发工具 Mixin
 * @author ygw
 */
export const devtoolsMixin = {
    data() {
        return {
            // Claude Code 配置
            claudeCodeConfig: {
                baseUrl: '',
                apiKey: '',
                configured: false,
                loading: false,
                saving: false
            },
            // Droid 配置
            droidConfig: {
                baseUrl: '',
                apiKey: '',
                configured: false,
                loading: false,
                saving: false
            }
        };
    },
    methods: {
        // 加载 Claude Code 配置
        // 注意：始终显示当前系统应配置的值（当前网站URL + 系统API Key），不回显已配置值
        // 这样用户只需点击"一键配置"即可完成配置
        async loadClaudeCodeConfig() {
            this.claudeCodeConfig.loading = true;
            try {
                const data = await API.getClaudeCodeConfig();
                // 只检查是否已配置，但始终使用当前系统的值
                this.claudeCodeConfig.configured = data.configured;
                // 始终使用当前系统应配置的值
                this.claudeCodeConfig.baseUrl = this.getDefaultBaseUrl();
                this.claudeCodeConfig.apiKey = this.getDefaultApiKey();
            } catch (err) {
                console.error('加载 Claude Code 配置失败:', err);
                this.claudeCodeConfig.baseUrl = this.getDefaultBaseUrl();
                this.claudeCodeConfig.apiKey = this.getDefaultApiKey();
            } finally {
                this.claudeCodeConfig.loading = false;
            }
        },

        // 获取默认 Base URL（当前网站地址）
        getDefaultBaseUrl() {
            return `${window.location.protocol}//${window.location.host}`;
        },

        // 获取默认 API Key（系统设置中的 API Key）
        getDefaultApiKey() {
            return this.settingsData?.apiKey || '123';
        },

        // 保存 Claude Code 配置
        async saveClaudeCodeConfig() {
            if (!this.claudeCodeConfig.baseUrl || !this.claudeCodeConfig.apiKey) {
                showToast('请填写 Base URL 和 API Key', 'warning');
                return;
            }

            this.claudeCodeConfig.saving = true;
            try {
                await API.saveClaudeCodeConfig(
                    this.claudeCodeConfig.baseUrl,
                    this.claudeCodeConfig.apiKey
                );
                this.claudeCodeConfig.configured = true;
                showToast('Claude Code 配置已保存', 'success');
            } catch (err) {
                showToast(err.message || '保存配置失败', 'error');
            } finally {
                this.claudeCodeConfig.saving = false;
            }
        },

        // 加载 Droid 配置
        // 注意：始终显示当前系统应配置的值，不回显已配置值
        async loadDroidConfig() {
            this.droidConfig.loading = true;
            try {
                const data = await API.getDroidConfig();
                // 只检查是否已配置，但始终使用当前系统的值
                this.droidConfig.configured = data.configured;
                // 始终使用当前系统应配置的值
                this.droidConfig.baseUrl = this.getDefaultBaseUrl();
                this.droidConfig.apiKey = this.getDefaultApiKey();
            } catch (err) {
                console.error('加载 Droid 配置失败:', err);
                this.droidConfig.baseUrl = this.getDefaultBaseUrl();
                this.droidConfig.apiKey = this.getDefaultApiKey();
            } finally {
                this.droidConfig.loading = false;
            }
        },

        // 保存 Droid 配置
        async saveDroidConfig() {
            if (!this.droidConfig.baseUrl || !this.droidConfig.apiKey) {
                showToast('请填写 Base URL 和 API Key', 'warning');
                return;
            }

            this.droidConfig.saving = true;
            try {
                await API.saveDroidConfig(
                    this.droidConfig.baseUrl,
                    this.droidConfig.apiKey
                );
                this.droidConfig.configured = true;
                showToast('Droid 配置已保存', 'success');
            } catch (err) {
                showToast(err.message || '保存配置失败', 'error');
            } finally {
                this.droidConfig.saving = false;
            }
        }
    }
};
