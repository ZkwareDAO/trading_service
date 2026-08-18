#!/bin/bash
# API测试脚本（带结果验证）
# PMS运行在8080端口，UOS运行在8081端口

PMS_URL="http://localhost:8080"
UOS_URL="http://localhost:8081"

GREEN='\033[0;32m'
RED='\033[0;31r'
YELLOW='\033[1;33m'
NC='\033[0m'

PASS=0
FAIL=0

check_success() {
    local response="$1"
    # 检查多种成功标志
    if echo "$response" | jq -e '.code == 0' > /dev/null 2>&1; then
        return 0
    elif echo "$response" | jq -e '.status == "healthy"' > /dev/null 2>&1; then
        return 0
    elif echo "$response" | jq -e '.msg == "rule created"' > /dev/null 2>&1; then
        return 0
    elif echo "$response" | jq -e '.id > 0' > /dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

test_api() {
    local test_name="$1"
    local url="$2"
    local should_fail="${3:-false}"

    echo -n "测试: $test_name ... "

    response=$(curl -s "$url" 2>&1)

    if check_success "$response"; then
        if [ "$should_fail" = "true" ]; then
            echo -e "${RED}❌ FAIL (预期失败但成功了)${NC}"
            FAIL=$((FAIL + 1))
        else
            echo -e "${GREEN}✅ PASS${NC}"
            PASS=$((PASS + 1))
        fi
    else
        if [ "$should_fail" = "true" ]; then
            echo -e "${GREEN}✅ PASS (预期失败)${NC}"
            PASS=$((PASS + 1))
        else
            echo -e "${RED}❌ FAIL${NC}"
            FAIL=$((FAIL + 1))
        fi
    fi
}

test_post() {
    local test_name="$1"
    local url="$2"
    local data="$3"
    local should_fail="${4:-false}"

    echo -n "测试: $test_name ... "

    response=$(curl -s -X POST "$url" \
        -H "Content-Type: application/json" \
        -d "$data" 2>&1)

    if check_success "$response"; then
        if [ "$should_fail" = "true" ]; then
            echo -e "${RED}❌ FAIL (预期失败但成功了)${NC}"
            FAIL=$((FAIL + 1))
        else
            echo -e "${GREEN}✅ PASS${NC}"
            PASS=$((PASS + 1))
        fi
    else
        if [ "$should_fail" = "true" ]; then
            echo -e "${GREEN}✅ PASS (预期失败)${NC}"
            PASS=$((PASS + 1))
        else
            echo -e "${RED}❌ FAIL${NC}"
            FAIL=$((FAIL + 1))
        fi
    fi
}

echo "=========================================="
echo "Trading Service API 自动化测试"
echo "=========================================="
echo ""

echo "=== 1. 健康检查 ==="
test_api "PMS健康检查" "$PMS_URL/health"
test_api "UOS健康检查" "$UOS_URL/health"
echo ""

echo "=== 2. 用户创建 (UOS:8081) ==="
test_post "创建新用户" "$UOS_URL/api/v1/users/create" \
    '{"name":"test_'$(date +%s)'","exchange":"binance","api_key":"x","api_secret":"y"}'
test_post "缺少参数" "$UOS_URL/api/v1/users/create" \
    '{"name":"test"}' true
echo ""

echo "=== 3. 用户策略 (UOS:8081) ==="
test_api "查询所有策略" "$UOS_URL/api/v1/user-strategies"
test_api "按user_id查询" "$UOS_URL/api/v1/user-strategies?user_id=1"
echo ""

echo "=== 4. 用户持仓 (PMS:8080) ==="
test_api "查询持仓" "$PMS_URL/api/v1/user-positions?page=1&page_size=10"
test_api "按交易所查询" "$PMS_URL/api/v1/user-positions?exchange=binance"
echo ""

echo "=== 5. 规则查询 (PMS:8080) ==="
test_api "按user_id查询" "$PMS_URL/api/v1/rules?user_id=1"
echo ""

echo "=== 6. 规则创建 (PMS:8080) ==="
test_post "创建规则" "$PMS_URL/api/v1/rules" \
    '{"user_strategy_id":100,"condition_name":"roi","operator":"<=","value":-0.3,"sort":1,"action":"reduce"}'
echo ""

echo "=========================================="
echo -e "${YELLOW}测试结果汇总${NC}"
echo "=========================================="
echo -e "总计: $((PASS + FAIL)) 个测试"
echo -e "${GREEN}通过: $PASS${NC}"
echo -e "${RED}失败: $FAIL${NC}"

if [ $FAIL -eq 0 ]; then
    echo -e "${GREEN}✅ 所有测试通过！${NC}"
    exit 0
else
    echo -e "${RED}❌ 有 $FAIL 个测试失败${NC}"
    exit 1
fi