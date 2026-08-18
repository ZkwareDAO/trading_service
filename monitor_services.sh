#!/bin/bash
# Service Monitor - Auto-restart PMS or UOS if stopped
# Usage: Add to crontab: */2 * * * * /path/to/monitor_services.sh
#
# Environment variables (inherited from crontab or shell):
# - BINANCE_TESTNET (default: false)
# - HYPERLIQUID_TESTNET (default: false)
# - FORCE_RESYNC_FILTERS - MUST be true to avoid service crashes

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

MONITOR_LOG="${LOG_DIR:-./logs}/service_monitor.log"
mkdir -p "$(dirname "$MONITOR_LOG")"

# Force resync filters to avoid service crashes (must be true)
export FORCE_RESYNC_FILTERS=true

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$MONITOR_LOG"
}

# Check if a service is running
is_running() {
    pgrep -f "bin/$1" > /dev/null 2>&1
}

# Check service health via HTTP
is_healthy() {
    local url="$1"
    curl -sf "$url" > /dev/null 2>&1
}

log "=== Service Monitor Check ==="

# Check PMS (position_monitor_service)
if ! is_running position_monitor_service; then
    log "⚠️  PMS not running - calling deploy.sh"
    ./deploy.sh
elif ! is_healthy http://localhost:8080/health; then
    log "⚠️  PMS unhealthy - restarting via deploy.sh"
    pkill -f bin/position_monitor_service || true
    ./deploy.sh
else
    log "✅ PMS healthy (PID=$(pgrep -f bin/position_monitor_service))"
fi

# Check UOS (user_order_service)
if ! is_running user_order_service; then
    log "⚠️  UOS not running - calling deploy.sh"
    ./deploy.sh
elif ! is_healthy http://localhost:8081/health; then
    log "⚠️  UOS unhealthy - restarting via deploy.sh"
    pkill -f bin/user_order_service || true
    ./deploy.sh
else
    log "✅ UOS healthy (PID=$(pgrep -f bin/user_order_service))"
fi

log "=== Check Complete ==="