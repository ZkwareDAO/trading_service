package main

import (
	"trading-service/internal/exchange/ws"

	hyperliquid "github.com/sonirico/go-hyperliquid"
)

func CollectNonMockExchanges(exchangeNames []string) map[string]bool {
	exchanges := make(map[string]bool)
	for _, name := range exchangeNames {
		if name == "" || name == "mock" {
			continue
		}
		exchanges[name] = true
	}
	return exchanges
}

func BuildPriceRuntimes(exchanges map[string]bool, hyperliquidTestnet bool, repo DeribitPositionSource) []PriceRuntime {
	runtimes := make([]PriceRuntime, 0, len(exchanges))
	if exchanges["binance"] {
		runtimes = append(runtimes, NewBinancePriceRuntime(ws.NewBinanceWsPriceManager()))
	}
	if exchanges["hyperliquid"] {
		var opts []ws.HyperliquidWsPriceManagerOption
		if hyperliquidTestnet {
			opts = append(opts, ws.WithBaseURLOption(hyperliquid.TestnetAPIURL))
		}
		wsMgr := ws.NewHyperliquidWsPriceManager(opts...)
		runtimes = append(runtimes, NewHyperliquidPriceRuntimeFromWS(wsMgr))
	}
	if exchanges["deribit"] {
		// Deribit requires explicit subscription for each option
		subscribeMgr := NewDeribitOptionExtractor(repo)
		runtimes = append(runtimes, NewDeribitPriceRuntime(ws.NewDeribitWsPriceManager(), subscribeMgr))
	}
	return runtimes
}
