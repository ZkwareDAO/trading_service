package ws

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/futures"
)

// PriceManager holds real-time prices from WebSocket.
type PriceManager struct {
	spotPrices    map[string]float64
	futuresPrices map[string]float64
	mu            sync.RWMutex
}

// NewPriceManager creates a new price manager.
func NewPriceManager() *PriceManager {
	return &PriceManager{
		spotPrices:    make(map[string]float64),
		futuresPrices: make(map[string]float64),
	}
}

// UpdateSpotPrice updates a spot price.
func (pm *PriceManager) UpdateSpotPrice(symbol string, price float64) {
	pm.mu.Lock()
	pm.spotPrices[symbol] = price
	pm.mu.Unlock()
}

// UpdateFuturesPrice updates a futures price.
func (pm *PriceManager) UpdateFuturesPrice(symbol string, price float64) {
	pm.mu.Lock()
	pm.futuresPrices[symbol] = price
	pm.mu.Unlock()
}

// UpdatePrice updates a price (uses futures map by default).
func (pm *PriceManager) UpdatePrice(symbol string, price float64) {
	pm.mu.Lock()
	pm.futuresPrices[symbol] = price
	pm.mu.Unlock()
}

// GetPrice returns the latest price for a symbol.
func (pm *PriceManager) GetPrice(symbol string) (float64, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	price, ok := pm.futuresPrices[symbol]
	if !ok {
		price, ok = pm.spotPrices[symbol]
	}
	return price, ok
}

// Snapshot returns a copy of all prices.
func (pm *PriceManager) Snapshot() map[string]float64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	result := make(map[string]float64, len(pm.spotPrices)+len(pm.futuresPrices))
	for k, v := range pm.spotPrices {
		result[k] = v
	}
	for k, v := range pm.futuresPrices {
		result[k] = v
	}
	return result
}

// All returns all prices (same as Snapshot).
func (pm *PriceManager) All() map[string]float64 {
	return pm.Snapshot()
}

// BinanceWsPriceManager manages WebSocket price subscriptions.
type BinanceWsPriceManager struct {
	Manager *PriceManager
	stopCh  chan struct{}
	stopMu  sync.Once
}

// NewBinanceWsPriceManager creates a new WS price manager.
func NewBinanceWsPriceManager() *BinanceWsPriceManager {
	return &BinanceWsPriceManager{
		Manager: NewPriceManager(),
		stopCh:  make(chan struct{}),
	}
}

// StartFuturesWS starts futures price WebSocket (non-blocking, auto-reconnect).
func (wpm *BinanceWsPriceManager) StartFuturesWS() {
	go func() {
		for {
			select {
			case <-wpm.stopCh:
				return
			default:
			}

			handler := func(event futures.WsAllMiniMarketTickerEvent) {
				for _, ticker := range event {
					price, err := strconv.ParseFloat(ticker.ClosePrice, 64)
					if err == nil {
						wpm.Manager.UpdateFuturesPrice(ticker.Symbol, price)
					}
				}
			}

			doneC, _, err := futures.WsAllMiniMarketTickerServe(handler, func(error) {})
			if err != nil {
				continue
			}

			<-doneC
		}
	}()
}

// StartSpotWS starts spot price WebSocket (non-blocking, auto-reconnect).
func (wpm *BinanceWsPriceManager) StartSpotWS() {
	go func() {
		for {
			select {
			case <-wpm.stopCh:
				return
			default:
			}

			handler := func(event binance.WsAllMiniMarketsStatEvent) {
				for _, ticker := range event {
					price, err := strconv.ParseFloat(ticker.LastPrice, 64)
					if err == nil {
						wpm.Manager.UpdateSpotPrice(ticker.Symbol, price)
					}
				}
			}

			doneC, _, err := binance.WsAllMiniMarketsStatServe(handler, func(error) {})
			if err != nil {
				continue
			}

			<-doneC
		}
	}()
}

// Stop stops all WebSocket connections.
func (wpm *BinanceWsPriceManager) Stop() {
	wpm.stopMu.Do(func() {
		close(wpm.stopCh)
	})
}

var _ = fmt.Sprint // suppress unused import
