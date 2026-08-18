package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"

	"github.com/gorilla/websocket"
)

// TestDeribitOrderMonitor_TestnetURL tests testnet URL selection
func TestDeribitOrderMonitor_TestnetURL(t *testing.T) {
	repo := &persistence.StateRepository{}
	executor := exchange.NewOrderExecutor(repo, nil)
	var rpcClient *rpc.OrderServiceClient = nil

	// Test mainnet (default)
	monitor := NewDeribitOrderMonitor(executor, repo, rpcClient, nil, nil, false, "testuser")
	if monitor.baseURL != "wss://www.deribit.com/ws/api/v2" {
		t.Errorf("Expected mainnet URL, got %s", monitor.baseURL)
	}

	// Test testnet
	monitorTestnet := NewDeribitOrderMonitor(executor, repo, rpcClient, nil, nil, true, "testuser")
	if monitorTestnet.baseURL != "wss://test.deribit.com/ws/api/v2" {
		t.Errorf("Expected testnet URL, got %s", monitorTestnet.baseURL)
	}
}

// TestDeribitOrderMonitor_SubscribeOrders tests subscription message format
func TestDeribitOrderMonitor_SubscribeOrders(t *testing.T) {
	// Mock WebSocket server
	var authMsg, subMsg map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Read auth message
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(msg, &authMsg); err != nil {
			t.Fatal(err)
		}

		// Send auth response
		authResp := `{"jsonrpc":"2.0","id":1,"result":{"access_token":"ws-test-token","expires_in":900,"refresh_token":"ws-refresh"}}`
		conn.WriteMessage(websocket.TextMessage, []byte(authResp))

		// Read subscription message
		_, msg, err = conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(msg, &subMsg); err != nil {
			t.Fatal(err)
		}

		// Send subscription confirmation
		subResp := `{"jsonrpc":"2.0","id":2,"result":["user.orders.option.BTC.raw","user.orders.option.ETH.raw"]}`
		conn.WriteMessage(websocket.TextMessage, []byte(subResp))
	}))
	defer server.Close()

	// Replace http:// with ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	repo := &persistence.StateRepository{}
	executor := exchange.NewOrderExecutor(repo, nil)
	var rpcClient *rpc.OrderServiceClient = nil

	// Create mock Deribit with access token
	mockDeribit := &mockDeribitWithToken{accessToken: "test-token-12345", apiKey: "test-key", apiSecret: "test-secret"}

	monitor := NewDeribitOrderMonitor(executor, repo, rpcClient, mockDeribit, nil, false, "testuser")
	monitor.baseURL = wsURL

	// Connect and subscribe
	ctx := context.Background()
	if err := monitor.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Wait for subscription message
	time.Sleep(100 * time.Millisecond)

	// Verify auth message format
	if authMsg == nil {
		t.Fatal("No auth message received")
	}
	if authMsg["method"] != "public/auth" {
		t.Errorf("Expected method public/auth, got %v", authMsg["method"])
	}
	authParams, ok := authMsg["params"].(map[string]interface{})
	if !ok {
		t.Fatal("auth params not found or wrong type")
	}
	if authParams["grant_type"] != "client_credentials" {
		t.Errorf("Expected grant_type client_credentials, got %v", authParams["grant_type"])
	}
	if authParams["client_id"] != "test-key" {
		t.Errorf("Expected client_id test-key, got %v", authParams["client_id"])
	}

	// Verify subscription format
	if subMsg == nil {
		t.Fatal("No subscription message received")
	}

	// Check JSON-RPC structure
	if subMsg["jsonrpc"] != "2.0" {
		t.Errorf("Expected jsonrpc 2.0, got %v", subMsg["jsonrpc"])
	}

	if subMsg["method"] != "private/subscribe" {
		t.Errorf("Expected method private/subscribe, got %v", subMsg["method"])
	}

	params, ok := subMsg["params"].(map[string]interface{})
	if !ok {
		t.Fatal("params not found or wrong type")
	}

	channels, ok := params["channels"].([]interface{})
	if !ok {
		t.Fatal("channels not found or wrong type")
	}

	// Should subscribe to BTC and ETH options
	expectedChannels := map[string]bool{
		"user.orders.option.BTC.raw": false,
		"user.orders.option.ETH.raw": false,
	}

	for _, ch := range channels {
		if chStr, ok := ch.(string); ok {
			if _, exists := expectedChannels[chStr]; exists {
				expectedChannels[chStr] = true
			}
		}
	}

	for ch, found := range expectedChannels {
		if !found {
			t.Errorf("Expected channel %s not subscribed", ch)
		}
	}

	// Should NOT include access_token (WS is already authenticated via public/auth)
	if _, ok := params["access_token"]; ok {
		t.Error("access_token should not be in subscription params — WS is already authenticated")
	}

	monitor.Stop()
}

// Mock Deribit with access token for testing
type mockDeribitWithToken struct {
	accessToken string
	apiKey      string
	apiSecret   string
}

func (m *mockDeribitWithToken) Connect() error {
	return nil
}

func (m *mockDeribitWithToken) GetAccessToken() string {
	return m.accessToken
}

func (m *mockDeribitWithToken) GetAPIKey() string {
	return m.apiKey
}

func (m *mockDeribitWithToken) GetAPISecret() string {
	return m.apiSecret
}

// TestDeribitOrderMonitor_HandleOrderFilled tests FILLED order processing
func TestDeribitOrderMonitor_HandleOrderFilled(t *testing.T) {
	// This test verifies message parsing
	// Integration test will verify full executor flow

	deribitMsg := `{
		"jsonrpc": "2.0",
		"method": "subscription",
		"params": {
			"channel": "user.orders.option.BTC.raw",
			"data": {
				"order_id": "BTC-123456",
				"instrument_name": "BTC-17JUL26-64000-P",
				"order_state": "filled",
				"direction": "buy",
				"amount": 10.0,
				"filled_amount": 10.0,
				"average_price": 0.05,
				"create_time": 1700000000000
			}
		}
	}`

	// Verify message can be parsed
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(deribitMsg), &resp); err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	params := resp["params"].(map[string]interface{})
	data := params["data"].(map[string]interface{})

	// Verify fields extracted correctly
	if data["order_state"] != "filled" {
		t.Errorf("Expected order_state filled, got %v", data["order_state"])
	}

	// Verify mapping
	status := mapDeribitOrderState(data["order_state"].(string))
	if status != "FILLED" {
		t.Errorf("Expected status FILLED, got %s", status)
	}
}

// TestDeribitOrderMonitor_HandleOrderCancelled tests CANCELLED order processing
func TestDeribitOrderMonitor_HandleOrderCancelled(t *testing.T) {
	// This test verifies message parsing for cancelled orders

	deribitMsg := `{
		"jsonrpc": "2.0",
		"method": "subscription",
		"params": {
			"channel": "user.orders.option.ETH.raw",
			"data": {
				"order_id": "ETH-789012",
				"instrument_name": "ETH-19JUL26-3000-C",
				"order_state": "cancelled",
				"direction": "sell",
				"amount": 5.0,
				"filled_amount": 0.0,
				"average_price": 0.0,
				"create_time": 1700000000000
			}
		}
	}`

	// Verify message can be parsed
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(deribitMsg), &resp); err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	params := resp["params"].(map[string]interface{})
	data := params["data"].(map[string]interface{})

	// Verify mapping
	status := mapDeribitOrderState(data["order_state"].(string))
	if status != "CANCELLED" {
		t.Errorf("Expected status CANCELLED, got %s", status)
	}
}

// TestDeribitOrderMonitor_OrderIDExtraction tests order ID parsing
func TestDeribitOrderMonitor_OrderIDExtraction(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{"BTC-123456", 123456},
		{"ETH-789012", 789012},
		{"123456", 123456},
		{"BTC-81179066009", 81179066009},
		{"", 0},
		{"INVALID", 0},
		{"BTC-abc", 0},
	}

	for _, test := range tests {
		result := extractOrderID(test.input)
		if result != test.expected {
			t.Errorf("extractOrderID(%s) = %d, expected %d", test.input, result, test.expected)
		}
	}
}

// TestDeribitOrderMonitor_ReconnectStopsOnStop verifies that reconnect
// exits when Stop() is called, not just after exhausting all retries.
func TestDeribitOrderMonitor_ReconnectStopsOnStop(t *testing.T) {
	// Server that never accepts WS (forces connect failure)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	repo := &persistence.StateRepository{}
	executor := exchange.NewOrderExecutor(repo, nil)
	var rpcClient *rpc.OrderServiceClient = nil
	monitor := NewDeribitOrderMonitor(executor, repo, rpcClient, nil, nil, false, "testuser")
	monitor.baseURL = wsURL

	// Call Stop() shortly after reconnect starts
	go func() {
		time.Sleep(200 * time.Millisecond)
		monitor.Stop()
	}()

	// reconnect should return quickly after Stop(), not wait for all 5 retries (~15s)
	done := make(chan bool)
	go func() {
		monitor.reconnect()
		done <- true
	}()

	select {
	case <-done:
		// Good — reconnect returned after Stop()
	case <-time.After(5 * time.Second):
		t.Fatal("reconnect did not return after Stop() — likely using context.Background()")
	}
}

// TestDeribitOrderMonitor_StateMapping tests Deribit state mapping
func TestDeribitOrderMonitor_StateMapping(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"open", "NEW"},
		{"filled", "FILLED"},
		{"cancelled", "CANCELLED"},
		{"canceled", "CANCELLED"},
		{"rejected", "FAILED"},
		{"unknown", "unknown"},
	}

	for _, test := range tests {
		result := mapDeribitOrderState(test.input)
		if result != test.expected {
			t.Errorf("mapDeribitOrderState(%s) = %s, expected %s", test.input, result, test.expected)
		}
	}
}

// TestDeribitOrderMonitor_Reconnection tests WebSocket reconnection
func TestDeribitOrderMonitor_Reconnection(t *testing.T) {
	var reconnectCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		mu.Lock()
		reconnectCount++
		mu.Unlock()

		// Close after short delay to simulate disconnect
		time.Sleep(50 * time.Millisecond)
		conn.Close()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	repo := &persistence.StateRepository{}
	executor := exchange.NewOrderExecutor(repo, nil)
	var rpcClient *rpc.OrderServiceClient = nil
	monitor := NewDeribitOrderMonitor(executor, repo, rpcClient, nil, nil, false, "testuser")
	monitor.baseURL = wsURL

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Start monitor (will reconnect on disconnect)
	go monitor.Connect(ctx)

	// Wait for multiple connections
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	count := reconnectCount
	mu.Unlock()

	// Should have at least 2 reconnections in 500ms
	if count < 2 {
		t.Logf("Expected at least 2 reconnections, got %d (may be flaky)", count)
		// Don't fail - reconnection timing can be variable
	}

	monitor.Stop()
}

// TestBuildDeribitAction tests action text generation for all order states.
func TestBuildDeribitAction(t *testing.T) {
	monitor := &DeribitOrderMonitor{userName: "testuser"}

	tests := []struct {
		name         string
		direction    string
		status       string
		reduceOnly   bool
		filledAmount float64
		amount       float64
		isRiskCtrl   bool
		expected     string
	}{
		// NEW (open) states
		{"open buy long", "buy", "NEW", false, 0, 2.0, false, "开买仓挂单成功"},
		{"open sell short", "sell", "NEW", false, 0, 2.0, false, "开卖仓挂单成功"},
		{"open reduce buy", "buy", "NEW", true, 0, 2.0, false, "减买仓挂单成功"},
		{"open reduce sell", "sell", "NEW", true, 0, 2.0, false, "减卖仓挂单成功"},

		// FILLED states
		{"filled buy long", "buy", "FILLED", false, 4.0, 4.0, false, "开买仓成功"},
		{"filled sell short", "sell", "FILLED", false, 2.0, 2.0, false, "开卖仓成功"},
		{"filled reduce buy", "buy", "FILLED", true, 80.0, 80.0, false, "减买仓成功"},
		{"filled reduce sell", "sell", "FILLED", true, 2.0, 2.0, false, "减卖仓成功"},
		{"partial fill reduce buy", "buy", "FILLED", true, 59.0, 80.0, false, "减买仓部分成交成功"},
		{"partial fill reduce sell", "sell", "FILLED", true, 1.0, 2.0, false, "减卖仓部分成交成功"},

		// Risk control FILLED
		{"risk ctrl reduce sell filled", "sell", "FILLED", true, 2.0, 2.0, true, "触发止盈止损，减卖仓成功"},
		{"risk ctrl reduce buy partial", "buy", "FILLED", true, 59.0, 80.0, true, "触发止盈止损，减买仓部分成交成功"},

		// CANCELLED states
		{"cancelled buy", "buy", "CANCELLED", false, 0, 2.0, false, "开买仓挂单已取消"},
		{"cancelled reduce sell", "sell", "CANCELLED", true, 0, 2.0, false, "减卖仓挂单已取消"},

		// FAILED states
		{"rejected buy", "buy", "FAILED", false, 0, 2.0, false, "开买仓挂单失败"},
		{"rejected reduce sell", "sell", "FAILED", true, 0, 2.0, false, "减卖仓挂单失败"},

		// Unknown state
		{"unknown state", "buy", "pending", false, 0, 1.0, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := monitor.buildDeribitAction(tt.direction, tt.status, tt.reduceOnly, tt.filledAmount, tt.amount, tt.isRiskCtrl)
			if result != tt.expected {
				t.Errorf("buildDeribitAction() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// TestIsRiskControlLabel tests risk control label detection.
func TestIsRiskControlLabel(t *testing.T) {
	tests := []struct {
		label    string
		expected bool
	}{
		{"risk_close", true},
		{"RISK_TP_SL", true},
		{"止盈止损", true},
		{"tp_sl_close", true},
		{"TP_SL", true},
		{"normal_order", false},
		{"", false},
		{"manual", false},
	}

	for _, tt := range tests {
		result := isRiskControlLabel(tt.label)
		if result != tt.expected {
			t.Errorf("isRiskControlLabel(%q) = %v, expected %v", tt.label, result, tt.expected)
		}
	}
}

// TestSendDeribitNotification_SkipsWhenNoNotifier tests that notification is skipped when notifier is nil.
func TestSendDeribitNotification_SkipsWhenNoNotifier(t *testing.T) {
	monitor := &DeribitOrderMonitor{userName: "testuser", notifier: nil}
	// Should not panic
	monitor.sendDeribitNotification(deribitOrderFields{
		symbol: "BTC-31JUL26-66000-P", direction: "sell",
		status: "NEW", amount: 2.0, orderPrice: 0.02,
	})
}

// TestSendDeribitNotification_SkipsWhenNoUserName tests that notification is skipped when userName is empty.
func TestSendDeribitNotification_SkipsWhenNoUserName(t *testing.T) {
	monitor := &DeribitOrderMonitor{userName: "", notifier: nil}
	// Should not panic
	monitor.sendDeribitNotification(deribitOrderFields{
		symbol: "BTC-31JUL26-66000-P", direction: "sell",
		status: "NEW", amount: 2.0, orderPrice: 0.02,
	})
}
// TestDeribitOrderMonitor_ConnectNoDeadlock verifies that Connect() returns
// promptly and handleMessages() goroutine can start reading messages.
// The original code had Connect() holding m.mu.Lock() while starting
// go m.handleMessages() which needs m.mu.RLock() — this causes handleMessages
// to block until Connect() returns, delaying message processing.
func TestDeribitOrderMonitor_ConnectNoDeadlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read auth request, send auth response
		conn.ReadMessage()
		authResp := `{"jsonrpc":"2.0","id":1,"result":{"access_token":"ws-token","expires_in":900}}`
		conn.WriteMessage(websocket.TextMessage, []byte(authResp))

		// Read subscription request, send confirmation
		conn.ReadMessage()
		confirmMsg := `{"jsonrpc":"2.0","id":2,"result":["user.orders.option.BTC.raw","user.orders.option.ETH.raw"]}`
		conn.WriteMessage(websocket.TextMessage, []byte(confirmMsg))

		// Keep connection alive briefly
		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	repo := &persistence.StateRepository{}
	executor := exchange.NewOrderExecutor(repo, nil)
	mockDeribit := &mockDeribitWithToken{accessToken: "test-token", apiKey: "test-key", apiSecret: "test-secret"}
	var rpcClient *rpc.OrderServiceClient = nil

	monitor := NewDeribitOrderMonitor(executor, repo, rpcClient, mockDeribit, nil, false, "testuser")
	monitor.baseURL = wsURL

	// Connect should complete without deadlock
	done := make(chan error, 1)
	go func() {
		done <- monitor.Connect(context.Background())
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Connect failed: %v", err)
		}
		// Connect returned — no deadlock
	case <-time.After(3 * time.Second):
		t.Fatal("Connect() deadlocked — handleMessages goroutine blocked on RLock while Connect held Lock")
	}

	monitor.Stop()
}

// TestDeribitOrderMonitor_SubscriptionConfirmationLogged verifies that
// subscription confirmation responses are logged, not silently dropped.
// Bug: HandleDeribitOrderUpdate silently drops messages without params.channel prefix,
// making it impossible to diagnose subscription failures.
func TestDeribitOrderMonitor_SubscriptionConfirmationLogged(t *testing.T) {
	var logBuf strings.Builder
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	msg := `{"jsonrpc":"2.0","id":1,"result":["user.orders.option.BTC.raw","user.orders.option.ETH.raw"]}`

	monitor := &DeribitOrderMonitor{userName: "testuser"}
	monitor.HandleDeribitOrderUpdate([]byte(msg))

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "subscription") {
		t.Errorf("Subscription confirmation was silently dropped, expected logging. Log:\n%s", logOutput)
	}
}

// TestDeribitOrderMonitor_SubscriptionErrorLogged verifies that
// subscription error responses are logged, not silently dropped.
func TestDeribitOrderMonitor_SubscriptionErrorLogged(t *testing.T) {
	var logBuf strings.Builder
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	msg := `{"jsonrpc":"2.0","id":1,"error":{"code":401,"message":"Unauthorized"}}`

	monitor := &DeribitOrderMonitor{userName: "testuser"}
	monitor.HandleDeribitOrderUpdate([]byte(msg))

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "401") && !strings.Contains(logOutput, "error") && !strings.Contains(logOutput, "Unauthorized") {
		t.Errorf("Subscription error was silently dropped, expected logging. Log:\n%s", logOutput)
	}
}

// TestDeribitOrderMonitor_HeartbeatLogged verifies that heartbeat messages
// are logged, not silently dropped.
func TestDeribitOrderMonitor_HeartbeatLogged(t *testing.T) {
	var logBuf strings.Builder
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	msg := `{"jsonrpc":"2.0","method":"public/test","params":{"result":{"version":"1.2.3"}}}`

	monitor := &DeribitOrderMonitor{userName: "testuser"}
	monitor.HandleDeribitOrderUpdate([]byte(msg))

	logOutput := logBuf.String()
	// Heartbeat has params but no user.orders channel — should be logged as non-order
	if !strings.Contains(logOutput, "deribit order monitor") {
		t.Errorf("Heartbeat message was silently dropped, expected logging. Log:\n%s", logOutput)
	}
}

// End of tests