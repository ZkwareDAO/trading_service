package ws

import (
	"context"
	"fmt"
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"

	"github.com/adshao/go-binance/v2/futures"
)

type orderMonitorRPCClient struct{}

func (orderMonitorRPCClient) UpdateUserOrderStatusFILLED(ctx context.Context, orderID uint64) error {
	return nil
}

func (orderMonitorRPCClient) UpdateUserOrderStatusFailed(ctx context.Context, orderID uint64) error {
	return nil
}

func (orderMonitorRPCClient) QueryOrderPositionMetadata(ctx context.Context, orderID uint64) (*rpc.QueryOrderPositionMetadataResponse, error) {
	return &rpc.QueryOrderPositionMetadataResponse{UserOrderID: orderID, UserStrategyID: 1, Leverage: 5, FallbackPrice: 50000}, nil
}

func setupOrderMonitor(t *testing.T) (*OrderMonitor, *persistence.GlobalState) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	ex := exchange.NewMockExchange()
	exec := exchange.NewOrderExecutor(repo, ex)
	exec.SetRPCClient(orderMonitorRPCClient{})
	return NewOrderMonitor(exec, repo), gs
}

func TestOrderMonitor_HandleOrderUpdate_OrderNotFound(t *testing.T) {
	mon, gs := setupOrderMonitor(t)
	defer gs.Shutdown()

	event := &futures.WsUserDataEvent{
		Event: futures.UserDataEventTypeOrderTradeUpdate,
		WsUserDataOrderTradeUpdate: futures.WsUserDataOrderTradeUpdate{
			OrderTradeUpdate: futures.WsOrderTradeUpdate{
				ID:     99999,
				Symbol: "BTCUSDT",
				Status: futures.OrderStatusTypeFilled,
			},
		},
	}

	// Should not panic even though order doesn't exist
	mon.HandleOrderUpdate(event)

	if len(gs.UserOrderPositions) != 0 {
		t.Error("expected no positions created for unknown order")
	}
}

func TestOrderMonitor_HandleOrderUpdate_NonTradeEventIgnored(t *testing.T) {
	mon, gs := setupOrderMonitor(t)
	defer gs.Shutdown()

	now := time.Now()
	repo := mon.repo

	uo := &order.UprunningOrder{
		UserID:          1,
		Symbol:          "BTCUSDT",
		Exchange:        "binance",
		PosType:         order.PosTypeFutures,
		Side:            order.SideLong,
		ExchangeOrderID: 42,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_ = repo.CreateUprunningOrder(uo)

	event := &futures.WsUserDataEvent{
		Event: "ACCOUNT_UPDATE", // not ORDER_TRADE_UPDATE
	}
	mon.HandleOrderUpdate(event)

	uoAfter, _ := repo.GetUprunningOrderByID(uo.ID)
	if uoAfter.ExchangeOrderStatus != "" {
		t.Errorf("expected status empty (non-trade events ignored), got %s", uoAfter.ExchangeOrderStatus)
	}
}

func TestOrderMonitor_HandleOrderUpdate_NewToFilled(t *testing.T) {
	mon, gs := setupOrderMonitor(t)
	defer gs.Shutdown()

	now := time.Now()
	repo := mon.repo

	uo := &order.UprunningOrder{
		UserID:          1,
		RelationID:      100,
		RelationType:    "user_orders",
		Symbol:          "BTCUSDT",
		Exchange:        "binance",
		PosType:         order.PosTypeFutures,
		Side:            order.SideLong,
		ExchangeOrderID: 42,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_ = repo.CreateUprunningOrder(uo)

	event := &futures.WsUserDataEvent{
		Event: futures.UserDataEventTypeOrderTradeUpdate,
		WsUserDataOrderTradeUpdate: futures.WsUserDataOrderTradeUpdate{
			OrderTradeUpdate: futures.WsOrderTradeUpdate{
				Symbol:               "BTCUSDT",
				ID:                   42,
				Status:               futures.OrderStatusTypeFilled,
				AveragePrice:         "50000.0",
				AccumulatedFilledQty: "0.1",
				OriginalQty:          "0.1",
				TradeTime:            time.Now().UnixMilli(),
				PositionSide:         futures.PositionSideTypeLong,
			},
		},
	}

	mon.HandleOrderUpdate(event)

	// Verify uprunning order status
	uoAfter, err := repo.GetUprunningOrderByID(uo.ID)
	if err != nil {
		t.Fatalf("get uprunning order: %v", err)
	}
	if uoAfter.ExchangeOrderStatus != "FILLED" {
		t.Errorf("expected status=FILLED, got %s", uoAfter.ExchangeOrderStatus)
	}
	if uoAfter.ExchangeOrderPrice != 50000.0 {
		t.Errorf("expected exchange_order_price=50000, got %f", uoAfter.ExchangeOrderPrice)
	}
	if uoAfter.ExchangeOrderQty != 0.1 {
		t.Errorf("expected exchange_order_quantity=0.1, got %f", uoAfter.ExchangeOrderQty)
	}

	if countRowsForID(t, gs, "uprunning_orders.csv", uo.ID, "FILLED") != 1 {
		t.Fatalf("expected exactly one FILLED CSV row for order %d", uo.ID)
	}

	// Verify position created
	if len(gs.UserOrderPositions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(gs.UserOrderPositions))
	}
	var pos *order.UserOrderPosition
	for _, p := range gs.UserOrderPositions {
		pos = p
		break
	}

	checks := []struct {
		name      string
		got, want interface{}
	}{
		{"UserID", pos.UserID, uint64(1)},
		{"UprunningOrderID", pos.UprunningOrderID, uo.ID},
		{"UserOrderID", pos.UserOrderID, uint64(100)},
		{"Exchange", pos.Exchange, "binance"},
		{"Asset", pos.Asset, "BTCUSDT"},
		{"CurrentPrice", pos.CurrentPrice, 50000.0},
		{"Quantity", pos.Quantity, 0.1},
		{"Side", pos.Side, order.SideLong},
		{"Deleted", pos.Deleted, 0},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestOrderMonitor_HandleOrderUpdate_DuplicateIgnored(t *testing.T) {
	mon, gs := setupOrderMonitor(t)
	defer gs.Shutdown()

	repo := mon.repo
	now := time.Now()

	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          100,
		RelationType:        "user_orders",
		Symbol:              "ETHUSDT",
		Exchange:            "binance",
		PosType:             order.PosTypeFutures,
		Side:                order.SideLong,
		ExchangeOrderID:     10,
		ExchangeOrderStatus: "FILLED",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	_ = repo.CreateUprunningOrder(uo)

	// Send duplicate FILLED event — should be ignored
	event := &futures.WsUserDataEvent{
		Event: futures.UserDataEventTypeOrderTradeUpdate,
		WsUserDataOrderTradeUpdate: futures.WsUserDataOrderTradeUpdate{
			OrderTradeUpdate: futures.WsOrderTradeUpdate{
				Symbol:               "ETHUSDT",
				ID:                   10,
				Status:               futures.OrderStatusTypeFilled,
				AveragePrice:         "3000.0",
				AccumulatedFilledQty: "1.0",
				OriginalQty:          "1.0",
				TradeTime:            time.Now().UnixMilli(),
				PositionSide:         futures.PositionSideTypeLong,
			},
		},
	}

	mon.HandleOrderUpdate(event)

	if len(gs.UserOrderPositions) != 0 {
		t.Errorf("expected 0 positions for duplicate, got %d", len(gs.UserOrderPositions))
	}
}

func TestOrderMonitor_HandleOrderUpdate_ClosePosition(t *testing.T) {
	mon, gs := setupOrderMonitor(t)
	defer gs.Shutdown()

	repo := mon.repo
	now := time.Now()

	// Create existing open position
	existingPos := &order.UserOrderPosition{
		UserID:           1,
		UprunningOrderID: 1,
		UserOrderID:      100,
		Asset:            "BTCUSDT",
		Exchange:         "binance",
		PosType:          order.PosTypeFutures,
		Side:             order.SideLong,
		Quantity:         0.1,
		Deleted:          0,
		CurrentPrice:     50000,
		PosPrice:         50000,
		PosValue:         5000,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	posID := repo.CreateUserOrderPosition(existingPos)

	// Create close order (non-user_orders relation type)
	closeUo := &order.UprunningOrder{
		UserID:          1,
		RelationID:      200,
		RelationType:    "risk_control",
		Symbol:          "BTCUSDT",
		Exchange:        "binance",
		PosType:         order.PosTypeFutures,
		Side:            order.SideShort,
		ExchangeOrderID: 99,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_ = repo.CreateUprunningOrder(closeUo)

	// Simulate FILLED close event
	event := &futures.WsUserDataEvent{
		Event: futures.UserDataEventTypeOrderTradeUpdate,
		WsUserDataOrderTradeUpdate: futures.WsUserDataOrderTradeUpdate{
			OrderTradeUpdate: futures.WsOrderTradeUpdate{
				Symbol:               "BTCUSDT",
				ID:                   99,
				Status:               futures.OrderStatusTypeFilled,
				AveragePrice:         "51000.0",
				AccumulatedFilledQty: "0.1",
				OriginalQty:          "0.1",
				TradeTime:            time.Now().UnixMilli(),
				PositionSide:         futures.PositionSideTypeShort,
			},
		},
	}

	mon.HandleOrderUpdate(event)

	pos, err := repo.GetUserOrderPositionByID(posID)
	if err != nil {
		t.Fatalf("get position: %v", err)
	}
	if pos.Deleted != 1 {
		t.Errorf("expected position closed (deleted=1), got %d", pos.Deleted)
	}
	if pos.CloseTime == nil {
		t.Error("expected CloseTime to be set")
	}
}

func TestOrderMonitor_HandleOrderUpdate_PartialFillNoPosition(t *testing.T) {
	mon, gs := setupOrderMonitor(t)
	defer gs.Shutdown()

	repo := mon.repo
	now := time.Now()

	uo := &order.UprunningOrder{
		UserID:          1,
		RelationID:      100,
		RelationType:    "user_orders",
		Symbol:          "BTCUSDT",
		Exchange:        "binance",
		PosType:         order.PosTypeFutures,
		Side:            order.SideLong,
		ExchangeOrderID: 42,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_ = repo.CreateUprunningOrder(uo)

	// NEW → NEW_UPDATE partial fill (status still not FILLED)
	event := &futures.WsUserDataEvent{
		Event: futures.UserDataEventTypeOrderTradeUpdate,
		WsUserDataOrderTradeUpdate: futures.WsUserDataOrderTradeUpdate{
			OrderTradeUpdate: futures.WsOrderTradeUpdate{
				Symbol:               "BTCUSDT",
				ID:                   42,
				Status:               futures.OrderStatusTypeNew,
				AveragePrice:         "50000.0",
				AccumulatedFilledQty: "0.05",
				OriginalQty:          "0.1",
				TradeTime:            time.Now().UnixMilli(),
				PositionSide:         futures.PositionSideTypeLong,
			},
		},
	}

	mon.HandleOrderUpdate(event)

	// Partial fill should NOT create a position
	if len(gs.UserOrderPositions) != 0 {
		t.Errorf("expected 0 positions for partial fill, got %d", len(gs.UserOrderPositions))
	}

	// But uprunning order status should still be updated
	uoAfter, _ := repo.GetUprunningOrderByID(uo.ID)
	if uoAfter.ExchangeOrderStatus != "NEW" {
		t.Errorf("expected status=NEW, got %s", uoAfter.ExchangeOrderStatus)
	}
}

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

func floatToStr(f float64) string {
	return fmt.Sprintf("%.8f", f)
}
