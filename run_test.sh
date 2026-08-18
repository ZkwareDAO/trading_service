#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

# 加载 .env（如果存在）
if [ -f .env ]; then
  set -a
  # shellcheck source=/dev/null
  source .env
  set +a
  echo ">>> Loaded .env"
fi

# Binance WS 不支持代理（proxy 不支持 WebSocket CONNECT）
# REST API 无需代理即可访问 testnet
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY 2>/dev/null || true

# ── 1. CSV setup ─────────────────────────────────────────────
mkdir -p data data/.compact

# Initialize CSV files with headers if missing (same as deploy.sh)
init_csv_files() {
  local now="${DEPLOY_SEED_TIME:-2026-06-22T00:00:00Z}"

  create_csv_if_missing() {
    local file="$1"
    local header="$2"
    local seed_row="${3:-}"
    local path="data/$file"

    if [ -f "$path" ]; then
      echo "  exists:  $path"
      return
    fi

    printf '%s\n' "$header" > "$path"
    if [ -n "$seed_row" ]; then
      printf '%s\n' "$seed_row" >> "$path"
    fi
    echo "  created: $path"
  }

  create_csv_if_missing "users.csv" \
    "id,name,exchange,api_key,api_secret,api_password,created_at,updated_at" \
    "1,test_user,mock,,,,${now},${now}"
  create_csv_if_missing "strategies.csv" \
    "id,name,strategy_type,model_name,description,params,created_at,updated_at"
  create_csv_if_missing "strategy_assets.csv" \
    "id,name,asset,strategy_id,pos_type,sort,created_at,updated_at"
  create_csv_if_missing "user_strategies.csv" \
    "id,user_id,name,exchange,valid_before,cash,parts,status,strategy_id,risk_strategy_type,orders_num,created_at,updated_at"
  create_csv_if_missing "user_orders.csv" \
    "id,user_id,user_strategy_id,pos_type,exchange,valid_before,base_asset,quote_asset,quantity,cash,trigger_price,slippage,side,order_type,status,finished_at,created_at,updated_at"
  create_csv_if_missing "leverage_configs.csv" \
    "id,user_id,asset,quote,leverage,exchange,status,pos_type,created_at,updated_at"
  create_csv_if_missing "uprunning_orders.csv" \
    "id,user_id,relation_id,relation_type,risk_control_strategy_id,user_order_position_id,user_position_id,exchange,symbol,pos_type,exchange_order_id,exchange_order_status,exchange_order_price,exchange_order_quantity,exchange_update_time,side,created_at,updated_at"
  create_csv_if_missing "user_order_positions.csv" \
    "id,user_id,uprunning_order_id,user_order_id,user_strategy_id,risk_control_strategy_id,exchange,pos_type,asset,current_price,quantity,pos_value,leverage,deleted,init_margin,pos_price,pnl_value,side,close_time,created_at,updated_at"
  create_csv_if_missing "user_positions.csv" \
    "id,user_id,user_strategy_id,exchange,pos_type,current_price,quantity,latest_market_capitalization,roi,pnl,win_rate,maximum_drawdown,total_margin,max_profit_percentage,max_loss_percentage,open_trades,closed_trades,profit_trades,loss_trades,deleted,close_time,created_at,updated_at,risk_control_strategy_id"
  create_csv_if_missing "exchange_symbol_filters.csv" \
    "id,exchange,pos_type,symbol,filter_type,min_price,max_price,tick_size,min_qty,max_qty,step_size,min_notional"
  create_csv_if_missing "rule.csv" \
    "id,user_strategy_id,condition_name,operator,value,sort,status,action,params"
  create_csv_if_missing "condition.csv" \
    "id,field,operator,value,value_type,params"
  create_csv_if_missing "action.csv" \
    "id,type,params"
}

echo ">>> Initializing CSV files..."
init_csv_files

# ── 2. Build ──────────────────────────────────────────────────
echo ""
echo ">>> Building ..."
make build
echo ">>> Build OK"

# ── 3. Kill existing services ─────────────────────────────────
echo ">>> Cleaning up existing services ..."
for port in 8080 8081; do
  pid=$(lsof -ti ":$port" 2>/dev/null || true)
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
    echo "  killed: $pid (port $port)"
  fi
done
sleep 1

# ── 4. Configuration ───────────────────────────────────────────
: "${DATA_DIR:=./data}"
: "${CONFIG_FILE:=config.yaml}"
: "${POSITION_MONITOR_ORDER_SERVICE_URL:=http://localhost:8081}"
: "${BINANCE_TESTNET:=true}"
: "${HYPERLIQUID_TESTNET:=true}"

# Use shared config.yaml for both services (if exists)
if [ -f "$CONFIG_FILE" ]; then
  ORDER_SERVICE_CONFIG="$CONFIG_FILE"
  POSITION_MONITOR_CONFIG="$CONFIG_FILE"
  echo ">>> Using config: $CONFIG_FILE"
fi

# ── 5. Start services (foreground) ───────────────────────────
echo ""
echo ">>> Starting user_order_service ..."
ORDER_SERVICE_CONFIG="$ORDER_SERVICE_CONFIG" \
ORDER_SERVICE_DATA_DIR="$DATA_DIR" \
BINANCE_TESTNET="$BINANCE_TESTNET" \
HYPERLIQUID_TESTNET="$HYPERLIQUID_TESTNET" \
  bin/user_order_service &
PID_ORDER=$!

echo ">>> Starting position_monitor_service ..."
DATA_DIR="$DATA_DIR" \
POSITION_MONITOR_ORDER_SERVICE_URL="$POSITION_MONITOR_ORDER_SERVICE_URL" \
POSITION_MONITOR_CONFIG="$POSITION_MONITOR_CONFIG" \
BINANCE_TESTNET="$BINANCE_TESTNET" \
HYPERLIQUID_TESTNET="$HYPERLIQUID_TESTNET" \
  bin/position_monitor_service &
PID_MONITOR=$!

echo ">>> Both services running (PID order=$PID_ORDER monitor=$PID_MONITOR)"
echo ">>> Press Ctrl+C to stop"

cleanup() {
  echo ""
  echo ">>> Stopping ..."
  kill "$PID_ORDER" "$PID_MONITOR" 2>/dev/null || true
  wait "$PID_ORDER" "$PID_MONITOR" 2>/dev/null || true
  echo ">>> Done"
}
trap cleanup EXIT

wait
