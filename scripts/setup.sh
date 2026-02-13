#!/bin/bash
# Claude Server 一键授权脚本

echo "正在设置权限..."

# 检测操作系统
if [[ "$OSTYPE" == "darwin"* ]]; then
    echo "检测到 macOS 系统，正在解除隔离限制..."
    sudo xattr -dr com.apple.quarantine . 2>/dev/null
    echo "✓ 隔离限制已解除"
fi

# 添加执行权限
chmod +x claude-server-* 2>/dev/null
chmod +x *.sh 2>/dev/null
echo "✓ 执行权限已设置"

echo ""
echo "设置完成！"
echo ""
echo "使用方法："
echo "  启动: ./start.sh"
echo "  停止: ./stop.sh"
echo "  访问: http://localhost:62311"

