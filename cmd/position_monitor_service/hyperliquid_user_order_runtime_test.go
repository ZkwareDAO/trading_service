package main

import (
	"context"
	"fmt"
	"testing"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"

	"github.com/sonirico/go-hyperliquid"
)

func TestHyperliquidOrderUpdate_DirectlyCallsOrderExecutor(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Create UprunningOrder — let persistence assign the ID
	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          100,
		RelationType:        "user_orders",
		Exchange:            "hyperliquid",
		Symbol:              "BTCUSDC",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     999,
		ExchangeOrderStatus: "NEW",
		Side:                order.SideLong,
	}
	localID := repo.CreateUprunningOrder(uo)

	exec := exchange.NewOrderExecutor(repo, &fakeExchange{})
	exec.SetRPCClient(&hyperliquidTestRPCClient{metadata: &rpc.QueryOrderPositionMetadataResponse{UserStrategyID: 1, Leverage: 5, FallbackPrice: 50000}})
	runtime := &HyperliquidUserOrderRuntime{
		repo:     repo,
		executor: exec,
	}

	runtime.handleUpdates([]hyperliquid.WsOrder{{
		Order: hyperliquid.WsBasicOrder{
			Oid:     999,
			Coin:    "BTC",
			Side:    "B",
			LimitPx: "50000",
			Sz:      "0",
			OrigSz:  "0.1",
		},
		Status: "filled",
	}}, nil)

	updated, err := repo.GetUprunningOrderByID(localID)
	if err != nil {
		t.Fatalf("GetUprunningOrderByID: %v", err)
	}
	if updated.ExchangeOrderStatus != "FILLED" {
		t.Errorf("expected FILLED, got %s", updated.ExchangeOrderStatus)
	}

	positions := repo.ListActivePositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 position created, got %d", len(positions))
	}
	if positions[0].UserOrderID != 100 {
		t.Errorf("expected UserOrderID=100, got %d", positions[0].UserOrderID)
	}
	if positions[0].Asset != "BTCUSDC" {
		t.Errorf("expected Asset=BTCUSDC, got %s", positions[0].Asset)
	}
}

func TestHyperliquidOrderUpdate_PartialFillUsesExecutedQuantity(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          100,
		RelationType:        order.RelationTypeUserOrders,
		Exchange:            "hyperliquid",
		Symbol:              "NEARUSDC",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     55719287630,
		ExchangeOrderStatus: "NEW",
		ExchangeOrderPrice:  1.77077,
		ExchangeOrderQty:    0,
		Side:                order.SideShort,
	}
	localID := repo.CreateUprunningOrder(uo)

	exec := exchange.NewOrderExecutor(repo, &fakeExchange{})
	exec.SetRPCClient(&hyperliquidTestRPCClient{metadata: &rpc.QueryOrderPositionMetadataResponse{UserStrategyID: 1, Leverage: 6, FallbackPrice: 1.77077}})
	runtime := &HyperliquidUserOrderRuntime{
		repo:     repo,
		executor: exec,
	}

	runtime.handleUpdates([]hyperliquid.WsOrder{{
		Order: hyperliquid.WsBasicOrder{
			Oid:     55719287630,
			Coin:    "NEAR",
			Side:    "A",
			LimitPx: "1.77077",
			Sz:      "173.2",
			OrigSz:  "290.1",
		},
		Status: "filled",
	}}, nil)

	updated, err := repo.GetUprunningOrderByID(localID)
	if err != nil {
		t.Fatalf("GetUprunningOrderByID: %v", err)
	}
	if updated.ExchangeOrderStatus != "FILLED" {
		t.Errorf("expected FILLED, got %s", updated.ExchangeOrderStatus)
	}
	if countRowsForID(t, gs, "uprunning_orders.csv", localID, "FILLED") != 1 {
		t.Fatalf("expected exactly one FILLED CSV row for order %d", localID)
	}

	positions := repo.ListActivePositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 position created, got %d", len(positions))
	}
	if diff := positions[0].Quantity - 116.9; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("expected position quantity 116.9, got %.12f", positions[0].Quantity)
	}
}

func TestHyperliquidOrderUpdate_FilledUsesStoredAveragePrice(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          100,
		RelationType:        order.RelationTypeUserOrders,
		Exchange:            "hyperliquid",
		Symbol:              "NEARUSDC",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     55727791117,
		ExchangeOrderStatus: "NEW",
		ExchangeOrderPrice:  1.8505,
		ExchangeOrderQty:    75.4,
		Side:                order.SideLong,
	}
	localID := repo.CreateUprunningOrder(uo)

	exec := exchange.NewOrderExecutor(repo, &fakeExchange{})
	exec.SetRPCClient(&hyperliquidTestRPCClient{metadata: &rpc.QueryOrderPositionMetadataResponse{UserStrategyID: 1, Leverage: 6, FallbackPrice: 1.8505}})
	runtime := &HyperliquidUserOrderRuntime{
		repo:     repo,
		executor: exec,
	}

	runtime.handleUpdates([]hyperliquid.WsOrder{{
		Order: hyperliquid.WsBasicOrder{
			Oid:     55727791117,
			Coin:    "NEAR",
			Side:    "B",
			LimitPx: "3.6499",
			Sz:      "0",
			OrigSz:  "75.4",
		},
		Status: "filled",
	}}, nil)

	updated, err := repo.GetUprunningOrderByID(localID)
	if err != nil {
		t.Fatalf("GetUprunningOrderByID: %v", err)
	}
	if diff := updated.ExchangeOrderPrice - 1.8505; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("expected exchange_order_price 1.8505, got %.12f", updated.ExchangeOrderPrice)
	}

	positions := repo.ListActivePositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 position created, got %d", len(positions))
	}
	if diff := positions[0].PosPrice - 1.8505; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("expected pos_price 1.8505, got %.12f", positions[0].PosPrice)
	}
	if diff := positions[0].CurrentPrice - 1.8505; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("expected current_price 1.8505, got %.12f", positions[0].CurrentPrice)
	}
}

func TestHyperliquidOrderUpdate_RiskControlStrategy_ClosesPosition(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Create position first — persistence assigns ID
	pos := &order.UserOrderPosition{
		UserID:          1,
		UserOrderID:     100,
		RiskCtrlStratID: 200,
		Exchange:        "hyperliquid",
		PosType:         order.PosTypeFutures,
		Asset:           "ETHUSDC",
		Quantity:        1.0,
		PosPrice:        3000,
		Side:            order.SideLong,
		Deleted:         0,
	}
	posID := repo.CreateUserOrderPosition(pos)

	// Create user position
	userPos := &order.UserPosition{
		UserID:         1,
		UserStrategyID: 100,
		Exchange:       "hyperliquid",
		PosType:        order.PosTypeFutures,
		Quantity:       1.0,
		CurrentPrice:   3000,
		Deleted:        0,
	}
	userPosID := repo.CreateUserPosition(userPos)

	// Create UprunningOrder with relation_type = risk_control_strategy
	// Must use the actual posID and userPosID assigned by persistence
	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          300,
		RelationType:        "risk_control_strategy",
		Exchange:            "hyperliquid",
		Symbol:              "ETHUSDC",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     888,
		ExchangeOrderStatus: "NEW",
		Side:                order.SideShort,
		RiskCtrlStratID:     200,
		UserOrderPositionID: posID,
		UserPositionID:      userPosID,
	}
	repo.CreateUprunningOrder(uo)

	exec := exchange.NewOrderExecutor(repo, &fakeExchange{})
	runtime := &HyperliquidUserOrderRuntime{
		repo:     repo,
		executor: exec,
	}

	runtime.handleUpdates([]hyperliquid.WsOrder{{
		Order: hyperliquid.WsBasicOrder{
			Oid:     888,
			Coin:    "ETH",
			Side:    "A",
			LimitPx: "2900",
			Sz:      "0",
			OrigSz:  "1.0",
		},
		Status: "filled",
	}}, nil)

	positions := repo.ListActivePositions()
	if len(positions) != 0 {
		t.Errorf("expected all positions closed, got %d active", len(positions))
	}
}

func TestHyperliquidOrderUpdate_NonFilledStatusUpdate(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          200,
		RelationType:        "risk_control_strategy",
		Exchange:            "hyperliquid",
		Symbol:              "BTCUSDC",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     777,
		ExchangeOrderStatus: "NEW",
		Side:                order.SideShort,
		RiskCtrlStratID:     50,
	}
	localID := repo.CreateUprunningOrder(uo)

	exec := exchange.NewOrderExecutor(repo, &fakeExchange{})
	runtime := &HyperliquidUserOrderRuntime{executor: exec, repo: repo}

	runtime.handleUpdates([]hyperliquid.WsOrder{{
		Order: hyperliquid.WsBasicOrder{
			Oid:     777,
			Coin:    "BTC",
			Side:    "A",
			LimitPx: "51000",
			Sz:      "0.5",
			OrigSz:  "0.5",
		},
		Status: "canceled",
	}}, nil)

	updated, err := repo.GetUprunningOrderByID(localID)
	if err != nil {
		t.Fatalf("GetUprunningOrderByID: %v", err)
	}
	if updated.ExchangeOrderStatus != "CANCELED" {
		t.Errorf("expected CANCELED, got %s", updated.ExchangeOrderStatus)
	}
}

type hyperliquidTestRPCClient struct {
	metadata *rpc.QueryOrderPositionMetadataResponse
}

func (c *hyperliquidTestRPCClient) UpdateUserOrderStatusFILLED(ctx context.Context, orderID uint64) error {
	return nil
}

func (c *hyperliquidTestRPCClient) UpdateUserOrderStatusFailed(ctx context.Context, orderID uint64) error {
	return nil
}

func (c *hyperliquidTestRPCClient) QueryOrderPositionMetadata(ctx context.Context, orderID uint64) (*rpc.QueryOrderPositionMetadataResponse, error) {
	if c.metadata == nil {
		return &rpc.QueryOrderPositionMetadataResponse{UserOrderID: orderID, UserStrategyID: 1, Leverage: 5, FallbackPrice: 50000}, nil
	}
	cp := *c.metadata
	cp.UserOrderID = orderID
	return &cp, nil
}

type fakeExchange struct{}

func (f *fakeExchange) Name() string { return "fake" }
func (f *fakeExchange) CreateOrder(exchange.CreateOrderRequest) (*exchange.CreateOrderResponse, error) {
	return nil, nil
}
func (f *fakeExchange) CancelOrder(uint64) error                             { return nil }
func (f *fakeExchange) GetOrder(uint64, string) (*exchange.OrderInfo, error) { return nil, nil }
func (f *fakeExchange) SetLeverage(string, int) error                        { return nil }
func (f *fakeExchange) GetLeverage(string) (int, error)                      { return 1, nil }
func (f *fakeExchange) GetPrice(string) (float64, error)                     { return 0, nil }
func (f *fakeExchange) Connect() error                                       { return nil }
func (f *fakeExchange) Close() error                                         { return nil }
func (f *fakeExchange) SubscribeOrders(exchange.OrderCallback) error         { return nil }
func (f *fakeExchange) GetPositions() ([]exchange.PositionInfo, error)       { return nil, nil }

func countRowsForID(t *testing.T, gs *persistence.GlobalState, table string, id uint64, status string) int {
	t.Helper()
	gs.Shutdown()
	rows, err := gs.Persister().ReadAllCSV(table)
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	count := 0
	wantID := fmt.Sprintf("%d", id)
	for _, row := range rows {
		if row["id"] == wantID && row["exchange_order_status"] == status {
			count++
		}
	}
	return count
}

func TestHyperliquidUserOrderRuntime_FactoryCreatesRuntime(t *testing.T) {
	factory := NewHyperliquidUserOrderRuntimeFactory(nil, "", false)
	runtime, err := factory.NewUserOrderRuntime(&order.User{
		ID:        1,
		Exchange:  "hyperliquid",
		APIKey:    "0xabc",
		APISecret: "privkey",
	})
	if err != nil {
		t.Fatalf("NewUserOrderRuntime: %v", err)
	}
	_, ok := runtime.(*HyperliquidUserOrderRuntime)
	if !ok {
		t.Fatalf("expected *HyperliquidUserOrderRuntime, got %T", runtime)
	}
}

func TestHyperliquidOrderStatus_Normalization(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"filled", "FILLED"},
		{"FILLED", "FILLED"},
		{"canceled", "CANCELED"},
		{"CANCELED", "CANCELED"},
		{"cancelled", "CANCELED"},
		{"open", "NEW"},
		{"OPEN", "NEW"},
		{"PARTIAL", "PARTIAL"},
	}
	for _, tc := range tests {
		got := normalizeHyperliquidOrderStatus(tc.input)
		if got != tc.expected {
			t.Errorf("normalizeHyperliquidOrderStatus(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestHyperliquidPositionSide_Normalization(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"B", "LONG"},
		{"BUY", "LONG"},
		{"buy", "LONG"},
		{"A", "SHORT"},
		{"SELL", "SHORT"},
	}
	for _, tc := range tests {
		got := hyperliquidPositionSide(tc.input)
		if got != tc.expected {
			t.Errorf("hyperliquidPositionSide(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestHyperliquidPriceSymbol_Normalization(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"BTC", "BTCUSDC"},
		{"ETH", "ETHUSDC"},
		{"ETHUSDC", "ETHUSDC"},
		{"btc", "BTCUSDC"},
		{" BTC ", "BTCUSDC"},
	}
	for _, tc := range tests {
		got := normalizeHyperliquidPriceSymbol(tc.input)
		if got != tc.expected {
			t.Errorf("normalizeHyperliquidPriceSymbol(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestHyperliquidUserOrderRuntime_ImplementsInterface(t *testing.T) {
	runtime := &HyperliquidUserOrderRuntime{stopCh: make(chan struct{})}
	var _ UserOrderRuntime = runtime
}

func TestHyperliquidUserOrderRuntime_StartWithoutWs(t *testing.T) {
	runtime := &HyperliquidUserOrderRuntime{stopCh: make(chan struct{})}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	runtime.Stop()
}

func TestHyperliquidUserOrderRuntime_StopIsIdempotent(t *testing.T) {
	runtime := &HyperliquidUserOrderRuntime{stopCh: make(chan struct{})}
	runtime.Stop()
	runtime.Stop()
}
