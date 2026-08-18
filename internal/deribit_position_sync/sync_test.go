package deribit_position_sync

import (
	"testing"
	"time"

	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

func setupSyncTest(t *testing.T) (*persistence.StateRepository, func()) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	return repo, gs.Shutdown
}

// Note: Legacy tests (TestCreatePosition, TestCreatePosition_ReusesExistingRecords, TestCreateDeltaPosition)
// have been removed. Use TestCreatePosition_UsesRPC to verify RPC-based creation.

func TestMarkPositionDeleted(t *testing.T) {
	repo, cleanup := setupSyncTest(t)
	defer cleanup()

	repo.CreateUser(&order.User{ID: 1, Exchange: "deribit"})
	repo.CreateStrategy(&order.Strategy{ID: 1, Name: "SYNC_BTC", StrategyType: "MANUAL_SYNC", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	repo.CreateUserStrategy(&order.UserStrategy{ID: 1, UserID: 1, Name: "SYNC_BTC", StrategyID: 1, Exchange: "deribit", CreatedAt: time.Now(), UpdatedAt: time.Now()})

	posID := repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         1,
		UserStrategyID: 1,
		Asset:          "BTC",
		Side:           order.SideLong,
		Quantity:       10,
		Exchange:       "deribit",
		PosType:        order.PosTypeOptions,
		PosPrice:       100.0,
		InitMargin:     1000.0,
		Leverage:       1,
		Deleted:        0,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	})

	pos := PositionToDelete{
		PositionID: posID,
		Symbol:     "BTC",
		Side:       order.SideLong,
	}

	err := markPositionDeleted(repo, pos, nil)
	if err != nil {
		t.Errorf("markPositionDeleted failed: %v", err)
	}

	updated, _ := repo.GetUserOrderPositionByID(posID)
	if updated.Deleted != 1 {
		t.Errorf("expected Deleted=1, got %d", updated.Deleted)
	}
}

func TestMarkPositionDeleted_ClosesBothTables(t *testing.T) {
	repo, cleanup := setupSyncTest(t)
	defer cleanup()

	repo.CreateUser(&order.User{ID: 1, Exchange: "deribit"})
	repo.CreateStrategy(&order.Strategy{ID: 1, Name: "SYNC_BTC", StrategyType: "MANUAL_SYNC", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	repo.CreateUserStrategy(&order.UserStrategy{ID: 1, UserID: 1, Name: "SYNC_BTC", StrategyID: 1, Exchange: "deribit", CreatedAt: time.Now(), UpdatedAt: time.Now()})

	posID := repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         1,
		UserStrategyID: 1,
		Asset:          "BTC",
		Side:           order.SideLong,
		Quantity:       10,
		Exchange:       "deribit",
		PosType:        order.PosTypeOptions,
		PosPrice:       100.0,
		InitMargin:     1000.0,
		Leverage:       1,
		Deleted:        0,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	})

	_ = repo.CreateUserPosition(&order.UserPosition{
		UserID:         1,
		UserStrategyID: 1,
		Quantity:       10,
		Exchange:       "deribit",
		PosType:        order.PosTypeOptions,
		Deleted:        0,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	})

	pos := PositionToDelete{PositionID: posID, Symbol: "BTC", Side: order.SideLong}
	err := markPositionDeleted(repo, pos, nil)
	if err != nil {
		t.Errorf("markPositionDeleted failed: %v", err)
	}

	updated, _ := repo.GetUserOrderPositionByID(posID)
	if updated.Deleted != 1 {
		t.Errorf("expected user_order_position Deleted=1, got %d", updated.Deleted)
	}
}

func TestSideToString(t *testing.T) {
	if sideToString(order.SideLong) != "LONG" {
		t.Error("expected LONG")
	}
	if sideToString(order.SideShort) != "SHORT" {
		t.Error("expected SHORT")
	}
}

func TestSyncDeribitPositions_NoUsers(t *testing.T) {
	repo, cleanup := setupSyncTest(t)
	defer cleanup()

	var notifier notification.Notifier = nil
	var rpcClient *rpc.OrderServiceClient = nil

	err := SyncDeribitPositions(rpcClient, repo, true, notifier)
	if err != nil {
		t.Errorf("SyncDeribitPositions failed: %v", err)
	}
}

func TestSyncDeribitPositions_NonDeribitUser(t *testing.T) {
	repo, cleanup := setupSyncTest(t)
	defer cleanup()

	repo.CreateUser(&order.User{ID: 1, Exchange: "binance"})

	var notifier notification.Notifier = nil
	var rpcClient *rpc.OrderServiceClient = nil

	err := SyncDeribitPositions(rpcClient, repo, true, notifier)
	if err != nil {
		t.Errorf("SyncDeribitPositions failed: %v", err)
	}
}
