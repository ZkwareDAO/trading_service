package exchange

import (
	"testing"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

func setupPositionManagerTest(t *testing.T) (*PositionManager, *MockExchange, *persistence.GlobalState) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	mock := NewMockExchange()
	pm := NewPositionManager(repo, mock)
	return pm, mock, gs
}

func TestSetLeverage(t *testing.T) {
	pm, mock, gs := setupPositionManagerTest(t)
	defer gs.Shutdown()

	if err := pm.SetLeverage("BTCUSDT", 10); err != nil {
		t.Fatalf("SetLeverage: %v", err)
	}

	lev, err := mock.GetLeverage("BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	if lev != 10 {
		t.Errorf("expected leverage 10, got %d", lev)
	}
}

func TestGetPrice(t *testing.T) {
	pm, mock, gs := setupPositionManagerTest(t)
	defer gs.Shutdown()

	mock.SetPrice("BTCUSDT", 50000)

	price, err := pm.GetPrice("BTCUSDT")
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if price != 50000 {
		t.Errorf("expected 50000, got %f", price)
	}
}

func TestListActivePositions(t *testing.T) {
	pm, _, gs := setupPositionManagerTest(t)
	defer gs.Shutdown()

	_ = pm.repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, Exchange: "binance", Deleted: 0,
		Asset: "BTC", PosType: order.PosTypeFutures,
		Side: order.SideLong, Quantity: 0.1, PosPrice: 50000,
	})
	_ = pm.repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, Exchange: "binance", Deleted: 1,
		Asset: "ETH", PosType: order.PosTypeFutures,
		Side: order.SideShort, Quantity: 1,
	})

	active := pm.ListActivePositions()
	if len(active) != 1 {
		t.Errorf("expected 1 active position, got %d", len(active))
	}
	if active[0].Asset != "BTC" {
		t.Errorf("expected BTC position, got %s", active[0].Asset)
	}
}

func TestClosePosition(t *testing.T) {
	pm, _, gs := setupPositionManagerTest(t)
	defer gs.Shutdown()

	id := pm.repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, Exchange: "binance", Deleted: 0,
		Asset: "BTC", PosType: order.PosTypeFutures,
		Side: order.SideLong, Quantity: 0.1, PosPrice: 50000,
	})

	if err := pm.ClosePosition(id); err != nil {
		t.Fatalf("ClosePosition: %v", err)
	}

	pos, err := pm.repo.GetUserOrderPositionByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if pos.Deleted != 1 {
		t.Errorf("expected deleted=1, got %d", pos.Deleted)
	}
	if pos.CloseTime == nil {
		t.Error("expected CloseTime to be set")
	}
}
