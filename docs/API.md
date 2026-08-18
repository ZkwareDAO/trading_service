# Trading Service API 接口文档

---

## 服务概述

本项目包含两个主要服务：

1. **User Order Service** - 处理交易信号和订单管理
2. **Position Monitor Service** - 监控持仓和执行风控规则

**基础信息**：

- **Base URL**: UOS `http://127.0.0.1:8081`，PMS `http://127.0.0.1:8080`
- **Content-Type**: `application/json`
- **时间格式**: 响应中的时间戳为 RFC3339（如 `2026-07-02T08:00:00Z`）；信号中的 `valid_before` 使用 `YYYY-MM-DD HH:MM:SS`
- **认证方式**: 无。服务仅绑定 `127.0.0.1`，依靠网络隔离保护。所有接口均无鉴权——能访问端口即等同于拥有完整账户权限（可下单、平仓、改风控规则）。**切勿暴露到公网**，详见 [README 安全说明](../README.md#security)。

---

## 一、User Order Service

**默认端口**: 根据配置文件 `config.yaml` 中的 `server.port` 设置

### 1.1 REST API 接口

#### 1.1.1 Health Check - 健康检查

**接口路径**: `GET /health`

**请求参数**: 无

**响应示例**:
```json
{
  "status": "healthy"
}
```

---

#### 1.1.2 Get State - 获取系统状态

**接口路径**: `GET /api/v1/state`

**请求参数**: 无

**响应示例**:
```json
{
  "users": 3
}
```

**字段说明**:
- `users`: 当前系统中的用户数量

---

#### 1.1.3 List Users - 获取用户列表

**接口路径**: `GET /api/v1/users`

**请求参数**: 无

**响应示例**:
```json
[
  {
    "id": 1,
    "name": "user1",
    "exchange": "binance",
    "api_key": "***",
    "api_secret": "***",
    "api_password": "",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  },
  {
    "id": 2,
    "name": "user2",
    "exchange": "hyperliquid",
    "api_key": "0x123...",
    "api_secret": "***",
    "api_password": "",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

**字段说明**:
- `id`: 用户ID
- `name`: 用户名称
- `exchange`: 交易所名称 (`binance`, `hyperliquid`, `mock`)
- `api_key`: API密钥（已脱敏显示）
- `api_secret`: API密钥密文（已脱敏显示）
- `api_password`: API密码（可选）
- `created_at`: 创建时间
- `updated_at`: 更新时间

---

#### 1.1.4 Signal - 处理交易信号

**接口路径**: `POST /api/v1/signals`

**说明**: 支持两种请求格式

---

**格式一: 标准信号格式**

**请求参数**:
```json
{
  "user_id": 1,                   // 必填: 用户ID
  "user_strategy_id": 100,        // 必填: 用户策略ID
  "symbol": "BTCUSDT",            // 必填: 交易符号
  "pos_type": 2,                  // 必填: 持仓类型 (1=现货, 2=合约)
  "exchange": "binance",          // 必填: 交易所名称
  "cash": 1000.0,                 // 必填: 投入资金金额
  "trigger_price": 50000.0,       // 必填: 触发价格
  "slippage": 0.001,              // 必填: 滑点容忍度
  "side": 0,                      // 必填: 方向 (0=做多, 1=做空)
  "order_type": 1,                // 必填: 订单类型 (0=限价单, 1=市价单)
  "leverage": 10                  // 必填: 杠杆倍数
}
```

**响应示例**:
```json
{
  "message": "success"
}
```

---

**格式二: 策略信号格式（嵌套结构）**

**请求参数**:
```json
{
  "SignalID": "sig-2024-001",                     // 必填: 信号唯一ID
  "SignalTimestamp": "2024-01-01T10:00:00Z",      // 必填: 信号时间戳
  "symbol": "BTC",                                // 必填: 基础资产符号
  "user_id": 1,                                   // 必填: 用户ID
  "pos_type": 2,                                  // 必填: 持仓类型
  "strategy_type": "trend_follow",                // 必填: 策略类型
  "risk_strategy_type": "cta_intraday",           // 必填: 风控策略类型
  "strategy": {                                   // 必填: 策略配置
    "name": "BTC Trend Strategy",
    "version": "1.0",
    "internal": "1h",
    "description": "Follow BTC trend",
    "params": {
      "threshold": 0.02,
      "stop_loss": 0.05
    },
    "valid_before": "2024-01-01T11:00:00Z",
    "cash": 1000.0,
    "parts": 3,
    "leverage": 10
  },
  "signal": {                                     // 必填: 信号订单配置
    "side": 0,                                    // 0=做多, 1=做空
    "action": "open",                             // 动作类型
    "exchange": "binance",
    "valid_before": "2024-01-01T11:00:00Z",
    "quantity": 0.1,
    "cash": 1000.0,
    "trigger_price": 50000.0,
    "slippage": 0.001,
    "order_type": 1
  }
}
```

**Action 类型说明**:
- `open`: 开仓（做多或做空）
- `buy`: 开多仓
- `sell`: 开空仓
- `buy_close`: 平空仓
- `sell_close`: 平多仓
- `reverse_long`: 反转做多（平空仓+开多仓）
- `reverse_short`: 反转做空（平多仓+开空仓）

**响应示例**:
```json
{
  "message": "success"
}
```

---

#### 1.1.5 Create User - 创建用户

**接口路径**: `POST /api/v1/users/create`

**请求参数**:
```json
{
  "name": "alice",              // 必填: 用户名称（唯一）
  "exchange": "binance",        // 必填: 交易所名称 (binance, hyperliquid, deribit, mock)
  "api_key": "your-api-key",    // 必填: API密钥
  "api_secret": "your-api-secret", // 必填: API密钥密文
  "api_password": ""            // 可选: API密码（Deribit等交易所需要）
}
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "name": "alice",
    "exchange": "binance",
    "created_at": "2026-07-10T10:00:00Z",
    "updated_at": "2026-07-10T10:00:00Z"
  }
}
```

**说明**: 响应中不返回敏感字段（api_key, api_secret, api_password）

**错误响应**:
- HTTP 400: 参数错误（缺少必填字段）
- HTTP 409: 用户名已存在

**示例请求**:
```bash
# 创建Binance用户
curl -X POST "http://localhost:8081/api/v1/users/create" \
  -H "Content-Type: application/json" \
  -d '{"name":"alice","exchange":"binance","api_key":"your_key","api_secret":"your_secret"}'

# 创建Deribit用户（需要api_password）
curl -X POST "http://localhost:8081/api/v1/users/create" \
  -H "Content-Type: application/json" \
  -d '{"name":"bob","exchange":"deribit","api_key":"your_key","api_secret":"your_secret","api_password":"your_password"}'
```

---

#### 1.1.6 List User Strategies - 获取用户策略列表

**接口路径**: `GET /api/v1/user-strategies`

**请求参数**:

| 参数 | 类型 | 必填 | 说明 | 示例 |
|------|------|------|------|------|
| user_id | int | 否 | 用户ID | 1 |
| user_name | string | 否 | 用户名 | alice |
| strategy_name | string | 否 | 策略名称（精确匹配） | ICT_1H |

**注意**: `user_id` 和 `user_name` 至少提供一个

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "id": 100,
      "user_id": 1,
      "name": "ICT_1H",
      "exchange": "binance",
      "cash": 1000.0,
      "parts": 5,
      "status": 1,
      "risk_strategy_type": "traditional",
      "orders_num": 0,
      "valid_before": "2026-12-31T23:59:59Z",
      "created_at": "2026-07-10T10:00:00Z",
      "updated_at": "2026-07-10T10:00:00Z"
    }
  ]
}
```

**示例请求**:
```bash
# 查询所有策略
curl "http://localhost:8081/api/v1/user-strategies"

# 按user_id查询
curl "http://localhost:8081/api/v1/user-strategies?user_id=1"

# 按user_name查询
curl "http://localhost:8081/api/v1/user-strategies?user_name=alice"

# 按strategy_name过滤
curl "http://localhost:8081/api/v1/user-strategies?user_id=1&strategy_name=ICT_1H"
```

---

### 1.2 RPC 接口

#### 1.2.1 Update Order Status - 更新订单状态

**接口路径**: `POST /rpc/v1/order/status/update`

**说明**: 由 Position Monitor Service 调用，用于更新订单状态

**请求参数**:
```json
{
  "user_order_id": 123,           // 必填: 用户订单ID
  "status": 2,                    // 必填: 订单状态 (2=已成交, 3=失败)
  "finished_at": "2024-01-01T10:30:00Z"  // 可选: 完成时间 (RFC3339格式)
}
```

**响应**: HTTP 200 OK (无响应体)

**错误响应**:
- HTTP 400: 参数错误
- HTTP 404: 订单不存在
- HTTP 500: 更新失败

---

#### 1.2.2 Query Order Position Metadata - 查询订单持仓元数据

**接口路径**: `POST /rpc/v1/order/position-metadata`

**说明**: 由 Position Monitor Service 调用，用于获取订单的持仓配置信息

**请求参数**:
```json
{
  "user_order_id": 123            // 必填: 用户订单ID
}
```

**响应示例**:
```json
{
  "user_order_id": 123,
  "user_strategy_id": 100,
  "leverage": 10,
  "fallback_price": 50000.0
}
```

**字段说明**:
- `user_order_id`: 用户订单ID
- `user_strategy_id`: 用户策略ID
- `leverage`: 杠杆倍数
- `fallback_price`: 备用价格（订单触发价格）

**错误响应**:
- HTTP 400: 参数错误
- HTTP 404: 订单不存在

---

#### 1.2.3 Reload Filters - 重新加载交易所过滤器

**接口路径**: `POST /rpc/v1/filters/reload`

**说明**: 由 Position Monitor Service 调用，用于通知 UOS 重新加载交易所过滤器数据

**请求参数**: 无

**响应示例**:
```json
{
  "status": "ok"
}
```

**说明**:
- PMS 在完成 Filter Sync 后调用此接口
- UOS 从 CSV 重新加载 `exchange_symbol_filters` 到内存
- 确保内存中的交易规则数据与 CSV 文件同步

**错误响应**:
- HTTP 405: 方法不允许（非 POST 请求）
- HTTP 500: 重载失败

---

## 二、Position Monitor Service

**默认端口**: 8080 (可通过环境变量 `POSITION_MONITOR_PORT` 设置)

### 2.1 REST API 接口

#### 2.1.1 Register Rule - 注册风控规则

**接口路径**: `POST /api/rules` 或 `POST /api/v1/rules`

**说明**:
- 创建规则前会验证 `user_strategy_id` 是否存在
- 验证该策略是否有活跃仓位（所有规则都是平仓规则，必须有仓位）
- 如果策略已有相同条件的活跃规则，则更新该规则（Upsert 逻辑）
- 如果规则正在使用中（`in_use` 状态），则拒绝更新

**请求参数**:
```json
{
  "user_strategy_id": 100,        // 必填: 用户策略ID（必须存在且有活跃仓位）
  "condition_name": "roi",        // 必填: 条件名称
  "operator": ">=",               // 必填: 操作符
  "value": 0.15,                  // 可选: 触发阈值 (holding_time可省略，使用默认值)
  "action": "reduce",             // 可选: 动作类型，默认 "reduce"
  "quantity_pct": 1.0,            // 可选: 平仓比例，默认 1.0 (全部平仓)
  "sort": 1,                      // 可选: 优先级，默认 1
  "fallback_rule": {              // 可选: 回落止盈规则配置
    "value": 0.03                 // 回落百分比（默认取 config.yaml 的 profit_drawdown_pct）
  }
}
```

> Upsert 匹配键为 `user_strategy_id` + `condition_name` + `operator`。

**Condition Name 说明**:
- `roi`: ROI收益率
- `price_btc`: BTC价格
- `price_sol`: SOL价格
- `holding_time`: 持仓时长（秒）
- `profit_trigger`: 盈利触发点
- `profit_drawdown_pct`: 盈利回落百分比
- `position_<symbol>`: 跨仓位监控（`<symbol>` 为代币或期权合约名）
  - **现货/合约格式**: `position_BTCUSDT`, `position_ETHUSDC`
  - **期权格式**: `position_BTC-7AUG26-64000-P`, `position_ETH-25DEC26-2400-C`
    - 格式: `UNDERLYING-DDMMMYY-STRIKE-TYPE`
    - UNDERLYING: BTC, ETH, SOL 等
    - DDMMMYY: 到期日（如 7AUG26, 25DEC26）
    - STRIKE: 行权价
    - TYPE: C=Call, P=Put
  - **用法**: `operator="=", value=0` 表示当该 symbol 无活跃仓位但有已平仓记录时触发
  - **创建验证**: 创建时该 user_strategy 对应的 user_id 必须存在该 symbol 的活跃仓位

**fallback_rule 行为说明**:
- 仅在 `condition_name="roi"` 且 `operator=">="` 时有效（止盈规则）
- 创建规则时：自动创建回落规则，主规则 `action` 指向回落规则 ID
- 更新规则时：
  - 规则无回落规则 → 创建新的回落规则
  - 规则已有回落规则 → 更新回落规则的 `value`
- 回落规则自动继承主规则的 `quantity_pct`

响应中会附带 `fallback_rule` 对象：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 5,
    "user_strategy_id": 100,
    "condition_name": "roi",
    "operator": ">=",
    "value": 0.15,
    "action": "6",
    "quantity_pct": 1.0,
    "sort": 1,
    "status": "active",
    "fallback_rule": {
      "id": 6,
      "user_strategy_id": 100,
      "condition_name": "profit_drawdown_pct",
      "operator": ">=",
      "value": 0.03,
      "sort": 2,
      "status": "active"
    }
  }
}
```

**Operator 说明**:
- `<`: 小于
- `<=`: 小于等于
- `>`: 大于
- `>=`: 大于等于
- `==`: 等于
- `!=`: 不等于

**Action 说明**:
- `reduce`: 减仓平仓
- 数字字符串: 链式规则ID（激活指定的规则）

**响应示例（创建成功）**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 5
  }
}
```

**响应示例（更新成功）**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 5
  }
}
```

**字段说明**:
- `code`: 状态码（0=成功）
- `message`: 操作结果消息
- `data.id`: 规则ID

**错误响应**:

| 错误码 | HTTP状态 | 说明 |
|--------|---------|------|
| 1001 | 400 | 参数校验失败（缺少必填字段、JSON 格式错误、无效操作符等） |
| 1002 | 404 | 数据不存在 |
| 4003 | 400 | 规则正在使用中（无法更新 `in_use` 状态的规则） |
| **4004** | **400** | **策略不存在**（user_strategy_id 未找到） |
| **4005** | **400** | **无活跃仓位**（策略没有活跃仓位，平仓规则必须有仓位） |
| 5000 | 500 | 服务器内部错误（通用） |
| 5001 | 500 | 服务器内部错误 |

**错误响应示例**:
```json
{
  "code": 4004,
  "message": "user_strategy_id 99999 not found"
}
```

```json
{
  "code": 4005,
  "message": "no active position found for strategy 100"
}
```

---

#### 2.1.2 List User Positions - 获取用户持仓列表

**接口路径**: `GET /api/v1/user-positions`

**请求参数**:

| 参数 | 类型 | 必填 | 说明 | 示例 |
|------|------|------|------|------|
| user_id | int | 否 | 用户ID | 1 |
| user_name | string | 否 | 用户名 | alice |
| exchange | string | 否 | 交易所 | binance |
| strategy_name | string | 否 | 策略名称 | ICT_1H |
| page | int | 否 | 页码（默认1） | 1 |
| page_size | int | 否 | 每页数量（默认10） | 10 |
| created_from | string | 否 | 创建时间起始（RFC3339，含），按 `created_at` 过滤 | 2026-07-01T00:00:00Z |
| created_to | string | 否 | 创建时间截止（RFC3339，含），按 `created_at` 过滤 | 2026-07-10T23:59:59Z |
| close_from | string | 否 | 平仓时间起始（RFC3339，含），按 `close_time` 过滤；传入任一 close 参数时排除活跃仓位 | 2026-07-01T00:00:00Z |
| close_to | string | 否 | 平仓时间截止（RFC3339，含），按 `close_time` 过滤；传入任一 close 参数时排除活跃仓位 | 2026-07-10T23:59:59Z |

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "id": 2335,
      "user_id": 1,
      "user_strategy_id": 2256,
      "exchange": "binance",
      "pos_type": 2,
      "current_price": 79.42,
      "quantity": 10.5,
      "latest_market_capitalization": 833.91,
      "roi": -0.0003,
      "pnl": -0.025,
      "win_rate": 0.65,
      "maximum_drawdown": -0.15,
      "total_margin": 299.26,
      "max_profit_percentage": 0.25,
      "max_loss_percentage": -0.10,
      "open_trades": 5,
      "closed_trades": 10,
      "profit_trades": 7,
      "loss_trades": 3,
      "deleted": false,
      "created_at": "2026-07-10T10:00:00Z",
      "updated_at": "2026-07-10T10:30:00Z"
    }
  ],
  "meta": {
    "page": 1,
    "page_size": 10,
    "total": 25
  }
}
```

**示例请求**:
```bash
# 查询所有持仓（分页）
curl "http://localhost:8080/api/v1/user-positions?page=1&page_size=10"

# 按user_id查询
curl "http://localhost:8080/api/v1/user-positions?user_id=1"

# 按时间范围查询
curl "http://localhost:8080/api/v1/user-positions?user_id=1&created_from=2026-07-01T00:00:00Z&created_to=2026-07-10T23:59:59Z"

# 按平仓时间范围查询（仅返回已平仓仓位，活跃仓位被排除）
curl "http://localhost:8080/api/v1/user-positions?user_id=1&close_from=2026-07-01T00:00:00Z&close_to=2026-07-10T23:59:59Z"

# 组合查询
curl "http://localhost:8080/api/v1/user-positions?user_name=alice&exchange=binance&strategy_name=ICT_1H"
```

---

#### 2.1.3 List Rules - 查询规则列表

**接口路径**: `GET /api/v1/rules`

**请求参数**:

| 参数 | 类型 | 必填 | 说明 | 示例 |
|------|------|------|------|------|
| user_strategy_id | int | 否 | 用户策略ID（快速路径，可单独使用） | 100 |
| user_id | int | 否 | 用户ID | 1 |
| user_name | string | 否 | 用户名 | alice |
| strategy_name | string | 否 | 策略名称（模糊匹配） | BTC |

**注意**: `user_id`、`user_name`、`user_strategy_id` 至少提供一个，否则返回 `1001`

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "id": 1,
      "user_strategy_id": 100,
      "condition_name": "roi",
      "operator": "<=",
      "value": -0.3,
      "sort": 1,
      "status": "active",
      "action": "reduce",
      "params": "{}"
    },
    {
      "id": 2,
      "user_strategy_id": 100,
      "condition_name": "price",
      "operator": ">=",
      "value": 50000.0,
      "sort": 2,
      "status": "active",
      "action": "close",
      "params": "{\"quantity_pct\": 0.5}"
    }
  ]
}
```

**示例请求**:
```bash
# 按user_id查询规则
curl "http://localhost:8080/api/v1/rules?user_id=1"

# 按user_name查询规则
curl "http://localhost:8080/api/v1/rules?user_name=alice"

# 按策略名称过滤
curl "http://localhost:8080/api/v1/rules?user_id=1&strategy_name=BTC"
```

---

### 2.2 RPC 接口

#### 2.2.1 Query User Order Positions - 查询用户订单持仓

**接口路径**: `POST /rpc/v1/user-order-positions/query`

**说明**: 由 User Order Service 调用，用于查询策略的持仓信息

**请求参数**:
```json
{
  "user_strategy_id": 100,        // 必填: 用户策略ID
  "side": 0,                      // 可选: 方向过滤 (0=做多, 1=做空)
  "active": true,                 // 可选: 是否活跃，默认 true
  "asset": "BTC",                 // 可选: 资产过滤
  "pos_type": 2,                  // 可选: 持仓类型过滤
  "include_positions": true       // 可选: 是否返回持仓详情，默认 false
}
```

**响应示例（不包含持仓详情）**:
```json
{
  "user_strategy_id": 100,
  "side": 0,
  "active": true,
  "asset": "BTC",
  "pos_type": 2,
  "count": 2
}
```

**响应示例（包含持仓详情）**:
```json
{
  "user_strategy_id": 100,
  "side": 0,
  "active": true,
  "asset": "BTC",
  "pos_type": 2,
  "count": 2,
  "positions": [
    {
      "id": 1,
      "user_id": 1,
      "user_order_id": 123,
      "user_strategy_id": 100,
      "exchange": "binance",
      "pos_type": 2,
      "asset": "BTC",
      "side": 0,                 // 0=做多, 1=做空
      "quantity": 0.1,
      "pos_price": 50000.0,
      "current_price": 51000.0,
      "leverage": 10,
      "deleted": 0
    },
    {
      "id": 2,
      "user_id": 1,
      "user_order_id": 124,
      "user_strategy_id": 100,
      "exchange": "binance",
      "pos_type": 2,
      "asset": "BTC",
      "side": 0,
      "quantity": 0.05,
      "pos_price": 49500.0,
      "current_price": 51000.0,
      "leverage": 10,
      "deleted": 0
    }
  ]
}
```

**字段说明**:
- `count`: 持仓数量
- `positions`: 持仓详情列表（仅当 `include_positions=true` 时返回）
  - `id`: 持仓ID
  - `user_id`: 用户ID
  - `user_order_id`: 用户订单ID
  - `user_strategy_id`: 用户策略ID
  - `exchange`: 交易所名称
  - `pos_type`: 持仓类型
  - `asset`: 基础资产符号
  - `side`: 方向 (0=做多, 1=做空)
  - `quantity`: 持仓数量
  - `pos_price`: 持仓均价
  - `current_price`: 当前价格
  - `leverage`: 杠杆倍数
  - `deleted`: 删除标记 (0=活跃, 1=已删除)

**错误响应**:
- HTTP 400: 参数错误
- HTTP 405: 方法不允许

---

#### 2.2.2 Invalidate Rules For Strategy - 失效策略的活跃规则

**接口路径**: `POST /rpc/v1/rules/invalidate-for-strategy`

**说明**: 由 User Order Service 调用，开仓时自动失效该策略的所有活跃规则

**触发时机**: UOS 接收到开仓信号（`open`、`buy`、`sell`）时自动调用

**业务逻辑**:
- 新开仓位时，旧规则对仓位状态已失效
- 避免旧规则错误触发平仓
- 失败不影响开仓流程（降级处理）

**请求参数**:
```json
{
  "user_strategy_id": 100        // 必填: 用户策略ID
}
```

**响应示例**:
```json
{
  "success": true,
  "invalidated_count": 2
}
```

**字段说明**:
- `success`: 操作是否成功
- `invalidated_count`: 实际失效的规则数量（仅统计 `active` 状态的规则）

**行为说明**:
- 仅失效 `status=active` 的规则
- 已是 `inactive` 或 `in_use` 状态的规则不受影响
- 操作为幂等性（多次调用结果一致）
- 线程安全（使用互斥锁保护）

**错误响应**:
- HTTP 400: 无效的 JSON 或缺少参数
- HTTP 500: 规则存储操作失败

**调用示例**:
```bash
# UOS 内部自动调用，无需手动触发
# 测试验证用：
curl -X POST http://localhost:8080/rpc/v1/rules/invalidate-for-strategy \
  -H "Content-Type: application/json" \
  -d '{"user_strategy_id": 100}'
```

---

## 三、数据类型定义

### 3.1 Side - 方向

| 值 | 说明 | 用途 |
|---|------|------|
| 0 | Long (做多) | 开多仓、持多仓 |
| 1 | Short (做空) | 开空仓、持空仓 |

**重要说明**:
- `side=0` 表示做多方向
- `side=1` 表示做空方向
- 平仓时使用相反方向：平多仓用 `side=1`，平空仓用 `side=0`

---

### 3.2 PosType - 持仓类型

| 值 | 说明 |
|---|------|
| 1 | Spot (现货) |
| 2 | Futures (合约) |

---

### 3.3 OrderType - 订单类型

| 值 | 说明 |
|---|------|
| 0 | LIMIT (限价单) |
| 1 | MARKET (市价单) |

---

### 3.4 Order Status - 订单状态

| 值 | 说明 |
|---|------|
| 1 | PENDING (待处理) |
| 2 | FILLED (已成交) |
| 3 | FAILED (失败) |

---

### 3.5 Exchange - 交易所

| 值 | 说明 |
|---|------|
| binance | Binance 交易所 |
| hyperliquid | Hyperliquid 交易所 |
| mock | Mock 交易所（测试用） |

---

### 3.6 Risk Strategy Type - 风控策略类型

| 值 | 说明 |
|---|------|
| traditional | 传统风控策略 |
| cta_intraday | CTA日内策略 |
| signal_close | 信号平仓策略（由信号驱动平仓，不生成默认风控规则） |

> 注: 默认风控规则的生成有两个例外 —— `exchange=deribit` 的仓位，以及对应
> `user_strategy.risk_strategy_type=signal_close` 的仓位，均不生成默认规则。
> 若仓位对应的 user_strategy 记录查询不到，仍按默认规则生成。

---

## 四、服务架构说明

### 4.1 服务职责

**User Order Service**:
- 接收和处理交易信号
- 创建和管理用户订单
- 与交易所交互执行订单
- 提供 REST API 给外部系统
- 提供 RPC 接口给 Position Monitor Service

**Position Monitor Service**:
- 监控用户持仓状态
- 定期聚合持仓信息
- 执行风控规则检查
- 触发减仓平仓动作
- 提供 REST API 用于规则注册
- 提供 RPC 接口给 User Order Service

### 4.2 服务通信

两个服务通过 RPC 接口相互通信：

- User Order Service 调用 Position Monitor Service:
  - `/rpc/v1/user-order-positions/query` - 查询持仓信息

- Position Monitor Service 调用 User Order Service:
  - `/rpc/v1/order/status/update` - 更新订单状态
  - `/rpc/v1/order/position-metadata` - 查询订单元数据
  - `/rpc/v1/filters/reload` - 通知重新加载交易所过滤器

### 4.3 数据流向

```
外部信号 → User Order Service → 创建订单 → 交易所执行
                                          ↓
                                Position Monitor Service ← 定期同步
                                          ↓
                                   聚合持仓 + 风控检查
                                          ↓
                                   触发减仓动作 → User Order Service → 执行平仓
```

### 4.4 Filter Sync 流程

```
Position Monitor Service 启动
        ↓
扫描 users.csv 获取交易所列表
        ↓
并发调用交易所 API (Binance/Hyperliquid)
        ↓
更新 exchange_symbol_filters.csv
        ↓
调用 UOS RPC: /rpc/v1/filters/reload
        ↓
UOS 重新加载 CSV 到内存
```

**说明**:
- Filter Sync 是后台定时任务，默认每 10 天执行一次
- 通过环境变量 `FILTER_SYNC_INTERVAL` 可配置同步间隔
- 确保交易规则（tick_size、step_size 等）保持最新

---

## 五、环境变量配置

### 5.1 User Order Service

| 环境变量 | 说明 | 默认值 |
|---------|------|--------|
| BINANCE_TESTNET | 是否使用Binance测试网 | false |
| HYPERLIQUID_TESTNET | 是否使用Hyperliquid测试网 | false |
| DERIBIT_TESTNET | 是否使用Deribit测试网 | false |
| HYPERLIQUID_PRIVATE_KEY | Hyperliquid私钥 | (从CSV读取) |
| POSITION_MONITOR_URL | Position Monitor服务地址 | http://localhost:8080 |

### 5.2 Position Monitor Service

| 环境变量 | 说明 | 默认值 |
|---------|------|--------|
| POSITION_MONITOR_PORT | 服务监听端口 | 8080 |
| POSITION_MONITOR_ORDER_SERVICE_URL | User Order Service地址 | http://localhost:8081 |
| DATA_DIR | 数据存储目录 | data |

---

## 六、错误处理

### 6.1 HTTP 状态码

| 状态码 | 说明 |
|--------|------|
| 200 | 成功 |
| 400 | 请求参数错误 |
| 404 | 资源不存在 |
| 405 | 方法不允许 |
| 500 | 服务器内部错误 |

### 6.2 错误响应格式

```json
{
  "error": "具体错误信息"
}
```

---

## 七、调用示例

### 7.1 发送交易信号

```bash
curl -X POST http://localhost:8081/api/v1/signals \
  -H "Content-Type: application/json" \
  -d '{
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
  }'
```

### 7.2 注册风控规则

```bash
curl -X POST http://localhost:8080/api/rules \
  -H "Content-Type: application/json" \
  -d '{
    "user_strategy_id": 100,
    "condition_name": "roi",
    "operator": ">=",
    "value": 0.15,
    "quantity_pct": 1.0
  }'
```

### 7.3 查询持仓信息

```bash
curl -X POST http://localhost:8080/rpc/v1/user-order-positions/query \
  -H "Content-Type: application/json" \
  -d '{
    "user_strategy_id": 100,
    "include_positions": true
  }'
```

---

#### 1.2.4 Get or Create Strategy - 获取或创建策略

**接口路径**: `POST /rpc/v1/strategy/get-or-create`

**说明**: 由 Position Monitor Service 调用，用于获取或创建策略记录

**请求参数**:
```json
{
  "name": "SYNC_BTC-25DEC26-64000-P",  // 必填: 策略名称
  "strategy_type": "MANUAL_SYNC"        // 必填: 策略类型
}
```

**响应示例**:
```json
{
  "strategy_id": 5157,
  "name": "SYNC_BTC-25DEC26-64000-P",
  "strategy_type": "MANUAL_SYNC",
  "created": true  // true=新创建, false=已存在
}
```

**说明**:
- PMS 在交易所仓位同步时调用
- 如果策略已存在，返回现有记录
- 如果不存在，创建新策略并返回
- 使用互斥锁保证并发安全

---

#### 1.2.5 Get or Create Strategy Asset - 获取或创建策略资产

**接口路径**: `POST /rpc/v1/strategy-asset/get-or-create`

**说明**: 由 Position Monitor Service 调用，用于获取或创建策略资产记录

**请求参数**:
```json
{
  "name": "SYNC_BTC-25DEC26-64000-P",  // 必填: 策略资产名称
  "asset": "BTC-25DEC26-64000-P",      // 必填: 资产名称
  "strategy_id": 5157,                 // 必填: 策略ID
  "pos_type": 3,                       // 必填: 仓位类型 (3=期权)
  "sort": 1                            // 必填: 排序号
}
```

**响应示例**:
```json
{
  "strategy_asset_id": 1203,
  "created": true
}
```

---

#### 1.2.6 Get or Create User Strategy - 获取或创建用户策略

**接口路径**: `POST /rpc/v1/user-strategy/get-or-create`

**说明**: 由 Position Monitor Service 调用，用于获取或创建用户策略记录

**请求参数**:
```json
{
  "user_id": 1,                          // 必填: 用户ID
  "name": "SYNC_BTC-25DEC26-64000-P",    // 必填: 策略名称
  "strategy_id": 5157,                   // 必填: 策略ID
  "exchange": "deribit",                 // 必填: 交易所
  "valid_before": "2030-12-31T08:00:00Z", // 必填: 有效期
  "cash": 1000.0,                        // 必填: 初始资金
  "parts": 3,                            // 必填: 分区数
  "status": 1,                           // 必填: 状态
  "risk_strategy_type": "traditional",   // 必填: 风控策略类型
  "orders_num": 0                        // 可选: 订单数
}
```

**响应示例**:
```json
{
  "user_strategy_id": 5142,
  "created": true
}
```

**说明**:
- 这三个接口用于统一策略管理
- 避免 PMS 和 UOS 并发写入 CSV 导致的数据冲突
- 所有策略创建必须通过 UOS RPC 接口
- 使用双重检查锁定保证并发安全

---

**文档版本**: v1.3
**更新时间**: 2026-07-29
**维护团队**: Trading Service Team

**最近更新**:
- 2026-07-29: 新增策略管理 RPC 端点 (1.2.4-1.2.6)，统一策略管理到 UOS 服务
- 2026-07-18: 新增 RPC 端点 `/rpc/v1/rules/invalidate-for-strategy`（开仓时自动失效规则）
- 2026-07-17: 新增规则创建前的策略和仓位验证，错误码 4004/4005
- 2026-07-01: 初始版本
