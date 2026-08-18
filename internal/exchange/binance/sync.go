package binance

import (
	"context"
	"log"
	"strconv"

	"trading-service/internal/order"
)

// SyncSymbolFilters fetches exchange info from Binance Futures and returns symbol filters.
func (b *BinanceFutures) SyncSymbolFilters() ([]*order.ExchangeSymbolFilter, error) {
	if b.client == nil {
		return nil, nil
	}

	info, err := b.client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		return nil, err
	}

	var filters []*order.ExchangeSymbolFilter
	for _, sym := range info.Symbols {
		if sym.ContractType != "PERPETUAL" {
			continue
		}

		// PRICE_FILTER - create separate record
		if pf := sym.PriceFilter(); pf != nil {
			filters = append(filters, &order.ExchangeSymbolFilter{
				Exchange:   "binance",
				PosType:    order.PosTypeFutures,
				Symbol:     sym.Symbol,
				FilterType: "PRICE_FILTER",
				TickSize:   parseBinStr(pf.TickSize),
				MinPrice:   parseBinStr(pf.MinPrice),
				MaxPrice:   parseBinStr(pf.MaxPrice),
			})
		}

		// LOT_SIZE - create separate record
		if lf := sym.LotSizeFilter(); lf != nil {
			filters = append(filters, &order.ExchangeSymbolFilter{
				Exchange:   "binance",
				PosType:    order.PosTypeFutures,
				Symbol:     sym.Symbol,
				FilterType: "LOT_SIZE",
				StepSize:   parseBinStr(lf.StepSize),
				MinQty:     parseBinStr(lf.MinQuantity),
				MaxQty:     parseBinStr(lf.MaxQuantity),
			})
		}

		// MIN_NOTIONAL - create separate record
		if nf := sym.MinNotionalFilter(); nf != nil {
			filters = append(filters, &order.ExchangeSymbolFilter{
				Exchange:    "binance",
				PosType:     order.PosTypeFutures,
				Symbol:      sym.Symbol,
				FilterType:  "MIN_NOTIONAL",
				MinNotional: parseBinStr(nf.Notional),
			})
		}
	}

	log.Printf("Binance: synced %d symbol filters", len(filters))
	return filters, nil
}

func parseBinStr(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
