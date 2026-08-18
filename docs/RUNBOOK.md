# Operations Runbook

This runbook provides operational procedures for deploying, monitoring, and troubleshooting the Trading Service Risk Control System.

<!-- AUTO-GENERATED: Architecture -->
## Architecture

```text
position_monitor_service
│
├── GlobalState (in-memory)
│   ├── Snapshot.Prices (WS prices)
│   └── Positions (aggregated from user_order_positions)
│
├── StateRepository (CSV persistence)
│   ├── users, user_order_positions, user_positions
│   ├── uprunning_orders, exchange_symbol_filters
│
├── RuleStore (rule.csv)
│   ├── status: active / inactive / in_use
│   ├── auto-generate default rules for new strategies
│
├── PriceRuntime (per exchange)
│   ├── Binance price WS
│   ├── Hyperliquid price WS
│   └── Deribit price WS (NEW: dynamic subscription)
│       ├── EnsureSubscribed(): periodic check for new options
│       ├── DeribitOptionExtractor: extract options from positions
│       └── Error resilient: WebSocket failures don't crash service
│
├── UserOrderRuntime (per user)
│   ├── Binance listenKey + user data WS
│   ├── Hyperliquid order WS
│   └── Deribit order WS (position sync on unfound order)
│       ├── Subscribe to user.orders.option.BTC.raw / ETH.raw
│       ├── Heartbeat: public/test every 30s (prevents idle disconnect)
│       ├── Auto-reconnect: up to 5 retries with backoff on read error
│       ├── Goroutine lifecycle: loopStopCh/heartbeatStopCh prevent leaks on reconnect
│       ├── WriteMu: protects concurrent conn.WriteJSON calls
│       ├── HandleDeribitOrderUpdate: parse order status
│       ├── Deribit-specific notification on ANY order state change
│       │   ├── sendDeribitNotification: before common FILLED processing
│       │   ├── buildDeribitAction: composable action text (开/减+买仓/卖仓+suffix)
│       │   ├── findROIForSymbol: ROI lookup via UserOrderPosition→UserPosition
│       │   └── isRiskControlLabel: detect risk/止盈止损/tp_sl labels
│       └── Order not found after retries → SyncDeribitPositions
│
├── SyncLoop (10s interval, single goroutine)
│   ├── 1. SyncPriceSnapshots → WS prices → GlobalState
│   ├── 2. AggregateFromPersistence → user_order_positions → GlobalState.Positions
│   ├── 3. UserPositionSyncer → update user_positions in DB
│   ├── 4. DefaultRuleGenerator → generate missing rules
│   ├── 5. Pipeline.Run → evaluate rules → ActionResult
│   └── 6. RiskActionApplier → exchange.CreateOrder → uprunning_orders
│
└── OrderExecutor (Scanner + WS → HandleOrderFilled)
    ├── Scanner: polls NEW orders → GetOrder → status update
    ├── WS (Binance/Hyperliquid): real-time order updates
    ├── user_orders FILLED → create user_order_positions + RPC
    └── risk_control_strategy FILLED → close positions + rule status

exchange_position_reporter (独立服务, 每小时运行)
│
├── ReadUsers → users.csv
├── FetchExchangePositions → UOS API (reporter.api_url)
├── SavePositionsToCSV → data/exchange_positions/YYYYMMDD/
└── SendWeChatNotification → 企微webhook (markdown格式)

Note: All exchanges (Binance, Deribit, Hyperliquid) return NEW status on CreateOrder.
Scanner handles status updates uniformly via GetOrder polling.
```
<!-- END AUTO-GENERATED -->

<!-- AUTO-GENERATED: Order Status Processing -->
## Order Status Processing

### Unified Status Handling

All exchanges (Binance, Deribit, Hyperliquid) follow a unified order status flow:

1. **CreateOrder** → Always returns `NEW` status
   - Even if immediately filled on exchange
   - Ensures consistent Scanner processing

2. **Scanner** → Polls `NEW` orders every 30s
   - Calls `GetOrder(exchangeOrderID)` to query actual status
   - Updates `uprunning_orders.status` in CSV
   - Triggers `handleFilled` for FILLED orders

3. **handleFilled** → Processes filled orders
   - Creates `user_order_positions` record
   - Calls RPC to update `user_orders.status = 2`
   - Sends notifications

### Exchange-Specific Behavior

| Exchange | CreateOrder Returns | Status Update Mechanism |
|----------|--------------------|------------------------|
| **Binance** | NEW | WebSocket + Scanner fallback |
| **Deribit** | NEW | WebSocket order updates + Position sync on unfound |
| **Hyperliquid** | NEW | WebSocket + Scanner fallback |

### Why Always Return NEW?

**Problem**: Orders filled immediately were skipped by Scanner
- Scanner only processes `NEW` or `open` orders
- `user_orders.status` remained at 1 (NEW)
- `user_order_positions` not created

**Solution**: Always return NEW, let Scanner query actual status
- Uniform behavior across all exchanges
- Scanner handles all status transitions
- No race conditions between CreateOrder and WebSocket

### Verification

Check order processing:
```bash
# View uprunning_orders with NEW status
grep ",NEW," data/uprunning_orders.csv

# Check Scanner logs for status updates
grep "order scanner" logs/position_monitor.log | tail -20

# Verify user_orders status updates
tail -10 data/user_orders.csv
```
<!-- END AUTO-GENERATED -->

<!-- AUTO-GENERATED: Deployment Procedures -->
## Deployment Procedures

### Prerequisites

- Go 1.25+ installed on target server
- Repository checked out on the target server
- Optional `config.yaml` prepared from `config.yaml.example` for webhook URLs, ports, runtime, exchange testnet flags, and default risk settings

### One-command Deployment

Use the deploy script from the repository root:

```bash
./deploy.sh
```

The script will:

1. Build `bin/user_order_service` and `bin/position_monitor_service` via `make build`.
2. Create `data/`, `data/.compact/`, and `logs/` if missing.
3. Create any missing CSV files with the current header structure only if the file does not already exist.
4. Seed `users.csv` with a mock user only when `users.csv` is first created.
5. Stop existing `bin/user_order_service` / `bin/position_monitor_service` processes.
6. Start both services with `nohup` and write logs to:
   - `logs/user_order.log`
   - `logs/position_monitor.log`

### Configuration

By default, `deploy.sh` looks for one shared config file:

```text
config.yaml
```

Both services can use the same file. The user-order service reads `server`, `storage`, `notification`, and `filter_sync_interval`; the position-monitor service reads `runtime`, `exchange`, `defaults`, `notification`, and `deribit_position_sync`.

Create a local config from the example:

```bash
cp config.yaml.example config.yaml
vim config.yaml
```

Do not commit real `config.yaml` files with webhook URLs or credentials.

To use a config in another location:

```bash
CONFIG_FILE=/etc/trading-service/config.yaml ./deploy.sh
```

Or set service-specific configs:

```bash
ORDER_SERVICE_CONFIG=/etc/trading-service/uos.yaml \
POSITION_MONITOR_CONFIG=/etc/trading-service/pms.yaml \
./deploy.sh
```

### Runtime Environment Overrides

| Variable | Default | Description |
|---|---|---|
| `DATA_DIR` | `./data` | CSV persistence directory |
| `LOG_DIR` | `./logs` | Service log directory |
| `CONFIG_FILE` | `config.yaml` | Shared config file for both services |
| `ORDER_SERVICE_CONFIG` | `$CONFIG_FILE` if present | UOS config path |
| `POSITION_MONITOR_CONFIG` | `$CONFIG_FILE` if present | PMS config path |
| `POSITION_MONITOR_URL` | `http://localhost:8080` | UOS → PMS URL |
| `POSITION_MONITOR_ORDER_SERVICE_URL` | `http://localhost:8081` | PMS → UOS URL |
| `BINANCE_TESTNET` | `true` | Binance testnet flag passed to services |
| `HYPERLIQUID_TESTNET` | `true` | Hyperliquid testnet flag passed to services |
| `DERIBIT_TESTNET` | `true` | Deribit testnet flag passed to services |

### CSV Files Initialized by deploy.sh

The script creates only missing files and never overwrites existing CSV files:

```text
action.csv
condition.csv
exchange_symbol_filters.csv
leverage_configs.csv
rule.csv
strategies.csv
strategy_assets.csv
uprunning_orders.csv
user_order_positions.csv
user_orders.csv
user_positions.csv
user_strategies.csv
users.csv
```

### Service Ports

| Service | Default Port | Configuration |
|---|---:|---|
| `user_order_service` | `8081` | `server.port` in `config.yaml` |
| `position_monitor_service` | `8080` | `POSITION_MONITOR_PORT` env var |

Signal entrypoint:

```text
POST http://<host>:<user_order_service_port>/api/v1/signals
```

### Verification

```bash
# Check processes
ps aux | grep "bin/.*_service" | grep -v grep

# Check listening ports
ss -ltnp | grep -E ':8080|:8081'

# Watch logs
tail -f logs/user_order.log
tail -f logs/position_monitor.log

# Verify CSV headers
for f in data/*.csv; do echo "== $f =="; head -1 "$f"; done
```
<!-- END AUTO-GENERATED -->

### Service Health Indicators

Monitor these indicators to assess service health:

| Indicator | Healthy Value | Check Method |
|-----------|--------------|--------------|
| Process Status | Running | `ps aux | grep risk-service` |
| CPU Usage | < 80% | `top -p <pid>` |
| Memory Usage | < 500MB | `ps -o rss <pid>` |
| Log Errors | 0 critical errors | Log monitoring |
| GlobalState Version | Incrementing | Internal metrics |
| Rule Evaluation Latency | < 100ms | Performance metrics |

### Manual Health Check

```bash
# Check if process is running
ps aux | grep risk-service

# Check memory usage
ps -o rss,vsz,pmem -p $(pgrep risk-service)

# Check CPU usage
top -b -n 1 | grep risk-service

# Check recent logs for errors
tail -100 /var/log/risk-service.log | grep -i error
```

## Monitoring Setup

### Key Metrics to Monitor

1. **Service Availability**
   - Process running status
   - Service uptime

2. **Performance Metrics**
   - GlobalState version increment rate
   - Pipeline processing latency
   - Rule evaluation time
   - Action execution time

3. **Business Metrics**
   - Number of active positions
   - Risk actions triggered (close/reduce/hedge)
   - Rules evaluated per minute

4. **System Metrics**
   - CPU utilization
   - Memory consumption
   - Disk I/O for CSV writes
   - Network traffic (WebSocket connections)

### Recommended Monitoring Tools

- **Prometheus + Grafana**: For metrics collection and visualization
- **ELK Stack**: For log aggregation and analysis
- **Process Manager**: systemd, supervisord, or pm2

### Log Monitoring

Monitor logs for these patterns:

**Critical Events** (immediate investigation):
- Service startup failures
- GlobalState corruption
- Rule evaluation errors
- Action execution failures

**Warning Events** (monitor closely):
- CSV file read failures
- High latency warnings
- Memory pressure warnings
- WebSocket disconnections
- Deribit position sync failures

```bash
# Monitor critical errors
tail -f /var/log/risk-service.log | grep -i "critical\|error"

# Monitor warnings
tail -f /var/log/risk-service.log | grep -i "warning"

# Monitor Deribit sync
tail -f /var/log/risk-service.log | grep -i "DeribitSync"
```

## Service Monitor Setup

The `monitor_services.sh` script provides automatic process monitoring and restart for PMS and UOS. It is designed to run via crontab.

### Crontab Configuration

**Production (Mainnet)**:
```bash
crontab -e
# Add: check every 2 minutes
*/2 * * * * cd /path/to/trading_service && ./monitor_services.sh >> logs/cron_monitor.log 2>&1
```

**Testnet**:
```bash
crontab -e
# Add: set testnet environment
*/2 * * * * cd /path/to/trading_service && BINANCE_TESTNET=true HYPERLIQUID_TESTNET=true DERIBIT_TESTNET=true ./monitor_services.sh >> logs/cron_monitor.log 2>&1
```

### Monitor Script Configuration

| Variable | Set By | Description |
|---|---|---|
| `FORCE_RESYNC_FILTERS` | Script (hardcoded `true`) | Must be true to avoid service crashes on restart |
| `BINANCE_TESTNET` | Inherited from env / deploy.sh | Binance testnet flag |
| `HYPERLIQUID_TESTNET` | Inherited from env / deploy.sh | Hyperliquid testnet flag |
| `DERIBIT_TESTNET` | Inherited from env / deploy.sh | Deribit testnet flag |

### Check Logic

1. **Process check**: `pgrep` checks if service binary is running
2. **Health check**: HTTP `/health` endpoint verifies service responsiveness
3. **Auto-restart**:
   - Process not found → restart via `deploy.sh`
   - Process exists but health check fails → kill and restart

### Monitored Services

| Service | Port | Health Endpoint | Log File |
|---|---|---|---|
| PMS | 8080 | `http://localhost:8080/health` | `logs/position_monitor.log` |
| UOS | 8081 | `http://localhost:8081/health` | `logs/user_order.log` |

### Manual Operations

```bash
# Run monitor check manually
./monitor_services.sh

# Restart all services
./deploy.sh

# Restart a single service
pkill -f bin/position_monitor_service
./monitor_services.sh  # Monitor will auto-start the missing service
```

### Monitor Log

```bash
# View monitor log
tail -f logs/service_monitor.log

# Check restart history
grep "NOT running\|restarting\|deploy.sh" logs/service_monitor.log
```

### Advanced Configuration

Change check interval:
```bash
# Every minute
* * * * * cd /path/to/trading_service && ./monitor_services.sh >> logs/cron_monitor.log 2>&1

# Every 5 minutes
*/5 * * * * cd /path/to/trading_service && ./monitor_services.sh >> logs/cron_monitor.log 2>&1
```

### Troubleshooting

**Service keeps restarting**: Check service logs for crash reasons:
```bash
tail -100 logs/position_monitor.log | grep -i error
tail -100 logs/user_order.log | grep -i error
```

**Health check fails but process exists**:
```bash
curl http://localhost:8080/health
curl http://localhost:8081/health
```

**Cron not executing**:
```bash
systemctl status cron
crontab -l
grep CRON /var/log/syslog | tail -20
```

## Deribit Position Sync

### Overview

The Deribit position sync feature automatically synchronizes positions between Deribit exchange and local records. It detects discrepancies and handles:
- **New positions**: Creates local records for exchange-only positions
- **Deleted positions**: Marks local positions as deleted when not found on exchange
- **Quantity mismatches**: Creates delta positions or recreates positions as needed

### Configuration

Enable in `config.yaml`:

```yaml
deribit_position_sync:
  enabled: true
  interval: 10m  # Sync interval (default 10 minutes)
```

### Prerequisites

1. **API Keys**: Users must have Deribit API keys configured in `users.csv`:
   ```csv
   id,name,exchange,api_key,api_secret,api_password
   1,user1,deribit,<api_key>,<api_secret>,<api_password>
   ```

2. **Testnet Mode**: Set in config if using testnet:
   ```yaml
   exchange:
     deribit_testnet: true
   ```

### Monitoring

Monitor sync operations via logs:

```bash
# View sync operations
tail -f logs/position_monitor.log | grep "\[DeribitSync\]"

# Check for sync errors
grep "DeribitSync.*error" logs/position_monitor.log
```

Expected log patterns:
- `[DeribitSync] Starting initial sync...` - Service startup
- `[DeribitSync] Starting scheduled sync...` - Periodic sync
- `[DeribitSync] Created position: BTC-PERPETUAL LONG qty=10.0000` - Position created
- `[DeribitSync] Marked position deleted: ID=123 BTC-PERPETUAL` - Position closed
- `[DeribitSync] Sync completed: X positions updated` - Sync summary

### Troubleshooting

**Issue: No users found**

Check `users.csv` contains Deribit users:
```bash
grep "deribit" data/users.csv
```

**Issue: Failed to create client**

Verify API credentials:
- API key, secret, and password are correct
- API permissions include position read access
- Testnet flag matches actual API endpoint

**Issue: Failed to get positions**

Check:
- Deribit API is accessible
- API rate limits not exceeded
- Network connectivity

**Issue: Position not synced**

Review sync logic:
- Only active positions (deleted=0) are compared
- Quantity tolerance is 0.0001
- Check logs for specific handling (create/delete/adjust)

**Issue: user_positions continuously created (FIXED 2026-07-21)**

**Symptoms**:
- `user_positions.csv` grows every 10 seconds
- Log shows: `[UserPositionSyncer] CLOSE orphan user_position` repeatedly
- Same strategy_id appears with deleted=0, then deleted=1, then new deleted=0

**Root Cause** (已修复):
1. `markPositionDeleted` only closed `user_order_position`, not `user_position`
2. Orphan `user_position` (deleted=0) existed with all `user_order_positions` deleted=1
3. `UserPositionSyncer` called `CloseAndCreateRemainingUserPosition(id, 0, ...)` creating new record

**Fix Applied**:
- `markPositionDeleted` now closes both `user_order_position` AND `user_position`
- `UserPositionSyncer` passes full quantity: `CloseAndCreateRemainingUserPosition(id, quantity, ...)`
- `remainingQty = 0`, no new record created

**Verification**:
```bash
# Check that user_positions.csv is not growing
wc -l data/user_positions.csv
sleep 30
wc -l data/user_positions.csv  # Should be same or only +1-2 lines

# Check logs for repeated orphan closures
tail -f logs/position_monitor.log | grep "CLOSE orphan"
# Should only appear once per orphan, not continuously
```

## Deribit Order Notifications

### Overview

Deribit-specific notifications are sent for **any** WS order state change (NEW/FILLED/CANCELLED/FAILED), regardless of whether the order exists in `uprunning_orders`. This is distinct from Binance/Hyperliquid, which only notify on FILLED via the common processing path.

Notifications are sent via `test_url` (ChannelTest) in `config.yaml`, using enterprise WeChat (企微) markdown format.

### Notification Format

```
> **DeribitPositions({userName})**
代币：{symbol}，{action}，数量：{qty}，价格: {price}
```

Action text examples:
- `开卖仓挂单成功` — NEW sell order (open position)
- `开买仓成功` — FILLED buy order (open position)
- `减卖仓成功` — FILLED sell order (reduce position, full fill)
- `减买仓部分成交成功` — FILLED buy order (reduce position, partial fill)
- `触发止盈止损，减卖仓成功` — Risk control triggered reduce
- `开买仓挂单已取消` — CANCELLED order
- `开卖仓挂单失败` — FAILED order

### Configuration

Ensure `test_url` is set in `config.yaml`:

```yaml
notification:
  test_url: "https://your-webhook-endpoint.example.com/send?key=YOUR_KEY"
```

### Monitoring

```bash
# Check Deribit notification delivery
grep "SendDeribitPositionNotification" logs/position_monitor.log | tail -20

# Check notification failures
grep "failed to send Deribit position notification" logs/position_monitor.log

# Check order state changes being processed
grep "deribit order monitor: parsed order update" logs/position_monitor.log | tail -10
```

### Troubleshooting

**Issue: No Deribit notifications received**

Check:
1. `test_url` is configured in `config.yaml`
2. User has `userName` set (passed via `user.Name` to `DeribitOrderMonitor`)
3. Deribit WS is connected: `grep "deribit order monitor: WebSocket connected" logs/position_monitor.log`
4. Notifier is not nil: check initialization in `deribit_user_order_runtime.go`

**Issue: Notifications missing ROI data**

ROI is looked up via `UserOrderPosition.Asset → UserStrategyID → UserPosition.ROI/MaxProfitPercentage`. If ROI shows as 0:
- No active `UserOrderPosition` exists for the symbol
- No `UserPosition` found for the strategy
- This is expected for NEW orders (position not yet created)

**Issue: Risk control label not detected**

Risk control is detected by `isRiskControlLabel()` which checks for:
- `risk` (case-insensitive)
- `止盈止损`
- `tp_sl`

Ensure order labels contain one of these patterns for risk control notifications.

**Issue: Deribit WS disconnects and stops receiving order updates**

Symptoms:
- No order updates received after network interruption
- Log shows: `read error: websocket: close 1006`

Auto-Recovery Behavior:
```
1. handleMessages detects read error → calls reconnect()
2. Old goroutines stopped via loopStopCh/heartbeatStopCh
3. reconnect() closes old conn, retries Connect() up to 5 times (1s-5s backoff)
4. New handleMessages + sendHeartbeat goroutines started
5. Message loop continues processing (return → continue fix)
6. Heartbeat resumes (public/test every 30s prevents idle disconnect)
```

Monitoring:
```bash
# Check for disconnect/reconnect events
grep "read error\|reconnect\|reconnected" logs/position_monitor.log | tail -10

# Verify heartbeat is active (every 30s, responses shown as subscription confirmation)
grep "deribit order monitor" logs/position_monitor.log | tail -20

# Simulate disconnect for testing
sudo iptables -A OUTPUT -d test.deribit.com -j DROP && sleep 5 && sudo iptables -D OUTPUT -d test.deribit.com -j DROP
```

Expected log pattern on successful reconnect:
```
06:22:41 deribit order monitor: read error: websocket: close 1006
06:22:42 deribit order monitor: Connect() called, conn=false
06:22:44 deribit order monitor: WebSocket connected successfully
06:22:44 deribit order monitor: reconnected after 1 attempts
```

## Common Issues and Fixes

### Issue 1: Hyperliquid API Connection Failures (AUTO-RECOVERY)

**Symptoms**:
- Log shows: `Hyperliquid init failed (backoff now Xs): ...`
- Filter sync fails with panic recovery message
- User orders may temporarily fail with "backing off" error

**Root Cause**:
- Hyperliquid API temporarily unavailable
- Network connectivity issues
- API rate limiting or maintenance

**Auto-Recovery Behavior**:
```
Service behavior during API failure:
1. Panic caught and logged (service doesn't crash)
2. Backoff applied: 5s → 10s → 30s → 60s (max)
3. Service continues running (other exchanges unaffected)
4. Automatic retry when backoff expires
5. Self-heals when API recovers (no restart needed)
```

**Monitoring**:
```bash
# Watch for auto-recovery logs
tail -f logs/position_monitor.log | grep -E "Hyperliquid init (failed|succeeded|backing off)"

# Check if service is still running (should be YES)
ps aux | grep position_monitor_service | grep -v grep

# Verify other exchanges working
grep "binance fetched" logs/position_monitor.log
```

**Expected Log Pattern**:
```
Hyperliquid init failed (backoff now 10s): hyperliquid NewInfo panic: ...
Filter sync failed for hyperliquid: hyperliquid init failed: ...
# ... API recovers after 5 minutes ...
Hyperliquid init succeeded
Filter sync: hyperliquid fetched 50 filters in 2.3s
```

**When Manual Intervention Needed**:
- Service crashes completely (should NOT happen with auto-recovery)
- Persistent failure after 60s backoff for extended period (>1 hour)
- All exchanges failing (indicates network/systemic issue)

**Fix for Persistent Failures**:
```bash
# Check network connectivity
curl -X POST https://api.hyperliquid.xyz/info -d '{"type": "meta"}'

# Check Hyperliquid status page (if available)
# Verify firewall rules allow outbound HTTPS

# If API is confirmed down, wait for recovery
# Service will auto-recover when API returns
```

### Issue 2: Service Won't Start

**Symptoms**:
- Process exits immediately
- No logs generated

**Diagnosis**:
```bash
# Check for missing configuration
ls -l $DATA_DIR/*.csv

# Check for permission issues
ls -la /opt/risk-service/

# Check Go version
go version
```

**Fix**:
```bash
# Ensure CSV files exist
cp data/*.csv /opt/risk-service/data/

# Fix permissions
chmod 644 /opt/risk-service/data/*.csv
chmod 755 /opt/risk-service/risk-service

# Verify Go version is 1.25+
go version
```

### Issue 2: CSV Configuration Errors

**Symptoms**:
- Rules not loading
- Errors in logs about CSV parsing

**Diagnosis**:
```bash
# Check CSV file format
head -5 data/rule.csv

# Check for missing files
ls -la data/*.csv
```

**Fix**:
```bash
# Validate CSV format
# Ensure headers: id,user_strategy_id,condition_name,operator,value,sort,status,action,params
# Ensure no malformed rows

# Replace corrupted files
cp data/*.csv.backup data/*.csv

# Restart service
systemctl restart risk-service
```

### Issue 3: High Memory Usage

**Symptoms**:
- Memory usage exceeds 500MB
- System memory pressure

**Diagnosis**:
```bash
# Check memory usage
ps -o rss,vsz -p $(pgrep risk-service)

# Check number of positions
# (Add internal metrics endpoint if available)

# Check for memory leaks
# Monitor memory growth over time
```

**Fix**:
```bash
# Restart service to clear memory
systemctl restart risk-service

# If persistent, investigate:
# - Large number of positions stored
# - State version not being cleaned up
# - Memory leak in pipeline processing
```

### Issue 4: Risk Actions Not Triggering

**Symptoms**:
- Positions exceeding thresholds not closed
- No actions logged despite rule matches

**Diagnosis**:
```bash
# Check if rules are loaded
# Check rule conditions in config
# Check rule.csv for strategy configuration
cat data/rule.csv

# Check if positions are updating
tail -f /var/log/risk-service.log | grep -i "position"

# Check rule evaluation
tail -f /var/log/risk-service.log | grep -i "rule"
```

**Fix**:
```bash
# Verify rule configuration
# Ensure condition thresholds are correct
# Ensure actions are enabled

# Restart to reload configuration
systemctl restart risk-service
```

### Issue 5: WebSocket Connection Failures

**Symptoms**:
- Market data not updating
- Stale prices in GlobalState

**Diagnosis**:
```bash
# Check WebSocket connection status
tail -f /var/log/risk-service.log | grep -i "websocket\|connection"

# Check network connectivity
ping <websocket-server>

# Check for authentication issues
tail -f /var/log/risk-service.log | grep -i "auth"
```

**Fix**:
```bash
# Check WebSocket credentials (if required)
# Verify network connectivity
# Check firewall rules

# Restart service to reconnect
systemctl restart risk-service
```

### Issue 6: Deribit Option Price Not Updating (NEW)

**Symptoms**:
- Log shows: `WARN: price not found in snapshot for deribit/BTC-...-P`
- Deribit options show stale or missing prices
- New options not being tracked after adding positions

**Root Cause**:
- Deribit WebSocket requires explicit subscription for each option
- New options added after startup need dynamic subscription
- WebSocket connection may have failed (fallback to REST API)

**Auto-Recovery Behavior**:
```
Deribit dynamic subscription architecture:
1. EnsureSubscribed() called every syncCycle (10s default)
2. DeribitOptionExtractor extracts options from positions
3. Automatically subscribes to new options not yet subscribed
4. Errors logged but don't crash service
5. Retries on next cycle if subscription fails
```

**Monitoring**:
```bash
# Check for new subscriptions
grep "Subscribed to.*new Deribit options" logs/position_monitor.log | tail -20

# Check for subscription errors
grep "Failed to subscribe to Deribit option" logs/position_monitor.log

# Verify WebSocket connection
grep "Deribit WS connection" logs/position_monitor.log | tail -5

# Check current subscriptions (requires mock mode in test)
# In production, check logs for subscription count
```

**Expected Log Pattern**:
```
# On startup
Subscribed to 5 Deribit options

# When new option position added
Subscribed to 1 new Deribit options (total: 6)

# If WebSocket fails (uses REST fallback)
Deribit WS connection failed (will use REST API fallback): ...
```

**When Manual Intervention Needed**:
- Persistent "price not found" errors for extended period
- WebSocket connection never succeeds
- All subscriptions failing

**Troubleshooting Steps**:
```bash
# 1. Check if WebSocket is connecting
grep "Deribit WS" logs/position_monitor.log | grep -v "read error"

# 2. Verify positions exist in user_order_positions.csv
grep "deribit" data/user_order_positions.csv | grep ",3,"  # PosType 3 = option

# 3. Check if options are being extracted
# (No direct log, but subscription count should match position count)

# 4. Test REST API fallback
curl -X POST "https://www.deribit.com/api/v2/public/ticker" \
  -d '{"instrument_name": "BTC-24JUL26-64000-P"}'
```

**Fix for Persistent Failures**:
```bash
# Check network connectivity to Deribit
curl -v https://www.deribit.com/api/v2/public/time

# Check proxy settings (Deribit uses system proxy)
echo $http_proxy
echo $https_proxy

# Verify firewall allows WebSocket (wss://www.deribit.com/ws/api/v2)

# If issue persists, service will use REST API fallback automatically
```

## Rollback Procedures

### Standard Rollback

```bash
# 1. Stop current service
systemctl stop risk-service

# 2. Backup current version
cp /opt/risk-service/risk-service /opt/risk-service/risk-service.failed
cp -r /opt/risk-service/data /opt/risk-service/data.failed

# 3. Restore previous version
cp /opt/risk-service/risk-service.prev /opt/risk-service/risk-service
cp -r /opt/risk-service/data.prev /opt/risk-service/data

# 4. Start previous version
systemctl start risk-service

# 5. Verify rollback
systemctl status risk-service
tail -f /var/log/risk-service.log
```

### Emergency Rollback

```bash
# Stop immediately
systemctl stop risk-service

# Restore known good backup
cp /opt/risk-service.backup/risk-service /opt/risk-service/
cp -r /opt/risk-service.backup/data /opt/risk-service/data

# Start immediately
systemctl start risk-service

# Verify quickly
systemctl status risk-service
```

### Backup Strategy

Maintain these backups:
- Previous working binary (`risk-service.prev`)
- Previous working config (`data.prev`)
- Known good backup (`/opt/risk-service.backup/`)
- Daily config backups (`data.YYYY-MM-DD/`)

```bash
# Create backup before deployment
cp /opt/risk-service/risk-service /opt/risk-service/risk-service.prev
cp -r /opt/risk-service/data /opt/risk-service/data.prev
```

## Alerting and Escalation

### Critical Alerts

Trigger immediate response:
1. **Service Down**: Process not running
2. **Configuration Failure**: CSV files missing or corrupt
3. **Memory Exhaustion**: Memory > 800MB
4. **Rule Evaluation Failure**: Errors in rule processing

### Warning Alerts

Monitor closely:
1. **High CPU**: CPU usage > 80%
2. **High Latency**: Pipeline processing > 500ms
3. **WebSocket Disconnection**: Market data stale
4. **Growing Positions**: Position count > expected

### Escalation Path

| Level | Issue Type | Response Time | Action |
|-------|-----------|--------------|---------|
| L1 | Warning alerts | 30 minutes | Investigate, monitor |
| L2 | Critical alerts | 5 minutes | Immediate investigation |
| L3 | Service failure | Immediate | Emergency rollback |

### Escalation Contacts

- **L1 Support**: [Team/Person responsible for monitoring]
- **L2 Engineer**: [Senior engineer for critical issues]
- **L3 Manager**: [Engineering manager for emergencies]

## Performance Tuning

### Optimizing Pipeline Performance

1. **Reduce Rule Complexity**:
   - Simplify condition evaluation logic
   - Use fewer conditions per rule
   - Order rules by priority (sort field)

2. **Optimize Position Count**:
   - Clean up closed positions regularly
   - Limit active positions stored

3. **Memory Management**:
   - Monitor GlobalState size
   - Implement periodic cleanup
   - Restart service periodically for long-running instances

### Configuration Optimization

```bash
# Adjust CSV rule loading frequency
# Implement hot-reload for rules (future enhancement)

# Optimize condition parameters
# Use appropriate thresholds based on historical data
```

## Security Operations

### Security Checklist

- [ ] No hardcoded secrets in configuration
- [ ] CSV files have correct permissions (644)
- [ ] Service runs with minimal privileges
- [ ] Logs don't contain sensitive data
- [ ] WebSocket connections authenticated (if required)

### Security Monitoring

Monitor for:
- Unauthorized configuration changes
- Unexpected service restarts
- Large position changes
- Unusual rule triggers

## Maintenance Procedures

### Regular Maintenance

**Daily**:
- Check service health
- Monitor logs for errors
- Verify backup integrity

**Weekly**:
- Review performance metrics
- Update configuration if needed
- Clean up old backups

**Monthly**:
- Full backup verification
- Security review
- Performance optimization review

### Configuration Updates

```bash
# 1. Backup current configuration
cp -r /opt/risk-service/data /opt/risk-service/data.backup

# 2. Update CSV files
# Edit or replace data/*.csv

# 3. Verify format
head -5 data/*.csv

# 4. Restart service to load new config
systemctl restart risk-service

# 5. Monitor for issues
tail -f /var/log/risk-service.log
```

## Troubleshooting Tools

### Diagnostic Commands

```bash
# Service status
systemctl status risk-service

# Process details
ps aux | grep risk-service

# Memory profiling
ps -o rss,vsz,pmem -p $(pgrep risk-service)

# CPU profiling
top -b -n 1 -p $(pgrep risk-service)

# Log analysis
tail -100 /var/log/risk-service.log
grep -i error /var/log/risk-service.log

# Configuration check
ls -la /opt/risk-service/data/
cat /opt/risk-service/data/*.csv

# Network connectivity
ping <websocket-server>
netstat -an | grep <websocket-port>
```

### Log Analysis

```bash
# Find recent errors
grep -i "error\|critical" /var/log/risk-service.log | tail -50

# Find specific events
grep -i "rule.*triggered" /var/log/risk-service.log | tail -20

# Monitor real-time
tail -f /var/log/risk-service.log | grep -v "INFO"  # Show only warnings/errors
```

## Quick Reference

### Common Operations

```bash
# Start service
systemctl start risk-service

# Stop service
systemctl stop risk-service

# Restart service
systemctl restart risk-service

# Check status
systemctl status risk-service

# View logs
tail -f /var/log/risk-service.log

# Check memory
ps -o rss -p $(pgrep risk-service)

# Check config
ls -la /opt/risk-service/data/
```

### Emergency Commands

```bash
# Emergency stop
kill -9 $(pgrep risk-service)

# Emergency restart
systemctl restart risk-service

# Emergency rollback
systemctl stop risk-service
cp /opt/risk-service.backup/* /opt/risk-service/
systemctl start risk-service
```

### File Locations

| File | Location |
|------|----------|
| Binary | `/opt/risk-service/risk-service` |
| Configuration | `/opt/risk-service/data/*.csv` |
| Logs | `/var/log/risk-service.log` |
| Backups | `/opt/risk-service.backup/` |

### Support Resources

- **Documentation**: `/docs/README.md`, `/docs/CONTRIBUTING.md`
- **Architecture**: `/.claude/plans/risk-service.plan.md`
- **Source Code**: `/internal/risk/*/`
- **Tests**: `/test/`
---

## RPC Rule Creation (Added 2026-07-11)

### Architecture

UOS now calls PMS via RPC to create rules instead of writing directly to `rule.csv`:

- **Endpoint:** `POST /rpc/v1/rules/create`
- **Client:** `internal/rpc/client.go`
- **Handler:** `internal/signal/close_rule_writer.go`

### Monitoring

Monitor these logs for RPC rule creation:

```bash
# Success logs
CloseRuleWriter: creating immediate close rule via RPC: userStrategyID=XXX
CloseRuleWriter: SUCCESS created rule via RPC: ID=XXX, userStrategyID=XXX

# Failure logs
CloseRuleWriter: FAILED to create rule via RPC: error=XXX
```

### Troubleshooting RPC Issues

**Issue:** RPC calls failing with connection refused

**Check:**
```bash
# Verify PMS is running
curl http://localhost:8080/health

# Check environment variable
echo $POSITION_MONITOR_URL

# Test RPC endpoint directly
curl -X POST http://localhost:8080/rpc/v1/rules/create \
  -H "Content-Type: application/json" \
  -d '{"user_strategy_id":1,"condition_name":"test","action":"reduce"}'
```

**Issue:** Rules not being created

**Debug:**
```bash
# Check UOS logs for RPC errors
grep "FAILED to create rule" /var/log/uos.log

# Check PMS logs for RPC handler errors
grep "HandleRPCCreateRule" /var/log/pms.log

# Verify rule.csv is being written
tail -f data/rule.csv
```


### Issue 7: CSV Data Corruption (CompactAll Race Condition) (NEW)

**Symptoms**:
- Service fails to start with error: `record on line X: wrong number of fields`
- CSV files contain truncated or merged rows
- `compact failed: no such file or directory` errors in logs

**Root Cause**:
- `CompactAll()` and `AppendRow()` running concurrently
- Compact renames file while AppendRow writing to old file
- Result: data corruption at CSV row boundaries

**Fix Applied** (2026-07-21):
```go
// internal/persistence/global_state.go
func (gs *GlobalState) CompactAll() error {
    gs.writeWg.Wait()  // Wait for pending writes
    gs.rw.Lock()       // Block new writes
    defer gs.rw.Unlock()
    // ... compact operations
}
```

**Verification**:
```bash
# Check for corrupted CSV rows
awk -F, 'NF != 24' data/user_positions.csv

# Should return only header line (24 fields)
# If other lines appear, data is corrupted
```

**Recovery from Corruption**:
```bash
# 1. Stop service
systemctl stop risk-service

# 2. Backup corrupted file
cp data/user_positions.csv data/user_positions.csv.corrupted

# 3. Remove corrupted rows manually or restore from backup
# Option A: Manual cleanup (identify and fix corrupted lines)
vim data/user_positions.csv

# Option B: Restore from backup
cp /opt/risk-service/data.prev/user_positions.csv data/

# 4. Restart service
systemctl start risk-service
```

**Prevention**:
- Fix ensures all writes complete before compact
- Tested with 200 concurrent iterations, no corruption
- Monitor for `wrong number of fields` errors

## 关联风控 (position_xxx) 排查

### 规则不触发

**症状**: 关联仓位已平掉，但 position_xxx 规则未触发风控动作。

**排查步骤**:

```bash
# 1. 确认规则是否存在且状态为active
grep "position_" data/rule.csv

# 2. 查看风控评估日志，确认该strategyID是否在被评估的仓位列表中
grep "Risk evaluation.*strategyID=<ID>" logs/position_monitor.log | tail -5

# 3. 查看被过滤掉的仓位（current_price=0）
grep "\[DEBUG\] FilterActivePositions: skipped" logs/position_monitor.log | tail -5

# 4. 查看position条件评估详情
grep "\[DEBUG\] evaluatePositionCondition" logs/position_monitor.log | tail -5
```

**常见原因**:

| 原因 | 现象 | 修复 |
|------|------|------|
| rule value=0 被解析为bool(false) | `value=%!s(bool=false)` | toFloat已加bool分支，false→0.0 |
| operator用`=`而非`==` | evaluateFloat走default返回false | 已支持`=`作为`==`别名 |
| 关联仓位current_price=0 | FilterActivePositions跳过该仓位 | 该仓位不会被评估，需确认position_xxx规则绑定策略的仓位是否有价格 |
| condition_name大小写不一致 | 创建时小写，内存中symbol大写 | 已支持大小写不敏感，自动规范化为大写 |

### 创建规则失败

**症状**: POST /api/v1/rules 返回错误。

**校验链路**:
1. 必填字段校验（user_strategy_id, condition_name, operator, value）
2. `NormalizePositionSymbol` — symbol大小写规范化为大写
3. `IsValidPositionSymbol` — 格式校验（期权4段式、月份合法、类型C/P）
4. strategy存在性校验
5. 该用户必须有该symbol的活跃仓位

```bash
# 测试创建关联风控规则（大小写不敏感）
curl -X POST http://localhost:8080/api/v1/rules \
  -H "Content-Type: application/json" \
  -d '{
    "user_strategy_id": 8,
    "condition_name": "position_btc-28aug26-67000-c",
    "operator": "=",
    "value": 0,
    "action": "reduce",
    "quantity_pct": 1,
    "sort": 1
  }'
```
