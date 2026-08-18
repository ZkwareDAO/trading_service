package signal

import (
	"trading-service/internal/exchange"
	"trading-service/internal/exchange/binance"
	"trading-service/internal/exchange/deribit"
	"trading-service/internal/exchange/hyperliquid"
)

// NewBinanceFuturesAdapter creates a Binance Futures exchange adapter.
func NewBinanceFuturesAdapter(apiKey, apiSecret string, testnet bool) exchange.Exchange {
	return binance.NewBinanceFutures(apiKey, apiSecret, testnet)
}

// NewBinanceFuturesAdapterWithFilters creates a Binance Futures exchange adapter with filter source.
func NewBinanceFuturesAdapterWithFilters(apiKey, apiSecret string, testnet bool, filterSource exchange.FilterSource) exchange.Exchange {
	ex := binance.NewBinanceFutures(apiKey, apiSecret, testnet)
	ex.SetFilterSource(filterSource)
	return ex
}

// NewHyperliquidAdapter creates a Hyperliquid exchange adapter.
func NewHyperliquidAdapter(privateKeyHex, accountAddr string, testnet bool) (exchange.Exchange, error) {
	return hyperliquid.NewHyperliquid(privateKeyHex, accountAddr, testnet)
}

// NewDeribitAdapter creates a Deribit exchange adapter for options trading.
func NewDeribitAdapter(apiKey, apiSecret, apiPassword string, testnet bool) (exchange.Exchange, error) {
	return deribit.NewDeribit(apiKey, apiSecret, apiPassword, testnet)
}
