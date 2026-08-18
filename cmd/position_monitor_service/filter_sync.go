package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"trading-service/internal/exchange/binance"
	"trading-service/internal/exchange/hyperliquid"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

// StartFilterSync periodically syncs symbol filters from exchanges used by users.
// Scans users.csv to determine which exchanges to sync.
func StartFilterSync(ctx context.Context, repo *persistence.StateRepository, uosRPCURL string, interval time.Duration, testnetBinance, testnetHyperliquid bool) {
	if interval <= 0 {
		log.Printf("Filter sync disabled (interval=0)")
		return
	}

	// Run once immediately
	go syncFiltersOnce(ctx, repo, uosRPCURL, testnetBinance, testnetHyperliquid)

	// Then periodic
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Filter sync stopped")
			return
		case <-ticker.C:
			syncFiltersOnce(ctx, repo, uosRPCURL, testnetBinance, testnetHyperliquid)
		}
	}
}

// syncFiltersOnce performs one-time filter sync for all exchanges found in users.csv.
func syncFiltersOnce(ctx context.Context, repo *persistence.StateRepository, uosRPCURL string, testnetBinance, testnetHyperliquid bool) {
	// 1. Scan users.csv to get unique exchanges
	users := repo.ListUsers()
	exchanges := make(map[string]bool)
	for _, user := range users {
		if user.Exchange != "" && user.Exchange != "mock" {
			exchanges[user.Exchange] = true
		}
	}

	if len(exchanges) == 0 {
		log.Println("Filter sync: no real exchanges found in users.csv")
		return
	}

	log.Printf("Filter sync: found %d exchanges to sync: %v", len(exchanges), exchanges)

	var mu sync.Mutex
	var allFilters []*order.ExchangeSymbolFilter
	var wg sync.WaitGroup

	// 2. Sync each exchange concurrently
	for exchangeName := range exchanges {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			start := time.Now()

			var filters []*order.ExchangeSymbolFilter
			var err error

			switch name {
			case "binance":
				// Create no-auth instance (public API)
				binanceEx := binance.NewBinanceFutures("", "", testnetBinance)
				filters, err = binanceEx.SyncSymbolFilters()
			case "hyperliquid":
				// Create no-auth instance (public API)
				hlEx := hyperliquid.NewHyperliquidNoAuth(testnetHyperliquid)
				filters, err = hlEx.SyncSymbolFilters()
			default:
				log.Printf("Filter sync: unsupported exchange %s", name)
				return
			}

			if err != nil {
				log.Printf("Filter sync failed for %s: %v", name, err)
				return
			}

			mu.Lock()
			allFilters = append(allFilters, filters...)
			mu.Unlock()

			log.Printf("Filter sync: %s fetched %d filters in %v", name, len(filters), time.Since(start))
		}(exchangeName)
	}

	wg.Wait()

	if len(allFilters) == 0 {
		log.Println("Filter sync: no filters fetched")
		return
	}

	// 3. Persist to CSV
	if err := repo.ReplaceExchangeSymbolFilters(allFilters); err != nil {
		log.Printf("Filter sync: failed to persist: %v", err)
		return
	}

	log.Printf("Filter sync: persisted %d filters total", len(allFilters))

	// 4. Notify UOS to reload
	if err := notifyUOSReloadFilters(uosRPCURL); err != nil {
		log.Printf("Filter sync: failed to notify UOS: %v", err)
	} else {
		log.Printf("Filter sync: notified UOS to reload filters")
	}
}

// notifyUOSReloadFilters calls UOS RPC to reload filters from CSV.
func notifyUOSReloadFilters(uosRPCURL string) error {
	if uosRPCURL == "" {
		return nil // UOS not configured
	}

	// HTTP POST to UOS RPC endpoint
	url := uosRPCURL + "/rpc/v1/filters/reload"
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("UOS returned status %d", resp.StatusCode)
	}

	return nil
}
