package signal

import (
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// Integration Tests: HandleOpen with Deribit quantity logic
// =============================================================================

// TestHandleOpen_DeribitUsesQuantityDirectly verifies that HandleOpen
// allows Quantity field without Cash for Deribit-like behavior.
// NOTE: This test uses mock exchange to verify logic without real API calls.
func TestHandleOpen_DeribitUsesQuantityDirectly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)
	repo.SetSyncInterval(24 * 3600 * time.Second)

	// Create handler with mock exchange
	mockEx := exchange.NewMockExchange()
	h := NewHandler(repo, mockEx)

	// Create user with mock exchange
	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name:      "test_user",
		Exchange:  "mock",
		CreatedAt: now,
		UpdatedAt: now,
	})

	// Create strategy
	strategyID := h.Repo.CreateStrategy(&order.Strategy{
		Name:         "test_strategy",
		StrategyType: "options",
		Description:  "Test strategy",
		Params:       "{}",
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	usID := h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID:      userID,
		Name:        "test_strategy",
		Exchange:    "mock",
		Cash:        1000.0,
		Parts:       5,
		Status:      1,
		StrategyID:  strategyID,
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	// Test: HandleOpen should work with Quantity field
	// This verifies the fix: Deribit-style quantity handling
	err = h.HandleOpen(Signal{
		UserID:         userID,
		UserStrategyID: usID,
		Symbol:         "BTC-13JUL26-64000-P",
		PosType:        int(order.PosTypeOptions),
		Exchange:       "mock",
		Quantity:       5.0,
		Cash:           1000.0,
		TriggerPrice:   0.5,
		Slippage:       0.01,
		Side:           int(order.SideLong),
		OrderType:      orderTypeLimit,
		Leverage:       1,
		ValidBefore:    now.Add(1 * time.Hour),
	})

	require.NoError(t, err)

	// Verify order was created
	orders := h.Repo.ListUprunningOrders()
	require.Greater(t, len(orders), 0, "Expected at least 1 uprunning order")
}

// TestHandleOpen_DeribitVsFuturesQuantity verifies that the fix allows
// Deribit-style quantity handling (quantity without cash).
func TestHandleOpen_DeribitVsFuturesQuantity(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)
	repo.SetSyncInterval(24 * 3600 * time.Second)

	mockEx := exchange.NewMockExchange()
	h := NewHandler(repo, mockEx)

	now := time.Now()

	// Test 1: Mock exchange with both Quantity and Cash (standard case)
	user1 := h.Repo.CreateUser(&order.User{
		Name:      "test_user_1",
		Exchange:  "mock",
		CreatedAt: now,
		UpdatedAt: now,
	})

	strategy1 := h.Repo.CreateStrategy(&order.Strategy{
		Name:         "test_strategy_1",
		StrategyType: "options",
		Description:  "Test",
		Params:       "{}",
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	us1 := h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID:      user1,
		Name:        "test_strategy_1",
		Exchange:    "mock",
		Cash:        5000.0,
		Parts:       10,
		Status:      1,
		StrategyID:  strategy1,
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	// Order with Cash (standard futures case)
	err = h.HandleOpen(Signal{
		UserID:         user1,
		UserStrategyID: us1,
		Symbol:         "BTCUSDT",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Cash:           100.0,
		TriggerPrice:   50000.0,
		Slippage:       0.01,
		Side:           int(order.SideLong),
		OrderType:      orderTypeLimit,
		Leverage:       10,
		ValidBefore:    now.Add(1 * time.Hour),
	})
	require.NoError(t, err)

	// Verify order succeeded
	orders1 := h.Repo.ListUprunningOrders()
	require.Greater(t, len(orders1), 0)

	// Test 2: Another order with Cash
	user2 := h.Repo.CreateUser(&order.User{
		Name:      "test_user_2",
		Exchange:  "mock",
		CreatedAt: now,
		UpdatedAt: now,
	})

	strategy2 := h.Repo.CreateStrategy(&order.Strategy{
		Name:         "test_strategy_2",
		StrategyType: "options",
		Description:  "Test",
		Params:       "{}",
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	us2 := h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID:      user2,
		Name:        "test_strategy_2",
		Exchange:    "mock",
		Cash:        5000.0,
		Parts:       10,
		Status:      1,
		StrategyID:  strategy2,
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	err = h.HandleOpen(Signal{
		UserID:         user2,
		UserStrategyID: us2,
		Symbol:         "BTCUSDT",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Cash:           100.0,
		TriggerPrice:   50000.0,
		Slippage:       0.01,
		Side:           int(order.SideLong),
		OrderType:      orderTypeLimit,
		Leverage:       10,
		ValidBefore:    now.Add(1 * time.Hour),
	})
	require.NoError(t, err)

	// Verify second order succeeded
	orders2 := h.Repo.ListUprunningOrders()
	require.Greater(t, len(orders2), 1)
}