package exchange

import (
	"context"
	"sync"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

// mockRPCClient tracks RPC calls for testing using sync.WaitGroup
// for deterministic waits instead of time.Sleep.
type mockRPCClient struct {
	mu          sync.Mutex
	calls       []rpcCall
	statusErr   error
	metadataErr error
	metadata    *rpc.QueryOrderPositionMetadataResponse
}

type rpcCall struct {
	orderID uint64
	status  int
}

type fakeRuleStatusUpdater struct {
	updates   map[int]string
	resets    map[uint64]bool
}

func (u *fakeRuleStatusUpdater) UpdateRuleStatus(id int, status string) error {
	if u.updates == nil {
		u.updates = make(map[int]string)
	}
	u.updates[id] = status
	return nil
}

func (u *fakeRuleStatusUpdater) ResetRulesForStrategy(strategyID uint64) error {
	if u.resets == nil {
		u.resets = make(map[uint64]bool)
	}
	u.resets[strategyID] = true
	return nil
}

func (m *mockRPCClient) UpdateUserOrderStatusFILLED(ctx context.Context, orderID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, rpcCall{orderID: orderID, status: 2})
	return m.statusErr
}

func (m *mockRPCClient) UpdateUserOrderStatusFailed(ctx context.Context, orderID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, rpcCall{orderID: orderID, status: 3})
	return m.statusErr
}

func (m *mockRPCClient) QueryOrderPositionMetadata(ctx context.Context, orderID uint64) (*rpc.QueryOrderPositionMetadataResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.metadataErr != nil {
		return nil, m.metadataErr
	}
	if m.metadata != nil {
		cp := *m.metadata
		cp.UserOrderID = orderID
		return &cp, nil
	}
	return &rpc.QueryOrderPositionMetadataResponse{UserOrderID: orderID, UserStrategyID: 9, Leverage: 5, FallbackPrice: 50000}, nil
}

func (m *mockRPCClient) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockRPCClient) getCall(i int) (rpcCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i >= len(m.calls) {
		return rpcCall{}, false
	}
	return m.calls[i], true
}

// expectRPC increments the WaitGroup before the action, then waits for
// the async goroutine to call the mock. Returns when RPC is confirmed.
func (m *mockRPCClient) expectRPC() func() {
	return m.expectRPCN(1)
}

func (m *mockRPCClient) expectRPCN(n int) func() {
	return func() {
		for i := 0; i < 400; i++ {
			if m.getCallCount() >= n {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func setupExecutorWithRPC(t *testing.T) (*OrderExecutor, *mockRPCClient, *persistence.GlobalState, *persistence.StateRepository) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	mock := NewMockExchange()
	mock.SetPrice("BTCUSDT", 50000)
	rpc := &mockRPCClient{metadata: &rpc.QueryOrderPositionMetadataResponse{UserStrategyID: 9, Leverage: 10, FallbackPrice: 50000}}
	exec := NewOrderExecutor(repo, mock)
	exec.SetRPCClient(rpc)
	return exec, rpc, gs, repo
}

func makeUprunningOrder(userID, relationID uint64, relationType, symbol string, posType order.PosType, side order.Side) *order.UprunningOrder {
	return &order.UprunningOrder{
		UserID:       userID,
		RelationID:   relationID,
		RelationType: relationType,
		Symbol:       symbol,
		PosType:      posType,
		Exchange:     "mock",
		Side:         side,
	}
}

func TestHandleOrderFilled_RPC_FILLED_UserOrders(t *testing.T) {
	exec, rpc, gs, repo := setupExecutorWithRPC(t)
	defer gs.Shutdown()

	orderID := repo.CreateUserOrder(&order.UserOrder{
		UserID:         1,
		UserStrategyID: 9,
		PosType:        order.PosTypeFutures,
		Exchange:       "mock",
		BaseAsset:      "BTC",
		QuoteAsset:     "USDT",
		TriggerPrice:   50000,
		Status:         1,
	})
	repo.UpsertLeverageConfig(&order.LeverageConfig{
		UserID:   1,
		Asset:    "BTC",
		Quote:    "USDT",
		Leverage: 10,
		Exchange: "mock",
		Status:   1,
		PosType:  order.PosTypeFutures,
	})

	uo := makeUprunningOrder(1, orderID, order.RelationTypeUserOrders, "BTCUSDT", order.PosTypeFutures, order.SideLong)
	uoID := exec.CreateRunningOrder(uo)
	update := &OrderUpdate{
		OrderID: uoID, Symbol: "BTCUSDT", Status: "FILLED",
		AvgPrice: 0, ExecutedQty: 0.1, PositionSide: "LONG",
		UserID: 1, PosType: order.PosTypeFutures, RelationID: orderID,
	}

	done := rpc.expectRPC()
	if err := exec.HandleOrderFilled(update); err != nil {
		t.Fatalf("HandleOrderFilled: %v", err)
	}
	done() // deterministic wait (no time.Sleep)

	if rpc.getCallCount() != 1 {
		t.Fatalf("expected 1 RPC call, got %d", rpc.getCallCount())
	}
	call, _ := rpc.getCall(0)
	if call.orderID != orderID || call.status != 2 {
		t.Errorf("expected orderID=%d, status=2; got orderID=%d, status=%d", orderID, call.orderID, call.status)
	}

	positions := gs.ListUserOrderPositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	pos := positions[0]
	if pos.UserStrategyID != 9 {
		t.Fatalf("expected user_strategy_id=9, got %d", pos.UserStrategyID)
	}
	if pos.Leverage != 10 {
		t.Fatalf("expected leverage=10, got %d", pos.Leverage)
	}
	if pos.InitMargin != 5000 {
		t.Fatalf("expected init_margin=5000, got %f", pos.InitMargin)
	}
}

func TestHandleOrderFilled_UserOrders_IgnoresRiskFields(t *testing.T) {
	exec, rpc, gs, _ := setupExecutorWithRPC(t)
	defer gs.Shutdown()

	uo := makeUprunningOrder(1, 100, order.RelationTypeUserOrders, "BTCUSDT", order.PosTypeFutures, order.SideLong)
	uo.RiskCtrlStratID = 0
	uo.UserOrderPositionID = 0
	uo.UserPositionID = 0
	uoID := exec.CreateRunningOrder(uo)

	update := &OrderUpdate{
		OrderID: uoID, Symbol: "BTCUSDT", Status: "FILLED",
		AvgPrice: 50000, ExecutedQty: 0.1, PositionSide: "LONG",
		UserID: 1, PosType: order.PosTypeFutures, RelationID: 100,
	}

	done := rpc.expectRPC()
	if err := exec.HandleOrderFilled(update); err != nil {
		t.Fatalf("HandleOrderFilled: %v", err)
	}
	done()

	if rpc.getCallCount() != 1 {
		t.Fatalf("expected 1 RPC call, got %d", rpc.getCallCount())
	}
	if positions := gs.ListUserOrderPositions(); len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
}

func TestHandleOrderFilled_RiskControl_RequiresRiskFields(t *testing.T) {
	exec, rpc, gs, _ := setupExecutorWithRPC(t)
	defer gs.Shutdown()

	uoID := exec.CreateRunningOrder(makeUprunningOrder(1, 200, order.RelationTypeRiskControlStrategy, "BTCUSDT", order.PosTypeFutures, order.SideShort))

	update := &OrderUpdate{
		OrderID: uoID, Symbol: "BTCUSDT", Status: "FILLED",
		AvgPrice: 45000, ExecutedQty: 0.2, PositionSide: "SHORT",
		UserID: 1, PosType: order.PosTypeFutures, RelationID: 200,
	}

	if err := exec.HandleOrderFilled(update); err == nil {
		t.Fatal("expected missing risk-control field error")
	}
	if rpc.getCallCount() != 0 {
		t.Errorf("expected 0 RPC calls for risk_control, got %d", rpc.getCallCount())
	}
}

func TestHandleOrderFilled_RiskControl_PartialReduceClosesAndAppendsRemaining(t *testing.T) {
	exec, rpc, gs, repo := setupExecutorWithRPC(t)
	defer gs.Shutdown()

	posID := repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, UserStrategyID: 1000, Asset: "BTCUSDT", Exchange: "mock",
		PosType: order.PosTypeFutures, Side: order.SideLong,
		Quantity: 0.5, PosPrice: 50000, Deleted: 0,
	})
	userPositionID := repo.CreateUserPosition(&order.UserPosition{
		UserID: 1, UserStrategyID: 1000, Exchange: "mock",
		PosType:  order.PosTypeFutures,
		Quantity: 0.5, CurrentPrice: 50000, TotalMargin: 5000, Deleted: 0,
	})
	uo := makeUprunningOrder(1, 200, order.RelationTypeRiskControlStrategy, "BTCUSDT", order.PosTypeFutures, order.SideShort)
	uo.RiskCtrlStratID = 200
	uo.UserOrderPositionID = posID
	uo.UserPositionID = userPositionID
	uoID := exec.CreateRunningOrder(uo)

	update := &OrderUpdate{
		OrderID: uoID, Symbol: "BTCUSDT", Status: "FILLED",
		AvgPrice: 45000, ExecutedQty: 0.2, PositionSide: "SHORT",
		UserID: 1, PosType: order.PosTypeFutures, RelationID: 200,
	}

	if err := exec.HandleOrderFilled(update); err != nil {
		t.Fatalf("HandleOrderFilled: %v", err)
	}
	if rpc.getCallCount() != 0 {
		t.Errorf("expected 0 RPC calls for risk_control, got %d", rpc.getCallCount())
	}

	closed, err := repo.GetUserOrderPositionByID(posID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Deleted != 1 {
		t.Fatalf("expected original closed, got %+v", closed)
	}

	active := repo.ListActivePositions()
	if len(active) != 1 {
		t.Fatalf("expected 1 remaining active position, got %d", len(active))
	}
	if active[0].Quantity != 0.3 || active[0].RiskCtrlStratID != 200 {
		t.Fatalf("unexpected remaining position: %+v", active[0])
	}

	closedUserPosition, err := repo.GetUserPositionByID(userPositionID)
	if err != nil {
		t.Fatal(err)
	}
	if closedUserPosition.Deleted != 1 {
		t.Fatalf("expected original user_position closed, got %+v", closedUserPosition)
	}
	activeUserPositions := repo.ListActiveUserPositions()
	if len(activeUserPositions) != 1 {
		t.Fatalf("expected 1 remaining active user_position, got %d", len(activeUserPositions))
	}
	if activeUserPositions[0].Quantity != 0.3 || activeUserPositions[0].RiskCtrlStratID != 200 {
		t.Fatalf("unexpected remaining user_position: %+v", activeUserPositions[0])
	}
}

func TestHandleOrderFilled_RiskControlMarksRuleInactive(t *testing.T) {
	exec, _, gs, repo := setupExecutorWithRPC(t)
	defer gs.Shutdown()
	updater := &fakeRuleStatusUpdater{}
	exec.SetRuleStatusUpdater(updater)

	posID := repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, UserStrategyID: 1000, Asset: "BTCUSDT", Exchange: "mock",
		PosType: order.PosTypeFutures, Side: order.SideLong, Quantity: 0.2, Deleted: 0,
	})
	userPositionID := repo.CreateUserPosition(&order.UserPosition{UserID: 1, UserStrategyID: 1000, Exchange: "mock", PosType: order.PosTypeFutures, Quantity: 0.2, Deleted: 0})
	uo := makeUprunningOrder(1, 200, order.RelationTypeRiskControlStrategy, "BTCUSDT", order.PosTypeFutures, order.SideShort)
	uo.RiskCtrlStratID = 200
	uo.UserOrderPositionID = posID
	uo.UserPositionID = userPositionID
	uoID := exec.CreateRunningOrder(uo)

	if err := exec.HandleOrderFilled(&OrderUpdate{OrderID: uoID, Symbol: "BTCUSDT", Status: "FILLED", AvgPrice: 45000, ExecutedQty: 0.2, PositionSide: "SHORT", UserID: 1, PosType: order.PosTypeFutures, RelationID: 200}); err != nil {
		t.Fatalf("HandleOrderFilled: %v", err)
	}
	if !updater.resets[1000] {
		t.Fatalf("expected ResetRulesForStrategy(1000) called, got resets=%+v", updater.resets)
	}
}

func TestHandleOrderStatusUpdate_RiskControlCancelledMarksRuleActive(t *testing.T) {
	exec, _, gs, _ := setupExecutorWithRPC(t)
	defer gs.Shutdown()
	updater := &fakeRuleStatusUpdater{}
	exec.SetRuleStatusUpdater(updater)
	uo := makeUprunningOrder(1, 200, order.RelationTypeRiskControlStrategy, "BTCUSDT", order.PosTypeFutures, order.SideShort)
	uo.RiskCtrlStratID = 200
	uo.UserOrderPositionID = 1
	uo.UserPositionID = 1
	uoID := exec.CreateRunningOrder(uo)

	if err := exec.HandleOrderStatusUpdate(uoID, "CANCELLED", 0, 0, nil); err != nil {
		t.Fatalf("HandleOrderStatusUpdate: %v", err)
	}
	if updater.updates[200] != "active" {
		t.Fatalf("expected rule 200 active, got %+v", updater.updates)
	}
}

func TestHandleOrderStatusUpdate_RPC_Cancelled_UserOrders(t *testing.T) {
	exec, rpc, gs, _ := setupExecutorWithRPC(t)
	defer gs.Shutdown()

	uoID := exec.CreateRunningOrder(makeUprunningOrder(1, 100, order.RelationTypeUserOrders, "BTCUSDT", order.PosTypeFutures, order.SideLong))

	done := rpc.expectRPC()
	if err := exec.HandleOrderStatusUpdate(uoID, "CANCELLED", 0, 0, nil); err != nil {
		t.Fatalf("HandleOrderStatusUpdate: %v", err)
	}
	done()

	if rpc.getCallCount() != 1 {
		t.Fatalf("expected 1 RPC call for CANCELLED, got %d", rpc.getCallCount())
	}
	call, _ := rpc.getCall(0)
	if call.orderID != 100 || call.status != 3 {
		t.Errorf("expected orderID=100, status=3; got orderID=%d, status=%d", call.orderID, call.status)
	}
}

func TestHandleOrderStatusUpdate_RPC_NoCall_NonUserOrders(t *testing.T) {
	exec, rpc, gs, _ := setupExecutorWithRPC(t)
	defer gs.Shutdown()

	uoID := exec.CreateRunningOrder(makeUprunningOrder(1, 200, order.RelationTypeRiskControlStrategy, "BTCUSDT", order.PosTypeFutures, order.SideShort))

	if err := exec.HandleOrderStatusUpdate(uoID, "CANCELLED", 0, 0, nil); err != nil {
		t.Fatalf("HandleOrderStatusUpdate: %v", err)
	}

	if rpc.getCallCount() != 0 {
		t.Errorf("expected 0 RPC calls for risk_control CANCELLED, got %d", rpc.getCallCount())
	}
}

func TestHandleOrderFilled_RPC_Error(t *testing.T) {
	exec, rpc, gs, _ := setupExecutorWithRPC(t)
	defer gs.Shutdown()

	rpc.statusErr = context.DeadlineExceeded // simulate RPC failure

	uoID := exec.CreateRunningOrder(makeUprunningOrder(1, 100, order.RelationTypeUserOrders, "BTCUSDT", order.PosTypeFutures, order.SideLong))
	update := &OrderUpdate{
		OrderID: uoID, Status: "FILLED",
		AvgPrice: 50000, ExecutedQty: 0.1, PositionSide: "LONG",
		UserID: 1, PosType: order.PosTypeFutures, RelationID: 100,
	}

	done := rpc.expectRPCN(3) // executor retries 3 times on failure
	if err := exec.HandleOrderFilled(update); err != nil {
		t.Fatalf("HandleOrderFilled should not fail on RPC error: %v", err)
	}
	done()

	if rpc.getCallCount() != 3 {
		t.Errorf("expected 3 RPC calls despite error, got %d", rpc.getCallCount())
	}
	if positions := gs.ListUserOrderPositions(); len(positions) != 1 {
		t.Errorf("expected 1 position despite RPC failure, got %d", len(positions))
	}
}
