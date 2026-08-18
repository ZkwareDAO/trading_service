package order

import (
	"fmt"
	"math"
)

const filterEpsilon = 1e-9

// ValidateExchangeSymbolFilters validates price, quantity, and notional against exchange filters.
func ValidateExchangeSymbolFilters(filters []*ExchangeSymbolFilter, price, quantity float64) error {
	for _, filter := range filters {
		if filter == nil {
			continue
		}
		if err := validateExchangeSymbolFilter(filter, price, quantity); err != nil {
			return err
		}
	}
	return nil
}

func validateExchangeSymbolFilter(filter *ExchangeSymbolFilter, price, quantity float64) error {
	if filter.MinPrice > 0 && price+filterEpsilon < filter.MinPrice {
		return fmt.Errorf("price %.8f below min_price %.8f for %s", price, filter.MinPrice, filter.Symbol)
	}
	if filter.MaxPrice > 0 && price-filterEpsilon > filter.MaxPrice {
		return fmt.Errorf("price %.8f above max_price %.8f for %s", price, filter.MaxPrice, filter.Symbol)
	}
	if filter.TickSize > 0 && !isMultiple(price, filter.TickSize) {
		return fmt.Errorf("price %.8f does not match tick_size %.8f for %s", price, filter.TickSize, filter.Symbol)
	}
	if filter.MinQty > 0 && quantity+filterEpsilon < filter.MinQty {
		return fmt.Errorf("quantity %.8f below min_qty %.8f for %s", quantity, filter.MinQty, filter.Symbol)
	}
	if filter.MaxQty > 0 && quantity-filterEpsilon > filter.MaxQty {
		return fmt.Errorf("quantity %.8f above max_qty %.8f for %s", quantity, filter.MaxQty, filter.Symbol)
	}
	if filter.StepSize > 0 && !isMultiple(quantity, filter.StepSize) {
		return fmt.Errorf("quantity %.8f does not match step_size %.8f for %s", quantity, filter.StepSize, filter.Symbol)
	}
	if filter.MinNotional > 0 && price*quantity+filterEpsilon < filter.MinNotional {
		return fmt.Errorf("notional %.8f below min_notional %.8f for %s", price*quantity, filter.MinNotional, filter.Symbol)
	}
	return nil
}

func isMultiple(value, step float64) bool {
	if step <= 0 {
		return true
	}
	quotient := value / step
	return math.Abs(quotient-math.Round(quotient)) <= filterEpsilon
}
