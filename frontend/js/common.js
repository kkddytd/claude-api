// ==================== 水印功能 ====================
let watermarkInitialized = false;

function initWatermark() {
    // 防止重复初始化
    if (watermarkInitialized) {
        updateWatermarkStyle();
        return;
    }
    watermarkInitialized = true;

    // 水印文字
    const watermarkText = 'Claude 无限畅享版';

    // 创建水印容器
    const container = document.createElement('div');
    container.id = 'watermark-container';

    // 生成水印背景
    function generateWatermarkBg() {
        const canvas = document.createElement('canvas');
        const ctx = canvas.getContext('2d');
        canvas.width = 200;
        canvas.height = 120;

        const isDark = document.documentElement.getAttribute('data-theme') === 'dark';
        ctx.font = '16px "PingFang SC", "Microsoft YaHei", sans-serif';
        ctx.fillStyle = isDark ? 'rgba(255, 255, 255, 0.02)' : 'rgba(0, 0, 0, 0.02)';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.translate(canvas.width / 2, canvas.height / 2);
        ctx.rotate(-20 * Math.PI / 180);
        ctx.fillText(watermarkText, 0, 0);

        return canvas.toDataURL('image/png');
    }

    // 设置样式
    container.style.cssText = `position: fixed; top: 0; left: 0; width: 100%; height: 100%; pointer-events: none; z-index: 99999; background-image: url(${generateWatermarkBg()}); background-repeat: repeat;`;

    // 添加到页面
    document.body.appendChild(container);

    // 保存生成函数供主题切换时使用
    window._watermarkGenerateBg = generateWatermarkBg;
}

// 更新水印样式（主题切换时调用）
function updateWatermarkStyle() {
    const container = document.getElementById('watermark-container');
    if (container && window._watermarkGenerateBg) {
        container.style.backgroundImage = `url(${window._watermarkGenerateBg()})`;
    }
}

// 监听主题变化
let watermarkObserverInitialized = false; // 防止重复创建 MutationObserver

function setupWatermarkThemeListener() {
    if (watermarkObserverInitialized) return; // 防止重复创建
    watermarkObserverInitialized = true;

    const observer = new MutationObserver((mutations) => {
        mutations.forEach((mutation) => {
            if (mutation.attributeName === 'data-theme') {
                updateWatermarkStyle();
            }
        });
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
}

// 初始化水印
document.addEventListener('DOMContentLoaded', () => {
    initWatermark();
    setupWatermarkThemeListener();
});

// 页面加载完成后也初始化（仅当 DOMContentLoaded 未触发时）
if (document.readyState !== 'loading') {
    // 使用 requestAnimationFrame 确保只执行一次
    requestAnimationFrame(() => {
        initWatermark();
        setupWatermarkThemeListener();
    });
}

// ==================== Sidebar Rendering ====================
function renderSidebar(activePage) {
    const sidebarContainer = document.getElementById('sidebar');
    if (!sidebarContainer) return;

    const menuItems = [
        { id: 'accounts', icon: 'ri-file-list-3-line', label: '账号管理', href: '/accounts' },
        { id: 'chat', icon: 'ri-chat-1-line', label: 'Chat 会话', href: '/chat' }
    ];

    sidebarContainer.innerHTML = `
        <div class="logo">
            <div class="logo-icon">Q2</div>
            <div class="logo-text">Claude 无限畅享版</div>
        </div>

        <div class="nav-menu">
            ${menuItems.map(item => `
                <a href="${item.href}" class="nav-item ${item.id === activePage ? 'active' : ''}">
                    <i class="${item.icon}"></i>
                    <span>${item.label}</span>
                </a>
            `).join('')}
        </div>

        <div class="sidebar-footer">
            <div class="user-profile">
                <i class="ri-user-smile-line"></i>
                <span>Admin</span>
            </div>
            <div style="display: flex; gap: 8px;">
                <button class="theme-toggle" id="themeToggle" title="切换主题">
                    <i class="ri-moon-line"></i>
                </button>
                <button class="theme-toggle" onclick="logout()" title="退出登录">
                    <i class="ri-logout-box-r-line"></i>
                </button>
            </div>
        </div>
    `;

    // Re-initialize theme toggle after sidebar is rendered
    initTheme();

    // Show sidebar after it's rendered
    sidebarContainer.classList.add('loaded');
}

// ==================== Theme Management ====================
// 全局标记，防止重复绑定事件
let themeInitialized = false;
let systemThemeListenerAdded = false;

function initTheme() {
    console.log('[Theme] 开始初始化主题...');

    // 先立即应用主题（不需要等待按钮）
    applyInitialTheme();

    // 然后初始化按钮
    initThemeToggleButton();

    // 设置系统主题变化监听
    setupSystemThemeListener();
}

// 应用初始主题
function applyInitialTheme() {
    const savedTheme = localStorage.getItem('theme');
    const systemTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';

    // 如果没有保存的主题，使用系统主题
    const theme = savedTheme || systemTheme;

    document.documentElement.setAttribute('data-theme', theme);

    // 初始化代码高亮主题
    const hljsLight = document.getElementById('hljs-light');
    const hljsDark = document.getElementById('hljs-dark');
    if (hljsLight && hljsDark) {
        hljsLight.disabled = theme === 'dark';
        hljsDark.disabled = theme === 'light';
    }

    console.log('[Theme] 应用主题:', theme, savedTheme ? '(已保存)' : '(跟随系统)');
}

// 初始化主题切换按钮
function initThemeToggleButton() {
    const themeToggle = document.getElementById('themeToggle');
    if (!themeToggle) {
        console.log('[Theme] 未找到主题切换按钮，稍后重试...');
        setTimeout(initThemeToggleButton, 100);
        return;
    }

    const themeIcon = themeToggle.querySelector('i');
    const currentTheme = document.documentElement.getAttribute('data-theme');

    // 更新图标状态
    if (themeIcon) {
        themeIcon.className = currentTheme === 'dark' ? 'ri-sun-line' : 'ri-moon-line';
    }

    // 避免重复绑定事件
    if (themeToggle.dataset.themeInitialized === 'true') {
        console.log('[Theme] 按钮已初始化，跳过');
        return;
    }

    themeToggle.dataset.themeInitialized = 'true';

    // 绑定点击事件
    themeToggle.addEventListener('click', function() {
        const current = document.documentElement.getAttribute('data-theme');
        const newTheme = current === 'dark' ? 'light' : 'dark';
        setTheme(newTheme, true);
    });

    console.log('[Theme] 按钮初始化完成');
}

// 设置系统主题变化监听
function setupSystemThemeListener() {
    if (systemThemeListenerAdded) return;
    systemThemeListenerAdded = true;

    const darkModeMediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    darkModeMediaQuery.addEventListener('change', (e) => {
        // 只在用户没有手动设置主题时自动跟随系统
        if (!localStorage.getItem('theme')) {
            console.log('[Theme] 系统主题变化，自动切换到:', e.matches ? 'dark' : 'light');
            setTheme(e.matches ? 'dark' : 'light', false);
        }
    });
    console.log('[Theme] 系统主题监听已设置');
}

// 设置主题
function setTheme(theme, showToast = true) {
    const themeToggle = document.getElementById('themeToggle');
    const themeIcon = themeToggle?.querySelector('i');

    // 添加切换动画
    if (themeToggle) {
        themeToggle.classList.add('switching');
    }

    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('theme', theme);

    // 切换代码高亮主题
    const hljsLight = document.getElementById('hljs-light');
    const hljsDark = document.getElementById('hljs-dark');
    if (hljsLight && hljsDark) {
        hljsLight.disabled = theme === 'dark';
        hljsDark.disabled = theme === 'light';
    }

    if (themeIcon) {
        themeIcon.className = theme === 'dark' ? 'ri-sun-line' : 'ri-moon-line';
    }

    // 移除动画类
    if (themeToggle) {
        setTimeout(() => {
            themeToggle.classList.remove('switching');
        }, 600);
    }

    // 显示主题切换提示
    if (showToast) {
        showThemeToast(theme);
    }

    console.log('[Theme] 主题已切换到:', theme);
}

// 主题切换提示
function showThemeToast(theme) {
    const existingToast = document.querySelector('.theme-toast');
    if (existingToast) existingToast.remove();

    const toast = document.createElement('div');
    toast.className = 'toast theme-toast info';
    const icon = theme === 'dark' ? 'ri-moon-line' : 'ri-sun-line';
    const text = theme === 'dark' ? '已切换到夜间模式' : '已切换到日间模式';

    toast.innerHTML = `
        <i class="${icon}" style="font-size: 18px;"></i>
        <span>${text}</span>
    `;

    document.body.appendChild(toast);

    // 淡出动画
    setTimeout(() => {
        toast.style.opacity = '0';
        toast.style.transform = 'translateX(-50%) translateY(-30px)';
        setTimeout(() => toast.remove(), 300);
    }, 2000);
}

// Initialize theme on page load
document.addEventListener('DOMContentLoaded', () => {
    console.log('[Theme] 初始化主题系统...');
    initTheme();
});

// 也在页面加载时立即尝试初始化（防止DOMContentLoaded已触发）
if (document.readyState === 'loading') {
    console.log('[Theme] 等待DOM加载...');
} else {
    console.log('[Theme] DOM已加载，立即初始化...');
    setTimeout(initTheme, 100);
}

// ==================== Authentication ====================
function getAuthPassword() {
    return localStorage.getItem('adminPassword');
}

function getAuthHeaders() {
    const password = getAuthPassword();
    if (!password) return {};
    return { 'Authorization': `Bearer ${password}` };
}

async function authFetch(url, options = {}) {
    const headers = { ...getAuthHeaders(), ...options.headers };
    const response = await fetch(url, { ...options, headers });
    if (response.status === 401) {
        localStorage.removeItem('adminPassword');
        window.location.href = '/login';
        throw new Error('Unauthorized');
    }
    return response;
}

function logout() {
    showLogoutConfirmation();
}

function showLogoutConfirmation() {
    // Create modal if it doesn't exist
    let modal = document.getElementById('logoutConfirmModal');
    if (!modal) {
        modal = document.createElement('div');
        modal.id = 'logoutConfirmModal';
        modal.className = 'modal-overlay';
        modal.innerHTML = `
            <div class="modal" style="max-width: 420px;">
                <div class="modal-body">
                    <div class="logout-modal-content">
                        <div class="logout-icon-wrapper">
                            <i class="ri-logout-box-r-line"></i>
                        </div>
                        <div class="logout-text">
                            <h3>确认退出登录</h3>
                            <p>退出后需要重新输入密码才能访问管理中心</p>
                        </div>
                    </div>
                </div>
                <div class="modal-footer">
                    <button class="btn btn-secondary" onclick="closeLogoutConfirmation()">取消</button>
                    <button class="btn btn-primary" onclick="confirmLogout()" style="background: var(--accent-color);">
                        <i class="ri-logout-box-r-line"></i> 确认退出
                    </button>
                </div>
            </div>
        `;
        document.body.appendChild(modal);

        // Close on overlay click
        modal.addEventListener('click', (e) => {
            if (e.target.id === 'logoutConfirmModal') {
                closeLogoutConfirmation();
            }
        });
    }

    modal.classList.add('active');
}

function closeLogoutConfirmation() {
    const modal = document.getElementById('logoutConfirmModal');
    if (modal) {
        modal.classList.remove('active');
    }
}

async function confirmLogout() {
    try {
        await fetch('/api/logout', { method: 'POST' });
    } catch (e) {
        console.error('退出登录失败:', e);
    }
    localStorage.removeItem('adminPassword');
    window.location.href = '/login';
}

// ==================== Toast Notifications ====================
function showToast(message, type = 'info') {
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    const icons = {
        success: 'ri-checkbox-circle-line',
        error: 'ri-error-warning-line',
        info: 'ri-information-line',
        warning: 'ri-alert-line'
    };
    toast.innerHTML = `
        <i class="${icons[type] || icons.info}" style="font-size: 18px;"></i>
        <span>${message}</span>
    `;
    document.body.appendChild(toast);
    setTimeout(() => {
        toast.style.opacity = '0';
        toast.style.transform = 'translateX(-50%) translateY(-20px)';
        setTimeout(() => toast.remove(), 300);
    }, 3000);
}

// ==================== 信息弹窗 ====================
function showInfoModal(title, message, type = 'info', duration = 3000) {
    const existingModal = document.querySelector('.info-modal-overlay');
    if (existingModal) existingModal.remove();
    const overlay = document.createElement('div');
    overlay.className = 'info-modal-overlay';
    const icons = { success: 'ri-checkbox-circle-line', error: 'ri-error-warning-line', info: 'ri-information-line', warning: 'ri-alert-line' };
    overlay.innerHTML = `<div class="info-modal"><div class="info-modal-content"><div class="info-modal-icon ${type}"><i class="${icons[type] || icons.info}"></i></div><div class="info-modal-text"><h3>${title}</h3><p>${message}</p></div><div class="info-modal-progress"><div class="info-modal-progress-bar"></div></div></div></div>`;
    document.body.appendChild(overlay);
    requestAnimationFrame(() => overlay.classList.add('active'));
    const closeModal = () => { overlay.classList.remove('active'); setTimeout(() => overlay.remove(), 300); };
    overlay.addEventListener('click', (e) => { if (e.target === overlay) closeModal(); });
    setTimeout(closeModal, duration);
}

// ==================== Time Formatting ====================
function formatTime(timeString) {
    if (!timeString) return '从未刷新';

    try {
        const time = new Date(timeString);
        if (isNaN(time.getTime())) return '从未刷新';

        const year = time.getFullYear();
        const month = String(time.getMonth() + 1).padStart(2, '0');
        const day = String(time.getDate()).padStart(2, '0');
        const hours = String(time.getHours()).padStart(2, '0');
        const minutes = String(time.getMinutes()).padStart(2, '0');
        const seconds = String(time.getSeconds()).padStart(2, '0');

        return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
    } catch (e) {
        return '从未刷新';
    }
}

// ==================== Modal Helpers ====================
function closeModalOnOverlayClick(modalId) {
    const modal = document.getElementById(modalId);
    if (modal) {
        modal.addEventListener('click', (e) => {
            if (e.target.id === modalId) {
                modal.classList.remove('active');
            }
        });
    }
}
