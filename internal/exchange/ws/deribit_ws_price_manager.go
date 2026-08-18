package ws

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// =============================================================================
// Deribit WebSocket Price Manager
// =============================================================================

// DeribitWsPriceManager manages real-time option price subscriptions via WebSocket.
//
// Deribit WebSocket API:
// - Endpoint: wss://www.deribit.com/ws/api/v2 (mainnet)
// - Endpoint: wss://test.deribit.com/ws/api/v2 (testnet)
// - Protocol: JSON-RPC 2.0 over WebSocket
// - Channel format: ticker.{instrument_name}.{interval}
//
// Example subscription:
//
//	{"jsonrpc": "2.0", "method": "public/subscribe",
//	 "params": {"channels": ["ticker.BTC-17JUL26-64000-P.100ms"]}}
type DeribitWsPriceManager struct {
	Manager     *PriceManager
	conn        *websocket.Conn
	baseURL     string
	mu          sync.RWMutex
	closeOnce   sync.Once
	reqID       int64 // int64 for atomic operations
	subs        map[string]bool
	mockEnabled bool // for testing: simulate connected state
}

// DeribitWsPriceManagerOption is a functional option.
type DeribitWsPriceManagerOption func(*DeribitWsPriceManager)

// WithDeribitWsURLOption sets the WebSocket URL.
func WithDeribitWsURLOption(url string) DeribitWsPriceManagerOption {
	return func(pm *DeribitWsPriceManager) { pm.baseURL = url }
}

// WithDeribitMockOption enables mock mode for testing (simulates connected state).
func WithDeribitMockOption() DeribitWsPriceManagerOption {
	return func(pm *DeribitWsPriceManager) { pm.mockEnabled = true }
}

// NewDeribitWsPriceManager creates a Deribit WebSocket price manager.
//
// Default URL: wss://www.deribit.com/ws/api/v2 (mainnet)
// Use WithDeribitWsURLOption for testnet.
func NewDeribitWsPriceManager(opts ...DeribitWsPriceManagerOption) *DeribitWsPriceManager {
	pm := &DeribitWsPriceManager{
		Manager: NewPriceManager(),
		baseURL: "wss://www.deribit.com/ws/api/v2",
		subs:    make(map[string]bool),
	}
	for _, opt := range opts {
		opt(pm)
	}
	return pm
}

// Connect establishes WebSocket connection to Deribit.
// If already connected, does nothing.
// If previously disconnected, reconnects and clears old subscriptions.
func (m *DeribitWsPriceManager) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn != nil {
		return nil // already connected
	}

	u, err := url.Parse(m.baseURL)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		// Log warning but don't fail - REST API can be used as fallback
		log.Printf("Deribit WS connection failed (will use REST API fallback): %v", err)
		return nil // Don't return error, allow REST fallback
	}

	m.conn = conn
	// Clear old subscriptions on reconnection - need to re-subscribe
	m.subs = make(map[string]bool)

	// Start message handler
	go m.handleMessages()

	return nil
}

// handleMessages processes incoming WebSocket messages.
func (m *DeribitWsPriceManager) handleMessages() {
	for {
		m.mu.RLock()
		conn := m.conn
		m.mu.RUnlock()

		if conn == nil {
			return
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Deribit WS read error: %v", err)
			m.clearConnection()
			return
		}

		m.processMessage(msg)
	}
}

// clearConnection clears the dead connection.
func (m *DeribitWsPriceManager) clearConnection() {
	m.mu.Lock()
	m.conn = nil
	m.mu.Unlock()
}

// processMessage handles a single WebSocket message.
func (m *DeribitWsPriceManager) processMessage(msg []byte) {
	var resp map[string]interface{}
	if err := json.Unmarshal(msg, &resp); err != nil {
		return
	}

	params, ok := resp["params"].(map[string]interface{})
	if !ok {
		return
	}

	channel, ok := params["channel"].(string)
	if !ok || !strings.HasPrefix(channel, "ticker.") {
		return
	}

	data, ok := params["data"].(map[string]interface{})
	if !ok {
		return
	}

	m.handleTickerUpdate(data)
}

// nextReqID generates unique request IDs (thread-safe using atomic).
func (m *DeribitWsPriceManager) nextReqID() int {
	return int(atomic.AddInt64(&m.reqID, 1))
}

// buildSubscribeRequest creates a JSON-RPC subscription request.
func (m *DeribitWsPriceManager) buildSubscribeRequest(method, instrument string) map[string]interface{} {
	channel := fmt.Sprintf("ticker.%s.100ms", instrument)
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      m.nextReqID(),
		"params": map[string]interface{}{
			"channels": []string{channel},
		},
	}
}

// handleTickerUpdate processes ticker price updates.
func (m *DeribitWsPriceManager) handleTickerUpdate(data map[string]interface{}) {
	instrument, ok := data["instrument_name"].(string)
	if !ok {
		return
	}

	markPrice, ok := data["mark_price"].(float64)
	if !ok {
		return
	}

	// Update price in manager
	m.Manager.UpdatePrice(instrument, markPrice)
}

// Subscribe subscribes to real-time price updates for an option.
//
// Channel format: ticker.{instrument_name}.100ms
func (m *DeribitWsPriceManager) Subscribe(instrument string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// In mock mode, just record the subscription without actual WebSocket
	if m.mockEnabled {
		if m.subs[instrument] {
			return nil // already subscribed
		}
		m.subs[instrument] = true
		return nil
	}

	if m.conn == nil {
		return fmt.Errorf("not connected")
	}

	if m.subs[instrument] {
		return nil // already subscribed
	}

	req := m.buildSubscribeRequest("public/subscribe", instrument)
	if err := m.conn.WriteJSON(req); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	m.subs[instrument] = true
	return nil
}

// Unsubscribe removes a price subscription.
func (m *DeribitWsPriceManager) Unsubscribe(instrument string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn == nil {
		return fmt.Errorf("not connected")
	}

	if !m.subs[instrument] {
		return nil // not subscribed
	}

	req := m.buildSubscribeRequest("public/unsubscribe", instrument)
	if err := m.conn.WriteJSON(req); err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}

	delete(m.subs, instrument)
	return nil
}

// GetPrice returns the latest price for an instrument.
func (m *DeribitWsPriceManager) GetPrice(instrument string) (float64, bool) {
	return m.Manager.GetPrice(instrument)
}

// GetSubscriptions returns all active subscriptions.
// Returns error if WebSocket is not connected (unless in mock mode).
func (m *DeribitWsPriceManager) GetSubscriptions() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// In mock mode, return subscriptions without checking connection
	if m.mockEnabled {
		return m.subscriptionList(), nil
	}

	// Connection is managed by handleMessages() - if dead, it clears m.conn
	if m.conn == nil {
		return nil, fmt.Errorf("WebSocket not connected")
	}

	return m.subscriptionList(), nil
}

// subscriptionList returns the current subscription list.
func (m *DeribitWsPriceManager) subscriptionList() []string {
	subs := make([]string, 0, len(m.subs))
	for instrument := range m.subs {
		subs = append(subs, instrument)
	}
	return subs
}

// Close closes the WebSocket connection.
func (m *DeribitWsPriceManager) Close() error {
	var err error
	m.closeOnce.Do(func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		if m.conn != nil {
			err = m.conn.Close()
			m.conn = nil
		}
	})
	return err
}
