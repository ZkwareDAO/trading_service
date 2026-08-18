package ws

import (
	"context"
	"log"
	"strconv"
	"sync"

	"github.com/sonirico/go-hyperliquid"
)

// hyperliquidWsAllMidsClient is the interface used by HyperliquidWsPriceManager.
type hyperliquidWsAllMidsClient interface {
	Connect() error
	Close() error
	SubscribeAllMids(func(hyperliquid.AllMids, error)) error
}

// HyperliquidWsPriceManagerOption is a functional option for HyperliquidWsPriceManager.
type HyperliquidWsPriceManagerOption func(*HyperliquidWsPriceManager)

// WithBaseURLOption sets the WebSocket base URL (testnet/mainnet).
func WithBaseURLOption(url string) HyperliquidWsPriceManagerOption {
	return func(pm *HyperliquidWsPriceManager) { pm.baseURL = url }
}

// WithTestClientOption sets a custom WS client for testing.
func WithTestClientOption(c hyperliquidWsAllMidsClient) HyperliquidWsPriceManagerOption {
	return func(pm *HyperliquidWsPriceManager) { pm.client = c }
}

// NewHyperliquidWsPriceManager creates a Hyperliquid WS price manager.
// Optional options: WithBaseURLOption(url), WithTestClientOption(client).
func NewHyperliquidWsPriceManager(opts ...HyperliquidWsPriceManagerOption) *HyperliquidWsPriceManager {
	pm := &HyperliquidWsPriceManager{
		Manager: NewPriceManager(),
	}
	for _, opt := range opts {
		opt(pm)
	}
	if pm.client == nil {
		ws := hyperliquid.NewWebsocketClient(pm.baseURL)
		pm.client = &realWsAllMidsClient{ws: ws}
	}
	return pm
}

// HyperliquidWsPriceManager manages Hyperliquid WebSocket price subscriptions.
//
// Unlike Binance which uses a single WS connection for all symbols,
// Hyperliquid's AllMids channel pushes the complete price book on every update.
// Each update replaces the entire price snapshot.
type HyperliquidWsPriceManager struct {
	Manager   *PriceManager
	client    hyperliquidWsAllMidsClient
	baseURL   string
	closeOnce sync.Once
	mu        sync.Mutex
}

// realWsAllMidsClient wraps go-hyperliquid.WebsocketClient for AllMids subscriptions.
type realWsAllMidsClient struct {
	ws *hyperliquid.WebsocketClient
}

func (r *realWsAllMidsClient) Connect() error {
	return r.ws.Connect(context.Background())
}

func (r *realWsAllMidsClient) Close() error {
	return r.ws.Close()
}

func (r *realWsAllMidsClient) SubscribeAllMids(fn func(hyperliquid.AllMids, error)) error {
	sub, err := r.ws.AllMids(hyperliquid.AllMidsSubscriptionParams{}, fn)
	if err != nil {
		return err
	}
	_ = sub
	return nil
}

// Connect establishes the WebSocket connection and subscribes to AllMids.
func (m *HyperliquidWsPriceManager) Connect() error {
	if err := m.client.Connect(); err != nil {
		return err
	}
	if err := m.client.SubscribeAllMids(m.handleAllMids); err != nil {
		return err
	}
	return nil
}

// handleAllMids processes incoming AllMids messages.
//
// Hyperliquid sends the full set of all mid-prices on every update,
// so we replace the entire price map atomically.
func (m *HyperliquidWsPriceManager) handleAllMids(allmids hyperliquid.AllMids, err error) {
	if err != nil {
		log.Printf("hyperliquid WS AllMids error: %v", err)
		return
	}

	prices := make(map[string]float64, len(allmids.Mids))
	for symbol, priceStr := range allmids.Mids {
		price, err := strconv.ParseFloat(priceStr, 64)
		if err != nil {
			continue
		}
		prices[symbol] = price
	}

	m.mu.Lock()
	m.Manager.mu.Lock()
	m.Manager.futuresPrices = prices
	m.Manager.mu.Unlock()
	m.mu.Unlock()
}

// Close closes the WebSocket connection.
func (m *HyperliquidWsPriceManager) Close() error {
	m.closeOnce.Do(func() {})
	return m.client.Close()
}
