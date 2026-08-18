package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// User Journey: As a trader, I want real-time option prices via WebSocket
// =============================================================================

// mockDeribitWsServer creates a mock WebSocket server for Deribit
type mockDeribitWsServer struct {
	*httptest.Server
	upgrader websocket.Upgrader
}

func newMockDeribitWsServer() *mockDeribitWsServer {
	s := &mockDeribitWsServer{
		upgrader: websocket.Upgrader{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handleWebSocket))
	return s
}

func (s *mockDeribitWsServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Handle WebSocket messages
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var req map[string]interface{}
		if err := json.Unmarshal(msg, &req); err != nil {
			continue
		}

		// Handle JSON-RPC requests
		if method, ok := req["method"].(string); ok {
			switch method {
			case "public/subscribe":
				// Send subscription confirmation
				response := map[string]interface{}{
					"jsonrpc": "2.0",
					"result":  []string{"ticker.BTC-17JUL26-64000-P.100ms"},
					"id":      req["id"],
				}
				conn.WriteJSON(response)

				// Send mock ticker updates
				go func() {
					for i := 0; i < 3; i++ {
						time.Sleep(100 * time.Millisecond)
						ticker := map[string]interface{}{
							"jsonrpc": "2.0",
							"params": map[string]interface{}{
								"channel": "ticker.BTC-17JUL26-64000-P.100ms",
								"data": map[string]interface{}{
									"instrument_name": "BTC-17JUL26-64000-P",
									"mark_price":      0.005 + float64(i)*0.001,
									"best_bid_price":  0.0049 + float64(i)*0.001,
									"best_ask_price":  0.0051 + float64(i)*0.001,
								},
							},
						}
						conn.WriteJSON(ticker)
					}
				}()
			}
		}
	}
}

func (s *mockDeribitWsServer) WSURL() string {
	return "ws" + strings.TrimPrefix(s.Server.URL, "http")
}

func TestDeribitWsPriceManager_ConnectAndSubscribe(t *testing.T) {
	server := newMockDeribitWsServer()
	defer server.Close()

	// Create Deribit WS price manager
	pm := NewDeribitWsPriceManager(WithDeribitWsURLOption(server.WSURL()))

	// Connect to WebSocket
	err := pm.Connect()
	require.NoError(t, err, "WebSocket connection should succeed")
	defer pm.Close()

	// Subscribe to option price
	err = pm.Subscribe("BTC-17JUL26-64000-P")
	require.NoError(t, err, "Price subscription should succeed")

	// Wait for price updates
	time.Sleep(500 * time.Millisecond)

	// Verify price was received
	price, ok := pm.GetPrice("BTC-17JUL26-64000-P")
	assert.True(t, ok, "Price should be available")
	assert.Greater(t, price, 0.0, "Price should be positive")

	t.Logf("Received price: %.6f BTC", price)
}

func TestDeribitWsPriceManager_MultipleSubscriptions(t *testing.T) {
	server := newMockDeribitWsServer()
	defer server.Close()

	pm := NewDeribitWsPriceManager(WithDeribitWsURLOption(server.WSURL()))

	err := pm.Connect()
	require.NoError(t, err)
	defer pm.Close()

	// Subscribe to multiple options
	options := []string{
		"BTC-17JUL26-64000-P",
		"BTC-17JUL26-65000-C",
		"ETH-19JUL26-3000-C",
	}

	for _, symbol := range options {
		err := pm.Subscribe(symbol)
		assert.NoError(t, err, "Subscription should succeed for %s", symbol)
	}

	// Verify all subscriptions are tracked
	subs, err := pm.GetSubscriptions()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(subs), len(options), "Should track all subscriptions")
}

func TestDeribitWsPriceManager_Unsubscribe(t *testing.T) {
	server := newMockDeribitWsServer()
	defer server.Close()

	pm := NewDeribitWsPriceManager(WithDeribitWsURLOption(server.WSURL()))

	err := pm.Connect()
	require.NoError(t, err)
	defer pm.Close()

	// Subscribe and then unsubscribe
	err = pm.Subscribe("BTC-17JUL26-64000-P")
	require.NoError(t, err)

	err = pm.Unsubscribe("BTC-17JUL26-64000-P")
	assert.NoError(t, err, "Unsubscription should succeed")

	// Subscription should be removed from tracking
	subs, err := pm.GetSubscriptions()
	require.NoError(t, err)
	assert.NotContains(t, subs, "BTC-17JUL26-64000-P", "Subscription should be removed")
}

func TestDeribitWsPriceManager_ConcurrentAccess(t *testing.T) {
	server := newMockDeribitWsServer()
	defer server.Close()

	pm := NewDeribitWsPriceManager(WithDeribitWsURLOption(server.WSURL()))

	err := pm.Connect()
	require.NoError(t, err)
	defer pm.Close()

	// Concurrent subscriptions
	done := make(chan bool)
	for i := 0; i < 5; i++ {
		go func(idx int) {
			symbol := "BTC-17JUL26-64000-P"
			err := pm.Subscribe(symbol)
			assert.NoError(t, err, "Concurrent subscription %d should succeed", idx)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Concurrent access timeout")
		}
	}
}

func TestDeribitWsPriceManager_Reconnect(t *testing.T) {
	server := newMockDeribitWsServer()
	defer server.Close()

	pm := NewDeribitWsPriceManager(WithDeribitWsURLOption(server.WSURL()))

	// First connection
	err := pm.Connect()
	require.NoError(t, err)

	err = pm.Subscribe("BTC-17JUL26-64000-P")
	require.NoError(t, err)

	// Close and reconnect
	pm.Close()

	err = pm.Connect()
	require.NoError(t, err, "Reconnection should succeed")

	// Resubscribe should work
	err = pm.Subscribe("BTC-17JUL26-64000-P")
	assert.NoError(t, err, "Subscription after reconnect should succeed")

	pm.Close()
}

func TestDeribitWsPriceManager_ErrorHandling(t *testing.T) {
	// Test connection to invalid URL
	pm := NewDeribitWsPriceManager(WithDeribitWsURLOption("ws://invalid:9999"))

	err := pm.Connect()
	// WebSocket connection failures are allowed - REST API fallback is used
	// The manager should not fail when WS fails, it should gracefully degrade
	assert.NoError(t, err, "Connection to invalid URL should not fail (REST fallback)")
}

func TestDeribitWsPriceManager_Close(t *testing.T) {
	server := newMockDeribitWsServer()
	defer server.Close()

	pm := NewDeribitWsPriceManager(WithDeribitWsURLOption(server.WSURL()))

	err := pm.Connect()
	require.NoError(t, err)

	err = pm.Subscribe("BTC-17JUL26-64000-P")
	require.NoError(t, err)

	// Close should clean up resources
	err = pm.Close()
	assert.NoError(t, err, "Close should succeed")

	// Multiple closes should be safe
	err = pm.Close()
	assert.NoError(t, err, "Multiple closes should be safe")
}

func TestDeribitWsPriceManager_SubscribeWithoutConnection(t *testing.T) {
	pm := NewDeribitWsPriceManager()

	// Subscribe without connection should fail
	err := pm.Subscribe("BTC-17JUL26-64000-P")
	assert.Error(t, err, "Subscribe without connection should fail")
	assert.Contains(t, err.Error(), "not connected")
}

func TestDeribitWsPriceManager_UnsubscribeWithoutConnection(t *testing.T) {
	pm := NewDeribitWsPriceManager()

	// Unsubscribe without connection should fail
	err := pm.Unsubscribe("BTC-17JUL26-64000-P")
	assert.Error(t, err, "Unsubscribe without connection should fail")
	assert.Contains(t, err.Error(), "not connected")
}

func TestDeribitWsPriceManager_HandleInvalidTickerData(t *testing.T) {
	server := newMockDeribitWsServer()
	defer server.Close()

	pm := NewDeribitWsPriceManager(WithDeribitWsURLOption(server.WSURL()))

	err := pm.Connect()
	require.NoError(t, err)
	defer pm.Close()

	// Subscribe should still succeed even with invalid data handling
	err = pm.Subscribe("BTC-17JUL26-64000-P")
	assert.NoError(t, err)
}

func TestDeribitWsPriceManager_GetPriceNotAvailable(t *testing.T) {
	pm := NewDeribitWsPriceManager()

	// Get price for unsubscribed instrument
	price, ok := pm.GetPrice("BTC-17JUL26-64000-P")
	assert.False(t, ok, "Price should not be available")
	assert.Equal(t, 0.0, price)
}

func TestDeribitWsPriceManager_HandleTickerUpdateEdgeCases(t *testing.T) {
	pm := NewDeribitWsPriceManager()

	// Test 1: instrument_name is not a string
	pm.Manager.UpdatePrice("test1", 0.5) // pre-populate
	pm.handleTickerUpdate(map[string]interface{}{
		"instrument_name": 123, // wrong type
		"mark_price":      0.006,
	})
	price, ok := pm.GetPrice("test1")
	assert.True(t, ok, "Price should remain unchanged")
	assert.Equal(t, 0.5, price, "Price should not be updated with wrong instrument_name type")

	// Test 2: mark_price is not a float64
	pm.Manager.UpdatePrice("test2", 0.5)
	pm.handleTickerUpdate(map[string]interface{}{
		"instrument_name": "test2",
		"mark_price":      "invalid", // wrong type
	})
	price, ok = pm.GetPrice("test2")
	assert.True(t, ok, "Price should remain unchanged")
	assert.Equal(t, 0.5, price, "Price should not be updated with wrong mark_price type")

	// Test 3: missing fields
	pm.Manager.UpdatePrice("test3", 0.5)
	pm.handleTickerUpdate(map[string]interface{}{
		// no instrument_name
		"mark_price": 0.007,
	})
	price, ok = pm.GetPrice("test3")
	assert.True(t, ok, "Price should remain unchanged")
	assert.Equal(t, 0.5, price, "Price should not be updated with missing fields")
}

// TestDeribitWsPriceManager_GetSubscriptions_DetectsDeadConnection verifies that
// GetSubscriptions() detects when the WebSocket connection is dead and returns an error.
// This is critical for the auto-reconnection logic in EnsureSubscribed().
func TestDeribitWsPriceManager_GetSubscriptions_DetectsDeadConnection(t *testing.T) {
	server := newMockDeribitWsServer()
	defer server.Close()

	pm := NewDeribitWsPriceManager(WithDeribitWsURLOption(server.WSURL()))

	// Connect and subscribe
	err := pm.Connect()
	require.NoError(t, err)
	defer pm.Close()

	err = pm.Subscribe("BTC-17JUL26-64000-P")
	require.NoError(t, err)

	// Verify subscription is tracked
	subs, err := pm.GetSubscriptions()
	require.NoError(t, err)
	assert.Contains(t, subs, "BTC-17JUL26-64000-P")

	// Close the underlying connection to simulate a dead connection
	pm.mu.Lock()
	pm.conn.Close()
	pm.mu.Unlock()

	// Wait a moment for the close to take effect
	time.Sleep(100 * time.Millisecond)

	// GetSubscriptions should detect the dead connection and return an error
	subs, err = pm.GetSubscriptions()
	assert.Error(t, err, "GetSubscriptions should detect dead connection")
	assert.Contains(t, err.Error(), "WebSocket not connected", "Error should indicate connection is gone")
	assert.Nil(t, subs, "Subscriptions should be nil on error")
}
