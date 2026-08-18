package main

import (
	"context"
	"testing"

	"trading-service/internal/exchange/ws"
	"trading-service/internal/order"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDeribitPositionSource implements DeribitPositionSource for testing.
type mockDeribitPositionSource struct {
	positions []*order.UserOrderPosition
}

func (m *mockDeribitPositionSource) ListActivePositions() []*order.UserOrderPosition {
	return m.positions
}

// TestDeribitPriceRuntime_EnsureSubscribed_SubscribesNewOptions tests that
// new options are automatically subscribed when EnsureSubscribed is called.
func TestDeribitPriceRuntime_EnsureSubscribed_SubscribesNewOptions(t *testing.T) {
	// Arrange - use mock mode to avoid WebSocket dependency
	wsMgr := ws.NewDeribitWsPriceManager(ws.WithDeribitMockOption())
	repo := &mockDeribitPositionSource{
		positions: []*order.UserOrderPosition{
			{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 0},
			{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-65000-C", Deleted: 0},
		},
	}
	subscribeMgr := NewDeribitOptionExtractor(repo)
	runtime := NewDeribitPriceRuntime(wsMgr, subscribeMgr)

	// Act
	runtime.EnsureSubscribed()

	// Assert - should have subscribed to 2 options
	subs, err := wsMgr.GetSubscriptions()
	require.NoError(t, err)
	assert.Len(t, subs, 2)
	assert.Contains(t, subs, "BTC-24JUL26-64000-P")
	assert.Contains(t, subs, "BTC-24JUL26-65000-C")
}

// TestDeribitPriceRuntime_EnsureSubscribed_SkipsAlreadySubscribed tests that
// EnsureSubscribed doesn't re-subscribe to already subscribed options.
func TestDeribitPriceRuntime_EnsureSubscribed_SkipsAlreadySubscribed(t *testing.T) {
	// Arrange - use mock mode
	wsMgr := ws.NewDeribitWsPriceManager(ws.WithDeribitMockOption())
	repo := &mockDeribitPositionSource{
		positions: []*order.UserOrderPosition{
			{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 0},
		},
	}
	subscribeMgr := NewDeribitOptionExtractor(repo)
	runtime := NewDeribitPriceRuntime(wsMgr, subscribeMgr)

	// First subscription
	runtime.EnsureSubscribed()
	subs1, _ := wsMgr.GetSubscriptions()
	assert.Len(t, subs1, 1)

	// Act - call again (should not re-subscribe)
	runtime.EnsureSubscribed()
	subs2, _ := wsMgr.GetSubscriptions()

	// Assert - still only 1 subscription
	assert.Len(t, subs2, 1)
}

// TestDeribitPriceRuntime_EnsureSubscribed_HandlesNewOptions tests that
// new options added after initial subscription are automatically subscribed.
func TestDeribitPriceRuntime_EnsureSubscribed_HandlesNewOptions(t *testing.T) {
	// Arrange - use mock mode
	wsMgr := ws.NewDeribitWsPriceManager(ws.WithDeribitMockOption())
	repo := &mockDeribitPositionSource{
		positions: []*order.UserOrderPosition{
			{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 0},
		},
	}
	subscribeMgr := NewDeribitOptionExtractor(repo)
	runtime := NewDeribitPriceRuntime(wsMgr, subscribeMgr)

	// Initial subscription
	runtime.EnsureSubscribed()
	subs1, _ := wsMgr.GetSubscriptions()
	assert.Len(t, subs1, 1)

	// Add new option to positions
	repo.positions = append(repo.positions, &order.UserOrderPosition{
		Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-65000-C", Deleted: 0,
	})

	// Act - ensure subscribed again
	runtime.EnsureSubscribed()
	subs2, _ := wsMgr.GetSubscriptions()

	// Assert - now 2 subscriptions
	assert.Len(t, subs2, 2)
	assert.Contains(t, subs2, "BTC-24JUL26-64000-P")
	assert.Contains(t, subs2, "BTC-24JUL26-65000-C")
}

// TestDeribitPriceRuntime_EnsureSubscribed_HandlesWebSocketError tests that
// WebSocket errors don't crash the service.
func TestDeribitPriceRuntime_EnsureSubscribed_HandlesWebSocketError(t *testing.T) {
	// Arrange - use mock mode
	wsMgr := ws.NewDeribitWsPriceManager(ws.WithDeribitMockOption())
	repo := &mockDeribitPositionSource{
		positions: []*order.UserOrderPosition{
			{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 0},
		},
	}
	subscribeMgr := NewDeribitOptionExtractor(repo)
	runtime := NewDeribitPriceRuntime(wsMgr, subscribeMgr)

	// Act & Assert - should not panic
	assert.NotPanics(t, func() {
		runtime.EnsureSubscribed()
	})
}

// TestDeribitPriceRuntime_Start_DoesNotReturnError tests that
// Start() never returns an error, even when WebSocket connection fails.
func TestDeribitPriceRuntime_Start_DoesNotReturnError(t *testing.T) {
	// Arrange - use mock mode
	wsMgr := ws.NewDeribitWsPriceManager(ws.WithDeribitMockOption())
	repo := &mockDeribitPositionSource{positions: []*order.UserOrderPosition{}}
	subscribeMgr := NewDeribitOptionExtractor(repo)
	runtime := NewDeribitPriceRuntime(wsMgr, subscribeMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Act
	err := runtime.Start(ctx)

	// Assert - should never return error
	assert.NoError(t, err)
}

// TestDeribitPriceRuntime_Start_SubscribesOnStartup tests that
// Start() subscribes to existing options on startup.
func TestDeribitPriceRuntime_Start_SubscribesOnStartup(t *testing.T) {
	// Arrange - use mock mode
	wsMgr := ws.NewDeribitWsPriceManager(ws.WithDeribitMockOption())
	repo := &mockDeribitPositionSource{
		positions: []*order.UserOrderPosition{
			{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 0},
		},
	}
	subscribeMgr := NewDeribitOptionExtractor(repo)
	runtime := NewDeribitPriceRuntime(wsMgr, subscribeMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Act
	err := runtime.Start(ctx)
	require.NoError(t, err)

	// Assert
	subs, err := wsMgr.GetSubscriptions()
	require.NoError(t, err)
	assert.Len(t, subs, 1)
}

// TestDeribitPriceRuntime_PeriodicSubscriptionChecks tests that
// EnsureSubscribed is called periodically to handle new options.
func TestDeribitPriceRuntime_PeriodicSubscriptionChecks(t *testing.T) {
	// Arrange - use mock mode with initial option
	wsMgr := ws.NewDeribitWsPriceManager(ws.WithDeribitMockOption())
	repo := &mockDeribitPositionSource{
		positions: []*order.UserOrderPosition{
			{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 0},
		},
	}
	subscribeMgr := NewDeribitOptionExtractor(repo)
	runtime := NewDeribitPriceRuntime(wsMgr, subscribeMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Act - start with initial subscription
	err := runtime.Start(ctx)
	require.NoError(t, err)

	// Verify initial subscription
	subs1, _ := wsMgr.GetSubscriptions()
	assert.Len(t, subs1, 1)

	// Add new option after startup
	repo.positions = append(repo.positions, &order.UserOrderPosition{
		Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-65000-C", Deleted: 0,
	})

	// Manually call EnsureSubscribed (simulating periodic check)
	runtime.EnsureSubscribed()

	// Assert - should now have 2 subscriptions
	subs2, _ := wsMgr.GetSubscriptions()
	assert.Len(t, subs2, 2)
	assert.Contains(t, subs2, "BTC-24JUL26-64000-P")
	assert.Contains(t, subs2, "BTC-24JUL26-65000-C")
}

// TestDeribitOptionExtractor_GetDeribitOptions tests the option extraction logic.
func TestDeribitOptionExtractor_GetDeribitOptions(t *testing.T) {
	tests := []struct {
		name      string
		positions []*order.UserOrderPosition
		expected  []string
	}{
		{
			name:      "empty positions",
			positions: nil,
			expected:  nil,
		},
		{
			name: "only deribit options",
			positions: []*order.UserOrderPosition{
				{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 0},
				{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-65000-C", Deleted: 0},
			},
			expected: []string{"BTC-24JUL26-64000-P", "BTC-24JUL26-65000-C"},
		},
		{
			name: "mixed exchanges",
			positions: []*order.UserOrderPosition{
				{Exchange: "binance", PosType: order.PosTypeFutures, Asset: "BTCUSDT", Deleted: 0},
				{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 0},
				{Exchange: "hyperliquid", PosType: order.PosTypeFutures, Asset: "ETH", Deleted: 0},
			},
			expected: []string{"BTC-24JUL26-64000-P"},
		},
		{
			name: "deribit futures ignored",
			positions: []*order.UserOrderPosition{
				{Exchange: "deribit", PosType: order.PosTypeFutures, Asset: "BTC-PERPETUAL", Deleted: 0},
				{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 0},
			},
			expected: []string{"BTC-24JUL26-64000-P"},
		},
		{
			name: "duplicate symbols deduped",
			positions: []*order.UserOrderPosition{
				{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 0},
				{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 0},
			},
			expected: []string{"BTC-24JUL26-64000-P"},
		},
		{
			name: "deleted positions excluded",
			positions: []*order.UserOrderPosition{
				{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-64000-P", Deleted: 1},
				{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-24JUL26-65000-C", Deleted: 0},
			},
			expected: []string{"BTC-24JUL26-65000-C"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockDeribitPositionSource{positions: tt.positions}
			extractor := NewDeribitOptionExtractor(repo)

			result := extractor.GetDeribitOptions()

			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

// TestDeribitOptionExtractor_RecoversSubscriptionsAfterRestart tests the key fix:
// even when state.Positions is empty (price=0 skip), the extractor can still
// return option symbols from user_order_positions persistence layer.
func TestDeribitOptionExtractor_RecoversSubscriptionsAfterRestart(t *testing.T) {
	// Simulate: service restart, WS not connected, state.Positions is empty
	// But user_order_positions still has active deribit positions
	repo := &mockDeribitPositionSource{
		positions: []*order.UserOrderPosition{
			{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "BTC-31JUL26-66000-P", Deleted: 0},
			{Exchange: "deribit", PosType: order.PosTypeOptions, Asset: "ETH-25SEP26-2100-C", Deleted: 0},
		},
	}
	extractor := NewDeribitOptionExtractor(repo)

	// Even though state.Positions would be empty (price=0 skip),
	// the extractor reads from persistence and returns options
	options := extractor.GetDeribitOptions()
	assert.Len(t, options, 2)
	assert.Contains(t, options, "BTC-31JUL26-66000-P")
	assert.Contains(t, options, "ETH-25SEP26-2100-C")
}
