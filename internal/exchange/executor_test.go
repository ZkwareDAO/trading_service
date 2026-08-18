package exchange

import (
	"testing"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

func setupOrderHandlerTest(t *testing.T) (*OrderExecutor, *MockExchange, *persistence.GlobalState, *persistence.StateRepository) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	mock := NewMockExchange()
	mock.SetPrice("BTCUSDT", 50000)
	exec := NewOrderExecutor(repo, mock)
	exec.SetRPCClient(&mockRPCClient{metadata: &rpc.QueryOrderPositionMetadataResponse{UserStrategyID: 1, Leverage: 5, FallbackPrice: 50000}})
	return exec, mock, gs, repo
}

func TestCreateOrder_CreatesRunningOrderAndCallsExchange(t *testing.T) {
	exec, mock, gs, repo := setupOrderHandlerTest(t)
	defer gs.Shutdown()

	exchangeOrderID := exec.CreateOrder(&order.UprunningOrder{
		UserID:             1,
		RelationID:         100,
		RelationType:       "user_orders",
		Symbol:             "BTCUSDT",
		PosType:            order.PosTypeFutures,
		Exchange:           "mock",
		Side:               order.SideLong,
		ExchangeOrderPrice: 50000,
		ExchangeOrderQty:   0.1,
	}, OrderSideBuy, OrderTypeLimit, PositionSideLong)

	if exchangeOrderID == 0 {
		t.Error("expected non-zero exchange order ID")
	}
	if len(mock.CreatedOrders) != 1 {
		t.Errorf("expected 1 exchange order, got %d", len(mock.CreatedOrders))
	}

	// Verify only ONE uprunning_order record exists (not duplicated)
	all := repo.ListUprunningOrders()
	if len(all) != 1 {
		t.Errorf("expected exactly 1 UprunningOrder record, got %d", len(all))
	}
}

func TestCreateOrder_ShortSide(t *testing.T) {
	exec, mock, gs, _ := setupOrderHandlerTest(t)
	defer gs.Shutdown()

	exec.CreateOrder(&order.UprunningOrder{
		UserID:       1,
		RelationID:   100,
		RelationType: order.RelationTypeUserOrders,
		Symbol:       "BTCUSDT",
		PosType:      order.PosTypeFutures,
		Exchange:     "mock",
		Side:         order.SideShort,
	}, OrderSideSell, OrderTypeLimit, PositionSideShort)

	if len(mock.CreatedOrders) != 1 {
		t.Errorf("expected 1 exchange order, got %d", len(mock.CreatedOrders))
	}
}

func TestHandleOrderFilled_CreatesPosition(t *testing.T) {
	exec, _, gs, _ := setupOrderHandlerTest(t)
	defer gs.Shutdown()

	uo := &order.UprunningOrder{
		UserID:       1,
		RelationID:   100,
		RelationType: order.RelationTypeUserOrders,
		Symbol:       "BTCUSDT",
		PosType:      order.PosTypeFutures,
		Exchange:     "mock",
		Side:         order.SideLong,
	}
	uoID := exec.CreateRunningOrder(uo)

	update := &OrderUpdate{
		OrderID:      uoID,
		Symbol:       "BTCUSDT",
		Status:       "FILLED",
		AvgPrice:     50000,
		ExecutedQty:  0.1,
		PositionSide: "LONG",
		UserID:       1,
		PosType:      order.PosTypeFutures,
		RelationID:   100,
	}

	if err := exec.HandleOrderFilled(update); err != nil {
		t.Fatalf("HandleOrderFilled: %v", err)
	}

	positions := gs.ListUserOrderPositions()
	if len(positions) != 1 {
		t.Errorf("expected 1 position, got %d", len(positions))
	}

	pos := positions[0]
	if pos.Deleted != 0 {
		t.Errorf("expected deleted=0, got %d", pos.Deleted)
	}
	if pos.CloseTime != nil {
		t.Error("expected nil CloseTime for new position")
	}
	if pos.Quantity != 0.1 {
		t.Errorf("expected quantity 0.1, got %f", pos.Quantity)
	}
	if pos.PosPrice != 50000 {
		t.Errorf("expected posPrice 50000, got %f", pos.PosPrice)
	}
}

func TestHandleOrderFilled_RiskControlRelation(t *testing.T) {
	exec, _, gs, repo := setupOrderHandlerTest(t)
	defer gs.Shutdown()

	// First create an existing position
	pos := &order.UserOrderPosition{
		UserID:         1,
		UserStrategyID: 1,
		Exchange:       "mock",
		PosType:        order.PosTypeFutures,
		Asset:          "BTCUSDT",
		Side:           order.SideLong,
		Quantity:       0.2,
		Deleted:        0,
	}
	posID := repo.CreateUserOrderPosition(pos)
	userPositionID := repo.CreateUserPosition(&order.UserPosition{
		UserID:         1,
		UserStrategyID: 1,
		Exchange:       "mock",
		PosType:        order.PosTypeFutures,
		Quantity:       0.2,
		Deleted:        0,
	})
	t.Logf("Created position posID=%d", posID)
	t.Logf("Active positions before: %d", len(repo.ListActivePositions()))
	t.Logf("All positions before: %d", len(gs.ListUserOrderPositions()))

	// risk_control_strategy FILLED should close the position
	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          200,
		RelationType:        order.RelationTypeRiskControlStrategy,
		RiskCtrlStratID:     200,
		UserOrderPositionID: posID,
		UserPositionID:      userPositionID,
		Symbol:              "BTCUSDT",
		PosType:             order.PosTypeFutures,
		Exchange:            "mock",
		Side:                order.SideShort,
	}
	uoID := exec.CreateRunningOrder(uo)
	t.Logf("Created UprunningOrder uoID=%d", uoID)

	update := &OrderUpdate{
		OrderID:      uoID,
		Symbol:       "BTCUSDT",
		Status:       "FILLED",
		AvgPrice:     45000,
		ExecutedQty:  0.2,
		PositionSide: "SHORT",
		UserID:       1,
		PosType:      order.PosTypeFutures,
		RelationID:   200,
	}

	// Check UprunningOrder
	uoRecord, err := repo.GetUprunningOrderByID(uoID)
	if err != nil {
		t.Fatalf("GetUprunningOrderByID: %v", err)
	}
	t.Logf("uoRecord.RelationType=%s, uoRecord.Symbol=%s", uoRecord.RelationType, uoRecord.Symbol)

	if err := exec.HandleOrderFilled(update); err != nil {
		t.Fatalf("HandleOrderFilled: %v", err)
	}

	t.Logf("Active positions after: %d", len(repo.ListActivePositions()))
	t.Logf("All positions after: %d", len(gs.ListUserOrderPositions()))

	// Verify the position was closed
	closedPos, err := repo.GetUserOrderPositionByID(posID)
	if err != nil {
		t.Fatal(err)
	}
	if closedPos.Deleted != 1 {
		t.Errorf("expected position deleted=1, got %d", closedPos.Deleted)
	}
	if closedPos.CloseTime == nil {
		t.Error("expected CloseTime to be set")
	}
	// No new position should be created (check active count)
	activePositions := repo.ListActivePositions()
	if len(activePositions) != 0 {
		t.Errorf("expected 0 active positions after close, got %d", len(activePositions))
	}
}

func TestHandleOrderCancelled(t *testing.T) {
	exec, _, gs, repo := setupOrderHandlerTest(t)
	defer gs.Shutdown()

	uo := &order.UprunningOrder{
		UserID:       1,
		RelationID:   100,
		RelationType: order.RelationTypeUserOrders,
		Symbol:       "BTCUSDT",
		PosType:      order.PosTypeFutures,
		Exchange:     "mock",
		Side:         order.SideLong,
	}
	uoID := exec.CreateRunningOrder(uo)

	err := exec.HandleOrderStatusUpdate(uoID, "CANCELLED", 0, 0, nil)
	if err != nil {
		t.Fatalf("HandleOrderStatusUpdate: %v", err)
	}

	// Verify in-memory status
	uoRecord, err := repo.GetUprunningOrderByID(uoID)
	if err != nil {
		t.Fatal(err)
	}
	if uoRecord.ExchangeOrderStatus != "CANCELLED" {
		t.Errorf("expected CANCELLED, got %s", uoRecord.ExchangeOrderStatus)
	}
}

func TestHandleOrderFilled_MultipleUpdates(t *testing.T) {
	exec, _, gs, _ := setupOrderHandlerTest(t)
	defer gs.Shutdown()

	uo := &order.UprunningOrder{
		UserID:       1,
		RelationID:   100,
		RelationType: order.RelationTypeUserOrders,
		Symbol:       "BTCUSDT",
		PosType:      order.PosTypeFutures,
		Exchange:     "mock",
		Side:         order.SideLong,
	}
	uoID := exec.CreateRunningOrder(uo)

	// First update: NEW → PARTIAL
	if err := exec.HandleOrderStatusUpdate(uoID, "PARTIAL", 0, 0, nil); err != nil {
		t.Fatalf("HandleOrderStatusUpdate PARTIAL: %v", err)
	}

	// Second update: PARTIAL → FILLED
	update := &OrderUpdate{
		OrderID:      uoID,
		Status:       "FILLED",
		AvgPrice:     50000,
		ExecutedQty:  0.1,
		PositionSide: "LONG",
		UserID:       1,
		PosType:      order.PosTypeFutures,
		RelationID:   100,
	}
	if err := exec.HandleOrderFilled(update); err != nil {
		t.Fatalf("HandleOrderFilled: %v", err)
	}

	// Only 1 position should be created
	positions := gs.ListUserOrderPositions()
	if len(positions) != 1 {
		t.Errorf("expected 1 position, got %d", len(positions))
	}
}
