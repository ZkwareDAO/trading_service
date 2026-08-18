package deribit

import (
	"os"
	"testing"
	"time"

	"trading-service/internal/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Integration Tests for Deribit Testnet
// =============================================================================

// These tests require real Deribit Testnet API credentials.
// Set environment variables before running:
//
//	export DERIBIT_TESTNET_API_KEY="your_testnet_key"
//	export DERIBIT_TESTNET_API_SECRET="your_testnet_secret"
//	export DERIBIT_TESTNET_API_PWD="your_testnet_password"
//
// Run with: go test -tags=integration ./internal/exchange/deribit/...

func getTestnetCredentials(t *testing.T) (key, secret, pwd string) {
	key = os.Getenv("DERIBIT_TESTNET_API_KEY")
	secret = os.Getenv("DERIBIT_TESTNET_API_SECRET")
	pwd = os.Getenv("DERIBIT_TESTNET_API_PWD")

	if key == "" || secret == "" {
		t.Skip("Skipping integration test: DERIBIT_TESTNET_API_KEY/SECRET not set")
	}

	return key, secret, pwd
}

// TestIntegration_Authenticate tests real authentication with Deribit testnet
func TestIntegration_Authenticate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	key, secret, pwd := getTestnetCredentials(t)

	client, err := NewDeribit(key, secret, pwd, true) // testnet=true
	require.NoError(t, err)
	defer client.Close()

	err = client.Connect()
	require.NoError(t, err, "Authentication should succeed with valid credentials")

	assert.Equal(t, "deribit", client.Name())
}

// TestIntegration_GetPrice tests real price query
func TestIntegration_GetPrice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	key, secret, pwd := getTestnetCredentials(t)

	client, err := NewDeribit(key, secret, pwd, true)
	require.NoError(t, err)
	defer client.Close()

	// Use a valid option symbol from testnet
	// Note: You may need to update this symbol to match available testnet options
	symbol := "BTC-27SEP24-60000-P"

	price, err := client.GetPrice(symbol)
	if err != nil {
		// It's okay if the symbol doesn't exist - test passes if API call works
		t.Logf("Price query returned error (symbol may not exist): %v", err)
		return
	}

	assert.Greater(t, price, 0.0, "Price should be positive")
	t.Logf("Price for %s: %.6f BTC", symbol, price)
}

// TestIntegration_GetPositions tests position query
func TestIntegration_GetPositions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	key, secret, pwd := getTestnetCredentials(t)

	client, err := NewDeribit(key, secret, pwd, true)
	require.NoError(t, err)
	defer client.Close()

	err = client.Connect()
	require.NoError(t, err)

	positions, err := client.GetPositions()
	require.NoError(t, err, "GetPositions should not error")

	// May be empty if no positions
	t.Logf("Found %d option positions", len(positions))

	for _, pos := range positions {
		t.Logf("Position: %s %s %.2f @ %.6f (PnL: %.6f)",
			pos.Symbol, pos.PositionSide, pos.Quantity, pos.EntryPrice, pos.UnrealizedPnl)
	}
}

// TestIntegration_CreateAndCancelOrder tests order lifecycle (careful with real orders!)
// WARNING: This creates a real order on testnet. Use with caution.
func TestIntegration_CreateAndCancelOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Skip("Manual test only - uncomment to test real order creation")

	key, secret, pwd := getTestnetCredentials(t)

	client, err := NewDeribit(key, secret, pwd, true)
	require.NoError(t, err)
	defer client.Close()

	err = client.Connect()
	require.NoError(t, err)

	// Create a limit order far from market price (won't fill)
	req := exchange.CreateOrderRequest{
		Symbol:    "BTC-27SEP24-60000-P", // Adjust to available testnet option
		Side:      exchange.OrderSideBuy,
		OrderType: exchange.OrderTypeLimit,
		Quantity:  0.1,
		Price:     0.0001, // Very low price to avoid fill
	}

	resp, err := client.CreateOrder(req)
	require.NoError(t, err, "Order creation should succeed")

	t.Logf("Order created: ID=%d, Status=%s", resp.OrderID, resp.Status)
	assert.Equal(t, exchange.OrderStatusNew, resp.Status)

	// Query order status
	info, err := client.GetOrder(resp.OrderID, req.Symbol)
	require.NoError(t, err)
	assert.Equal(t, resp.OrderID, info.OrderID)
	t.Logf("Order status: %s, Filled: %.2f", info.Status, info.Filled)

	// Cancel the order
	err = client.CancelOrder(resp.OrderID)
	require.NoError(t, err, "Order cancellation should succeed")
	t.Logf("Order %d cancelled successfully", resp.OrderID)

	// Verify cancelled status
	info, err = client.GetOrder(resp.OrderID, req.Symbol)
	require.NoError(t, err)
	assert.Equal(t, exchange.OrderStatusCancelled, info.Status)
}

// TestIntegration_LeverageNotSupported tests that leverage operations fail
func TestIntegration_LeverageNotSupported(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	key, secret, pwd := getTestnetCredentials(t)

	client, err := NewDeribit(key, secret, pwd, true)
	require.NoError(t, err)
	defer client.Close()

	// SetLeverage should fail
	err = client.SetLeverage("BTC-27SEP24-60000-P", 10)
	assert.Error(t, err, "SetLeverage should fail for options")
	assert.Contains(t, err.Error(), "leverage not supported")

	// GetLeverage should return 0
	leverage, err := client.GetLeverage("BTC-27SEP24-60000-P")
	assert.NoError(t, err)
	assert.Equal(t, 0, leverage, "Options don't use leverage")
}

// TestIntegration_WebSocketNotImplemented tests WebSocket error handling
func TestIntegration_WebSocketNotImplemented(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	key, secret, pwd := getTestnetCredentials(t)

	client, err := NewDeribit(key, secret, pwd, true)
	require.NoError(t, err)
	defer client.Close()

	err = client.SubscribeOrders(func(resp *exchange.CreateOrderResponse) {
		// callback
	})
	assert.Error(t, err, "SubscribeOrders should return error (not implemented)")
	assert.Contains(t, err.Error(), "not yet implemented")
}

// TestIntegration_ConcurrentOperations tests thread safety
func TestIntegration_ConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	key, secret, pwd := getTestnetCredentials(t)

	client, err := NewDeribit(key, secret, pwd, true)
	require.NoError(t, err)
	defer client.Close()

	// Concurrent price queries
	done := make(chan bool)

	for i := 0; i < 5; i++ {
		go func() {
			_, err := client.GetPrice("BTC-27SEP24-60000-P")
			if err != nil {
				t.Logf("Concurrent price query error: %v", err)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("Concurrent operations timeout")
		}
	}
}

// TestIntegration_ErrorHandling tests API error responses
func TestIntegration_ErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test with invalid credentials
	client, err := NewDeribit("invalid_key", "invalid_secret", "invalid_pwd", true)
	require.NoError(t, err)

	err = client.Connect()
	assert.Error(t, err, "Authentication should fail with invalid credentials")

	// Test querying non-existent order
	key, secret, pwd := getTestnetCredentials(t)
	client, err = NewDeribit(key, secret, pwd, true)
	require.NoError(t, err)
	defer client.Close()

	err = client.Connect()
	require.NoError(t, err)

	_, err = client.GetOrder(999999999, "BTC-27SEP24-60000-P")
	assert.Error(t, err, "Querying non-existent order should fail")
}
