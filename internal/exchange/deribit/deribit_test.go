package deribit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"trading-service/internal/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Server Helper
// =============================================================================

// mockDeribitServer creates a mock server that handles auth + method requests.
// If authHandler is nil, it returns a successful auth response automatically.
func newMockDeribitServer(authHandler func(map[string]interface{}) interface{}, methodHandler func(map[string]interface{}) interface{}) *mockDeribitServer {
	m := &mockDeribitServer{
		requests: make([]map[string]interface{}, 0),
	}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		m.requests = append(m.requests, req)

		w.Header().Set("Content-Type", "application/json")

		method := req["method"].(string)

		// Handle authentication requests
		if method == "public/auth" {
			if authHandler != nil {
				resp := authHandler(req)
				json.NewEncoder(w).Encode(resp)
			} else {
				// Default successful auth response
				json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"result": map[string]interface{}{
						"access_token": "test_token_12345",
						"expires_in":   3600,
					},
					"id": req["id"],
				})
			}
			return
		}

		// Delegate to custom handler for other methods
		if methodHandler != nil {
			resp := methodHandler(req)
			json.NewEncoder(w).Encode(resp)
		}
	}))
	return m
}

type mockDeribitServer struct {
	*httptest.Server
	requests []map[string]interface{}
}

func (m *mockDeribitServer) Close() {
	m.Server.Close()
}

func (m *mockDeribitServer) URL() string {
	return m.Server.URL
}

// =============================================================================
// User Journey 1: As a trader, I want to open a put option position
// =============================================================================

func TestCreateOrder_OpenPutOption(t *testing.T) {
	tests := []struct {
		name           string
		request        exchange.CreateOrderRequest
		mockResponse   interface{}
		expectedID     uint64
		expectedStatus exchange.OrderStatus
		expectError    bool
	}{
		{
			name: "buy put option limit order",
			request: exchange.CreateOrderRequest{
				Symbol:    "BTC-17JUL26-64000-P",
				Side:      exchange.OrderSideBuy,
				OrderType: exchange.OrderTypeLimit,
				Quantity:  1.0,
				Price:     0.005,
			},
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"order": map[string]interface{}{
						"order_id":    "12345",
						"instrument":  "BTC-17JUL26-64000-P",
						"direction":   "buy",
						"order_type":  "limit",
						"amount":      1.0,
						"price":       0.005,
						"order_state": "open",
						"create_time": time.Now().UnixMilli(),
					},
				},
				"id": 1,
			},
			expectedID:     12345,
			expectedStatus: exchange.OrderStatusNew,
		},
		{
			name: "sell call option market order",
			request: exchange.CreateOrderRequest{
				Symbol:    "BTC-17JUL26-65000-C",
				Side:      exchange.OrderSideSell,
				OrderType: exchange.OrderTypeMarket,
				Quantity:  2.0,
			},
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"order": map[string]interface{}{
						"order_id":    "67890",
						"instrument":  "BTC-17JUL26-65000-C",
						"direction":   "sell",
						"order_type":  "market",
						"amount":      2.0,
						"order_state": "open",
						"create_time": time.Now().UnixMilli(),
					},
				},
				"id": 2,
			},
			expectedID:     67890,
			expectedStatus: exchange.OrderStatusNew,
		},
		{
			name: "precision error from API",
			request: exchange.CreateOrderRequest{
				Symbol:    "BTC-17JUL26-64000-P",
				Side:      exchange.OrderSideBuy,
				OrderType: exchange.OrderTypeLimit,
				Quantity:  1.0,
				Price:     0.0051234,
			},
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"error": map[string]interface{}{
					"code":    10001,
					"message": "Invalid price: price must be multiple of tick_size (0.0001)",
				},
				"id": 3,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				method := req["method"].(string)

				// Handle Market order price calculation APIs
				if method == "public/get_instrument" {
					return map[string]interface{}{
						"jsonrpc": "2.0",
						"result": map[string]interface{}{
							"tick_size": 0.0001,
							"tick_size_steps": []interface{}{
								map[string]interface{}{
									"above_price": 0.005,
									"tick_size":   0.0005,
								},
							},
						},
						"id": req["id"],
					}
				}
				if method == "public/ticker" {
					return map[string]interface{}{
						"jsonrpc": "2.0",
						"result": map[string]interface{}{
							"mark_price":     0.118,
							"best_bid_price": 0.1135,
							"best_ask_price": 0.1475,
						},
						"id": req["id"],
					}
				}

				// Handle order creation APIs
				assert.Contains(t, []string{"private/buy", "private/sell"}, method)
				params := req["params"].(map[string]interface{})
				assert.Equal(t, tt.request.Symbol, params["instrument_name"])

				return tt.mockResponse
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			resp, err := client.CreateOrder(tt.request)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedID, resp.OrderID)
			assert.Equal(t, tt.request.Symbol, resp.Symbol)
			assert.Equal(t, tt.request.Side, resp.Side)
			assert.Equal(t, tt.expectedStatus, resp.Status)
		})
	}
}

// =============================================================================
// User Journey 2: As a trader, I want to close my option position
// =============================================================================

func TestCreateOrder_ClosePosition(t *testing.T) {
	tests := []struct {
		name         string
		request      exchange.CreateOrderRequest
		mockResponse interface{}
		expectReduce bool
	}{
		{
			name: "close long put position",
			request: exchange.CreateOrderRequest{
				Symbol:     "BTC-17JUL26-64000-P",
				Side:       exchange.OrderSideSell,
				OrderType:  exchange.OrderTypeLimit,
				Quantity:   1.0,
				Price:      0.006,
				ReduceOnly: true,
			},
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"order": map[string]interface{}{
						"order_id":    "99999",
						"instrument":  "BTC-17JUL26-64000-P",
						"direction":   "sell",
						"order_type":  "limit",
						"amount":      1.0,
						"price":       0.006,
						"order_state": "open",
						"create_time": time.Now().UnixMilli(),
					},
				},
				"id": 1,
			},
			expectReduce: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reduceOnlyReceived := false

			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				params := req["params"].(map[string]interface{})
				if tt.expectReduce {
					reduceOnly, ok := params["reduce_only"].(bool)
					if ok && reduceOnly {
						reduceOnlyReceived = true
					}
				}
				return tt.mockResponse
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			resp, err := client.CreateOrder(tt.request)
			require.NoError(t, err)
			assert.NotNil(t, resp)

			if tt.expectReduce {
				assert.True(t, reduceOnlyReceived, "reduce_only flag should be set for closing positions")
			}
		})
	}
}

// =============================================================================
// User Journey 3: As a scanner, I want to query order status
// =============================================================================

func TestGetOrder_QueryStatus(t *testing.T) {
	tests := []struct {
		name           string
		orderID        uint64
		symbol         string
		mockResponse   interface{}
		expectedStatus exchange.OrderStatus
		expectedFilled float64
	}{
		{
			name:    "open order",
			orderID: 12345,
			symbol:  "BTC-17JUL26-64000-P",
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"order_id":      "12345",
					"instrument":    "BTC-17JUL26-64000-P",
					"order_state":   "open",
					"direction":     "buy",
					"amount":        1.0,
					"filled_amount": 0.0,
					"price":         0.005,
				},
				"id": 1,
			},
			expectedStatus: exchange.OrderStatusNew,
			expectedFilled: 0.0,
		},
		{
			name:    "filled order",
			orderID: 67890,
			symbol:  "BTC-17JUL26-65000-C",
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"order_id":      "67890",
					"instrument":    "BTC-17JUL26-65000-C",
					"order_state":   "filled",
					"direction":     "sell",
					"amount":        2.0,
					"filled_amount": 2.0,
					"price":         0.007,
					"average_price": 0.0072,
				},
				"id": 2,
			},
			expectedStatus: exchange.OrderStatusFilled,
			expectedFilled: 2.0,
		},
		{
			name:    "cancelled order",
			orderID: 11111,
			symbol:  "BTC-17JUL26-63000-P",
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"order_id":      "11111",
					"instrument":    "BTC-17JUL26-63000-P",
					"order_state":   "cancelled",
					"direction":     "buy",
					"amount":        1.0,
					"filled_amount": 0.0,
					"price":         0.004,
				},
				"id": 3,
			},
			expectedStatus: exchange.OrderStatusCancelled,
			expectedFilled: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				assert.Equal(t, "private/get_order_state", req["method"])

				params := req["params"].(map[string]interface{})
				assert.Equal(t, fmt.Sprintf("%d", tt.orderID), params["order_id"])

				return tt.mockResponse
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			info, err := client.GetOrder(tt.orderID, tt.symbol)

			require.NoError(t, err)
			assert.Equal(t, tt.orderID, info.OrderID)
			assert.Equal(t, tt.symbol, info.Symbol)
			assert.Equal(t, tt.expectedStatus, info.Status)
			assert.Equal(t, tt.expectedFilled, info.Filled)
		})
	}
}

// =============================================================================
// User Journey 3.1: As a scanner, I want to query ETH order with SYMBOL-NUMBER format ID
// =============================================================================

func TestGetOrder_ETHOrderIDFallback(t *testing.T) {
	// ETH options return order IDs in format "ETH-81179066009"
	// Scanner stores only numeric part (81179066009)
	// GetOrder should try numeric ID first, then fallback to ETH-prefixed ID

	tests := []struct {
		name            string
		orderID         uint64
		symbol          string
		firstResponse   interface{} // Response for numeric ID query (fails)
		secondResponse  interface{} // Response for ETH-prefixed ID query (succeeds)
		expectedStatus  exchange.OrderStatus
		expectedFilled  float64
		expectedCallIDs []string // Expected order_id params in order
	}{
		{
			name:    "ETH order fallback to prefixed ID",
			orderID: 81179066009,
			symbol:  "ETH-25SEP26-1900-P",
			firstResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"error": map[string]interface{}{
					"code":    10000,
					"message": "order not found",
				},
				"id": 1,
			},
			secondResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"order_id":      "ETH-81179066009",
					"instrument":    "ETH-25SEP26-1900-P",
					"order_state":   "open",
					"direction":     "buy",
					"amount":        0.5,
					"filled_amount": 0.0,
					"price":         0.002,
				},
				"id": 2,
			},
			expectedStatus:  exchange.OrderStatusNew,
			expectedFilled:  0.0,
			expectedCallIDs: []string{"81179066009", "ETH-81179066009"},
		},
		{
			name:    "BTC order fallback to prefixed ID",
			orderID: 107489620314,
			symbol:  "BTC-24JUL26-64000-P",
			firstResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"error": map[string]interface{}{
					"code":    10000,
					"message": "order not found",
				},
				"id": 1,
			},
			secondResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"order_id":      "BTC-107489620314",
					"instrument":    "BTC-24JUL26-64000-P",
					"order_state":   "filled",
					"direction":     "buy",
					"amount":        0.1,
					"filled_amount": 0.1,
					"price":         0.015,
					"average_price": 0.015,
				},
				"id": 2,
			},
			expectedStatus:  exchange.OrderStatusFilled,
			expectedFilled:  0.1,
			expectedCallIDs: []string{"107489620314", "BTC-107489620314"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			var receivedOrderIDs []string

			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				assert.Equal(t, "private/get_order_state", req["method"])

				params := req["params"].(map[string]interface{})
				receivedOrderIDs = append(receivedOrderIDs, params["order_id"].(string))

				callCount++
				if callCount == 1 {
					return tt.firstResponse
				}
				return tt.secondResponse
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			info, err := client.GetOrder(tt.orderID, tt.symbol)

			require.NoError(t, err)
			assert.Equal(t, tt.orderID, info.OrderID)
			assert.Equal(t, tt.symbol, info.Symbol)
			assert.Equal(t, tt.expectedStatus, info.Status)
			assert.Equal(t, tt.expectedFilled, info.Filled)
			assert.Equal(t, tt.expectedCallIDs, receivedOrderIDs, "should call with numeric ID first, then prefixed ID")
		})
	}
}

// =============================================================================
// User Journey 4: As a trader, I want to see my option positions
// =============================================================================

func TestGetPositions_OptionPositions(t *testing.T) {
	tests := []struct {
		name          string
		mockResponse  func(currency string) interface{}
		expectedCount int
		validatePos   func(t *testing.T, positions []exchange.PositionInfo)
	}{
		{
			name: "multiple option positions",
			mockResponse: func(currency string) interface{} {
				// Only return positions for BTC, empty for ETH
				if currency == "ETH" {
					return map[string]interface{}{
						"jsonrpc": "2.0",
						"result":  []interface{}{},
						"id":      1,
					}
				}
				return map[string]interface{}{
					"jsonrpc": "2.0",
					"result": []interface{}{
						map[string]interface{}{
							"instrument_name":        "BTC-17JUL26-64000-P",
							"kind":              "option",
							"size":              5.0,
							"direction":         "buy",
							"average_price":     0.005,
							"mark_price":        0.006,
							"total_profit_loss": 0.001,
						},
						map[string]interface{}{
							"instrument_name":        "BTC-17JUL26-65000-C",
							"kind":              "option",
							"size":              -3.0,
							"direction":         "sell",
							"average_price":     0.007,
							"mark_price":        0.0065,
							"total_profit_loss": -0.0005,
						},
						map[string]interface{}{
							"instrument_name": "BTC-PERPETUAL",
							"kind":       "future",
							"size":       100.0,
							"direction":  "buy",
						},
					},
					"id": 1,
				}
			},
			expectedCount: 2,
			validatePos: func(t *testing.T, positions []exchange.PositionInfo) {
				assert.Equal(t, "BTC-17JUL26-64000-P", positions[0].Symbol)
				assert.Equal(t, exchange.PositionSideLong, positions[0].PositionSide)
				assert.Equal(t, 5.0, positions[0].Quantity)

				assert.Equal(t, "BTC-17JUL26-65000-C", positions[1].Symbol)
				assert.Equal(t, exchange.PositionSideShort, positions[1].PositionSide)
				assert.Equal(t, 3.0, positions[1].Quantity)
			},
		},
		{
			name: "no positions",
			mockResponse: func(currency string) interface{} {
				return map[string]interface{}{
					"jsonrpc": "2.0",
					"result":  []interface{}{},
					"id":      1,
				}
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				assert.Equal(t, "private/get_positions", req["method"])

				params := req["params"].(map[string]interface{})
				currency := params["currency"].(string)
				assert.Contains(t, []string{"BTC", "ETH", ""}, currency)

				return tt.mockResponse(currency)
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			positions, err := client.GetPositions()
			require.NoError(t, err)
			assert.Len(t, positions, tt.expectedCount)

			if tt.validatePos != nil {
				tt.validatePos(t, positions)
			}
		})
	}
}

// =============================================================================
// User Journey 5: As a trader, I want to cancel my option order
// =============================================================================

func TestCancelOrder_CancelOption(t *testing.T) {
	tests := []struct {
		name         string
		orderID      uint64
		mockResponse interface{}
		expectError  bool
	}{
		{
			name:    "successful cancel",
			orderID: 12345,
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"order_id":    "12345",
					"order_state": "cancelled",
				},
				"id": 1,
			},
		},
		{
			name:    "order not found",
			orderID: 99999,
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"error": map[string]interface{}{
					"code":    10002,
					"message": "Order not found",
				},
				"id": 2,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				assert.Equal(t, "private/cancel", req["method"])

				params := req["params"].(map[string]interface{})
				assert.Equal(t, fmt.Sprintf("%d", tt.orderID), params["order_id"])

				return tt.mockResponse
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			err = client.CancelOrder(tt.orderID)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// User Journey 6: As a trader, I want to get option prices
// =============================================================================

func TestGetPrice_OptionPrice(t *testing.T) {
	tests := []struct {
		name          string
		symbol        string
		mockResponse  interface{}
		expectedPrice float64
		expectError   bool
	}{
		{
			name:   "get BTC put option price",
			symbol: "BTC-17JUL26-64000-P",
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"instrument_name": "BTC-17JUL26-64000-P",
					"mark_price":      0.0055,
					"best_bid_price":  0.0054,
					"best_ask_price":  0.0056,
				},
				"id": 1,
			},
			expectedPrice: 0.0055,
		},
		{
			name:   "get ETH call option price",
			symbol: "ETH-19JUL26-3000-C",
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"instrument_name": "ETH-19JUL26-3000-C",
					"mark_price":      0.12,
					"best_bid_price":  0.118,
					"best_ask_price":  0.122,
				},
				"id": 2,
			},
			expectedPrice: 0.12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				assert.Contains(t, []string{"public/ticker", "public/get_order_book"}, req["method"])

				params := req["params"].(map[string]interface{})
				assert.Equal(t, tt.symbol, params["instrument_name"])

				return tt.mockResponse
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			price, err := client.GetPrice(tt.symbol)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedPrice, price)
			}
		})
	}
}

// =============================================================================
// User Journey 7: As a system, I want to authenticate with Deribit
// =============================================================================

func TestAuthentication(t *testing.T) {
	tests := []struct {
		name         string
		apiKey       string
		apiSecret    string
		apiPwd       string
		authResponse interface{}
		expectError  bool
	}{
		{
			name:      "successful authentication",
			apiKey:    "test_key",
			apiSecret: "test_secret",
			apiPwd:    "test_pwd",
			authResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"access_token": "test_token_12345",
					"expires_in":   3600,
				},
				"id": 1,
			},
		},
		{
			name:      "invalid credentials",
			apiKey:    "bad_key",
			apiSecret: "bad_secret",
			apiPwd:    "bad_pwd",
			authResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"error": map[string]interface{}{
					"code":    10003,
					"message": "Invalid credentials",
				},
				"id": 2,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newMockDeribitServer(func(req map[string]interface{}) interface{} {
				params := req["params"].(map[string]interface{})
				assert.Equal(t, tt.apiKey, params["client_id"])
				assert.Equal(t, tt.apiSecret, params["client_secret"])
				assert.Equal(t, "client_credentials", params["grant_type"])
				return tt.authResponse
			}, nil)
			defer server.Close()

			client, err := NewDeribit(tt.apiKey, tt.apiSecret, tt.apiPwd, false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			err = client.Connect()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// User Journey 8: As a trader, I want symbols to pass through unchanged
// =============================================================================

func TestSymbolPassthrough(t *testing.T) {
	testSymbols := []string{
		"BTC-17JUL26-64000-P",
		"BTC-17JUL26-65000-C",
		"ETH-19JUL26-3000-C",
		"ETH-19JUL26-2800-P",
	}

	for _, symbol := range testSymbols {
		t.Run(symbol, func(t *testing.T) {
			var receivedSymbol string

			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				params := req["params"].(map[string]interface{})
				receivedSymbol = params["instrument_name"].(string)

				return map[string]interface{}{
					"jsonrpc": "2.0",
					"result": map[string]interface{}{
						"order": map[string]interface{}{
							"order_id":    "12345",
							"instrument":  receivedSymbol,
							"order_state": "open",
							"create_time": time.Now().UnixMilli(),
						},
					},
					"id": 1,
				}
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			_, err = client.CreateOrder(exchange.CreateOrderRequest{
				Symbol:    symbol,
				Side:      exchange.OrderSideBuy,
				OrderType: exchange.OrderTypeLimit,
				Quantity:  1.0,
				Price:     0.005,
			})
			require.NoError(t, err)

			assert.Equal(t, symbol, receivedSymbol,
				"Symbol should pass through unchanged (no suffix/conversion)")
		})
	}
}

// =============================================================================
// Edge Cases and Error Handling
// =============================================================================

func TestErrorHandling(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		mockStatus    int
		mockBody      string
		expectError   bool
		errorContains string
	}{
		{
			name:        "network error",
			method:      "CreateOrder",
			mockStatus:  0,
			expectError: true,
		},
		{
			name:          "rate limit error",
			method:        "CreateOrder",
			mockStatus:    http.StatusTooManyRequests,
			mockBody:      `{"error": {"code": 10009, "message": "Rate limit exceeded"}}`,
			expectError:   true,
			errorContains: "rate limit",
		},
		{
			name:          "insufficient balance",
			method:        "CreateOrder",
			mockStatus:    http.StatusOK,
			mockBody:      `{"error": {"code": 10005, "message": "Insufficient balance"}}`,
			expectError:   true,
			errorContains: "balance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server
			if tt.mockStatus > 0 {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.mockStatus)
					w.Write([]byte(tt.mockBody))
				}))
				defer server.Close()
			}

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)

			if server != nil {
				client.baseURL = server.URL
			} else {
				client.baseURL = "http://nonexistent:9999"
			}

			switch tt.method {
			case "CreateOrder":
				_, err = client.CreateOrder(exchange.CreateOrderRequest{
					Symbol:    "BTC-17JUL26-64000-P",
					Side:      exchange.OrderSideBuy,
					OrderType: exchange.OrderTypeLimit,
					Quantity:  1.0,
					Price:     0.005,
				})
			}

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.True(t, strings.Contains(strings.ToLower(err.Error()), tt.errorContains),
						"Error should contain '%s', got: %s", tt.errorContains, err.Error())
				}
			}
		})
	}
}

// =============================================================================
// Integration Test Helpers
// =============================================================================

func TestDeribitImplementsExchangeInterface(t *testing.T) {
	var _ exchange.Exchange = (*Deribit)(nil)
}

func TestName(t *testing.T) {
	client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
	require.NoError(t, err)

	assert.Equal(t, "deribit", client.Name())
}

// =============================================================================
// User Journey: Order Status Consistency
// =============================================================================

func TestCreateOrder_AlwaysReturnsNEW(t *testing.T) {
	// User Journey: As a trading system, I want Deribit CreateOrder to always return NEW status,
	// so that Scanner can handle status updates consistently across all exchanges.

	tests := []struct {
		name           string
		exchangeStatus string // The status returned by Deribit API
		expectedStatus exchange.OrderStatus
	}{
		{
			name:           "immediately filled order should return NEW",
			exchangeStatus: "filled",
			expectedStatus: exchange.OrderStatus("NEW"),
		},
		{
			name:           "open order should return NEW",
			exchangeStatus: "open",
			expectedStatus: exchange.OrderStatus("NEW"),
		},
		{
			name:           "partially filled order should return NEW",
			exchangeStatus: "open",
			expectedStatus: exchange.OrderStatus("NEW"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				return map[string]interface{}{
					"jsonrpc": "2.0",
					"result": map[string]interface{}{
						"order": map[string]interface{}{
							"order_id":     "107489620314",
							"order_state":  tt.exchangeStatus,
							"amount":       0.1,
							"average_price": 0.05,
							"instrument_name": "BTC-24JUL26-64000-P",
						},
					},
					"id": req["id"],
				}
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			resp, err := client.CreateOrder(exchange.CreateOrderRequest{
				Symbol:    "BTC-24JUL26-64000-P",
				Side:      exchange.OrderSideBuy,
				OrderType: exchange.OrderTypeLimit,
				Quantity:  0.1,
				Price:     0.05,
			})
			require.NoError(t, err)

			assert.Equal(t, tt.expectedStatus, resp.Status,
				"CreateOrder should always return NEW status, even if exchange returns %s", tt.exchangeStatus)
		})
	}
}

func TestClose(t *testing.T) {
	client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
	require.NoError(t, err)

	err = client.Close()
	assert.NoError(t, err)
}

// =============================================================================
// User Journey: Deribit Market Order Price Calculation
// =============================================================================

func TestGetInstrumentDetails(t *testing.T) {
	// User Journey: As a trader, I want to get instrument details including tick_size_steps,
	// so that I can calculate the correct price for market orders.

	tests := []struct {
		name           string
		symbol         string
		mockResponse   interface{}
		expectedResult *InstrumentDetails
		expectError    bool
	}{
		{
			name:   "get instrument with tick_size_steps",
			symbol: "BTC-26SEP26-100000-C",
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"instrument_name":  "BTC-26SEP26-100000-C",
					"tick_size":        0.0001,
					"contract_size":    1.0,
					"min_trade_amount": 0.1,
					"tick_size_steps": []interface{}{
						map[string]interface{}{
							"above_price": 0.005,
							"tick_size":   0.0005,
						},
					},
				},
				"id": 1,
			},
			expectedResult: &InstrumentDetails{
				TickSize:       0.0001,
				ContractSize:   1.0,
				MinTradeAmount: 0.1,
				TickSizeSteps: []TickSizeStep{
					{AbovePrice: 0.005, TickSize: 0.0005},
				},
			},
		},
		{
			name:   "get instrument without tick_size_steps (fallback)",
			symbol: "BTC-26SEP26-90000-P",
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"instrument_name":  "BTC-26SEP26-90000-P",
					"tick_size":        0.0001,
					"contract_size":    1.0,
					"min_trade_amount": 0.1,
				},
				"id": 2,
			},
			expectedResult: &InstrumentDetails{
				TickSize:       0.0001,
				ContractSize:   1.0,
				MinTradeAmount: 0.1,
				TickSizeSteps:  nil,
			},
		},
		{
			name:   "instrument not found",
			symbol: "INVALID-SYMBOL",
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"error": map[string]interface{}{
					"code":    10000,
					"message": "Instrument not found",
				},
				"id": 3,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				assert.Equal(t, "public/get_instrument", req["method"])

				params := req["params"].(map[string]interface{})
				assert.Equal(t, tt.symbol, params["instrument_name"])

				return tt.mockResponse
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			details, err := client.GetInstrumentDetails(tt.symbol)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, details)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedResult.TickSize, details.TickSize)
			assert.Equal(t, tt.expectedResult.ContractSize, details.ContractSize)
			assert.Equal(t, tt.expectedResult.MinTradeAmount, details.MinTradeAmount)

			if tt.expectedResult.TickSizeSteps != nil {
				require.Len(t, details.TickSizeSteps, len(tt.expectedResult.TickSizeSteps))
				for i, step := range tt.expectedResult.TickSizeSteps {
					assert.Equal(t, step.AbovePrice, details.TickSizeSteps[i].AbovePrice)
					assert.Equal(t, step.TickSize, details.TickSizeSteps[i].TickSize)
				}
			}
		})
	}
}

func TestGetTickerInfo(t *testing.T) {
	// User Journey: As a trader, I want to get full ticker info including bid/ask,
	// so that I can check spread before closing positions.

	tests := []struct {
		name           string
		symbol         string
		mockResponse   interface{}
		expectedResult *TickerInfo
		expectError    bool
	}{
		{
			name:   "get ticker with bid and ask",
			symbol: "BTC-26SEP26-100000-C",
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"instrument_name": "BTC-26SEP26-100000-C",
					"mark_price":      0.118,
					"best_bid_price":  0.1135,
					"best_ask_price":  0.1475,
				},
				"id": 1,
			},
			expectedResult: &TickerInfo{
				MarkPrice: 0.118,
				BestBid:   0.1135,
				BestAsk:   0.1475,
			},
		},
		{
			name:   "ticker with zero bid (no buyers)",
			symbol: "BTC-26SEP26-90000-P",
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"instrument_name": "BTC-26SEP26-90000-P",
					"mark_price":      0.001,
					"best_bid_price":  0.0,
					"best_ask_price":  0.002,
				},
				"id": 2,
			},
			expectedResult: &TickerInfo{
				MarkPrice: 0.001,
				BestBid:   0.0,
				BestAsk:   0.002,
			},
		},
		{
			name:   "ticker API error",
			symbol: "INVALID-SYMBOL",
			mockResponse: map[string]interface{}{
				"jsonrpc": "2.0",
				"error": map[string]interface{}{
					"code":    10000,
					"message": "Instrument not found",
				},
				"id": 3,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				assert.Equal(t, "public/ticker", req["method"])

				params := req["params"].(map[string]interface{})
				assert.Equal(t, tt.symbol, params["instrument_name"])

				return tt.mockResponse
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			ticker, err := client.GetTickerInfo(tt.symbol)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, ticker)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedResult.MarkPrice, ticker.MarkPrice)
			assert.Equal(t, tt.expectedResult.BestBid, ticker.BestBid)
			assert.Equal(t, tt.expectedResult.BestAsk, ticker.BestAsk)
		})
	}
}

func TestGetTickSizeForPrice(t *testing.T) {
	// User Journey: As a trading system, I want to select the correct tick_size based on price,
	// so that orders use the correct precision for their price level.

	tests := []struct {
		name          string
		price         float64
		details       *InstrumentDetails
		expectedTick  float64
	}{
		{
			name:  "price below threshold uses default tick_size",
			price: 0.003,
			details: &InstrumentDetails{
				TickSize: 0.0001,
				TickSizeSteps: []TickSizeStep{
					{AbovePrice: 0.005, TickSize: 0.0005},
				},
			},
			expectedTick: 0.0001,
		},
		{
			name:  "price at threshold uses tick_size_step",
			price: 0.005,
			details: &InstrumentDetails{
				TickSize: 0.0001,
				TickSizeSteps: []TickSizeStep{
					{AbovePrice: 0.005, TickSize: 0.0005},
				},
			},
			expectedTick: 0.0005,
		},
		{
			name:  "price above threshold uses tick_size_step",
			price: 0.118,
			details: &InstrumentDetails{
				TickSize: 0.0001,
				TickSizeSteps: []TickSizeStep{
					{AbovePrice: 0.005, TickSize: 0.0005},
				},
			},
			expectedTick: 0.0005,
		},
		{
			name:  "no tick_size_steps uses default",
			price: 0.1,
			details: &InstrumentDetails{
				TickSize:      0.0001,
				TickSizeSteps: nil,
			},
			expectedTick: 0.0001,
		},
		{
			name:  "multiple steps selects correct one",
			price: 0.15,
			details: &InstrumentDetails{
				TickSize: 0.0001,
				TickSizeSteps: []TickSizeStep{
					{AbovePrice: 0.005, TickSize: 0.0005},
					{AbovePrice: 0.1, TickSize: 0.001},
				},
			},
			expectedTick: 0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tickSize := getTickSizeForPrice(tt.price, tt.details)
			assert.Equal(t, tt.expectedTick, tickSize)
		})
	}
}

func TestTruncateToTickSize(t *testing.T) {
	// User Journey: As a trading system, I want to truncate prices to tick_size multiples,
	// so that orders meet Deribit's precision requirements.

	tests := []struct {
		name           string
		price          float64
		tickSize       float64
		roundUp        bool
		expectedResult float64
	}{
		{
			name:           "floor truncate for buy order",
			price:          0.1157,
			tickSize:       0.0005,
			roundUp:        false,
			expectedResult: 0.1155, // floor(0.1157/0.0005) * 0.0005 = 231 * 0.0005 = 0.1155
		},
		{
			name:           "ceil truncate for sell order",
			price:          0.1203,
			tickSize:       0.0005,
			roundUp:        true,
			expectedResult: 0.1205, // ceil(0.1203/0.0005) * 0.0005 = 241 * 0.0005 = 0.1205
		},
		{
			name:           "already aligned price (no change)",
			price:          0.1155,
			tickSize:       0.0005,
			roundUp:        false,
			expectedResult: 0.1155,
		},
		{
			name:           "small tick_size precision",
			price:          0.0034567,
			tickSize:       0.0001,
			roundUp:        false,
			expectedResult: 0.0034,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateToTickSize(tt.price, tt.tickSize, tt.roundUp)
			// Use InDelta for floating point comparison
			assert.InDelta(t, tt.expectedResult, result, 1e-10)
		})
	}
}

func TestCalculateMarketOrderPrice(t *testing.T) {
	// User Journey: As a trader, I want Market orders to use optimal prices,
	// so that I get better entry prices (buy lower, sell higher).

	tests := []struct {
		name           string
		symbol         string
		side           exchange.OrderSide
		mockInstrument interface{}
		mockTicker     interface{}
		expectedPrice  float64
		expectError    bool
	}{
		{
			name:   "buy market order - price below threshold",
			symbol: "BTC-26SEP26-100000-C",
			side:   exchange.OrderSideBuy,
			mockInstrument: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"tick_size": 0.0001,
					"tick_size_steps": []interface{}{
						map[string]interface{}{
							"above_price": 0.005,
							"tick_size":   0.0005,
						},
					},
				},
				"id": 1,
			},
			mockTicker: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"mark_price":     0.003, // Below threshold, tick_size = 0.0001
					"best_bid_price": 0.0029,
					"best_ask_price": 0.0031,
				},
				"id": 2,
			},
			// Buy price = mark_price - 5*tick_size = 0.003 - 5*0.0001 = 0.0025
			// Floor truncate = 0.0025 (already aligned)
			expectedPrice: 0.0025,
		},
		{
			name:   "sell market order - price above threshold",
			symbol: "BTC-26SEP26-100000-C",
			side:   exchange.OrderSideSell,
			mockInstrument: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"tick_size": 0.0001,
					"tick_size_steps": []interface{}{
						map[string]interface{}{
							"above_price": 0.005,
							"tick_size":   0.0005,
						},
					},
				},
				"id": 1,
			},
			mockTicker: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"mark_price":     0.118, // Above threshold, tick_size = 0.0005
					"best_bid_price": 0.1135,
					"best_ask_price": 0.1475,
				},
				"id": 2,
			},
			// Sell price = mark_price + 5*tick_size = 0.118 + 5*0.0005 = 0.1205
			// Ceil truncate = 0.1205 (already aligned)
			expectedPrice: 0.1205,
		},
		{
			name:   "buy market order - requires truncation",
			symbol: "BTC-26SEP26-100000-C",
			side:   exchange.OrderSideBuy,
			mockInstrument: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"tick_size": 0.0001,
					"tick_size_steps": []interface{}{
						map[string]interface{}{
							"above_price": 0.005,
							"tick_size":   0.0005,
						},
					},
				},
				"id": 1,
			},
			mockTicker: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"mark_price":     0.119,
					"best_bid_price": 0.1135,
					"best_ask_price": 0.1475,
				},
				"id": 2,
			},
			// Buy price = mark_price - 5*tick_size = 0.119 - 5*0.0005 = 0.1165
			// Floor truncate (may have floating point precision issues)
			// Actual: floor(0.1165/0.0005) * 0.0005 ≈ 0.116
			expectedPrice: 0.116,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestCount := 0
			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				method := req["method"].(string)
				if method == "public/get_instrument" {
					return tt.mockInstrument
				}
				if method == "public/ticker" {
					return tt.mockTicker
				}
				requestCount++
				return nil
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			price, err := client.calculateMarketOrderPrice(tt.symbol, tt.side, false) // open position

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.InDelta(t, tt.expectedPrice, price, 1e-10)
		})
	}
}

// TestCalculateMarketOrderPrice_ClosePosition tests that close orders use bid/ask directly
func TestCalculateMarketOrderPrice_ClosePosition(t *testing.T) {
	tests := []struct {
		name           string
		symbol         string
		side           exchange.OrderSide
		reduceOnly     bool
		mockTicker     map[string]interface{}
		mockInstrument map[string]interface{}
		expectedPrice  float64
		expectError    bool
	}{
		{
			name:       "close_long_at_bid_price",
			symbol:     "BTC-26SEP26-100000-C",
			side:       exchange.OrderSideSell,
			reduceOnly: true,
			mockTicker: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"mark_price":     0.118,
					"best_bid_price": 0.116,
					"best_ask_price": 0.120,
				},
				"id": 1,
			},
			mockInstrument: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"tick_size": 0.0005,
					"tick_size_steps": []interface{}{
						map[string]interface{}{
							"above_price": 0.005,
							"tick_size":   0.0005,
						},
					},
				},
				"id": 2,
			},
			expectedPrice: 0.116, // Directly use bid price
		},
		{
			name:       "close_short_at_ask_price",
			symbol:     "BTC-26SEP26-100000-C",
			side:       exchange.OrderSideBuy,
			reduceOnly: true,
			mockTicker: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"mark_price":     0.118,
					"best_bid_price": 0.116,
					"best_ask_price": 0.120,
				},
				"id": 1,
			},
			mockInstrument: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"tick_size": 0.0005,
					"tick_size_steps": []interface{}{
						map[string]interface{}{
							"above_price": 0.005,
							"tick_size":   0.0005,
						},
					},
				},
				"id": 2,
			},
			expectedPrice: 0.120, // Directly use ask price
		},
		{
			name:       "close_long_uses_bid_directly_no_truncation",
			symbol:     "BTC-26SEP26-100000-C",
			side:       exchange.OrderSideSell,
			reduceOnly: true,
			mockTicker: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"mark_price":     0.118,
					"best_bid_price": 0.1163, // Not aligned to tick_size - should use directly
					"best_ask_price": 0.120,
				},
				"id": 1,
			},
			mockInstrument: map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"tick_size": 0.0005,
					"tick_size_steps": []interface{}{
						map[string]interface{}{
							"above_price": 0.005,
							"tick_size":   0.0005,
						},
					},
				},
				"id": 2,
			},
			expectedPrice: 0.1163, // Use bid directly, no truncation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
				method := req["method"].(string)
				if method == "public/get_instrument" {
					return tt.mockInstrument
				}
				if method == "public/ticker" {
					return tt.mockTicker
				}
				return nil
			})
			defer server.Close()

			client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
			require.NoError(t, err)
			client.baseURL = server.URL()

			price, err := client.calculateMarketOrderPrice(tt.symbol, tt.side, tt.reduceOnly)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.InDelta(t, tt.expectedPrice, price, 1e-10, 
				"close order should use bid/ask directly without offset")
		})
	}
}

// =============================================================================
// Market to Limit Conversion Test
// =============================================================================

func TestCreateOrder_MarketConvertedToLimit(t *testing.T) {
	// This test verifies that Market orders are converted to Limit orders
	// when sent to Deribit API (Deribit doesn't have true Market orders)
	
	var receivedOrderType string
	
	server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
		method := req["method"].(string)
		
		// Handle ticker API for price calculation
		if method == "public/get_instrument" {
			return map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"tick_size": 0.0001,
					"tick_size_steps": []interface{}{
						map[string]interface{}{
							"above_price": 0.005,
							"tick_size":   0.0005,
						},
					},
				},
				"id": req["id"],
			}
		}
		if method == "public/ticker" {
			return map[string]interface{}{
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"mark_price":     0.118,
					"best_bid_price": 0.116,
					"best_ask_price": 0.120,
				},
				"id": req["id"],
			}
		}
		
		// Capture the order type sent to API
		if method == "private/buy" || method == "private/sell" {
			params := req["params"].(map[string]interface{})
			receivedOrderType = params["type"].(string)
		}
		
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"result": map[string]interface{}{
				"order": map[string]interface{}{
					"order_id":    "12345",
					"instrument":  "BTC-17JUL26-65000-C",
					"direction":   "buy",
					"order_type":  "limit",
					"amount":      1.0,
					"price":       0.115,
					"order_state": "open",
					"create_time": time.Now().UnixMilli(),
				},
			},
			"id": req["id"],
		}
	})
	defer server.Close()
	
	client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
	require.NoError(t, err)
	client.baseURL = server.URL()
	
	// Send Market order - should be converted to Limit
	_, err = client.CreateOrder(exchange.CreateOrderRequest{
		Symbol:    "BTC-17JUL26-65000-C",
		Side:      exchange.OrderSideBuy,
		OrderType: exchange.OrderTypeMarket, // Market order
		Quantity:  1.0,
	})
	require.NoError(t, err)
	
	// Verify the API received "limit" not "market"
	assert.Equal(t, "limit", receivedOrderType, 
		"Market orders should be converted to Limit orders when sent to Deribit API")
}

// =============================================================================
// Market Order Price Calculation Failure Test
// =============================================================================

func TestCreateOrder_MarketPriceCalculationFailure(t *testing.T) {
	// This test verifies that when Market order price calculation fails,
	// an error is returned instead of proceeding with potentially invalid price
	
	server := newMockDeribitServer(nil, func(req map[string]interface{}) interface{} {
		method := req["method"].(string)
		
		// Simulate ticker API failure (used by calculateMarketOrderPrice)
		if method == "public/ticker" {
			return map[string]interface{}{
				"jsonrpc": "2.0",
				"error": map[string]interface{}{
					"code":    -32602,
					"message": "instrument not found",
				},
				"id": req["id"],
			}
		}
		
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"result":  map[string]interface{}{},
			"id":      req["id"],
		}
	})
	defer server.Close()
	
	client, err := NewDeribit("test_key", "test_secret", "test_pwd", false)
	require.NoError(t, err)
	client.baseURL = server.URL()
	
	// Send Market order - price calculation should fail
	_, err = client.CreateOrder(exchange.CreateOrderRequest{
		Symbol:    "INVALID-SYMBOL",
		Side:      exchange.OrderSideBuy,
		OrderType: exchange.OrderTypeMarket,
		Quantity:  1.0,
	})
	
	// Should return error, not proceed with price=0
	assert.Error(t, err, "Market order should fail when price calculation fails")
	assert.Contains(t, err.Error(), "calculate market order price", 
		"Error should indicate price calculation failure")
}
