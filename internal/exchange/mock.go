package exchange

import (
	"fmt"
	"sync"
	"time"
)

// MockExchange is an in-memory exchange for testing.
type MockExchange struct {
	mu              sync.Mutex
	orders          map[uint64]*CreateOrderResponse
	orderInfos      map[uint64]*OrderInfo
	nextID          uint64
	cancelled       map[uint64]bool
	prices          map[string]float64
	leverages       map[string]int
	callbacks       []OrderCallback
	CreatedOrders   []*CreateOrderResponse
	CancelledOrders map[uint64]bool
	callOrder       []string // track call order for testing

	// Error injection
	forceCreateError bool
}

// NewMockExchange creates a new mock exchange.
func NewMockExchange() *MockExchange {
	return &MockExchange{
		orders:          make(map[uint64]*CreateOrderResponse),
		orderInfos:      make(map[uint64]*OrderInfo),
		cancelled:       make(map[uint64]bool),
		prices:          make(map[string]float64),
		leverages:       make(map[string]int),
		CreatedOrders:   make([]*CreateOrderResponse, 0),
		CancelledOrders: make(map[uint64]bool),
		callOrder:       make([]string, 0),
	}
}

// SetCreateError enables/disables forced CreateOrder error.
func (m *MockExchange) SetCreateError(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forceCreateError = v
}

// CallOrder returns the ordered list of method calls (SetLeverage, CreateOrder).
func (m *MockExchange) CallOrder() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.callOrder))
	copy(result, m.callOrder)
	return result
}

func (m *MockExchange) Name() string { return "mock" }

func (m *MockExchange) CreateOrder(req CreateOrderRequest) (*CreateOrderResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callOrder = append(m.callOrder, "CreateOrder")

	if m.forceCreateError {
		return nil, fmt.Errorf("forced mock error")
	}

	m.nextID++
	id := m.nextID

	resp := &CreateOrderResponse{
		OrderID:    id,
		Symbol:     req.Symbol,
		Side:       req.Side,
		Status:     OrderStatusNew,
		Price:      req.Price,
		Quantity:   req.Quantity,
		ExecutedAt: time.Now(),
	}

	m.orders[id] = resp
	m.orderInfos[id] = &OrderInfo{
		OrderID: id, Symbol: req.Symbol, Side: req.Side,
		Status: OrderStatusNew, Price: req.Price, Qty: req.Quantity,
	}
	m.CreatedOrders = append(m.CreatedOrders, resp)
	return resp, nil
}

func (m *MockExchange) CancelOrder(orderID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.orders[orderID]; !ok {
		return fmt.Errorf("order %d not found", orderID)
	}
	m.orders[orderID].Status = OrderStatusCancelled
	m.orderInfos[orderID].Status = OrderStatusCancelled
	m.cancelled[orderID] = true
	m.CancelledOrders[orderID] = true
	return nil
}

func (m *MockExchange) GetOrder(orderID uint64, symbol string) (*OrderInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.orderInfos[orderID]
	if !ok {
		return nil, fmt.Errorf("order %d not found", orderID)
	}
	return info, nil
}

func (m *MockExchange) SetLeverage(symbol string, leverage int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callOrder = append(m.callOrder, "SetLeverage")
	m.leverages[symbol] = leverage
	return nil
}

func (m *MockExchange) GetLeverage(symbol string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leverages[symbol], nil
}

func (m *MockExchange) GetPrice(symbol string) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prices[symbol], nil
}

func (m *MockExchange) GetPositions() ([]PositionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mock implementation: return empty positions
	return []PositionInfo{}, nil
}

// SetPrice sets the mock price for a symbol.
func (m *MockExchange) SetPrice(symbol string, price float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prices[symbol] = price
}

func (m *MockExchange) Connect() error { return nil }
func (m *MockExchange) Close() error   { return nil }

func (m *MockExchange) SubscribeOrders(callback OrderCallback) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, callback)
	return nil
}

// notifySubscribers triggers all registered callbacks (for testing).
func (m *MockExchange) notifySubscribers(resp *CreateOrderResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cb := range m.callbacks {
		cb(resp)
	}
}
