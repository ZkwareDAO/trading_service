package main

import (
	"fmt"
	"sync"

	"trading-service/internal/exchange"
	"trading-service/internal/exchange/binance"
	"trading-service/internal/exchange/deribit"
	"trading-service/internal/exchange/hyperliquid"
	"trading-service/internal/persistence"
)

type UserExchangeResolver struct {
	repo               *persistence.StateRepository
	binanceTestnet     bool
	hyperliquidTestnet bool
	deribitTestnet     bool
	mu                 sync.Mutex
	cache              map[string]exchange.Exchange
}

func NewUserExchangeResolver(repo *persistence.StateRepository, binanceTestnet, hyperliquidTestnet, deribitTestnet bool) *UserExchangeResolver {
	return &UserExchangeResolver{repo: repo, binanceTestnet: binanceTestnet, hyperliquidTestnet: hyperliquidTestnet, deribitTestnet: deribitTestnet, cache: make(map[string]exchange.Exchange)}
}

func (r *UserExchangeResolver) ResolveExchange(userID uint64, exchangeName string) (exchange.Exchange, error) {
	key := fmt.Sprintf("%d:%s", userID, exchangeName)

	// For Deribit, don't use cache - create fresh instance each time
	// Deribit tokens expire after 900 seconds, and cached instances may have
	// stale tokens that fail to refresh due to network/testnet instability
	if exchangeName == "deribit" {
		return r.createExchange(userID, exchangeName)
	}

	// For other exchanges, use cache
	r.mu.Lock()
	if ex := r.cache[key]; ex != nil {
		r.mu.Unlock()
		return ex, nil
	}
	r.mu.Unlock()

	ex, err := r.createExchange(userID, exchangeName)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.cache[key] = ex
	r.mu.Unlock()
	return ex, nil
}

// createExchange creates a new exchange instance with user credentials.
func (r *UserExchangeResolver) createExchange(userID uint64, exchangeName string) (exchange.Exchange, error) {
	user, err := r.repo.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user.Exchange != exchangeName {
		return nil, fmt.Errorf("user %d exchange mismatch: has %s want %s", userID, user.Exchange, exchangeName)
	}

	switch exchangeName {
	case "binance":
		if user.APIKey == "" || user.APISecret == "" {
			return nil, fmt.Errorf("user %d missing binance credentials", userID)
		}
		bf := binance.NewBinanceFutures(user.APIKey, user.APISecret, r.binanceTestnet)
		// Enable precision validation for Binance orders
		bf.SetFilterSource(r.repo)
		return bf, nil
	case "hyperliquid":
		if user.APISecret == "" {
			return nil, fmt.Errorf("user %d missing hyperliquid private key", userID)
		}
		return hyperliquid.NewHyperliquid(user.APISecret, user.APIKey, r.hyperliquidTestnet)
	case "deribit":
		if user.APIKey == "" || user.APISecret == "" {
			return nil, fmt.Errorf("user %d missing deribit credentials", userID)
		}
		return deribit.NewDeribit(user.APIKey, user.APISecret, user.APIPassword, r.deribitTestnet)
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", exchangeName)
	}
}
