#!/bin/bash

# =============================================================================
# Claude API Server - DMG 打包脚本
# =============================================================================
# 用途: 将 macOS 应用打包成 DMG 镜像文件,方便分发
# 作者: @author ygw
# =============================================================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 项目路径
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DESKTOP_DIR="$PROJECT_ROOT/desktop"
BUILD_DIR="$DESKTOP_DIR/build/bin"
DIST_DIR="$PROJECT_ROOT/dist/desktop"
TEMP_DIR="$(mktemp -d)"

# 应用信息
APP_NAME="Claude API Server"
APP_FILE="$BUILD_DIR/$APP_NAME.app"
DMG_NAME="Claude-API-Server-macOS-Installer"
VOLUME_NAME="Claude API Server"

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

# 清理函数
cleanup() {
    if [ -d "$TEMP_DIR" ]; then
        log_info "清理临时文件..."
        rm -rf "$TEMP_DIR"
    fi
}

trap cleanup EXIT

# 清理遗留的 DMG 挂载
cleanup_stale_mounts() {
    # 检查是否有遗留的 Claude API Server 卷
    if mount | grep -q "/Volumes/$VOLUME_NAME"; then
        log_warn "发现遗留的 DMG 挂载，正在清理..."
        
        # 尝试通过 hdiutil info 获取所有相关的磁盘镜像并卸载
        local disks=$(hdiutil info 2>/dev/null | grep -B20 "$VOLUME_NAME" | grep "^/dev/disk" | awk '{print $1}' | sort -u)
        
        for disk in $disks; do
            # 获取父磁盘（去掉分区号）
            local parent_disk=$(echo "$disk" | sed 's/s[0-9]*$//')
            hdiutil detach "$parent_disk" -force 2>/dev/null || true
        done
        
        sleep 2
        
        # 如果还没清理干净，强制卸载挂载点
        if mount | grep -q "/Volumes/$VOLUME_NAME"; then
            diskutil unmount force "/Volumes/$VOLUME_NAME" 2>/dev/null || true
            sleep 1
        fi
    fi
}

# 检查应用是否存在
check_app() {
    if [ ! -d "$APP_FILE" ]; then
        log_error "找不到应用文件: $APP_FILE"
        log_info "请先运行 ./build.sh desktop 构建应用"
        exit 1
    fi
    log_success "找到应用文件: $APP_FILE"
}

# 创建 DMG 内容
create_dmg_content() {
    log_info "准备 DMG 内容..."

    # 创建临时目录结构
    local dmg_content="$TEMP_DIR/dmg-content"
    mkdir -p "$dmg_content"

    # 禁止 Spotlight 索引（在源目录创建）
    touch "$dmg_content/.metadata_never_index"

    # 复制应用到临时目录
    log_info "复制应用文件..."
    cp -R "$APP_FILE" "$dmg_content/" 2>&1 > /dev/null

    # 创建 Applications 符号链接(方便用户拖拽安装)
    log_info "创建 Applications 符号链接..."
    ln -s /Applications "$dmg_content/Applications" 2>&1 > /dev/null

    # 创建安装说明文件
    log_info "创建安装说明..."
    cat > "$dmg_content/安装说明.txt" << 'EOF'
Claude API Server 安装说明
==========================

安装步骤:
1. 将 "Claude API Server.app" 拖拽到 "Applications" 文件夹
2. 首次打开时,如果提示"已损坏",请按照以下步骤操作:

方法一: 使用终端命令(推荐)
   打开终端,执行以下命令:
   xattr -cr "/Applications/Claude API Server.app"

方法二: 使用系统设置
   1. 打开"系统设置" > "隐私与安全性"
   2. 找到被阻止的应用,点击"仍要打开"

方法三: 临时允许任何来源
   在终端执行: sudo spctl --master-disable
   (不推荐,会降低系统安全性)

注意事项:
- 本应用未经过 Apple 公证,因此会触发安全警告
- 这是正常现象,不影响应用功能
- 如有问题,请访问项目主页获取帮助

项目主页: https://github.com/your-repo/claude-api
EOF

    # 创建自动修复脚本
    log_info "创建自动修复脚本..."
    cat > "$dmg_content/修复应用.command" << 'EOF'
#!/bin/bash

echo "======================================"
echo "  Claude API Server 自动修复工具"
echo "======================================"
echo ""

APP_PATH="/Applications/Claude API Server.app"

if [ ! -d "$APP_PATH" ]; then
    echo "错误: 找不到应用,请先将应用拖拽到 Applications 文件夹"
    echo ""
    read -p "按回车键退出..."
    exit 1
fi

echo "正在清除隔离属性..."
xattr -cr "$APP_PATH"

if [ $? -eq 0 ]; then
    echo ""
    echo "✓ 修复成功!"
    echo ""
    echo "现在可以正常打开 Claude API Server 了"
else
    echo ""
    echo "✗ 修复失败"
    echo ""
    echo "请尝试手动执行以下命令:"
    echo "  xattr -cr \"$APP_PATH\""
fi

echo ""
read -p "按回车键退出..."
EOF

    chmod +x "$dmg_content/修复应用.command" 2>&1 > /dev/null

    log_success "DMG 内容准备完成"

    # 只返回目录路径,不包含日志输出
    return 0
}

# 创建 DMG 镜像（简化版本，不需要挂载）
create_dmg_image() {
    local dmg_content="$1"
    local dmg_final="$DIST_DIR/$DMG_NAME.dmg"

    mkdir -p "$DIST_DIR"

    # 删除已存在的 DMG
    if [ -f "$dmg_final" ]; then
        log_warn "删除已存在的 DMG: $dmg_final"
        rm -f "$dmg_final"
    fi

    log_info "创建 DMG 镜像..."

    # 直接创建压缩的只读 DMG（跳过挂载步骤）
    # 使用 UDZO 格式（压缩的只读）直接从源文件夹创建
    if hdiutil create -volname "$VOLUME_NAME" \
        -srcfolder "$dmg_content" \
        -ov -format UDZO \
        -imagekey zlib-level=9 \
        "$dmg_final" > /dev/null 2>&1; then
        log_success "DMG 创建完成: $dmg_final"
    else
        log_error "DMG 创建失败"
        exit 1
    fi

    # 显示文件大小
    local size=$(ls -lh "$dmg_final" | awk '{print $5}')
    log_info "文件大小: $size"
}

# 主函数
main() {
    echo ""
    echo "=============================================="
    echo "  Claude API Server - DMG 打包工具"
    echo "=============================================="
    echo ""

    # 清理遗留挂载
    cleanup_stale_mounts

    # 检查应用
    check_app

    # 创建 DMG 内容
    create_dmg_content
    local dmg_content="$TEMP_DIR/dmg-content"

    # 创建 DMG 镜像
    create_dmg_image "$dmg_content"

    echo ""
    echo "=============================================="
    echo "  打包完成!"
    echo "=============================================="
    echo ""
    echo "DMG 文件位置:"
    echo "  $DIST_DIR/$DMG_NAME.dmg"
    echo ""
    echo "用户安装步骤:"
    echo "  1. 双击打开 DMG 文件"
    echo "  2. 将应用拖拽到 Applications 文件夹"
    echo "  3. 如果提示'已损坏',双击'修复应用.command'"
    echo ""
}

# 运行主函数
main "$@"
