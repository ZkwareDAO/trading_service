package exchange

import (
	"trading-service/internal/order"
	"time"
)

// ============================================
// Types
// ============================================

// OrderSide represents buy/sell direction.
type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

// PositionSide represents long/short position.
type PositionSide string

const (
	PositionSideLong  PositionSide = "LONG"
	PositionSideShort PositionSide = "SHORT"
)

// OrderType represents limit/market order.
type OrderType string

const (
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeMarket OrderType = "MARKET"
)

// OrderStatus represents order lifecycle state.
type OrderStatus string

const (
	OrderStatusNew       OrderStatus = "NEW"
	OrderStatusFilled    OrderStatus = "FILLED"
	OrderStatusCancelled OrderStatus = "CANCELLED"
	OrderStatusFailed    OrderStatus = "FAILED"
)

// ============================================
// Request/Response
// ============================================

// CreateOrderRequest is the input for placing an order.
type CreateOrderRequest struct {
	Symbol       string
	Side         OrderSide
	OrderType    OrderType
	Quantity     float64
	Price        float64
	PositionSide PositionSide
	ReduceOnly   bool // true for risk control orders (only reduce position, never open new)

	UserID       uint64
	RelationID   uint64
	RelationType string
}

// CreateOrderResponse is the output from placing an order.
type CreateOrderResponse struct {
	OrderID    uint64
	Symbol     string
	Side       OrderSide
	Status     OrderStatus
	Price      float64
	Quantity   float64
	ExecutedAt time.Time
}

// OrderInfo is detailed order information.
type OrderInfo struct {
	OrderID   uint64
	Symbol    string
	Side      OrderSide
	Status    OrderStatus
	Price     float64 // Order price (limit price or trigger price)
	Qty       float64 // Original order quantity
	Filled    float64 // Filled quantity
	AvgPrice  float64 // Average execution price (actual fill price)
}

// PositionInfo represents a position on the exchange.
type PositionInfo struct {
	Symbol        string        // Trading pair symbol
	PositionSide  PositionSide  // LONG or SHORT
	Quantity      float64       // Position size (absolute value)
	EntryPrice    float64       // Average entry price
	MarkPrice     float64       // Current mark price
	UnrealizedPnl float64       // Unrealized profit/loss
	Leverage      int           // Current leverage (0 if not applicable)
}

// MakePositionInfo creates a PositionInfo with normalized quantity.
// Negative quantities indicate SHORT positions and are converted to positive.
func MakePositionInfo(symbol string, qty float64, entryPrice, markPrice, unrealizedPnl float64, leverage int) PositionInfo {
	positionSide := PositionSideLong
	if qty < 0 {
		positionSide = PositionSideShort
		qty = -qty
	}
	return PositionInfo{
		Symbol:        symbol,
		PositionSide:  positionSide,
		Quantity:      qty,
		EntryPrice:    entryPrice,
		MarkPrice:     markPrice,
		UnrealizedPnl: unrealizedPnl,
		Leverage:      leverage,
	}
}

// OrderCallback is invoked on order status changes.
type OrderCallback func(*CreateOrderResponse)

// ============================================
// Exchange interface
// ============================================

// Exchange abstracts a trading venue.
type Exchange interface {
	Name() string
	CreateOrder(req CreateOrderRequest) (*CreateOrderResponse, error)
	CancelOrder(orderID uint64) error
	GetOrder(orderID uint64, symbol string) (*OrderInfo, error)
	GetPositions() ([]PositionInfo, error) // Query all positions with non-zero quantity
	SetLeverage(symbol string, leverage int) error
	GetLeverage(symbol string) (int, error)
	GetPrice(symbol string) (float64, error)
	Connect() error
	Close() error
	SubscribeOrders(callback OrderCallback) error
}

// ============================================
// Optional interface: Precision validation
// ============================================

// FilterSource provides exchange symbol filters for order validation.
type FilterSource interface {
	ListExchangeSymbolFilters(exchange string, posType order.PosType, symbol string) []*order.ExchangeSymbolFilter
}

// PrecisionValidator is an optional interface for exchanges that need precision validation.
// Only exchanges that require strict precision (like Binance) need to implement this.
// Hyperliquid does not need to implement this as it uses SDK for precision handling.
type PrecisionValidator interface {
	SetFilterSource(source FilterSource)
}
