#!/bin/bash
# Claude Server 启动脚本

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# 自动检测可执行文件（支持 claude-server 或 claude-server-* 格式）
EXE_FILE=""
for f in claude-server claude-server-*; do
    if [ -f "$f" ] && [ -x "$f" ] && [[ ! "$f" =~ \.(sh|bat|md|log|pid)$ ]]; then
        EXE_FILE="$f"
        break
    fi
done

if [ -z "$EXE_FILE" ]; then
    echo "错误：未找到 claude-server* 可执行文件"
    echo "请确保可执行文件与此脚本在同一目录下"
    exit 1
fi

echo "检测到可执行文件: $EXE_FILE"

# 检查是否已在运行
if [ -f "claude-server.pid" ]; then
    PID=$(cat claude-server.pid)
    if ps -p $PID > /dev/null 2>&1; then
        echo "Claude Server 已在运行 (PID: $PID)"
        exit 1
    fi
    rm -f claude-server.pid
fi

# 启动服务
echo "正在启动 Claude Server..."
nohup ./"$EXE_FILE" > claude-server.log 2>&1 &
PID=$!
echo $PID > claude-server.pid

sleep 2

# 检查是否启动成功
if ps -p $PID > /dev/null 2>&1; then
    echo "Claude Server 启动成功 (PID: $PID)"
    echo "日志文件: $SCRIPT_DIR/claude-server.log"
    echo "访问地址: http://localhost:62311"
else
    echo "Claude Server 启动失败，请查看日志: $SCRIPT_DIR/claude-server.log"
    rm -f claude-server.pid
    exit 1
fi
