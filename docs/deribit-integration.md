# Deribit 期权交易所集成

## 概述

本实现提供 Deribit 期权交易所的完整适配器，遵循 `exchange.Exchange` 接口。

## 核心特性

### 1. 符号格式（关键差异）

**Deribit 期权符号使用原生格式，不做任何转换：**

```
BTC-17JUL26-64000-P   # BTC 看跌期权
BTC-17JUL26-65000-C   # BTC 看涨期权
ETH-19JUL26-3000-C    # ETH 看涨期权
ETH-19JUL26-2800-P    # ETH 看跌期权
```

格式解析：`{基础资产}-{到期日}-{行权价}-{类型}`
- 到期日：`17JUL26` = 2026年7月17日
- 类型：`P` = Put (看跌), `C` = Call (看涨)

**⚠️ 与 Binance/Hyperliquid 的关键差异：**
- Binance: 需要 `BTCUSDT` → `BTC-PERPETUAL` 转换
- Hyperliquid: 需要 `BTC` → `@BTC` 转换
- **Deribit: 直接使用原生格式，无任何转换**

### 2. Market 订单价格计算

**开仓 vs 平仓不同策略：**

Market 订单在 Deribit 上会被自动转换为 Limit 订单，并根据操作类型计算最优价格：

#### 平仓 (reduceOnly=true)
- **平多 (SELL)**：直接使用 `best_bid` 价格，不截断
- **平空 (BUY)**：直接使用 `best_ask` 价格，不截断

#### 开仓 (reduceOnly=false)
- 实时获取 `tick_size`：调用 `public/get_instrument` API 获取 `tick_size_steps`
- 根据价格选择对应的 tick_size
- **买入开仓**：`mark_price - 5 * tick_size`，向下取整
- **卖出开仓**：`mark_price + 5 * tick_size`，向上取整

**示例：**

```go
// 平仓示例
// 假设 bid = 0.116, ask = 0.120
// 平多 (SELL) → 价格 = 0.116 (直接使用bid)
// 平空 (BUY)  → 价格 = 0.120 (直接使用ask)

// 开仓示例
// 假设 mark_price = 0.118, tick_size = 0.0005
// 买入开仓 → 0.118 - 5*0.0005 = 0.1155, 截断后 = 0.1155
// 卖出开仓 → 0.118 + 5*0.0005 = 0.1205, 截断后 = 0.1205
```

**为什么这样做？**
- 买入时使用更低价格，获得更优成本
- 卖出时使用更高价格，获得更优收益
- 自动对齐到 tick_size 整数倍，避免精度错误

**错误处理：**

如果价格计算失败（如 API 调用失败、交易对不存在等），`CreateOrder` 会返回明确的错误：

```go
if err != nil {
    return nil, fmt.Errorf("calculate market order price for %s: %w", symbol, err)
}
```

这确保了不会使用无效价格（如 0）发送订单到交易所。

### 3. 平仓价差检查（新增）

**问题背景：**

Deribit 期权市场流动性较低，买一卖一价差可能悬殊。直接市价平仓可能成交在极差的价格。

**解决方案：**

在平仓前检查 bid/ask 价差绝对值：

```go
spread = |ask - bid|
if spread > threshold:  // 默认 0.005
    // 发送企业微信通知
    // 保持规则 active 状态等待下次重试
    return SpreadTooWideError
else:
    // 价差正常，继续平仓
```

**配置项：**

```yaml
# config.yaml
deribit_spread_threshold: 0.005  # 可选，默认 0.005
```

**通知内容：**

```
需要手动平仓
- 合约代币：BTC-26SEP26-100000-C
- 策略名称：XXX策略
- 买一价：0.1135
- 卖一价：0.1475
- 价差：0.034
- 说明：买一卖一差距悬殊（0.034），需要手动操作
```

**关键设计：**
- 价差悬殊时返回 `SpreadTooWideError` 特殊错误
- 规则状态保持 `active`，下次风控周期会重新尝试
- 不影响其他仓位的风控执行
- 异步发送通知，不阻塞风控流程

### 4. 认证方式

Deribit 使用 OAuth2 `client_credentials` 流程：

```go
// 认证参数
client, err := deribit.NewDeribit(
    "your_api_key",     // client_id
    "your_api_secret",  // client_secret
    "your_api_pwd",     // API password (可选)
    false,              // testnet=false 使用主网
)
```

认证流程：
1. 调用 `public/auth` 端点
2. 获取 `access_token`
3. Token 有效期 1 小时（自动续期）

### 5. 下单参数

```go
// 开仓示例
resp, err := client.CreateOrder(exchange.CreateOrderRequest{
    Symbol:    "BTC-17JUL26-64000-P",  // 完整期权符号
    Side:      exchange.OrderSideBuy,  // 买入
    OrderType: exchange.OrderTypeLimit, // 限价单
    Quantity:  1.0,                     // 1 张合约
    Price:     0.005,                   // 期权价格 (BTC)
})

// 平仓示例 (reduce_only)
resp, err := client.CreateOrder(exchange.CreateOrderRequest{
    Symbol:     "BTC-17JUL26-64000-P",
    Side:       exchange.OrderSideSell,
    OrderType:  exchange.OrderTypeLimit,
    Quantity:   1.0,
    Price:      0.006,
    ReduceOnly: true,  // 关键：只减仓，不开新仓
})
```

**关键参数：**
- `Quantity`: 合约张数（不是 BTC 数量）
- `Price`: 期权权利金（以 BTC 计价）
- `ReduceOnly`: 平仓时必须设置为 `true`

### 4. Scanner 支持

Deribit 适配器完整支持 Scanner 扫描机制：

```go
// 查询订单状态
info, err := client.GetOrder(orderID, "BTC-17JUL26-64000-P")
```

**订单ID格式处理：**

Deribit 对不同币种可能返回不同格式的订单ID：
- BTC期权：纯数字格式 `107489620314`
- ETH期权：带前缀格式 `ETH-81179066009`

Scanner内部只存储数字部分，`GetOrder` 方法会自动处理：

```go
// GetOrder 内部逻辑：
// 1. 先用纯数字ID查询
// 2. 如果失败，根据symbol前缀拼接完整ID重试
//    例如：symbol="ETH-25SEP26-1900-P" → 拼接为 "ETH-81179066009"
// 3. 返回结果时提取数字部分，保持接口一致性
```

**订单状态映射：
// Deribit "open"      → OrderStatusNew
// Deribit "filled"    → OrderStatusFilled
// Deribit "cancelled" → OrderStatusCancelled
// Deribit "rejected"  → OrderStatusFailed
```

Scanner 工作流程：
1. 查询 `uprunning_orders` 中状态为 `NEW` 的订单
2. 调用 `GetOrder()` 查询 Deribit 订单状态
3. 根据状态变化创建仓位或更新状态

### 5. 仓位查询

```go
// 查询所有期权仓位
positions, err := client.GetPositions()

// 返回结果：
// - 只返回期权仓位 (kind="option")
// - 过滤掉永续期货仓位
// - 包含 BTC 和 ETH 期权
```

### 6. 价格查询

```go
// 查询期权标记价格
price, err := client.GetPrice("BTC-17JUL26-64000-P")
```

### 7. 精度校验

**Deribit 不需要预校验精度，API 会自动校验：**

```go
// 如果精度错误，Deribit 会返回清晰错误：
// {
//   "error": {
//     "code": 10001,
//     "message": "Invalid price: price must be multiple of tick_size (0.0001)"
//   }
// }
```

错误处理：
```go
if err != nil {
    if strings.Contains(err.Error(), "tick_size") {
        // 精度错误，调整价格后重试
    }
}
```

## 使用工厂注册

```go
import (
    "trading-service/internal/exchange"
    "trading-service/internal/exchange/deribit"
)

// 1. 创建工厂
factory := exchange.NewExchangeFactory()

// 2. 设置配置
factory.SetConfig("deribit", exchange.ExchangeConfig{
    APIKey:    "your_api_key",
    APISecret: "your_api_secret",
    APIPwd:    "your_api_pwd",
    Testnet:   false,
})

// 3. 创建实例
cfg := factory.GetConfig("deribit")
ex, err := deribit.NewDeribit(
    cfg.APIKey,
    cfg.APISecret,
    cfg.APIPwd,
    cfg.Testnet,
)
if err != nil {
    panic(err)
}

// 4. 注册到工厂
factory.Register("deribit", ex)

// 5. 使用工厂创建
ex, err = factory.Create("deribit")
```

## 完整示例

```go
package main

import (
    "fmt"
    
    "trading-service/internal/exchange"
    "trading-service/internal/exchange/deribit"
)

func main() {
    // 创建客户端
    client, err := deribit.NewDeribit(
        "your_api_key",
        "your_api_secret",
        "your_api_pwd",
        true, // 使用 testnet
    )
    if err != nil {
        panic(err)
    }
    
    // 认证
    if err := client.Connect(); err != nil {
        panic(err)
    }
    
    // 查询价格
    price, err := client.GetPrice("BTC-17JUL26-64000-P")
    if err != nil {
        panic(err)
    }
    fmt.Printf("Mark price: %.6f BTC\n", price)
    
    // 开仓买入看跌期权
    resp, err := client.CreateOrder(exchange.CreateOrderRequest{
        Symbol:    "BTC-17JUL26-64000-P",
        Side:      exchange.OrderSideBuy,
        OrderType: exchange.OrderTypeLimit,
        Quantity:  1.0,
        Price:     0.005,
    })
    if err != nil {
        panic(err)
    }
    fmt.Printf("Order created: ID=%d, Status=%s\n", resp.OrderID, resp.Status)
    
    // 查询订单状态
    info, err := client.GetOrder(resp.OrderID, resp.Symbol)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Order status: %s, Filled: %.2f\n", info.Status, info.Filled)
    
    // 查询仓位
    positions, err := client.GetPositions()
    if err != nil {
        panic(err)
    }
    for _, pos := range positions {
        fmt.Printf("Position: %s %s %.2f @ %.6f\n",
            pos.Symbol, pos.PositionSide, pos.Quantity, pos.EntryPrice)
    }
    
    // 清理
    client.Close()
}
```

## 测试覆盖率

```bash
$ go test ./internal/exchange/deribit/... -cover
ok      trading-service/internal/exchange/deribit    coverage: 81.0% of statements
```

## API 端点映射

| 功能 | Deribit 端点 | 实现方法 |
|------|--------------|----------|
| 认证 | `public/auth` | `Connect()` |
| 开仓 | `private/buy` | `CreateOrder()` |
| 平仓 | `private/sell` | `CreateOrder()` |
| 查询订单 | `private/get_order_state` | `GetOrder()` |
| 查询仓位 | `private/get_positions` | `GetPositions()` |
| 取消订单 | `private/cancel` | `CancelOrder()` |
| 查询价格 | `public/ticker` | `GetPrice()` |

## 注意事项

1. **WebSocket 支持**: ✅ 已支持实时订单推送（新增功能，见"WebSocket 订单监控"章节）
2. **无杠杆概念**：期权交易不使用杠杆，`SetLeverage()` 会返回错误
3. **数量单位**：`Quantity` 是合约张数，不是 BTC 数量
4. **价格单位**：`Price` 以 BTC 计价，不是 USDT
5. **测试网**：使用 `testnet=true` 连接 Deribit 测试网

## 相关文档

- [Deribit API 文档](https://docs.deribit.com/)

## 集成测试

### 前置要求

1. 获取 Deribit Testnet API 凭证：
   - 访问 https://test.deribit.com/
   - 注册账号并创建 API 密钥

2. 设置环境变量：
```bash
export DERIBIT_TESTNET_API_KEY="your_testnet_key"
export DERIBIT_TESTNET_API_SECRET="your_testnet_secret"
export DERIBIT_TESTNET_API_PWD="your_testnet_password"
```

### 运行测试

```bash
# 运行所有集成测试
go test -tags=integration ./internal/exchange/deribit/... -v

# 运行单个测试
go test -tags=integration ./internal/exchange/deribit/... -v -run TestIntegration_Authenticate

# 跳过集成测试（默认行为）
go test ./internal/exchange/deribit/... -v
```

### 测试列表

| 测试 | 功能 | 备注 |
|------|------|------|
| `TestIntegration_Authenticate` | OAuth2 认证 | 验证 Testnet 连接 |
| `TestIntegration_GetPrice` | 价格查询 | 测试公共 API |
| `TestIntegration_GetPositions` | 仓位查询 | 测试私有 API |
| `TestIntegration_CreateAndCancelOrder` | 订单生命周期 | ⚠️ 创建真实订单，默认跳过 |
| `TestIntegration_LeverageNotSupported` | 杠杆不支持 | 验证期权特性 |
| `TestIntegration_WebSocketNotImplemented` | WebSocket 未实现 | 验证错误处理 |
| `TestIntegration_ConcurrentOperations` | 并发安全 | 多线程测试 |
| `TestIntegration_ErrorHandling` | 错误处理 | 测试异常场景 |

## WebSocket 价格管理器

### 概述

Deribit WebSocket 价格管理器 (`DeribitWsPriceManager`) 提供实时期权价格订阅功能。

**文件位置**: `internal/exchange/ws/deribit_ws_price_manager.go`

**测试覆盖率**: 95.4% 平均 (超过 80% TDD 要求)

### 核心特性

1. **实时价格订阅**: 通过 WebSocket 订阅期权 ticker 更新
2. **自动重连**: 连接断开后可重新连接
3. **线程安全**: 使用 `sync.RWMutex` 和 `atomic` 确保并发安全
4. **符号格式**: 直接使用 Deribit 原生格式（如 `BTC-17JUL26-64000-P`）

### 使用示例

```go
package main

import (
    "fmt"
    "time"
    
    "trading-service/internal/exchange/ws"
)

func main() {
    // 创建 WebSocket 价格管理器
    pm := ws.NewDeribitWsPriceManager(
        ws.WithDeribitWsURLOption("wss://test.deribit.com/ws/api/v2"), // testnet
    )
    
    // 连接 WebSocket
    if err := pm.Connect(); err != nil {
        panic(err)
    }
    defer pm.Close()
    
    // 订阅期权价格
    if err := pm.Subscribe("BTC-17JUL26-64000-P"); err != nil {
        panic(err)
    }
    
    // 等待价格更新
    time.Sleep(2 * time.Second)
    
    // 获取最新价格
    price, ok := pm.GetPrice("BTC-17JUL26-64000-P")
    if ok {
        fmt.Printf("Mark price: %.6f BTC\n", price)
    }
    
    // 取消订阅
    if err := pm.Unsubscribe("BTC-17JUL26-64000-P"); err != nil {
        panic(err)
    }
}
```

### WebSocket API 细节

**端点**:
- 主网: `wss://www.deribit.com/ws/api/v2`
- 测试网: `wss://test.deribit.com/ws/api/v2`

**频道格式**: `ticker.{instrument_name}.100ms`

**订阅请求示例**:
```json
{
  "jsonrpc": "2.0",
  "method": "public/subscribe",
  "id": 1,
  "params": {
    "channels": ["ticker.BTC-17JUL26-64000-P.100ms"]
  }
}
```

**接收更新示例**:
```json
{
  "jsonrpc": "2.0",
  "params": {
    "channel": "ticker.BTC-17JUL26-64000-P.100ms",
    "data": {
      "instrument_name": "BTC-17JUL26-64000-P",
      "mark_price": 0.0052,
      "best_bid_price": 0.0051,
      "best_ask_price": 0.0053
    }
  }
}
```

### 测试

**测试文件**: `internal/exchange/ws/deribit_ws_price_manager_test.go`

**运行测试**:
```bash
# 运行所有 Deribit WebSocket 测试
go test ./internal/exchange/ws -run TestDeribitWsPriceManager -v

# 检查覆盖率
go test ./internal/exchange/ws -cover -run TestDeribitWsPriceManager
```

**测试覆盖率明细**:
- `Connect`: 84.6%
- `handleMessages`: 87.0%
- `handleTickerUpdate`: 100.0%
- `Subscribe`: 90.9%
- `Unsubscribe`: 81.8%
- `buildSubscribeRequest`: 100.0%
- 其他方法: 100.0%

**测试场景** (12 个测试用例):
1. 连接和订阅
2. 多订阅管理
3. 取消订阅
4. 并发访问安全
5. 重连机制
6. 错误处理
7. 边界情况测试

### Scanner 机制

**注意**: Deribit 订单状态监控通过 Scanner 机制实现，而非 WebSocket。

**原因**:
- Deribit 提供 `private/get_order_state` 接口
- Scanner 机制可靠性高，延迟可接受
- 降低维护成本

**订单状态映射**:
| Deribit 状态 | 系统状态 | Scanner 处理 |
|-------------|---------|-------------|
| `"open"` | `OrderStatusNew` | 扫描并查询 |
| `"filled"` | `OrderStatusFilled` | 创建仓位 |
| `"cancelled"` | `OrderStatusCancelled` | 更新状态 |
| `"rejected"` | `OrderStatusFailed` | 通知失败 |

### 代码简化

WebSocket 价格管理器最近进行了代码简化和重构:

1. **Guard Clauses**: 将深层嵌套的 if 语句改为清晰的 guard clauses
2. **DRY 原则**: 提取 `buildSubscribeRequest` 函数消除重复代码
3. **改进注释**: 使用更清晰的注释说明意图

简化后代码更易读、更易维护，同时保持了所有功能和测试覆盖率。

### 自动重连机制（重要更新）

**问题背景：**

WebSocket 连接可能因为网络问题断开，但之前的实现没有正确检测死连接，导致：
- `GetSubscriptions()` 返回缓存的订阅列表，认为连接正常
- `EnsureSubscribed()` 不触发重连
- 价格一直使用旧缓存值，不再更新

**修复方案：**

在 `handleMessages()` 中，当检测到 WebSocket 读取错误时，立即清空 `m.conn`：

```go
_, msg, err := conn.ReadMessage()
if err != nil {
    log.Printf("Deribit WS read error: %v", err)
    m.clearConnection()  // 清空连接状态
    return
}
```

这样当 `GetSubscriptions()` 被调用时，会检测到 `m.conn == nil` 并返回错误，触发 `EnsureSubscribed()` 执行重连。

**架构改进：**

提取了三个辅助函数提高代码可读性：
- `clearConnection()`: 清理死连接
- `processMessage()`: 处理单条 WebSocket 消息
- `subscriptionList()`: 构建订阅列表

**验证测试：**

新增测试 `TestDeribitWsPriceManager_GetSubscriptions_DetectsDeadConnection` 验证：
- 连接断开后，`GetSubscriptions()` 返回 "WebSocket not connected" 错误
- 确保重连逻辑能正确触发

## WebSocket 订单监控

### 概述

Deribit WebSocket 订单监控提供实时订单状态更新功能，替代 REST API Scanner 机制，实现零延迟订单状态同步。

**文件位置**: 
- `internal/exchange/ws/deribit_order_monitor.go` (347行)
- `cmd/position_monitor_service/deribit_user_order_runtime.go` (132行)

**测试覆盖率**: 10/10 测试通过 (100%)

### 核心特性

1. **实时订单更新**: 通过 WebSocket 接收订单状态变化
2. **JSON-RPC 2.0**: 使用 Deribit 标准 WebSocket 协议
3. **自动重连**: 连接断开后自动重新连接
4. **线程安全**: 使用 `sync.RWMutex` 和 `sync.Once` 确保并发安全
5. **订单查找重试**: 处理 WebSocket 消息早于数据库更新的情况

### 架构设计

```
DeribitUserOrderRuntimeFactory (创建用户实例)
         │
         ▼
DeribitUserOrderRuntime (管理连接生命周期)
         │
         ▼
DeribitOrderMonitor (WebSocket 监控核心)
         │
         ▼
OrderExecutor (处理订单状态变化)
```

### WebSocket 连接详情

**端点**:
- 主网: `wss://www.deribit.com/ws/api/v2`
- 测试网: `wss://test.deribit.com/ws/api/v2`

**订阅频道**:
- `user.orders.option.BTC.raw` - BTC 期权订单更新
- `user.orders.option.ETH.raw` - ETH 期权订单更新

**认证方式**: 使用 OAuth2 `access_token` 进行私有频道订阅

### 订单状态映射

| Deribit 状态 | 系统状态 | 处理方法 |
|-------------|---------|----------|
| `"open"` | `NEW` | `HandleOrderStatusUpdate()` |
| `"filled"` | `FILLED` | `HandleOrderFilled()` |
| `"cancelled"` | `CANCELLED` | `HandleOrderStatusUpdate()` |
| `"rejected"` | `FAILED` | `HandleOrderStatusUpdate()` |

### 订单ID格式处理

Deribit 订单ID格式：
- BTC期权: `BTC-123456` 或纯数字 `123456`
- ETH期权: `ETH-789012`

系统提取数字部分用于本地订单匹配。

### 订单查找重试机制

**问题**: WebSocket 消息可能在数据库更新之前到达

**解决方案**:
1. 先查询 executor 的 pending 缓存（内存）
2. 缓存未找到，查询数据库并重试（最多5次，间隔300ms）

这个机制确保即使在消息乱序的情况下，订单也能被正确处理。

### 仓位同步触发机制

**触发时机**:
1. **服务启动时**: 调用 `SyncDeribitPositions()` 执行一次全量同步
2. **WS订单未找到时**: 订单重试5次后仍无本地记录，触发 `SyncDeribitPositions()`

**并发保护**: `syncMu` 互斥锁防止多个WS事件同时触发同步，确保同一时刻只有一个同步在执行。

**重连安全**: `reconnect()` 监听 `stopCh`，`Stop()` 调用后立即取消重连，不会阻塞服务关闭。

### Testnet/Mainnet 切换

**配置方式**: 服务启动时通过配置文件指定

```yaml
# config.yaml
exchange:
  deribit_testnet: true  # 使用测试网
```

**注意**: 运行时无法切换，需要重启服务。

### 与 Scanner 的关系

**WebSocket 优先**: 系统现在通过 WebSocket 接收实时订单更新

**Scanner 作为补充**: Scanner 仍然运行，作为兜底机制：
- 定期扫描 NEW 状态订单
- 检查 WebSocket 可能遗漏的订单
- 确保 100% 可靠性

| 特性 | WebSocket | Scanner |
|------|-----------|---------|
| 延迟 | 毫秒级 | 30秒间隔 |
| 可靠性 | 高 | 极高（兜底） |
| 资源消耗 | 低 | 中等 |

### 测试

**测试文件**: 
- `internal/exchange/ws/deribit_order_monitor_test.go` (7个测试)
- `cmd/position_monitor_service/deribit_user_order_runtime_test.go` (3个测试)

**运行测试**:
```bash
go test ./internal/exchange/ws -run TestDeribitOrderMonitor -v
go test ./cmd/position_monitor_service -run TestDeribitUserOrderRuntimeFactory -v
```

### 相关文档

- [Deribit WebSocket API 文档](https://docs.deribit.com/#websocket-api)
- [WebSocket 价格管理器](#websocket-价格管理器)

### 注意事项

1. **订单测试**：`TestIntegration_CreateAndCancelOrder` 会创建真实订单，默认跳过
2. **符号更新**：测试中使用的期权符号可能需要根据 Testnet 可用合约更新
3. **Testnet 限制**：Testnet 可能与主网有差异，仅用于功能验证

## 仓位同步工具

### 概述

`cmd/sync_deribit_positions` 是一个独立工具，用于将 Deribit 交易所的现有仓位同步到本地系统。

**使用场景：**
- Deribit 已有仓位，但系统未监听到
- 需要为新仓位初始化 user_strategy、strategy、strategy_asset
- 仓位数据迁移或恢复

### 核心功能

1. **仓位对比** - 比较交易所仓位与本地仓位
   - 查询本地 active 仓位（`Deleted == 0`）
   - 按 `symbol+side` 聚合本地仓位数量
   - 使用浮点数容差比较（避免精度问题）
   - 返回数量不匹配的仓位列表

2. **自动初始化** - 为新仓位创建完整记录
   - 创建 Strategy（名称：`SYNC_{symbol}`）
   - 创建 StrategyAsset（关联期权符号）
   - 创建 UserStrategy（包含完整字段）
   - 创建 UserOrderPosition（包含成本价）

### 创建的记录详情

同步工具会创建以下记录：

**Strategy**:
- `Name`: `SYNC_{symbol}`（如 `SYNC_BTC-24JUL26-64000-P`）
- `StrategyType`: `MANUAL_SYNC`

**StrategyAsset**:
- `Asset`: 期权符号
- `StrategyID`: 关联的 Strategy ID
- `PosType`: `option`

**UserStrategy**:
- `ValidBefore`: `2030-12-31T08:00:00Z`
- `Cash`: `1000`
- `Parts`: `3`
- `Status`: `1`
- `RiskStrategyType`: `traditional`
- `OrdersNum`: `0`

**UserOrderPosition**:
- `Quantity`: 交易所仓位数量
- `PosPrice`: 交易所返回的 `EntryPrice`（成本价）
- `PosType`: `option`

### 使用方法

```bash
# 1. 设置环境变量
export DERIBIT_API_KEY="your_api_key"
export DERIBIT_API_SECRET="your_api_secret"
export DERIBIT_API_PWD="your_api_password"  # 可选

# 2. 运行同步工具（需要指定用户ID）
go run ./cmd/sync_deribit_positions <user_id>

# 示例
go run ./cmd/sync_deribit_positions 123
```

### 输出示例

```
正在连接 Deribit...
正在查询期权仓位...
找到 2 个期权仓位
需要同步 1 个仓位:
  - BTC-24JUL26-64000-P SHORT 0.1000
正在同步: BTC-24JUL26-64000-P SHORT
✅ 同步成功: BTC-24JUL26-64000-P
同步完成
```

### 聚合逻辑

本地系统一个订单对应一个 `user_order_position` 记录，但交易所只显示聚合后的总仓位。

**示例：**
- 本地有 3 个订单：0.1 + 0.2 + 0.1 = 0.4
- 交易所显示：0.4
- 数量匹配 → 无需同步

**数量不匹配时：**
- 本地有 0.3
- 交易所显示 0.5
- 创建新的 `SYNC_{symbol}` 策略记录 0.5 的仓位

### 注意事项

1. **幂等性** - 可以多次运行，不会重复创建已同步的仓位
2. **数据路径** - 默认使用 `./data/` 目录下的 CSV 文件
3. **测试网** - 默认连接 Deribit 测试网（testnet=true）
4. **清理资源** - 程序退出时自动调用 `gs.Shutdown()` 刷新 CSV 缓存

### 测试覆盖率

```bash
go test ./cmd/sync_deribit_positions/... -cover
ok      trading-service/cmd/sync_deribit_positions    coverage: 23.5% of statements
```

**测试用例：**
- ✅ 跳过已存在的仓位（symbol+side+quantity 匹配）
- ✅ 同步新仓位
- ✅ 正确聚合本地多个仓位
- ✅ 检测数量不匹配
- ✅ 忽略已删除仓位（`Deleted == 1`）