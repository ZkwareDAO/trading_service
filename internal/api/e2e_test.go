package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
	tradesignal "trading-service/internal/signal"
)

// TestE2E_SignalFlow_FromAPIToExchange traces the full signal lifecycle:
//
//	API POST /api/v1/signals → Signal → HandleOpen → CreateUserOrder → Exchange.CreateOrder
//	Also verifies: UserOrder CSV, LeverageConfig, MockExchange call order, position count
func TestE2E_SignalFlow_FromAPIToExchange(t *testing.T) {
	// ── 1. Setup ──────────────────────────────────────────────────────
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)
	mock := exchange.NewMockExchange()
	h := tradesignal.NewHandler(repo, mock)
	srv := NewServer(repo, h)
	defer srv.Close()

	// ── 2. Seed UserStrategy (required before signal) ─────────────────
	// Matches user's strategy: leverage=2, parts=1, cash=500, status=active
	now := time.Now()
	userID := repo.CreateUser(&order.User{
		Name: "e2e_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now,
	})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:           userID,
		Name:             "OBVATRV2",
		Exchange:         "binance",
		ValidBefore:      time.Date(2030, 12, 31, 8, 0, 0, 0, time.UTC),
		Cash:             5000, // > signal cash (1000) so it passes
		Parts:            5,
		Status:           1, // active
		RiskStrategyType: "traditional",
		CreatedAt:        now,
		UpdatedAt:        now,
	})

	// ── 3. Send signal via HTTP (user's signal mapped to flat format) ─
	//   side: 0 → Long → BUY
	//   order_type: 1 → Market
	//   cash: 1000, trigger_price: 87315.1, leverage: 2, pos_type: 2 (Futures)
	sigReq := SignalRequest{
		UserID:         userID,
		UserStrategyID: usID,
		Symbol:         "BTC",
		PosType:        2, // Futures
		Exchange:       "mock",
		Cash:           1000,
		TriggerPrice:   87315.1,
		Slippage:       0,
		Side:           0, // Long → BUY
		OrderType:      1, // Market
		Leverage:       2,
	}
	bodyBytes, _ := json.Marshal(sigReq)

	resp, err := http.Post(srv.URL+"/api/v1/signals", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /api/v1/signals: %v", err)
	}
	defer resp.Body.Close()

	// ── 4. Verify API response ───────────────────────────────────────
	if resp.StatusCode != 200 {
		var errMsg map[string]string
		json.NewDecoder(resp.Body).Decode(&errMsg)
		t.Fatalf("expected 200, got %d: %v", resp.StatusCode, errMsg)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["message"] != "success" {
		t.Fatalf("expected 'success', got '%v'", result)
	}

	// ── 5. Verify UserOrder created in GlobalState ───────────────────
	if len(gs.UserOrders) != 1 {
		t.Fatalf("expected 1 user_order, got %d", len(gs.UserOrders))
	}
	var co *order.UserOrder
	for _, o := range gs.UserOrders {
		co = o
		break
	}

	t.Run("UserOrder fields", func(t *testing.T) {
		checks := []struct {
			name      string
			got, want interface{}
		}{
			{"UserID", co.UserID, userID},
			{"UserStrategyID", co.UserStrategyID, usID},
			{"PosType", co.PosType, order.PosTypeFutures},
			{"Exchange", co.Exchange, "mock"},
			{"BaseAsset", co.BaseAsset, "BTC"},
			{"QuoteAsset", co.QuoteAsset, "USDT"},
			{"Cash", co.Cash, float64(1000)},
			{"TriggerPrice", co.TriggerPrice, 87315.1},
			{"Side", co.Side, order.SideLong},
			{"OrderType", co.OrderType, 1}, // market
			{"Status", co.Status, 1},       // NEW
		}
		for _, c := range checks {
			if c.got != c.want {
				t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
			}
		}
	})

	// ── 6. Verify LeverageConfig (Futures only) ──────────────────────
	t.Run("LeverageConfig", func(t *testing.T) {
		if len(gs.LeverageConfigs) != 1 {
			t.Fatalf("expected 1 leverage_config, got %d", len(gs.LeverageConfigs))
		}
		var lc *order.LeverageConfig
		for _, l := range gs.LeverageConfigs {
			lc = l
			break
		}
		if lc.Leverage != 2 {
			t.Errorf("leverage: got %d, want 2", lc.Leverage)
		}
		if lc.Exchange != "mock" {
			t.Errorf("exchange: got %s, want mock", lc.Exchange)
		}
		if lc.PosType != order.PosTypeFutures {
			t.Errorf("posType: got %d, want %d", lc.PosType, order.PosTypeFutures)
		}
	})

	// ── 7. Verify MockExchange call order ─────────────────────────────
	t.Run("Exchange call order", func(t *testing.T) {
		callOrder := mock.CallOrder()
		if len(callOrder) < 2 {
			t.Fatalf("expected 2 calls (SetLeverage+CreateOrder), got %d: %v", len(callOrder), callOrder)
		}
		if callOrder[0] != "SetLeverage" {
			t.Errorf("call[0]: got %s, want SetLeverage", callOrder[0])
		}
		if callOrder[1] != "CreateOrder" {
			t.Errorf("call[1]: got %s, want CreateOrder", callOrder[1])
		}
	})

	// ── 8. Verify exchange order details ──────────────────────────────
	t.Run("Exchange order details", func(t *testing.T) {
		if len(mock.CreatedOrders) != 1 {
			t.Fatalf("expected 1 exchange order, got %d", len(mock.CreatedOrders))
		}
		eo := mock.CreatedOrders[0]
		if eo.Symbol != "BTCUSDT" {
			t.Errorf("symbol: got %s, want BTCUSDT", eo.Symbol)
		}
		if eo.Side != exchange.OrderSideBuy {
			t.Errorf("side: got %s, want BUY", eo.Side)
		}
		if eo.Status != exchange.OrderStatusNew {
			t.Errorf("status: got %s, want NEW", eo.Status)
		}
	})
}

// TestE2E_Signal_Rejected_MissingStrategy — signal to missing strategy → 400
func TestE2E_Signal_Rejected_MissingStrategy(t *testing.T) {
	dir := t.TempDir()
	gs, _ := persistence.NewGlobalState(dir)
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)
	mock := exchange.NewMockExchange()
	h := tradesignal.NewHandler(repo, mock)
	srv := NewServer(repo, h)
	defer srv.Close()

	sigReq := SignalRequest{
		UserID: 1, UserStrategyID: 999, Symbol: "BTC",
		Exchange: "mock", Cash: 100, TriggerPrice: 50000,
	}
	body, _ := json.Marshal(sigReq)
	resp, err := http.Post(srv.URL+"/api/v1/signals", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/signals: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestE2E_Signal_Rejected_CashExceeds — signal cash > strategy cash → 400
func TestE2E_Signal_Rejected_CashExceeds(t *testing.T) {
	dir := t.TempDir()
	gs, _ := persistence.NewGlobalState(dir)
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)
	mock := exchange.NewMockExchange()
	h := tradesignal.NewHandler(repo, mock)
	srv := NewServer(repo, h)
	defer srv.Close()

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test", CreatedAt: now, UpdatedAt: now})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "test", Exchange: "binance",
		Cash: 500, Parts: 5, Status: 1,
		CreatedAt: now, UpdatedAt: now,
	})

	sigReq := SignalRequest{
		UserID: userID, UserStrategyID: usID, Symbol: "BTC",
		Exchange: "mock", Cash: 1000, TriggerPrice: 50000,
	}
	body, _ := json.Marshal(sigReq)
	resp, err := http.Post(srv.URL+"/api/v1/signals", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/signals: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for cash exceeding strategy, got %d", resp.StatusCode)
	}
}

// TestE2E_Signal_Rejected_PartsLimit — strategy at max parts → 400
func TestE2E_Signal_Rejected_PartsLimit(t *testing.T) {
	dir := t.TempDir()
	gs, _ := persistence.NewGlobalState(dir)
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)
	mock := exchange.NewMockExchange()
	h := tradesignal.NewHandler(repo, mock)
	srv := NewServer(repo, h)
	defer srv.Close()

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test", CreatedAt: now, UpdatedAt: now})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "test", Exchange: "binance",
		Cash: 10000, Parts: 1, Status: 1,
		CreatedAt: now, UpdatedAt: now,
	})

	sigReq := SignalRequest{
		UserID: userID, UserStrategyID: usID, Symbol: "BTC",
		Exchange: "mock", Cash: 1000, TriggerPrice: 50000,
	}

	// First signal: succeeds
	body, _ := json.Marshal(sigReq)
	resp, err := http.Post(srv.URL+"/api/v1/signals", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/signals: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("first signal expected 200, got %d", resp.StatusCode)
	}

	// Second signal: parts limit (pending=1 + active=0 >= parts=1) → 400
	body, _ = json.Marshal(sigReq)
	resp2, err := http.Post(srv.URL+"/api/v1/signals", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/signals (2nd): %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 400 {
		t.Errorf("second signal expected 400 (parts limit), got %d", resp2.StatusCode)
	}
}

// TestE2E_Signal_Spot_NoLeverageConfig — Spot (pos_type=1) should NOT create LeverageConfig
func TestE2E_Signal_Spot_NoLeverageConfig(t *testing.T) {
	dir := t.TempDir()
	gs, _ := persistence.NewGlobalState(dir)
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)
	mock := exchange.NewMockExchange()
	h := tradesignal.NewHandler(repo, mock)
	srv := NewServer(repo, h)
	defer srv.Close()

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test", CreatedAt: now, UpdatedAt: now})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "test", Exchange: "binance",
		Cash: 5000, Parts: 5, Status: 1,
		CreatedAt: now, UpdatedAt: now,
	})

	// pos_type=1 (Spot), leverage should NOT be called
	sigReq := SignalRequest{
		UserID: userID, UserStrategyID: usID, Symbol: "BTC",
		PosType: 1, Exchange: "mock", Cash: 1000, TriggerPrice: 50000,
		Side: 0, OrderType: 1, Leverage: 10,
	}
	body, _ := json.Marshal(sigReq)
	resp, err := http.Post(srv.URL+"/api/v1/signals", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/signals: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errMsg map[string]string
		json.NewDecoder(resp.Body).Decode(&errMsg)
		t.Fatalf("expected 200, got %d: %v", resp.StatusCode, errMsg)
	}

	// LeverageConfig should NOT exist for Spot
	if len(gs.LeverageConfigs) != 0 {
		t.Errorf("expected 0 leverage_config for Spot, got %d", len(gs.LeverageConfigs))
	}

	// CallOrder should NOT include SetLeverage
	callOrder := mock.CallOrder()
	for _, c := range callOrder {
		if c == "SetLeverage" {
			t.Error("SetLeverage should not be called for Spot orders")
		}
	}
}

func TestE2E_NestedStrategySignal_BuyCreatesStrategyUserOrderAndExchangeOrder(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)
	mock := exchange.NewMockExchange()
	h := tradesignal.NewHandler(repo, mock)
	srv := NewServer(repo, h)
	defer srv.Close()

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "nested_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	payload := nestedSignalPayload(userID, "buy", 5000, 1000)

	resp := postNestedSignal(t, srv.URL, payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errMsg map[string]string
		json.NewDecoder(resp.Body).Decode(&errMsg)
		t.Fatalf("expected 200, got %d: %v", resp.StatusCode, errMsg)
	}

	if len(gs.Strategies) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(gs.Strategies))
	}
	var strategy *order.Strategy
	for _, s := range gs.Strategies {
		strategy = s
	}
	if strategy.Name != "OBVATRV2_4H_2_BTCUSDT" {
		t.Fatalf("expected strategy key OBVATRV2_4H_2_BTCUSDT, got %s", strategy.Name)
	}

	if len(gs.StrategyAssets) != 1 {
		t.Fatalf("expected 1 strategy_asset, got %d", len(gs.StrategyAssets))
	}
	var strategyAsset *order.StrategyAsset
	for _, asset := range gs.StrategyAssets {
		strategyAsset = asset
	}
	if strategyAsset.Name != strategy.Name || strategyAsset.Asset != "BTC" || strategyAsset.StrategyID != strategy.ID || strategyAsset.PosType != order.PosTypeFutures {
		t.Fatalf("unexpected strategy_asset: %+v", strategyAsset)
	}

	if len(gs.UserStrategies) != 1 {
		t.Fatalf("expected 1 user_strategy, got %d", len(gs.UserStrategies))
	}
	var userStrategy *order.UserStrategy
	for _, us := range gs.UserStrategies {
		userStrategy = us
	}
	if userStrategy.Cash != 5000 || userStrategy.Parts != 1 || userStrategy.RiskStrategyType != order.RiskStrategyTypeTraditional {
		t.Fatalf("unexpected user_strategy: %+v", userStrategy)
	}

	if len(gs.UserOrders) != 1 {
		t.Fatalf("expected 1 user_order, got %d", len(gs.UserOrders))
	}
	var userOrder *order.UserOrder
	for _, o := range gs.UserOrders {
		userOrder = o
	}
	if userOrder.BaseAsset != "BTC" || userOrder.QuoteAsset != "USDT" || userOrder.Side != order.SideLong {
		t.Fatalf("unexpected user_order asset/side: %+v", userOrder)
	}
	if userOrder.Cash != 1000 || userOrder.TriggerPrice != 87315.1 {
		t.Fatalf("unexpected user_order cash/price: %+v", userOrder)
	}

	callOrder := mock.CallOrder()
	if len(callOrder) < 2 || callOrder[0] != "SetLeverage" || callOrder[1] != "CreateOrder" {
		t.Fatalf("expected SetLeverage then CreateOrder, got %v", callOrder)
	}
	if len(mock.CreatedOrders) != 1 {
		t.Fatalf("expected 1 exchange order, got %d", len(mock.CreatedOrders))
	}
	if mock.CreatedOrders[0].Symbol != "BTCUSDT" || mock.CreatedOrders[0].Side != exchange.OrderSideBuy {
		t.Fatalf("unexpected exchange order: %+v", mock.CreatedOrders[0])
	}
}

func TestE2E_NestedStrategySignal_CloseWritesImmediateRule(t *testing.T) {
	tests := []struct{ action, desc string }{
		{"sell_close", "sell_close"},
		{"buy_close", "buy_close"},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			dir := t.TempDir()
			gs, err := persistence.NewGlobalState(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer gs.Shutdown()

			repo := persistence.NewStateRepository(gs)
			mock := exchange.NewMockExchange()
			h := tradesignal.NewHandlerWithDataDir(repo, mock, dir)
			srv := NewServer(repo, h)
			defer srv.Close()

			now := time.Now()
			userID := repo.CreateUser(&order.User{Name: "nested_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
			payload := nestedSignalPayload(userID, tt.action, 5000, 1000)

			resp := postNestedSignal(t, srv.URL, payload)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				var errMsg map[string]string
				json.NewDecoder(resp.Body).Decode(&errMsg)
				t.Fatalf("expected 200, got %d: %v", resp.StatusCode, errMsg)
			}
			if len(mock.CreatedOrders) != 0 {
				t.Fatalf("expected close signal not to call exchange directly, got %d orders", len(mock.CreatedOrders))
			}

			records := readRuleCSVForAPI(t, dir)
			if len(records) != 2 {
				t.Fatalf("expected header + 1 close rule, got %d records", len(records))
			}
			rule := records[1]
			if rule[2] != "always" || rule[3] != "==" || rule[4] != "true" || rule[7] != "reduce" {
				t.Fatalf("unexpected close rule: %v", rule)
			}
		})
	}
}

func TestE2E_NestedStrategySignal_CashExceedsStrategyRejectsBeforeExchange(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)
	mock := exchange.NewMockExchange()
	h := tradesignal.NewHandler(repo, mock)
	srv := NewServer(repo, h)
	defer srv.Close()

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "nested_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	payload := nestedSignalPayload(userID, "buy", 500, 1000)

	resp := postNestedSignal(t, srv.URL, payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if len(mock.CreatedOrders) != 0 {
		t.Fatalf("expected no exchange orders, got %d", len(mock.CreatedOrders))
	}
	if len(gs.UserOrders) != 0 {
		t.Fatalf("expected no user_orders, got %d", len(gs.UserOrders))
	}
}

func TestE2E_NestedStrategySignal_ExchangeSymbolFilterRejectsBeforeExchange(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)
	repo.CreateExchangeSymbolFilter(&order.ExchangeSymbolFilter{
		Exchange:    "binance",
		PosType:     order.PosTypeFutures,
		Symbol:      "BTCUSDT",
		FilterType:  "MIN_NOTIONAL",
		MinNotional: 3000,
	})
	mock := exchange.NewMockExchange()
	h := tradesignal.NewHandler(repo, mock)
	srv := NewServer(repo, h)
	defer srv.Close()

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "nested_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	payload := nestedSignalPayload(userID, "buy", 5000, 1000)

	resp := postNestedSignal(t, srv.URL, payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if len(mock.CallOrder()) != 0 || len(mock.CreatedOrders) != 0 {
		t.Fatalf("expected no exchange calls, got calls=%v orders=%d", mock.CallOrder(), len(mock.CreatedOrders))
	}
}

func TestE2E_NestedStrategySignal_ReverseWritesCloseRuleThenOpensCorrectSide(t *testing.T) {
	tests := []struct {
		action   string
		wantSide exchange.OrderSide
	}{
		{"reverse_long", exchange.OrderSideBuy},
		{"reverse_short", exchange.OrderSideSell},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			dir := t.TempDir()
			gs, err := persistence.NewGlobalState(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer gs.Shutdown()

			repo := persistence.NewStateRepository(gs)
			mock := exchange.NewMockExchange()
			fakeClient := &mockPositionQueryClient{count: 0}
			h := tradesignal.NewHandlerWithDataDirAndPositionClient(repo, tradesignal.InitFactoryForTest(mock), dir, fakeClient, nil)
			srv := NewServer(repo, h)
			defer srv.Close()

			now := time.Now()
			userID := repo.CreateUser(&order.User{Name: "nested_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
			payload := nestedSignalPayload(userID, tt.action, 5000, 1000)

			resp := postNestedSignal(t, srv.URL, payload)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				var errMsg map[string]string
				json.NewDecoder(resp.Body).Decode(&errMsg)
				t.Fatalf("expected 200, got %d: %v", resp.StatusCode, errMsg)
			}

			// Verify close rule was written
			records := readRuleCSVForAPI(t, dir)
			if len(records) != 2 {
				t.Fatalf("expected header + 1 close rule, got %d records", len(records))
			}
			if records[1][2] != "always" || records[1][4] != "true" || records[1][7] != "reduce" {
				t.Fatalf("unexpected close rule: %v", records[1])
			}

			// Verify reverse opened correct side via exchange
			if len(mock.CreatedOrders) != 1 {
				t.Fatalf("expected 1 open order after close rule, got %d", len(mock.CreatedOrders))
			}
			if mock.CreatedOrders[0].Side != tt.wantSide {
				t.Fatalf("expected %s to open %s, got %s", tt.action, tt.wantSide, mock.CreatedOrders[0].Side)
			}
		})
	}
}

// mockPositionQueryClient implements tradesignal.PositionQueryClient for E2E tests
type mockPositionQueryClient struct {
	count int
}

func (m *mockPositionQueryClient) QueryUserOrderPositions(ctx context.Context, req rpc.QueryUserOrderPositionsRequest) (*rpc.QueryUserOrderPositionsResponse, error) {
	return &rpc.QueryUserOrderPositionsResponse{Count: m.count}, nil
}

func (m *mockPositionQueryClient) GetMarketPrice(ctx context.Context, req rpc.GetMarketPriceRequest) (*rpc.GetMarketPriceResponse, error) {
	// Return a fixed test price for E2E tests
	return &rpc.GetMarketPriceResponse{
		Exchange: req.Exchange,
		Symbol:   req.Symbol,
		Price:    45000.50, // Test market price
	}, nil
}

func (m *mockPositionQueryClient) InvalidateRulesForStrategy(ctx context.Context, strategyID uint64) error {
	return nil
}

func (m *mockPositionQueryClient) CreateUprunningOrder(ctx context.Context, req rpc.CreateUprunningOrderRequest) (*rpc.CreateUprunningOrderResponse, error) {
	// Mock success for E2E tests - return a fake order ID
	return &rpc.CreateUprunningOrderResponse{UprunningOrderID: 12345}, nil
}

func (m *mockPositionQueryClient) CreateRule(ctx context.Context, req rpc.CreateRuleRequest) (*rpc.CreateRuleResponse, error) {
	// Return a mock rule ID for testing
	return &rpc.CreateRuleResponse{
		Success: true,
		RuleID:  999,
	}, nil
}

func nestedSignalPayload(userID uint64, action string, strategyCash, signalCash float64) map[string]interface{} {
	return map[string]interface{}{
		"SignalID":           "7ce0f619-86d7-4aae-ac50-bcd147b99049",
		"SignalTimestamp":    "2026-04-15 12:16:05",
		"symbol":             "BTCUSDT",
		"pos_type":           2,
		"strategy_type":      "CTAFutureFactory",
		"risk_strategy_type": "traditional",
		"strategy": map[string]interface{}{
			"name":         "OBVATRV2",
			"leverage":     2,
			"version":      "2",
			"internal":     "4h",
			"description":  "cta_trend_strength_001 strategy",
			"params":       map[string]interface{}{"StopLossThreshold": 0.02},
			"valid_before": "2030-12-31 08:00:00",
			"cash":         strategyCash,
			"parts":        1,
		},
		"user_id": userID,
		"signal": map[string]interface{}{
			"side":          0,
			"action":        action,
			"exchange":      "binance",
			"valid_before":  "2030-06-30 20:16:05",
			"quantity":      nil,
			"cash":          signalCash,
			"trigger_price": 87315.1,
			"slippage":      0,
			"order_type":    1,
		},
	}
}

func postNestedSignal(t *testing.T, serverURL string, payload map[string]interface{}) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal nested signal: %v", err)
	}
	resp, err := http.Post(serverURL+"/api/v1/signals", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/signals: %v", err)
	}
	return resp
}

func readRuleCSVForAPI(t *testing.T, dir string) [][]string {
	t.Helper()
	file, err := os.Open(filepath.Join(dir, "rule.csv"))
	if err != nil {
		t.Fatalf("open rule.csv: %v", err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("read rule.csv: %v", err)
	}
	return records
}
