package persistence

import (
	"testing"

	"trading-service/internal/order"
)

// TestPerTableIDCounter verifies that each CSV table maintains its own ID sequence
func TestPerTableIDCounter(t *testing.T) {
	t.Run("users and strategies have independent ID sequences", func(t *testing.T) {
		dir := t.TempDir()
		gs, err := NewGlobalState(dir)
		if err != nil {
			t.Fatalf("NewGlobalState failed: %v", err)
		}
		defer gs.Shutdown()

		repo := NewStateRepository(gs)

		// Create first user
		user1 := &order.User{Name: "user1", Exchange: "test"}
		userID1 := repo.CreateUser(user1)

		// Create first strategy
		strategy1 := &order.Strategy{Name: "strategy1", StrategyType: "test"}
		strategyID1 := repo.CreateStrategy(strategy1)

		// Create second user
		user2 := &order.User{Name: "user2", Exchange: "test"}
		userID2 := repo.CreateUser(user2)

		// Create second strategy
		strategy2 := &order.Strategy{Name: "strategy2", StrategyType: "test"}
		strategyID2 := repo.CreateStrategy(strategy2)

		// Verify users have sequential IDs
		if userID1 != 1 {
			t.Errorf("First user should have ID 1, got %d", userID1)
		}
		if userID2 != 2 {
			t.Errorf("Second user should have ID 2, got %d", userID2)
		}

		// Verify strategies have sequential IDs (independent from users)
		if strategyID1 != 1 {
			t.Errorf("First strategy should have ID 1, got %d", strategyID1)
		}
		if strategyID2 != 2 {
			t.Errorf("Second strategy should have ID 2, got %d", strategyID2)
		}

		t.Logf("User IDs: %d, %d", userID1, userID2)
		t.Logf("Strategy IDs: %d, %d", strategyID1, strategyID2)
	})

	t.Run("ID continues from max existing ID after restart", func(t *testing.T) {
		dir := t.TempDir()

		// First instance: create records
		gs1, err := NewGlobalState(dir)
		if err != nil {
			t.Fatalf("NewGlobalState failed: %v", err)
		}
		repo1 := NewStateRepository(gs1)

		user1 := &order.User{Name: "user1", Exchange: "test"}
		userID1 := repo1.CreateUser(user1)

		strategy1 := &order.Strategy{Name: "strategy1", StrategyType: "test"}
		strategyID1 := repo1.CreateStrategy(strategy1)

		// Compact to persist
		gs1.CompactAll()
		gs1.Shutdown()

		// Second instance: reload from CSV
		gs2, err := NewGlobalState(dir)
		if err != nil {
			t.Fatalf("NewGlobalState failed: %v", err)
		}
		repo2 := NewStateRepository(gs2)
		defer gs2.Shutdown()

		// Create new records - IDs should continue from max
		user2 := &order.User{Name: "user2", Exchange: "test"}
		userID2 := repo2.CreateUser(user2)

		strategy2 := &order.Strategy{Name: "strategy2", StrategyType: "test"}
		strategyID2 := repo2.CreateStrategy(strategy2)

		// Verify IDs continue from existing max
		if userID2 != userID1+1 {
			t.Errorf("User ID should be %d, got %d", userID1+1, userID2)
		}
		if strategyID2 != strategyID1+1 {
			t.Errorf("Strategy ID should be %d, got %d", strategyID1+1, strategyID2)
		}

		t.Logf("After restart: User ID %d (expected %d)", userID2, userID1+1)
		t.Logf("After restart: Strategy ID %d (expected %d)", strategyID2, strategyID1+1)
	})

	t.Run("different entity types have independent counters", func(t *testing.T) {
		dir := t.TempDir()
		gs, err := NewGlobalState(dir)
		if err != nil {
			t.Fatalf("NewGlobalState failed: %v", err)
		}
		defer gs.Shutdown()

		repo := NewStateRepository(gs)

		// Create entities of different types
		userID := repo.CreateUser(&order.User{Name: "user", Exchange: "test"})
		strategyID := repo.CreateStrategy(&order.Strategy{Name: "strategy", StrategyType: "test"})
		assetID := repo.CreateStrategyAsset(&order.StrategyAsset{Name: "asset"})
		userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{Name: "us"})

		// All should start from 1 (independent counters)
		if userID != 1 {
			t.Errorf("User ID should be 1, got %d", userID)
		}
		if strategyID != 1 {
			t.Errorf("Strategy ID should be 1, got %d", strategyID)
		}
		if assetID != 1 {
			t.Errorf("StrategyAsset ID should be 1, got %d", assetID)
		}
		if userStrategyID != 1 {
			t.Errorf("UserStrategy ID should be 1, got %d", userStrategyID)
		}

		t.Logf("All entity types start with ID 1: user=%d, strategy=%d, asset=%d, userStrategy=%d",
			userID, strategyID, assetID, userStrategyID)
	})

	t.Run("pre-existing records don't interfere with other tables", func(t *testing.T) {
		dir := t.TempDir()

		// Create a CSV with existing high ID for users
		p := NewDualPersister(dir)
		existingUser := &order.User{ID: 100, Name: "existing", Exchange: "test"}
		p.AppendRow("users.csv", existingUser)

		// Load state
		gs, err := NewGlobalState(dir)
		if err != nil {
			t.Fatalf("NewGlobalState failed: %v", err)
		}
		defer gs.Shutdown()

		repo := NewStateRepository(gs)

		// Create new user - should get ID 101
		newUser := &order.User{Name: "new_user", Exchange: "test"}
		newUserID := repo.CreateUser(newUser)

		// Create strategy - should get ID 1 (not affected by user IDs)
		newStrategy := &order.Strategy{Name: "new_strategy", StrategyType: "test"}
		newStrategyID := repo.CreateStrategy(newStrategy)

		if newUserID != 101 {
			t.Errorf("New user ID should be 101, got %d", newUserID)
		}
		if newStrategyID != 1 {
			t.Errorf("New strategy ID should be 1 (independent), got %d", newStrategyID)
		}

		t.Logf("User ID: %d (expected 101), Strategy ID: %d (expected 1)", newUserID, newStrategyID)
	})
}