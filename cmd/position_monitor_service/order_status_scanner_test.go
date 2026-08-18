package main

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

// mockExchange implements exchange.Exchange for testing.
type mockExchange struct {
	orderInfos map[uint64]*exchange.OrderInfo
}

func (m *mockExchange) GetOrder(orderID uint64, symbol string) (*exchange.OrderInfo, error) {
	info, ok := m.orderInfos[orderID]
	if !ok {
		return nil, fmt.Errorf("order %d not found", orderID)
	}
	return info, nil
}

func (m *mockExchange) Name() string { return "mock" }
func (m *mockExchange) CreateOrder(exchange.CreateOrderRequest) (*exchange.CreateOrderResponse, error) {
	return nil, nil
}
func (m *mockExchange) CancelOrder(uint64) error                       { return nil }
func (m *mockExchange) SetLeverage(string, int) error                  { return nil }
func (m *mockExchange) GetLeverage(string) (int, error)                { return 1, nil }
func (m *mockExchange) GetPrice(string) (float64, error)               { return 0, nil }
func (m *mockExchange) Connect() error                                 { return nil }
func (m *mockExchange) Close() error                                   { return nil }
func (m *mockExchange) SubscribeOrders(exchange.OrderCallback) error   { return nil }
func (m *mockExchange) GetPositions() ([]exchange.PositionInfo, error) { return nil, nil }

// mockResolver implements UserExchangeResolver for testing.
type mockResolver struct {
	exchanges map[string]exchange.Exchange
}

func (r *mockResolver) ResolveExchange(userID uint64, name string) (exchange.Exchange, error) {
	key := fmt.Sprintf("%d:%s", userID, name)
	if ex, ok := r.exchanges[key]; ok {
		return ex, nil
	}
	return nil, fmt.Errorf("exchange not found: %s", key)
}

// mockRuleUpdater implements ruleStatusUpdater for testing.
type mockRuleUpdater struct {
	updatedRules map[int]string
}

func newMockRuleUpdater() *mockRuleUpdater {
	return &mockRuleUpdater{updatedRules: make(map[int]string)}
}

func (m *mockRuleUpdater) UpdateRuleStatus(id int, status string) error {
	m.updatedRules[id] = status
	return nil
}

func (m *mockRuleUpdater) ResetRulesForStrategy(strategyID uint64) error {
	return nil
}

// mockRPCClient tracks RPC calls for scanner fallback testing.
type mockRPCClient struct {
	callCount       int64
	failedCallCount int64
	orderID         uint64
	failedOrderID   uint64
	metadata        *rpc.QueryOrderPositionMetadataResponse
}

func (m *mockRPCClient) UpdateUserOrderStatusFILLED(ctx context.Context, orderID uint64) error {
	atomic.AddInt64(&m.callCount, 1)
	atomic.StoreUint64(&m.orderID, orderID)
	return nil
}

func (m *mockRPCClient) UpdateUserOrderStatusFailed(ctx context.Context, orderID uint64) error {
	atomic.AddInt64(&m.failedCallCount, 1)
	atomic.StoreUint64(&m.failedOrderID, orderID)
	return nil
}

func (m *mockRPCClient) QueryOrderPositionMetadata(ctx context.Context, orderID uint64) (*rpc.QueryOrderPositionMetadataResponse, error) {
	if m.metadata != nil {
		cp := *m.metadata
		cp.UserOrderID = orderID
		return &cp, nil
	}
	return &rpc.QueryOrderPositionMetadataResponse{UserOrderID: orderID, UserStrategyID: 9, Leverage: 5, FallbackPrice: 2.5}, nil
}

// createTestRepoWithFlush returns a repo plus a flush func that waits for pending
// async CSV writes. Needed because scan() calls ReloadUserStrategies(), which clears
// the in-memory map and reloads from CSV — any strategy whose async write has not
// landed yet would be lost, making the strategy name fall back to "strategy_<id>".
func createTestRepoWithFlush(t *testing.T) (*persistence.StateRepository, func()) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatalf("failed to create GlobalState: %v", err)
	}
	t.Cleanup(func() {
		gs.Shutdown() // wait for async writes
	})
	return persistence.NewStateRepository(gs), gs.Shutdown
}

func createTestRepo(t *testing.T) *persistence.StateRepository {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatalf("failed to create GlobalState: %v", err)
	}
	t.Cleanup(func() {
		gs.Shutdown() // wait for async writes
	})
	return persistence.NewStateRepository(gs)
}

// TestOrderStatusScanner_DetectsFilledOrder verifies scanner detects FILLED orders
// that WS missed.
func TestOrderStatusScanner_DetectsFilledOrder(t *testing.T) {
	repo := createTestRepo(t)

	orderID := repo.CreateUserOrder(&order.UserOrder{
		UserID:         1,
		UserStrategyID: 9,
		PosType:        order.PosTypeFutures,
		Exchange:       "binance",
		BaseAsset:      "NEAR",
		TriggerPrice:   2.5,
		Status:         1,
	})
	repo.UpsertLeverageConfig(&order.LeverageConfig{
		UserID:   1,
		Asset:    "NEAR",
		Quote:    "USDT",
		Leverage: 5,
		Exchange: "binance",
		Status:   1,
		PosType:  order.PosTypeFutures,
	})

	uo := &order.UprunningOrder{
		UserID:              1,
		Exchange:            "binance",
		Symbol:              "NEARUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     100,
		ExchangeOrderStatus: "NEW",
		RelationID:          orderID,
		RelationType:        order.RelationTypeUserOrders,
		Side:                order.SideLong,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	repo.CreateUprunningOrder(uo)
	time.Sleep(100 * time.Millisecond)

	mock := &mockExchange{
		orderInfos: map[uint64]*exchange.OrderInfo{
			100: {
				OrderID: 100,
				Symbol:  "NEARUSDT",
				Status:  exchange.OrderStatusFilled,
				Price:   2.5,
				Qty:     100,
				Filled:  100,
			},
		},
	}
	resolver := &mockResolver{
		exchanges: map[string]exchange.Exchange{
			"1:binance": mock,
		},
	}

	scanner := NewOrderStatusScanner(repo, resolver, nil, time.Hour, &mockRPCClient{}, nil)
	scanner.scan()

	updated, err := repo.FindUprunningOrderByExchangeID(100)
	if err != nil {
		t.Fatalf("expected order to be found: %v", err)
	}
	if !strings.EqualFold(updated.ExchangeOrderStatus, "filled") {
		t.Errorf("expected status FILLED, got %s", updated.ExchangeOrderStatus)
	}

	positions := repo.ListActivePositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	if positions[0].Asset != "NEARUSDT" {
		t.Errorf("expected position asset NEARUSDT, got %s", positions[0].Asset)
	}
	if positions[0].Quantity != 100 {
		t.Errorf("expected position quantity 100, got %f", positions[0].Quantity)
	}
	if positions[0].UserStrategyID != 9 {
		t.Errorf("expected position user_strategy_id 9, got %d", positions[0].UserStrategyID)
	}
	if positions[0].Leverage != 5 {
		t.Errorf("expected position leverage 5, got %d", positions[0].Leverage)
	}
	if positions[0].InitMargin != 250 {
		t.Errorf("expected position init_margin 250, got %f", positions[0].InitMargin)
	}
}

// TestOrderStatusScanner_NotifiesUserOrderServiceWhenFilled verifies the scanner
// acts as a fallback when WS RPC notification missed a FILLED user order.
func TestOrderStatusScanner_NotifiesUserOrderServiceWhenFilled(t *testing.T) {
	repo := createTestRepo(t)

	uo := &order.UprunningOrder{
		UserID:              1,
		Exchange:            "binance",
		Symbol:              "NEARUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     500,
		ExchangeOrderStatus: "NEW",
		RelationID:          50,
		RelationType:        order.RelationTypeUserOrders,
		Side:                order.SideLong,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	repo.CreateUprunningOrder(uo)
	time.Sleep(100 * time.Millisecond)

	mock := &mockExchange{
		orderInfos: map[uint64]*exchange.OrderInfo{
			500: {
				OrderID: 500,
				Symbol:  "NEARUSDT",
				Status:  exchange.OrderStatusFilled,
				Price:   2.5,
				Qty:     100,
				Filled:  100,
			},
		},
	}
	resolver := &mockResolver{
		exchanges: map[string]exchange.Exchange{
			"1:binance": mock,
		},
	}
	rpc := &mockRPCClient{}

	scanner := NewOrderStatusScanner(repo, resolver, nil, time.Hour, rpc, nil)
	scanner.scan()

	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt64(&rpc.callCount) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if count := atomic.LoadInt64(&rpc.callCount); count != 1 {
		t.Fatalf("expected 1 RPC call, got %d", count)
	}
	if orderID := atomic.LoadUint64(&rpc.orderID); orderID != 50 {
		t.Fatalf("expected RPC orderID 50, got %d", orderID)
	}
}

// TestOrderStatusScanner_HyperliquidPrefersExchangeFilledQty verifies scanner does not
// use stale exchange_order_quantity when Hyperliquid GetOrder reports actual fill.
func TestOrderStatusScanner_HyperliquidPrefersExchangeFilledQty(t *testing.T) {
	repo := createTestRepo(t)

	uo := &order.UprunningOrder{
		UserID:              1,
		Exchange:            "hyperliquid",
		Symbol:              "NEARUSDC",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     55719287630,
		ExchangeOrderStatus: "NEW",
		ExchangeOrderQty:    290.1,
		RelationID:          50,
		RelationType:        order.RelationTypeUserOrders,
		Side:                order.SideShort,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	repo.CreateUprunningOrder(uo)
	time.Sleep(100 * time.Millisecond)

	mock := &mockExchange{
		orderInfos: map[uint64]*exchange.OrderInfo{
			55719287630: {
				OrderID: 55719287630,
				Symbol:  "NEARUSDC",
				Status:  exchange.OrderStatusFilled,
				Price:   1.77077,
				Qty:     290.1,
				Filled:  116.9,
			},
		},
	}
	resolver := &mockResolver{
		exchanges: map[string]exchange.Exchange{
			"1:hyperliquid": mock,
		},
	}

	scanner := NewOrderStatusScanner(repo, resolver, nil, time.Hour, &mockRPCClient{}, nil)
	scanner.scan()

	updated, err := repo.FindUprunningOrderByExchangeID(55719287630)
	if err != nil {
		t.Fatalf("expected order to be found: %v", err)
	}
	if diff := updated.ExchangeOrderQty - 116.9; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("expected exchange_order_quantity 116.9, got %.12f", updated.ExchangeOrderQty)
	}

	positions := repo.ListActivePositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	if diff := positions[0].Quantity - 116.9; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("expected position quantity 116.9, got %.12f", positions[0].Quantity)
	}
}

func TestOrderStatusScanner_SendsOpenNotificationWhenFilled(t *testing.T) {
	repo, flush := createTestRepoWithFlush(t)
	now := time.Now()
	repo.CreateUser(&order.User{ID: 1, Name: "follow_prod", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateUserStrategy(&order.UserStrategy{UserID: 1, Name: "OBVATR_4H_2_NEARUSDT", Exchange: "binance", CreatedAt: now, UpdatedAt: now})

	uo := &order.UprunningOrder{
		UserID:              1,
		Exchange:            "binance",
		Symbol:              "NEARUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     700,
		ExchangeOrderStatus: "NEW",
		RelationID:          70,
		RelationType:        order.RelationTypeUserOrders,
		Side:                order.SideLong,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	repo.CreateUprunningOrder(uo)

	mock := &mockExchange{orderInfos: map[uint64]*exchange.OrderInfo{700: {OrderID: 700, Symbol: "NEARUSDT", Status: exchange.OrderStatusFilled, Price: 2.5, Qty: 100, Filled: 100}}}
	resolver := &mockResolver{exchanges: map[string]exchange.Exchange{"1:binance": mock}}
	notifier := &scannerRecordingNotifier{}
	rpc := &mockRPCClient{metadata: &rpc.QueryOrderPositionMetadataResponse{UserStrategyID: strategyID, Leverage: 5, FallbackPrice: 2.5}}
	scanner := NewOrderStatusScanner(repo, resolver, nil, time.Hour, rpc, notifier)
	flush() // ensure user/strategy rows are on disk before scan() reloads from CSV
	scanner.scan()

	if len(notifier.openMessages) != 1 {
		t.Fatalf("expected one open notification, got %d", len(notifier.openMessages))
	}
	msg := notifier.openMessages[0]
	if msg.UserName != "follow_prod" || msg.EventName != "FutureOrder" || msg.StrategyName != "OBVATR_4H_2_NEARUSDT" || msg.Price != 2.5 || msg.Quantity != 100 {
		t.Fatalf("unexpected open notification: %+v", msg)
	}
}

type scannerRecordingNotifier struct {
	openMessages  []*notification.OpenOrderMessage
	closeMessages []*notification.CloseOrderMessage
}

func (n *scannerRecordingNotifier) SendOpenOrder(msg *notification.OpenOrderMessage) error {
	n.openMessages = append(n.openMessages, msg)
	return nil
}
func (n *scannerRecordingNotifier) SendCloseOrder(msg *notification.CloseOrderMessage) error {
	n.closeMessages = append(n.closeMessages, msg)
	return nil
}
func (n *scannerRecordingNotifier) SendTest(*notification.TestMessage) error { return nil }
func (n *scannerRecordingNotifier) SendManualCloseNotification(*notification.ManualCloseMessage) error {
	return nil
}

func (n *scannerRecordingNotifier) SendDeribitPositionNotification(*notification.DeribitPositionMessage) error {
	return nil
}

func TestOrderStatusScanner_NoChange(t *testing.T) {
	repo := createTestRepo(t)

	uo := &order.UprunningOrder{
		UserID:              1,
		Exchange:            "binance",
		Symbol:              "NEARUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     200,
		ExchangeOrderStatus: "NEW",
		RelationID:          20,
		RelationType:        order.RelationTypeUserOrders,
		Side:                order.SideLong,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	repo.CreateUprunningOrder(uo)
	time.Sleep(100 * time.Millisecond)

	mock := &mockExchange{
		orderInfos: map[uint64]*exchange.OrderInfo{
			200: {
				OrderID: 200,
				Status:  exchange.OrderStatusNew,
			},
		},
	}
	resolver := &mockResolver{
		exchanges: map[string]exchange.Exchange{
			"1:binance": mock,
		},
	}

	scanner := NewOrderStatusScanner(repo, resolver, nil, time.Hour, nil, nil)
	scanner.scan()

	positions := repo.ListActivePositions()
	if len(positions) != 0 {
		t.Errorf("expected 0 positions, got %d", len(positions))
	}
}

// TestOrderStatusScanner_EmptyList verifies scanner handles no NEW orders.
func TestOrderStatusScanner_EmptyList(t *testing.T) {
	repo := createTestRepo(t)
	resolver := &mockResolver{exchanges: make(map[string]exchange.Exchange)}
	scanner := NewOrderStatusScanner(repo, resolver, nil, time.Hour, nil, nil)
	scanner.scan() // should not panic
}

// TestOrderStatusScanner_GetOrderFails verifies scanner retries on error.
func TestOrderStatusScanner_GetOrderFails(t *testing.T) {
	repo := createTestRepo(t)

	uo := &order.UprunningOrder{
		UserID:              1,
		Exchange:            "binance",
		Symbol:              "NEARUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     300,
		ExchangeOrderStatus: "NEW",
		RelationID:          30,
		RelationType:        order.RelationTypeUserOrders,
		Side:                order.SideLong,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	repo.CreateUprunningOrder(uo)
	time.Sleep(100 * time.Millisecond)

	mock := &mockExchange{orderInfos: map[uint64]*exchange.OrderInfo{}}
	resolver := &mockResolver{
		exchanges: map[string]exchange.Exchange{
			"1:binance": mock,
		},
	}

	scanner := NewOrderStatusScanner(repo, resolver, nil, time.Hour, nil, nil)
	scanner.scan()

	updated, _ := repo.FindUprunningOrderByExchangeID(300)
	if updated == nil || !strings.EqualFold(updated.ExchangeOrderStatus, "NEW") {
		t.Errorf("expected status NEW, got %v", updated)
	}
}

func TestOrderStatusScanner_SendsRiskCloseNotificationWhenFilled(t *testing.T) {
	repo, flush := createTestRepoWithFlush(t)
	now := time.Now()
	repo.CreateUser(&order.User{ID: 1, Name: "machineLightGbm", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateUserStrategy(&order.UserStrategy{UserID: 1, Name: "ICT_1D_3_XRPUSDT", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	posID := repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: 1, UserStrategyID: strategyID, Asset: "XRPUSDT", Exchange: "binance", PosType: order.PosTypeFutures, Side: order.SideShort, Quantity: 4776.4, Deleted: 0})
	userPositionID := repo.CreateUserPosition(&order.UserPosition{UserID: 1, UserStrategyID: strategyID, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 4776.4, PnL: -44.898180, ROI: -0.044899, Deleted: 0})

	uo := &order.UprunningOrder{UserID: 1, Exchange: "binance", Symbol: "XRPUSDT", PosType: order.PosTypeFutures, ExchangeOrderID: 800, ExchangeOrderStatus: "NEW", RelationID: 200, RelationType: order.RelationTypeRiskControlStrategy, RiskCtrlStratID: 200, UserOrderPositionID: posID, UserPositionID: userPositionID, Side: order.SideShort, CreatedAt: now, UpdatedAt: now}
	repo.CreateUprunningOrder(uo)

	mock := &mockExchange{orderInfos: map[uint64]*exchange.OrderInfo{800: {OrderID: 800, Symbol: "XRPUSDT", Status: exchange.OrderStatusFilled, Price: 1.1, Qty: 4776.4, Filled: 4776.4}}}
	resolver := &mockResolver{exchanges: map[string]exchange.Exchange{"1:binance": mock}}
	notifier := &scannerRecordingNotifier{}
	scanner := NewOrderStatusScanner(repo, resolver, nil, time.Hour, nil, notifier)
	flush() // ensure user/strategy rows are on disk before scan() reloads from CSV
	scanner.scan()

	if len(notifier.closeMessages) != 1 {
		t.Fatalf("expected one close notification, got %d", len(notifier.closeMessages))
	}
	msg := notifier.closeMessages[0]
	if msg.UserName != "machineLightGbm" || msg.EventName != "新风控下单" || msg.StrategyName != "ICT_1D_3_XRPUSDT" || msg.Price != 1.1 || msg.Quantity != 4776.4 || msg.Profit != -44.898180 || msg.ProfitPercentage != -0.044899 {
		t.Fatalf("unexpected close notification: %+v", msg)
	}
}

// TestOrderStatusScanner_CanceledOrder verifies scanner re-activates rules.
func TestOrderStatusScanner_CanceledOrder(t *testing.T) {
	repo := createTestRepo(t)

	uo := &order.UprunningOrder{
		UserID:              1,
		Exchange:            "binance",
		Symbol:              "NEARUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     400,
		ExchangeOrderStatus: "NEW",
		RelationID:          40,
		RelationType:        order.RelationTypeRiskControlStrategy,
		RiskCtrlStratID:     5,
		Side:                order.SideLong,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	repo.CreateUprunningOrder(uo)
	time.Sleep(100 * time.Millisecond)

	mock := &mockExchange{
		orderInfos: map[uint64]*exchange.OrderInfo{
			400: {
				OrderID: 400,
				Status:  exchange.OrderStatusCancelled,
			},
		},
	}
	resolver := &mockResolver{
		exchanges: map[string]exchange.Exchange{
			"1:binance": mock,
		},
	}
	ruleUpdater := newMockRuleUpdater()

	scanner := NewOrderStatusScanner(repo, resolver, ruleUpdater, time.Hour, nil, nil)
	scanner.scan()

	if status, ok := ruleUpdater.updatedRules[5]; !ok || status != "active" {
		t.Errorf("expected rule 5 to be active, got %s (ok=%v)", status, ok)
	}
}

// TestOrderStatusScanner_Start verifies scanner starts and stops with context.
func TestOrderStatusScanner_Start(t *testing.T) {
	repo := createTestRepo(t)
	resolver := &mockResolver{exchanges: make(map[string]exchange.Exchange)}

	ctx, cancel := context.WithCancel(context.Background())
	scanner := NewOrderStatusScanner(repo, resolver, nil, 100*time.Millisecond, nil, nil)
	scanner.Start(ctx)

	time.Sleep(200 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
}

// TestListUprunningOrdersByExchangeStatus verifies the repo method.
func TestListUprunningOrdersByExchangeStatus(t *testing.T) {
	repo := createTestRepo(t)

	uo1 := &order.UprunningOrder{
		UserID:              1,
		ExchangeOrderID:     1,
		ExchangeOrderStatus: "NEW",
	}
	uo2 := &order.UprunningOrder{
		UserID:              2,
		ExchangeOrderID:     2,
		ExchangeOrderStatus: "FILLED",
	}
	repo.CreateUprunningOrder(uo1)
	repo.CreateUprunningOrder(uo2)

	newOrders := repo.ListUprunningOrdersByExchangeStatus("NEW")
	if len(newOrders) != 1 {
		t.Errorf("expected 1 NEW order, got %d", len(newOrders))
	}

	filledOrders := repo.ListUprunningOrdersByExchangeStatus("FILLED")
	if len(filledOrders) != 1 {
		t.Errorf("expected 1 FILLED order, got %d", len(filledOrders))
	}

	emptyOrders := repo.ListUprunningOrdersByExchangeStatus("CANCELLED")
	if len(emptyOrders) != 0 {
		t.Errorf("expected 0 CANCELLED orders, got %d", len(emptyOrders))
	}
}

// TestOrderStatusScanner_NotifiesUserOrderServiceWhenCanceled verifies that
// when an order is CANCELED, the scanner calls RPC to update user_order.status=3
func TestOrderStatusScanner_NotifiesUserOrderServiceWhenCanceled(t *testing.T) {
	repo := createTestRepo(t)

	uo := &order.UprunningOrder{
		UserID:              1,
		Exchange:            "binance",
		Symbol:              "NEARUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     600,
		ExchangeOrderStatus: "NEW",
		RelationID:          60,
		RelationType:        order.RelationTypeUserOrders,
		Side:                order.SideLong,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	repo.CreateUprunningOrder(uo)
	time.Sleep(100 * time.Millisecond)

	mock := &mockExchange{
		orderInfos: map[uint64]*exchange.OrderInfo{
			600: {
				OrderID: 600,
				Symbol:  "NEARUSDT",
				Status:  exchange.OrderStatusCancelled,
				Price:   2.5,
				Qty:     100,
			},
		},
	}
	resolver := &mockResolver{
		exchanges: map[string]exchange.Exchange{
			"1:binance": mock,
		},
	}
	rpc := &mockRPCClient{}

	scanner := NewOrderStatusScanner(repo, resolver, nil, time.Hour, rpc, nil)
	scanner.scan()

	// Wait for async RPC call
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt64(&rpc.failedCallCount) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if count := atomic.LoadInt64(&rpc.failedCallCount); count != 1 {
		t.Fatalf("expected 1 RPC UpdateUserOrderStatusFailed call, got %d", count)
	}
	if orderID := atomic.LoadUint64(&rpc.failedOrderID); orderID != 60 {
		t.Fatalf("expected RPC orderID 60, got %d", orderID)
	}
}
