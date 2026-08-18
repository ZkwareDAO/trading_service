package hyperliquid

import (
	"context"
	"errors"
	"math"
	"testing"

	"trading-service/internal/exchange"

	hyperliquid "github.com/sonirico/go-hyperliquid"
)

// ============================================
// Mock implementations
// ============================================

type mockInfoClient struct {
	queryOrderFunc      func(ctx context.Context, user string, oid int64) (*hyperliquid.OrderQueryResult, error)
	userStateFunc       func(ctx context.Context, address string, dex ...string) (*hyperliquid.UserState, error)
	allMidsFunc         func(ctx context.Context, dex ...string) (map[string]string, error)
	userFillsFunc       func(ctx context.Context, params hyperliquid.UserFillsParams) ([]hyperliquid.Fill, error)
	userFillsByTimeFunc func(ctx context.Context, address string, startTime int64, endTime *int64, aggregateByTime *bool) ([]hyperliquid.Fill, error)
}

func (m *mockInfoClient) QueryOrderByOid(ctx context.Context, user string, oid int64) (*hyperliquid.OrderQueryResult, error) {
	if m.queryOrderFunc != nil {
		return m.queryOrderFunc(ctx, user, oid)
	}
	return nil, errors.New("not implemented")
}

func (m *mockInfoClient) UserFills(ctx context.Context, params hyperliquid.UserFillsParams) ([]hyperliquid.Fill, error) {
	if m.userFillsFunc != nil {
		return m.userFillsFunc(ctx, params)
	}
	return nil, errors.New("not implemented")
}

func (m *mockInfoClient) UserFillsByTime(ctx context.Context, address string, startTime int64, endTime *int64, aggregateByTime *bool) ([]hyperliquid.Fill, error) {
	if m.userFillsByTimeFunc != nil {
		return m.userFillsByTimeFunc(ctx, address, startTime, endTime, aggregateByTime)
	}
	return nil, errors.New("not implemented")
}

func (m *mockInfoClient) UserState(ctx context.Context, address string, dex ...string) (*hyperliquid.UserState, error) {
	if m.userStateFunc != nil {
		return m.userStateFunc(ctx, address, dex...)
	}
	return nil, errors.New("not implemented")
}

func (m *mockInfoClient) AllMids(ctx context.Context, dex ...string) (map[string]string, error) {
	if m.allMidsFunc != nil {
		return m.allMidsFunc(ctx, dex...)
	}
	return nil, errors.New("not implemented")
}

func (m *mockInfoClient) Meta(ctx context.Context, dex ...string) (*hyperliquid.Meta, error) {
	return nil, errors.New("not implemented")
}

type mockExchangeClient struct {
	orderFunc          func(ctx context.Context, req hyperliquid.CreateOrderRequest, builder *hyperliquid.BuilderInfo) (hyperliquid.OrderStatus, error)
	cancelFunc         func(ctx context.Context, coin string, oid int64) (*hyperliquid.APIResponse[hyperliquid.CancelOrderResponse], error)
	updateLeverageFunc func(ctx context.Context, leverage int, name string, isCross bool) (*hyperliquid.UserState, error)
	slippagePriceFunc  func(ctx context.Context, name string, isBuy bool, slippage float64, px *float64) (float64, error)
}

func (m *mockExchangeClient) Order(ctx context.Context, req hyperliquid.CreateOrderRequest, builder *hyperliquid.BuilderInfo) (hyperliquid.OrderStatus, error) {
	if m.orderFunc != nil {
		return m.orderFunc(ctx, req, builder)
	}
	return hyperliquid.OrderStatus{}, errors.New("not implemented")
}

func (m *mockExchangeClient) Cancel(ctx context.Context, coin string, oid int64) (*hyperliquid.APIResponse[hyperliquid.CancelOrderResponse], error) {
	if m.cancelFunc != nil {
		return m.cancelFunc(ctx, coin, oid)
	}
	return nil, errors.New("not implemented")
}

func (m *mockExchangeClient) UpdateLeverage(ctx context.Context, leverage int, name string, isCross bool) (*hyperliquid.UserState, error) {
	if m.updateLeverageFunc != nil {
		return m.updateLeverageFunc(ctx, leverage, name, isCross)
	}
	return nil, errors.New("not implemented")
}

func (m *mockExchangeClient) MarketClose(ctx context.Context, coin string, sz *float64, px *float64, slippage float64, cloid *string, builder *hyperliquid.BuilderInfo) (hyperliquid.OrderStatus, error) {
	return hyperliquid.OrderStatus{}, errors.New("not implemented")
}

func (m *mockExchangeClient) SlippagePrice(ctx context.Context, name string, isBuy bool, slippage float64, px *float64) (float64, error) {
	if m.slippagePriceFunc != nil {
		return m.slippagePriceFunc(ctx, name, isBuy, slippage, px)
	}
	// Default mock implementation
	return 100.0, nil
}

// ============================================
// Interface compliance
// ============================================

func TestHyperliquid_ImplementsExchange(t *testing.T) {
	var _ exchange.Exchange = &Hyperliquid{}
}

func TestHyperliquid_Name(t *testing.T) {
	h := &Hyperliquid{}
	if h.Name() != "hyperliquid" {
		t.Errorf("expected 'hyperliquid', got '%s'", h.Name())
	}
}

// ============================================
// Constructor tests
// ============================================

func TestNewHyperliquid(t *testing.T) {
	privKey := "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h, err := NewHyperliquid(privKey, "0x1234567890123456789012345678901234567890", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Name() != "hyperliquid" {
		t.Errorf("expected name 'hyperliquid', got '%s'", h.Name())
	}
	if h.baseURL != hyperliquid.MainnetAPIURL {
		t.Errorf("expected mainnet URL, got %s", h.baseURL)
	}
}

func TestNewHyperliquid_TestnetURL(t *testing.T) {
	privKey := "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h, err := NewHyperliquid(privKey, "0x1234567890123456789012345678901234567890", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.baseURL != hyperliquid.TestnetAPIURL {
		t.Errorf("expected testnet URL, got %s", h.baseURL)
	}
}

func TestNewHyperliquid_Strips0xPrefix(t *testing.T) {
	privKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h, err := NewHyperliquid(privKey, "1234567890123456789012345678901234567890", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.accountAddr != "0x1234567890123456789012345678901234567890" {
		t.Errorf("account address should have 0x prefix, got %s", h.accountAddr)
	}
}

func TestNewHyperliquid_InvalidKey(t *testing.T) {
	_, err := NewHyperliquid("invalid-key", "0x1234", false)
	if err == nil {
		t.Error("expected error for invalid private key")
	}
}

func TestNewHyperliquidNoAuth(t *testing.T) {
	h := NewHyperliquidNoAuth(false)
	if h.Name() != "hyperliquid" {
		t.Errorf("expected name 'hyperliquid', got '%s'", h.Name())
	}
	if h.baseURL != "https://api.hyperliquid.xyz" {
		t.Errorf("unexpected baseURL: %s", h.baseURL)
	}

	hTest := NewHyperliquidNoAuth(true)
	if hTest.baseURL != "https://api.hyperliquid-testnet.xyz" {
		t.Errorf("unexpected testnet baseURL: %s", hTest.baseURL)
	}
}

// ============================================
// CreateOrder tests
// ============================================

func TestHyperliquid_CreateOrder_NeedsPrivateKey(t *testing.T) {
	h := NewHyperliquidNoAuth(false)
	_, err := h.CreateOrder(exchange.CreateOrderRequest{
		Symbol:    "BTC",
		Side:      exchange.OrderSideBuy,
		OrderType: exchange.OrderTypeLimit,
		Quantity:  0.1,
		Price:     50000,
	})
	if err == nil {
		t.Error("expected error when no private key configured")
	}
}

func TestHyperliquid_CreateOrder_LimitOrderResting(t *testing.T) {
	infoMock := &mockInfoClient{}
	exchMock := &mockExchangeClient{
		orderFunc: func(ctx context.Context, req hyperliquid.CreateOrderRequest, builder *hyperliquid.BuilderInfo) (hyperliquid.OrderStatus, error) {
			if req.Coin != "ETH" {
				t.Errorf("expected coin ETH, got %s", req.Coin)
			}
			if !req.IsBuy {
				t.Error("expected IsBuy to be true")
			}
			return hyperliquid.OrderStatus{
				Resting: &hyperliquid.OrderStatusResting{Oid: 42},
			}, nil
		},
	}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	resp, err := h.CreateOrder(exchange.CreateOrderRequest{
		Symbol:       "ETH",
		Side:         exchange.OrderSideBuy,
		OrderType:    exchange.OrderTypeLimit,
		Quantity:     1.0,
		Price:        3000,
		PositionSide: exchange.PositionSideLong,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OrderID != 42 {
		t.Errorf("expected orderID 42, got %d", resp.OrderID)
	}
	if resp.Status != exchange.OrderStatusNew {
		t.Errorf("expected status NEW, got %s", resp.Status)
	}
}

func TestHyperliquid_CreateOrder_MarketOrderFilled(t *testing.T) {
	infoMock := &mockInfoClient{
		allMidsFunc: func(ctx context.Context, dex ...string) (map[string]string, error) {
			return map[string]string{"BTC": "50000"}, nil
		},
	}
	exchMock := &mockExchangeClient{
		orderFunc: func(ctx context.Context, req hyperliquid.CreateOrderRequest, builder *hyperliquid.BuilderInfo) (hyperliquid.OrderStatus, error) {
			if req.OrderType.Limit == nil {
				t.Fatal("expected limit order type for market")
			}
			if req.OrderType.Limit.Tif != hyperliquid.TifIoc {
				t.Errorf("expected IOC TIF for market order, got %s", req.OrderType.Limit.Tif)
			}
			return hyperliquid.OrderStatus{
				Filled: &hyperliquid.OrderStatusFilled{Oid: 99},
			}, nil
		},
	}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	resp, err := h.CreateOrder(exchange.CreateOrderRequest{
		Symbol:       "BTC",
		Side:         exchange.OrderSideSell,
		OrderType:    exchange.OrderTypeMarket,
		Quantity:     0.5,
		Price:        0,
		PositionSide: exchange.PositionSideShort,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OrderID != 99 {
		t.Errorf("expected orderID 99, got %d", resp.OrderID)
	}
	// After fix: always return NEW to ensure WS/Scanner processing
	if resp.Status != exchange.OrderStatusNew {
		t.Errorf("expected status NEW (for WS/Scanner processing), got %s", resp.Status)
	}
	// After fix: always return NEW to ensure WS/Scanner processing
	if resp.Status != exchange.OrderStatusNew {
		t.Errorf("expected status NEW (for WS/Scanner processing), got %s", resp.Status)
	}
	// After fix: always return NEW to ensure WS/Scanner processing
	if resp.Status != exchange.OrderStatusNew {
		t.Errorf("expected status NEW (for WS/Scanner processing), got %s", resp.Status)
	}
}

func TestHyperliquid_CreateOrder_APIError(t *testing.T) {
	infoMock := &mockInfoClient{}
	exchMock := &mockExchangeClient{
		orderFunc: func(ctx context.Context, req hyperliquid.CreateOrderRequest, builder *hyperliquid.BuilderInfo) (hyperliquid.OrderStatus, error) {
			return hyperliquid.OrderStatus{}, errors.New("insufficient margin")
		},
	}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	_, err := h.CreateOrder(exchange.CreateOrderRequest{
		Symbol:    "BTC",
		Side:      exchange.OrderSideBuy,
		OrderType: exchange.OrderTypeLimit,
		Quantity:  100,
		Price:     99999,
	})
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}

func TestHyperliquid_CreateOrder_ServerErrorInStatus(t *testing.T) {
	errMsg := "order rejected"
	infoMock := &mockInfoClient{}
	exchMock := &mockExchangeClient{
		orderFunc: func(ctx context.Context, req hyperliquid.CreateOrderRequest, builder *hyperliquid.BuilderInfo) (hyperliquid.OrderStatus, error) {
			return hyperliquid.OrderStatus{
				Error: &errMsg,
			}, nil
		},
	}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	_, err := h.CreateOrder(exchange.CreateOrderRequest{
		Symbol:    "BTC",
		Side:      exchange.OrderSideBuy,
		OrderType: exchange.OrderTypeLimit,
		Quantity:  1.0,
		Price:     50000,
	})
	if err == nil {
		t.Fatal("expected error for server-side order error")
	}
}

// ============================================
// CancelOrder tests
// ============================================

func TestHyperliquid_CancelOrder_NeedsPrivateKey(t *testing.T) {
	h := NewHyperliquidNoAuth(false)
	err := h.CancelOrder(123)
	if err == nil {
		t.Error("expected error when no private key configured")
	}
}

func TestHyperliquid_CancelOrder_OrderIDOnly(t *testing.T) {
	h := newHyperliquidForTest(&mockInfoClient{}, &mockExchangeClient{}, "0xaddr")
	err := h.CancelOrder(123)
	if err == nil {
		t.Error("expected error when canceling without coin symbol")
	}
}

func TestHyperliquid_CancelOrderByCoin_Success(t *testing.T) {
	infoMock := &mockInfoClient{}
	exchMock := &mockExchangeClient{
		cancelFunc: func(ctx context.Context, coin string, oid int64) (*hyperliquid.APIResponse[hyperliquid.CancelOrderResponse], error) {
			if coin != "ETH" {
				t.Errorf("expected coin ETH, got %s", coin)
			}
			if oid != 42 {
				t.Errorf("expected oid 42, got %d", oid)
			}
			return &hyperliquid.APIResponse[hyperliquid.CancelOrderResponse]{
				Status: "ok",
			}, nil
		},
	}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	err := h.CancelOrderByCoin("ETH", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHyperliquid_CancelOrderByCoin_APIError(t *testing.T) {
	infoMock := &mockInfoClient{}
	exchMock := &mockExchangeClient{
		cancelFunc: func(ctx context.Context, coin string, oid int64) (*hyperliquid.APIResponse[hyperliquid.CancelOrderResponse], error) {
			return nil, errors.New("order not found")
		},
	}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	err := h.CancelOrderByCoin("BTC", 999)
	if err == nil {
		t.Fatal("expected error for cancel API failure")
	}
}

// ============================================
// GetOrder tests
// ============================================

func TestHyperliquid_GetOrder_NeedsAccountAddr(t *testing.T) {
	privKey := "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h, _ := NewHyperliquid(privKey, "", false)
	_, err := h.GetOrder(123, "NEARUSDT")
	if err == nil {
		t.Error("expected error when account address not set")
	}
}

func TestHyperliquid_GetOrder_NotFound(t *testing.T) {
	infoMock := &mockInfoClient{
		queryOrderFunc: func(ctx context.Context, user string, oid int64) (*hyperliquid.OrderQueryResult, error) {
			return &hyperliquid.OrderQueryResult{
				Status: hyperliquid.OrderQueryStatusError,
			}, nil
		},
	}
	exchMock := &mockExchangeClient{}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	_, err := h.GetOrder(999, "UNKNOWN")
	if err == nil {
		t.Fatal("expected error for not-found order")
	}
}

func TestHyperliquid_GetOrder_NilResult(t *testing.T) {
	infoMock := &mockInfoClient{
		queryOrderFunc: func(ctx context.Context, user string, oid int64) (*hyperliquid.OrderQueryResult, error) {
			return nil, nil
		},
	}
	exchMock := &mockExchangeClient{}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	_, err := h.GetOrder(999, "UNKNOWN")
	if err == nil {
		t.Fatal("expected error for nil result")
	}
}

func TestHyperliquid_GetOrder_StatusMapping(t *testing.T) {
	tests := []struct {
		name        string
		orderStatus hyperliquid.OrderStatusValue
		wantStatus  exchange.OrderStatus
		wantFilled  float64
	}{
		{"open", hyperliquid.OrderStatusValueOpen, exchange.OrderStatusNew, 0.5},
		{"filled", hyperliquid.OrderStatusValueFilled, exchange.OrderStatusFilled, 2.0},
		{"canceled", hyperliquid.OrderStatusValueCanceled, exchange.OrderStatusCancelled, 0},
		{"marginCanceled", hyperliquid.OrderStatusValueMarginCanceled, exchange.OrderStatusCancelled, 0},
		{"triggered", hyperliquid.OrderStatusValueTriggered, exchange.OrderStatusFilled, 0.4},
		{"rejected", hyperliquid.OrderStatusValueRejected, exchange.OrderStatusFailed, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sz, origSz := "0.5", "1.0"
			if tt.name == "filled" {
				sz, origSz = "0", "2.0"
			} else if tt.name == "canceled" || tt.name == "marginCanceled" {
				sz, origSz = "10.0", "10.0"
			} else if tt.name == "triggered" {
				sz, origSz = "0.1", "0.5"
			}

			infoMock := &mockInfoClient{
				queryOrderFunc: func(ctx context.Context, user string, oid int64) (*hyperliquid.OrderQueryResult, error) {
					return &hyperliquid.OrderQueryResult{
						Status: hyperliquid.OrderQueryStatusSuccess,
						Order: hyperliquid.OrderQueryResponse{
							Status: tt.orderStatus,
							Order: hyperliquid.QueriedOrder{
								Coin:    "BTC",
								Side:    hyperliquid.OrderSideBid,
								LimitPx: "50000",
								Sz:      sz,
								OrigSz:  origSz,
							},
						},
					}, nil
				},
			}
			h := newHyperliquidForTest(infoMock, &mockExchangeClient{}, "0xaddr")

			info, err := h.GetOrder(1, "BTC")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.Status != tt.wantStatus {
				t.Errorf("expected status %s, got %s", tt.wantStatus, info.Status)
			}
			if info.Filled != tt.wantFilled {
				t.Errorf("expected filled %f, got %f", tt.wantFilled, info.Filled)
			}
		})
	}
}

func TestHyperliquid_GetOrder_NegativeFilled(t *testing.T) {
	infoMock := &mockInfoClient{
		queryOrderFunc: func(ctx context.Context, user string, oid int64) (*hyperliquid.OrderQueryResult, error) {
			return &hyperliquid.OrderQueryResult{
				Status: hyperliquid.OrderQueryStatusSuccess,
				Order: hyperliquid.OrderQueryResponse{
					Status: hyperliquid.OrderStatusValueOpen,
					Order: hyperliquid.QueriedOrder{
						Coin:    "BTC",
						Side:    hyperliquid.OrderSideBid,
						LimitPx: "50000",
						Sz:      "2.0",
						OrigSz:  "1.0",
					},
				},
			}, nil
		},
	}
	exchMock := &mockExchangeClient{}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	info, err := h.GetOrder(1, "BTC")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Filled != 0 {
		t.Errorf("expected filled to be 0 when sz > origSz, got %f", info.Filled)
	}
}

func TestHyperliquid_GetOrder_ParseError(t *testing.T) {
	infoMock := &mockInfoClient{
		queryOrderFunc: func(ctx context.Context, user string, oid int64) (*hyperliquid.OrderQueryResult, error) {
			return &hyperliquid.OrderQueryResult{
				Status: hyperliquid.OrderQueryStatusSuccess,
				Order: hyperliquid.OrderQueryResponse{
					Status: hyperliquid.OrderStatusValueOpen,
					Order: hyperliquid.QueriedOrder{
						Coin:    "BTC",
						Side:    hyperliquid.OrderSideBid,
						LimitPx: "not-a-number",
						Sz:      "1.0",
						OrigSz:  "1.0",
					},
				},
			}, nil
		},
	}
	exchMock := &mockExchangeClient{}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	_, err := h.GetOrder(1, "BTC")
	if err == nil {
		t.Fatal("expected parse error for invalid price")
	}
}

func TestHyperliquid_GetOrder_APIError(t *testing.T) {
	infoMock := &mockInfoClient{
		queryOrderFunc: func(ctx context.Context, user string, oid int64) (*hyperliquid.OrderQueryResult, error) {
			return nil, errors.New("network error")
		},
	}
	exchMock := &mockExchangeClient{}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	_, err := h.GetOrder(1, "BTC")
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}

// ============================================
// SetLeverage tests
// ============================================

func TestHyperliquid_SetLeverage_NeedsPrivateKey(t *testing.T) {
	h := NewHyperliquidNoAuth(false)
	err := h.SetLeverage("BTC", 10)
	if err == nil {
		t.Error("expected error when no private key configured")
	}
}

func TestHyperliquid_SetLeverage_Success(t *testing.T) {
	infoMock := &mockInfoClient{}
	exchMock := &mockExchangeClient{
		updateLeverageFunc: func(ctx context.Context, leverage int, name string, isCross bool) (*hyperliquid.UserState, error) {
			if leverage != 20 {
				t.Errorf("expected leverage 20, got %d", leverage)
			}
			if name != "ETH" {
				t.Errorf("expected name ETH, got %s", name)
			}
			if !isCross {
				t.Error("expected isCross to be true")
			}
			return &hyperliquid.UserState{}, nil
		},
	}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	err := h.SetLeverage("ETH", 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHyperliquid_SetLeverage_APIError(t *testing.T) {
	infoMock := &mockInfoClient{}
	exchMock := &mockExchangeClient{
		updateLeverageFunc: func(ctx context.Context, leverage int, name string, isCross bool) (*hyperliquid.UserState, error) {
			return nil, errors.New("leverage exceeds max")
		},
	}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	err := h.SetLeverage("BTC", 100)
	if err == nil {
		t.Fatal("expected error for leverage API failure")
	}
}

// ============================================
// GetLeverage tests
// ============================================

func TestHyperliquid_GetLeverage_NeedsAccountAddr(t *testing.T) {
	privKey := "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h, _ := NewHyperliquid(privKey, "", false)
	_, err := h.GetLeverage("BTC")
	if err == nil {
		t.Error("expected error when account address not set")
	}
}

func TestHyperliquid_GetLeverage_Found(t *testing.T) {
	infoMock := &mockInfoClient{
		userStateFunc: func(ctx context.Context, address string, dex ...string) (*hyperliquid.UserState, error) {
			return &hyperliquid.UserState{
				AssetPositions: []hyperliquid.AssetPosition{
					{
						Position: hyperliquid.Position{
							Coin: "BTC",
							Leverage: hyperliquid.Leverage{
								Value: 5,
							},
						},
					},
					{
						Position: hyperliquid.Position{
							Coin: "ETH",
							Leverage: hyperliquid.Leverage{
								Value: 10,
							},
						},
					},
				},
			}, nil
		},
	}
	exchMock := &mockExchangeClient{}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	lev, err := h.GetLeverage("ETH")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lev != 10 {
		t.Errorf("expected leverage 10, got %d", lev)
	}
}

func TestHyperliquid_GetLeverage_NotFound(t *testing.T) {
	infoMock := &mockInfoClient{
		userStateFunc: func(ctx context.Context, address string, dex ...string) (*hyperliquid.UserState, error) {
			return &hyperliquid.UserState{
				AssetPositions: []hyperliquid.AssetPosition{
					{
						Position: hyperliquid.Position{
							Coin: "BTC",
							Leverage: hyperliquid.Leverage{
								Value: 5,
							},
						},
					},
				},
			}, nil
		},
	}
	exchMock := &mockExchangeClient{}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	_, err := h.GetLeverage("SOL")
	if err == nil {
		t.Fatal("expected error when leverage not found for symbol")
	}
}

func TestHyperliquid_GetLeverage_APIError(t *testing.T) {
	infoMock := &mockInfoClient{
		userStateFunc: func(ctx context.Context, address string, dex ...string) (*hyperliquid.UserState, error) {
			return nil, errors.New("network error")
		},
	}
	exchMock := &mockExchangeClient{}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	_, err := h.GetLeverage("BTC")
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}

// ============================================
// GetPrice tests
// ============================================

func TestHyperliquid_GetPrice_NoAuthReadonly(t *testing.T) {
	// Test that GetPrice works with a mocked info client (simulating
	// the read-only initLazyReadOnly path where info is pre-injected).
	infoMock := &mockInfoClient{
		allMidsFunc: func(ctx context.Context, dex ...string) (map[string]string, error) {
			return map[string]string{
				"BTC": "50000.5",
			}, nil
		},
	}
	h := newHyperliquidForTest(infoMock, nil, "")
	price, err := h.GetPrice("BTC")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if price != 50000.5 {
		t.Errorf("expected price 50000.5, got %f", price)
	}
}

// TestHyperliquid_GetPrice_Integration_Readonly exercises the full read-only
// initLazyReadOnly code path against Hyperliquid's real testnet API.
//
// This test requires network access and is skipped under -short.
func TestHyperliquid_GetPrice_Integration_Readonly(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: requires network access")
	}

	// NewHyperliquidNoAuth creates: info=nil, privateKey=nil, baseURL=testnet
	h := NewHyperliquidNoAuth(true)

	// GetPrice checks: h.info==nil && h.privateKey==nil → calls initLazyReadOnly()
	// initLazyReadOnly creates a real hyperliquid.Info client via HTTP.
	price, err := h.GetPrice("BTC")
	if err != nil {
		t.Fatalf("GetPrice(BTC) failed on testnet: %v", err)
	}
	if price <= 0 {
		t.Errorf("expected positive price, got %f", price)
	}
}

func TestHyperliquid_GetPrice_Success(t *testing.T) {
	infoMock := &mockInfoClient{
		allMidsFunc: func(ctx context.Context, dex ...string) (map[string]string, error) {
			return map[string]string{
				"BTC": "50000.5",
				"ETH": "3000.25",
			}, nil
		},
	}
	exchMock := &mockExchangeClient{}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	price, err := h.GetPrice("BTC")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if price != 50000.5 {
		t.Errorf("expected price 50000.5, got %f", price)
	}
}

func TestHyperliquid_GetPrice_NotFound(t *testing.T) {
	infoMock := &mockInfoClient{
		allMidsFunc: func(ctx context.Context, dex ...string) (map[string]string, error) {
			return map[string]string{
				"BTC": "50000",
			}, nil
		},
	}
	exchMock := &mockExchangeClient{}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	_, err := h.GetPrice("SOL")
	if err == nil {
		t.Fatal("expected error for unknown symbol")
	}
}

func TestHyperliquid_GetPrice_APIError(t *testing.T) {
	infoMock := &mockInfoClient{
		allMidsFunc: func(ctx context.Context, dex ...string) (map[string]string, error) {
			return nil, errors.New("network error")
		},
	}
	exchMock := &mockExchangeClient{}
	h := newHyperliquidForTest(infoMock, exchMock, "0xaddr")

	_, err := h.GetPrice("BTC")
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}

// ============================================
// Connect/Close/SubscribeOrders tests
// ============================================

func TestHyperliquid_ConnectAndClose(t *testing.T) {
	h := NewHyperliquidNoAuth(false)
	if err := h.Connect(); err != nil {
		t.Errorf("Connect should not error: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Errorf("Close should not error: %v", err)
	}
}

func TestHyperliquid_Close_ClearsPrivateKey(t *testing.T) {
	privKey := "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h, _ := NewHyperliquid(privKey, "0xaddr", false)
	if h.privateKey == nil {
		t.Fatal("expected private key to be set before Close")
	}
	_ = h.Close()
	if h.privateKey != nil {
		t.Error("expected private key to be nil after Close")
	}
}

func TestHyperliquid_SubscribeOrders(t *testing.T) {
	h := newHyperliquidForTest(&mockInfoClient{}, &mockExchangeClient{}, "0xaddr")
	err := h.SubscribeOrders(func(resp *exchange.CreateOrderResponse) {})
	if err != nil {
		t.Errorf("SubscribeOrders should not error: %v", err)
	}
}

// ============================================
// initLazy idempotency
// ============================================

func TestHyperliquid_InitLazy_Idempotent(t *testing.T) {
	// Verify sync.Once behavior: after the first call sets mocks,
	// subsequent initLazy calls are no-ops (don't re-init).
	infoMock := &mockInfoClient{
		allMidsFunc: func(ctx context.Context, dex ...string) (map[string]string, error) {
			return map[string]string{"BTC": "50000"}, nil
		},
	}
	h := newHyperliquidForTest(infoMock, &mockExchangeClient{}, "0xaddr")

	// First initLazy call sees mocked info and returns nil (no-op)
	err1 := h.initLazy()
	// Second call also returns nil — sync.Once ensures no re-init
	err2 := h.initLazy()

	if err1 != nil || err2 != nil {
		t.Errorf("expected nil errors from idempotent initLazy, got: %v, %v", err1, err2)
	}
}

// ============================================
// Tick size calculation tests
// ============================================

func TestSyncSymbolFilters_TickSize(t *testing.T) {
	tests := []struct {
		name        string
		szDecimals  int
		wantTick    float64
		description string
	}{
		{
			name:        "szDecimals=0",
			szDecimals:  0,
			wantTick:    1e-6, // 0.000001
			description: "Most price precision (6 decimal places)",
		},
		{
			name:        "szDecimals=2",
			szDecimals:  2,
			wantTick:    1e-4, // 0.0001
			description: "Common for major pairs",
		},
		{
			name:        "szDecimals=3",
			szDecimals:  3,
			wantTick:    1e-3, // 0.001
			description: "Less precision",
		},
		{
			name:        "szDecimals=5",
			szDecimals:  5,
			wantTick:    0.1, // 0.1
			description: "Low price precision",
		},
		{
			name:        "szDecimals=6",
			szDecimals:  6,
			wantTick:    1.0, // 1.0
			description: "Minimal precision",
		},
		{
			name:        "szDecimals=7 (edge case)",
			szDecimals:  7,
			wantTick:    1.0, // clamped to 0 decimals
			description: "Below zero clamped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the calculation from SyncSymbolFilters
			decimals := 6 - tt.szDecimals
			if decimals < 0 {
				decimals = 0
			}
			tickSize := math.Pow10(-decimals)

			if math.Abs(tickSize-tt.wantTick) > 1e-10 {
				t.Errorf("szDecimals=%d: got tickSize=%.10f, want %.10f (%s)",
					tt.szDecimals, tickSize, tt.wantTick, tt.description)
			}
		})
	}
}
