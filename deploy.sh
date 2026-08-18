#!/bin/bash
set -euo pipefail

# Go path (adjust on target host if needed)
export PATH="$PATH:/usr/local/go/bin"

DATA_DIR="${DATA_DIR:-./data}"
LOG_DIR="${LOG_DIR:-./logs}"
POSITION_MONITOR_ORDER_SERVICE_URL="${POSITION_MONITOR_ORDER_SERVICE_URL:-http://localhost:8081}"
POSITION_MONITOR_URL="${POSITION_MONITOR_URL:-http://localhost:8080}"

# Testnet/Mainnet configuration
# Default: true for testnet, false for mainnet
# Override via environment variables: BINANCE_TESTNET=false HYPERLIQUID_TESTNET=false DERIBIT_TESTNET=false ./deploy.sh
BINANCE_TESTNET="${BINANCE_TESTNET:-false}"  # Changed default to mainnet
HYPERLIQUID_TESTNET="${HYPERLIQUID_TESTNET:-false}"  # Changed default to mainnet
DERIBIT_TESTNET="${DERIBIT_TESTNET:-false}"  # Changed default to mainnet

# Force resync symbol filters from exchange APIs (fix corrupted CSV)
FORCE_RESYNC_FILTERS="${FORCE_RESYNC_FILTERS:-false}"

CONFIG_FILE="${CONFIG_FILE:-config.yaml}"

if [ -z "${ORDER_SERVICE_CONFIG:-}" ] && [ -f "$CONFIG_FILE" ]; then
  ORDER_SERVICE_CONFIG="$CONFIG_FILE"
fi
if [ -z "${POSITION_MONITOR_CONFIG:-}" ] && [ -f "$CONFIG_FILE" ]; then
  POSITION_MONITOR_CONFIG="$CONFIG_FILE"
fi

mkdir -p "$LOG_DIR" "$DATA_DIR" "$DATA_DIR/.compact"

create_csv_if_missing() {
  local file="$1"
  local header="$2"
  local seed_row="${3:-}"
  local path="$DATA_DIR/$file"

  if [ -f "$path" ]; then
    echo "exists:  $path"
    return
  fi

  printf '%s\n' "$header" > "$path"
  if [ -n "$seed_row" ]; then
    printf '%s\n' "$seed_row" >> "$path"
  fi
  echo "created: $path"
}

init_csv_files() {
  local now="${DEPLOY_SEED_TIME:-2026-06-22T00:00:00Z}"

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

echo "=== Build services ==="
make build

echo ""
echo "=== Initialize CSV files ==="
init_csv_files

echo ""
echo "=== Force resync symbol filters ==="
if [ "$FORCE_RESYNC_FILTERS" = "true" ]; then
  if [ -f "$DATA_DIR/exchange_symbol_filters.csv" ]; then
    echo "Removing exchange_symbol_filters.csv to force resync from exchange APIs"
    rm "$DATA_DIR/exchange_symbol_filters.csv"
  else
    echo "exchange_symbol_filters.csv not found (will be created on startup)"
  fi
else
  echo "Skip (set FORCE_RESYNC_FILTERS=true to force resync)"
fi

echo ""
echo "=== Stop existing services ==="
pkill -f "bin/user_order_service" 2>/dev/null || true
pkill -f "bin/position_monitor_service" 2>/dev/null || true
sleep 1

echo ""
echo "=== Start position_monitor_service ==="
if [ -n "${POSITION_MONITOR_CONFIG:-}" ]; then
  env DATA_DIR="$DATA_DIR" \
    POSITION_MONITOR_ORDER_SERVICE_URL="$POSITION_MONITOR_ORDER_SERVICE_URL" \
    POSITION_MONITOR_CONFIG="$POSITION_MONITOR_CONFIG" \
    BINANCE_TESTNET="$BINANCE_TESTNET" \
    HYPERLIQUID_TESTNET="$HYPERLIQUID_TESTNET" \
    DERIBIT_TESTNET="$DERIBIT_TESTNET" \
    nohup ./bin/position_monitor_service >> "$LOG_DIR/position_monitor.log" 2>&1 &
else
  env DATA_DIR="$DATA_DIR" \
    POSITION_MONITOR_ORDER_SERVICE_URL="$POSITION_MONITOR_ORDER_SERVICE_URL" \
    BINANCE_TESTNET="$BINANCE_TESTNET" \
    HYPERLIQUID_TESTNET="$HYPERLIQUID_TESTNET" \
    DERIBIT_TESTNET="$DERIBIT_TESTNET" \
    nohup ./bin/position_monitor_service >> "$LOG_DIR/position_monitor.log" 2>&1 &
fi
echo "position_monitor_service started (PID=$!)"

echo ""
echo "=== Start user_order_service ==="
if [ -n "${ORDER_SERVICE_CONFIG:-}" ]; then
  env ORDER_SERVICE_CONFIG="$ORDER_SERVICE_CONFIG" \
    ORDER_SERVICE_DATA_DIR="$DATA_DIR" \
    POSITION_MONITOR_URL="$POSITION_MONITOR_URL" \
    BINANCE_TESTNET="$BINANCE_TESTNET" \
    HYPERLIQUID_TESTNET="$HYPERLIQUID_TESTNET" \
    DERIBIT_TESTNET="$DERIBIT_TESTNET" \
    nohup ./bin/user_order_service >> "$LOG_DIR/user_order.log" 2>&1 &
else
  env ORDER_SERVICE_DATA_DIR="$DATA_DIR" \
    POSITION_MONITOR_URL="$POSITION_MONITOR_URL" \
    BINANCE_TESTNET="$BINANCE_TESTNET" \
    HYPERLIQUID_TESTNET="$HYPERLIQUID_TESTNET" \
    DERIBIT_TESTNET="$DERIBIT_TESTNET" \
    nohup ./bin/user_order_service >> "$LOG_DIR/user_order.log" 2>&1 &
fi
echo "user_order_service started (PID=$!)"

sleep 2
echo ""
echo "=== Service status ==="
ps aux | grep "bin/.*_service" | grep -v grep || echo "no services running"

echo ""
echo "=== Logs ==="
echo "  tail -f $LOG_DIR/position_monitor.log"
echo "  tail -f $LOG_DIR/user_order.log"
echo ""
echo "If you use real exchanges, edit $DATA_DIR/users.csv with API credentials and restart."
