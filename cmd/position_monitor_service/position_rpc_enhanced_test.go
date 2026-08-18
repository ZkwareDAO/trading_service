package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

// TestHandleUpdateUserOrderStatus - REMOVED (interface in UOS, not PMS)
// See internal/rpc/server.go for the actual implementation

// TestHandleGetMarketPriceEnhanced tests the enhanced market price endpoint
// Fixes: Market orders cannot get price when no position exists (fallback to REST API)
func TestHandleGetMarketPriceEnhanced(t *testing.T) {
	// Setup test repository
	state, err := persistence.NewGlobalState("/tmp/test_rpc_price_" + time.Now().Format("20060102150405"))
	if err != nil {
		t.Fatalf("Failed to create test state: %v", err)
	}
	defer state.Shutdown()

	repo := persistence.NewStateRepository(state)

	// A user on the target exchange is required: fetchMarketPrice only reaches the
	// REST fallback when findUserWithExchange returns a non-zero user id.
	now := time.Now()
	repo.CreateUser(&order.User{Name: "test_binance", Exchange: "binance", CreatedAt: now, UpdatedAt: now})

	// Create mock exchange resolver (reuse existing mockExchange from order_status_scanner_test.go)
	mockResolver := &testRPCResolver{}

	// Create handler with resolver
	handler := NewPositionRPCHandler(repo, nil, mockResolver)

	// Test case: Get price for BTCUSDT on binance (no position exists, should use REST API fallback)
	reqBody := GetMarketPriceRequest{
		Exchange: "binance",
		Symbol:   "BTCUSDT",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/rpc/v1/market-price/get", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleGetMarketPriceEnhanced(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	// Parse response
	var resp MarketPriceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Price <= 0 {
		t.Errorf("Expected positive price, got %.2f", resp.Price)
	}

	if resp.Source != "rest_api" {
		t.Errorf("Expected source='rest_api' (fallback), got '%s'", resp.Source)
	}

	t.Logf("SUCCESS: Got price=%.2f from source='%s' (market order fallback works when no position exists)", resp.Price, resp.Source)
}

// testRPCResolver implements exchangeResolver for testing
type testRPCResolver struct{}

func (m *testRPCResolver) ResolveExchange(userID uint64, name string) (exchange.Exchange, error) {
	// Return existing mockExchange (defined in order_status_scanner_test.go) with price
	return &mockExchangeWithPrice{price: 50000.0}, nil
}

// mockExchangeWithPrice extends mockExchange with GetPrice method
type mockExchangeWithPrice struct {
	price float64
}

func (m *mockExchangeWithPrice) Name() string { return "mock" }
func (m *mockExchangeWithPrice) CreateOrder(req exchange.CreateOrderRequest) (*exchange.CreateOrderResponse, error) {
	return nil, nil
}
func (m *mockExchangeWithPrice) CancelOrder(orderID uint64) error { return nil }
func (m *mockExchangeWithPrice) GetOrder(orderID uint64, symbol string) (*exchange.OrderInfo, error) {
	return nil, nil
}
func (m *mockExchangeWithPrice) GetPositions() ([]exchange.PositionInfo, error)        { return nil, nil }
func (m *mockExchangeWithPrice) SetLeverage(symbol string, leverage int) error         { return nil }
func (m *mockExchangeWithPrice) GetLeverage(symbol string) (int, error)                { return 1, nil }
func (m *mockExchangeWithPrice) GetPrice(symbol string) (float64, error)               { return m.price, nil }
func (m *mockExchangeWithPrice) Connect() error                                        { return nil }
func (m *mockExchangeWithPrice) Close() error                                          { return nil }
func (m *mockExchangeWithPrice) SubscribeOrders(callback exchange.OrderCallback) error { return nil }
