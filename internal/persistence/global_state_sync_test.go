package persistence

import (
	"testing"
	"time"

	"trading-service/internal/order"
)

// TestReloadUserOrderPositionsIfNeeded_SkipsWhenWithinInterval tests that
// reload is skipped when called within the sync interval.
func TestReloadUserOrderPositionsIfNeeded_SkipsWhenWithinInterval(t *testing.T) {
	dir := setupTestDir(t)

	// Create initial CSV with one position (deleted=0)
	now := time.Now()
	writeCSV(t, dir, "user_order_positions.csv", []interface{}{
		&order.UserOrderPosition{
			ID:             1,
			UserID:         1,
			UserOrderID:    10,
			UserStrategyID: 100,
			Exchange:       "binance",
			PosType:        order.PosTypeFutures,
			Asset:          "NEARUSDT",
			Side:           order.SideLong,
			Quantity:       1.5,
			PosPrice:       5.0,
			CurrentPrice:   5.5,
			Leverage:       5,
			Deleted:        0,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	})

	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	repo := NewStateRepository(gs)
	repo.SetSyncInterval(5 * time.Second)

	// First call - should reload
	if err := repo.ReloadUserOrderPositionsIfNeeded(); err != nil {
		t.Fatalf("first reload failed: %v", err)
	}

	// Check position loaded correctly
	pos, err := repo.GetUserOrderPositionByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if pos.Deleted != 0 {
		t.Errorf("expected deleted=0, got %d", pos.Deleted)
	}

	// Update CSV externally (simulating PMS update) - set deleted=1
	later := now.Add(time.Second)
	writeCSV(t, dir, "user_order_positions.csv", []interface{}{
		&order.UserOrderPosition{
			ID:             1,
			UserID:         1,
			UserOrderID:    10,
			UserStrategyID: 100,
			Exchange:       "binance",
			PosType:        order.PosTypeFutures,
			Asset:          "NEARUSDT",
			Side:           order.SideLong,
			Quantity:       1.5,
			PosPrice:       5.0,
			CurrentPrice:   5.5,
			Leverage:       5,
			Deleted:        1, // Changed to deleted
			CreatedAt:      now,
			UpdatedAt:      later,
		},
	})

	// Second call immediately (within interval) - should skip
	if err := repo.ReloadUserOrderPositionsIfNeeded(); err != nil {
		t.Fatalf("second reload failed: %v", err)
	}

	// Position should still have old value (deleted=0) because reload was skipped
	pos, err = repo.GetUserOrderPositionByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if pos.Deleted != 0 {
		t.Errorf("expected deleted=0 (cached), got %d", pos.Deleted)
	}
}

// TestReloadUserOrderPositionsIfNeeded_ReloadsWhenIntervalPassed tests that
// reload happens when the sync interval has passed.
func TestReloadUserOrderPositionsIfNeeded_ReloadsWhenIntervalPassed(t *testing.T) {
	dir := setupTestDir(t)

	now := time.Now()
	writeCSV(t, dir, "user_order_positions.csv", []interface{}{
		&order.UserOrderPosition{
			ID:             1,
			UserID:         1,
			UserOrderID:    10,
			UserStrategyID: 100,
			Exchange:       "binance",
			PosType:        order.PosTypeFutures,
			Asset:          "NEARUSDT",
			Side:           order.SideLong,
			Quantity:       1.5,
			PosPrice:       5.0,
			CurrentPrice:   5.5,
			Leverage:       5,
			Deleted:        0,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	})

	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	repo := NewStateRepository(gs)
	repo.SetSyncInterval(2 * time.Second) // Short interval for test

	// First call - should reload
	if err := repo.ReloadUserOrderPositionsIfNeeded(); err != nil {
		t.Fatal(err)
	}

	// Update CSV externally - set deleted=1
	later := now.Add(time.Second)
	writeCSV(t, dir, "user_order_positions.csv", []interface{}{
		&order.UserOrderPosition{
			ID:             1,
			UserID:         1,
			UserOrderID:    10,
			UserStrategyID: 100,
			Exchange:       "binance",
			PosType:        order.PosTypeFutures,
			Asset:          "NEARUSDT",
			Side:           order.SideLong,
			Quantity:       1.5,
			PosPrice:       5.0,
			CurrentPrice:   5.5,
			Leverage:       5,
			Deleted:        1,
			CreatedAt:      now,
			UpdatedAt:      later,
		},
	})

	// Wait for interval to pass
	time.Sleep(3 * time.Second)

	// Third call - should reload because interval passed
	if err := repo.ReloadUserOrderPositionsIfNeeded(); err != nil {
		t.Fatal(err)
	}

	// Position should now have updated value (deleted=1)
	pos, err := repo.GetUserOrderPositionByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if pos.Deleted != 1 {
		t.Errorf("expected deleted=1 (reloaded), got %d", pos.Deleted)
	}
}

// TestReloadUserOrderPositionsIfNeeded_ConcurrentCalls tests that
// concurrent calls don't cause duplicate reloads.
func TestReloadUserOrderPositionsIfNeeded_ConcurrentCalls(t *testing.T) {
	dir := setupTestDir(t)

	now := time.Now()
	writeCSV(t, dir, "user_order_positions.csv", []interface{}{
		&order.UserOrderPosition{
			ID:             1,
			UserID:         1,
			UserOrderID:    10,
			UserStrategyID: 100,
			Exchange:       "binance",
			PosType:        order.PosTypeFutures,
			Asset:          "NEARUSDT",
			Side:           order.SideLong,
			Quantity:       1.5,
			Deleted:        0,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	})

	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	repo := NewStateRepository(gs)
	repo.SetSyncInterval(5 * time.Second)

	// Reset last sync time to force reload
	repo.resetLastSyncTime()

	// Simulate 10 concurrent calls (like dense signals)
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			done <- repo.ReloadUserOrderPositionsIfNeeded()
		}()
	}

	// Wait for all calls to complete
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent call %d failed: %v", i, err)
		}
	}

	// Position should be loaded correctly
	pos, err := repo.GetUserOrderPositionByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if pos == nil {
		t.Fatal("position not loaded")
	}
}
