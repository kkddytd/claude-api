// ==================== 工具函数 ====================

/**
 * 初始化水印
 */
export function initWatermark(theme) {
    const watermarkText = 'Claude 无限畅享版';

    const generateWatermarkBg = () => {
        const canvas = document.createElement('canvas');
        const ctx = canvas.getContext('2d');
        canvas.width = 200;
        canvas.height = 120;

        // 从 DOM 读取当前主题，确保切换主题时使用最新值
        const currentTheme = document.documentElement.getAttribute('data-theme') || 'light';
        const isDark = currentTheme === 'dark';

        ctx.font = '14px "PingFang SC", "Microsoft YaHei", sans-serif';
        ctx.fillStyle = isDark ? 'rgba(255, 255, 255, 0.08)' : 'rgba(0, 0, 0, 0.08)';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.translate(canvas.width / 2, canvas.height / 2);
        ctx.rotate(-20 * Math.PI / 180);
        ctx.fillText(watermarkText, 0, 0);

        return canvas.toDataURL('image/png');
    };

    const container = document.getElementById('watermark-container');
    if (container) {
        container.style.backgroundImage = `url(${generateWatermarkBg()})`;
    }

    return generateWatermarkBg;
}

/**
 * 格式化日期
 */
export function formatChatDate(dateStr) {
    return new Date(dateStr).toLocaleDateString();
}

/**
 * 获取账号短ID
 */
export function getAccountShortId(accountId) {
    if (!accountId) return '';
    return accountId.substring(0, 6).toUpperCase();
}

/**
 * 加密邮箱显示
 */
export function maskEmail(email) {
    if (!email || !email.includes('@')) return email;
    const [name, domain] = email.split('@');
    if (name.length <= 3) return name[0] + '***@' + domain;
    return name.slice(0, 3) + '***@' + domain;
}

/**
 * 格式化客户端IP，将本地IP显示为友好名称
 */
export function formatClientIP(ip) {
    if (!ip) return '';
    if (ip === '::1' || ip === '127.0.0.1' || ip === 'localhost') {
        return '本地';
    }
    return ip;
}

/**
 * 下载文件
 */
export function downloadFile(content, filename, type = 'application/json') {
    const blob = new Blob([content], { type });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
}

/**
 * 生成时间戳
 */
export function generateTimestamp() {
    const now = new Date();
    return `${now.getFullYear()}${String(now.getMonth()+1).padStart(2,'0')}${String(now.getDate()).padStart(2,'0')}_${String(now.getHours()).padStart(2,'0')}${String(now.getMinutes()).padStart(2,'0')}${String(now.getSeconds()).padStart(2,'0')}`;
}

/**
 * 滚动到底部
 */
export function scrollToBottom(container) {
    if (container) {
        container.scrollTop = container.scrollHeight;
    }
}

/**
 * 高亮代码块
 */
export function highlightCode() {
    if (typeof hljs !== 'undefined') {
        // 配置 highlight.js 忽略未转义 HTML 警告（内容来自可信的 Markdown 渲染）
        hljs.configure({ ignoreUnescapedHTML: true });

        document.querySelectorAll('pre code').forEach((block) => {
            // 跳过已经高亮过的代码块
            if (block.dataset.highlighted === 'yes') return;
            // 高亮代码块
            hljs.highlightElement(block);
        });
    }
}

/**
 * 渲染Markdown
 */
export function renderMarkdown(content) {
    if (typeof marked === 'undefined') return content;
    return marked.parse(content);
}

/**
 * 生成符合 Claude Code API 格式的 API Key
 * 格式: sk-<random>
 * @author ygw
 */
export function generateClaudeAPIKey() {
    const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';
    let key = '';
    for (let i = 0; i < 43; i++) {
        key += chars.charAt(Math.floor(Math.random() * chars.length));
    }
    return `sk-${key}`;
}
