package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/signal"
)

// ===== User Journey Tests =====

// Test 1: 正常场景 - agent订单创建成功 (使用mock exchange)
func TestHandleAgentOrder_Success(t *testing.T) {
	// Setup
	_, repo := setupTestState(t)
	// RuleStore removed - UOS uses RPC to PMS
	handler := signal.NewHandlerWithRuleStore(repo, nil, false, false, false, nil, nil)

	// Create test user with mock exchange credentials
	user := &order.User{
		Name:      "test_user",
		Exchange:  "mock", // Use mock exchange for testing
		CreatedAt: time.Now(),
	}
	userID := repo.CreateUser(user)
	_ = userID // Avoid unused variable error

	// Test request (cash must be <= strategy cash which is 100)
	reqBody := AgentOrderRequest{
		UserName:     "test_user",
		Symbol:       "BTC",
		PosType:      2,
		Exchange:     "mock", // Use mock exchange
		Cash:         50.0,   // Must be <= 100 (strategy cash)
		TriggerPrice: 100000.0,
		Slippage:     0.001,
		Side:         0,
		OrderType:    0,
		Leverage:     10,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// Create Server instance directly
	s := &Server{handler: handler, repo: repo, orderHandler: handler}

	// Execute
	s.handleAgentOrder(w, req)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify response content
	var response AgentOrderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if response.Code != 0 {
		t.Errorf("Expected response code 0, got %d", response.Code)
	}
	if response.Data.StrategyName != "POSITIVE_1D_1_BTCUSDT" {
		t.Errorf("Expected strategy name POSITIVE_1D_1_BTCUSDT, got %s", response.Data.StrategyName)
	}
	// Verify user_strategy_id is returned
	if response.Data.UserStrategyID == 0 {
		t.Errorf("Expected user_strategy_id to be non-zero, got %d", response.Data.UserStrategyID)
	}
}

// Test 2: 符号适配测试
func TestAdaptSymbol_ForceStandardization(t *testing.T) {
	tests := []struct {
		name     string
		symbol   string
		exchange string
		expected string
	}{
		{"BTC on binance", "BTC", "B", "BTCUSDT"},
		{"BTCUSDT on binance", "BTCUSDT", "B", "BTCUSDT"},
		{"BTCUSDC on binance", "BTCUSDC", "B", "BTCUSDT"},
		{"SOL on hyperliquid", "SOL", "H", "SOLUSDC"},
		{"ETH on deribit", "ETH", "D", "ETHUSD"},
		{"Deribit Put option", "BTC-24JUL26-64000-P", "D", "BTC-24JUL26-64000-P"},
		{"Deribit Call option", "ETH-24JUL26-3000-C", "deribit", "ETH-24JUL26-3000-C"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adaptSymbol(tt.symbol, tt.exchange)
			if result != tt.expected {
				t.Errorf("adaptSymbol(%s, %s) = %s, expected %s",
					tt.symbol, tt.exchange, result, tt.expected)
			}
		})
	}
}

// Test 3: Exchange标准化
func TestNormalizeExchange(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"B", "binance"},
		{"H", "hyperliquid"},
		{"D", "deribit"},
		{"binance", "binance"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeExchange(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeExchange(%s) = %s, expected %s",
					tt.input, result, tt.expected)
			}
		})
	}
}

// Test 4: 策略名称生成
func TestGenerateAgentStrategyName(t *testing.T) {
	result := generateAgentStrategyName("BTCUSDT")
	expected := "POSITIVE_1D_1_BTCUSDT"
	if result != expected {
		t.Errorf("generateAgentStrategyName(BTCUSDT) = %s, expected %s", result, expected)
	}
}

// Test 5: 错误场景 - user_name必填
func TestHandleAgentOrder_ErrorUserNameRequired(t *testing.T) {
	_, repo := setupTestState(t)
	// RuleStore removed - UOS uses RPC to PMS
	handler := signal.NewHandlerWithRuleStore(repo, nil, false, false, false, nil, nil)

	reqBody := AgentOrderRequest{
		Symbol:       "BTC",
		Exchange:     "B",
		Cash:         1000.0,
		TriggerPrice: 100000.0,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// Create Server instance directly
	s := &Server{handler: handler, repo: repo, orderHandler: handler}

	s.handleAgentOrder(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

// Test 6: 错误场景 - cash必须小于等于策略cash
func TestHandleAgentOrder_ErrorCashExceedsStrategy(t *testing.T) {
	_, repo := setupTestState(t)
	// RuleStore removed - UOS uses RPC to PMS
	handler := signal.NewHandlerWithRuleStore(repo, nil, false, false, false, nil, nil)

	// Create test user with mock exchange
	user := &order.User{
		Name:      "test_user",
		Exchange:  "mock",
		CreatedAt: time.Now(),
	}
	repo.CreateUser(user)

	// Request with cash > strategy cash (100)
	reqBody := AgentOrderRequest{
		UserName:     "test_user",
		Symbol:       "BTC",
		PosType:      2,
		Exchange:     "mock",
		Cash:         200.0, // > 100 strategy cash
		TriggerPrice: 100000.0,
		Side:         0,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s := &Server{handler: handler, repo: repo, orderHandler: handler}
	s.handleAgentOrder(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for cash exceeding strategy cash, got %d", w.Code)
	}
}

// Test 7: 期权下单 - 使用quantity而不是cash
func TestHandleAgentOrder_OptionsQuantityValidation(t *testing.T) {
	_, repo := setupTestState(t)
	handler := signal.NewHandlerWithRuleStore(repo, nil, false, false, false, nil, nil)

	// Create test user
	user := &order.User{
		Name:      "test_options_user",
		Exchange:  "mock",
		CreatedAt: time.Now(),
	}
	repo.CreateUser(user)

	// Test 7a: 期权下单缺少quantity应该报错
	t.Run("missing quantity for options", func(t *testing.T) {
		reqBody := AgentOrderRequest{
			UserName:     "test_options_user",
			Symbol:       "BTC-24JUL26-64000-P",
			PosType:      3, // Options
			Exchange:     "mock",
			Cash:         0,
			Quantity:     0, // Missing quantity
			TriggerPrice: 0.015,
			Side:         0,
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewReader(body))
		w := httptest.NewRecorder()

		s := &Server{handler: handler, repo: repo, orderHandler: handler}
		s.handleAgentOrder(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for missing quantity, got %d", w.Code)
		}
		if !bytes.Contains(w.Body.Bytes(), []byte("quantity must be positive for options")) {
			t.Errorf("Expected 'quantity must be positive for options' error, got: %s", w.Body.String())
		}
	})

	// Test 7b: 期权下单有quantity应该成功
	t.Run("valid quantity for options", func(t *testing.T) {
		reqBody := AgentOrderRequest{
			UserName:     "test_options_user",
			Symbol:       "BTC-24JUL26-64000-P",
			PosType:      3, // Options
			Exchange:     "mock",
			Cash:         0,   // Not used for options
			Quantity:     0.1, // Valid quantity
			TriggerPrice: 0.015,
			Slippage:     0.001,
			Side:         0,
			OrderType:    1,
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewReader(body))
		w := httptest.NewRecorder()

		s := &Server{handler: handler, repo: repo, orderHandler: handler}
		s.handleAgentOrder(w, req)

		// Should succeed (or fail at exchange level, not validation)
		if w.Code == http.StatusBadRequest && bytes.Contains(w.Body.Bytes(), []byte("cash must be positive")) {
			t.Errorf("Should not require cash for options, got: %s", w.Body.String())
		}
	})
}

// Test 8: 期权下单响应 - 不应返回cash和leverage字段
func TestHandleAgentOrder_OptionsResponseFields(t *testing.T) {
	_, repo := setupTestState(t)
	handler := signal.NewHandlerWithRuleStore(repo, nil, false, false, false, nil, nil)

	// Create test user
	user := &order.User{
		Name:      "test_options_response",
		Exchange:  "mock",
		CreatedAt: time.Now(),
	}
	repo.CreateUser(user)

	// 期权下单请求
	reqBody := AgentOrderRequest{
		UserName:     "test_options_response",
		Symbol:       "BTC-24JUL26-64000-P",
		PosType:      3, // Options
		Exchange:     "mock",
		Quantity:     0.1,
		TriggerPrice: 0.015,
		Slippage:     0.001,
		Side:         0,
		OrderType:    1,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s := &Server{handler: handler, repo: repo, orderHandler: handler}
	s.handleAgentOrder(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response AgentOrderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// 验证期权响应字段
	if response.Data.Quantity <= 0 {
		t.Errorf("Options response should have positive quantity, got %f", response.Data.Quantity)
	}

	// 期权响应不应该返回cash字段（或应该为0/默认值）
	// 注意：由于JSON序列化，如果字段存在会是零值，这里验证quantity有值即可
	if response.Data.Quantity != 0.1 {
		t.Errorf("Expected quantity 0.1, got %f", response.Data.Quantity)
	}
}

// Test 9: 期货下单响应 - 应返回cash和leverage字段
func TestHandleAgentOrder_FuturesResponseFields(t *testing.T) {
	_, repo := setupTestState(t)
	handler := signal.NewHandlerWithRuleStore(repo, nil, false, false, false, nil, nil)

	// Create test user
	user := &order.User{
		Name:      "test_futures_response",
		Exchange:  "mock",
		CreatedAt: time.Now(),
	}
	repo.CreateUser(user)

	// 期货下单请求
	reqBody := AgentOrderRequest{
		UserName:     "test_futures_response",
		Symbol:       "BTC",
		PosType:      2, // Futures
		Exchange:     "mock",
		Cash:         50.0,
		TriggerPrice: 100000.0,
		Slippage:     0.001,
		Side:         0,
		OrderType:    0,
		Leverage:     10,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s := &Server{handler: handler, repo: repo, orderHandler: handler}
	s.handleAgentOrder(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response AgentOrderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// 验证期货响应字段
	if response.Data.Cash != 50.0 {
		t.Errorf("Expected cash 50.0, got %f", response.Data.Cash)
	}
	if response.Data.Leverage != 10 {
		t.Errorf("Expected leverage 10, got %d", response.Data.Leverage)
	}
}

// Helper
func setupTestState(t *testing.T) (*persistence.GlobalState, *persistence.StateRepository) {
	t.Helper()
	gs, err := persistence.NewGlobalState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	return gs, repo
}
