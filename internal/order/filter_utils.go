package order

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// OrderType constants for validation (matching exchange.OrderType)
const (
	OrderTypeLimit  = "LIMIT"
	OrderTypeMarket = "MARKET"
)

// GetDecimalPlaces returns the number of decimal places in a float.
func GetDecimalPlaces(f float64) int {
	fStr := strconv.FormatFloat(f, 'f', -1, 64)
	if strings.Contains(fStr, ".") {
		return len(strings.Split(fStr, ".")[1])
	}
	return 0
}

// TruncateToDecimalPlaces truncates a float to the specified number of decimal places.
// Uses epsilon to handle floating-point precision issues (e.g., 148.64 * 100 = 14863.999...).
func TruncateToDecimalPlaces(f float64, decimalPlaces int) float64 {
	factor := math.Pow(10, float64(decimalPlaces))
	return math.Floor(f*factor + filterEpsilon) / factor
}

// VerifiedOrderParams contains validated price and quantity strings for Binance API.
type VerifiedOrderParams struct {
	PriceStr    string
	QuantityStr string
}

// VerifyOrderParamsForBinance validates and formats order parameters for Binance API.
// This follows the same logic as the original cta_trading_service project.
// For market orders (orderType="MARKET"), price validation is skipped.
func VerifyOrderParamsForBinance(filters []*ExchangeSymbolFilter, price, quantity float64, symbol string, orderType string) (*VerifiedOrderParams, error) {
	// Separate filters by type
	var priceFilter, quantityFilter, notionalFilter *ExchangeSymbolFilter
	for _, f := range filters {
		if f == nil {
			continue
		}
		switch f.FilterType {
		case "PRICE_FILTER":
			priceFilter = f
		case "LOT_SIZE":
			quantityFilter = f
		case "MIN_NOTIONAL":
			notionalFilter = f
		}
	}

	// Validate price filter
	if priceFilter == nil {
		return nil, fmt.Errorf("PRICE_FILTER not found for symbol %s", symbol)
	}

	// Determine if this is a market order
	isMarketOrder := orderType == OrderTypeMarket

	// For market orders, skip price validation (price determined by exchange)
	var legalPrice float64
	var legalPriceStr string
	if !isMarketOrder {
		// Limit order: validate and format price
		if price <= 0 {
			return nil, fmt.Errorf("limit order requires positive price for symbol %s", symbol)
		}

		// Check price range
		if price < priceFilter.MinPrice || price > priceFilter.MaxPrice {
			return nil, fmt.Errorf("price %.8f not in valid range [%.8f, %.8f] for %s",
				price, priceFilter.MinPrice, priceFilter.MaxPrice, symbol)
		}

		// Truncate price to tick size precision
		pricePlaces := GetDecimalPlaces(priceFilter.TickSize)
		legalPrice = TruncateToDecimalPlaces(price, pricePlaces)
		legalPriceStr = fmt.Sprintf("%.*f", pricePlaces, legalPrice)

		// Re-check price range after truncation
		if legalPrice < priceFilter.MinPrice || legalPrice > priceFilter.MaxPrice {
			return nil, fmt.Errorf("truncated price %.8f not in valid range [%.8f, %.8f] for %s",
				legalPrice, priceFilter.MinPrice, priceFilter.MaxPrice, symbol)
		}
	} else {
		// Market order: price will be determined by exchange
		legalPrice = 0
		legalPriceStr = "0"
	}

	// Validate quantity filter
	if quantityFilter == nil {
		return nil, fmt.Errorf("LOT_SIZE not found for symbol %s", symbol)
	}

	// Check quantity range with tolerance for floating-point precision
	// Epsilon handles cases like 0.00999999999999 being validated as 0.01
	if quantity < quantityFilter.MinQty-filterEpsilon || quantity > quantityFilter.MaxQty+filterEpsilon {
		return nil, fmt.Errorf("quantity %.8f not in valid range [%.8f, %.8f] for %s",
			quantity, quantityFilter.MinQty, quantityFilter.MaxQty, symbol)
	}

	// Truncate quantity to step size precision
	// Note: Use MinQty to determine decimal places (as in original project)
	quantityPlaces := GetDecimalPlaces(quantityFilter.MinQty)
	legalQuantity := TruncateToDecimalPlaces(quantity, quantityPlaces)
	legalQuantityStr := fmt.Sprintf("%.*f", quantityPlaces, legalQuantity)

	// Re-check quantity range after truncation with tolerance
	if legalQuantity < quantityFilter.MinQty-filterEpsilon || legalQuantity > quantityFilter.MaxQty+filterEpsilon {
		return nil, fmt.Errorf("truncated quantity %.8f not in valid range [%.8f, %.8f] for %s",
			legalQuantity, quantityFilter.MinQty, quantityFilter.MaxQty, symbol)
	}

	// Validate min notional (skip for market orders)
	if notionalFilter != nil && notionalFilter.MinNotional > 0 && legalPrice > 0 {
		notional := legalPrice * legalQuantity
		if notional < notionalFilter.MinNotional {
			return nil, fmt.Errorf("notional %.8f below min_notional %.8f for %s",
				notional, notionalFilter.MinNotional, symbol)
		}
	}

	return &VerifiedOrderParams{
		PriceStr:    legalPriceStr,
		QuantityStr: legalQuantityStr,
	}, nil
}
