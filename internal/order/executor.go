package order

import "fmt"

// ============================================
// Calculator
// ============================================

// CalculateCashToQuantity converts cash to quantity: cash * leverage / price.
func CalculateCashToQuantity(cash, price float64, leverage int) float64 {
	if price <= 0 || leverage <= 0 {
		return 0
	}
	return cash * float64(leverage) / price
}

// ApplySlippage adjusts price for slippage.
func ApplySlippage(price, slippage float64, side Side) float64 {
	if side == SideLong {
		return price * (1 + slippage)
	}
	return price * (1 - slippage)
}

// CalculatePnL calculates unrealized PnL.
func CalculatePnL(entryPrice, exitPrice, quantity float64, side Side) float64 {
	if side == SideLong {
		return (exitPrice - entryPrice) * quantity
	}
	return (entryPrice - exitPrice) * quantity
}

// ============================================
// Validator
// ============================================

// CreateOrderRequest for order validation.
type CreateOrderRequest struct {
	OrderType int
	Price     float64
	Quantity  float64
}

// VerifyCreateOrderInfo validates order parameters.
func VerifyCreateOrderInfo(req CreateOrderRequest) error {
	if req.Quantity <= 0 {
		return fmt.Errorf("quantity must be positive, got %f", req.Quantity)
	}
	if req.OrderType == 0 && req.Price <= 0 {
		return fmt.Errorf("limit order requires a positive price")
	}
	return nil
}
