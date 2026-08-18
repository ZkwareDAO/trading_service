package exchange

import (
	"testing"
	"time"
)

func TestOrderSideConstants(t *testing.T) {
	if OrderSideBuy != OrderSideBuy {
		t.Error("OrderSideBuy should equal itself")
	}
}

func TestPositionSideConstants(t *testing.T) {
	if PositionSideLong != PositionSideLong {
		t.Error("PositionSideLong should equal itself")
	}
}

func TestOrderTypeConstants(t *testing.T) {
	if OrderTypeLimit != OrderTypeLimit {
		t.Error("OrderTypeLimit should equal itself")
	}
}

func TestOrderStatusConstants(t *testing.T) {
	if OrderStatusNew != OrderStatusNew {
		t.Error("OrderStatusNew should equal itself")
	}
}

func TestCreateOrderRequestStruct(t *testing.T) {
	req := CreateOrderRequest{
		Symbol:       "BTCUSDT",
		Side:         OrderSideBuy,
		OrderType:    OrderTypeLimit,
		Quantity:     0.1,
		Price:        50000,
		PositionSide: PositionSideLong,
		UserID:       1,
		RelationID:   100,
		RelationType: "user_orders",
	}
	if req.Symbol != "BTCUSDT" {
		t.Errorf("expected 'BTCUSDT', got '%s'", req.Symbol)
	}
}

func TestCreateOrderResponseStruct(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	resp := CreateOrderResponse{
		OrderID:    12345,
		Symbol:     "BTCUSDT",
		Side:       OrderSideBuy,
		Status:     OrderStatusNew,
		Price:      50000,
		Quantity:   0.1,
		ExecutedAt: now,
	}
	if resp.OrderID != 12345 {
		t.Errorf("expected OrderID 12345, got %d", resp.OrderID)
	}
}

func TestExchangeInterface(t *testing.T) {
	// Verify MockExchange implements Exchange
	var _ Exchange = &MockExchange{}
}

func TestMockExchange_Name(t *testing.T) {
	mock := NewMockExchange()
	if mock.Name() != "mock" {
		t.Errorf("expected 'mock', got '%s'", mock.Name())
	}
}

func TestMockExchange_CreateOrder(t *testing.T) {
	mock := NewMockExchange()

	req := CreateOrderRequest{
		Symbol:    "BTCUSDT",
		Side:      OrderSideBuy,
		OrderType: OrderTypeLimit,
		Quantity:  0.1,
		Price:     50000,
	}

	resp, err := mock.CreateOrder(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OrderID == 0 {
		t.Error("expected non-zero OrderID")
	}
	if resp.Symbol != "BTCUSDT" {
		t.Errorf("expected 'BTCUSDT', got '%s'", resp.Symbol)
	}
	if resp.Status != OrderStatusNew {
		t.Errorf("expected Status NEW, got '%s'", resp.Status)
	}

	// Verify order was recorded
	if len(mock.CreatedOrders) != 1 {
		t.Errorf("expected 1 created order, got %d", len(mock.CreatedOrders))
	}
}

func TestMockExchange_CreateOrderMultiple(t *testing.T) {
	mock := NewMockExchange()

	for i := 0; i < 3; i++ {
		mock.CreateOrder(CreateOrderRequest{
			Symbol: "BTCUSDT", Side: OrderSideBuy, OrderType: OrderTypeMarket,
		})
	}

	if len(mock.CreatedOrders) != 3 {
		t.Errorf("expected 3 created orders, got %d", len(mock.CreatedOrders))
	}
	// IDs should be sequential
	if mock.CreatedOrders[0].OrderID != 1 {
		t.Errorf("expected first order ID 1, got %d", mock.CreatedOrders[0].OrderID)
	}
	if mock.CreatedOrders[2].OrderID != 3 {
		t.Errorf("expected third order ID 3, got %d", mock.CreatedOrders[2].OrderID)
	}
}

func TestMockExchange_GetOrder(t *testing.T) {
	mock := NewMockExchange()

	resp, _ := mock.CreateOrder(CreateOrderRequest{
		Symbol: "BTCUSDT", Side: OrderSideBuy, OrderType: OrderTypeLimit,
		Quantity: 0.1, Price: 50000,
	})

	info, err := mock.GetOrder(resp.OrderID, resp.Symbol)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.OrderID != resp.OrderID {
		t.Errorf("expected OrderID %d, got %d", resp.OrderID, info.OrderID)
	}
}

func TestMockExchange_GetOrderNotFound(t *testing.T) {
	mock := NewMockExchange()

	_, err := mock.GetOrder(999, "UNKNOWN")
	if err == nil {
		t.Error("expected error for non-existent order")
	}
}

func TestMockExchange_CancelOrder(t *testing.T) {
	mock := NewMockExchange()

	resp, _ := mock.CreateOrder(CreateOrderRequest{
		Symbol: "BTCUSDT", Side: OrderSideBuy, OrderType: OrderTypeLimit,
	})

	if err := mock.CancelOrder(resp.OrderID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify order was cancelled
	if mock.CancelledOrders[resp.OrderID] != true {
		t.Error("expected order to be marked as cancelled")
	}
}

func TestMockExchange_GetPrice(t *testing.T) {
	mock := NewMockExchange()
	mock.SetPrice("BTCUSDT", 50000)

	price, err := mock.GetPrice("BTCUSDT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if price != 50000 {
		t.Errorf("expected 50000, got %f", price)
	}
}

func TestMockExchange_SetLeverage(t *testing.T) {
	mock := NewMockExchange()

	if err := mock.SetLeverage("BTCUSDT", 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lev, err := mock.GetLeverage("BTCUSDT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lev != 10 {
		t.Errorf("expected leverage 10, got %d", lev)
	}
}

func TestMockExchange_SubscribeOrders(t *testing.T) {
	mock := NewMockExchange()

	orderCount := 0
	cb := func(resp *CreateOrderResponse) {
		orderCount++
	}

	if err := mock.SubscribeOrders(cb); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate a filled order notification
	mock.notifySubscribers(&CreateOrderResponse{OrderID: 1, Status: OrderStatusFilled})
	if orderCount != 1 {
		t.Errorf("expected 1 callback, got %d", orderCount)
	}
}
