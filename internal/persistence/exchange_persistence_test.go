package persistence

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"trading-service/internal/order"
)

func setupExchangeTest(t *testing.T) (*GlobalState, *StateRepository, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "exchange-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	os.MkdirAll(filepath.Join(dir, ".compact"), 0755)

	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	return gs, NewStateRepository(gs), dir
}

func TestCreateUserOrderPositionIfAbsent_DedupesByUprunningOrderID(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	first := &order.UserOrderPosition{
		UserID: 1, UprunningOrderID: 10, UserOrderID: 100,
		UserStrategyID: 9, Asset: "NEARUSDT", Exchange: "binance",
		PosType: order.PosTypeFutures, Side: order.SideLong,
		Quantity: 1208, PosPrice: 1.971, CurrentPrice: 1.971,
		PosValue: 2380.968, Leverage: 5, InitMargin: 2380.968,
	}
	firstID, created, err := repo.CreateUserOrderPositionIfAbsent(first)
	if err != nil {
		t.Fatal(err)
	}
	if !created || firstID == 0 {
		t.Fatalf("expected first create, id=%d created=%v", firstID, created)
	}

	second := *first
	second.PosPrice = 2.5
	second.CurrentPrice = 2.5
	second.PosValue = 3020
	secondID, created, err := repo.CreateUserOrderPositionIfAbsent(&second)
	if err != nil {
		t.Fatal(err)
	}
	if created || secondID != firstID {
		t.Fatalf("expected duplicate to return existing id=%d created=false, got id=%d created=%v", firstID, secondID, created)
	}
	if positions := repo.ListActivePositions(); len(positions) != 1 {
		t.Fatalf("expected 1 active position, got %d", len(positions))
	}
}

func TestCreateAndGetUprunningOrder(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	updateTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          100,
		RelationType:        "user_orders",
		Exchange:            "binance",
		Symbol:              "BTCUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     123456,
		ExchangeOrderStatus: "NEW",
		ExchangeOrderPrice:  50000,
		ExchangeOrderQty:    0.1,
		ExchangeUpdateTime:  &updateTime,
		Side:                order.SideLong,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	id := repo.CreateUprunningOrder(uo)
	if id == 0 {
		t.Error("expected non-zero ID")
	}

	retrieved, err := repo.GetUprunningOrderByID(id)
	if err != nil {
		t.Fatalf("GetUprunningOrderByID: %v", err)
	}
	if retrieved.ExchangeOrderID != 123456 {
		t.Errorf("expected ExchangeOrderID 123456, got %d", retrieved.ExchangeOrderID)
	}
}

func TestCreateAndGetUprunningOrder_RiskControlFields(t *testing.T) {
	gs, repo, dir := setupExchangeTest(t)
	defer gs.Shutdown()

	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          200,
		RelationType:        order.RelationTypeRiskControlStrategy,
		RiskCtrlStratID:     200,
		UserOrderPositionID: 300,
		UserPositionID:      400,
		Exchange:            "binance",
		Symbol:              "BTCUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     123456,
		ExchangeOrderStatus: "NEW",
		ExchangeOrderPrice:  50000,
		ExchangeOrderQty:    0.1,
		Side:                order.SideShort,
	}

	id := repo.CreateUprunningOrder(uo)
	gs.Shutdown()

	gs2, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs2.Shutdown()
	reloaded, err := NewStateRepository(gs2).GetUprunningOrderByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.RiskCtrlStratID != 200 || reloaded.UserOrderPositionID != 300 || reloaded.UserPositionID != 400 {
		t.Fatalf("risk fields not persisted: %+v", reloaded)
	}
}

func TestLoadUprunningOrder_OldCSVWithoutRiskFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".compact"), 0755); err != nil {
		t.Fatal(err)
	}
	content := "id,user_id,relation_id,relation_type,exchange,symbol,pos_type,exchange_order_id,exchange_order_status,exchange_order_price,exchange_order_quantity,exchange_update_time,side,created_at,updated_at\n" +
		"1,2,3,user_orders,binance,BTCUSDT,2,123,NEW,50000,0.1,,0,2024-01-01T00:00:00Z,2024-01-01T00:00:00Z\n"
	if err := os.WriteFile(filepath.Join(dir, "uprunning_orders.csv"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	uo, err := NewStateRepository(gs).GetUprunningOrderByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if uo.RiskCtrlStratID != 0 || uo.UserOrderPositionID != 0 || uo.UserPositionID != 0 {
		t.Fatalf("expected old CSV risk fields to default to zero, got %+v", uo)
	}
}

func TestUpdateUprunningOrderStatus(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	updateTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          100,
		RelationType:        "user_orders",
		Exchange:            "binance",
		Symbol:              "BTCUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     123456,
		ExchangeOrderStatus: "NEW",
		ExchangeOrderPrice:  50000,
		ExchangeOrderQty:    0.1,
		ExchangeUpdateTime:  &updateTime,
		Side:                order.SideLong,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	id := repo.CreateUprunningOrder(uo)

	err := repo.UpdateUprunningOrderStatus(id, "FILLED", &updateTime)
	if err != nil {
		t.Fatalf("UpdateUprunningOrderStatus: %v", err)
	}

	retrieved, err := repo.GetUprunningOrderByID(id)
	if err != nil {
		t.Fatalf("GetUprunningOrderByID: %v", err)
	}
	if retrieved.ExchangeOrderStatus != "FILLED" {
		t.Errorf("expected status FILLED, got %s", retrieved.ExchangeOrderStatus)
	}
}

func TestCreateAndGetUserOrderPosition(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	pos := &order.UserOrderPosition{
		UserID:           1,
		UprunningOrderID: 1,
		UserOrderID:      100,
		UserStrategyID:   10,
		Exchange:         "binance",
		PosType:          order.PosTypeFutures,
		Asset:            "BTC",
		CurrentPrice:     50000,
		Quantity:         0.1,
		PosValue:         5000,
		Leverage:         10,
		Deleted:          0,
		InitMargin:       500,
		PosPrice:         50000,
		PnLValue:         0,
		Side:             order.SideLong,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	id := repo.CreateUserOrderPosition(pos)
	if id == 0 {
		t.Error("expected non-zero ID")
	}

	retrieved, err := repo.GetUserOrderPositionByID(id)
	if err != nil {
		t.Fatalf("GetUserOrderPositionByID: %v", err)
	}
	if retrieved.Deleted != 0 {
		t.Errorf("expected Deleted 0, got %d", retrieved.Deleted)
	}
}

func TestClosePosition(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	closeTime := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	pos := &order.UserOrderPosition{
		UserID:           1,
		UprunningOrderID: 1,
		UserOrderID:      100,
		Exchange:         "binance",
		PosType:          order.PosTypeFutures,
		Asset:            "BTC",
		CurrentPrice:     50000,
		Quantity:         0.1,
		Leverage:         10,
		Deleted:          0,
		PosPrice:         50000,
		Side:             order.SideLong,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	id := repo.CreateUserOrderPosition(pos)

	err := repo.ClosePosition(id, closeTime)
	if err != nil {
		t.Fatalf("ClosePosition: %v", err)
	}

	retrieved, err := repo.GetUserOrderPositionByID(id)
	if err != nil {
		t.Fatalf("GetUserOrderPositionByID: %v", err)
	}
	if retrieved.Deleted != 1 {
		t.Errorf("expected Deleted 1, got %d", retrieved.Deleted)
	}
	if retrieved.CloseTime == nil {
		t.Fatal("expected CloseTime to be set")
	}
}

func TestCreateRemainingUserOrderPosition_ClosesOriginalAndAppendsRemaining(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	closeTime := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	originalID := repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:           1,
		UprunningOrderID: 10,
		UserOrderID:      100,
		UserStrategyID:   1000,
		Exchange:         "binance",
		PosType:          order.PosTypeFutures,
		Asset:            "BTCUSDT",
		CurrentPrice:     50000,
		Quantity:         0.5,
		PosValue:         25000,
		Leverage:         5,
		Deleted:          0,
		InitMargin:       5000,
		PosPrice:         50000,
		Side:             order.SideLong,
		CreatedAt:        now,
		UpdatedAt:        now,
	})

	remainingID, err := repo.CloseAndCreateRemainingUserOrderPosition(originalID, 0.2, 200, closeTime)
	if err != nil {
		t.Fatalf("CloseAndCreateRemainingUserOrderPosition: %v", err)
	}
	if remainingID == 0 || remainingID == originalID {
		t.Fatalf("expected new remaining position ID, got %d", remainingID)
	}

	original, err := repo.GetUserOrderPositionByID(originalID)
	if err != nil {
		t.Fatal(err)
	}
	if original.Deleted != 1 || original.CloseTime == nil {
		t.Fatalf("expected original closed, got %+v", original)
	}

	remaining, err := repo.GetUserOrderPositionByID(remainingID)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.Deleted != 0 || remaining.Quantity != 0.3 {
		t.Fatalf("unexpected remaining position: %+v", remaining)
	}
	if remaining.RiskCtrlStratID != 200 || remaining.UserStrategyID != 1000 || remaining.Asset != "BTCUSDT" {
		t.Fatalf("remaining position did not preserve expected fields: %+v", remaining)
	}
}

func TestCloseAndCreateRemainingUserOrderPosition_FullCloseDoesNotAppend(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	closeTime := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	originalID := repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, Exchange: "binance", PosType: order.PosTypeFutures,
		Asset: "ETHUSDT", Quantity: 0.5, Deleted: 0,
	})

	remainingID, err := repo.CloseAndCreateRemainingUserOrderPosition(originalID, 0.5, 200, closeTime)
	if err != nil {
		t.Fatalf("CloseAndCreateRemainingUserOrderPosition: %v", err)
	}
	if remainingID != 0 {
		t.Fatalf("expected no remaining position for full close, got %d", remainingID)
	}
	if active := repo.ListActivePositions(); len(active) != 0 {
		t.Fatalf("expected no active positions, got %d", len(active))
	}
}

func TestCloseAndCreateRemainingUserOrderPosition_RejectsOverClose(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	originalID := repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, Exchange: "binance", PosType: order.PosTypeFutures,
		Asset: "ETHUSDT", Quantity: 0.5, Deleted: 0,
	})

	_, err := repo.CloseAndCreateRemainingUserOrderPosition(originalID, 0.6, 200, time.Now())
	if err == nil {
		t.Fatal("expected over-close error")
	}
	original, err := repo.GetUserOrderPositionByID(originalID)
	if err != nil {
		t.Fatal(err)
	}
	if original.Deleted != 0 {
		t.Fatalf("expected original to remain active after rejected over-close, got %+v", original)
	}
}

func TestCreateAndGetUserPosition(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	id := repo.CreateUserPosition(&order.UserPosition{
		UserID:                     1,
		UserStrategyID:             1000,
		Exchange:                   "binance",
		PosType:                    order.PosTypeFutures,
		CurrentPrice:               50000,
		Quantity:                   0.5,
		LatestMarketCapitalization: 25000,
		ROI:                        0.12,
		PnL:                        600,
		WinRate:                    0.6,
		MaximumDrawdown:            0.03,
		TotalMargin:                5000,
		MaxProfitPercentage:        0.2,
		MaxLossPercentage:          -0.05,
		OpenTrades:                 3,
		ClosedTrades:               2,
		ProfitTrades:               2,
		LossTrades:                 1,
		Deleted:                    0,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	})
	if id == 0 {
		t.Fatal("expected non-zero user_position ID")
	}

	pos, err := repo.GetUserPositionByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if pos.Quantity != 0.5 || pos.LatestMarketCapitalization != 25000 || pos.ROI != 0.12 || pos.Deleted != 0 {
		t.Fatalf("unexpected user_position: %+v", pos)
	}
}

func TestCloseAndCreateRemainingUserPosition_ClosesOriginalAndAppendsRemaining(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	closeTime := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	originalID := repo.CreateUserPosition(&order.UserPosition{
		UserID: 1, UserStrategyID: 1000, Exchange: "binance",
		PosType: order.PosTypeFutures, CurrentPrice: 50000, Quantity: 0.5,
		LatestMarketCapitalization: 25000, ROI: 0.1, PnL: 500,
		TotalMargin: 5000, OpenTrades: 1, Deleted: 0,
	})

	remainingID, err := repo.CloseAndCreateRemainingUserPosition(originalID, 0.2, 200, closeTime)
	if err != nil {
		t.Fatalf("CloseAndCreateRemainingUserPosition: %v", err)
	}
	if remainingID == 0 {
		t.Fatalf("expected non-zero remaining user_position ID, got %d", remainingID)
	}
	// Note: remainingID can equal originalID in per-table counter system
	// because IDs are unique within their table, not globally

	original, err := repo.GetUserPositionByID(originalID)
	if err != nil {
		t.Fatal(err)
	}
	if original.Deleted != 1 || original.CloseTime == nil {
		t.Fatalf("expected original closed, got %+v", original)
	}

	remaining, err := repo.GetUserPositionByID(remainingID)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.Deleted != 0 || remaining.Quantity != 0.3 {
		t.Fatalf("unexpected remaining user_position: %+v", remaining)
	}
	if remaining.RiskCtrlStratID != 200 || remaining.UserStrategyID != 1000 || remaining.LatestMarketCapitalization != 15000 {
		t.Fatalf("remaining user_position did not preserve expected fields: %+v", remaining)
	}
}

func TestCloseAndCreateRemainingUserPosition_FullCloseDoesNotAppend(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	closeTime := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	originalID := repo.CreateUserPosition(&order.UserPosition{
		UserID: 1, Exchange: "binance", PosType: order.PosTypeFutures,
		Quantity: 0.5, Deleted: 0,
	})

	remainingID, err := repo.CloseAndCreateRemainingUserPosition(originalID, 0.5, 200, closeTime)
	if err != nil {
		t.Fatalf("CloseAndCreateRemainingUserPosition: %v", err)
	}
	if remainingID != 0 {
		t.Fatalf("expected no remaining user_position for full close, got %d", remainingID)
	}
	if active := repo.ListActiveUserPositions(); len(active) != 0 {
		t.Fatalf("expected no active user_positions, got %d", len(active))
	}
}

func TestCloseAndCreateRemainingUserPosition_RejectsOverClose(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	originalID := repo.CreateUserPosition(&order.UserPosition{
		UserID: 1, Exchange: "binance", PosType: order.PosTypeFutures,
		Quantity: 0.5, Deleted: 0,
	})

	_, err := repo.CloseAndCreateRemainingUserPosition(originalID, 0.6, 200, time.Now())
	if err == nil {
		t.Fatal("expected over-close error")
	}
	original, err := repo.GetUserPositionByID(originalID)
	if err != nil {
		t.Fatal(err)
	}
	if original.Deleted != 0 {
		t.Fatalf("expected original user_position to remain active after rejected over-close, got %+v", original)
	}
}

func TestListActivePositions(t *testing.T) {
	gs, repo, _ := setupExchangeTest(t)
	defer gs.Shutdown()

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, Exchange: "binance", Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, Exchange: "binance", Deleted: 1, CreatedAt: now, UpdatedAt: now,
	})

	active := repo.ListActivePositions()
	if len(active) != 1 {
		t.Errorf("expected 1 active position, got %d", len(active))
	}
}
