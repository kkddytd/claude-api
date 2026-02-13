// ==================== UI 工具函数 ====================

let toastTimer = null;

/**
 * 显示Toast通知
 */
export function showToast(app, message, type = 'info') {
    if (toastTimer) {
        clearTimeout(toastTimer);
    }

    app.toastMessage = message;
    app.toastType = type;
    app.toastVisible = true;

    toastTimer = setTimeout(() => {
        app.toastVisible = false;
    }, 3000);
}

/**
 * 获取Toast图标类
 */
export function getToastIconClass(type) {
    const iconMap = {
        success: 'ri-checkbox-circle-line',
        error: 'ri-error-warning-line',
        info: 'ri-information-line',
        warning: 'ri-error-warning-line'
    };
    return iconMap[type] || iconMap.info;
}

/**
 * 应用主题
 */
export function applyTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme);

    // 切换代码高亮主题
    const lightStyleEl = document.getElementById('hljs-light');
    const darkStyleEl = document.getElementById('hljs-dark');
    if (theme === 'dark') {
        if (lightStyleEl) lightStyleEl.disabled = true;
        if (darkStyleEl) darkStyleEl.disabled = false;
    } else {
        if (lightStyleEl) lightStyleEl.disabled = false;
        if (darkStyleEl) darkStyleEl.disabled = true;
    }
}
