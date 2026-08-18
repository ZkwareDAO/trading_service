package aggregator

import (
	"math"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

func setupPersistenceAggregatorTest(t *testing.T) (*persistence.GlobalState, *persistence.StateRepository) {
	t.Helper()
	gs, err := persistence.NewGlobalState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return gs, persistence.NewStateRepository(gs)
}

func assertNear(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %.12f want %.12f", got, want)
	}
}

func TestAggregateFromPersistenceWithMetrics_SkipsPositionWhenPriceZero(t *testing.T) {
	gs, repo := setupPersistenceAggregatorTest(t)
	defer gs.Shutdown()
	now := time.Now()
	// SHORT position with CurrentPrice=0 and no WS price available
	// This simulates service restart before WS connects
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, UserStrategyID: 100, Exchange: "deribit", PosType: order.PosTypeOptions,
		Asset: "BTC-31JUL26-66000-P", Side: order.SideShort, Quantity: 0.4, PosPrice: 0.1,
		CurrentPrice: 0, InitMargin: 0.04, Leverage: 1, Deleted: 0,
		CreatedAt: now, UpdatedAt: now,
	})

	// No WS prices available (empty map simulates WS not connected)
	metrics := AggregateFromPersistenceWithMetrics(repo, map[string]map[string]float64{})
	if len(metrics) != 0 {
		t.Errorf("expected 0 metrics when price=0 (should skip position), got %d", len(metrics))
	}
}

func TestAggregateFromPersistenceWithMetrics_SkipsPositionWhenWSPriceMissing(t *testing.T) {
	gs, repo := setupPersistenceAggregatorTest(t)
	defer gs.Shutdown()
	now := time.Now()
	// Position with CurrentPrice=0 and WS prices exist but don't contain this asset
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, UserStrategyID: 100, Exchange: "deribit", PosType: order.PosTypeOptions,
		Asset: "BTC-31JUL26-66000-P", Side: order.SideShort, Quantity: 0.4, PosPrice: 0.1,
		CurrentPrice: 0, InitMargin: 0.04, Leverage: 1, Deleted: 0,
		CreatedAt: now, UpdatedAt: now,
	})

	// WS prices exist for deribit but not for this specific option
	metrics := AggregateFromPersistenceWithMetrics(repo, map[string]map[string]float64{"deribit": {"BTC-PERPETUAL": 65000}})
	if len(metrics) != 0 {
		t.Errorf("expected 0 metrics when no price available for asset, got %d", len(metrics))
	}
}

func TestAggregateFromPersistenceWithMetrics_IncludesPositionWhenPriceAvailable(t *testing.T) {
	gs, repo := setupPersistenceAggregatorTest(t)
	defer gs.Shutdown()
	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, UserStrategyID: 100, Exchange: "deribit", PosType: order.PosTypeOptions,
		Asset: "BTC-31JUL26-66000-P", Side: order.SideShort, Quantity: 0.4, PosPrice: 0.1,
		CurrentPrice: 0, InitMargin: 0.04, Leverage: 1, Deleted: 0,
		CreatedAt: now, UpdatedAt: now,
	})

	// WS price available for this option
	metrics := AggregateFromPersistenceWithMetrics(repo, map[string]map[string]float64{"deribit": {"BTC-31JUL26-66000-P": 0.096}})
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric when price available, got %d", len(metrics))
	}
	// SHORT: PnL = (posPrice - currentPrice) * qty = (0.1 - 0.096) * 0.4 = 0.0016
	assertNear(t, metrics[0].PnL, 0.0016)
}

func TestUserPositionSyncer_DoesNotUpdateWhenPriceZero(t *testing.T) {
	gs, repo := setupPersistenceAggregatorTest(t)
	defer gs.Shutdown()
	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, UserStrategyID: 100, Exchange: "deribit", PosType: order.PosTypeOptions,
		Asset: "BTC-31JUL26-66000-P", Side: order.SideShort, Quantity: 0.4, PosPrice: 0.1,
		CurrentPrice: 0, InitMargin: 0.04, Leverage: 1, Deleted: 0,
		CreatedAt: now, UpdatedAt: now,
	})
	userPositionID := repo.CreateUserPosition(&order.UserPosition{
		UserID: 1, UserStrategyID: 100, Exchange: "deribit", PosType: order.PosTypeOptions,
		Quantity: 0.4, TotalMargin: 0.04, PnL: 0, ROI: 0,
		MaxProfitPercentage: 0.5, MaxLossPercentage: -0.1,
		Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})

	// No WS prices - should NOT update user_position
	if err := UserPositionSyncer(repo, repo, map[string]map[string]float64{}); err != nil {
		t.Fatal(err)
	}

	updated, err := repo.GetUserPositionByID(userPositionID)
	if err != nil {
		t.Fatal(err)
	}
	// Should preserve original values, not overwrite with ROI=1.0
	assertNear(t, updated.ROI, 0)
	assertNear(t, updated.PnL, 0)
	assertNear(t, updated.MaxProfitPercentage, 0.5)
	assertNear(t, updated.MaxLossPercentage, -0.1)
}

func TestAggregateFromPersistenceWithMetrics_ShortUsesDirectionAwarePnL(t *testing.T) {
	gs, repo := setupPersistenceAggregatorTest(t)
	defer gs.Shutdown()
	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, UserStrategyID: 100, Exchange: "hyperliquid", PosType: order.PosTypeFutures,
		Asset: "NEARUSDC", Side: order.SideShort, Quantity: 100, PosPrice: 2.0,
		CurrentPrice: 2.0, InitMargin: 100, Leverage: 5, Deleted: 0,
		CreatedAt: now, UpdatedAt: now,
	})

	metrics := AggregateFromPersistenceWithMetrics(repo, map[string]map[string]float64{"hyperliquid": {"NEAR": 1.8}})
	if len(metrics) != 1 {
		t.Fatalf("expected one metric result, got %d", len(metrics))
	}
	assertNear(t, metrics[0].PnL, 20)
	assertNear(t, metrics[0].ROI, 1.0)
}

func TestUserPositionSyncer_UpdatesRuntimeMetricsInMemory(t *testing.T) {
	gs, repo := setupPersistenceAggregatorTest(t)
	defer gs.Shutdown()
	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, UserStrategyID: 100, Exchange: "hyperliquid", PosType: order.PosTypeFutures,
		Asset: "NEARUSDC", Side: order.SideLong, Quantity: 100, PosPrice: 1.0,
		CurrentPrice: 1.0, InitMargin: 100, Leverage: 5, Deleted: 0,
		CreatedAt: now, UpdatedAt: now,
	})
	userPositionID := repo.CreateUserPosition(&order.UserPosition{
		UserID: 1, UserStrategyID: 100, Exchange: "hyperliquid", PosType: order.PosTypeFutures,
		Quantity: 100, TotalMargin: 100, MaxProfitPercentage: 2.0, MaxLossPercentage: -0.2,
		Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})

	if err := UserPositionSyncer(repo, repo, map[string]map[string]float64{"hyperliquid": {"NEAR": 1.2}}); err != nil {
		t.Fatal(err)
	}

	updated, err := repo.GetUserPositionByID(userPositionID)
	if err != nil {
		t.Fatal(err)
	}
	assertNear(t, updated.PnL, 20)
	assertNear(t, updated.ROI, 1.0)
	assertNear(t, updated.MaxProfitPercentage, 2.0)
	assertNear(t, updated.MaxLossPercentage, -0.2)
}

func TestUserPositionSyncer_UpdatesMaxLossInMemory(t *testing.T) {
	gs, repo := setupPersistenceAggregatorTest(t)
	defer gs.Shutdown()
	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: 1, UserStrategyID: 100, Exchange: "hyperliquid", PosType: order.PosTypeFutures,
		Asset: "NEARUSDC", Side: order.SideLong, Quantity: 100, PosPrice: 1.0,
		CurrentPrice: 1.0, InitMargin: 100, Leverage: 5, Deleted: 0,
		CreatedAt: now, UpdatedAt: now,
	})
	userPositionID := repo.CreateUserPosition(&order.UserPosition{
		UserID: 1, UserStrategyID: 100, Exchange: "hyperliquid", PosType: order.PosTypeFutures,
		Quantity: 100, TotalMargin: 100, MaxLossPercentage: -0.2,
		Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})

	if err := UserPositionSyncer(repo, repo, map[string]map[string]float64{"hyperliquid": {"NEAR": 0.9}}); err != nil {
		t.Fatal(err)
	}

	updated, err := repo.GetUserPositionByID(userPositionID)
	if err != nil {
		t.Fatal(err)
	}
	assertNear(t, updated.PnL, -10)
	assertNear(t, updated.ROI, -0.5)
	assertNear(t, updated.MaxLossPercentage, -0.5)
}
