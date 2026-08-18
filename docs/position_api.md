# Position API 接口文档

## 概述

Position API 提供订单层仓位和聚合仓位的查询功能。

**数据模型：**

| 数据表 | 路由 | 说明 |
|--------|------|------|
| `user_order_positions` | `/api/v1/user-order-positions` | 订单层仓位（每个订单对应一条记录） |
| `user_positions` | `/api/v1/user-positions` | 聚合仓位（策略级别的聚合数据） |

---

## 1. User Order Positions API（订单层仓位）

### 1.1 获取订单仓位列表

**请求：**
```
GET /api/v1/user-order-positions
```

**查询参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `user_strategy_id` | uint64 | 否 | 用户策略 ID（最高优先级） |
| `strategy_name` | string | 否 | 策略名称（可独立使用或配合 `user_id`） |
| `user_id` | uint64 | 否 | 用户 ID（配合 `strategy_name` 时做后缀适配） |
| `user_name` | string | 否* | 用户名称（唯一，可单独使用或配合 exchange） |
| `exchange` | string | 否* | 交易所："binance", "hyperliquid"（可配合 user_name 或单独使用） |
| `asset` | string | 否 | 资产名称（如 "BTCUSDT", "SOLUSDC"） |
| `side` | int | 否 | 方向：0=多单，1=空单 |
| `deleted` | int | 否 | 状态：0=活跃，1=已平仓 |
| `pos_type` | int | 否 | 仓位类型：1=现货，2=合约（默认 2） |
| `created_from` | string | 否 | 创建时间起始（RFC3339，含），按 `created_at` 过滤 |
| `created_to` | string | 否 | 创建时间截止（RFC3339，含），按 `created_at` 过滤 |
| `close_from` | string | 否 | 平仓时间起始（RFC3339，含），按 `close_time` 过滤；传入任一 close 参数时排除活跃仓位（`close_time` 为 null） |
| `close_to` | string | 否 | 平仓时间截止（RFC3339，含），按 `close_time` 过滤；传入任一 close 参数时排除活跃仓位（`close_time` 为 null） |
| `page` | int | 否 | 页码，默认 1 |
| `page_size` | int | 否 | 每页数量，最大 100，不传则不分页 |

**参数优先级说明：**

1. **最高优先级：`user_strategy_id`**
   - 直接查询指定策略的所有订单仓位
   - 其他参数作为过滤条件

2. **第二优先级：`strategy_name`**
   - 可独立使用：跨用户查询同名策略的所有订单仓位（不做后缀适配）
   - 配合 `user_id`：按交易所适配后缀（USDT/USDC），转为 `user_strategy_id`

3. **第三优先级：`user_name` + `exchange`**
   - 查询指定用户在特定交易所的所有订单仓位
   - `user_name` 在系统中唯一
   - 最推荐的前端 Agent 使用方式

4. **第四优先级：仅 `user_name`**
   - 查询指定用户所有交易所的所有订单仓位
   - 返回范围较大，建议配合 `exchange` 使用

5. **第五优先级：仅 `exchange`**
   - 查询该交易所所有用户的所有订单仓位
   - Agent 可用于获取"币安所有仓位"等场景

6. **默认：无上述参数**
   - 查询所有订单仓位（可配合其他过滤条件）

**说明：**
- 参数优先级从高到低：`user_strategy_id` → `strategy_name` → `user_name+exchange` → `user_name` → `exchange` → 无参数
- `strategy_name` 可独立使用（跨用户查询），配合 `user_id` 时自动适配交易所后缀
- `page_size` 不传时返回全部数据（不分页），传值时启用分页
- 最大分页数量为 100

**请求示例：**

```bash
# 查询所有活跃订单仓位
GET /api/v1/user-order-positions?deleted=0

# 查询指定策略的订单仓位（通过 user_strategy_id）
GET /api/v1/user-order-positions?user_strategy_id=996

# 查询指定策略的订单仓位（通过 user_id + strategy_name，自动适配后缀）
GET /api/v1/user-order-positions?user_id=100&strategy_name=my_strategy

# 查询同名策略的所有订单仓位（跨用户，不做后缀适配）
GET /api/v1/user-order-positions?strategy_name=DOLPHIN_USDT

# 按创建时间范围查询
GET /api/v1/user-order-positions?user_id=1&created_from=2026-07-01T00:00:00Z&created_to=2026-07-31T23:59:59Z

# 按平仓时间范围查询（仅返回已平仓仓位，活跃仓位被排除）
GET /api/v1/user-order-positions?user_id=1&close_from=2026-07-01T00:00:00Z&close_to=2026-07-31T23:59:59Z

# 查询指定用户在特定交易所的所有订单仓位（推荐 Agent 使用）
GET /api/v1/user-order-positions?user_name=test_strategy&exchange=binance

# 查询指定用户所有交易所的订单仓位
GET /api/v1/user-order-positions?user_name=test_strategy

# 查询币安所有用户的所有订单仓位（Agent 场景）
GET /api/v1/user-order-positions?exchange=binance

# 查询指定资产的订单仓位
GET /api/v1/user-order-positions?asset=SOLUSDC&deleted=0

# 组合过滤：查询用户在特定交易所的特定资产仓位
GET /api/v1/user-order-positions?user_name=test_hy&exchange=hyperliquid&asset=SOLUSDC

# 分页查询
GET /api/v1/user-order-positions?page=1&page_size=20
```

**响应：**

**成功响应（200）：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "list": [
      {
        "id": 1,
        "user_id": 100,
        "uprunning_order_id": 200,
        "user_order_id": 300,
        "user_strategy_id": 996,
        "risk_control_strategy_id": 0,
        "exchange": "hyperliquid",
        "pos_type": 2,
        "asset": "SOLUSDC",
        "current_price": 80.2650,
        "quantity": 10.0,
        "pos_value": 802.65,
        "leverage": 6,
        "deleted": 0,
        "init_margin": 133.775,
        "pos_price": 79.5,
        "pnl_value": 7.65,
        "side": 0,
        "close_time": null,
        "user_name": "alice",
        "strategy_name": "cta_BTCUSDT",
        "created_at": "2026-07-02T10:00:00Z",
        "updated_at": "2026-07-03T00:00:00Z"
      }
    ],
    "total": 1,
    "page": 1,
    "page_size": 20
  }
}
```

**不分页时的响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "list": [...],
    "total": 10,
    "page": 1,
    "page_size": 0
  }
}
```

**错误响应：**

**参数错误（400）：**
```json
{
  "code": 1001,
  "message": "Invalid user_id",
  "data": null
}
```

```json
{
  "code": 1001,
  "message": "Strategy not found",
  "data": null
}
```

**字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | uint64 | 仓位 ID |
| `user_id` | uint64 | 用户 ID |
| `uprunning_order_id` | uint64 | 运行中的订单 ID（交易所订单） |
| `user_order_id` | uint64 | 用户订单 ID（内部订单） |
| `user_strategy_id` | uint64 | 用户策略 ID |
| `risk_control_strategy_id` | uint64 | 风控策略 ID |
| `exchange` | string | 交易所："binance", "hyperliquid" |
| `pos_type` | int | 仓位类型：1=现货，2=合约 |
| `asset` | string | 资产名称（如 "BTCUSDT", "SOLUSDC"） |
| `current_price` | float64 | 当前价格（最新市场价格） |
| `quantity` | float64 | 数量 |
| `pos_value` | float64 | 仓位价值 = current_price × quantity |
| `leverage` | int | 杠杆倍数 |
| `deleted` | int | 状态：0=活跃，1=已平仓 |
| `init_margin` | float64 | 初始保证金 |
| `pos_price` | float64 | 开仓价格（入场价） |
| `pnl_value` | float64 | 未实现盈亏（实时计算） |
| `side` | int | 方向：0=多单，1=空单 |
| `close_time` | string/null | 平仓时间（ISO 8601，活跃仓位为 null） |
| `user_name` | string | 用户名称（enrichment，来自 users 表） |
| `strategy_name` | string | 策略名称（enrichment，来自 user_strategies 表） |
| `created_at` | string | 创建时间（ISO 8601） |
| `updated_at` | string | 更新时间（ISO 8601） |

**响应字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `list` | array | 仓位列表 |
| `total` | int | 总数量 |
| `page` | int | 当前页码 |
| `page_size` | int | 每页数量（0 表示不分页） |

---

### 1.2 获取单个订单仓位

**请求：**
```
GET /api/v1/user-order-positions/:id
```

**路径参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `id` | uint64 | 是 | 仓位 ID |

**请求示例：**
```bash
GET /api/v1/user-order-positions/1
```

**响应：**

**成功响应（200）：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "user_id": 100,
    "uprunning_order_id": 200,
    "user_order_id": 300,
    "user_strategy_id": 996,
    "risk_control_strategy_id": 0,
    "exchange": "hyperliquid",
    "pos_type": 2,
    "asset": "SOLUSDC",
    "current_price": 80.2650,
    "quantity": 10.0,
    "pos_value": 802.65,
    "leverage": 6,
    "deleted": 0,
    "init_margin": 133.775,
    "pos_price": 79.5,
    "pnl_value": 7.65,
    "side": 0,
    "close_time": null,
    "user_name": "alice",
    "strategy_name": "cta_BTCUSDT",
    "created_at": "2026-07-02T10:00:00Z",
    "updated_at": "2026-07-03T00:00:00Z"
  }
}
```

**错误响应：**

**参数错误（400）：**
```json
{
  "code": 1001,
  "message": "Invalid id",
  "data": null
}
```

**内部错误（500）：**
```json
{
  "code": 5001,
  "message": "user_order_position 1 not found",
  "data": null
}
```

---

## 2. User Positions API（聚合仓位）

### 2.1 获取聚合仓位列表

**请求：**
```
GET /api/v1/user-positions
```

**查询参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `user_strategy_id` | uint64 | 否 | 用户策略 ID（最高优先级） |
| `strategy_name` | string | 否 | 策略名称（可独立使用或配合 `user_id`） |
| `user_id` | uint64 | 否 | 用户 ID（配合 `strategy_name` 时做后缀适配） |
| `user_name` | string | 否* | 用户名称（唯一，可单独使用或配合 exchange） |
| `exchange` | string | 否* | 交易所："binance", "hyperliquid"（可配合 user_name 或单独使用） |
| `deleted` | int | 否 | 状态：0=活跃，1=已平仓 |
| `pos_type` | int | 否 | 仓位类型：1=现货，2=合约（默认 2） |
| `created_from` | string | 否 | 创建时间起始（RFC3339，含），按 `created_at` 过滤 |
| `created_to` | string | 否 | 创建时间截止（RFC3339，含），按 `created_at` 过滤 |
| `close_from` | string | 否 | 平仓时间起始（RFC3339，含），按 `close_time` 过滤；传入任一 close 参数时排除活跃仓位（`close_time` 为 null） |
| `close_to` | string | 否 | 平仓时间截止（RFC3339，含），按 `close_time` 过滤；传入任一 close 参数时排除活跃仓位（`close_time` 为 null） |
| `page` | int | 否 | 页码，默认 1 |
| `page_size` | int | 否 | 每页数量，默认 10，最大 100 |

**参数优先级说明：**

1. **最高优先级：`user_strategy_id`**
   - 直接查询指定策略的聚合仓位

2. **第二优先级：`strategy_name`**
   - 可独立使用：跨用户查询同名策略的所有聚合仓位（不做后缀适配）
   - 配合 `user_id`：按交易所适配后缀（USDT/USDC），转为 `user_strategy_id`

3. **第三优先级：`user_name` + `exchange`**
   - 查询指定用户在特定交易所的所有聚合仓位
   - 最推荐的前端 Agent 使用方式

4. **第四优先级：仅 `user_name`**
   - 查询指定用户所有交易所的所有聚合仓位

5. **第五优先级：仅 `exchange`**
   - 查询该交易所所有用户的所有聚合仓位
   - Agent 可用于获取"币安所有仓位汇总"等场景

6. **默认：无上述参数**
   - 查询所有聚合仓位（可配合其他过滤条件）

**说明：**
- 参数优先级从高到低：`user_strategy_id` → `strategy_name` → `user_name+exchange` → `user_name` → `exchange` → 无参数
- `strategy_name` 可独立使用（跨用户查询），配合 `user_id` 时自动适配交易所后缀
- 聚合仓位默认启用分页（page_size=10），最大 100
- 聚合仓位是策略级别的汇总数据，包含 ROI/PnL 等风控指标

**请求示例：**

```bash
# 查询所有活跃聚合仓位
GET /api/v1/user-positions?deleted=0

# 查询指定策略的聚合仓位（通过 user_strategy_id）
GET /api/v1/user-positions?user_strategy_id=996

# 查询指定策略的聚合仓位（通过 user_id + strategy_name，自动适配后缀）
GET /api/v1/user-positions?user_id=100&strategy_name=my_strategy

# 查询同名策略的所有聚合仓位（跨用户，不做后缀适配）
GET /api/v1/user-positions?strategy_name=DOLPHIN_USDT

# 按创建时间范围查询
GET /api/v1/user-positions?user_id=1&created_from=2026-07-01T00:00:00Z&created_to=2026-07-31T23:59:59Z

# 按平仓时间范围查询（仅返回已平仓仓位，活跃仓位被排除）
GET /api/v1/user-positions?user_id=1&close_from=2026-07-01T00:00:00Z&close_to=2026-07-31T23:59:59Z

# 查询指定用户在特定交易所的所有聚合仓位（推荐 Agent 使用）
GET /api/v1/user-positions?user_name=test_strategy&exchange=binance

# 查询指定用户所有交易所的聚合仓位
GET /api/v1/user-positions?user_name=test_strategy

# 查询币安所有用户的聚合仓位（Agent 场景）
GET /api/v1/user-positions?exchange=binance

# 分页查询
GET /api/v1/user-positions?page=1&page_size=20
```

**响应：**

**成功响应（200）：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "list": [
      {
        "id": 1000,
        "user_id": 100,
        "user_strategy_id": 996,
        "exchange": "hyperliquid",
        "pos_type": 2,
        "current_price": 80.2650,
        "quantity": 10.0,
        "latest_market_capitalization": 802.65,
        "roi": 0.0347,
        "pnl": 7.65,
        "win_rate": 0.0,
        "maximum_drawdown": 0.0,
        "total_margin": 133.775,
        "max_profit_percentage": 0.0388,
        "max_loss_percentage": 0.0,
        "open_trades": 1,
        "closed_trades": 0,
        "profit_trades": 1,
        "loss_trades": 0,
        "deleted": 0,
        "close_time": null,
        "user_name": "alice",
        "strategy_name": "cta_BTCUSDT",
        "created_at": "2026-07-02T10:00:00Z",
        "updated_at": "2026-07-03T00:00:00Z",
        "risk_control_strategy_id": 0
      }
    ],
    "total": 1,
    "page": 1,
    "page_size": 10
  }
}
```

**错误响应：**

**参数错误（400）：**
```json
{
  "code": 1001,
  "message": "Invalid user_id",
  "data": null
}
```

```json
{
  "code": 1001,
  "message": "Strategy not found",
  "data": null
}
```

**字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | uint64 | 聚合仓位 ID |
| `user_id` | uint64 | 用户 ID |
| `user_strategy_id` | uint64 | 用户策略 ID |
| `exchange` | string | 交易所 |
| `pos_type` | int | 仓位类型：1=现货，2=合约 |
| `current_price` | float64 | 当前价格 |
| `quantity` | float64 | 总数量（聚合所有订单仓位） |
| `latest_market_capitalization` | float64 | 最新市值 = current_price × quantity |
| `roi` | float64 | 收益率（含杠杆）= (pnl / total_margin) × leverage |
| `pnl` | float64 | 未实现盈亏（聚合） |
| `win_rate` | float64 | 胜率 = profit_trades / (profit_trades + loss_trades) |
| `maximum_drawdown` | float64 | 最大回撤 |
| `total_margin` | float64 | 总保证金（聚合） |
| `max_profit_percentage` | float64 | 最高盈利百分比（用于回落止盈） |
| `max_loss_percentage` | float64 | 最大亏损百分比 |
| `open_trades` | int | 开仓交易数 |
| `closed_trades` | int | 已平仓交易数 |
| `profit_trades` | int | 盈利交易数 |
| `loss_trades` | int | 亏损交易数 |
| `deleted` | int | 状态：0=活跃，1=已平仓 |
| `close_time` | string/null | 平仓时间 |
| `user_name` | string | 用户名称（enrichment，来自 users 表） |
| `strategy_name` | string | 策略名称（enrichment，来自 user_strategies 表） |
| `created_at` | string | 创建时间 |
| `updated_at` | string | 更新时间 |
| `risk_control_strategy_id` | uint64 | 风控策略 ID |

**重要指标说明：**

| 指标 | 计算公式 | 说明 |
|------|---------|------|
| `roi` | `(pnl / total_margin) × leverage` | 收益率（含杠杆），规则评估时会除以杠杆得到价格波动 |
| `pnl` | Σ(position_pnl) | 聚合所有订单仓位的盈亏 |
| `total_margin` | Σ(init_margin) | 聚合所有订单仓位的保证金 |
| `max_profit_percentage` | max(roi_history) | 最高盈利百分比，用于回落止盈计算 |
| `max_loss_percentage` | min(roi_history) | 最大亏损百分比 |

---

### 2.2 获取单个聚合仓位

**请求：**
```
GET /api/v1/user-positions/:id
```

**路径参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `id` | uint64 | 是 | 聚合仓位 ID |

**请求示例：**
```bash
GET /api/v1/user-positions/1000
```

**响应：**

**成功响应（200）：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1000,
    "user_id": 100,
    "user_strategy_id": 996,
    "exchange": "hyperliquid",
    "pos_type": 2,
    "current_price": 80.2650,
    "quantity": 10.0,
    "latest_market_capitalization": 802.65,
    "roi": 0.0347,
    "pnl": 7.65,
    "win_rate": 0.0,
    "maximum_drawdown": 0.0,
    "total_margin": 133.775,
    "max_profit_percentage": 0.0388,
    "max_loss_percentage": 0.0,
    "open_trades": 1,
    "closed_trades": 0,
    "profit_trades": 1,
    "loss_trades": 0,
    "deleted": 0,
    "close_time": null,
    "user_name": "alice",
    "strategy_name": "cta_BTCUSDT",
    "created_at": "2026-07-02T10:00:00Z",
    "updated_at": "2026-07-03T00:00:00Z",
    "risk_control_strategy_id": 0
  }
}
```

**错误响应：**

**参数错误（400）：**
```json
{
  "code": 1001,
  "message": "Invalid id",
  "data": null
}
```

**内部错误（500）：**
```json
{
  "code": 5001,
  "message": "user_position 1000 not found",
  "data": null
}
```

---

## 3. 错误码定义

| 错误码 | 说明 |
|-------|------|
| 0 | 成功 |
| 1001 | 参数错误 |
| 5001 | 内部错误 |

---

## 4. 实现状态

- ✅ `GET /api/v1/user-order-positions` - 订单仓位列表
- ✅ `GET /api/v1/user-order-positions/:id` - 订单仓位详情
- ✅ `GET /api/v1/user-positions` - 聚合仓位列表
- ✅ `GET /api/v1/user-positions/:id` - 聚合仓位详情
- ✅ `POST /api/v1/positions/close-all` - 全部平仓
- ✅ `POST /api/v1/positions/close-partial` - 部分平仓
- ✅ `POST /api/v1/exchange/positions` - 查询交易所实时仓位

---

## 5. Exchange Positions API（交易所实时仓位查询）

### 5.1 查询交易所实时仓位

**请求：**
```
POST /api/v1/exchange/positions
```

**请求参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `user_name` | string | 是 | 用户名称 |
| `exchange` | string | 是 | 交易所："binance"、"hyperliquid" 或 "deribit" |

**示例请求：**

```json
{
  "user_name": "machineLightGbm",
  "exchange": "binance"
}
```

**成功响应（200）：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "positions": [
      {
        "Symbol": "BTCUSDT",
        "PositionSide": "LONG",
        "Quantity": 0.1,
        "EntryPrice": 45000.0,
        "MarkPrice": 45500.0,
        "UnrealizedPnl": 50.0,
        "Leverage": 10
      }
    ],
    "exchange": "binance",
    "user": "machineLightGbm"
  }
}
```

**字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `Symbol` | string | 交易对（Binance: BTCUSDT, Hyperliquid: NEARUSDC, Deribit: BTC-17JUL26-64000-P） |
| `PositionSide` | string | 方向："LONG" 或 "SHORT" |
| `Quantity` | float | 仓位大小（绝对值，始终为正数） |
| `EntryPrice` | float | 平均入场价格 |
| `MarkPrice` | float | 当前标记价格 |
| `UnrealizedPnl` | float | 未实现盈亏 |
| `Leverage` | int | 杠杆倍数 |

### 5.2 各交易所字段对比

| 字段 | Binance | Hyperliquid | Deribit |
|------|---------|-------------|---------|
| Symbol | ✅ BTCUSDT | ✅ NEARUSDC | ✅ BTC-17JUL26-64000-P |
| PositionSide | ✅ | ✅ | ✅ |
| Quantity | ✅ | ✅ | ✅ |
| EntryPrice | ✅ | ✅ | ✅ |
| MarkPrice | ✅ API直接返回 | ✅ 通过GetPrice获取 | ✅ API直接返回 |
| UnrealizedPnl | ✅ | ✅ | ✅ |
| Leverage | ✅ | ✅ | ✅ (期权固定为0) |

### 5.3 使用场景

1. **实时仓位查询** - 查询交易所当前的持仓情况
2. **仓位验证** - 验证系统记录与交易所实际仓位是否一致
3. **调试工具** - 在平仓前确认交易所实际仓位状态

### 5.4 注意事项

- **API 凭证要求**：用户必须配置完整的 `api_key` 和 `api_secret`
- **零仓位过滤**：仅返回非零仓位（数量 > 0）
- **Hyperliquid**: MarkPrice 通过 GetPrice 单独获取，SDK backoff 期间可能返回 0
---

## 6. Agent Supplementary Close Interface（Agent 补充平仓接口）

### 6.1 全部平仓

**请求：**
```
POST /api/v1/positions/close-all
```

**请求体：**
```json
{
  "user_name": "machineLightGbm",
  "exchange": "binance",
  "pos_type": 2,
  "asset": "BTC"
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `user_name` | string | 是 | 用户名 |
| `exchange` | string | 是 | 交易所名称（binance/hyperliquid） |
| `pos_type` | int | 是 | 仓位类型：0=任意, 1=现货, 2=合约 |
| `asset` | string | 是 | 基础资产或完整交易对（如 `BTC`、`BTCUSDT`、`XRPUSDC`），报价币后缀按交易所自动适配 |

**响应示例：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "rule_ids": [1, 2],
    "closed_count": 5,
    "strategy_count": 2
  }
}
```

**错误处理：**
- 如果部分策略创建规则失败，返回错误并报告失败的策略列表
- 失败时用户可以重试或手动处理

### 6.2 部分平仓（价格触发）

**请求：**
```
POST /api/v1/positions/close-partial
```

**请求体：**
```json
{
  "user_name": "machineLightGbm",
  "exchange": "binance",
  "pos_type": 2,
  "asset": "BTC",
  "price": 50000.0,
  "quantity_pct": 0.5,
  "trigger_type": "take_profit"
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `user_name` | string | 是 | 用户名 |
| `exchange` | string | 是 | 交易所名称（binance/hyperliquid） |
| `pos_type` | int | 是 | 仓位类型：0=任意, 1=现货, 2=合约 |
| `asset` | string | 是 | 基础资产或完整交易对，报价币后缀按交易所自动适配 |
| `price` | float | 是 | 触发价格 |
| `quantity_pct` | float | 是 | 平仓比例（0.0-1.0） |
| `trigger_type` | string | 是 | 触发类型：take_profit/stop_loss |

**响应示例：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "rule_id": 1,
    "condition": "price_btc >= 50000",
    "quantity_pct": 0.5
  }
}
```

**逻辑说明：**
- 根据仓位方向和触发类型自动确定比较操作符
- Long + take_profit → 价格 >= 目标价
- Long + stop_loss → 价格 <= 目标价
- Short + take_profit → 价格 <= 目标价
- Short + stop_loss → 价格 >= 目标价

### 6.3 使用场景

1. **紧急平仓** - Agent 检测到风险时快速全部平仓
2. **分批止盈止损** - 设置价格触发的部分平仓规则
3. **策略控制** - 根据市场条件自动调整仓位

### 6.4 安全保障

- **用户验证** - 必须提供有效的用户名和交易所
- **仓位验证** - 创建规则前验证仓位是否存在
- **错误追踪** - 部分失败时明确报告失败的策略
- **输入验证** - 验证交易所名称、pos_type 范围、价格有效性

---

## 查询优先级 (Query Priority)

### user-order-positions 接口

支持以下查询优先级：

#### Priority 1: user_strategy_id (最高优先级)
```bash
GET /api/v1/user-order-positions?user_strategy_id=123
```
- 仅通过策略ID查询
- 忽略其他参数

#### Priority 1.5: user_id only (新增)
```bash
GET /api/v1/user-order-positions?user_id=1
```
- 仅通过用户ID查询
- 返回该用户的所有持仓
- 可与其他筛选参数组合使用

#### Priority 2: user_id + strategy_name
```bash
GET /api/v1/user-order-positions?user_id=1&strategy_name=OBVATRV2_BTCUSDT
```
- 通过用户ID和策略名称组合查询
- 策略名称会根据交易所自动适配后缀

#### Priority 3-5: user_name based
```bash
# Priority 3: user_name + exchange
GET /api/v1/user-order-positions?user_name=test_user&exchange=binance

# Priority 4: user_name only
GET /api/v1/user-order-positions?user_name=test_user

# Priority 5: exchange only
GET /api/v1/user-order-positions?exchange=binance
```

### user-positions 接口

查询优先级与user-order-positions相同。

### 示例：组合筛选

```bash
# 仅查询user_id=1的活跃多头持仓
GET /api/v1/user-order-positions?user_id=1&side=0&deleted=0

# 查询user_id=1的合约持仓
GET /api/v1/user-order-positions?user_id=1&pos_type=2
```
