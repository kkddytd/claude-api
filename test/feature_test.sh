#!/bin/bash

# 功能测试脚本 - 测试新增的账号管理、IP管理等功能
# @author ygw

BASE_URL="http://localhost:62311"
ADMIN_PWD="admin123"  # 请替换为实际的管理员密码

echo "====================================="
echo "      功能测试脚本 - CLAUDE-API-Go"
echo "====================================="
echo ""

# 1. 测试获取账号列表（带状态筛选）
echo "[TEST 1] 测试账号列表（带状态筛选）"
echo ">>> GET /v2/accounts?status=all"
curl -s -H "Authorization: Bearer $ADMIN_PWD" "$BASE_URL/v2/accounts?status=all" | head -c 200
echo ""
echo ""

# 2. 测试按状态筛选账号
echo "[TEST 2] 测试按状态筛选账号"
for status in normal disabled suspended exhausted expired; do
    echo ">>> GET /v2/accounts?status=$status"
    result=$(curl -s -H "Authorization: Bearer $ADMIN_PWD" "$BASE_URL/v2/accounts?status=$status" | jq -r '.count // .pagination.total // 0')
    echo "    Status: $status - Count: $result"
done
echo ""

# 3. 测试批量启用账号
echo "[TEST 3] 测试批量启用账号 API"
echo ">>> POST /v2/accounts/enable-all"
curl -s -X POST \
    -H "Authorization: Bearer $ADMIN_PWD" \
    -H "X-Test-Password: test123" \
    "$BASE_URL/v2/accounts/enable-all" | jq .
echo ""

# 4. 测试批量禁用账号
echo "[TEST 4] 测试批量禁用账号 API"
echo ">>> POST /v2/accounts/disable-all"
curl -s -X POST \
    -H "Authorization: Bearer $ADMIN_PWD" \
    -H "X-Test-Password: test123" \
    "$BASE_URL/v2/accounts/disable-all" | jq .
echo ""

# 5. 测试获取访问 IP 列表（含用户关联和时间段统计）
echo "[TEST 5] 测试获取访问 IP 列表"
echo ">>> GET /v2/ips/visitors"
curl -s -H "Authorization: Bearer $ADMIN_PWD" "$BASE_URL/v2/ips/visitors" | jq '.ips[:3] // .[:3]'
echo ""

# 6. 测试获取用户关联的 IP 列表
echo "[TEST 6] 测试获取用户关联 IP 列表"
# 首先获取用户列表
users=$(curl -s -H "Authorization: Bearer $ADMIN_PWD" "$BASE_URL/v2/users" | jq -r '.[0].id // empty')
if [ -n "$users" ]; then
    echo ">>> GET /v2/users/$users/ips"
    curl -s -H "Authorization: Bearer $ADMIN_PWD" "$BASE_URL/v2/users/$users/ips" | jq .
else
    echo "    (没有用户数据，跳过此测试)"
fi
echo ""

# 7. 测试系统设置 - 检查账号选择方式
echo "[TEST 7] 测试系统设置 - 账号选择方式"
echo ">>> GET /v2/settings"
curl -s -H "Authorization: Bearer $ADMIN_PWD" "$BASE_URL/v2/settings" | jq '{accountSelectionMode: .accountSelectionMode, supportedModes: .supportedAccountSelectionModes}'
echo ""

# 8. 测试 IP 限流设置
echo "[TEST 8] 测试 IP 访问控制设置"
curl -s -H "Authorization: Bearer $ADMIN_PWD" "$BASE_URL/v2/settings" | jq '{enableIPRateLimit: .enableIPRateLimit, ipRateLimitMax: .ipRateLimitMax, ipRateLimitWindow: .ipRateLimitWindow}'
echo ""

echo "====================================="
echo "            测试完成"
echo "====================================="
