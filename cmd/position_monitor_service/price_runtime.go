package main

import (
	"context"
	"log"
	"strings"
	"sync"
	// "time" — only used by the commented-out StartPriceSnapshotLoop below;
	// restore this import if that function is re-enabled.

	exchangews "trading-service/internal/exchange/ws"
	"trading-service/internal/order"
	"trading-service/internal/risk"
)

// PriceRuntime exposes exchange price snapshots to the position monitor.
type PriceRuntime interface {
	Start(ctx context.Context) error
	Snapshot() map[string]float64
	ExchangeName() string
	Stop()
}

type BinancePriceRuntime struct {
	manager *exchangews.BinanceWsPriceManager
}

func NewBinancePriceRuntime(manager *exchangews.BinanceWsPriceManager) *BinancePriceRuntime {
	if manager == nil {
		manager = exchangews.NewBinanceWsPriceManager()
	}
	return &BinancePriceRuntime{manager: manager}
}

func (r *BinancePriceRuntime) ExchangeName() string {
	return "binance"
}

func (r *BinancePriceRuntime) Start(ctx context.Context) error {
	r.manager.StartFuturesWS()
	go func() {
		<-ctx.Done()
		r.Stop()
	}()
	return nil
}

func (r *BinancePriceRuntime) Snapshot() map[string]float64 {
	return r.manager.Manager.Snapshot()
}

func (r *BinancePriceRuntime) Stop() {
	r.manager.Stop()
}

type HyperliquidPriceRuntime struct {
	wsMgr    *exchangews.HyperliquidWsPriceManager
	stopOnce sync.Once
}

func NewHyperliquidPriceRuntimeFromWS(wsMgr *exchangews.HyperliquidWsPriceManager) *HyperliquidPriceRuntime {
	return &HyperliquidPriceRuntime{wsMgr: wsMgr}
}

func (r *HyperliquidPriceRuntime) ExchangeName() string {
	return "hyperliquid"
}

func (r *HyperliquidPriceRuntime) Start(ctx context.Context) error {
	if r.wsMgr == nil {
		return nil // no WS manager — no-op
	}
	if err := r.wsMgr.Connect(); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		r.Stop()
	}()
	return nil
}

func (r *HyperliquidPriceRuntime) Snapshot() map[string]float64 {
	if r.wsMgr == nil {
		return make(map[string]float64)
	}
	return r.wsMgr.Manager.Snapshot()
}

func (r *HyperliquidPriceRuntime) Stop() {
	r.stopOnce.Do(func() {
		if r.wsMgr != nil {
			_ = r.wsMgr.Close()
		}
	})
}

func normalizeHyperliquidPriceSymbol(symbol string) string {
	upper := strings.ToUpper(strings.TrimSpace(symbol))
	if strings.HasSuffix(upper, "USDC") {
		return upper
	}
	return upper + "USDC"
}

// DeribitPriceRuntime provides real-time option prices from Deribit WebSocket.
// Unlike Binance/Hyperliquid which push all symbols automatically,
// Deribit requires explicit subscription for each option.
type DeribitPriceRuntime struct {
	wsMgr        *exchangews.DeribitWsPriceManager
	subscribeMgr DeribitSubscribeManager
	stopOnce     sync.Once
}

// DeribitSubscribeManager provides list of options to subscribe.
type DeribitSubscribeManager interface {
	GetDeribitOptions() []string
}

// NewDeribitPriceRuntime creates a Deribit price runtime.
func NewDeribitPriceRuntime(wsMgr *exchangews.DeribitWsPriceManager, subscribeMgr DeribitSubscribeManager) *DeribitPriceRuntime {
	if wsMgr == nil {
		wsMgr = exchangews.NewDeribitWsPriceManager()
	}
	return &DeribitPriceRuntime{
		wsMgr:        wsMgr,
		subscribeMgr: subscribeMgr,
	}
}

func (r *DeribitPriceRuntime) ExchangeName() string {
	return "deribit"
}

// EnsureSubscribed subscribes to any new options not already subscribed.
// Also handles automatic WebSocket reconnection if disconnected.
// Errors are logged but do not crash the service.
func (r *DeribitPriceRuntime) EnsureSubscribed() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("DeribitPriceRuntime.EnsureSubscribed panic recovered: %v", err)
		}
	}()

	if r.wsMgr == nil || r.subscribeMgr == nil {
		return
	}

	// Get current subscriptions (with error handling)
	currentSubs, err := r.wsMgr.GetSubscriptions()
	if err != nil {
		// WebSocket not connected - try to reconnect
		log.Printf("Deribit WebSocket not connected, attempting to reconnect: %v", err)
		if reconnectErr := r.wsMgr.Connect(); reconnectErr != nil {
			log.Printf("Deribit WebSocket reconnection failed: %v (will retry next cycle)", reconnectErr)
			return
		}
		log.Printf("Deribit WebSocket reconnected successfully")
		// After reconnection, need to re-subscribe to all options
		currentSubs = nil // reset to trigger full subscription
	}

	subscribedSet := make(map[string]bool)
	for _, sub := range currentSubs {
		subscribedSet[sub] = true
	}

	// Subscribe to new options (individual errors don't stop the loop)
	options := r.subscribeMgr.GetDeribitOptions()
	newCount := 0
	for _, option := range options {
		if subscribedSet[option] {
			continue // already subscribed
		}
		if err := r.wsMgr.Subscribe(option); err != nil {
			log.Printf("Failed to subscribe to Deribit option %s: %v (will retry next cycle)", option, err)
			continue
		}
		newCount++
	}

	if newCount > 0 {
		log.Printf("Subscribed to %d new Deribit options (total: %d)", newCount, len(currentSubs)+newCount)
	}
}

func (r *DeribitPriceRuntime) Start(ctx context.Context) error {
	if r.wsMgr == nil {
		return nil // no WS manager — no-op
	}
	if err := r.wsMgr.Connect(); err != nil {
		return err
	}

	// Subscribe to all Deribit options from the manager
	if r.subscribeMgr != nil {
		options := r.subscribeMgr.GetDeribitOptions()
		for _, option := range options {
			if err := r.wsMgr.Subscribe(option); err != nil {
				// Log but continue - some options might not be tradeable
				log.Printf("Failed to subscribe to Deribit option %s: %v", option, err)
			}
		}
		if len(options) > 0 {
			log.Printf("Subscribed to %d Deribit options", len(options))
		}
	}

	go func() {
		<-ctx.Done()
		r.Stop()
	}()
	return nil
}

// DeribitOptionExtractor extracts Deribit option symbols from user_order_positions.
// It reads from the persistence layer (user_order_positions) rather than state.Positions
// to ensure subscriptions can be recovered even when state.Positions is empty
// (e.g., after restart when WS prices are not yet available).
type DeribitOptionExtractor struct {
	repo DeribitPositionSource
}

// DeribitPositionSource provides access to active user_order_positions.
type DeribitPositionSource interface {
	ListActivePositions() []*order.UserOrderPosition
}

// NewDeribitOptionExtractor creates an extractor that reads from persistence.
func NewDeribitOptionExtractor(repo DeribitPositionSource) *DeribitOptionExtractor {
	return &DeribitOptionExtractor{repo: repo}
}

// GetDeribitOptions returns unique Deribit option symbols from active positions.
func (e *DeribitOptionExtractor) GetDeribitOptions() []string {
	if e.repo == nil {
		return nil
	}

	seen := make(map[string]bool)
	var options []string

	for _, pos := range e.repo.ListActivePositions() {
		if pos.Exchange == "deribit" && pos.PosType == order.PosTypeOptions && pos.Deleted == 0 {
			if !seen[pos.Asset] {
				seen[pos.Asset] = true
				options = append(options, pos.Asset)
			}
		}
	}

	return options
}

func (r *DeribitPriceRuntime) Snapshot() map[string]float64 {
	if r.wsMgr == nil {
		return make(map[string]float64)
	}
	return r.wsMgr.Manager.Snapshot()
}

func (r *DeribitPriceRuntime) Stop() {
	r.stopOnce.Do(func() {
		if r.wsMgr != nil {
			_ = r.wsMgr.Close()
		}
	})
}

// StartPriceSnapshotLoop is currently unused: production drives price syncing
// through syncCycle (main.go), which calls SyncPriceSnapshots inline and then
// reads state.Snapshot.Prices in the same goroutine — no concurrency involved.
//
// Commented out rather than deleted because it is the intended shape if price
// syncing ever moves to its own goroutine. Before re-enabling it, note that
// SyncPriceSnapshots writes state.Snapshot.Prices with no synchronization,
// while syncCycle's steps 2/3/5 read that same map. Running both concurrently
// would be a genuine data race (Go panics on concurrent map read/write), so
// locking around MarketSnapshot is a prerequisite.
//
// func StartPriceSnapshotLoop(ctx context.Context, state *risk.GlobalState, runtimes []PriceRuntime, interval time.Duration) (<-chan struct{}, error) {
// 	if interval <= 0 {
// 		interval = 10 * time.Second
// 	}
// 	for _, runtime := range runtimes {
// 		if runtime == nil {
// 			continue
// 		}
// 		if err := runtime.Start(ctx); err != nil {
// 			return nil, err
// 		}
// 	}
// 	SyncPriceSnapshots(state, runtimes)
//
// 	done := make(chan struct{})
// 	go func() {
// 		defer close(done)
// 		defer func() {
// 			for _, runtime := range runtimes {
// 				if runtime != nil {
// 					runtime.Stop()
// 				}
// 			}
// 		}()
//
// 		ticker := time.NewTicker(interval)
// 		defer ticker.Stop()
// 		for {
// 			select {
// 			case <-ctx.Done():
// 				return
// 			case <-ticker.C:
// 				SyncPriceSnapshots(state, runtimes)
// 			}
// 		}
// 	}()
// 	return done, nil
// }

func SyncPriceSnapshots(state *risk.GlobalState, runtimes []PriceRuntime) {
	if state.Snapshot == nil {
		state.Snapshot = &risk.MarketSnapshot{
			Prices:  make(map[string]map[string]float64),
			Funding: make(map[string]float64),
		}
	}
	if state.Snapshot.Funding == nil {
		state.Snapshot.Funding = make(map[string]float64)
	}
	for _, runtime := range runtimes {
		if runtime == nil {
			continue
		}
		exchange := runtime.ExchangeName()
		if state.Snapshot.Prices[exchange] == nil {
			state.Snapshot.Prices[exchange] = make(map[string]float64)
		}
		for symbol, price := range runtime.Snapshot() {
			state.Snapshot.Prices[exchange][symbol] = price
		}
	}
}
