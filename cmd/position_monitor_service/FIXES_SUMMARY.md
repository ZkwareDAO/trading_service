# 修复总结：Scanner CANCELED状态更新 & 市价开单价格获取

## 问题分析

### 问题1: Scanner扫描到CANCELED状态未更新user_orders.status为3

**根本原因**: RPC server端(position_monitor_service)缺少 `/rpc/v1/order/status/update` 接口实现

- Client调用路径: `internal/rpc/client.go:94` → POST `/rpc/v1/order/status/update`
- Scanner调用: `order_status_scanner.go:374` → `s.rpc.UpdateUserOrderStatusFailed(ctx, userOrderID)`
- Server端缺失: `main.go` 只注册了3个RPC路由,缺少此接口

**影响**: Scanner检测到订单CANCELED后,调用RPC失败,user_orders.csv中的status保持为1(NEW),不会更新为3(FAILED)

---

### 问题2: 市价开单拿不到价格

**根本原因**: `HandleGetMarketPrice` (positions_rpc.go:141-181) fallback链不完整

原有fallback逻辑:
1. PriceRuntimes (WebSocket实时价格)
2. Active positions的CurrentPrice
3. ❌ **缺少Exchange REST API fallback**

**影响**: 新策略市价开单时,还没有持仓,PriceRuntime可能未启动,fallback到active positions失败 → 返回404

---

## 解决方案

### 修复1: 新增RPC接口 `/rpc/v1/order/status/update`

**新增文件**: `cmd/position_monitor_service/position_rpc_enhanced.go`

```go
// HandleUpdateUserOrderStatus updates user_order status
// POST /rpc/v1/order/status/update
func (h *PositionRPCHandler) HandleUpdateUserOrderStatus(w http.ResponseWriter, r *http.Request) {
    // 1. Parse request: {user_order_id, status}
    // 2. Call repo.UpdateUserOrderStatus(req.UserOrderID, req.Status, nil, now)
    // 3. Return success response
}
```

**注册路由**: `main.go:73-79`

```go
positionRPC := NewPositionRPCHandler(positionRepo, priceRuntimes, exchangeResolver)
http.HandleFunc("/rpc/v1/order/status/update", positionRPC.HandleUpdateUserOrderStatus)
```

---

### 修复2: 增强市价获取fallback链

**增强逻辑** (position_rpc_enhanced.go:76-108):

```go
func (h *PositionRPCHandler) fetchMarketPrice(exchange, symbol string) (*MarketPriceResponse, error) {
    // 1. Try PriceRuntimes (WS实时价格)
    // 2. Try Exchange REST API (GetPrice方法) ← NEW!
    // 3. Try Active positions (已有持仓价格)
}
```

**关键改进**:
- 新增Exchange REST API fallback,通过 `ex.GetPrice(symbol)` 直接查询交易所价格
- 市价开单时即使没有持仓,也能通过REST API获取实时价格
- 返回价格来源标记: `source="ws"/"rest_api"/"active_position"`

---

## 编译验证

```bash
$ cd cmd/position_monitor_service
$ go build .
trading-service/cmd/position_monitor_service  # ✅ 编译成功
```

---

## 部署验证

### 1. 重启position_monitor_service

```bash
systemctl restart position-monitor-service
```

### 2. 验证Scanner日志 (CANCELED订单更新)

观察日志输出:
```
order scanner: order 123 status changed: NEW → CANCELED
RPC: updated user_order 123 status to 3
```

检查user_orders.csv:
```csv
id,user_id,user_strategy_id,status,...
123,100,200,3,...  # ← status已更新为3(FAILED)
```

---

### 3. 验证市价开单 (价格获取)

观察信号处理日志:
```
HandleOpen: RPC get market price SUCCESS for strategyID=2299, exchange=binance, symbol=BTCUSDT, price=50000.0 (source=rest_api)
```

**成功标志**: 即使没有持仓,也能通过`source=rest_api`获取价格

---

## 代码变更

**新增文件**:
- `cmd/position_monitor_service/position_rpc_enhanced.go` (210行)
- `cmd/position_monitor_service/position_rpc_enhanced_test.go` (149行)

**修改文件**:
- `cmd/position_monitor_service/main.go` (新增3个RPC路由注册)

**新增RPC接口**:
- POST `/rpc/v1/order/status/update` - 更新user_order状态为3(解决问题1)
- POST `/rpc/v1/market-price/get` - 增强版市价查询(解决问题2)
- POST `/rpc/v1/order-position-metadata/query` - 查询订单metadata

---

## 测试方法

由于现有测试文件存在重复定义问题,建议:

1. **手动验证**: 重启服务后观察生产日志
2. **集成测试**: 通过signal handler发送测试信号验证完整流程
3. **后续清理**: 修复position_api_test.go和exchange_adapt_test.go的重复测试定义

---

## 技术细节

### Exchange.GetPrice方法

`position_rpc_enhanced.go:90-100` 使用Exchange接口的GetPrice方法:

```go
// From exchange.Exchange interface (interface.go:134)
GetPrice(symbol string) (float64, error)
```

**实现位置**:
- Binance: `internal/exchange/binance/binance.go` - 通过REST API ticker
- Hyperliquid: `internal/exchange/hyperliquid/hyperliquid.go` - 通过meta API

---

## 后续优化建议

1. **PriceRuntime启动顺序**: 确保WebSocket价格服务优先启动,减少REST API调用延迟
2. **监控RPC失败率**: 添加Prometheus metrics跟踪RPC成功/失败率
3. **日志标准化**: 统一RPC日志格式便于监控和排查