#!/bin/bash

# =============================================================================
# Claude Server 统一构建脚本
# =============================================================================
# 用法:
#   ./build.sh                  # 构建所有组件（当前平台）
#   ./build.sh server           # 仅构建后端服务
#   ./build.sh desktop          # 仅构建桌面应用（macOS + Windows）
#   ./build.sh license-server   # 仅构建授权验证服务
#   ./build.sh all              # 构建所有平台的所有组件
#   ./build.sh clean            # 清理构建缓存和产物
#
# 参数:
#   -v, --version VERSION       指定版本号（默认从 git tag 获取）
#   -p, --platform PLATFORM     指定目标平台（darwin/amd64, linux/arm64 等）
#   --no-cache                  禁用构建缓存
#   --verbose                   显示详细日志
#
# 环境变量:
#   BUILD_VERSION               版本号
#   BUILD_PLATFORM              目标平台
# =============================================================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 项目路径
PROJECT_ROOT="$(cd "$(dirname "$0")" && pwd)"
DIST_DIR="$PROJECT_ROOT/dist"
CACHE_DIR="$PROJECT_ROOT/.build-cache"
DESKTOP_DIR="$PROJECT_ROOT/desktop"

# 默认配置
VERSION=""
TARGET=""
PLATFORM=""
NO_CACHE=false
VERBOSE=false
CURRENT_OS=$(uname -s | tr '[:upper:]' '[:lower:]')
CURRENT_ARCH=$(uname -m)

# 转换架构名称
case "$CURRENT_ARCH" in
    x86_64)  CURRENT_ARCH="amd64" ;;
    aarch64) CURRENT_ARCH="arm64" ;;
esac

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_verbose() {
    if [ "$VERBOSE" = true ]; then
        echo -e "${BLUE}[DEBUG]${NC} $1"
    fi
}

# 打印帮助信息
print_help() {
    echo "Claude Server 统一构建脚本"
    echo ""
    echo "用法:"
    echo "  $0 [target] [options]"
    echo ""
    echo "目标 (target):"
    echo "  server          仅构建后端服务 (claude-server)"
    echo "  desktop         仅构建桌面应用 (macOS + Windows)"
    echo "  license-server  仅构建授权验证服务 (需要 CGO)"
    echo "  all             构建所有平台的所有组件"
    echo "  clean           清理构建缓存和产物"
    echo "  (空)            构建当前平台的 server 和 desktop"
    echo ""
    echo "选项:"
    echo "  -v, --version VERSION   指定版本号 (默认: git tag 或 dev)"
    echo "  -p, --platform PLATFORM 指定目标平台 (如: darwin/arm64, windows/amd64)"
    echo "  --no-cache              禁用构建缓存"
    echo "  --verbose               显示详细日志"
    echo "  -h, --help              显示帮助信息"
    echo ""
    echo "示例:"
    echo "  $0                              # 构建当前平台"
    echo "  $0 server -v v1.2.0             # 构建指定版本的 server"
    echo "  $0 desktop -p darwin/arm64      # 构建 macOS ARM64 桌面应用"
    echo "  $0 all                          # 构建所有平台"
}

# 解析命令行参数
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -v|--version)
                VERSION="$2"
                shift 2
                ;;
            -p|--platform)
                PLATFORM="$2"
                shift 2
                ;;
            --no-cache)
                NO_CACHE=true
                shift
                ;;
            --verbose)
                VERBOSE=true
                shift
                ;;
            -h|--help)
                print_help
                exit 0
                ;;
            server|desktop|license-server|all|clean)
                TARGET="$1"
                shift
                ;;
            *)
                log_error "未知参数: $1"
                print_help
                exit 1
                ;;
        esac
    done
}

# 获取版本号
get_version() {
    if [ -n "$VERSION" ]; then
        echo "$VERSION"
    elif [ -n "$BUILD_VERSION" ]; then
        echo "$BUILD_VERSION"
    elif git describe --tags --abbrev=0 &>/dev/null; then
        git describe --tags --abbrev=0
    else
        echo "dev"
    fi
}

# 计算源文件哈希（用于缓存检测）
calculate_source_hash() {
    local component=$1
    local hash_file="$CACHE_DIR/${component}.hash"
    local new_hash=""
    
    case "$component" in
        server)
            new_hash=$(find "$PROJECT_ROOT" -name "*.go" -not -path "*/desktop/*" -not -path "*/tools/*" -not -path "*/.build-cache/*" | sort | xargs cat 2>/dev/null | md5sum | cut -d' ' -f1)
            ;;
        desktop)
            new_hash=$(find "$DESKTOP_DIR" -name "*.go" -o -name "*.html" -o -name "*.js" -o -name "*.css" 2>/dev/null | sort | xargs cat 2>/dev/null | md5sum | cut -d' ' -f1)
            ;;
        license-server)
            new_hash=$(cat "$PROJECT_ROOT/tools/license-verify-server/main.go" 2>/dev/null | md5sum | cut -d' ' -f1)
            ;;
    esac
    
    echo "$new_hash"
}

# 检查是否需要重新构建
needs_rebuild() {
    local component=$1
    local platform=$2
    local cache_file="$CACHE_DIR/${component}-${platform//\//-}.hash"
    
    if [ "$NO_CACHE" = true ]; then
        return 0
    fi
    
    if [ ! -f "$cache_file" ]; then
        return 0
    fi
    
    local old_hash=$(cat "$cache_file" 2>/dev/null)
    local new_hash=$(calculate_source_hash "$component")
    
    if [ "$old_hash" != "$new_hash" ]; then
        return 0
    fi
    
    log_info "[$component-$platform] 源文件未变更，跳过构建"
    return 1
}

# 更新缓存
update_cache() {
    local component=$1
    local platform=$2
    local hash=$(calculate_source_hash "$component")
    
    mkdir -p "$CACHE_DIR"
    echo "$hash" > "$CACHE_DIR/${component}-${platform//\//-}.hash"
}

# 前端资源处理（已移除哈希功能，直接使用原始文件）
process_frontend_assets() {
    log_info "前端资源准备就绪（使用原始文件）"
}

# 构建 Server（后端服务）
build_server() {
    local goos=$1
    local goarch=$2
    local version=$(get_version)
    local platform="$goos/$goarch"
    local output_name="claude-server-$goos-$goarch"
    
    if [ "$goos" = "windows" ]; then
        output_name="$output_name.exe"
    fi
    
    # 检查缓存
    if ! needs_rebuild "server" "$platform"; then
        return 0
    fi
    
    log_info "构建 Server [$platform]..."
    
    cd "$PROJECT_ROOT"
    
    export GOOS=$goos
    export GOARCH=$goarch
    export CGO_ENABLED=0
    
    mkdir -p "$DIST_DIR/server"
    
    go build -ldflags="-s -w -X main.Version=$version" \
        -o "$DIST_DIR/server/$output_name" .
    
    # 打包
    cd "$DIST_DIR/server"
    if [ "$goos" = "windows" ]; then
        zip -q "${output_name%.exe}.zip" "$output_name"
        rm "$output_name"
        log_success "Server [$platform] -> $DIST_DIR/server/${output_name%.exe}.zip"
    else
        tar -czf "$output_name.tar.gz" "$output_name"
        rm "$output_name"
        log_success "Server [$platform] -> $DIST_DIR/server/$output_name.tar.gz"
    fi
    
    update_cache "server" "$platform"
}

# 构建嵌入式后端（给桌面应用用）
build_embedded_backend() {
    local goos=$1
    local goarch=$2
    local version=$(get_version)
    local output_name="claude-server"
    
    if [ "$goos" = "windows" ]; then
        output_name="claude-server.exe"
    fi
    
    log_info "构建嵌入式后端 [$goos/$goarch]..."
    
    cd "$PROJECT_ROOT"
    
    mkdir -p "$DESKTOP_DIR/embedded"
    
    export GOOS=$goos
    export GOARCH=$goarch
    export CGO_ENABLED=0
    
    go build -ldflags="-s -w -X main.Version=$version" \
        -o "$DESKTOP_DIR/embedded/$output_name" .
    
    log_success "嵌入式后端 -> $DESKTOP_DIR/embedded/$output_name"
}

# 检查 Wails 是否安装
check_wails() {
    if ! command -v wails &> /dev/null; then
        log_error "Wails CLI 未安装"
        log_info "请运行: go install github.com/wailsapp/wails/v2/cmd/wails@latest"
        exit 1
    fi
    log_verbose "Wails 版本: $(wails version 2>/dev/null | head -1)"
}

# 构建 Desktop（桌面应用）
build_desktop() {
    local goos=$1
    local goarch=$2
    local version=$(get_version)
    local platform="$goos/$goarch"
    
    # 检查缓存
    if ! needs_rebuild "desktop" "$platform"; then
        return 0
    fi
    
    check_wails
    
    log_info "构建 Desktop [$platform]..."
    
    # 先构建嵌入式后端
    build_embedded_backend "$goos" "$goarch"
    
    cd "$DESKTOP_DIR"
    go mod tidy
    
    mkdir -p "$DIST_DIR/desktop"
    
    # 构建 Wails 应用
    local ldflags="-X main.Version=$version"
    if [ "$goos" = "windows" ]; then
        ldflags="$ldflags -H windowsgui"
    fi
    
    wails build -platform "$platform" -ldflags "$ldflags"
    
    # 打包
    case "$goos" in
        darwin)
            local app_path="$DESKTOP_DIR/build/bin/Claude API Server.app"
            local resources_dir="$app_path/Contents/Resources"

            if [ -d "$app_path" ]; then
                # 复制后端到 app 包内
                mkdir -p "$resources_dir"
                cp "$DESKTOP_DIR/embedded/claude-server" "$resources_dir/claude-server"
                chmod +x "$resources_dir/claude-server"

                # 打包 ZIP
                local zip_name="Claude-API-Server-macOS-$goarch.zip"
                cd "$DESKTOP_DIR/build/bin"
                zip -rq "$DIST_DIR/desktop/$zip_name" "Claude API Server.app"
                log_success "Desktop [$platform] -> $DIST_DIR/desktop/$zip_name"

                # 创建 DMG (仅在当前平台构建时)
                if [ "$goos" = "$CURRENT_OS" ] && [ "$goarch" = "$CURRENT_ARCH" ]; then
                    log_info "创建 DMG 安装包..."
                    if [ -x "$PROJECT_ROOT/scripts/create-dmg.sh" ]; then
                        "$PROJECT_ROOT/scripts/create-dmg.sh"
                    else
                        log_warn "DMG 打包脚本不存在或不可执行: $PROJECT_ROOT/scripts/create-dmg.sh"
                    fi
                fi
            else
                log_error "找不到 macOS 应用: $app_path"
                exit 1
            fi
            ;;
        windows)
            local exe_path="$DESKTOP_DIR/build/bin/Claude API Server.exe"
            if [ -f "$exe_path" ]; then
                local zip_name="Claude-API-Server-Windows-$goarch.zip"
                cd "$DESKTOP_DIR/build/bin"
                zip -jq "$DIST_DIR/desktop/$zip_name" "Claude API Server.exe"
                log_success "Desktop [$platform] -> $DIST_DIR/desktop/$zip_name"
            else
                log_error "找不到 Windows 可执行文件: $exe_path"
                exit 1
            fi
            ;;
    esac
    
    update_cache "desktop" "$platform"
}

# 构建 License Verify Server
build_license_server() {
    local goos=$1
    local goarch=$2
    local version=$(get_version)
    local platform="$goos/$goarch"
    local output_name="license-verify-server-$goos-$goarch"
    
    if [ "$goos" = "windows" ]; then
        output_name="$output_name.exe"
    fi
    
    # 检查缓存
    if ! needs_rebuild "license-server" "$platform"; then
        return 0
    fi
    
    log_info "构建 License Server [$platform]..."
    log_warn "注意: license-verify-server 需要 CGO (sqlite3)，仅支持本机平台构建"
    
    cd "$PROJECT_ROOT/tools/license-verify-server"
    
    # 确保依赖完整 - 先下载再 tidy
    log_info "下载 license-verify-server 依赖..."
    go mod download || log_warn "go mod download 失败，继续尝试构建"
    go mod tidy 2>&1 || log_warn "go mod tidy 失败，继续尝试构建"
    
    mkdir -p "$DIST_DIR/license-server"
    
    # 需要 CGO
    export GOOS=$goos
    export GOARCH=$goarch
    export CGO_ENABLED=1
    
    go build -ldflags="-s -w -X main.Version=$version" \
        -o "$DIST_DIR/license-server/$output_name" .
    
    log_success "License Server [$platform] -> $DIST_DIR/license-server/$output_name"
    
    update_cache "license-server" "$platform"
}

# 构建所有平台的 Server
build_all_servers() {
    local version=$(get_version)
    log_info "开始构建所有平台的 Server (版本: $version)..."
    
    process_frontend_assets
    
    local platforms=(
        "linux/amd64"
        "darwin/amd64"
        "darwin/arm64"
        "windows/amd64"
    )
    
    for platform in "${platforms[@]}"; do
        IFS='/' read -r goos goarch <<< "$platform"
        build_server "$goos" "$goarch"
    done
    
    log_success "所有平台 Server 构建完成"
}

# 构建所有平台的 Desktop
build_all_desktops() {
    local version=$(get_version)
    log_info "开始构建所有平台的 Desktop (版本: $version)..."
    
    case "$CURRENT_OS" in
        darwin)
            # macOS 可以同时构建 macOS 和 Windows 桌面应用
            build_desktop "darwin" "$CURRENT_ARCH"
            build_desktop "windows" "amd64"
            ;;
        mingw*|msys*|cygwin*|windows)
            build_desktop "windows" "amd64"
            ;;
        linux)
            log_warn "Linux 暂不支持 Wails 桌面应用构建"
            ;;
    esac
    
    log_success "Desktop 构建完成"
}

# 清理
clean() {
    log_info "清理构建缓存和产物..."
    rm -rf "$DIST_DIR"
    rm -rf "$CACHE_DIR"
    rm -rf "$DESKTOP_DIR/embedded"
    rm -rf "$DESKTOP_DIR/build/bin"
    log_success "清理完成"
}

# 验证构建产物
verify_build() {
    echo ""
    echo "=============================================="
    echo "  构建产物验证"
    echo "=============================================="
    
    local missing=0
    local found=0
    
    # 预期的产物列表（使用普通数组以兼容 Bash 3.x）
    expected_files=()
    
    case "$TARGET" in
        server)
            expected_files+=("$DIST_DIR/server/claude-server-$CURRENT_OS-$CURRENT_ARCH.tar.gz")
            if [ "$CURRENT_OS" = "darwin" ]; then
                expected_files+=("$DIST_DIR/server/claude-server-darwin-$CURRENT_ARCH.tar.gz")
            fi
            ;;
        desktop)
            if [ "$CURRENT_OS" = "darwin" ]; then
                expected_files+=("$DIST_DIR/desktop/Claude-API-Server-macOS-$CURRENT_ARCH.zip")
                expected_files+=("$DIST_DIR/desktop/Claude-API-Server-Windows-amd64.zip")
            fi
            ;;
        license-server)
            expected_files+=("$DIST_DIR/license-server/license-verify-server-$CURRENT_OS-$CURRENT_ARCH")
            ;;
        all)
            # Server 所有平台
            expected_files+=("$DIST_DIR/server/claude-server-linux-amd64.tar.gz")
            expected_files+=("$DIST_DIR/server/claude-server-darwin-amd64.tar.gz")
            expected_files+=("$DIST_DIR/server/claude-server-darwin-arm64.tar.gz")
            expected_files+=("$DIST_DIR/server/claude-server-windows-amd64.zip")
            # Desktop
            if [ "$CURRENT_OS" = "darwin" ]; then
                expected_files+=("$DIST_DIR/desktop/Claude-API-Server-macOS-$CURRENT_ARCH.zip")
                expected_files+=("$DIST_DIR/desktop/Claude-API-Server-Windows-amd64.zip")
            fi
            # License Server
            expected_files+=("$DIST_DIR/license-server/license-verify-server-$CURRENT_OS-$CURRENT_ARCH")
            ;;
        *)
            # 默认构建
            if [ "$CURRENT_OS" = "darwin" ]; then
                expected_files+=("$DIST_DIR/server/claude-server-darwin-$CURRENT_ARCH.tar.gz")
                expected_files+=("$DIST_DIR/desktop/Claude-API-Server-macOS-$CURRENT_ARCH.zip")
                expected_files+=("$DIST_DIR/desktop/Claude-API-Server-Windows-amd64.zip")
            fi
            ;;
    esac
    
    echo ""
    echo "检查构建产物:"
    echo ""
    
    for file in "${expected_files[@]}"; do
        if [ -f "$file" ]; then
            local size=$(ls -lh "$file" 2>/dev/null | awk '{print $5}')
            echo -e "  ${GREEN}✓${NC} $(basename "$file") ($size)"
            ((found++))
        else
            echo -e "  ${RED}✗${NC} $(basename "$file") - 缺失"
            ((missing++))
        fi
    done
    
    echo ""
    echo "----------------------------------------------"
    echo "  总计: $found 个成功, $missing 个缺失"
    echo "----------------------------------------------"
    
    # 列出 dist 目录的所有内容
    echo ""
    echo "dist/ 目录完整内容:"
    echo ""
    if [ -d "$DIST_DIR" ]; then
        find "$DIST_DIR" -type f | while read f; do
            local size=$(ls -lh "$f" 2>/dev/null | awk '{print $5}')
            echo "  - $(echo "$f" | sed "s|$DIST_DIR/||") ($size)"
        done
    else
        echo "  (目录不存在)"
    fi
    echo ""
    
    if [ $missing -gt 0 ]; then
        log_error "有 $missing 个构建产物缺失！"
        return 1
    fi
    
    log_success "所有构建产物验证通过！"
    return 0
}

# 主函数
main() {
    parse_args "$@"
    
    local version=$(get_version)
    
    echo ""
    echo "=============================================="
    echo "  Claude Server 统一构建系统"
    echo "  版本: $version"
    echo "  平台: $CURRENT_OS/$CURRENT_ARCH"
    echo "=============================================="
    echo ""
    
    mkdir -p "$DIST_DIR"
    mkdir -p "$CACHE_DIR"
    
    case "$TARGET" in
        clean)
            clean
            return 0
            ;;
        server)
            process_frontend_assets
            if [ -n "$PLATFORM" ]; then
                IFS='/' read -r goos goarch <<< "$PLATFORM"
                build_server "$goos" "$goarch"
            else
                build_server "$CURRENT_OS" "$CURRENT_ARCH"
            fi
            ;;
        desktop)
            if [ -n "$PLATFORM" ]; then
                IFS='/' read -r goos goarch <<< "$PLATFORM"
                build_desktop "$goos" "$goarch"
            else
                # 默认构建 macOS 和 Windows
                if [ "$CURRENT_OS" = "darwin" ]; then
                    build_desktop "darwin" "$CURRENT_ARCH"
                    build_desktop "windows" "amd64"
                else
                    build_desktop "$CURRENT_OS" "$CURRENT_ARCH"
                fi
            fi
            ;;
        license-server)
            if [ -n "$PLATFORM" ]; then
                IFS='/' read -r goos goarch <<< "$PLATFORM"
                build_license_server "$goos" "$goarch"
            else
                build_license_server "$CURRENT_OS" "$CURRENT_ARCH"
            fi
            ;;
        all)
            build_all_servers
            build_all_desktops
            # license-server 仅构建当前平台
            build_license_server "$CURRENT_OS" "$CURRENT_ARCH"
            ;;
        *)
            # 默认：构建当前平台的 server 和 desktop (包括 Windows 交叉编译)
            process_frontend_assets
            build_server "$CURRENT_OS" "$CURRENT_ARCH"
            if [ "$CURRENT_OS" = "darwin" ]; then
                build_desktop "darwin" "$CURRENT_ARCH"
                build_desktop "windows" "amd64"
            else
                build_desktop "$CURRENT_OS" "$CURRENT_ARCH"
            fi
            ;;
    esac
    
    # 验证构建产物
    verify_build
    
    echo ""
    echo "=============================================="
    echo "  构建完成!"
    echo "=============================================="
    echo ""
}

# 运行主函数
main "$@"
