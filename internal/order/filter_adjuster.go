package order

import "math"

// TruncateToExchangeSymbolFilters truncates price and quantity down to exchange increments.
func TruncateToExchangeSymbolFilters(filters []*ExchangeSymbolFilter, price, quantity float64) (float64, float64) {
	truncatedPrice := price
	truncatedQuantity := quantity
	for _, filter := range filters {
		if filter == nil {
			continue
		}
		if filter.TickSize > 0 {
			truncatedPrice = truncateToStep(truncatedPrice, filter.TickSize)
		}
		if filter.StepSize > 0 {
			truncatedQuantity = truncateToStep(truncatedQuantity, filter.StepSize)
		}
	}
	return truncatedPrice, truncatedQuantity
}

func truncateToStep(value, step float64) float64 {
	if step <= 0 {
		return value
	}
	return math.Floor((value+filterEpsilon)/step) * step
}
