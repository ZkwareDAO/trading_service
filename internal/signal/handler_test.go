package signal

import (
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

func setupHandler(t *testing.T) (*Handler, *exchange.MockExchange, *persistence.GlobalState) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	mock := exchange.NewMockExchange()
	return NewHandler(repo, mock), mock, gs
}

func TestNewHandler(t *testing.T) {
	h, _, _ := setupHandler(t)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	// Handler no longer has factory - creates exchange dynamically per request
}

func TestHandleSignal_CreateOrder(t *testing.T) {
	h, mock, gs := setupHandler(t)
	mock.SetPrice("BTCUSDT", 50000)

	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name: "test_user", Exchange: "mock",
		CreatedAt: now, UpdatedAt: now,
	})

	usID := h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "ICT_1H", Exchange: "mock",
		Cash: 1000, Parts: 5, Status: 1,
		RiskStrategyType: order.RiskStrategyTypeCtaIntraday,
		CreatedAt:        now, UpdatedAt: now,
	})

	sig := Signal{
		UserID:         userID,
		Symbol:         "BTC",
		UserStrategyID: usID,
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Cash:           100,
		TriggerPrice:   50000,
		Slippage:       0.01,
		Side:           int(order.SideLong),
		OrderType:      0, // limit order
	}

	err := h.HandleSignal(sig)
	if err != nil {
		t.Fatalf("HandleSignal: %v", err)
	}

	if len(mock.CreatedOrders) != 1 {
		t.Errorf("expected 1 exchange order, got %d", len(mock.CreatedOrders))
	}

	// Shutdown to ensure pending writes complete before temp dir cleanup
	gs.Shutdown()
}

func TestHandleOpen_TruncatesOrderToExchangeFilters(t *testing.T) {
	h, mock, gs := setupHandler(t)
	defer gs.Shutdown()

	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name:      "test_user",
		Exchange:  "binance",
		CreatedAt: now,
		UpdatedAt: now,
	})

	usID := h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID:           userID,
		Name:             "ICT_1H",
		Exchange:         "binance",
		Cash:             1000,
		Parts:            5,
		Status:           1,
		RiskStrategyType: order.RiskStrategyTypeCtaIntraday,
		CreatedAt:        now,
		UpdatedAt:        now,
	})

	h.Repo.CreateExchangeSymbolFilter(&order.ExchangeSymbolFilter{
		Exchange:   "mock",
		PosType:    order.PosTypeFutures,
		Symbol:     "BTCUSDT",
		FilterType: "PRICE_FILTER",
		TickSize:   0.1,
	})
	h.Repo.CreateExchangeSymbolFilter(&order.ExchangeSymbolFilter{
		Exchange:   "mock",
		PosType:    order.PosTypeFutures,
		Symbol:     "BTCUSDT",
		FilterType: "LOT_SIZE",
		MinQty:     0.001,
		StepSize:   0.001,
	})

	err := h.HandleOpen(Signal{
		UserID:         userID,
		Symbol:         "BTC",
		UserStrategyID: usID,
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Cash:           123,
		TriggerPrice:   50000.03,
		Slippage:       0.001,
		Side:           int(order.SideLong),
		OrderType:      0,
		Leverage:       1,
	})
	if err != nil {
		t.Fatalf("HandleOpen: %v", err)
	}
	if len(mock.CreatedOrders) != 1 {
		t.Fatalf("expected 1 exchange order, got %d", len(mock.CreatedOrders))
	}

	created := mock.CreatedOrders[0]
	if created.Quantity != 0.002 {
		t.Fatalf("expected truncated quantity 0.002, got %.12f", created.Quantity)
	}
	if created.Price != 50050 {
		t.Fatalf("expected truncated price 50050, got %.12f", created.Price)
	}
}

func TestHandleOpen_CreatesUprunningOrder(t *testing.T) {
	h, mock, gs := setupHandler(t)
	defer gs.Shutdown()

	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name:      "test_user",
		Exchange:  "binance",
		CreatedAt: now,
		UpdatedAt: now,
	})
	usID := h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID:           userID,
		Name:             "ICT_1H",
		Exchange:         "binance",
		Cash:             1000,
		Parts:            5,
		Status:           1,
		RiskStrategyType: order.RiskStrategyTypeCtaIntraday,
		CreatedAt:        now,
		UpdatedAt:        now,
	})

	err := h.HandleOpen(Signal{
		UserID:         userID,
		Symbol:         "BTC",
		UserStrategyID: usID,
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Cash:           100,
		TriggerPrice:   50000,
		Side:           int(order.SideLong),
		OrderType:      0,
		Leverage:       10,
	})
	if err != nil {
		t.Fatalf("HandleOpen: %v", err)
	}
	if len(mock.CreatedOrders) != 1 {
		t.Fatalf("expected 1 exchange order, got %d", len(mock.CreatedOrders))
	}

	running := h.Repo.ListUprunningOrders()
	if len(running) != 1 {
		t.Fatalf("expected 1 uprunning order, got %d", len(running))
	}
	uo := running[0]
	if uo.UserID != userID || uo.RelationType != "user_orders" || uo.RelationID == 0 {
		t.Fatalf("unexpected uprunning order relation: %+v", uo)
	}
	if uo.ExchangeOrderID != mock.CreatedOrders[0].OrderID || uo.ExchangeOrderStatus != string(mock.CreatedOrders[0].Status) {
		t.Fatalf("unexpected exchange order fields: %+v", uo)
	}
	if uo.Symbol != "BTCUSDT" || uo.ExchangeOrderQty <= 0 {
		t.Fatalf("unexpected symbol/quantity: %+v", uo)
	}
}

func TestHandleSignal_MissingUserStrategy(t *testing.T) {
	h, _, gs := setupHandler(t)

	sig := Signal{
		UserID: 1, UserStrategyID: 999,
		Exchange: "mock",
	}

	err := h.HandleSignal(sig)
	if err == nil {
		t.Error("expected error for missing user strategy")
	}
	gs.Shutdown()
}

func TestHandleSignal_InvalidCash(t *testing.T) {
	h, _, gs := setupHandler(t)

	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name: "test_user", CreatedAt: now, UpdatedAt: now,
	})

	usID := h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "s1", Cash: 1000, Parts: 5, Status: 1,
		CreatedAt: now, UpdatedAt: now,
	})

	sig := Signal{
		UserID: userID, UserStrategyID: usID,
		Exchange: "mock", Cash: 0,
	}

	err := h.HandleSignal(sig)
	if err == nil {
		t.Error("expected error for zero cash")
	}
	gs.Shutdown()
}

// TestHandleOpen_Status3OnRESTError verifies that when exchange.CreateOrder fails,
// user_order.status is set to 3 (FAILED) locally.
func TestHandleOpen_Status3OnRESTError(t *testing.T) {
	h, mock, gs := setupHandler(t)
	defer gs.Shutdown()
	mock.SetPrice("BTCUSDT", 50000)
	mock.SetCreateError(true) // force CreateOrder to fail

	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name: "test_user", Exchange: "mock",
		CreatedAt: now, UpdatedAt: now,
	})

	usID := h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "ICT_1H", Exchange: "mock",
		Cash: 1000, Parts: 5, Status: 1,
		RiskStrategyType: order.RiskStrategyTypeCtaIntraday,
		CreatedAt:        now, UpdatedAt: now,
	})

	sig := Signal{
		UserID: userID, UserStrategyID: usID, Symbol: "BTC",
		PosType: int(order.PosTypeFutures), Exchange: "mock",
		Cash: 100, TriggerPrice: 50000, Slippage: 0.01,
		Side: int(order.SideLong), OrderType: 0, Leverage: 10,
	}

	err := h.HandleOpen(sig)
	if err == nil {
		t.Fatal("expected error when CreateOrder fails")
	}

	// Verify user_order.status = 3 (FAILED)
	if len(gs.UserOrders) != 1 {
		t.Fatalf("expected 1 user order, got %d", len(gs.UserOrders))
	}
	for _, o := range gs.UserOrders {
		if o.Status != 3 {
			t.Errorf("expected user_order.status=3 (FAILED), got %d", o.Status)
		}
	}
}

// TestHandleOpen_LeverageBeforeOrder verifies that SetLeverage is called
// BEFORE CreateOrder (not after).
func TestHandleOpen_LeverageBeforeOrder(t *testing.T) {
	h, mock, gs := setupHandler(t)
	defer gs.Shutdown()
	mock.SetPrice("BTCUSDT", 50000)

	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name: "test_user", Exchange: "mock",
		CreatedAt: now, UpdatedAt: now,
	})

	usID := h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "ICT_1H", Exchange: "mock",
		Cash: 1000, Parts: 5, Status: 1,
		RiskStrategyType: order.RiskStrategyTypeCtaIntraday,
		CreatedAt:        now, UpdatedAt: now,
	})

	err := h.HandleOpen(Signal{
		UserID: userID, UserStrategyID: usID, Symbol: "BTC",
		PosType: int(order.PosTypeFutures), Exchange: "mock",
		Cash: 100, TriggerPrice: 50000, Slippage: 0.01,
		Side: int(order.SideLong), OrderType: 0, Leverage: 10,
	})
	if err != nil {
		t.Fatalf("HandleOpen: %v", err)
	}

	// Verify call order: SetLeverage must come before CreateOrder
	callOrder := mock.CallOrder()
	if len(callOrder) < 2 {
		t.Fatalf("expected at least 2 calls (SetLeverage + CreateOrder), got %d: %v", len(callOrder), callOrder)
	}
	if callOrder[0] != "SetLeverage" {
		t.Errorf("expected first call to be SetLeverage, got %s", callOrder[0])
	}
	if callOrder[1] != "CreateOrder" {
		t.Errorf("expected second call to be CreateOrder, got %s", callOrder[1])
	}
}

func TestSanitizeStrategyParams_PositiveStopLossNegated(t *testing.T) {
	params := map[string]interface{}{
		"StopLossThreshold": 0.02,
	}
	sanitizeStrategyParams(params)
	if v, ok := params["StopLossThreshold"].(float64); !ok || v != -0.02 {
		t.Errorf("expected -0.02, got %v", params["StopLossThreshold"])
	}
}

func TestSanitizeStrategyParams_NegativeStopLossUnchanged(t *testing.T) {
	params := map[string]interface{}{
		"StopLossThreshold": -0.05,
	}
	sanitizeStrategyParams(params)
	if v, ok := params["StopLossThreshold"].(float64); !ok || v != -0.05 {
		t.Errorf("expected -0.05, got %v", params["StopLossThreshold"])
	}
}

func TestSanitizeStrategyParams_MissingKeyNoOp(t *testing.T) {
	params := map[string]interface{}{
		"OtherParam": "value",
	}
	sanitizeStrategyParams(params)
	if _, ok := params["StopLossThreshold"]; ok {
		t.Error("should not have created StopLossThreshold")
	}
}

func TestSanitizeStrategyParams_IntValueNoOp(t *testing.T) {
	params := map[string]interface{}{
		"StopLossThreshold": 5, // int, not float64
	}
	sanitizeStrategyParams(params)
	if v, ok := params["StopLossThreshold"].(int); !ok || v != 5 {
		t.Errorf("expected int 5 unchanged, got %v", params["StopLossThreshold"])
	}
}

// =============================================================================
// ValidateSignal Tests
// =============================================================================

func TestValidateSignal_RequiresTriggerPriceForLimitOrder(t *testing.T) {
	// Limit order with zero TriggerPrice should fail
	sig := Signal{
		UserID:         1,
		UserStrategyID: 1,
		Exchange:       "binance",
		Symbol:         "BTCUSDT",
		OrderType:      orderTypeLimit, // 0
		TriggerPrice:   0,              // should fail
	}

	err := ValidateSignal(sig)

	if err == nil {
		t.Error("expected error for limit order with zero TriggerPrice, got nil")
	}
	if err != nil && err.Error() != "TriggerPrice must be positive for limit orders" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSignal_AllowsZeroTriggerPriceForMarketOrder(t *testing.T) {
	// Market order with zero TriggerPrice should pass
	sig := Signal{
		UserID:         1,
		UserStrategyID: 1,
		Exchange:       "binance",
		Symbol:         "BTCUSDT",
		OrderType:      orderTypeMarket, // 1
		TriggerPrice:   0,               // should be allowed
	}

	err := ValidateSignal(sig)

	if err != nil {
		t.Errorf("expected no error for market order with zero TriggerPrice, got: %v", err)
	}
}

func TestValidateSignal_RequiresTriggerPriceForLimitOrderWithPositiveValue(t *testing.T) {
	// Limit order with positive TriggerPrice should pass
	sig := Signal{
		UserID:         1,
		UserStrategyID: 1,
		Exchange:       "binance",
		Symbol:         "BTCUSDT",
		OrderType:      orderTypeLimit, // 0
		TriggerPrice:   50000,          // positive value
	}

	err := ValidateSignal(sig)

	if err != nil {
		t.Errorf("expected no error for limit order with positive TriggerPrice, got: %v", err)
	}
}

func TestValidateSignal_AllowsPositiveTriggerPriceForMarketOrder(t *testing.T) {
	// Market order with positive TriggerPrice should also pass (used as fallback)
	sig := Signal{
		UserID:         1,
		UserStrategyID: 1,
		Exchange:       "binance",
		Symbol:         "BTCUSDT",
		OrderType:      orderTypeMarket, // 1
		TriggerPrice:   50000,           // optional but allowed
	}

	err := ValidateSignal(sig)

	if err != nil {
		t.Errorf("expected no error for market order with positive TriggerPrice, got: %v", err)
	}
}

func TestValidateSignal_RequiresBasicFields(t *testing.T) {
	tests := []struct {
		name    string
		sig     Signal
		wantErr string
	}{
		{
			name: "missing UserID",
			sig: Signal{
				UserStrategyID: 1,
				Exchange:       "binance",
				Symbol:         "BTCUSDT",
				OrderType:      orderTypeMarket,
			},
			wantErr: "UserID is required",
		},
		{
			name: "missing UserStrategyID",
			sig: Signal{
				UserID:    1,
				Exchange:  "binance",
				Symbol:    "BTCUSDT",
				OrderType: orderTypeMarket,
			},
			wantErr: "UserStrategyID is required",
		},
		{
			name: "missing Exchange",
			sig: Signal{
				UserID:         1,
				UserStrategyID: 1,
				Symbol:         "BTCUSDT",
				OrderType:      orderTypeMarket,
			},
			wantErr: "Exchange is required",
		},
		{
			name: "missing Symbol",
			sig: Signal{
				UserID:         1,
				UserStrategyID: 1,
				Exchange:       "binance",
				OrderType:      orderTypeMarket,
			},
			wantErr: "Symbol is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSignal(tt.sig)
			if err == nil {
				t.Errorf("expected error %q, got nil", tt.wantErr)
			} else if err.Error() != tt.wantErr {
				t.Errorf("expected error %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}
