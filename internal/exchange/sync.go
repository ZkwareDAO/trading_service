package exchange

import (
	"context"
	"log"
	"sync"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

// SyncableExchange is an exchange that can sync symbol filters.
type SyncableExchange interface {
	Exchange
	SyncSymbolFilters() ([]*order.ExchangeSymbolFilter, error)
}

// StartFilterSync periodically syncs symbol filters from all registered real exchanges.
// Skips "mock" exchange. Runs once immediately, then on the given interval.
func StartFilterSync(ctx context.Context, factory *ExchangeFactory, repo *persistence.StateRepository, interval time.Duration) {
	if interval <= 0 {
		log.Printf("Filter sync disabled (interval=0)")
		return
	}

	// Run once immediately
	go syncFiltersOnce(ctx, factory, repo)

	// Then periodic
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Filter sync stopped")
			return
		case <-ticker.C:
			syncFiltersOnce(ctx, factory, repo)
		}
	}
}

func syncFiltersOnce(ctx context.Context, factory *ExchangeFactory, repo *persistence.StateRepository) {
	var mu sync.Mutex
	var allFilters []*order.ExchangeSymbolFilter
	var wg sync.WaitGroup

	// Get all registered exchanges
	for _, name := range []string{"binance", "hyperliquid"} {
		ex, err := factory.Create(name)
		if err != nil {
			continue
		}

		syncable, ok := ex.(SyncableExchange)
		if !ok {
			continue
		}

		wg.Add(1)
		go func(n string, s SyncableExchange) {
			defer wg.Done()
			start := time.Now()

			filters, err := s.SyncSymbolFilters()
			if err != nil {
				log.Printf("Filter sync failed for %s: %v", n, err)
				return
			}

			mu.Lock()
			allFilters = append(allFilters, filters...)
			mu.Unlock()

			log.Printf("Filter sync: %s fetched %d filters in %v", n, len(filters), time.Since(start))
		}(name, syncable)
	}

	wg.Wait()

	if len(allFilters) == 0 {
		log.Println("Filter sync: no filters fetched")
		return
	}

	// Persist to CSV via GlobalState
	if err := persistFilters(repo, allFilters); err != nil {
		log.Printf("Filter sync: failed to persist: %v", err)
		return
	}

	log.Printf("Filter sync: persisted %d filters total", len(allFilters))
}

func persistFilters(repo *persistence.StateRepository, filters []*order.ExchangeSymbolFilter) error {
	return repo.ReplaceExchangeSymbolFilters(filters)
}
