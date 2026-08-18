package ws

import (
	"testing"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

func TestPriceManager_UpdateAndGet(t *testing.T) {
	pm := NewPriceManager()
	pm.UpdatePrice("BTCUSDT", 50000)
	pm.UpdatePrice("ETHUSDT", 3000)

	price, ok := pm.GetPrice("BTCUSDT")
	if !ok {
		t.Fatal("expected BTCUSDT price")
	}
	if price != 50000 {
		t.Errorf("expected 50000, got %f", price)
	}
	if _, ok := pm.GetPrice("NONEXISTENT"); ok {
		t.Error("expected not found for NONEXISTENT")
	}
}

func TestPriceManager_Snapshot(t *testing.T) {
	pm := NewPriceManager()
	pm.UpdatePrice("BTCUSDT", 50000)
	pm.UpdatePrice("ETHUSDT", 3000)

	snapshot := pm.Snapshot()
	if len(snapshot) != 2 {
		t.Errorf("expected 2 prices, got %d", len(snapshot))
	}
	if snapshot["BTCUSDT"] != 50000 {
		t.Errorf("expected BTCUSDT 50000, got %f", snapshot["BTCUSDT"])
	}
}

func TestPriceManager_UpdateOverwrites(t *testing.T) {
	pm := NewPriceManager()
	pm.UpdatePrice("BTCUSDT", 50000)
	pm.UpdatePrice("BTCUSDT", 55000)

	price, _ := pm.GetPrice("BTCUSDT")
	if price != 55000 {
		t.Errorf("expected 55000, got %f", price)
	}
}

func TestPriceManager_All(t *testing.T) {
	pm := NewPriceManager()
	pm.UpdatePrice("BTCUSDT", 50000)
	pm.UpdatePrice("ETHUSDT", 3000)

	all := pm.All()
	if len(all) != 2 {
		t.Errorf("expected 2, got %d", len(all))
	}
}

func TestParseHelpers_Float64(t *testing.T) {
	f, err := parseFloat64("50000.5")
	if err != nil {
		t.Fatal(err)
	}
	if f != 50000.5 {
		t.Errorf("expected 50000.5, got %f", f)
	}
}

func TestOrderMonitor_FindRunningOrderByExchangeID(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	defer gs.Shutdown()

	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          100,
		RelationType:        "user_orders",
		Symbol:              "BTCUSDT",
		PosType:             order.PosTypeFutures,
		Exchange:            "binance",
		Side:                order.SideLong,
		ExchangeOrderID:     123456,
		ExchangeOrderStatus: "NEW",
	}
	repo.CreateUprunningOrder(uo)

	found, err := repo.FindUprunningOrderByExchangeID(123456)
	if err != nil {
		t.Fatalf("FindUprunningOrderByExchangeID: %v", err)
	}
	if found.ExchangeOrderID != 123456 {
		t.Errorf("expected ExchangeOrderID 123456, got %d", found.ExchangeOrderID)
	}
}

func TestOrderMonitor_FindNotFound(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	defer gs.Shutdown()

	_, err = repo.FindUprunningOrderByExchangeID(999)
	if err == nil {
		t.Error("expected error for non-existent order")
	}
}

func TestBinanceWsPriceManager_PriceUpdates(t *testing.T) {
	wpm := NewBinanceWsPriceManager()
	wpm.Manager.UpdateFuturesPrice("BTCUSDT", 50000)
	wpm.Manager.UpdateFuturesPrice("ETHUSDT", 3000)

	snap := wpm.Manager.Snapshot()
	if len(snap) != 2 {
		t.Errorf("expected 2 prices, got %d", len(snap))
	}
	if snap["BTCUSDT"] != 50000 {
		t.Errorf("expected BTCUSDT 50000, got %f", snap["BTCUSDT"])
	}
}
