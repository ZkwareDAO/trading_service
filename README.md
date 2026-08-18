# Trading Service

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://golang.org/dl/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A multi-exchange cryptocurrency trading risk control system built in Go. It provides automated position monitoring, risk rule evaluation, and order execution across Binance Futures, Hyperliquid, and Deribit Options.

## Features

- **Multi-Exchange Support** — Binance Futures, Hyperliquid DEX, Deribit Options
- **Real-Time Price Monitoring** — WebSocket price streams with REST fallback
- **Risk Rule Engine** — Configurable rules for ROI, price thresholds, holding time, profit drawdown, and cross-position conditions
- **Automated Position Management** — Stop-loss, take-profit, trailing stop, time-based exit
- **Deribit Position Sync** — Automatic synchronization of Deribit options positions
- **Webhook Notifications** — Enterprise WeChat (企微) integration for trade and risk alerts
- **CSV Persistence** — Lightweight data storage without database dependency

## Architecture

```
┌─────────────────┐     RPC      ┌─────────────────────────┐
│  User Order     │◄────────────►│  Position Monitor       │
│  Service (UOS)  │              │  Service (PMS)          │
│  Port: 8081     │              │  Port: 8080             │
├─────────────────┤              ├─────────────────────────┤
│ Signal Receiver │              │ Price WS (Binance/HL/   │
│ Order Execution │              │           Deribit)      │
│ Strategy Mgmt   │              │ Rule Engine             │
│ Exchange Adapters│             │ Position Aggregation    │
└─────────────────┘              │ Risk Action Execution   │
                                 │ Deribit Position Sync   │
                                 └─────────────────────────┘
                                           │
                                 ┌─────────┴──────────┐
                                 │ Exchange Position   │
                                 │ Reporter            │
                                 │ (Hourly snapshots)  │
                                 └────────────────────┘
```

**Data Flow:**

```
External Signal → UOS → Create Order → Exchange Execution
                                        ↓
                                  PMS ← Periodic Sync
                                        ↓
                                 Aggregate Positions + Risk Check
                                        ↓
                                 Trigger Close Action → UOS → Execute Close
```

## Before You Start

Nothing is mandatory to boot: if `config.yaml` is missing the services fall back to
built-in defaults and start anyway. That convenience is also the main hazard, because
**the default network is mainnet**. Decide these three things first.

### 1. Which network? — decide before the first signal

| | What happens |
|---|---|
| Nothing configured | **MAINNET — real funds** |
| `BINANCE_TESTNET=true` etc. | testnet for that exchange |
| `exchange.*_testnet: true` in `config.yaml` | testnet for that exchange |

Testnet is opt-in per exchange and only the exact string `true` enables it. Environment
variables win over `config.yaml`; both services honour both sources. For a safe first run:

```bash
cp .env.example .env                      # ships with all three testnets enabled
set -a; source .env; set +a; ./deploy.sh  # set -a is required, see note below
```

The UOS logs the resolved network for every exchange at startup. Verify it before trading:

```
Exchange networks: binance=testnet, hyperliquid=testnet, deribit=testnet
```

> `source .env` alone does **not** export the variables to `deploy.sh` — the child process
> would not see them. `set -a` marks them for export; without it you silently get mainnet.

### 2. Credentials — the only thing you must add by hand

Not in `config.yaml`. They live in `data/users.csv`, which `./deploy.sh` creates on first
run, seeded with a `mock` exchange user that needs no keys. You can complete the entire
[First Trade](#first-trade) walkthrough on `mock` without any exchange account. To trade
for real, add a row — see [Exchange Credentials](#exchange-credentials).

### 3. Everything else is optional

`config.yaml` only overrides defaults (ports, leverage, stop-loss percentages, sync
intervals, webhooks). Copy it when you want to change something:

```bash
cp config.example.yaml config.yaml
```

Notifications are off unless you set `notification.enabled: true` **and** supply webhook
URLs. See [Configuration](#configuration) for the full key list.

## Quick Start

### Prerequisites

- Go 1.25+
- Make

### Build & Run

```bash
# Clone
git clone https://github.com/<your-org>/trading_service.git
cd trading_service

# Install dependencies
go mod download

# Build
make build

# Configure (ports, defaults, notifications — no credentials here)
cp config.example.yaml config.yaml

# Deploy (builds, initializes CSV files, starts both services)
./deploy.sh
```

> **`deploy.sh` defaults to mainnet.** To run against testnets instead:
>
> ```bash
> BINANCE_TESTNET=true HYPERLIQUID_TESTNET=true DERIBIT_TESTNET=true ./deploy.sh
> ```

### Exchange Credentials

Credentials do **not** live in `config.yaml`. They live in `data/users.csv`, one row per (user, exchange) pair. `./deploy.sh` creates this file on first run and seeds a single row using the built-in `mock` exchange, so the service starts with no real keys:

```csv
id,name,exchange,api_key,api_secret,api_password,created_at,updated_at
1,test_user,mock,,,,2026-06-22T00:00:00Z,2026-06-22T00:00:00Z
```

To trade on a real exchange, add a row (or use the API in [First Trade](#first-trade) below, which appends it for you):

```csv
2,alice,binance,YOUR_API_KEY,YOUR_API_SECRET,,2026-06-22T00:00:00Z,2026-06-22T00:00:00Z
```

- `exchange` must be one of `binance`, `hyperliquid`, `deribit`, or `mock`, and must match the `exchange` in any signal sent for that user.
- `api_password` is optional and only used by exchanges that require a passphrase; leave it empty otherwise.
- Timestamps are RFC3339.
- **Restart the services after editing the file by hand** — state is loaded into memory at startup.
- The file is plaintext and gitignored. `chmod 600 data/users.csv`.
- `GET /api/v1/users` deliberately omits `id` and all credential fields. If you lose a `user_id`, read it from the first column of this file.

### Verify

```bash
# Check processes
ps aux | grep "_service" | grep -v grep

# Check ports
ss -ltnp | grep -E ':8080|:8081'

# Health check
curl -s http://127.0.0.1:8081/health   # UOS → {"status":"healthy"}
curl -s http://127.0.0.1:8080/health   # PMS

# Watch logs
tail -f logs/position_monitor.log
tail -f logs/user_order.log
```

## First Trade

This walkthrough places and closes one position on the built-in `mock` exchange — no real funds, no exchange account. Every request below was executed against the running service; the responses are actual output.

Note there is **no create-strategy endpoint**. Strategies are created implicitly by the first signal that references them.

### 1. Create a user

```bash
curl -X POST http://127.0.0.1:8081/api/v1/users/create \
  -H "Content-Type: application/json" \
  -d '{
    "name": "demo_user",
    "exchange": "mock",
    "api_key": "dummy",
    "api_secret": "dummy"
  }'
```

```json
{"code":0,"message":"success","data":{"id":2,"name":"demo_user","exchange":"mock","created_at":"...","updated_at":"..."}}
```

**The returned `id` is your `user_id` for every later step — read it from the response, don't assume it.** `./deploy.sh` already seeds a `test_user` at `id=1`, so the first user you create is normally `id=2`. Passing the wrong `user_id` does *not* error if that ID happens to exist; the signal is silently attributed to the other user.

To avoid transcribing it, capture it in a shell variable and use `$UID_` below:

```bash
UID_=$(curl -s -X POST http://127.0.0.1:8081/api/v1/users/create \
  -H "Content-Type: application/json" \
  -d '{"name":"demo_user","exchange":"mock","api_key":"dummy","api_secret":"dummy"}' \
  | jq -r '.data.id')
echo "user_id=$UID_"
```

(`api_key`/`api_secret` are required fields even for `mock`; any placeholder works.)

### 2. Send an open signal — this also creates the strategy

```bash
curl -X POST http://127.0.0.1:8081/api/v1/signals \
  -H "Content-Type: application/json" \
  -d '{
    "SignalID": "demo_001",
    "symbol": "BTCUSDT",
    "pos_type": 2,
    "user_id": '"$UID_"',
    "strategy_type": "demo",
    "strategy": {
      "name": "DEMO",
      "version": "1",
      "internal": "4h",
      "description": "first trade",
      "cash": 1000,
      "parts": 5,
      "leverage": 2,
      "valid_before": "2035-12-31 08:00:00"
    },
    "signal": {
      "action": "buy",
      "exchange": "mock",
      "cash": 100,
      "trigger_price": 60000,
      "slippage": 0,
      "order_type": 1,
      "valid_before": "2035-12-31 08:00:00"
    }
  }'
```

```json
{"message":"success"}
```

Required fields are `user_id`, `symbol`, `pos_type`, `strategy.name`, `signal.exchange`, `signal.action`, and a non-zero `signal.cash` or `signal.quantity`. `valid_before` is optional, but when present it must be in the future or the signal is rejected as expired — use the `YYYY-MM-DD HH:MM:SS` format shown. `signal.exchange` must match the user's `exchange`.

### 3. Confirm the strategy and find its ID

```bash
curl -s http://127.0.0.1:8081/api/v1/user-strategies
```

```json
{"code":0,"message":"success","data":[{"id":1,"user_id":2,"name":"DEMO_4H_1_BTCUSDT","exchange":"mock","cash":1000,"parts":5,"status":1,"risk_strategy_type":"traditional","orders_num":0,"valid_before":"2035-12-31T08:00:00Z"}]}
```

The strategy name is derived from the signal as `{name}_{INTERVAL}_{VERSION}_{symbol}` → `DEMO_4H_1_BTCUSDT`. Note two wrinkles in [`ExtractStrategyName`](internal/signal/strategy_signal.go): the symbol is rewritten per exchange (`binance` → `…USDT`, `hyperliquid` → `…USDC`, so `BTCUSDT` becomes `BTCUSDC` on Hyperliquid), and if `name` already ends in `_{INTERVAL}_{VERSION}` the suffix is not repeated.

The `id` here is the `user_strategy_id` used by risk rules.

### 4. Attach a stop-loss rule (optional)

```bash
curl -X POST http://127.0.0.1:8080/api/v1/rules \
  -H "Content-Type: application/json" \
  -d '{
    "user_strategy_id": 1,
    "condition_name": "roi",
    "operator": "<=",
    "value": -0.02,
    "action": "reduce",
    "quantity_pct": 1.0
  }'
```

Rules are registered on the **PMS (port 8080)**, not the UOS. A rule requires an *already-visible* active position for the strategy, so this can return error `4005` if you run it immediately after step 2 — the PMS picks up new positions on its sync cycle (`runtime.price_snapshot_interval`, default 10s). Wait one cycle and retry if you see `4005`.

### 5. Close the position

```bash
curl -X POST http://127.0.0.1:8081/api/v1/signals \
  -H "Content-Type: application/json" \
  -d '{
    "SignalID": "demo_002",
    "symbol": "BTCUSDT",
    "pos_type": 2,
    "user_id": '"$UID_"',
    "strategy_type": "demo",
    "strategy": {
      "name": "DEMO",
      "version": "1",
      "internal": "4h",
      "cash": 1000,
      "parts": 5,
      "leverage": 2,
      "valid_before": "2035-12-31 08:00:00"
    },
    "signal": {
      "action": "sell_close",
      "exchange": "mock",
      "cash": 100,
      "trigger_price": 61000,
      "slippage": 0,
      "order_type": 1,
      "valid_before": "2035-12-31 08:00:00"
    }
  }'
```

The `strategy` block must repeat the same `name`/`internal`/`version` as step 2 so the signal resolves to the same strategy.

Two things to know about closing:

- It requires **both** services running — the UOS asks the PMS for the position, and returns `check position: position query client is not configured` if the PMS is unreachable.
- If no active position is visible yet, the close is **skipped and still returns `{"message":"success"}`**. A success response is therefore not proof that anything closed. Confirm with the position queries below, or check `logs/user_order.log` for `SKIP - no active position`.

### Inspect the results

```bash
# Order-level positions
curl -s "http://127.0.0.1:8080/api/v1/user-order-positions?user_id=1"

# Aggregated positions
curl -s "http://127.0.0.1:8080/api/v1/user-positions?user_id=1"

# Raw persistence
column -s, -t < data/user_orders.csv
```

## Configuration

Copy `config.example.yaml` to `config.yaml` and adjust:

```yaml
server:
  port: 8081
  mode: release

storage:
  type: csv
  data_dir: ./data

defaults:
  leverage: 1
  slippage: 0.01
  stop_loss_pct: -0.02
  profit_drawdown_pct: 0.05
  trailing_activation_pct: 0.05
  time_stop_hours: 72

notification:
  enabled: false
  open_url: ""
  close_url: ""
  test_url: ""

runtime:
  price_snapshot_interval: 10s

exchange:
  binance_testnet: true
  hyperliquid_testnet: true
  deribit_testnet: true

deribit:
  spread_threshold: 0.005

deribit_position_sync:
  enabled: false
  interval: 10m

# Exchange trading-rule sync interval (tick_size, step_size). Rarely changes.
filter_sync_interval: 240h

# How often UOS re-reads CSV for position updates written by PMS.
position_sync_interval: 5s

reporter:
  api_url: "http://localhost:8080/api/v1/exchange/positions"
```

| Key | Default | Description |
|---|---|---|
| `server.port` | `8081` | UOS HTTP port (PMS port is set by `POSITION_MONITOR_PORT`) |
| `server.mode` | `release` | Run mode: `release`, `debug`, `test` |
| `storage.type` | `csv` | Storage backend (only `csv` is supported) |
| `storage.data_dir` | `./data` | CSV data directory |
| `runtime.price_snapshot_interval` | `10s` | How often PMS aggregates positions and evaluates rules |
| `filter_sync_interval` | `240h` | Exchange symbol-filter sync interval |
| `position_sync_interval` | `5s` | UOS→CSV position resync interval |
| `deribit.spread_threshold` | `0.005` | Skip auto-close and notify when bid-ask spread exceeds this |

> **The config parser does not strip inline comments.** It is a minimal hand-rolled
> reader ([`internal/config/config.go`](internal/config/config.go)), so `port: 8081  # my port`
> fails to parse and silently falls back to the default. Keep comments on their own lines.

### Environment Variables

Environment variables take precedence over `config.yaml`.

| Variable | Default | Description |
|---|---|---|
| `DATA_DIR` | `./data` | CSV data directory |
| `LOG_DIR` | `./logs` | Log directory |
| `CONFIG_FILE` | `config.yaml` | Shared config path |
| `POSITION_MONITOR_CONFIG` | `$CONFIG_FILE` | PMS config path |
| `ORDER_SERVICE_CONFIG` | `$CONFIG_FILE` | UOS config path |
| `POSITION_MONITOR_PORT` | `8080` | PMS HTTP port |
| `POSITION_MONITOR_URL` | `http://localhost:8080` | UOS → PMS URL |
| `POSITION_MONITOR_ORDER_SERVICE_URL` | `http://localhost:8081` | PMS → UOS URL |
| `BINANCE_TESTNET` | `false` | Use Binance testnet (only `true` enables it) |
| `HYPERLIQUID_TESTNET` | `false` | Use Hyperliquid testnet (only `true` enables it) |
| `DERIBIT_TESTNET` | `false` | Use Deribit testnet (only `true` enables it) |

> **Testnet is opt-in: unconfigured means MAINNET.** All three exchanges support testnet.
> A flag is only ever turned on by the exact string `true` — from the `BINANCE_TESTNET` /
> `HYPERLIQUID_TESTNET` / `DERIBIT_TESTNET` environment variables, or from the matching
> `exchange:` keys in `config.yaml`. Anything else, including entirely missing
> configuration, means **mainnet with real funds**. The UOS logs the resolved network for
> each exchange at startup — always check that line before sending a signal.

**Precedence is uniform across both services:** environment variables > `config.yaml`
`exchange:` section > mainnet default. The UOS and the PMS read the same YAML keys, so a
single `exchange:` block controls both order placement (UOS) and order-status monitoring,
price streams and Deribit sync (PMS). `deploy.sh` passes the same env vars to both
services, keeping them consistent.

> Both parsers are minimal hand-rolled readers with different supported keys:
> [`internal/config/config.go`](internal/config/config.go) for the UOS (`server`, `storage`,
> `defaults`, `notification`, `exchange`, `reporter`, `filter_sync_interval`,
> `position_sync_interval`) and
> [`cmd/position_monitor_service/config.go`](cmd/position_monitor_service/config.go) for
> the PMS (which additionally handles `runtime`, `deribit`,
> `deribit_position_sync`). A key placed in the wrong section, or one that only the other
> service understands, is silently ignored rather than rejected.

## API Overview

### User Order Service (Port 8081)

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Health check |
| `GET` | `/api/v1/state` | System state |
| `GET` | `/api/v1/users` | List users (credentials omitted) |
| `POST` | `/api/v1/users/create` | Create user |
| `GET` | `/api/v1/user-strategies` | List strategies (`user_id`, `user_name`, `strategy_name` filters) |
| `POST` | `/api/v1/signals` | Submit trading signal |
| `POST` | `/api/v1/orders` | Agent order interface |
| `POST` | `/rpc/v1/order/status/update` | Update order status (PMS→UOS) |
| `POST` | `/rpc/v1/order/position-metadata` | Query order metadata (PMS→UOS) |
| `POST` | `/rpc/v1/filters/reload` | Reload exchange filters (PMS→UOS) |
| `POST` | `/rpc/v1/strategy/get-or-create` | Get or create strategy (PMS→UOS) |
| `POST` | `/rpc/v1/strategy-asset/get-or-create` | Get or create strategy asset (PMS→UOS) |
| `POST` | `/rpc/v1/user-strategy/get-or-create` | Get or create user strategy (PMS→UOS) |

### Position Monitor Service (Port 8080)

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Health check |
| `POST` | `/api/v1/rules` | Register risk rule |
| `GET` | `/api/v1/user-order-positions` | List order positions |
| `GET` | `/api/v1/user-positions` | List aggregated positions |
| `POST` | `/api/v1/positions/close-all` | Close all positions |
| `POST` | `/api/v1/positions/close-partial` | Partially close a position |
| `GET` | `/api/v1/exchange/positions` | Query live exchange positions |
| `POST` | `/rpc/v1/user-order-positions/query` | Query positions (UOS→PMS) |
| `POST` | `/rpc/v1/uprunning-order/create` | Create uprunning order (UOS→PMS) |
| `POST` | `/rpc/v1/market-price/get` | Get market price (UOS→PMS) |
| `POST` | `/rpc/v1/rules/create` | Create rule via RPC (UOS→PMS) |
| `POST` | `/rpc/v1/rules/invalidate-for-strategy` | Invalidate strategy rules (UOS→PMS) |
| `POST` | `/rpc/v1/order-position-metadata/query` | Query order position metadata (UOS→PMS) |

> Full API documentation: [docs/API.md](docs/API.md) · Position queries: [docs/position_api.md](docs/position_api.md)

## Risk Rule Engine

Rules are evaluated every sync cycle (default 10s). Each rule binds to a `user_strategy_id` and triggers a close/reduce action when conditions are met.

### Supported Conditions

| Condition | Description |
|---|---|
| `roi` | ROI percentage |
| `price_btc` / `price_sol` | Asset price threshold |
| `holding_time` | Position duration (seconds) |
| `profit_trigger` | Profit activation threshold |
| `profit_drawdown_pct` | Drawdown from peak profit |
| `position_<symbol>` | Cross-position condition (e.g., `position_btc-28aug26-67000-c`) |

### Example: Register a Stop-Loss Rule

```bash
curl -X POST http://localhost:8080/api/v1/rules \
  -H "Content-Type: application/json" \
  -d '{
    "user_strategy_id": 100,
    "condition_name": "roi",
    "operator": "<=",
    "value": -0.02,
    "action": "reduce",
    "quantity_pct": 1.0
  }'
```

### Default Auto-Generated Rules

New strategies automatically get rules based on `config.yaml` defaults:

- **Stop Loss**: `roi <= stop_loss_pct` (default -2%)
- **Trailing Take-Profit**: `profit_drawdown_pct >= 0.05` after `trailing_activation_pct >= 0.05`
- **Time Stop**: `holding_time >= time_stop_hours * 3600` (if configured)

## Project Structure

```
cmd/
├── position_monitor_service/    # PMS main + API handlers
├── user_order_service/          # UOS main + signal handlers
├── exchange_position_reporter/  # Hourly position snapshot reporter
├── sync_deribit_positions/      # Utility: manual Deribit sync
└── test_deribit_positions/      # Utility: test Deribit positions

internal/
├── api/           # HTTP endpoint handlers
├── config/        # YAML & env configuration
├── exchange/      # Exchange interface + adapters
│   ├── binance/   # Binance Futures
│   ├── hyperliquid/ # Hyperliquid DEX
│   ├── deribit/   # Deribit Options
│   └── ws/        # WebSocket connection management
├── notification/  # Webhook notifications (open/close/test/deribit/manual)
├── order/         # Order & strategy models
├── persistence/   # CSV engine, GlobalState, StateRepository
├── rpc/           # HTTP JSON RPC client/server
├── risk/          # Risk control core
│   ├── aggregator/  # Position aggregation
│   ├── config/      # Rule store
│   ├── engine/      # Rule evaluation
│   ├── executor/    # Action execution
│   ├── metrics/     # PnL, ROI calculations
│   ├── pipeline/    # Pipeline orchestration
│   ├── scheduler/   # Sync loop & scheduling
│   ├── signal/      # Risk signal types
│   └── state/       # Atomic GlobalState
├── signal/         # Signal handler (open/close/reverse)
└── deribit_position_sync/  # Deribit position synchronization

data/              # CSV persistence files (gitignored)
docs/              # Documentation
```

## Development

### Make Targets

| Command | Description |
|---|---|
| `make all` | Run tests + build |
| `make test` | Run all tests |
| `make test-unit` | Run unit tests (`./internal/...`) |
| `make build` | Build all services to `bin/` |
| `make lint` | Run golangci-lint |
| `make coverage` | Generate coverage report |
| `make clean` | Remove build artifacts |
| `make run` | Run PMS via `go run` |
| `make run-order` | Run UOS via `go run` |

### Testing

```bash
# Quick unit test
make test-unit

# Full test suite
make test

# Coverage report
make coverage
```

### Pre-Commit Checklist

```bash
go fmt ./...
make lint
make test
```

## Documentation

| Document | Description |
|---|---|
| [docs/API.md](docs/API.md) | Full API reference (REST + RPC) |
| [docs/position_api.md](docs/position_api.md) | Position query & close endpoints |
| [docs/RUNBOOK.md](docs/RUNBOOK.md) | Operations runbook |
| [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) | Contributing guide |
| [docs/deribit-integration.md](docs/deribit-integration.md) | Deribit integration details |
| [CHANGELOG.md](CHANGELOG.md) | Version history |

Deployment and configuration are covered in [Quick Start](#quick-start) and [Configuration](#configuration) above.

## 中文文档

### 信号格式

支持两种信号格式：**标准格式**和**策略信号格式**。

#### 标准信号格式

适用于简单的开仓/平仓操作：

```json
{
  "user_id": 1,
  "user_strategy_id": 100,
  "symbol": "BTCUSDT",
  "pos_type": 2,
  "exchange": "binance",
  "cash": 1000.0,
  "trigger_price": 50000.0,
  "slippage": 0.001,
  "side": 0,
  "order_type": 1,
  "leverage": 10
}
```

| 字段 | 说明 | 示例 |
|---|---|---|
| `user_id` | 用户ID | `1` |
| `user_strategy_id` | 策略ID | `100` |
| `symbol` | 交易对 | `BTCUSDT`, `ETHUSDC` |
| `pos_type` | 持仓类型 | `1`=现货, `2`=合约 |
| `exchange` | 交易所 | `binance`, `hyperliquid`, `deribit`, `mock` |
| `cash` | 投入资金 | `1000.0` |
| `trigger_price` | 触发价格 | `50000.0` |
| `slippage` | 滑点容忍度 | `0.001` |
| `side` | 方向 | `0`=做多, `1`=做空 |
| `order_type` | 订单类型 | `0`=限价, `1`=市价 |
| `leverage` | 杠杆倍数 | `10` |

#### 策略信号格式（推荐）

包含完整的策略信息和嵌套信号配置：

```json
{
  "SignalID": "sig_test_001",
  "SignalTimestamp": "2026-06-23 08:00:00",
  "symbol": "ETHUSDC",
  "pos_type": 2,
  "strategy_type": "CTAFutureFactory",
  "risk_strategy_type": "traditional",
  "user_id": 103,
  "strategy": {
    "name": "OBVATR",
    "version": "2",
    "internal": "4h",
    "description": "OBVATR strategy",
    "cash": 100,
    "parts": 5,
    "leverage": 6,
    "valid_before": "2030-12-31 08:00:00"
  },
  "signal": {
    "action": "sell",
    "exchange": "hyperliquid",
    "cash": 10,
    "trigger_price": 2.068,
    "slippage": 0,
    "order_type": 1,
    "valid_before": "2030-06-24 08:01:02"
  }
}
```

**strategy 对象字段**：

| 字段 | 说明 | 示例 |
|---|---|---|
| `name` | 策略名称 | `"OBVATR"` |
| `version` | 策略版本 | `"2"` |
| `internal` | 时间周期 | `"4h"`, `"1h"`, `"15m"` |
| `description` | 策略描述 | `"OBVATR strategy"` |
| `cash` | 策略总资金 | `100` |
| `parts` | 分批开仓次数 | `5` |
| `leverage` | 杠杆倍数 | `6` |
| `valid_before` | 策略有效期 | `"2030-12-31 08:00:00"` |

**signal 对象字段**：

| 字段 | 说明 | 示例 |
|---|---|---|
| `action` | 信号动作 | `"buy"`, `"sell"`, `"buy_close"`, `"sell_close"` |
| `exchange` | 交易所 | `"hyperliquid"` |
| `cash` | 本次投入资金 | `10` |
| `trigger_price` | 触发价格 | `2.068` |
| `slippage` | 滑点容忍度 | `0` |
| `order_type` | 订单类型 | `0`=限价, `1`=市价 |
| `valid_before` | 信号有效期 | `"2030-06-24 08:01:02"` |

#### Action 动作类型

| Action | 说明 | 行为 |
|---|---|---|
| `buy` | 开多仓 | 明确开多 |
| `sell` | 开空仓 | 明确开空 |
| `buy_close` | 平空仓 | 平空仓 + 自动创建止盈止损规则 |
| `sell_close` | 平多仓 | 平多仓 + 自动创建止盈止损规则 |
| `reverse_long` | 反转做多 | 平空仓 + 开多仓 |
| `reverse_short` | 反转做空 | 平多仓 + 开空仓 |

> 以上是 `Action` 的全部合法取值（见 [`internal/signal/strategy_signal.go`](internal/signal/strategy_signal.go)）。
> 其他值（包括 `open`）会被拒绝并返回 `unknown action`。

### 风控条件详解

| 风控条件 | operator | value 示例 | 说明 | 触发场景 |
|---|---|---|---|---|
| `roi` | `<=`, `>=` | `-0.02` (止损 -2%)<br>`0.15` (止盈 +15%) | 收益率阈值 | 亏损超过 2% 或盈利超过 15% |
| `holding_time` | `>` | `259200` (72小时)<br>`86400` (24小时) | 持仓时长（秒） | 持仓超过 72 小时强制平仓 |
| `profit_drawdown_pct` | `<=` | `0.5` (回撤 50%)<br>`0.3` (回撤 30%) | 盈利回撤比例 | 最高盈利回撤 50% 止盈 |
| `price_btc` | `<`, `>` | `45000.0`<br>`60000.0` | BTC 价格阈值 | BTC 跌破 45000 或突破 60000 |
| `price_sol` | `<`, `>` | `100.0`<br>`150.0` | SOL 价格阈值 | SOL 跌破 100 或突破 150 |
| `always` | `==` | `true` | 立即触发 | 开仓后立即激活链式规则 |
| `position_<symbol>` | `>=` | `1` | 跨仓位条件 | 指定 symbol 有持仓时触发 |

**风控动作**：
- `reduce`: 减仓/平仓（支持部分平仓，通过 `quantity_pct` 控制）
  - `quantity_pct: 1.0` — 全部平仓
  - `quantity_pct: 0.5` — 平仓 50%
- 链式规则：`action` 设置为规则 ID 字符串（如 `"123"`），触发后激活指定规则

#### 示例

**止损规则**（亏损 2% 触发）：
```json
{
  "user_strategy_id": 100,
  "condition_name": "roi",
  "operator": "<=",
  "value": -0.02,
  "quantity_pct": 1.0
}
```

**盈利回撤止盈**（最高盈利回撤 50% 触发）：
```json
{
  "user_strategy_id": 100,
  "condition_name": "profit_drawdown_pct",
  "operator": "<=",
  "value": 0.5,
  "quantity_pct": 0.5
}
```
> 说明：最高盈利 1000U，当前盈利 400U，回撤比例 = (1000-400)/1000 = 0.6，触发平仓 50%

**链式规则**（开仓后立即激活止损止盈）：
```json
{
  "user_strategy_id": 100,
  "condition_name": "always",
  "operator": "==",
  "value": true,
  "action": "123"
}
```
> 说明：`action` 设置为另一个规则的 ID，开仓后立即激活规则 123

### 开仓自动失效旧规则

当策略开新仓位时，自动失效该策略的所有活跃风控规则：
- 避免旧规则错误触发新仓位的平仓
- 防止历史规则污染新持仓
- 简化策略管理复杂度

### 规则状态机

```
active (待触发) → in_use (触发中) → inactive (已失效)
  ↓
开仓时自动失效所有 active 规则
```

### 常见问题

**Q1: Filter Sync 失败，CSV 文件为空**

原因：网络连接问题或代理配置错误
```bash
# 检查网络连接
curl -v https://api.binance.com/api/v3/ping
# 配置代理排除
export no_proxy="binance.com,hyperliquid.xyz,deribit.com"
```

**Q2: 规则创建失败，返回 4004/4005**

- 4004: 策略不存在
- 4005: 策略无活跃持仓

解决：确认 `user_strategy_id` 存在 → 先发送开仓信号创建持仓 → 再创建风控规则

**Q3: 开仓后旧规则未失效**

排查：检查 UOS 和 PMS 服务是否都正常运行 → 查看 UOS 日志中的警告信息 → 确认 PMS 的 RPC 端点可访问

**Q4: 测试网 API 连接失败**

- 使用 mainnet 进行生产交易
- 检查防火墙和代理设置
- 使用 Mock Exchange 进行本地测试

## Security

> **This service has no built-in authentication. Never expose it to the public internet.**

Both services deliberately bind to `127.0.0.1` only ([`cmd/user_order_service/main.go`](cmd/user_order_service/main.go), [`cmd/position_monitor_service/main.go`](cmd/position_monitor_service/main.go)) — they are not reachable from outside the host by default. This is the primary safeguard, and it must not be weakened.

### What "no authentication" means

There is no API key check, token validation, or authorization middleware on any endpoint. Anyone able to reach the listening port can:

- Place and close orders with real funds
- Create, modify, and delete risk-control rules
- Read all strategies and positions

The only barrier is network reachability. Treat port access as equivalent to full account access.

### Deployment requirements

- **Do not change the bind address to `0.0.0.0`** or any external interface.
- **Do not publish the container ports** if running under Docker — use an internal network. Note that `-p 8081:8081` exposes the port on *all* host interfaces regardless of what the process binds to; use `-p 127.0.0.1:8081:8081` if you must map it.
- Restrict host firewall rules to ports 8080 and 8081 on loopback.

### On reverse proxies

Source comments in `main.go` mention routing external traffic through an nginx reverse proxy. **This is a deployment suggestion, not a feature — this repository ships no proxy configuration of any kind.** You must build it yourself, and you must configure authentication yourself.

Be aware that a reverse proxy provides **no authentication by default**. A minimal config such as:

```nginx
location / {
    proxy_pass http://127.0.0.1:8081;   # NO auth — do not do this
}
```

is *worse* than having no proxy at all: it takes a service that was unreachable from outside and publishes it, unauthenticated, to anyone who can reach the proxy. Authentication (Basic Auth, mTLS, an IP allowlist, an auth gateway) must be added explicitly, on every `location` block, or that path is open.

If you only need occasional remote access, an SSH tunnel avoids the whole problem — authentication is handled by SSH and no new listening port is created:

```bash
ssh -L 8081:127.0.0.1:8081 user@your-server
# then reach the service at http://127.0.0.1:8081 locally
```

### Credential handling

- API keys and secrets live in `data/users.csv`, stored **in plaintext**. Restrict file permissions (`chmod 600`) and never commit this file — it is gitignored.
- `data/`, `logs/`, `config.yaml`, and `test_user_order.yaml` are all gitignored because they contain live credentials, webhook URLs, or trading history. Verify they are absent before pushing to any remote.
- Rotate exchange API keys if the host is ever compromised. Where the exchange supports it, disable withdrawal permissions on the keys used here.

## Disclaimer

**This software is provided for educational and informational purposes only.** It does not constitute financial advice, investment advice, trading advice, or any other sort of advice.

- Trading cryptocurrencies involves substantial risk of loss and is not suitable for every investor. You could lose all your invested capital.
- Past performance is not indicative of future results. The developers and contributors of this project are not responsible for any trading decisions or financial outcomes.
- Use this software at your own risk. You are solely responsible for your own trading activity and any gains or losses incurred.
- Always do your own research (DYOR) and consult a qualified financial advisor before making any investment decisions.

## License

Licensed under the [Apache License 2.0](LICENSE).
