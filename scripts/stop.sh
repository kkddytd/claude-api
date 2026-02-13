#!/bin/bash
# Claude Server 停止脚本

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

if [ ! -f "claude-server.pid" ]; then
    echo "Claude Server 未在运行（找不到 PID 文件）"
    exit 0
fi

PID=$(cat claude-server.pid)

if ! ps -p $PID > /dev/null 2>&1; then
    echo "Claude Server 进程不存在 (PID: $PID)"
    rm -f claude-server.pid
    exit 0
fi

echo "正在停止 Claude Server (PID: $PID)..."
kill $PID

# 等待进程退出
for i in {1..10}; do
    if ! ps -p $PID > /dev/null 2>&1; then
        echo "Claude Server 已停止"
        rm -f claude-server.pid
        exit 0
    fi
    sleep 1
done

# 强制终止
echo "进程未响应，强制终止..."
kill -9 $PID 2>/dev/null
rm -f claude-server.pid
echo "Claude Server 已强制停止"
