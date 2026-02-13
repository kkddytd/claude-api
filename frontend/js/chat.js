// ==================== Chat 模块 ====================

import * as API from './api.js';
import { showToast } from './ui.js';
import { formatChatDate, scrollToBottom, highlightCode, renderMarkdown } from './utils.js';

/**
 * Chat功能Mixin
 * @author ygw
 */
export const chatMixin = {
    data() {
        return {
            chatSessions: [],
            chatActiveSessionId: null,
            chatModel: '',  // 默认模型从接口获取
            chatModels: [], // 可用模型列表
            chatInputText: '',
            chatAbortController: null,
            chatStreaming: false,
            showClearSessionModal: false,
            showDeleteSessionModal: false,
            showClearAllSessionsModal: false,
            deleteSessionId: null,
            modelSelectOpen: false,
            _chatScrollThrottleTimer: null
        };
    },

    computed: {
        chatCurrentSession() {
            return this.chatSessions.find(s => s.id === this.chatActiveSessionId);
        },
        chatMessages() {
            return this.chatCurrentSession?.messages || [];
        },
        // 根据当前选中的模型判断是否启用 think 模式
        isThinkModeEnabled() {
            const model = this.chatModels.find(m => m.id === this.chatModel);
            return model?.thinking || false;
        },
        // 获取实际请求使用的模型（如果是 think 模型，使用 baseModel）
        actualRequestModel() {
            const model = this.chatModels.find(m => m.id === this.chatModel);
            return model?.baseModel || this.chatModel;
        }
    },

    methods: {
        formatChatDate,
        renderChatMarkdown: renderMarkdown,

        /**
         * 初始化模型列表
         * @author ygw
         */
        async initializeChatModels() {
            try {
                const models = await API.fetchModels();
                if (models && models.length > 0) {
                    this.chatModels = models;
                    // 设置默认模型
                    const defaultModel = models.find(m => m.default);
                    if (defaultModel && !this.chatModel) {
                        this.chatModel = defaultModel.id;
                    } else if (!this.chatModel && models.length > 0) {
                        this.chatModel = models[0].id;
                    }
                }
            } catch (error) {
                console.error('获取模型列表失败:', error);
                // 使用备用默认值
                this.chatModels = [
                    {id: 'claude-opus-4.5-think', name: 'Claude Opus 4.5 (Think)', description: '最强推理+深度思考', default: true, thinking: true, baseModel: 'claude-opus-4.5'}
                ];
                this.chatModel = 'claude-opus-4.5-think';
            }
        },

        /**
         * 选择模型
         */
        selectModel(model) {
            this.chatModel = model;
            this.modelSelectOpen = false;
        },

        /**
         * 复制消息内容
         */
        async handleCopyMessage(content, isMarkdown) {
            const textToCopy = isMarkdown ? content : this.stripMarkdown(content);

            try {
                if (navigator.clipboard && window.isSecureContext) {
                    await navigator.clipboard.writeText(textToCopy);
                } else {
                    const textarea = document.createElement('textarea');
                    textarea.value = textToCopy;
                    textarea.style.position = 'fixed';
                    textarea.style.opacity = '0';
                    document.body.appendChild(textarea);
                    textarea.select();
                    document.execCommand('copy');
                    document.body.removeChild(textarea);
                }
                showToast(this, isMarkdown ? '已复制Markdown格式' : '已复制内容', 'success');
            } catch (err) {
                showToast(this, '复制失败', 'error');
            }
        },

        /**
         * 去除Markdown格式
         */
        stripMarkdown(markdown) {
            // 简单的Markdown转纯文本
            return markdown
                .replace(/#{1,6}\s/g, '') // 标题
                .replace(/\*\*(.+?)\*\*/g, '$1') // 粗体
                .replace(/\*(.+?)\*/g, '$1') // 斜体
                .replace(/`(.+?)`/g, '$1') // 行内代码
                .replace(/```[\s\S]*?```/g, (match) => {
                    // 代码块保留内容
                    return match.replace(/```\w*\n?/g, '').replace(/```$/g, '');
                })
                .replace(/\[(.+?)\]\(.+?\)/g, '$1') // 链接
                .replace(/!\[(.+?)\]\(.+?\)/g, '$1') // 图片
                .trim();
        },

        /**
         * 重试发送消息
         */
        async handleRetryMessage() {
            const session = this.chatCurrentSession;
            if (!session || session.messages.length === 0) return;

            // 找到最后一条用户消息
            const lastUserMessageIndex = session.messages.length - 1;
            const lastUserMessage = session.messages[lastUserMessageIndex];

            if (lastUserMessage.role !== 'user') return;

            // 删除最后一条用户消息和之后的AI回复（如果有）
            session.messages = session.messages.slice(0, lastUserMessageIndex);
            this.saveChatSessions();

            // 重新设置输入框内容并发送
            this.chatInputText = lastUserMessage.content;
            await this.$nextTick();
            await this.handleSendChatMessage();
        },

        /**
         * 重试AI回答（重新生成）
         */
        async handleRetryAssistantMessage(messageIndex) {
            const session = this.chatCurrentSession;
            if (!session || this.chatStreaming) return;

            // 找到这条AI消息对应的用户消息（前一条）
            const userMessageIndex = messageIndex - 1;
            if (userMessageIndex < 0 || session.messages[userMessageIndex]?.role !== 'user') return;

            const userMessage = session.messages[userMessageIndex];

            // 删除从用户消息开始的所有后续消息
            session.messages = session.messages.slice(0, userMessageIndex);
            this.saveChatSessions();

            // 重新发送用户消息
            this.chatInputText = userMessage.content;
            await this.$nextTick();
            await this.handleSendChatMessage();
        },

        initializeChatSessions() {
            const sessions = JSON.parse(localStorage.getItem('chat_sessions_v2') || '[]');
            this.chatSessions = sessions;

            const currentId = localStorage.getItem('current_chat_session_v2');
            this.chatActiveSessionId = currentId;

            if (this.chatSessions.length === 0) {
                this.handleCreateChatSession();
            } else if (!currentId || !this.chatSessions.find(s => s.id === currentId)) {
                this.chatActiveSessionId = this.chatSessions[0].id;
            }

            // 初始化模型列表
            this.initializeChatModels();
        },

        handleCreateChatSession() {
            const sessionCount = this.chatSessions.length + 1;
            const newSession = {
                id: Date.now().toString(),
                title: `会话 ${sessionCount}`,
                date: new Date().toISOString(),
                messages: []
            };
            this.chatSessions.unshift(newSession);
            this.chatActiveSessionId = newSession.id;
            this.saveChatSessions();
        },

        handleLoadChatSession(sessionId) {
            this.chatActiveSessionId = sessionId;
            localStorage.setItem('current_chat_session_v2', sessionId);
            this.$nextTick(() => {
                this.scrollChatToBottom();
                this.highlightChatCode();
            });
        },

        handleDeleteChatSession(sessionId) {
            this.showDeleteSessionConfirm(sessionId);
        },

        confirmDeleteChatSession(sessionId) {
            const index = this.chatSessions.findIndex(s => s.id === sessionId);
            if (index === -1) return;

            this.chatSessions.splice(index, 1);

            if (this.chatActiveSessionId === sessionId) {
                if (this.chatSessions.length > 0) {
                    this.chatActiveSessionId = this.chatSessions[0].id;
                } else {
                    this.handleCreateChatSession();
                }
            }

            this.saveChatSessions();
            this.closeDeleteSessionConfirm();
            showToast(this, '会话已删除', 'success');
        },

        handleClearChatSession() {
            this.showClearSessionConfirm();
        },

        confirmClearChatSession() {
            const session = this.chatCurrentSession;
            if (session) {
                session.messages = [];
                this.saveChatSessions();
                this.closeClearSessionConfirm();
                showToast(this, '当前会话已清空', 'success');
            }
        },

        showClearSessionConfirm() {
            this.showClearSessionModal = true;
        },

        closeClearSessionConfirm() {
            this.showClearSessionModal = false;
        },

        showDeleteSessionConfirm(sessionId) {
            this.deleteSessionId = sessionId;
            this.showDeleteSessionModal = true;
        },

        closeDeleteSessionConfirm() {
            this.showDeleteSessionModal = false;
            this.deleteSessionId = null;
        },

        handleClearAllChatSessions() {
            this.showClearAllSessionsModal = true;
        },

        confirmClearAllChatSessions() {
            this.chatSessions = [];
            this.chatActiveSessionId = null;
            this.saveChatSessions();

            this.handleCreateChatSession();

            this.closeClearAllSessionsConfirm();
            showToast(this, '所有会话已清空', 'success');
        },

        closeClearAllSessionsConfirm() {
            this.showClearAllSessionsModal = false;
        },

        async handleSendChatMessage() {
            const content = this.chatInputText.trim();
            if (!content) return;
            if (this.chatStreaming) return;

            const session = this.chatCurrentSession;
            if (!session) return;

            session.messages.push({ role: 'user', content });

            if (session.messages.length === 1) {
                session.title = content.slice(0, 30) + (content.length > 30 ? '...' : '');
            }
            session.date = new Date().toISOString();

            this.saveChatSessions();
            this.chatInputText = '';

            this.$nextTick(() => {
                this.scrollChatToBottom();
            });

            // 根据模型判断是否启用 think 模式
            const thinkEnabled = this.isThinkModeEnabled;

            // Show initial thinking indicator
            const thinkingMsg = {
                role: 'assistant',
                content: '',
                thinking: '',
                hasThinking: thinkEnabled,
                isThinking: thinkEnabled,  // Show "Thinking..." loading state
                thinkingExpanded: true,  // Auto-expand when thinking
                showAnswer: false  // 先不展示回答框，等有回答增量再展示
            };
            session.messages.push(thinkingMsg);

            try {
                // 获取 Chat 配置
                const chatConfig = await API.getChatConfig();

                // 构建请求（保留最近 10 条上下文，排除占位消息）
                const claudeMessages = session.messages
                    .slice(-10, -1)
                    .map(msg => ({
                        role: msg.role,
                        content: msg.content
                    }));

                // 使用实际请求模型（think 模型使用 baseModel）
                const response = await API.claudeMessagesConsole(
                    this.actualRequestModel,
                    claudeMessages,
                    thinkEnabled
                );

                const aiMessage = session.messages[session.messages.length - 1];
                let aiContent = '';
                let thinkingContent = '';
                let streamFinished = false;

                this.chatAbortController = new AbortController();
                this.chatStreaming = true;

                await API.consumeSSEStream(response, {
                    onMeta: () => {
                        if (thinkEnabled) {
                            aiMessage.hasThinking = true;
                            aiMessage.isThinking = true;
                            aiMessage.showAnswer = false;
                        }
                    },
                    onThinking: (payload = {}) => {
                        const delta = payload.text || payload.thinking || '';
                        if (!delta) return;
                        aiMessage.hasThinking = true;
                        aiMessage.isThinking = true;
                        aiMessage.thinkingExpanded = true;
                        thinkingContent += delta;
                        aiMessage.thinking = thinkingContent;
                        this.$nextTick(() => this.scrollChatToBottom());
                    },
                    onAnswer: (payload = {}) => {
                        const delta = payload.text || '';
                        if (!delta) return;
                        aiContent += delta;
                        aiMessage.content = aiContent;
                        aiMessage.isThinking = false;
                        // 有回答增量才展示回答框
                        aiMessage.showAnswer = aiContent.length > 0;
                        this.$nextTick(() => this.scrollChatToBottom());
                    },
                    onDone: () => {
                        aiMessage.isThinking = false;
                        // 有回答内容才展示回答框
                        aiMessage.showAnswer = aiContent.length > 0;
                        this.chatStreaming = false;
                        this.chatAbortController = null;
                        this.saveChatSessions();
                        this.$nextTick(() => {
                            this.scrollChatToBottom();
                            this.highlightChatCode();
                        });
                        streamFinished = true;
                    },
                    onError: (payload = {}) => {
                        const msg = payload.message || '流式请求失败';
                        throw new Error(msg);
                    }
                }, { signal: this.chatAbortController.signal, timeoutMs: chatConfig.timeoutMs });

                if (!aiContent) {
                    throw new Error('No response content');
                }

                if (!streamFinished) {
                    aiMessage.isThinking = false;
                    aiMessage.showAnswer = aiContent.length > 0;
                    this.saveChatSessions();
                    this.$nextTick(() => {
                        this.scrollChatToBottom();
                        this.highlightChatCode();
                    });
                }

            } catch (error) {
                session.messages.pop();
                session.messages.push({ role: 'assistant', content: '❌ 请求失败: ' + error.message });
                this.saveChatSessions();
                showToast(this, '请求失败: ' + error.message, 'error');
            } finally {
                this.chatStreaming = false;
                this.chatAbortController = null;
            }
        },

        handleStopStreaming() {
            if (this.chatAbortController) {
                this.chatAbortController.abort();
            }
            this.chatStreaming = false;
        },

        saveChatSessions() {
            localStorage.setItem('chat_sessions_v2', JSON.stringify(this.chatSessions));
        },

        scrollChatToBottom() {
            // 节流：100ms 内只执行一次
            if (this._chatScrollThrottleTimer) return;
            this._chatScrollThrottleTimer = setTimeout(() => {
                this._chatScrollThrottleTimer = null;
            }, 100);
            const container = this.$refs.chatMessagesContainer;
            scrollToBottom(container);
        },

        highlightChatCode() {
            this.$nextTick(() => {
                highlightCode();
            });
        },

        /**
         * Toggle thinking block expansion
         */
        toggleThinkingBlock(message) {
            message.thinkingExpanded = !message.thinkingExpanded;
        }
    }
};
