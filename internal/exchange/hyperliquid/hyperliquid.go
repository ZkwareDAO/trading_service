package hyperliquid

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"trading-service/internal/exchange"

	"github.com/ethereum/go-ethereum/crypto"
	hyperliquid "github.com/sonirico/go-hyperliquid"
)

// infoClient abstracts the Hyperliquid Info client for testability.
type infoClient interface {
	QueryOrderByOid(ctx context.Context, user string, oid int64) (*hyperliquid.OrderQueryResult, error)
	UserState(ctx context.Context, address string, dex ...string) (*hyperliquid.UserState, error)
	AllMids(ctx context.Context, dex ...string) (map[string]string, error)
	Meta(ctx context.Context, dex ...string) (*hyperliquid.Meta, error)
	UserFills(ctx context.Context, params hyperliquid.UserFillsParams) ([]hyperliquid.Fill, error)
	UserFillsByTime(ctx context.Context, address string, startTime int64, endTime *int64, aggregateByTime *bool) ([]hyperliquid.Fill, error)
}

// exchangeClient abstracts the Hyperliquid Exchange client for testability.
type exchangeClient interface {
	Order(ctx context.Context, req hyperliquid.CreateOrderRequest, builder *hyperliquid.BuilderInfo) (hyperliquid.OrderStatus, error)
	Cancel(ctx context.Context, coin string, oid int64) (*hyperliquid.APIResponse[hyperliquid.CancelOrderResponse], error)
	UpdateLeverage(ctx context.Context, leverage int, name string, isCross bool) (*hyperliquid.UserState, error)
	MarketClose(ctx context.Context, coin string, sz *float64, px *float64, slippage float64, cloid *string, builder *hyperliquid.BuilderInfo) (hyperliquid.OrderStatus, error)
	SlippagePrice(ctx context.Context, name string, isBuy bool, slippage float64, px *float64) (float64, error)
}

// Hyperliquid implements exchange.Exchange for the Hyperliquid perp DEX.
//
// Unlike CEX APIs (Binance, etc.), Hyperliquid is on-chain: authentication
// uses an Ethereum private key for message signing rather than API key/secret.
// The account address (derived from the private key or explicitly provided) is
// required for read operations like GetOrder and GetLeverage.
//
// A non-empty accountAddr enables GetOrder/GetLeverage; an empty accountAddr
// restricts the adapter to trading operations only (CreateOrder, Cancel, etc.).
type Hyperliquid struct {
	privateKey  *ecdsa.PrivateKey
	accountAddr string
	info        infoClient
	exch        exchangeClient
	baseURL     string

	// initOnceMu protects initialization with auto-retry capability
	initOnceMu sync.Mutex

	// readOnlyInitMu protects read-only initialization and retry logic
	readOnlyInitMu sync.Mutex

	// Auto-recovery state
	lastInitAttempt time.Time
	initBackoff     time.Duration
	initErr         error
}

// checkBackoffUnderLock returns an error if still within backoff window.
// Must be called with the appropriate mutex already held.
// Sets initial backoff if first attempt.
func (h *Hyperliquid) checkBackoffUnderLock() error {
	now := time.Now()
	if h.lastInitAttempt.IsZero() {
		h.initBackoff = 5 * time.Second
		return nil
	}

	if since := now.Sub(h.lastInitAttempt); since < h.initBackoff {
		return fmt.Errorf("hyperliquid init backing off (%v remaining)", h.initBackoff-since)
	}
	return nil
}

// updateBackoffOnFailureLocked increases backoff after failure (max 60s).
// Must be called with the appropriate mutex already held.
func (h *Hyperliquid) updateBackoffOnFailureLocked(err error) error {
	h.initBackoff = min(h.initBackoff*2, 60*time.Second)
	h.initErr = err
	h.lastInitAttempt = time.Now()
	log.Printf("Hyperliquid init failed (backoff now %v): %v", h.initBackoff, err)
	return err
}

// resetBackoffOnSuccessLocked resets backoff after successful init.
// Must be called with the appropriate mutex already held.
func (h *Hyperliquid) resetBackoffOnSuccessLocked() {
	h.initBackoff = 5 * time.Second
	h.initErr = nil
	h.lastInitAttempt = time.Now()
	log.Printf("Hyperliquid init succeeded")
}

// NewHyperliquid creates a Hyperliquid exchange adapter.
//
// Parameters:
//
//	privateKeyHex — hex-encoded ECDSA private key (with or without 0x prefix)
//	accountAddr  — your Hyperliquid wallet address (required for GetOrder/GetLeverage); with or without 0x prefix
//	testnet      — use true for Hyperliquid testnet
func NewHyperliquid(privateKeyHex, accountAddr string, testnet bool) (*Hyperliquid, error) {
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	if accountAddr != "" && !strings.HasPrefix(accountAddr, "0x") {
		accountAddr = "0x" + accountAddr
	}

	baseURL := hyperliquid.MainnetAPIURL
	if testnet {
		baseURL = hyperliquid.TestnetAPIURL
	}

	return &Hyperliquid{
		privateKey:  privateKey,
		accountAddr: accountAddr,
		baseURL:     baseURL,
	}, nil
}

// NewHyperliquidNoAuth creates a read-only Hyperliquid adapter (market data only).
// Trading operations will fail.
func NewHyperliquidNoAuth(testnet bool) *Hyperliquid {
	baseURL := hyperliquid.MainnetAPIURL
	if testnet {
		baseURL = hyperliquid.TestnetAPIURL
	}
	return &Hyperliquid{
		baseURL: baseURL,
	}
}

// newHyperliquidForTest creates a Hyperliquid adapter with injected mocks.
func newHyperliquidForTest(info infoClient, exch exchangeClient, accountAddr string) *Hyperliquid {
	return &Hyperliquid{
		info:        info,
		exch:        exch,
		accountAddr: accountAddr,
	}
}

// initLazy ensures the exchange and info clients are initialized.
// AUTO-RECOVERY: applies backoff on failure, allows retry after backoff period.
// Thread-safe: all checks and updates are protected by mutex.
func (h *Hyperliquid) initLazy() error {
	h.initOnceMu.Lock()
	defer h.initOnceMu.Unlock()

	// Fast path: already initialized (check under lock)
	if h.exch != nil {
		return nil
	}
	if h.info != nil && h.privateKey == nil {
		return nil // info-only mode
	}

	// Check backoff
	if err := h.checkBackoffUnderLock(); err != nil {
		return err
	}

	// Attempt initialization with panic recovery
	ex, err := h.initExchangeWithRecovery()
	if err != nil {
		h.updateBackoffOnFailureLocked(err)
		return err
	}

	h.exch = ex
	h.info = ex.Info()
	h.resetBackoffOnSuccessLocked()
	return nil
}

// initExchangeWithRecovery calls NewExchange with panic recovery and timeout.
func (h *Hyperliquid) initExchangeWithRecovery() (*hyperliquid.Exchange, error) {
	var ex *hyperliquid.Exchange
	var panicErr error

	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicErr = fmt.Errorf("hyperliquid SDK panic: %v", r)
				log.Printf("Recovered from hyperliquid SDK panic: %v", r)
			}
			close(done)
		}()

		if h.privateKey == nil {
			panicErr = fmt.Errorf("private key not configured")
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		ex = hyperliquid.NewExchange(
			ctx,
			h.privateKey,
			h.baseURL,
			nil, "", h.accountAddr, nil, nil,
		)
	}()

	select {
	case <-done:
		if panicErr != nil {
			return nil, panicErr
		}
		if ex == nil {
			return nil, fmt.Errorf("hyperliquid.NewExchange returned nil")
		}
		return ex, nil
	case <-time.After(35 * time.Second):
		return nil, fmt.Errorf("hyperliquid init timeout after 35s")
	}
}

// initLazyReadOnly ensures the info client is initialized for read-only access.
// AUTO-RECOVERY: applies backoff on failure, allows retry after backoff period.
// Thread-safe: all checks and updates are protected by mutex.
func (h *Hyperliquid) initLazyReadOnly() error {
	h.readOnlyInitMu.Lock()
	defer h.readOnlyInitMu.Unlock()

	// Fast path: already initialized (check under lock)
	if h.info != nil {
		return nil
	}

	// Check backoff
	if err := h.checkBackoffUnderLock(); err != nil {
		return err
	}

	// Attempt initialization with panic recovery
	info, err := h.initInfoWithRecovery()
	if err != nil {
		h.updateBackoffOnFailureLocked(err)
		return err
	}

	h.info = info
	h.resetBackoffOnSuccessLocked()
	return nil
}

// initInfoWithRecovery calls NewInfo with panic recovery and timeout.
func (h *Hyperliquid) initInfoWithRecovery() (*hyperliquid.Info, error) {
	var info *hyperliquid.Info
	var panicErr error

	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicErr = fmt.Errorf("hyperliquid NewInfo panic: %v", r)
			}
			close(done)
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		info = hyperliquid.NewInfo(ctx, h.baseURL, true, nil, nil, nil)
	}()

	select {
	case <-done:
		if panicErr != nil {
			return nil, panicErr
		}
		return info, nil
	case <-time.After(35 * time.Second):
		return nil, fmt.Errorf("hyperliquid NewInfo timeout after 35s")
	}
}

func (h *Hyperliquid) Name() string { return "hyperliquid" }

func (h *Hyperliquid) CreateOrder(req exchange.CreateOrderRequest) (*exchange.CreateOrderResponse, error) {
	if err := h.initLazy(); err != nil {
		return nil, err
	}

	if h.exch == nil {
		return nil, fmt.Errorf("hyperliquid exchange client not initialized (privateKey=%v, accountAddr=%v)", h.privateKey != nil, h.accountAddr)
	}

	ctx := context.Background()

	isBuy := req.Side == exchange.OrderSideBuy

	// Build order type — Hyperliquid uses limit orders with TIF for both limit and "market"
	orderType := hyperliquid.OrderType{}
	price := req.Price
	var err error
	if req.OrderType == exchange.OrderTypeLimit {
		orderType.Limit = &hyperliquid.LimitOrderType{Tif: hyperliquid.TifGtc}
	} else {
		// Market orders: use limit with IOC (Immediate-or-Cancel).
		// Hyperliquid requires a price even for IOC orders.
		// Use the SDK's SlippagePrice to ensure correct tick size compliance.
		orderType.Limit = &hyperliquid.LimitOrderType{Tif: hyperliquid.TifIoc}

		// Use default slippage (5%) for aggressive matching
		slippage := hyperliquid.DefaultSlippage

		// Recover from potential SDK panic when accessing uninitialized data structures
		var panicErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicErr = fmt.Errorf("hyperliquid SDK panic in SlippagePrice: %v", r)
					log.Printf("Recovered from SDK panic in SlippagePrice: %v", r)
				}
			}()
			price, err = h.exch.SlippagePrice(ctx, stripQuoteCurrency(req.Symbol), isBuy, slippage, nil)
		}()

		if panicErr != nil {
			return nil, panicErr
		}
		if err != nil {
			return nil, fmt.Errorf("hyperliquid market order: calculate slippage price: %w", err)
		}
	}

	hlReq := hyperliquid.CreateOrderRequest{
		Coin:       stripQuoteCurrency(req.Symbol),
		IsBuy:      isBuy,
		Price:      price,
		Size:       req.Quantity,
		ReduceOnly: req.ReduceOnly, // pass through for risk control orders
		OrderType:  orderType,
	}

	status, err := h.exch.Order(ctx, hlReq, nil)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid create order: %w", err)
	}

	// Extract order ID, status, and actual fill data from status
	var orderID uint64
	var fillQty float64
	var fillPrice float64
	if status.Resting != nil {
		orderID = uint64(status.Resting.Oid)
		fillQty = req.Quantity
		fillPrice = price
	} else if status.Filled != nil {
		orderID = uint64(status.Filled.Oid)
		fillPrice, _ = strconv.ParseFloat(status.Filled.AvgPx, 64)
		fillQty, _ = strconv.ParseFloat(status.Filled.TotalSz, 64)
		if fillQty <= 0 {
			fillQty = req.Quantity
		}
		if fillPrice <= 0 {
			fillPrice = price
		}
	} else if status.Error != nil {
		return nil, fmt.Errorf("hyperliquid order error: %s", *status.Error)
	}

	// Always return NEW status to ensure WS/Scanner processing chain
	// Even if immediately filled, set to NEW and let Scanner handle it
	return &exchange.CreateOrderResponse{
		OrderID:    orderID,
		Symbol:     req.Symbol,
		Side:       req.Side,
		Status:     exchange.OrderStatus("NEW"),
		Price:      fillPrice,
		Quantity:   fillQty,
		ExecutedAt: time.Now(),
	}, nil
}

func (h *Hyperliquid) CancelOrder(orderID uint64) error {
	if err := h.initLazy(); err != nil {
		return err
	}
	// Cancel requires a symbol (coin). Since our interface doesn't carry it,
	// we can't cancel without knowing the coin. Return an error instructing
	// the caller to use a coin-aware cancel if needed.
	return fmt.Errorf("hyperliquid cancel requires coin symbol; use CancelOrderByCoin(symbol, orderID)")
}

// CancelOrderByCoin cancels an order with the known coin symbol.
//
// This is a Hyperliquid-specific extension beyond the exchange.Exchange interface,
// because Hyperliquid's Cancel API requires both coin and order ID.
//
// Use this when you know the symbol for the order; prefer CancelOrder for
// interface-compatible code where the coin is not readily available.
func (h *Hyperliquid) CancelOrderByCoin(symbol string, orderID uint64) error {
	if err := h.initLazy(); err != nil {
		return err
	}

	ctx := context.Background()
	_, err := h.exch.Cancel(ctx, symbol, int64(orderID))
	if err != nil {
		return fmt.Errorf("hyperliquid cancel order: %w", err)
	}
	return nil
}

func (h *Hyperliquid) GetOrder(orderID uint64, symbol string) (*exchange.OrderInfo, error) {
	if h.accountAddr == "" {
		return nil, fmt.Errorf("account address required for GetOrder")
	}
	if err := h.initLazy(); err != nil {
		return nil, err
	}

	ctx := context.Background()
	result, err := h.info.QueryOrderByOid(ctx, h.accountAddr, int64(orderID))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid get order: %w", err)
	}

	// Fallback path: QueryOrderByOid only returns active orders
	if result == nil || result.Status == hyperliquid.OrderQueryStatusError {
		log.Printf("Hyperliquid GetOrder: QueryOrderByOid not found for order %d, trying UserFills fallback", orderID)
		return h.getOrderFromUserFills(ctx, orderID, "", exchange.OrderSideBuy)
	}

	// Parse order from QueryOrderByOid result
	qo := result.Order.Order
	priceFloat, err := parseStringToFloat(qo.LimitPx)
	if err != nil {
		return nil, fmt.Errorf("parse price for order %d: %w", orderID, err)
	}

	szFloat, err := parseStringToFloat(qo.Sz)
	if err != nil {
		return nil, fmt.Errorf("parse size for order %d: %w", orderID, err)
	}

	origSzFloat, err := parseStringToFloat(qo.OrigSz)
	if err != nil {
		return nil, fmt.Errorf("parse original size for order %d: %w", orderID, err)
	}

	filled := origSzFloat - szFloat
	if filled < 0 {
		filled = 0
	}

	side := mapHyperliquidSide(qo.Side)
	status := mapHyperliquidStatus(result.Order.Status)

	// CRITICAL: For filled orders, fetch actual execution price from UserFills
	// (QueryOrderByOid only returns limit price, not actual fill price)
	if status == exchange.OrderStatusFilled {
		fillInfo, err := h.getOrderFromUserFills(ctx, orderID, qo.Coin, side)
		if err == nil {
			return fillInfo, nil
		}
		log.Printf("Hyperliquid GetOrder: UserFills fallback failed for filled order %d: %v, using limit price (INACCURATE)", orderID, err)
	}

	return &exchange.OrderInfo{
		OrderID:  orderID,
		Symbol:   qo.Coin,
		Side:     side,
		Status:   status,
		Price:    priceFloat,
		Qty:      origSzFloat,
		Filled:   filled,
		AvgPrice: priceFloat, // For active orders, use limit price
	}, nil
}

// getOrderFromUserFills fetches actual execution price from UserFills API.
// Used as fallback when QueryOrderByOid fails or returns filled order without execution price.
// IMPORTANT: Aggregates multiple partial fills for the same order to calculate avg price.
func (h *Hyperliquid) getOrderFromUserFills(ctx context.Context, orderID uint64, expectedSymbol string, expectedSide exchange.OrderSide) (*exchange.OrderInfo, error) {
	fills, err := h.info.UserFills(ctx, hyperliquid.UserFillsParams{
		Address: h.accountAddr,
	})
	if err != nil {
		return nil, fmt.Errorf("userFills query failed: %w", err)
	}

	log.Printf("Hyperliquid GetOrder: UserFills returned %d records", len(fills))

	// Collect all fills for this order (partial fills have multiple records with same oid)
	var orderFills []hyperliquid.Fill
	var symbol string
	var side exchange.OrderSide

	for _, fill := range fills {
		if fill.Oid == int64(orderID) {
			orderFills = append(orderFills, fill)

			// Use first fill's metadata if not provided
			if symbol == "" && expectedSymbol == "" {
				symbol = fill.Coin
			} else if expectedSymbol != "" {
				symbol = expectedSymbol
			}

			if side == exchange.OrderSideBuy && expectedSide == exchange.OrderSideBuy {
				if fill.Side == "B" {
					side = exchange.OrderSideBuy
				} else {
					side = exchange.OrderSideSell
				}
			} else {
				side = expectedSide
			}
		}
	}

	if len(orderFills) == 0 {
		return nil, fmt.Errorf("order %d not found in %d fill records", orderID, len(fills))
	}

	// Calculate weighted average price and total quantity
	// avgPrice = sum(px * sz) / sum(sz)
	var totalValue float64
	var totalQty float64

	for _, fill := range orderFills {
		px, _ := strconv.ParseFloat(fill.Price, 64)
		sz, _ := strconv.ParseFloat(fill.Size, 64)
		totalValue += px * sz
		totalQty += sz
	}

	avgPrice := totalValue / totalQty

	log.Printf("Hyperliquid GetOrder: found order %d in fills (%d partial fills, avg price=%.8f, total qty=%.8f)",
		orderID, len(orderFills), avgPrice, totalQty)

	return &exchange.OrderInfo{
		OrderID:  orderID,
		Symbol:   symbol,
		Side:     side,
		Status:   exchange.OrderStatusFilled,
		Price:    avgPrice, // Weighted average price
		Qty:      totalQty, // Total filled quantity
		Filled:   totalQty,
		AvgPrice: avgPrice, // Weighted average price
	}, nil
}

// parseStringToFloat converts a decimal string to float64.
func parseStringToFloat(s string) (float64, error) {
	val, _, err := big.NewFloat(0).Parse(s, 10)
	if err != nil {
		return 0, err
	}
	f, _ := val.Float64()
	return f, nil
}

// mapHyperliquidSide converts Hyperliquid order side to exchange.OrderSide.
func mapHyperliquidSide(side hyperliquid.OrderSide) exchange.OrderSide {
	if side == hyperliquid.OrderSideBid {
		return exchange.OrderSideBuy
	}
	return exchange.OrderSideSell
}

// mapHyperliquidStatus converts Hyperliquid order status to exchange.OrderStatus.
func mapHyperliquidStatus(status hyperliquid.OrderStatusValue) exchange.OrderStatus {
	switch status {
	case hyperliquid.OrderStatusValueOpen:
		return exchange.OrderStatusNew
	case hyperliquid.OrderStatusValueFilled, hyperliquid.OrderStatusValueTriggered:
		return exchange.OrderStatusFilled
	case hyperliquid.OrderStatusValueCanceled, hyperliquid.OrderStatusValueMarginCanceled:
		return exchange.OrderStatusCancelled
	default:
		return exchange.OrderStatusFailed
	}
}

func (h *Hyperliquid) SetLeverage(symbol string, leverage int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hyperliquid set leverage panic: %v", r)
		}
	}()

	if err := h.initLazy(); err != nil {
		return err
	}

	if h.exch == nil {
		return fmt.Errorf("hyperliquid exchange client not initialized")
	}

	ctx := context.Background()
	// Hyperliquid takes the total leverage multiplier (e.g., 5 = 5x).
	// We use cross margin mode (isCross = true) as the default.
	_, err = h.exch.UpdateLeverage(ctx, leverage, stripQuoteCurrency(symbol), true)
	if err != nil {
		return fmt.Errorf("hyperliquid set leverage: %w", err)
	}
	return nil
}

func (h *Hyperliquid) GetLeverage(symbol string) (int, error) {
	if h.accountAddr == "" {
		return 0, fmt.Errorf("account address required for GetLeverage")
	}
	if err := h.initLazy(); err != nil {
		return 0, err
	}

	ctx := context.Background()
	userState, err := h.info.UserState(ctx, h.accountAddr)
	if err != nil {
		return 0, fmt.Errorf("hyperliquid get leverage: %w", err)
	}

	for _, ap := range userState.AssetPositions {
		if ap.Position.Coin == symbol {
			return ap.Position.Leverage.Value, nil
		}
	}
	return 0, fmt.Errorf("leverage not found for %s (no open position)", symbol)
}

func (h *Hyperliquid) GetPrice(symbol string) (float64, error) {
	// For read-only access (no private key), use the standalone path.
	if h.info == nil && h.privateKey == nil {
		if err := h.initLazyReadOnly(); err != nil {
			return 0, err
		}
	} else {
		// Normal path: initialize exchange client
		if err := h.initLazy(); err != nil {
			return 0, err
		}
	}

	ctx := context.Background()
	mids, err := h.info.AllMids(ctx)
	if err != nil {
		return 0, fmt.Errorf("hyperliquid get price: %w", err)
	}

	priceStr, ok := mids[symbol]
	if !ok {
		return 0, fmt.Errorf("no price for %s", symbol)
	}

	price, _, err := big.NewFloat(0).Parse(priceStr, 10)
	if err != nil {
		return 0, fmt.Errorf("parse price: %w", err)
	}
	f, _ := price.Float64()
	return f, nil
}

func (h *Hyperliquid) AllMids(ctx context.Context) (map[string]string, error) {
	if h.info == nil && h.privateKey == nil {
		if err := h.initLazyReadOnly(); err != nil {
			return nil, err
		}
	} else {
		if err := h.initLazy(); err != nil {
			return nil, err
		}
	}
	return h.info.AllMids(ctx)
}

func (h *Hyperliquid) Connect() error {
	// Hyperliquid is a stateless HTTP API — no persistent connection needed.
	// We lazy-init the exchange client on first operation.
	return nil
}

func (h *Hyperliquid) Close() error {
	// No persistent connection to close.
	// Clear private key reference for defense-in-depth.
	h.privateKey = nil
	return nil
}

func (h *Hyperliquid) SubscribeOrders(callback exchange.OrderCallback) error {
	return nil // handled by WebSocket manager
}

// stripQuoteCurrency removes USDC/USDT suffix from a symbol for Hyperliquid.
// Hyperliquid uses bare coin names (e.g., "NEAR") not "NEARUSDC".
func stripQuoteCurrency(symbol string) string {
	s := strings.ToUpper(symbol)
	for _, suffix := range []string{"USDC", "USDT", "USD"} {
		if strings.HasSuffix(s, suffix) {
			return strings.TrimSuffix(s, suffix)
		}
	}
	return s
}

// AssetPosition represents a position in the clearinghouse state.
type AssetPosition struct {
	Coin          string
	Szi           string
	EntryPx       string
	UnrealizedPnl string
	Leverage      int
}

// ClearinghouseState represents the user's account state.
type ClearinghouseState struct {
	AccountValue   string
	TotalNtlPos    string
	AssetPositions []AssetPosition
}

// MarketClose closes the entire position for a given coin.
// Hyperliquid-specific extension beyond the exchange.Exchange interface.
func (h *Hyperliquid) MarketClose(ctx context.Context, coin string, sz *float64, px *float64, slippage float64, cloid *string) (hyperliquid.OrderStatus, error) {
	if err := h.initLazy(); err != nil {
		return hyperliquid.OrderStatus{}, err
	}
	if h.exch == nil {
		return hyperliquid.OrderStatus{}, fmt.Errorf("hyperliquid exchange client not initialized")
	}
	return h.exch.MarketClose(ctx, coin, sz, px, slippage, cloid, nil)
}

// GetClearinghouseState returns the user's account state.
func (h *Hyperliquid) GetClearinghouseState() (*ClearinghouseState, error) {
	if err := h.initLazyReadOnly(); err != nil {
		return nil, err
	}
	ctx := context.Background()
	userState, err := h.info.UserState(ctx, h.accountAddr)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid get state: %w", err)
	}

	result := &ClearinghouseState{
		AccountValue: userState.MarginSummary.AccountValue,
		TotalNtlPos:  userState.MarginSummary.TotalNtlPos,
	}
	for _, ap := range userState.AssetPositions {
		entryPx := ""
		if ap.Position.EntryPx != nil {
			entryPx = *ap.Position.EntryPx
		}
		result.AssetPositions = append(result.AssetPositions, AssetPosition{
			Coin:          ap.Position.Coin,
			Szi:           ap.Position.Szi,
			EntryPx:       entryPx,
			UnrealizedPnl: ap.Position.UnrealizedPnl,
			Leverage:      ap.Position.Leverage.Value,
		})
	}
	return result, nil
}

// GetPositions returns all current positions on Hyperliquid.
func (h *Hyperliquid) GetPositions() ([]exchange.PositionInfo, error) {
	state, err := h.GetClearinghouseState()
	if err != nil {
		return nil, fmt.Errorf("get clearinghouse state: %w", err)
	}

	var result []exchange.PositionInfo
	for _, ap := range state.AssetPositions {
		szi, err := strconv.ParseFloat(ap.Szi, 64)
		if err != nil || szi == 0 {
			continue // Skip invalid or zero positions
		}

		entryPrice, _ := strconv.ParseFloat(ap.EntryPx, 64)
		unrealizedPnl, _ := strconv.ParseFloat(ap.UnrealizedPnl, 64)
		symbol := ap.Coin + "USDC"

		// Get market price for this coin (Hyperliquid uses bare coin names in AllMids)
		coinName := stripQuoteCurrency(symbol)
		markPrice, err := h.GetPrice(coinName)
		if err != nil {
			log.Printf("Hyperliquid GetPositions: GetPrice(%s) failed: %v", coinName, err)
		}

		result = append(result, exchange.MakePositionInfo(
			symbol, szi, entryPrice, markPrice, unrealizedPnl, ap.Leverage,
		))
	}

	return result, nil
}
