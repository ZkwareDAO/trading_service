package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"trading-service/internal/deribit_position_sync"
	"trading-service/internal/exchange"
	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"

	"github.com/gorilla/websocket"
)

// DeribitOrderMonitor monitors Deribit order status changes via WebSocket.
//
// Uses JSON-RPC 2.0 protocol over WebSocket.
// Subscribes to user.orders.option.BTC.raw and user.orders.option.ETH.raw channels.
type DeribitOrderMonitor struct {
	executor    *exchange.OrderExecutor
	repo        *persistence.StateRepository
	rpcClient   *rpc.OrderServiceClient // For strategy creation via RPC
	deribit     deribitTokenProvider // Interface for getting access_token
	notifier    notification.Notifier // For position sync and Deribit-specific notifications
	userName    string               // user.Name for DeribitPositions({userName}) prefix
	testnet     bool                  // For position sync
	conn          *websocket.Conn
	baseURL       string
	stopCh        chan struct{}
	stopOnce      sync.Once
	reqID         int64 // atomic counter for JSON-RPC
	syncMu        sync.Mutex           // prevents concurrent SyncDeribitPositions calls
	mu            sync.RWMutex
	writeMu       sync.Mutex           // protects conn.WriteJSON from concurrent writes
	loopStopCh    chan struct{}        // stops handleMessages goroutine on reconnect
	heartbeatStopCh chan struct{}      // stops sendHeartbeat goroutine on reconnect
}

// deribitTokenProvider is the interface for getting Deribit credentials.
// Used to authenticate the WS connection via public/auth before subscribing.
type deribitTokenProvider interface {
	Connect() error
	GetAccessToken() string
	GetAPIKey() string
	GetAPISecret() string
}

// NewDeribitOrderMonitor creates a Deribit order monitor.
//
// Parameters:
//   - executor: OrderExecutor to handle order updates
//   - repo: StateRepository for order lookups
//   - deribit: Deribit exchange instance (for access_token), can be nil
//   - notifier: Notifier for Deribit-specific notifications
//   - testnet: Use testnet if true, mainnet otherwise
//   - userName: user.Name for DeribitPositions({userName}) notification prefix
func NewDeribitOrderMonitor(
	executor *exchange.OrderExecutor,
	repo *persistence.StateRepository,
	rpcClient *rpc.OrderServiceClient,
	deribit deribitTokenProvider,
	notifier notification.Notifier,
	testnet bool,
	userName string,
) *DeribitOrderMonitor {
	baseURL := "wss://www.deribit.com/ws/api/v2"
	if testnet {
		baseURL = "wss://test.deribit.com/ws/api/v2"
	}

	return &DeribitOrderMonitor{
		executor:  executor,
		repo:      repo,
		rpcClient: rpcClient,
		deribit:   deribit,
		notifier:  notifier,
		testnet:   testnet,
		userName:  userName,
		baseURL:   baseURL,
		stopCh:    make(chan struct{}),
	}
}

// Connect establishes WebSocket connection and subscribes to order updates.
func (m *DeribitOrderMonitor) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("deribit order monitor: Connect() called, conn=%v", m.conn != nil)
	if m.conn != nil {
		log.Printf("deribit order monitor: already connected, skipping")
		return nil // already connected
	}

	// Ensure Deribit is authenticated (get access_token)
	if m.deribit != nil {
		if err := m.deribit.Connect(); err != nil {
			return fmt.Errorf("authenticate: %w", err)
		}
		log.Printf("deribit order monitor: authenticated successfully")
	}

	// Establish WebSocket connection
	u, err := url.Parse(m.baseURL)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}

	log.Printf("deribit order monitor: connecting to %s (testnet=%v)", u.String(), strings.Contains(m.baseURL, "test."))
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	m.conn = conn
	log.Printf("deribit order monitor: WebSocket connected successfully")

	// Authenticate the WS connection via public/auth.
	// Deribit tokens are connection-scoped: a token obtained via REST is bound
	// to that REST connection and is NOT valid on a different WS connection.
	// We must authenticate the WS connection itself before subscribing.
	if err := m.authenticateWS(); err != nil {
		m.conn = nil
		return fmt.Errorf("ws authenticate: %w", err)
	}
	log.Printf("deribit order monitor: WS connection authenticated successfully")

	// Subscribe to order updates
	if err := m.SubscribeOrders(); err != nil {
		m.conn = nil
		return fmt.Errorf("subscribe: %w", err)
	}
	log.Printf("deribit order monitor: subscribed to order channels")

	// Stop old goroutines before starting new ones (handles reconnect scenario)
	if m.loopStopCh != nil {
		close(m.loopStopCh)
	}
	if m.heartbeatStopCh != nil {
		close(m.heartbeatStopCh)
	}
	m.loopStopCh = make(chan struct{})
	m.heartbeatStopCh = make(chan struct{})

	// Start message handler
	go m.handleMessages()

	// Start heartbeat to keep connection alive (prevents idle disconnection)
	go m.sendHeartbeat()

	return nil
}

// authenticateWS sends public/auth on the WS connection to authenticate it.
// This is required because Deribit tokens are connection-scoped:
// a token obtained via REST is bound to that REST connection and cannot
// be used on a different WS connection.
func (m *DeribitOrderMonitor) authenticateWS() error {
	if m.deribit == nil {
		return fmt.Errorf("no deribit provider for WS authentication")
	}

	apiKey := m.deribit.GetAPIKey()
	apiSecret := m.deribit.GetAPISecret()
	if apiKey == "" || apiSecret == "" {
		return fmt.Errorf("missing API key/secret for WS authentication")
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      m.nextReqID(),
		"method":  "public/auth",
		"params": map[string]interface{}{
			"grant_type":    "client_credentials",
			"client_id":     apiKey,
			"client_secret": apiSecret,
		},
	}

	log.Printf("deribit order monitor: sending WS public/auth with client_id (len=%d)", len(apiKey))
	m.writeMu.Lock()
	err := m.conn.WriteJSON(req)
	m.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("write auth request: %w", err)
	}

	// Read the auth response
	_, msg, err := m.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(msg, &resp); err != nil {
		return fmt.Errorf("parse auth response: %w", err)
	}

	// Check for error
	if errVal, ok := resp["error"]; ok {
		return fmt.Errorf("WS auth failed: %v", errVal)
	}

	// Verify we got an access_token in result
	result, _ := resp["result"].(map[string]interface{})
	if token, _ := result["access_token"].(string); token != "" {
		log.Printf("deribit order monitor: WS auth successful, access_token (len=%d)", len(token))
	} else {
		log.Printf("deribit order monitor: WARNING - WS auth response missing access_token, response: %s", string(msg))
	}

	return nil
}

// SubscribeOrders sends JSON-RPC subscription request for order updates.
// Must be called after authenticateWS() — the WS connection is already authenticated,
// so no access_token is needed in the subscription params.
func (m *DeribitOrderMonitor) SubscribeOrders() error {
	if m.conn == nil {
		return fmt.Errorf("not connected")
	}

	// Build subscription request — no access_token needed since WS is already authenticated
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      m.nextReqID(),
		"method":  "private/subscribe",
		"params": map[string]interface{}{
			"channels": []string{
				"user.orders.option.BTC.raw",
				"user.orders.option.ETH.raw",
			},
		},
	}

	log.Printf("deribit order monitor: sending subscription request for channels: user.orders.option.BTC.raw, user.orders.option.ETH.raw")
	m.writeMu.Lock()
	err := m.conn.WriteJSON(req)
	m.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("write subscription: %w", err)
	}

	// Read subscription confirmation response
	_, msg, err := m.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read subscription response: %w", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(msg, &resp); err != nil {
		return fmt.Errorf("parse subscription response: %w", err)
	}

	// Check for error
	if errVal, ok := resp["error"]; ok {
		return fmt.Errorf("subscription failed: %v", errVal)
	}

	// Log successful subscription
	if result, ok := resp["result"]; ok {
		log.Printf("deribit order monitor: subscription confirmed, channels: %v", result)
	}

	return nil
}

// handleMessages processes incoming WebSocket messages.
func (m *DeribitOrderMonitor) handleMessages() {
	loopCh := m.loopStopCh // capture once at start
	for {
		select {
		case <-m.stopCh:
			return
		case <-loopCh:
			return
		default:
		}

		m.mu.RLock()
		conn := m.conn
		m.mu.RUnlock()

		if conn == nil {
			return
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("deribit order monitor: read error: %v", err)
			m.reconnect()
			// After reconnect, continue the loop to process messages from new connection
			continue
		}

		m.HandleDeribitOrderUpdate(msg)
	}
}

// HandleDeribitOrderUpdate processes a Deribit order update message.
func (m *DeribitOrderMonitor) HandleDeribitOrderUpdate(msg []byte) {
	var resp map[string]interface{}
	if err := json.Unmarshal(msg, &resp); err != nil {
		log.Printf("deribit order monitor: failed to parse message: %v", err)
		return
	}

	// Check if this is a subscription confirmation (has "result")
	if result, ok := resp["result"]; ok {
		// Ignore heartbeat responses (public/test)
		if method, _ := resp["method"].(string); method != "public/test" {
			log.Printf("deribit order monitor: subscription confirmation: %v", result)
		}
		return
	}

	// Check if this is an error response
	if errVal, ok := resp["error"]; ok {
		log.Printf("deribit order monitor: error response: %v", errVal)
		return
	}

	// Check if this is a notification (has params)
	params, ok := resp["params"].(map[string]interface{})
	if !ok {
		// Log unrecognized messages (e.g. heartbeats) for debugging
		method, _ := resp["method"].(string)
		if method != "" {
			log.Printf("deribit order monitor: received method=%s message (non-order)", method)
		} else {
			log.Printf("deribit order monitor: unrecognized message: %s", string(msg))
		}
		return
	}

	// Check channel
	channel, _ := params["channel"].(string)
	if !strings.HasPrefix(channel, "user.orders.") {
		log.Printf("deribit order monitor: ignoring non-order channel: %s", channel)
		return
	}

	log.Printf("deribit order monitor: received order update on channel=%s", channel)

	// Extract order data
	data, ok := params["data"].(map[string]interface{})
	if !ok {
		log.Printf("deribit order monitor: no data in order update")
		return
	}

	// Parse order fields
	orderIDStr, _ := data["order_id"].(string)
	exchangeOrderID := extractOrderID(orderIDStr)

	orderState, _ := data["order_state"].(string)
	status := mapDeribitOrderState(orderState)

	instrumentName, _ := data["instrument_name"].(string)
	direction, _ := data["direction"].(string)

	avgPrice := 0.0
	if ap, ok := data["average_price"].(float64); ok {
		avgPrice = ap
	}

	execQty := 0.0
	if fa, ok := data["filled_amount"].(float64); ok {
		execQty = fa
	}

	// Additional fields for Deribit-specific notifications
	amount := 0.0
	if a, ok := data["amount"].(float64); ok {
		amount = a
	}

	orderPrice := 0.0
	if p, ok := data["price"].(float64); ok {
		orderPrice = p
	}

	reduceOnly := false
	if ro, ok := data["reduce_only"].(bool); ok {
		reduceOnly = ro
	}

	label, _ := data["label"].(string)

	log.Printf("deribit order monitor: parsed order update: order_id=%s exchange_order_id=%d state=%s status=%s symbol=%s direction=%s avgPrice=%.8f execQty=%.8f amount=%.8f price=%.8f reduceOnly=%v label=%s",
		orderIDStr, exchangeOrderID, orderState, status, instrumentName, direction, avgPrice, execQty, amount, orderPrice, reduceOnly, label)

	// Send Deribit-specific notification for any order state change
	m.sendDeribitNotification(deribitOrderFields{
		symbol:       instrumentName,
		direction:    direction,
		status:       status,
		amount:       amount,
		filledAmount: execQty,
		orderPrice:   orderPrice,
		avgPrice:     avgPrice,
		reduceOnly:   reduceOnly,
		label:        label,
		isRiskCtrl:   isRiskControlLabel(label),
	})

	// Find local order by exchange_order_id
	var uo *order.UprunningOrder
	var err error

	// Try pending cache first
	if uo = m.executor.FindPendingOrderByExchangeID(exchangeOrderID); uo == nil {
		// Fall back to database with retry
		for attempt := 0; attempt < 5; attempt++ {
			uo, err = m.repo.FindUprunningOrderByExchangeID(exchangeOrderID)
			if err == nil {
				break
			}
			if attempt < 4 {
				time.Sleep(300 * time.Millisecond)
			}
		}
	}

	if err != nil {
		log.Printf("deribit order monitor: order not found for exchangeOrderID=%d after retries, triggering position sync", exchangeOrderID)
		// Trigger position sync when order not found (may be from external/manual order)
		// syncMu prevents concurrent syncs from multiple WS events
		go func() {
			m.syncMu.Lock()
			defer m.syncMu.Unlock()
			log.Printf("deribit order monitor: starting position sync due to unfound order %d", exchangeOrderID)
			if syncErr := deribit_position_sync.SyncDeribitPositions(m.rpcClient, m.repo, m.testnet, m.notifier); syncErr != nil {
				log.Printf("deribit order monitor: position sync failed: %v", syncErr)
			} else {
				log.Printf("deribit order monitor: position sync completed successfully")
			}
		}()
		return
	}

	log.Printf("deribit order monitor: processing order %d status=%s symbol=%s avgPrice=%.8f execQty=%.8f",
		uo.ID, status, instrumentName, avgPrice, execQty)

	// Process based on status
	if status == "FILLED" {
		update := &exchange.OrderUpdate{
			OrderID:      uo.ID,
			Symbol:       instrumentName,
			Status:       status,
			AvgPrice:     avgPrice,
			ExecutedQty:  execQty,
			PositionSide: mapDeribitDirection(direction),
			UserID:       uo.UserID,
			PosType:      uo.PosType,
			RelationID:   uo.RelationID,
		}

		if err := m.executor.HandleOrderFilled(update); err != nil {
			log.Printf("deribit order monitor: HandleOrderFilled failed: %v", err)
		}
	} else {
		// Non-FILLED status update
		if err := m.executor.HandleOrderStatusUpdate(uo.ID, status, avgPrice, execQty, nil); err != nil {
			log.Printf("deribit order monitor: HandleOrderStatusUpdate failed: %v", err)
		}
	}
}

// reconnect handles WebSocket reconnection.
// Respects stopCh: returns early if Stop() is called during reconnection.
func (m *DeribitOrderMonitor) reconnect() {
	m.mu.Lock()
	if m.conn != nil {
		m.conn.Close()
		m.conn = nil
	}
	m.mu.Unlock()

	// Create context that cancels when Stop() is called
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-m.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Attempt reconnection
	for i := 0; i < 5; i++ {
		select {
		case <-ctx.Done():
			log.Printf("deribit order monitor: reconnect cancelled by Stop()")
			return
		default:
		}

		time.Sleep(time.Duration(i+1) * time.Second)

		select {
		case <-ctx.Done():
			log.Printf("deribit order monitor: reconnect cancelled by Stop()")
			return
		default:
		}

		if err := m.Connect(ctx); err == nil {
			log.Printf("deribit order monitor: reconnected after %d attempts", i+1)
			return
		} else {
			log.Printf("deribit order monitor: reconnection attempt %d failed: %v", i+1, err)
		}
	}

	log.Printf("deribit order monitor: reconnection failed after 5 attempts, giving up")
}

// sendHeartbeat sends periodic test requests to keep the connection alive.
// Deribit testnet disconnects idle connections after ~60 seconds.
func (m *DeribitOrderMonitor) sendHeartbeat() {
	hbCh := m.heartbeatStopCh // capture once at start
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-hbCh:
			return
		case <-ticker.C:
			m.mu.RLock()
			conn := m.conn
			m.mu.RUnlock()

			if conn == nil {
				return
			}

			// Send public/test to keep connection alive
			req := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      m.nextReqID(),
				"method":  "public/test",
				"params":  map[string]interface{}{},
			}

			m.writeMu.Lock()
			writeErr := conn.WriteJSON(req)
			m.writeMu.Unlock()
			if writeErr != nil {
				log.Printf("deribit order monitor: heartbeat failed: %v", writeErr)
				// Don't reconnect here - handleMessages will detect the error
			}
		}
	}
}

// Stop stops the monitor.
func (m *DeribitOrderMonitor) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
		m.mu.Lock()
		if m.conn != nil {
			m.conn.Close()
			m.conn = nil
		}
		m.mu.Unlock()
	})
}

// nextReqID generates unique request IDs for JSON-RPC.
func (m *DeribitOrderMonitor) nextReqID() int {
	return int(atomic.AddInt64(&m.reqID, 1))
}

// extractOrderID extracts numeric order ID from Deribit format.
// Examples: "BTC-123456" -> 123456, "ETH-789012" -> 789012
func extractOrderID(orderIDStr string) uint64 {
	// Handle "SYMBOL-NUMBER" format
	if idx := strings.LastIndex(orderIDStr, "-"); idx >= 0 {
		orderIDStr = orderIDStr[idx+1:]
	}

	id, err := strconv.ParseUint(orderIDStr, 10, 64)
	if err != nil {
		log.Printf("deribit order monitor: failed to parse order ID %q: %v", orderIDStr, err)
		return 0
	}
	return id
}

// mapDeribitOrderState converts Deribit order state to internal status.
func mapDeribitOrderState(state string) string {
	switch strings.ToLower(state) {
	case "open":
		return "NEW"
	case "filled":
		return "FILLED"
	case "cancelled", "canceled":
		return "CANCELLED"
	case "rejected":
		return "FAILED"
	default:
		return state
	}
}

// mapDeribitDirection converts Deribit direction to position side.
func mapDeribitDirection(direction string) string {
	if strings.ToLower(direction) == "buy" {
		return "LONG"
	}
	return "SHORT"
}

// deribitOrderFields holds parsed order data from a Deribit WS update.
// Used to pass order details to notification logic without excessive parameters.
type deribitOrderFields struct {
	symbol       string
	direction    string
	status       string
	amount       float64
	filledAmount float64
	orderPrice   float64
	avgPrice     float64
	reduceOnly   bool
	label        string
	isRiskCtrl   bool // pre-computed from label, avoids repeated isRiskControlLabel calls
}

// sendDeribitNotification sends a Deribit-specific notification for any order state change.
// This is called before the common FILLED processing path, regardless of uprunning_order existence.
func (m *DeribitOrderMonitor) sendDeribitNotification(f deribitOrderFields) {
	if m.notifier == nil || m.userName == "" {
		return
	}

	action := m.buildDeribitAction(f.direction, f.status, f.reduceOnly, f.filledAmount, f.amount, f.isRiskCtrl)
	if action == "" {
		return // unknown state, skip notification
	}

	// Determine notification price and quantity
	notifyPrice := f.orderPrice
	notifyQty := f.amount
	unfilledQty := 0.0

	if f.status == "FILLED" {
		notifyPrice = f.avgPrice
		notifyQty = f.filledAmount
		if f.filledAmount < f.amount {
			unfilledQty = f.amount - f.filledAmount
		}
	}

	// Get ROI from UserPosition if available
	roi, maxROI := m.findROIForSymbol(f.symbol)

	msg := &notification.DeribitPositionMessage{
		UserName:    m.userName,
		Symbol:      f.symbol,
		Action:      action,
		Quantity:    notifyQty,
		UnfilledQty: unfilledQty,
		Price:       notifyPrice,
		ROI:         roi,
		MaxROI:      maxROI,
		IsRiskCtrl:  f.isRiskCtrl,
	}

	go func() {
		if err := m.notifier.SendDeribitPositionNotification(msg); err != nil {
			log.Printf("deribit order monitor: failed to send Deribit position notification: %v", err)
		}
	}()
}

// buildDeribitAction constructs the action text for Deribit notifications.
// Returns empty string for unhandled states.
//
// Action format: [prefix] + open/reduce + 买仓/卖仓 + action suffix
// e.g. "触发止盈止损，减卖仓部分成交成功" = "触发止盈止损，" + "减" + "卖仓" + "部分成交成功"
func (m *DeribitOrderMonitor) buildDeribitAction(
	direction, status string,
	reduceOnly bool,
	filledAmount, amount float64,
	isRiskCtrl bool,
) string {
	isBuy := strings.ToLower(direction) == "buy"

	// Determine position type: 买仓/卖仓
	posType := "卖仓"
	if isBuy {
		posType = "买仓"
	}

	// Determine open/reduce prefix
	posPrefix := "开"
	if reduceOnly {
		posPrefix = "减"
	}

	// Build action based on status
	switch status {
	case "NEW":
		return posPrefix + posType + "挂单成功"

	case "FILLED":
		partialFill := filledAmount < amount
		// Risk control prefix only for FILLED orders
		riskPrefix := ""
		if isRiskCtrl {
			riskPrefix = "触发止盈止损，"
		}
		// Open position filled has no partial fill concept
		if !reduceOnly {
			return posPrefix + posType + "成功"
		}
		// Reduce position: distinguish full vs partial fill
		fillSuffix := "成功"
		if partialFill {
			fillSuffix = "部分成交成功"
		}
		return riskPrefix + posPrefix + posType + fillSuffix

	case "CANCELLED":
		return posPrefix + posType + "挂单已取消"

	case "FAILED":
		return posPrefix + posType + "挂单失败"

	default:
		return ""
	}
}

// findROIForSymbol looks up ROI and MaxROI from UserPosition for the given symbol.
// Returns (0, 0) if not found — caller should skip ROI display in that case.
func (m *DeribitOrderMonitor) findROIForSymbol(symbol string) (roi, maxROI float64) {
	if m.repo == nil {
		return 0, 0
	}

	// Find active UserOrderPosition by symbol to get UserStrategyID
	active := true
	positions := m.repo.ListUserOrderPositionsByFilter(persistence.UserOrderPositionFilter{
		Active: &active,
	})

	var strategyID uint64
	for _, pos := range positions {
		if pos.Asset == symbol && pos.Deleted == 0 {
			strategyID = pos.UserStrategyID
			break
		}
	}

	if strategyID == 0 {
		return 0, 0
	}

	// Find UserPosition by UserStrategyID
	for _, up := range m.repo.ListActiveUserPositions() {
		if up.UserStrategyID == strategyID {
			return up.ROI, up.MaxProfitPercentage
		}
	}

	return 0, 0
}

// isRiskControlLabel checks if the order label indicates risk control.
func isRiskControlLabel(label string) bool {
	lower := strings.ToLower(label)
	return strings.Contains(lower, "risk") ||
		strings.Contains(lower, "止盈止损") ||
		strings.Contains(lower, "tp_sl")
}