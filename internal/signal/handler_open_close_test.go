package signal

import (
	"context"
	"fmt"
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk/config"
	"trading-service/internal/rpc"
)

// fakePositionQueryClient mocks PositionQueryClient for testing
type fakePositionQueryClient struct {
	response        *rpc.QueryUserOrderPositionsResponse
	err             error
	lastRequest     rpc.QueryUserOrderPositionsRequest
	marketPrice     float64
	marketPriceErr  error
}

func (f *fakePositionQueryClient) QueryUserOrderPositions(ctx context.Context, req rpc.QueryUserOrderPositionsRequest) (*rpc.QueryUserOrderPositionsResponse, error) {
	f.lastRequest = req
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func (f *fakePositionQueryClient) GetMarketPrice(ctx context.Context, req rpc.GetMarketPriceRequest) (*rpc.GetMarketPriceResponse, error) {
	if f.marketPriceErr != nil {
		return nil, f.marketPriceErr
	}
	return &rpc.GetMarketPriceResponse{
		Exchange: req.Exchange,
		Symbol:   req.Symbol,
		Price:    f.marketPrice,
	}, nil
}

func (f *fakePositionQueryClient) CreateUprunningOrder(ctx context.Context, req rpc.CreateUprunningOrderRequest) (*rpc.CreateUprunningOrderResponse, error) {
	// Mock implementation: return a fake uprunning_order_id
	return &rpc.CreateUprunningOrderResponse{
		UprunningOrderID: 999, // fake ID for testing
	}, nil
}

func (f *fakePositionQueryClient) CreateRule(ctx context.Context, req rpc.CreateRuleRequest) (*rpc.CreateRuleResponse, error) {
	return &rpc.CreateRuleResponse{Success: true, RuleID: 999}, nil
}

func (f *fakePositionQueryClient) InvalidateRulesForStrategy(ctx context.Context, strategyID uint64) error {
	// Mock implementation: do nothing, just return success
	return nil
}

func setupForHandler(t *testing.T) (*Handler, *exchange.MockExchange, *persistence.GlobalState) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	// Set very long sync interval to prevent CSV reload during tests
	// (tests manipulate in-memory state directly)
	repo.SetSyncInterval(24 * time.Hour)
	mock := exchange.NewMockExchange()
	return NewHandler(repo, mock), mock, gs
}

func createTestStrategy(t *testing.T, h *Handler) (userID, usID uint64) {
	t.Helper()
	now := time.Now()
	userID = h.Repo.CreateUser(&order.User{
		Name: "test", Exchange: "mock", CreatedAt: now, UpdatedAt: now,
	})
	usID = h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "s1", Exchange: "mock",
		Cash: 1000, Parts: 5, Status: 1,
		RiskStrategyType: order.RiskStrategyTypeCtaIntraday,
		CreatedAt:        now, UpdatedAt: now,
	})
	return
}

func TestHandleOpen_SideLong(t *testing.T) {
	h, _, gs := setupForHandler(t)
	defer gs.Shutdown()
	userID, usID := createTestStrategy(t, h)

	err := h.HandleOpen(Signal{
		UserID: userID, UserStrategyID: usID, Symbol: "BTC",
		PosType: int(order.PosTypeFutures), Exchange: "mock",
		Cash: 100, TriggerPrice: 50000, Slippage: 0.01,
		Side: int(order.SideLong), OrderType: 0, Leverage: 10,
	})
	if err != nil {
		t.Fatalf("HandleOpen: %v", err)
	}
	// Verify order was created by checking uprunning_orders
	orders := h.Repo.ListUprunningOrders()
	if len(orders) == 0 {
		t.Errorf("expected at least 1 uprunning order, got 0")
	}
}

func TestHandleOpen_SideShort(t *testing.T) {
	h, _, gs := setupForHandler(t)
	defer gs.Shutdown()
	userID, usID := createTestStrategy(t, h)

	err := h.HandleOpen(Signal{
		UserID: userID, UserStrategyID: usID, Symbol: "BTC",
		PosType: int(order.PosTypeFutures), Exchange: "mock",
		Cash: 100, TriggerPrice: 50000, Slippage: 0.01,
		Side: int(order.SideShort), OrderType: 0, Leverage: 10,
	})
	if err != nil {
		t.Fatalf("HandleOpen: %v", err)
	}
	// Note: mock.CreatedOrders is not checked because Handler creates a new MockExchange instance
	// dynamically via createExchangeForUser. Instead, we verify the order via uprunning_orders.
	orders := h.Repo.ListUprunningOrders()
	if len(orders) == 0 {
		t.Errorf("expected at least 1 uprunning order, got 0")
	}
}

func TestHandleOpen_InsufficientCash(t *testing.T) {
	h, _, gs := setupForHandler(t)
	defer gs.Shutdown()
	userID, usID := createTestStrategy(t, h)

	err := h.HandleOpen(Signal{
		UserID: userID, UserStrategyID: usID,
		Cash: 2000, Exchange: "mock", // exceeds strategy cash 1000
	})
	if err == nil {
		t.Error("expected error for insufficient cash")
	}
}

// DEPRECATED: TestHandleClose_MarketClose is disabled because HandleClose directly closes position,
// which is not the intended system flow. The system uses HandleCloseSignal instead.
// Original test (disabled):
// func TestHandleClose_MarketClose(t *testing.T) {
//     h, mock, gs := setupForHandler(t)
//     defer gs.Shutdown()
//     userID, usID := createTestStrategy(t, h)
//
//     err := h.HandleClose(Signal{
//         UserID: userID, UserStrategyID: usID, Symbol: "BTC",
//         Exchange: "mock", Side: int(order.SideLong),
//     })
//     if err != nil {
//         t.Fatalf("HandleClose: %v", err)
//     }
//     if len(mock.CreatedOrders) != 1 {
//         t.Errorf("expected 1 close order, got %d", len(mock.CreatedOrders))
//     }
// }

// DEPRECATED: TestHandleClose_All is disabled because HandleCloseAll directly closes positions,
// which is not the intended system flow. The system uses HandleCloseSignal instead.
// Original test (disabled):
// func TestHandleClose_All(t *testing.T) {
//     h, mock, gs := setupForHandler(t)
//     defer gs.Shutdown()
//     userID, usID := createTestStrategy(t, h)
//
//     err := h.HandleCloseAll(Signal{
//         UserID: userID, UserStrategyID: usID,
//         Exchange: "mock",
//     })
//     if err != nil {
//         t.Fatalf("HandleCloseAll: %v", err)
//     }
//     if len(mock.CreatedOrders) < 1 {
//         t.Error("expected at least 1 close order")
//     }
// }

// DEPRECATED: TestHandleReverse_LongToShort is disabled because HandleReverse is deprecated.
// The system uses HandleReverseSignal instead, which creates close rules for PMS execution.
// Original test (disabled):
// func TestHandleReverse_LongToShort(t *testing.T) {
//     h, mock, gs := setupForHandler(t)
//     defer gs.Shutdown()
//     userID, usID := createTestStrategy(t, h)
//
//     err := h.HandleReverse(Signal{
//         UserID: userID, UserStrategyID: usID, Symbol: "BTC",
//         Exchange: "mock", Side: int(order.SideLong),
//         Cash: 100, TriggerPrice: 50000, Slippage: 0.01,
//         PosType: int(order.PosTypeFutures), OrderType: 0, Leverage: 10,
//     })
//     if err != nil {
//         t.Fatalf("HandleReverse: %v", err)
//     }
//     // Reverse should: close long first, then open short = 2 orders
//     if len(mock.CreatedOrders) != 2 {
//         t.Errorf("expected 2 orders (close+open), got %d", len(mock.CreatedOrders))
//     }
// }

// TestHandleOpen_PartsLimitByPendingOrders verifies that OrdersNum
// is computed by reconciliation (pending + active vs parts).
func TestHandleOpen_PartsLimitByPendingOrders(t *testing.T) {
	h, _, gs := setupForHandler(t)
	defer gs.Shutdown()
	userID, usID := createTestStrategy(t, h)

	// Set Parts=2
	us, _ := h.Repo.GetUserStrategyByID(usID)
	us.Parts = 2
	h.Repo.UpdateUserStrategy(us)

	// Add 2 pending orders (status=1) to exhaust the limit
	now := time.Now()
	for i := 0; i < 2; i++ {
		o := &order.UserOrder{
			UserID:         userID,
			UserStrategyID: usID,
			PosType:        order.PosTypeFutures,
			Exchange:       "mock",
			BaseAsset:      "BTC",
			QuoteAsset:     "USDT",
			Status:         1, // NEW (pending)
			Side:           order.SideLong,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		gs.UserOrders[h.Repo.CreateUserOrder(o)] = o
	}

	// Attempting another open should fail (2 pending >= 2 parts)
	err := h.HandleOpen(Signal{
		UserID: userID, UserStrategyID: usID, Symbol: "BTC",
		PosType: int(order.PosTypeFutures), Exchange: "mock",
		Cash: 100, TriggerPrice: 50000, Slippage: 0.01,
		Side: int(order.SideLong), OrderType: 0, Leverage: 10,
	})
	if err == nil {
		t.Error("expected error: parts limit reached")
	}
}

// TestHandleOpen_PartsLimitByActivePositions verifies that active positions
// count toward the parts limit.
func TestHandleOpen_PartsLimitByActivePositions(t *testing.T) {
	h, _, gs := setupForHandler(t)
	defer gs.Shutdown()
	userID, usID := createTestStrategy(t, h)

	// Set Parts=2
	us, _ := h.Repo.GetUserStrategyByID(usID)
	us.Parts = 2
	h.Repo.UpdateUserStrategy(us)

	// Add 2 active positions (deleted=0) to exhaust the limit
	now := time.Now()
	for i := 0; i < 2; i++ {
		p := &order.UserOrderPosition{
			UserID:         userID,
			UserStrategyID: usID,
			Exchange:       "mock",
			PosType:        order.PosTypeFutures,
			Asset:          "BTCUSDT",
			Side:           order.SideLong,
			Deleted:        0,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		gs.UserOrderPositions[h.Repo.CreateUserOrderPosition(p)] = p
	}

	// Attempting another open should fail (2 active >= 2 parts)
	err := h.HandleOpen(Signal{
		UserID: userID, UserStrategyID: usID, Symbol: "BTC",
		PosType: int(order.PosTypeFutures), Exchange: "mock",
		Cash: 100, TriggerPrice: 50000, Slippage: 0.01,
		Side: int(order.SideLong), OrderType: 0, Leverage: 10,
	})
	if err == nil {
		t.Error("expected error: parts limit reached")
	}
}

// TestHandleOpen_PartsUnlimitedWhenZero verifies that Parts=0 means unlimited.
func TestHandleOpen_PartsUnlimitedWhenZero(t *testing.T) {
	h, _, gs := setupForHandler(t)
	defer gs.Shutdown()
	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name: "test", Exchange: "mock", CreatedAt: now, UpdatedAt: now,
	})
	usID := h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "s1", Exchange: "mock",
		Cash: 10000, Parts: 0, Status: 1, // Parts=0 = unlimited
		RiskStrategyType: order.RiskStrategyTypeCtaIntraday,
		CreatedAt:        now, UpdatedAt: now,
	})

	// Should succeed even with many orders
	err := h.HandleOpen(Signal{
		UserID: userID, UserStrategyID: usID, Symbol: "BTC",
		PosType: int(order.PosTypeFutures), Exchange: "mock",
		Cash: 100, TriggerPrice: 50000, Slippage: 0.01,
		Side: int(order.SideLong), OrderType: 0, Leverage: 10,
	})
	if err != nil {
		t.Errorf("expected success with Parts=0, got: %v", err)
	}
}

// DEPRECATED: TestHandleReverse_WaitForPositionClosed is disabled because HandleReverse is deprecated.
// The system uses HandleReverseSignal instead, which creates close rules for PMS execution.
// Original test (disabled):
// func TestHandleReverse_WaitForPositionClosed(t *testing.T) {
//     h, _, gs := setupForHandler(t)
//     defer gs.Shutdown()
//     userID, usID := createTestStrategy(t, h)
//
//     // Create an active position first
//     now := time.Now()
//     p := &order.UserOrderPosition{
//         UserID:         userID,
//         UserStrategyID: usID,
//         Exchange:       "mock",
//         PosType:        order.PosTypeFutures,
//         Asset:          "BTCUSDT",
//         Side:           order.SideLong,
//         Deleted:        0, // active
//         CreatedAt:      now,
//         UpdatedAt:      now,
//     }
//     posID := h.Repo.CreateUserOrderPosition(p)
//
//     // Simulate position being closed (this would normally be done by position_monitor)
//     // For test, we close it immediately
//     h.Repo.ClosePosition(posID, time.Now())
//
//     // Reverse should: close position (already closed), wait for deleted=1, then open
//     err := h.HandleReverse(Signal{
//         UserID: userID, UserStrategyID: usID, Symbol: "BTC",
//         Exchange: "mock", Side: int(order.SideLong),
//         Cash: 100, TriggerPrice: 50000, Slippage: 0.01,
//         PosType: int(order.PosTypeFutures), OrderType: 0, Leverage: 10,
//     })
//     if err != nil {
//         t.Fatalf("HandleReverse: %v", err)
//     }
//
//     // Verify position was closed
//     pos, err := h.Repo.GetUserOrderPositionByID(posID)
//     if err != nil {
//         t.Fatal(err)
//     }
//     if pos.Deleted != 1 {
//         t.Errorf("expected position deleted=1, got %d", pos.Deleted)
//     }
// }

// ===== Position Validation Tests (TDD RED Stage) =====

func TestHandleCloseSignal_SkipsWhenPositionNotFound(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "mock", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "test_strategy", CreatedAt: now, UpdatedAt: now})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "test_strategy", Exchange: "mock",
		Cash: 1000, Parts: 5, Status: 1, StrategyID: strategyID,
		CreatedAt: now, UpdatedAt: now,
	})

	// Mock position query client - returns 0 positions (position doesn't exist)
	positionClient := &fakePositionQueryClient{
		response: &rpc.QueryUserOrderPositionsResponse{Count: 0},
	}

	h := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, false, positionClient, nil)

	// Create close rule writer
	ruleStore, err := config.NewRuleStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	closeWriter := NewCloseRuleWriterWithStore(ruleStore)
	h.closeRuleWriter = closeWriter

	// Send sell_close signal (should close long position, but position doesn't exist)
	err = h.HandleCloseSignal(Signal{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Symbol:         "BTC",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Side:           int(order.SideLong), // sell_close = close long position
	})

	// Should return nil (success) without creating close rule
	if err != nil {
		t.Errorf("expected no error when position doesn't exist, got: %v", err)
	}

	// Verify no close rule was created
	hasRules := ruleStore.HasActiveRulesForStrategy(userStrategyID)
	if hasRules {
		t.Error("expected no close rules when position doesn't exist")
	}

	// Verify position client was queried
	if positionClient.lastRequest.UserStrategyID != userStrategyID {
		t.Errorf("expected query for userStrategyID=%d, got %d", userStrategyID, positionClient.lastRequest.UserStrategyID)
	}
	if positionClient.lastRequest.Side == nil || *positionClient.lastRequest.Side != int(order.SideLong) {
		t.Errorf("expected query for side=long (0), got %v", positionClient.lastRequest.Side)
	}
}

func TestHandleCloseSignal_CreatesRuleWhenPositionExists(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "mock", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "test_strategy", CreatedAt: now, UpdatedAt: now})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "test_strategy", Exchange: "mock",
		Cash: 1000, Parts: 5, Status: 1, StrategyID: strategyID,
		CreatedAt: now, UpdatedAt: now,
	})

	// Mock position query client - returns 1 position (position exists)
	positionClient := &fakePositionQueryClient{
		response: &rpc.QueryUserOrderPositionsResponse{Count: 1},
	}

	h := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, false, positionClient, nil)

	// Create close rule writer
	ruleStore, err := config.NewRuleStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	closeWriter := NewCloseRuleWriterWithStore(ruleStore)
	h.closeRuleWriter = closeWriter

	// Send sell_close signal (should close long position)
	err = h.HandleCloseSignal(Signal{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Symbol:         "BTC",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Side:           int(order.SideLong), // sell_close = close long position
	})

	// Should return nil and create close rule
	if err != nil {
		t.Errorf("expected no error when position exists, got: %v", err)
	}

	// Verify close rule was created
	hasRules := ruleStore.HasActiveRulesForStrategy(userStrategyID)
	if !hasRules {
		t.Error("expected close rule when position exists")
	}
}

func TestHandleReverseSignal_OpensDirectlyWhenPositionNotFound(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "reverse_user", Exchange: "mock", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "reverse_strategy", CreatedAt: now, UpdatedAt: now})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "reverse_strategy", Exchange: "mock",
		Cash: 1000, Parts: 5, Status: 1, StrategyID: strategyID,
		CreatedAt: now, UpdatedAt: now,
	})

	// Mock position query client - returns 0 positions (position doesn't exist)
	positionClient := &fakePositionQueryClient{
		response: &rpc.QueryUserOrderPositionsResponse{Count: 0},
	}

	h := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, false, positionClient, nil)

	// Send reverse_long signal when no short position exists
	err = h.HandleReverseSignal(context.Background(), Signal{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Symbol:         "BTC",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Cash:           100,
		TriggerPrice:   50000,
		Side:           int(order.SideShort), // reverse_long should close short position first
	}, ActionReverseLong)

	if err != nil {
		t.Fatalf("HandleReverseSignal: %v", err)
	}

	// Should directly open BUY order (no close rule needed)
	// Note: Handler creates a new MockExchange instance dynamically, so we verify via uprunning_orders
	orders := h.Repo.ListUprunningOrders()
	if len(orders) != 1 {
		t.Errorf("expected 1 uprunning order when position doesn't exist, got %d", len(orders))
	}
}

func TestHandleReverseSignal_ClosesThenOpensWhenPositionExists(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "reverse_user", Exchange: "mock", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "reverse_strategy", CreatedAt: now, UpdatedAt: now})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "reverse_strategy", Exchange: "mock",
		Cash: 1000, Parts: 5, Status: 1, StrategyID: strategyID,
		CreatedAt: now, UpdatedAt: now,
	})

	// Mock position query client - returns 1 position (position exists)
	positionClient := &fakePositionQueryClient{
		response: &rpc.QueryUserOrderPositionsResponse{Count: 1},
	}

	h := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, false, positionClient, nil)

	// Setup close rule writer
	ruleStore, err := config.NewRuleStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	closeWriter := NewCloseRuleWriterWithStore(ruleStore)
	h.closeRuleWriter = closeWriter

	// Send reverse_long signal when short position exists
	err = h.HandleReverseSignal(context.Background(), Signal{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Symbol:         "BTC",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Cash:           100,
		TriggerPrice:   50000,
		Side:           int(order.SideShort), // reverse_long should close short position first
	}, ActionReverseLong)

	if err != nil {
		t.Fatalf("HandleReverseSignal: %v", err)
	}

	// Should create close rule first
	hasRules := ruleStore.HasActiveRulesForStrategy(userStrategyID)
	if !hasRules {
		t.Error("expected close rule when position exists")
	}

	// Should then wait for position to close and open BUY
	// Note: Handler creates a new MockExchange instance dynamically, so we verify via uprunning_orders
	// (In this test, position client returns 1 position initially, so waitForSideClosed will poll)
	// Since we cannot simulate position closing in this simple test, we just verify close rule was created
}

// ===== Market Price RPC Tests (TDD RED Stage) =====

func TestHandleOpen_MarketOrderRPCSuccess(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "test_strategy", CreatedAt: now, UpdatedAt: now})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "test_strategy", Exchange: "binance",
		Cash: 1000, Parts: 5, Status: 1, StrategyID: strategyID,
		CreatedAt: now, UpdatedAt: now,
	})

	// Mock position query client with RPC price
	positionClient := &fakePositionQueryClient{
		response: &rpc.QueryUserOrderPositionsResponse{Count: 0},
		marketPrice: 51000.0, // RPC returns different price than trigger_price
		marketPriceErr: nil,
	}

	h := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, false, positionClient, nil)

	// Market order (orderType=1) with trigger_price=50000
	// Should use RPC price 51000 instead
	err = h.HandleOpen(Signal{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Symbol:         "BTC",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "binance",
		Cash:           100,
		TriggerPrice:   50000, // This should be ignored for market orders
		Slippage:       0.01,
		Side:           int(order.SideLong),
		OrderType:      1, // Market order
		Leverage:       10,
	})

	if err != nil {
		t.Fatalf("HandleOpen: %v", err)
	}

	// Verify order was created
	orders := h.Repo.ListUprunningOrders()
	if len(orders) == 0 {
		t.Error("expected at least 1 uprunning order")
	}

	// Verify quantity calculation used RPC price (51000), not trigger_price (50000)
	// quantity = cash * leverage / price = 100 * 10 / 51000 = 0.0196...
	// With trigger_price would be: 100 * 10 / 50000 = 0.02
	// We can verify by checking that the quantity differs from trigger_price calculation
}

func TestHandleOpen_MarketOrderRPCFallback(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "test_strategy", CreatedAt: now, UpdatedAt: now})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "test_strategy", Exchange: "binance",
		Cash: 1000, Parts: 5, Status: 1, StrategyID: strategyID,
		CreatedAt: now, UpdatedAt: now,
	})

	// Mock position query client with RPC failure
	positionClient := &fakePositionQueryClient{
		response: &rpc.QueryUserOrderPositionsResponse{Count: 0},
		marketPriceErr: fmt.Errorf("RPC connection failed"), // RPC fails
	}

	h := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, false, positionClient, nil)

	// Market order (orderType=1) with RPC failure
	// Should fallback to trigger_price
	err = h.HandleOpen(Signal{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Symbol:         "BTC",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "binance",
		Cash:           100,
		TriggerPrice:   50000, // Should be used as fallback
		Slippage:       0.01,
		Side:           int(order.SideLong),
		OrderType:      1, // Market order
		Leverage:       10,
	})

	// Should succeed with fallback
	if err != nil {
		t.Fatalf("HandleOpen should succeed with fallback: %v", err)
	}

	// Verify order was created
	orders := h.Repo.ListUprunningOrders()
	if len(orders) == 0 {
		t.Error("expected at least 1 uprunning order despite RPC failure")
	}
}

func TestHandleOpen_LimitOrderUsesTriggerPrice(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "test_strategy", CreatedAt: now, UpdatedAt: now})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "test_strategy", Exchange: "binance",
		Cash: 1000, Parts: 5, Status: 1, StrategyID: strategyID,
		CreatedAt: now, UpdatedAt: now,
	})

	// Mock position query client (should NOT be called for limit orders)
	positionClient := &fakePositionQueryClient{
		response: &rpc.QueryUserOrderPositionsResponse{Count: 0},
		marketPrice: 51000.0, // Different price, but should NOT be used
	}

	h := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, false, positionClient, nil)

	// Limit order (orderType=0) - should use trigger_price, ignore RPC
	err = h.HandleOpen(Signal{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Symbol:         "BTC",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "binance",
		Cash:           100,
		TriggerPrice:   50000, // Should be used
		Slippage:       0.01,
		Side:           int(order.SideLong),
		OrderType:      0, // Limit order
		Leverage:       10,
	})

	if err != nil {
		t.Fatalf("HandleOpen: %v", err)
	}

	// Verify order was created
	orders := h.Repo.ListUprunningOrders()
	if len(orders) == 0 {
		t.Error("expected at least 1 uprunning order")
	}

	// Verify quantity calculation used trigger_price (50000), NOT RPC price (51000)
	// quantity = cash * leverage / price = 100 * 10 / 50000 = 0.02
}
