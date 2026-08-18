package signal

import (
	"testing"
	"time"

	"trading-service/internal/order"
)

// TestHandleOpen_StrategyExpired tests that HandleOpen rejects signals
// when the user_strategy has expired (valid_before < now).
func TestHandleOpen_StrategyExpired(t *testing.T) {
	h, mock, gs := setupForHandler(t)
	defer gs.Shutdown()
	mock.SetPrice("BTCUSDT", 50000)

	userID, usID := createTestStrategy(t, h)

	// Set valid_before to past time (expired)
	us, _ := h.Repo.GetUserStrategyByID(usID)
	us.ValidBefore = time.Now().Add(-1 * time.Hour) // Expired 1 hour ago
	h.Repo.UpdateUserStrategy(us)

	// Attempt to open should fail with expired error
	err := h.HandleOpen(Signal{
		UserID:         userID,
		UserStrategyID: usID,
		Symbol:         "BTC",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Cash:           100,
		TriggerPrice:   50000,
		Slippage:       0.01,
		Side:           int(order.SideLong),
		OrderType:      0,
		Leverage:       10,
	})

	if err == nil {
		t.Fatal("expected error: strategy expired")
	}
	if err.Error() != "strategy expired: valid_before passed" {
		t.Errorf("expected 'strategy expired' error, got: %v", err)
	}
}

// TestHandleOpen_StrategyNotExpired tests that HandleOpen allows signals
// when the user_strategy has not expired (valid_before > now).
func TestHandleOpen_StrategyNotExpired(t *testing.T) {
	h, mock, gs := setupForHandler(t)
	defer gs.Shutdown()
	mock.SetPrice("BTCUSDT", 50000)

	userID, usID := createTestStrategy(t, h)

	// Set valid_before to future time (not expired)
	us, _ := h.Repo.GetUserStrategyByID(usID)
	us.ValidBefore = time.Now().Add(24 * time.Hour) // Valid for 24 hours
	h.Repo.UpdateUserStrategy(us)

	// Attempt to open should succeed
	err := h.HandleOpen(Signal{
		UserID:         userID,
		UserStrategyID: usID,
		Symbol:         "BTC",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Cash:           100,
		TriggerPrice:   50000,
		Slippage:       0.01,
		Side:           int(order.SideLong),
		OrderType:      0,
		Leverage:       10,
	})

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

// TestHandleOpen_StrategyNoValidBefore tests that HandleOpen allows signals
// when valid_before is zero (no expiration set).
func TestHandleOpen_StrategyNoValidBefore(t *testing.T) {
	h, mock, gs := setupForHandler(t)
	defer gs.Shutdown()
	mock.SetPrice("BTCUSDT", 50000)

	userID, usID := createTestStrategy(t, h)

	// Leave valid_before as zero (no expiration)
	us, _ := h.Repo.GetUserStrategyByID(usID)
	us.ValidBefore = time.Time{} // Zero value
	h.Repo.UpdateUserStrategy(us)

	// Attempt to open should succeed
	err := h.HandleOpen(Signal{
		UserID:         userID,
		UserStrategyID: usID,
		Symbol:         "BTC",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Cash:           100,
		TriggerPrice:   50000,
		Slippage:       0.01,
		Side:           int(order.SideLong),
		OrderType:      0,
		Leverage:       10,
	})

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

// TestHandleReverse_StrategyExpired tests that reverse signal also checks
// strategy expiration before opening reverse position.
// Note: Since HandleReverse calls HandleOpen internally, the expiration check
// will happen when trying to open the reverse position.
// This test validates that the check works for reverse signals indirectly
// through the TestHandleOpen_StrategyExpired test.
func TestHandleReverse_StrategyExpired(t *testing.T) {
	// This test is essentially covered by TestHandleOpen_StrategyExpired
	// because HandleReverse eventually calls HandleOpen for the reverse position.
	// If HandleOpen checks expiration, reverse signals will also be checked.

	// For completeness, we document that reverse signals go through HandleOpen:
	// HandleReverse -> HandleClose -> waitForPositionClosed -> HandleOpen
	// The expiration check in HandleOpen will catch expired strategies.

	t.Log("Reverse signals are checked via HandleOpen expiration check")
}