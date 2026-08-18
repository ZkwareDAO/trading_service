package deribit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"trading-service/internal/exchange"
)

// Deribit implements exchange.Exchange for Deribit options trading.
//
// Deribit uses JSON-RPC 2.0 protocol for all API calls.
// Authentication uses OAuth2 client_credentials flow via public/auth endpoint.
//
// Symbol Format:
// Unlike Binance/Hyperliquid, Deribit option symbols use native format directly:
// - BTC-17JUL26-64000-P (Put option)
// - BTC-17JUL26-65000-C (Call option)
// - ETH-19JUL26-3000-C (ETH call)
//
// No symbol conversion/transformation is needed - symbols pass through unchanged.
type Deribit struct {
	apiKey    string
	apiSecret string
	apiPwd    string
	baseURL   string
	testnet   bool

	// Authentication state
	accessToken string
	tokenExpiry time.Time
	authMu      sync.Mutex

	// HTTP client - uses system proxy from environment (http_proxy/https_proxy)
	httpClient *http.Client

	// Request ID counter for JSON-RPC
	reqIDMu sync.Mutex
	reqID   int

	// Optional: WebSocket for real-time updates (future implementation)
	wsConnected bool
	wsMu        sync.Mutex
}

// NewDeribit creates a Deribit exchange adapter.
//
// Parameters:
//
//	apiKey    — Deribit API key (client_id)
//	apiSecret — Deribit API secret (client_secret)
//	apiPwd    — Deribit API password (optional for some accounts)
//	testnet   — use true for Deribit testnet
func NewDeribit(apiKey, apiSecret, apiPwd string, testnet bool) (*Deribit, error) {
	baseURL := "https://www.deribit.com/api/v2"
	if testnet {
		baseURL = "https://test.deribit.com/api/v2"
	}

	// Create HTTP client that uses system proxy.
	// Go's http.Client automatically uses proxy from environment variables
	// (http_proxy, https_proxy, no_proxy). This matches the behavior of
	// Binance and Hyperliquid clients which also rely on system proxy.
	return &Deribit{
		apiKey:    apiKey,
		apiSecret: apiSecret,
		apiPwd:    apiPwd,
		baseURL:   baseURL,
		testnet:   testnet,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Name returns the exchange identifier.
func (d *Deribit) Name() string {
	return "deribit"
}

// =============================================================================
// JSON-RPC Helpers
// =============================================================================

// jsonRPCRequest represents a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonRPCResponse represents a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError represents a JSON-RPC error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// nextRequestID generates a unique request ID for JSON-RPC.
func (d *Deribit) nextRequestID() int {
	d.reqIDMu.Lock()
	defer d.reqIDMu.Unlock()
	d.reqID++
	return d.reqID
}

// call makes a JSON-RPC API call to Deribit.
func (d *Deribit) call(ctx context.Context, method string, params interface{}) (*jsonRPCResponse, error) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      d.nextRequestID(),
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", d.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Add authorization header if we have a token
	if d.accessToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+d.accessToken)
	}

	resp, err := d.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Handle non-200 status codes
	if resp.StatusCode != http.StatusOK {
		var rpcErr jsonRPCResponse
		if json.Unmarshal(respBody, &rpcErr) == nil && rpcErr.Error != nil {
			return nil, fmt.Errorf("API error %d: %s", rpcErr.Error.Code, rpcErr.Error.Message)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("API error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return &rpcResp, nil
}

// =============================================================================
// Authentication (Exchange Interface)
// =============================================================================

// Connect authenticates with Deribit using OAuth2 client_credentials.
func (d *Deribit) Connect() error {
	d.authMu.Lock()
	defer d.authMu.Unlock()

	// Skip if token is still valid
	if d.accessToken != "" && time.Now().Before(d.tokenExpiry) {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := map[string]interface{}{
		"client_id":     d.apiKey,
		"client_secret": d.apiSecret,
		"grant_type":    "client_credentials",
	}

	resp, err := d.call(ctx, "public/auth", params)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	var authResult struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(resp.Result, &authResult); err != nil {
		return fmt.Errorf("parse auth response: %w", err)
	}

	d.accessToken = authResult.AccessToken
	d.tokenExpiry = time.Now().Add(time.Duration(authResult.ExpiresIn-60) * time.Second) // 60s buffer

	log.Printf("Deribit authenticated successfully, token expires in %d seconds", authResult.ExpiresIn)
	return nil
}

// GetAPIKey returns the Deribit API key (client_id).
func (d *Deribit) GetAPIKey() string {
	return d.apiKey
}

// GetAPISecret returns the Deribit API secret (client_secret).
func (d *Deribit) GetAPISecret() string {
	return d.apiSecret
}

// Close cleanup resources.
func (d *Deribit) Close() error {
	d.authMu.Lock()
	defer d.authMu.Unlock()

	d.accessToken = ""
	d.tokenExpiry = time.Time{}

	return nil
}

// GetAccessToken returns the current access token (thread-safe).
// Returns empty string if not authenticated.
func (d *Deribit) GetAccessToken() string {
	d.authMu.Lock()
	defer d.authMu.Unlock()
	return d.accessToken
}

// =============================================================================
// Trading Operations (Exchange Interface)
// =============================================================================

// CreateOrder places an option order on Deribit.
//
// Key characteristics:
// - Symbol passes through unchanged (e.g., "BTC-17JUL26-64000-P")
// - Quantity is in contracts (not BTC amount)
// - Price is in BTC (option premium)
// - Deribit uses "private/buy" or "private/sell" endpoints
// - Order status is "open" (not "NEW" like Binance)
func (d *Deribit) CreateOrder(req exchange.CreateOrderRequest) (*exchange.CreateOrderResponse, error) {
	// Ensure authentication
	if err := d.Connect(); err != nil {
		return nil, err
	}

	// Deribit Market order special handling: convert to Limit order with calculated price
	// Deribit doesn't have true Market orders - we convert them to Limit orders
	// with prices that ensure execution
	if req.OrderType == exchange.OrderTypeMarket {
		adjustedPrice, err := d.calculateMarketOrderPrice(req.Symbol, req.Side, req.ReduceOnly)
		if err != nil {
			return nil, fmt.Errorf("calculate market order price for %s: %w", req.Symbol, err)
		}
		log.Printf("[Deribit] Market order converted to Limit for %s: price %.6f", req.Symbol, adjustedPrice)
		req.Price = adjustedPrice
		req.OrderType = exchange.OrderTypeLimit // Convert to limit order
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Determine method: buy or sell
	method := "private/buy"
	if req.Side == exchange.OrderSideSell {
		method = "private/sell"
	}

	// Build params - CRITICAL: symbol passes through unchanged
	params := map[string]interface{}{
		"instrument_name": req.Symbol, // No transformation needed
		"amount":          req.Quantity,
		"type":            strings.ToLower(string(req.OrderType)), // Use converted order type
		"price":           req.Price,                              // Always include price (Limit orders require it)
	}

	// Add reduce_only flag for closing positions
	if req.ReduceOnly {
		params["reduce_only"] = true
	}

	resp, err := d.call(ctx, method, params)
	if err != nil {
		// Parse precision errors for better error messages
		if strings.Contains(err.Error(), "tick_size") ||
			strings.Contains(err.Error(), "Invalid price") ||
			strings.Contains(err.Error(), "Invalid amount") {
			return nil, fmt.Errorf("precision error: %w (let Deribit validate)", err)
		}
		return nil, err
	}

	var orderResult struct {
		Order struct {
			OrderID    string  `json:"order_id"`
			Instrument string  `json:"instrument"`
			Direction  string  `json:"direction"`
			OrderType  string  `json:"order_type"`
			Amount     float64 `json:"amount"`
			Price      float64 `json:"price"`
			OrderState string  `json:"order_state"`
			CreateTime int64   `json:"create_time"`
		} `json:"order"`
	}
	if err := json.Unmarshal(resp.Result, &orderResult); err != nil {
		return nil, fmt.Errorf("parse order response: %w", err)
	}

	// Convert order ID from string to uint64
	// Deribit order IDs have format like "ETH-81179066009" (prefix-number)
	// Extract just the numeric portion
	orderIDStr := orderResult.Order.OrderID
	if idx := strings.LastIndex(orderIDStr, "-"); idx >= 0 {
		orderIDStr = orderIDStr[idx+1:]
	}
	orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse order ID from '%s': %w", orderResult.Order.OrderID, err)
	}

	// CRITICAL: Always return NEW status to ensure Scanner processing chain
	// Even if immediately filled, set to NEW and let Scanner handle it
	// This matches Hyperliquid's approach for consistent behavior across exchanges
	return &exchange.CreateOrderResponse{
		OrderID:    orderID,
		Symbol:     orderResult.Order.Instrument,
		Side:       req.Side,
		Status:     exchange.OrderStatus("NEW"),
		Price:      orderResult.Order.Price,
		Quantity:   orderResult.Order.Amount,
		ExecutedAt: time.UnixMilli(orderResult.Order.CreateTime),
	}, nil
}

// CancelOrder cancels an option order.
func (d *Deribit) CancelOrder(orderID uint64) error {
	// Ensure authentication
	if err := d.Connect(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := map[string]interface{}{
		"order_id": strconv.FormatUint(orderID, 10),
	}

	resp, err := d.call(ctx, "private/cancel", params)
	if err != nil {
		return err
	}

	var cancelResult struct {
		OrderID    string `json:"order_id"`
		OrderState string `json:"order_state"`
	}
	if err := json.Unmarshal(resp.Result, &cancelResult); err != nil {
		return fmt.Errorf("parse cancel response: %w", err)
	}

	return nil
}

// GetOrder queries order status for scanner mechanism.
//
// Uses Deribit's private/get_order_state endpoint.
// This is critical for the scanner to detect filled/cancelled orders.
//
// Fallback logic: Deribit may return order IDs in format "SYMBOL-NUMBER" (e.g., "ETH-81179066009").
// The scanner stores only the numeric part. This method tries the numeric ID first,
// then falls back to the prefixed ID based on the symbol.
func (d *Deribit) GetOrder(orderID uint64, symbol string) (*exchange.OrderInfo, error) {
	// Ensure authentication
	if err := d.Connect(); err != nil {
		return nil, err
	}

	orderIDStr := strconv.FormatUint(orderID, 10)

	// Try with numeric ID first
	info, err := d.getOrderByID(orderIDStr)
	if err == nil {
		return info, nil
	}

	// If numeric ID fails, try with symbol prefix
	// Extract prefix from symbol (e.g., "ETH-25SEP26-1900-P" -> "ETH")
	if idx := strings.Index(symbol, "-"); idx > 0 {
		prefixedID := symbol[:idx] + "-" + orderIDStr
		info, err = d.getOrderByID(prefixedID)
		if err == nil {
			return info, nil
		}
	}

	return nil, err
}

// getOrderByID queries order status by order ID string.
func (d *Deribit) getOrderByID(orderIDStr string) (*exchange.OrderInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := map[string]interface{}{
		"order_id": orderIDStr,
	}

	resp, err := d.call(ctx, "private/get_order_state", params)
	if err != nil {
		return nil, err
	}

	var orderState struct {
		OrderID      string  `json:"order_id"`
		Instrument   string  `json:"instrument"`
		OrderState   string  `json:"order_state"`
		Direction    string  `json:"direction"`
		Amount       float64 `json:"amount"`
		FilledAmount float64 `json:"filled_amount"`
		Price        float64 `json:"price"`
		AveragePrice float64 `json:"average_price"`
	}
	if err := json.Unmarshal(resp.Result, &orderState); err != nil {
		return nil, fmt.Errorf("parse order state: %w", err)
	}

	// Convert order ID - extract numeric portion if prefixed
	idStr := orderState.OrderID
	if idx := strings.LastIndex(idStr, "-"); idx >= 0 {
		idStr = idStr[idx+1:]
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse order ID: %w", err)
	}

	// Map direction to side
	side := exchange.OrderSideBuy
	if orderState.Direction == "sell" {
		side = exchange.OrderSideSell
	}

	// Map order state
	status := mapDeribitOrderState(orderState.OrderState)

	return &exchange.OrderInfo{
		OrderID:  id,
		Symbol:   orderState.Instrument,
		Side:     side,
		Status:   status,
		Price:    orderState.Price,
		Qty:      orderState.Amount,
		Filled:   orderState.FilledAmount,
		AvgPrice: orderState.AveragePrice,
	}, nil
}

// GetPositions queries all option positions (filters out futures).
//
// Uses private/get_positions endpoint.
// Only returns positions with kind="option" to avoid mixing with perpetual futures.
func (d *Deribit) GetPositions() ([]exchange.PositionInfo, error) {
	// Ensure authentication
	if err := d.Connect(); err != nil {
		return nil, err
	}

	result := make([]exchange.PositionInfo, 0)

	// Query positions for BTC and ETH currencies
	for _, currency := range []string{"BTC", "ETH"} {
		positions, err := d.getPositionsForCurrency(currency)
		if err != nil {
			// Log but continue with other currencies
			log.Printf("Warning: failed to query %s positions: %v", currency, err)
			continue
		}
		result = append(result, positions...)
	}

	return result, nil
}

// getPositionsForCurrency queries positions for a specific currency.
func (d *Deribit) getPositionsForCurrency(currency string) ([]exchange.PositionInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := map[string]interface{}{
		"currency": currency,
	}

	resp, err := d.call(ctx, "private/get_positions", params)
	if err != nil {
		return nil, err
	}

	var positions []struct {
		InstrumentName  string  `json:"instrument_name"`
		Kind            string  `json:"kind"`
		Size            float64 `json:"size"`
		Direction       string  `json:"direction"`
		AveragePrice    float64 `json:"average_price"`
		MarkPrice       float64 `json:"mark_price"`
		TotalProfitLoss float64 `json:"total_profit_loss"`
	}
	if err := json.Unmarshal(resp.Result, &positions); err != nil {
		return nil, fmt.Errorf("parse positions: %w", err)
	}

	// Filter to only option positions (not futures)
	result := make([]exchange.PositionInfo, 0)
	for _, pos := range positions {
		// Skip non-option positions (perpetual futures, etc.)
		if pos.Kind != "option" {
			continue
		}

		// Skip zero-size positions
		if pos.Size == 0 {
			continue
		}

		// Convert size to positive value and determine side
		qty := pos.Size
		positionSide := exchange.PositionSideLong
		if qty < 0 {
			positionSide = exchange.PositionSideShort
			qty = -qty
		}

		// Deribit options don't have leverage (leverage=1)
		result = append(result, exchange.PositionInfo{
			Symbol:        pos.InstrumentName,
			PositionSide:  positionSide,
			Quantity:      qty,
			EntryPrice:    pos.AveragePrice,
			MarkPrice:     pos.MarkPrice,
			UnrealizedPnl: pos.TotalProfitLoss,
			Leverage:      0, // Options don't use leverage concept
		})
	}

	return result, nil
}

// =============================================================================
// Price Queries (Exchange Interface)
// =============================================================================

// GetPrice queries the mark price for an option.
func (d *Deribit) GetPrice(symbol string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := map[string]interface{}{
		"instrument_name": symbol,
	}

	resp, err := d.call(ctx, "public/ticker", params)
	if err != nil {
		return 0, err
	}

	var ticker struct {
		Instrument   string  `json:"instrument_name"`
		MarkPrice    float64 `json:"mark_price"`
		BestBidPrice float64 `json:"best_bid_price"`
		BestAskPrice float64 `json:"best_ask_price"`
	}
	if err := json.Unmarshal(resp.Result, &ticker); err != nil {
		return 0, fmt.Errorf("parse ticker: %w", err)
	}

	return ticker.MarkPrice, nil
}

// =============================================================================
// Leverage (Exchange Interface - Not Applicable for Options)
// =============================================================================

// SetLeverage is not applicable for options (options don't use leverage).
// Returns an error indicating leverage is not supported for options.
func (d *Deribit) SetLeverage(symbol string, leverage int) error {
	return fmt.Errorf("leverage not supported for Deribit options")
}

// GetLeverage returns 0 for options (no leverage concept).
func (d *Deribit) GetLeverage(symbol string) (int, error) {
	return 0, nil // Options don't have leverage
}

// =============================================================================
// WebSocket Subscriptions (Exchange Interface - Future Implementation)
// =============================================================================

// SubscribeOrders subscribes to order updates via WebSocket.
//
// TODO: Implement WebSocket connection for real-time order updates.
// For now, scanner mechanism will poll via GetOrder.
func (d *Deribit) SubscribeOrders(callback exchange.OrderCallback) error {
	d.wsMu.Lock()
	defer d.wsMu.Unlock()

	// For now, return error indicating WebSocket not yet implemented
	// Scanner mechanism will use polling via GetOrder instead
	return fmt.Errorf("WebSocket order subscription not yet implemented for Deribit - use scanner polling")
}

// =============================================================================
// Helper Functions
// =============================================================================

// mapDeribitOrderState converts Deribit order state to our OrderStatus.
func mapDeribitOrderState(state string) exchange.OrderStatus {
	switch state {
	case "open":
		return exchange.OrderStatusNew // Deribit uses "open" for active orders
	case "filled":
		return exchange.OrderStatusFilled
	case "cancelled":
		return exchange.OrderStatusCancelled
	case "rejected":
		return exchange.OrderStatusFailed
	default:
		return exchange.OrderStatusFailed
	}
}

// InstrumentDetails represents Deribit instrument information.
type InstrumentDetails struct {
	TickSize       float64        `json:"tick_size"`
	ContractSize   float64        `json:"contract_size"`
	MinTradeAmount float64        `json:"min_trade_amount"`
	TickSizeSteps  []TickSizeStep `json:"tick_size_steps"`
}

// TickSizeStep represents a tick size threshold.
type TickSizeStep struct {
	AbovePrice float64 `json:"above_price"`
	TickSize   float64 `json:"tick_size"`
}

// TickerInfo represents Deribit ticker information.
type TickerInfo struct {
	MarkPrice float64 `json:"mark_price"`
	BestBid   float64 `json:"best_bid_price"`
	BestAsk   float64 `json:"best_ask_price"`
}

// GetInstrumentDetails queries instrument details including tick_size.
func (d *Deribit) GetInstrumentDetails(symbol string) (*InstrumentDetails, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := map[string]interface{}{"instrument_name": symbol}
	resp, err := d.call(ctx, "public/get_instrument", params)
	if err != nil {
		return nil, err
	}

	var result InstrumentDetails
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode instrument details: %w", err)
	}

	return &result, nil
}

// GetTickerInfo queries full ticker information including bid/ask prices.
func (d *Deribit) GetTickerInfo(symbol string) (*TickerInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := map[string]interface{}{"instrument_name": symbol}
	resp, err := d.call(ctx, "public/ticker", params)
	if err != nil {
		return nil, err
	}

	var result TickerInfo
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode ticker: %w", err)
	}

	return &result, nil
}

// getTickSizeForPrice selects the correct tick_size based on price.
func getTickSizeForPrice(price float64, details *InstrumentDetails) float64 {
	// Check tick_size_steps in reverse order (highest threshold first)
	for i := len(details.TickSizeSteps) - 1; i >= 0; i-- {
		step := details.TickSizeSteps[i]
		if price >= step.AbovePrice {
			return step.TickSize
		}
	}
	// Default tick_size
	return details.TickSize
}

// truncateToTickSize truncates price to tick_size multiple.
func truncateToTickSize(price, tickSize float64, ceil bool) float64 {
	if ceil {
		return math.Ceil(price/tickSize) * tickSize
	}
	return math.Floor(price/tickSize) * tickSize
}

// calculateMarketOrderPrice calculates the optimal price for a Market order.
// For CLOSE orders (reduceOnly=true): use bid/ask directly (no truncation)
// For OPEN orders (reduceOnly=false): use mark_price with 5-tick offset
func (d *Deribit) calculateMarketOrderPrice(symbol string, side exchange.OrderSide, reduceOnly bool) (float64, error) {
	ticker, err := d.GetTickerInfo(symbol)
	if err != nil {
		return 0, fmt.Errorf("get ticker: %w", err)
	}

	// Close position: use bid/ask directly (no truncation)
	if reduceOnly {
		if side == exchange.OrderSideBuy {
			log.Printf("[Deribit] Close short: symbol=%s, ask=%.6f", symbol, ticker.BestAsk)
			return ticker.BestAsk, nil
		}
		log.Printf("[Deribit] Close long: symbol=%s, bid=%.6f", symbol, ticker.BestBid)
		return ticker.BestBid, nil
	}

	// Open position: use mark_price with 5-tick offset
	details, err := d.GetInstrumentDetails(symbol)
	if err != nil {
		return 0, fmt.Errorf("get instrument details: %w", err)
	}

	tickSize := getTickSizeForPrice(ticker.MarkPrice, details)
	var orderPrice float64
	if side == exchange.OrderSideBuy {
		orderPrice = truncateToTickSize(ticker.MarkPrice-5*tickSize, tickSize, false)
	} else {
		orderPrice = truncateToTickSize(ticker.MarkPrice+5*tickSize, tickSize, true)
	}

	log.Printf("[Deribit] Open %s: symbol=%s, mark=%.6f, tick_size=%.6f, price=%.6f",
		side, symbol, ticker.MarkPrice, tickSize, orderPrice)
	return orderPrice, nil
}
