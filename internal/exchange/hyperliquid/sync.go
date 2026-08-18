package hyperliquid

import (
	"context"
	"log"
	"math"

	"trading-service/internal/order"
)

// SyncSymbolFilters fetches asset metadata from Hyperliquid and returns symbol filters.
func (h *Hyperliquid) SyncSymbolFilters() ([]*order.ExchangeSymbolFilter, error) {
	if err := h.initLazyReadOnly(); err != nil {
		return nil, err
	}

	meta, err := h.info.Meta(context.Background())
	if err != nil {
		return nil, err
	}

	var filters []*order.ExchangeSymbolFilter
	for _, asset := range meta.Universe {
		sizeIncrement := math.Pow10(-asset.SzDecimals)

		f := &order.ExchangeSymbolFilter{
			Exchange:  "hyperliquid",
			PosType:   order.PosTypeFutures,
			Symbol:    asset.Name + "USDC",
			FilterType: "LOT_SIZE",
		}

		// Quantity constraints (step size)
		f.StepSize = sizeIncrement
		f.MinQty = sizeIncrement
		f.MaxQty = float64(asset.MaxLeverage) * 1000

		// Price tick size: Hyperliquid uses 5 significant figures
		// For perp: decimals = 6 - szDecimals
		// This is derived from the go-hyperliquid library's SlippagePrice implementation
		// (see: https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/tick-and-lot-size)
		decimals := 6 - asset.SzDecimals
		if decimals < 0 {
			decimals = 0
		}
		f.TickSize = math.Pow10(-decimals)

		filters = append(filters, f)
	}

	log.Printf("Hyperliquid: synced %d symbol filters with tick_size", len(filters))
	return filters, nil
}
