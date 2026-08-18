package persistence

import (
	"testing"
	"time"

	"trading-service/internal/order"
)

// TestGlobalState_ShutdownReload verifies that after Shutdown + Reload,
// the in-memory state reflects all CSV changes (including those from
// async writes and other services).
func TestGlobalState_ShutdownReload(t *testing.T) {
	dir := t.TempDir()

	// Create service A (simulates user_order_service)
	gsA, err := NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState A: %v", err)
	}
	repoA := NewStateRepository(gsA)

	// Service A creates an order
	uo := &order.UprunningOrder{
		UserID:              1,
		Exchange:            "binance",
		Symbol:              "NEARUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     100,
		ExchangeOrderStatus: "NEW",
		RelationID:          10,
		RelationType:        order.RelationTypeUserOrders,
		Side:                order.SideLong,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	repoA.CreateUprunningOrder(uo)

	// Service A writes and shuts down (async write completes)
	gsA.Shutdown()

	// Create service B (simulates position_monitor_service)
	// It should pick up the order from CSV
	gsB, err := NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState B: %v", err)
	}
	repoB := NewStateRepository(gsB)

	// Service B finds the order
	found, err := repoB.FindUprunningOrderByExchangeID(100)
	if err != nil {
		t.Fatalf("Service B should find order: %v", err)
	}
	if found.ExchangeOrderStatus != "NEW" {
		t.Errorf("expected NEW, got %s", found.ExchangeOrderStatus)
	}

	// Service B updates status to FILLED
	updateTime := time.Now()
	if err := repoB.UpdateUprunningOrderStatus(found.ID, "FILLED", &updateTime); err != nil {
		t.Fatalf("UpdateUprunningOrderStatus: %v", err)
	}

	// Service B creates a position
	pos := &order.UserOrderPosition{
		UserID:           1,
		UprunningOrderID: found.ID,
		UserOrderID:      10,
		UserStrategyID:   100,
		Exchange:         "binance",
		PosType:          order.PosTypeFutures,
		Asset:            "NEARUSDT",
		CurrentPrice:     2.5,
		Quantity:         100,
		PosValue:         250,
		PosPrice:         2.5,
		Side:             order.SideLong,
		Deleted:          0,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	repoB.CreateUserOrderPosition(pos)

	// Service B shuts down and compacts (simulates graceful shutdown)
	gsB.Shutdown()
	if err := gsB.CompactAll(); err != nil {
		t.Fatalf("CompactAll: %v", err)
	}

	// Create service C (simulates restart after ctrl+C)
	// It should see FILLED status and the position
	gsC, err := NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState C: %v", err)
	}
	repoC := NewStateRepository(gsC)

	// Verify order status
	updated, err := repoC.FindUprunningOrderByExchangeID(100)
	if err != nil {
		t.Fatalf("Service C should find order: %v", err)
	}
	if updated.ExchangeOrderStatus != "FILLED" {
		t.Errorf("expected FILLED after restart, got %s", updated.ExchangeOrderStatus)
	}

	// Verify position exists
	positions := repoC.ListActivePositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 position after restart, got %d", len(positions))
	}
	if positions[0].UserStrategyID != 100 {
		t.Errorf("expected UserStrategyID=100, got %d", positions[0].UserStrategyID)
	}
	if positions[0].Quantity != 100 {
		t.Errorf("expected Quantity=100, got %f", positions[0].Quantity)
	}
}

// TestGlobalState_ReloadMergesState verifies Reload picks up CSV changes
// that were made after initial load.
func TestGlobalState_ReloadMergesState(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState: %v", err)
	}
	repo := NewStateRepository(gs)

	// Initially empty
	orders := repo.ListUprunningOrdersByExchangeStatus("NEW")
	if len(orders) != 0 {
		t.Errorf("expected 0 orders, got %d", len(orders))
	}

	// Simulate another service writing to CSV directly
	gs2, _ := NewGlobalState(dir)
	repo2 := NewStateRepository(gs2)
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
	repo2.CreateUprunningOrder(uo)
	gs2.Shutdown()

	// gs should NOT see it yet (no reload)
	orders = repo.ListUprunningOrdersByExchangeStatus("NEW")
	if len(orders) != 0 {
		t.Errorf("expected 0 orders before reload, got %d", len(orders))
	}

	// After reload, gs should see the order
	if err := gs.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	orders = repo.ListUprunningOrdersByExchangeStatus("NEW")
	if len(orders) != 1 {
		t.Fatalf("expected 1 order after reload, got %d", len(orders))
	}
	if orders[0].ExchangeOrderID != 200 {
		t.Errorf("expected ExchangeOrderID=200, got %d", orders[0].ExchangeOrderID)
	}
}

// TestGlobalState_CompactAllPreservesLatest verifies CompactAll keeps the
// latest version of each record (dedup by ID, latest UpdatedAt wins).
func TestGlobalState_CompactAllPreservesLatest(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState: %v", err)
	}
	repo := NewStateRepository(gs)

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
	time.Sleep(50 * time.Millisecond)

	// Get the actual ID
	orders := repo.ListUprunningOrdersByExchangeStatus("NEW")
	if len(orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orders))
	}

	// Update to FILLED
	updateTime := time.Now()
	repo.UpdateUprunningOrderStatus(orders[0].ID, "FILLED", &updateTime)

	// Shutdown and compact
	gs.Shutdown()
	if err := gs.CompactAll(); err != nil {
		t.Fatalf("CompactAll: %v", err)
	}

	// Reload and verify we see FILLED (not NEW)
	if err := gs.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	loaded, err := repo.FindUprunningOrderByExchangeID(300)
	if err != nil {
		t.Fatalf("FindUprunningOrderByExchangeID: %v", err)
	}
	if loaded.ExchangeOrderStatus != "FILLED" {
		t.Errorf("expected FILLED after compact+reload, got %s", loaded.ExchangeOrderStatus)
	}
}
