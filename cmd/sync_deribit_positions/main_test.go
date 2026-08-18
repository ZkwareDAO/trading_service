package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestState creates a GlobalState with proper CSV structure for testing
func setupTestState(t *testing.T) (*persistence.GlobalState, *persistence.StateRepository) {
	dir := t.TempDir()
	// Create .compact directory required by DualPersister
	if err := os.MkdirAll(filepath.Join(dir, ".compact"), 0755); err != nil {
		t.Fatal(err)
	}
	// Create minimal CSV files to avoid load errors
	csvFiles := []string{
		"users.csv",
		"strategies.csv",
		"strategy_assets.csv",
		"user_strategies.csv",
		"user_orders.csv",
		"leverage_configs.csv",
		"exchange_symbol_filters.csv",
		"uprunning_orders.csv",
		"user_order_positions.csv",
		"user_positions.csv",
	}
	for _, file := range csvFiles {
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}

	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	repo := persistence.NewStateRepository(gs)
	return gs, repo
}

// =============================================================================
// User Journey: As an admin, I want to sync Deribit positions to the system
// =============================================================================

func TestSyncDeribitPositions_SkipsExistingPositions(t *testing.T) {
	// Setup: Create in-memory state with existing position
	gs, repo := setupTestState(t)
	defer gs.Shutdown()

	// Create user
	user := &order.User{
		Name:      "test_deribit",
		Exchange:  "deribit",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	user.ID = repo.CreateUser(user)

	// Create existing position: ETH-25SEP26-1900-P LONG 0.3
	strategyID := repo.CreateStrategy(&order.Strategy{
		Name:         "EXISTING_ETH",
		StrategyType: "MANUAL",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     user.ID,
		Name:       "EXISTING_ETH",
		Exchange:   "deribit",
		StrategyID: strategyID,
		Status:     1,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         user.ID,
		UserStrategyID: usID,
		Asset:          "ETH-25SEP26-1900-P",
		Side:           order.SideLong,
		Quantity:       0.3,
		Exchange:       "deribit",
		PosType:        order.PosTypeOptions,
		Deleted:        0,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	})

	// Mock exchange positions: ETH-25SEP26-1900-P LONG 0.3 (same as existing)
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "ETH-25SEP26-1900-P",
			PositionSide: exchange.PositionSideLong,
			Quantity:     0.3,
			EntryPrice:   0.002,
		},
	}

	// Execute sync
	toSync := filterPositionsToSync(repo, user.ID, exchangePositions)

	// Verify: Should skip existing position
	assert.Empty(t, toSync, "should skip position with matching symbol, side, and quantity")
}

func TestSyncDeribitPositions_SyncsNewPosition(t *testing.T) {
	// Setup
	gs, repo := setupTestState(t)
	defer gs.Shutdown()

	user := &order.User{
		Name:      "test_deribit",
		Exchange:  "deribit",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	user.ID = repo.CreateUser(user)

	// Mock exchange positions: new position not in system
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "BTC-24JUL26-64000-P",
			PositionSide: exchange.PositionSideShort,
			Quantity:     0.1,
			EntryPrice:   0.015,
		},
	}

	// Execute sync
	toSync := filterPositionsToSync(repo, user.ID, exchangePositions)

	// Verify: Should detect new position
	require.Len(t, toSync, 1)
	assert.Equal(t, "BTC-24JUL26-64000-P", toSync[0].Symbol)
	assert.Equal(t, exchange.PositionSideShort, toSync[0].PositionSide)
	assert.Equal(t, 0.1, toSync[0].Quantity)
}

func TestSyncDeribitPositions_AggregatesLocalPositions(t *testing.T) {
	// Setup: Multiple local positions for same symbol+side
	gs, repo := setupTestState(t)
	defer gs.Shutdown()

	user := &order.User{
		Name:      "test_deribit",
		Exchange:  "deribit",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	user.ID = repo.CreateUser(user)

	strategyID := repo.CreateStrategy(&order.Strategy{
		Name:         "EXISTING",
		StrategyType: "MANUAL",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     user.ID,
		Name:       "EXISTING",
		Exchange:   "deribit",
		StrategyID: strategyID,
		Status:     1,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	})

	// Create multiple positions for same symbol+side (simulating multiple orders)
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         user.ID,
		UserStrategyID: usID,
		Asset:          "ETH-25SEP26-1900-P",
		Side:           order.SideLong,
		Quantity:       0.1,
		Exchange:       "deribit",
		PosType:        order.PosTypeOptions,
		Deleted:        0,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         user.ID,
		UserStrategyID: usID,
		Asset:          "ETH-25SEP26-1900-P",
		Side:           order.SideLong,
		Quantity:       0.2,
		Exchange:       "deribit",
		PosType:        order.PosTypeOptions,
		Deleted:        0,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	})

	// Mock exchange: total 0.3 (0.1 + 0.2)
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "ETH-25SEP26-1900-P",
			PositionSide: exchange.PositionSideLong,
			Quantity:     0.3,
			EntryPrice:   0.002,
		},
	}

	// Execute
	toSync := filterPositionsToSync(repo, user.ID, exchangePositions)

	// Verify: Should skip (0.1+0.2 = 0.3 matches exchange)
	assert.Empty(t, toSync, "should skip when aggregated local quantity matches exchange")
}

func TestSyncDeribitPositions_DetectsQuantityMismatch(t *testing.T) {
	// Setup
	gs, repo := setupTestState(t)
	defer gs.Shutdown()

	user := &order.User{
		Name:      "test_deribit",
		Exchange:  "deribit",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	user.ID = repo.CreateUser(user)

	strategyID := repo.CreateStrategy(&order.Strategy{
		Name:         "EXISTING",
		StrategyType: "MANUAL",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     user.ID,
		Name:       "EXISTING",
		Exchange:   "deribit",
		StrategyID: strategyID,
		Status:     1,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	})

	// Local: 0.3
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         user.ID,
		UserStrategyID: usID,
		Asset:          "ETH-25SEP26-1900-P",
		Side:           order.SideLong,
		Quantity:       0.3,
		Exchange:       "deribit",
		PosType:        order.PosTypeOptions,
		Deleted:        0,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	})

	// Exchange: 0.5 (different from local 0.3)
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "ETH-25SEP26-1900-P",
			PositionSide: exchange.PositionSideLong,
			Quantity:     0.5,
			EntryPrice:   0.002,
		},
	}

	// Execute
	toSync := filterPositionsToSync(repo, user.ID, exchangePositions)

	// Verify: Should detect mismatch
	require.Len(t, toSync, 1)
	assert.Equal(t, 0.5, toSync[0].Quantity)
}

func TestSyncDeribitPositions_IgnoresDeletedPositions(t *testing.T) {
	// Setup
	gs, repo := setupTestState(t)
	defer gs.Shutdown()

	user := &order.User{
		Name:      "test_deribit",
		Exchange:  "deribit",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	user.ID = repo.CreateUser(user)

	strategyID := repo.CreateStrategy(&order.Strategy{
		Name:         "DELETED",
		StrategyType: "MANUAL",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     user.ID,
		Name:       "DELETED",
		Exchange:   "deribit",
		StrategyID: strategyID,
		Status:     1,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	})

	// Create DELETED position
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         user.ID,
		UserStrategyID: usID,
		Asset:          "ETH-25SEP26-1900-P",
		Side:           order.SideLong,
		Quantity:       0.3,
		Exchange:       "deribit",
		PosType:        order.PosTypeOptions,
		Deleted:        1, // DELETED
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	})

	// Exchange has same position
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "ETH-25SEP26-1900-P",
			PositionSide: exchange.PositionSideLong,
			Quantity:     0.3,
			EntryPrice:   0.002,
		},
	}

	// Execute
	toSync := filterPositionsToSync(repo, user.ID, exchangePositions)

	// Verify: Should sync (deleted position is ignored)
	require.Len(t, toSync, 1, "should sync when only deleted position exists")
}

// TestSyncDeribitPositions_CompleteFields verifies that synced positions have all required fields.
func TestSyncDeribitPositions_CompleteFields(t *testing.T) {
	// Setup
	gs, repo := setupTestState(t)
	defer gs.Shutdown()

	user := &order.User{
		Name:      "test_deribit",
		Exchange:  "deribit",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	user.ID = repo.CreateUser(user)

	// Mock exchange position with entry price
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "BTC-24JUL26-64000-P",
			PositionSide: exchange.PositionSideLong,
			Quantity:     0.1,
			EntryPrice:   0.015, // Entry price from exchange
		},
	}

	// Execute sync
	toSync := filterPositionsToSync(repo, user.ID, exchangePositions)
	require.Len(t, toSync, 1)

	// Sync the position
	err := syncPosition(repo, user.ID, toSync[0])
	require.NoError(t, err)

	// Verify UserStrategy has all required fields
	strategies := repo.ListUserStrategiesByUser(user.ID)
	require.Len(t, strategies, 1)
	us := strategies[0]

	// Check UserStrategy fields
	assert.Equal(t, "SYNC_BTC-24JUL26-64000-P", us.Name)
	assert.Equal(t, "deribit", us.Exchange)
	assert.Equal(t, 1000.0, us.Cash, "Cash should be 1000")
	assert.Equal(t, 3, us.Parts, "Parts should be 3")
	assert.Equal(t, 1, us.Status, "Status should be 1")
	assert.Equal(t, order.RiskStrategyTypeTraditional, us.RiskStrategyType, "RiskStrategyType should be traditional")
	assert.Equal(t, 0, us.OrdersNum, "OrdersNum should be 0")
	assert.False(t, us.ValidBefore.IsZero(), "ValidBefore should be set")

	// Verify StrategyAsset was created
	// Note: StrategyAsset is linked to Strategy, not UserStrategy
	// We need to verify through Strategy
	strategy, err := repo.GetStrategyByID(us.StrategyID)
	require.NoError(t, err)
	assert.Equal(t, "SYNC_BTC-24JUL26-64000-P", strategy.Name)

	// Verify UserOrderPosition has entry price
	positions := repo.ListUserOrderPositionsByFilter(persistence.UserOrderPositionFilter{
		UserID: user.ID,
	})
	require.Len(t, positions, 1)
	pos := positions[0]

	assert.Equal(t, "BTC-24JUL26-64000-P", pos.Asset)
	assert.Equal(t, 0.1, pos.Quantity)
	assert.Equal(t, 0.015, pos.PosPrice, "PosPrice should equal EntryPrice from exchange")
}
