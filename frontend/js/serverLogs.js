// ==================== 服务日志模块 ====================

export const serverLogsMixin = {
    data() {
        return {
            // 服务日志状态
            serverLogs: [],
            serverLogsConnected: false,
            serverLogsAutoScroll: true,
            serverLogsFilter: '',
            serverLogsLevel: 'all', // all, info, warn, error
            serverLogsEventSource: null,
            serverLogsMaxLines: 1000,
            serverLogsProgrammaticScroll: false, // 标记是否为程序触发的滚动
            logLevelSelectOpen: false,
            serverLogsRetryCount: 0, // 重连次数
            serverLogsMaxRetries: 10, // 最大重连次数
            serverLogsRetryTimer: null, // 重连定时器
            serverLogsDestroyed: false, // 组件是否已销毁
            _scrollThrottleTimer: null // 滚动节流定时器
        };
    },

    computed: {
        filteredServerLogs() {
            let logs = this.serverLogs;

            // 按级别过滤
            if (this.serverLogsLevel !== 'all') {
                logs = logs.filter(log => log.level === this.serverLogsLevel);
            }

            // 按关键词过滤
            if (this.serverLogsFilter.trim()) {
                const keyword = this.serverLogsFilter.toLowerCase();
                logs = logs.filter(log =>
                    log.message.toLowerCase().includes(keyword) ||
                    (log.source && log.source.toLowerCase().includes(keyword))
                );
            }

            return logs;
        }
    },

    methods: {
        // 连接服务日志 SSE
        connectServerLogs() {
            // 如果组件已销毁，不再连接
            if (this.serverLogsDestroyed) return;

            if (this.serverLogsEventSource) {
                this.serverLogsEventSource.close();
            }

            const token = localStorage.getItem('adminPassword');
            const es = new EventSource(`/v2/server-logs/stream?token=${encodeURIComponent(token)}`);
            this.serverLogsEventSource = es;

            // 保存事件处理函数引用，以便后续移除
            this._onOpen = () => {
                this.serverLogsConnected = true;
                this.serverLogsRetryCount = 0;
            };

            this._onConnected = () => {
                this.serverLogsConnected = true;
                this.serverLogsRetryCount = 0;
            };

            this._onLog = (event) => {
                this.serverLogsConnected = true;
                this.serverLogsRetryCount = 0;
                const rawLog = event.data;
                const log = this.parseLogLine(rawLog);
                if (log) {
                    this.serverLogs.push(log);

                    // 限制日志数量
                    if (this.serverLogs.length > this.serverLogsMaxLines) {
                        this.serverLogs = this.serverLogs.slice(-this.serverLogsMaxLines);
                    }

                    // 自动滚动
                    if (this.serverLogsAutoScroll) {
                        this.scrollServerLogsToBottom();
                    }
                }
            };

            this._onError = () => {
                this.serverLogsConnected = false;

                // 如果组件已销毁，不再重连
                if (this.serverLogsDestroyed) return;

                // 检查重连次数
                if (this.serverLogsRetryCount >= this.serverLogsMaxRetries) {
                    console.warn('[服务日志] 达到最大重连次数，停止重连');
                    return;
                }

                this.serverLogsRetryCount++;
                const delay = Math.min(3000 * this.serverLogsRetryCount, 30000);

                // 清除之前的重连定时器
                if (this.serverLogsRetryTimer) {
                    clearTimeout(this.serverLogsRetryTimer);
                }

                this.serverLogsRetryTimer = setTimeout(() => {
                    if (!this.serverLogsDestroyed) {
                        this.connectServerLogs();
                    }
                }, delay);
            };

            es.onopen = this._onOpen;
            es.addEventListener('connected', this._onConnected);
            es.addEventListener('log', this._onLog);
            es.onerror = this._onError;
        },

        // 断开服务日志连接
        disconnectServerLogs() {
            // 清除重连定时器
            if (this.serverLogsRetryTimer) {
                clearTimeout(this.serverLogsRetryTimer);
                this.serverLogsRetryTimer = null;
            }

            if (this.serverLogsEventSource) {
                // 移除事件监听器
                if (this._onConnected) {
                    this.serverLogsEventSource.removeEventListener('connected', this._onConnected);
                }
                if (this._onLog) {
                    this.serverLogsEventSource.removeEventListener('log', this._onLog);
                }
                this.serverLogsEventSource.close();
                this.serverLogsEventSource = null;
            }
            this.serverLogsConnected = false;
            this.serverLogsRetryCount = 0;
        },

        // 清空日志
        clearServerLogs() {
            this.serverLogs = [];
        },

        // 导出日志
        exportServerLogs() {
            if (this.serverLogs.length === 0) return;
            
            const lines = this.serverLogs.map(log => {
                const time = log.timestamp ? new Date(log.timestamp).toLocaleString('zh-CN') : '';
                const level = log.level ? `[${log.level.toUpperCase()}]` : '';
                const source = log.source ? `[${log.source}]` : '';
                return `${time} ${level} ${source} ${log.message}`;
            });
            
            const content = lines.join('\n');
            const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `server-logs-${new Date().toISOString().slice(0, 19).replace(/[T:]/g, '-')}.log`;
            a.click();
            URL.revokeObjectURL(url);
        },

        // 处理用户手动滚动，自动关闭自动滚动
        handleServerLogsScroll(event) {
            // 如果是程序触发的滚动，忽略
            if (this.serverLogsProgrammaticScroll) {
                return;
            }
            const container = event.target;
            // 判断是否滚动到底部（允许 5px 误差）
            const isAtBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 5;
            
            // 如果用户向上滚动（不在底部），关闭自动滚动
            if (!isAtBottom && this.serverLogsAutoScroll) {
                this.serverLogsAutoScroll = false;
            }
        },

        // 切换自动滚动状态
        toggleServerLogsAutoScroll() {
            this.serverLogsAutoScroll = !this.serverLogsAutoScroll;
            // 如果开启自动滚动，立即滚动到底部
            if (this.serverLogsAutoScroll) {
                this.scrollServerLogsToBottom();
            }
        },

        // 滚动服务日志到底部（带节流）
        scrollServerLogsToBottom() {
            // 节流：100ms 内只执行一次
            if (this._scrollThrottleTimer) return;
            this._scrollThrottleTimer = setTimeout(() => {
                this._scrollThrottleTimer = null;
            }, 100);

            const container = this.$refs.serverLogsContainer;
            if (container) {
                this.serverLogsProgrammaticScroll = true;
                container.scrollTop = container.scrollHeight;
                setTimeout(() => {
                    this.serverLogsProgrammaticScroll = false;
                }, 50);
            }
        },

        // 获取日志级别样式
        getLogLevelClass(level) {
            switch (level) {
                case 'error': return 'log-level--error';
                case 'warn': return 'log-level--warn';
                case 'info': return 'log-level--info';
                case 'debug': return 'log-level--debug';
                default: return '';
            }
        },

        // 高亮日志消息中的关键词
        highlightLogMessage(message) {
            if (!message) return '';
            let html = message
                .replace(/&/g, '&amp;')
                .replace(/</g, '&lt;')
                .replace(/>/g, '&gt;');
            
            // HTTP 方法和路径高亮
            html = html.replace(/\b(GET|POST|PUT|PATCH|DELETE|OPTIONS)\s+(\/[\w\/-]*)/g, '<span class="log-hl-method">$1</span> <span class="log-hl-path">$2</span>');
            // IP 地址高亮
            html = html.replace(/\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b/g, '<span class="log-hl-ip">$1</span>');
            // 模型名称高亮
            html = html.replace(/(claude-[\w.-]+|auto)/gi, '<span class="log-hl-model">$1</span>');
            // 耗时高亮
            html = html.replace(/(Duration[：:]\s*[\d.]+\s*[msµn]*s?|耗时[：:]\s*[\d.]+\s*[ms秒]+|[\d.]+ms\b)/gi, '<span class="log-hl-duration">$1</span>');
            // 状态码高亮 (4xx, 5xx 错误)
            html = html.replace(/(Status[：:]\s*)([45]\d{2})\b/g, '$1<span class="log-hl-status">$2</span>');
            // 状态码高亮 (2xx 成功)
            html = html.replace(/(Status[：:]\s*)([23]\d{2})\b/g, '$1<span class="log-hl-success">$2</span>');
            // Token 数量高亮
            html = html.replace(/(\d+)\s*(tokens?|个事件)/gi, '<span class="log-hl-token">$1</span> $2');
            
            return html;
        },

        // 格式化日志时间
        formatLogTimestamp(timestamp) {
            if (!timestamp) return '';
            const date = new Date(timestamp);
            return date.toLocaleTimeString('zh-CN', {
                hour: '2-digit',
                minute: '2-digit',
                second: '2-digit',
                hour12: false
            }) + '.' + String(date.getMilliseconds()).padStart(3, '0');
        },

        // 解析日志行（格式: [LEVEL] 2024/01/01 12:00:00 file.go:123: message）
        parseLogLine(line) {
            if (!line || !line.trim()) return null;

            // 匹配格式: [LEVEL] YYYY/MM/DD HH:MM:SS file.go:line: message
            const match = line.match(/^\[(\w+)\]\s+(\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2}:\d{2})\s+([^:]+:\d+):\s*(.*)$/);
            if (match) {
                const [, level, timeStr, source, message] = match;
                // 解析时间
                const [datePart, timePart] = timeStr.split(' ');
                const [year, month, day] = datePart.split('/');
                const timestamp = new Date(`${year}-${month}-${day}T${timePart}`);

                return {
                    level: level.toLowerCase(),
                    timestamp: timestamp.toISOString(),
                    source: source,
                    message: message
                };
            }

            // 如果不匹配标准格式，作为普通消息处理
            return {
                level: 'info',
                timestamp: new Date().toISOString(),
                source: '',
                message: line.trim()
            };
        }
    },

    // 移除 Tab 切换时的连接/断开逻辑，服务日志在应用启动时自动连接，Tab 切换不影响连接

    beforeUnmount() {
        this.serverLogsDestroyed = true; // 标记组件已销毁
        this.disconnectServerLogs();
        // 清理滚动节流定时器
        if (this._scrollThrottleTimer) {
            clearTimeout(this._scrollThrottleTimer);
            this._scrollThrottleTimer = null;
        }
    }
};
