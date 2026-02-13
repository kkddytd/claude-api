#!/bin/bash

# =============================================================================
# Claude API Server - macOS 应用修复工具
# =============================================================================
# 用途: 修复 macOS 提示"应用已损坏"的问题
# 作者: @author ygw
# =============================================================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 应用路径
APP_NAME="Claude API Server"
APP_PATH="/Applications/$APP_NAME.app"

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# 打印帮助信息
print_help() {
    echo ""
    echo "=============================================="
    echo "  Claude API Server - macOS 应用修复工具"
    echo "=============================================="
    echo ""
    echo "用途:"
    echo "  修复 macOS 提示'应用已损坏'的问题"
    echo ""
    echo "用法:"
    echo "  $0 [选项]"
    echo ""
    echo "选项:"
    echo "  -p, --path PATH    指定应用路径(默认: /Applications/Claude API Server.app)"
    echo "  -h, --help         显示帮助信息"
    echo ""
    echo "示例:"
    echo "  $0                                    # 修复默认位置的应用"
    echo "  $0 -p ~/Desktop/Claude\\ API\\ Server.app  # 修复指定位置的应用"
    echo ""
}

# 解析命令行参数
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -p|--path)
                APP_PATH="$2"
                shift 2
                ;;
            -h|--help)
                print_help
                exit 0
                ;;
            *)
                log_error "未知参数: $1"
                print_help
                exit 1
                ;;
        esac
    done
}

# 检查应用是否存在
check_app() {
    if [ ! -d "$APP_PATH" ]; then
        log_error "找不到应用: $APP_PATH"
        echo ""
        echo "请确保:"
        echo "  1. 应用已安装到 Applications 文件夹"
        echo "  2. 应用名称正确"
        echo ""
        echo "如果应用在其他位置,请使用 -p 参数指定路径:"
        echo "  $0 -p /path/to/Claude\\ API\\ Server.app"
        echo ""
        exit 1
    fi
    log_success "找到应用: $APP_PATH"
}

# 显示当前隔离属性
show_quarantine_status() {
    log_info "检查当前隔离属性..."
    echo ""

    local attrs=$(xattr "$APP_PATH" 2>/dev/null || echo "")

    if [ -z "$attrs" ]; then
        echo "  当前状态: 无隔离属性 ✓"
    else
        echo "  当前隔离属性:"
        echo "$attrs" | sed 's/^/    /'
    fi
    echo ""
}

# 清除隔离属性
remove_quarantine() {
    log_info "清除隔离属性..."

    # 清除扩展属性
    xattr -cr "$APP_PATH"

    if [ $? -eq 0 ]; then
        log_success "隔离属性已清除"
        return 0
    else
        log_error "清除失败"
        return 1
    fi
}

# 验证修复结果
verify_fix() {
    log_info "验证修复结果..."
    echo ""

    local attrs=$(xattr "$APP_PATH" 2>/dev/null || echo "")

    if [ -z "$attrs" ]; then
        log_success "验证通过: 应用已修复"
        echo ""
        echo "现在可以正常打开 $APP_NAME 了!"
        return 0
    else
        log_warn "验证失败: 仍存在扩展属性"
        echo ""
        echo "剩余属性:"
        echo "$attrs" | sed 's/^/  /'
        return 1
    fi
}

# 提供其他解决方案
show_alternatives() {
    echo ""
    echo "=============================================="
    echo "  其他解决方案"
    echo "=============================================="
    echo ""
    echo "如果上述方法无效,可以尝试以下方案:"
    echo ""
    echo "方案 1: 使用系统设置"
    echo "  1. 尝试打开应用"
    echo "  2. 打开'系统设置' > '隐私与安全性'"
    echo "  3. 找到被阻止的应用,点击'仍要打开'"
    echo ""
    echo "方案 2: 临时允许任何来源(不推荐)"
    echo "  sudo spctl --master-disable"
    echo "  (使用后记得重新启用: sudo spctl --master-enable)"
    echo ""
    echo "方案 3: 重新下载应用"
    echo "  从官方渠道重新下载最新版本"
    echo ""
}

# 主函数
main() {
    parse_args "$@"

    echo ""
    echo "=============================================="
    echo "  Claude API Server - macOS 应用修复工具"
    echo "=============================================="
    echo ""

    # 检查应用
    check_app

    # 显示当前状态
    show_quarantine_status

    # 清除隔离属性
    remove_quarantine

    # 验证结果
    if verify_fix; then
        echo ""
        echo "=============================================="
        echo "  修复完成!"
        echo "=============================================="
        echo ""
        exit 0
    else
        show_alternatives
        exit 1
    fi
}

# 运行主函数
main "$@"
