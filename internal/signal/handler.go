package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk/config"
	"trading-service/internal/rpc"
)

const (
	pollInterval   = 10 * time.Second
	pollIterations = 10

	// Order type constants (shared convention across codebase)
	orderTypeLimit  = 0
	orderTypeMarket = 1

	// Exchange name constants (avoid typos, improve maintainability)
	ExchangeDeribit     = "deribit"
	ExchangeBinance     = "binance"
	ExchangeHyperliquid = "hyperliquid"
	ExchangeMock        = "mock"
)

// Signal represents a trading signal.
type Signal struct {
	UserID         uint64
	Symbol         string
	UserStrategyID uint64
	PosType        int
	Exchange       string
	Cash           float64
	Quantity       float64
	TriggerPrice   float64
	Slippage       float64
	Side           int
	OrderType      int
	ValidBefore    time.Time
	Leverage       int
}

type PositionQueryClient interface {
	QueryUserOrderPositions(ctx context.Context, req rpc.QueryUserOrderPositionsRequest) (*rpc.QueryUserOrderPositionsResponse, error)
	GetMarketPrice(ctx context.Context, req rpc.GetMarketPriceRequest) (*rpc.GetMarketPriceResponse, error)
	CreateUprunningOrder(ctx context.Context, req rpc.CreateUprunningOrderRequest) (*rpc.CreateUprunningOrderResponse, error)
	CreateRule(ctx context.Context, req rpc.CreateRuleRequest) (*rpc.CreateRuleResponse, error)
	InvalidateRulesForStrategy(ctx context.Context, strategyID uint64) error
}

// Handler processes trading signals.
type Handler struct {
	Repo                *persistence.StateRepository
	closeRuleWriter     *CloseRuleWriter
	positionQueryClient PositionQueryClient
	notifier            notification.Notifier
	testnetBinance      bool
	testnetHyperliquid  bool
	testnetDeribit      bool
	missingOrderLogger  *persistence.MissingOrderLogger // 专门记录 RPC 失败导致的缺失订单
}

// NewHandler creates a new signal handler (deprecated: use NewHandlerWithDataDirAndTestnetConfig).
//
// Deprecated: This function hardcodes testnet flags to false (production mode).
// It exists only for backward compatibility with existing test code that uses mock exchanges.
// For production use, call NewHandlerWithDataDirAndTestnetConfig with proper testnet configuration.
func NewHandler(repo *persistence.StateRepository, ex exchange.Exchange) *Handler {
	return NewHandlerWithTestnetConfig(repo, false, false, false)
}

// NewHandlerWithTestnetConfig creates a signal handler with testnet configuration.
func NewHandlerWithTestnetConfig(repo *persistence.StateRepository, testnetBinance, testnetHyperliquid, testnetDeribit bool) *Handler {
	return &Handler{Repo: repo, testnetBinance: testnetBinance, testnetHyperliquid: testnetHyperliquid, testnetDeribit: testnetDeribit}
}

// NewHandlerWithDataDir creates a signal handler with data directory (deprecated).
//
// Deprecated: This function hardcodes testnet flags to false (production mode).
// It exists only for backward compatibility with existing test code that uses mock exchanges.
// For production use, call NewHandlerWithDataDirAndTestnetConfig with proper testnet configuration.
func NewHandlerWithDataDir(repo *persistence.StateRepository, ex exchange.Exchange, dataDir string) *Handler {
	return NewHandlerWithDataDirAndTestnetConfig(repo, dataDir, false, false, false, nil, nil)
}

// NewHandlerWithDataDirAndPositionClient creates a signal handler with position client (deprecated).
//
// Deprecated: This function hardcodes testnet flags to false (production mode).
// It exists only for backward compatibility with existing test code that uses mock exchanges.
// For production use, call NewHandlerWithDataDirAndTestnetConfig with proper testnet configuration.
func NewHandlerWithDataDirAndPositionClient(repo *persistence.StateRepository, factory *exchange.ExchangeFactory, dataDir string, client PositionQueryClient, notifier notification.Notifier) *Handler {
	return NewHandlerWithDataDirAndTestnetConfig(repo, dataDir, false, false, false, client, notifier)
}

func NewHandlerWithDataDirAndTestnetConfig(repo *persistence.StateRepository, dataDir string, testnetBinance, testnetHyperliquid, testnetDeribit bool, client PositionQueryClient, notifier notification.Notifier) *Handler {
	// Create missing order logger for RPC failures
	missingLogger, err := persistence.NewMissingOrderLogger(dataDir)
	if err != nil {
		log.Printf("Warning: failed to create missing order logger: %v", err)
	}

	// Use RPC mode for CloseRuleWriter (recommended)
	// PMS centrally manages all rule.csv writes
	var closeRuleWriter *CloseRuleWriter
	if rpcClient, ok := client.(*rpc.OrderServiceClient); ok {
		closeRuleWriter = NewCloseRuleWriterWithRPC(rpcClient)
		log.Printf("Handler: using RPC mode for CloseRuleWriter (PMS manages rule.csv)")
	} else {
		// Fallback to direct mode if client is not *rpc.OrderServiceClient
		// This should not happen in production
		log.Printf("Warning: client is not *rpc.OrderServiceClient, falling back to direct mode")
		ruleStore, err := config.NewRuleStore(dataDir)
		if err != nil {
			log.Printf("Warning: failed to create RuleStore, closeRuleWriter will be nil: %v", err)
		} else {
			closeRuleWriter = NewCloseRuleWriterWithStore(ruleStore)
		}
	}

	return &Handler{
		Repo:                repo,
		closeRuleWriter:     closeRuleWriter,
		positionQueryClient: client,
		notifier:            notifier,
		testnetBinance:      testnetBinance,
		testnetHyperliquid:  testnetHyperliquid,
		testnetDeribit:      testnetDeribit,
		missingOrderLogger:  missingLogger,
	}
}

// NewHandlerWithRuleStore creates a signal handler with RuleStore for unified rule management.
// This is the recommended constructor for production use.
func NewHandlerWithRuleStore(repo *persistence.StateRepository, ruleStore *config.RuleStore, testnetBinance, testnetHyperliquid, testnetDeribit bool, client PositionQueryClient, notifier notification.Notifier) *Handler {
	return &Handler{
		Repo:                repo,
		closeRuleWriter:     NewCloseRuleWriterWithStore(ruleStore),
		positionQueryClient: client,
		notifier:            notifier,
		testnetBinance:      testnetBinance,
		testnetHyperliquid:  testnetHyperliquid,
		testnetDeribit:      testnetDeribit,
	}
}

// InitFactoryForTest creates a mock exchange for testing (deprecated: use NewHandlerWithTestnetConfig).
func InitFactoryForTest(ex exchange.Exchange) *exchange.ExchangeFactory {
	f := exchange.NewExchangeFactory()
	f.Register(ExchangeMock, ex)
	return f
}

// createExchangeForUser dynamically creates an exchange instance with user credentials.
// This ensures each order uses the correct user's API keys.
func (h *Handler) createExchangeForUser(user *order.User) (exchange.Exchange, error) {
	switch user.Exchange {
	case ExchangeMock:
		return exchange.NewMockExchange(), nil
	case ExchangeBinance:
		if user.APIKey == "" || user.APISecret == "" {
			return nil, fmt.Errorf("user %d missing binance credentials", user.ID)
		}
		return NewBinanceFuturesAdapterWithFilters(user.APIKey, user.APISecret, h.testnetBinance, h.Repo), nil
	case ExchangeHyperliquid:
		if user.APISecret == "" {
			return nil, fmt.Errorf("user %d missing hyperliquid private key", user.ID)
		}
		return NewHyperliquidAdapter(user.APISecret, user.APIKey, h.testnetHyperliquid)
	case ExchangeDeribit:
		if user.APIKey == "" || user.APISecret == "" || user.APIPassword == "" {
			return nil, fmt.Errorf("user %d missing deribit credentials", user.ID)
		}
		return NewDeribitAdapter(user.APIKey, user.APISecret, user.APIPassword, h.testnetDeribit)
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", user.Exchange)
	}
}

// HandleSignal processes a single normalized trading signal.
func (h *Handler) HandleSignal(sig Signal) error {
	log.Printf("Signal received: userID=%d, strategyID=%d, exchange=%s, symbol=%s, side=%d, orderType=%d, cash=%.2f, triggerPrice=%.4f, slippage=%.4f",
		sig.UserID, sig.UserStrategyID, sig.Exchange, sig.Symbol, sig.Side, sig.OrderType, sig.Cash, sig.TriggerPrice, sig.Slippage)

	err := h.HandleOpen(sig)
	if err != nil {
		log.Printf("Signal processing failed: %v", err)
	} else {
		log.Printf("Signal processed successfully: userID=%d, strategyID=%d, symbol=%s", sig.UserID, sig.UserStrategyID, sig.Symbol)
	}
	return err
}

// HandleStrategySignal processes the nested strategy signal payload emitted upstream.
// Returns the user_strategy_id and any error.
func (h *Handler) HandleStrategySignal(msg StrategySignal) (uint64, error) {
	log.Printf("Strategy signal received: userID=%d, symbol=%s, exchange=%s",
		msg.UserID, msg.Symbol, msg.Signal.Exchange)

	if err := h.validateStrategySignal(&msg); err != nil {
		log.Printf("Strategy signal validation failed: %v", err)
		return 0, err
	}

	// 保存原始symbol用于ExtractStrategyName（在NormalizeSymbol之前）
	originalSymbol := msg.Symbol
	msg.NormalizeSymbol()

	// 使用原始symbol调用resolveUserStrategy（修复agent订单策略名称问题）
	us, err := h.resolveUserStrategyWithSymbol(&msg, originalSymbol)
	if err != nil {
		return 0, err
	}

	sig := Signal{
		UserID:         msg.UserID,
		UserStrategyID: us.ID,
		Symbol:         msg.Symbol,
		PosType:        int(msg.PosType),
		Exchange:       msg.Signal.Exchange,
		Cash:           msg.Signal.Cash,
		Quantity:       msg.Signal.Quantity,
		TriggerPrice:   msg.Signal.TriggerPrice,
		Slippage:       msg.Signal.Slippage,
		OrderType:      msg.Signal.OrderType,
		ValidBefore:    msg.Signal.ValidBefore.Time,
		Leverage:       msg.Strategy.Leverage,
	}

	action := msg.Signal.Action
	log.Printf("Strategy signal action: %v, userID=%d, strategyID=%d", action, msg.UserID, us.ID)

	switch {
	case action.IsOpenAction():
		side, _ := action.GetOpenSide()
		sig.Side = int(side)
		log.Printf("Routing to OPEN handler: userID=%d, strategyID=%d, symbol=%s, side=%d", sig.UserID, sig.UserStrategyID, sig.Symbol, sig.Side)
		return us.ID, h.HandleOpen(sig)
	case action.IsCloseAction():
		side, _ := action.GetCloseSide()
		sig.Side = int(side)
		log.Printf("Routing to CLOSE handler: userID=%d, strategyID=%d, symbol=%s, side=%d", sig.UserID, sig.UserStrategyID, sig.Symbol, sig.Side)
		return us.ID, h.HandleCloseSignal(sig)
	case action.IsReverseAction():
		side, _ := action.GetCloseSide()
		sig.Side = int(side)
		return us.ID, h.HandleReverseSignal(context.Background(), sig, action)
	default:
		return 0, fmt.Errorf("unknown action: %s", action)
	}
}

func (h *Handler) validateStrategySignal(msg *StrategySignal) error {
	if msg.UserID == 0 {
		return fmt.Errorf("user_id is required")
	}
	if msg.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if msg.PosType == 0 {
		return fmt.Errorf("pos_type is required")
	}
	if msg.Strategy.Name == "" {
		return fmt.Errorf("strategy.name is required")
	}
	if msg.Signal.Exchange == "" {
		return fmt.Errorf("signal.exchange is required")
	}
	if msg.Signal.Action == "" {
		return fmt.Errorf("signal.action is required")
	}
	// Deribit-specific validation: quantity is required
	if msg.Signal.Exchange == ExchangeDeribit {
		if msg.Signal.Quantity == 0 {
			return fmt.Errorf("quantity is required for deribit options")
		}
	} else {
		// Non-Deribit exchanges: allow cash or quantity, but at least one must be non-zero
		if msg.Signal.Cash == 0 && msg.Signal.Quantity == 0 {
			return fmt.Errorf("cash and quantity cannot both be zero")
		}
	}
	now := time.Now()
	if !msg.Strategy.ValidBefore.IsZero() && !msg.Strategy.ValidBefore.After(now) {
		return fmt.Errorf("strategy expired")
	}
	if !msg.Signal.ValidBefore.IsZero() && !msg.Signal.ValidBefore.After(now) {
		return fmt.Errorf("signal expired")
	}
	return nil
}

func (h *Handler) resolveUserStrategy(msg *StrategySignal) (*order.UserStrategy, error) {
	return h.resolveUserStrategyWithSymbol(msg, msg.Symbol)
}

func (h *Handler) resolveUserStrategyWithSymbol(msg *StrategySignal, originalSymbol string) (*order.UserStrategy, error) {
	if _, err := h.Repo.GetUserByID(msg.UserID); err != nil {
		return nil, fmt.Errorf("user_id %d not found: %w", msg.UserID, err)
	}

	keyName := msg.Strategy.ExtractStrategyName(originalSymbol, msg.Signal.Exchange)
	now := time.Now()

	// Sanitize params before persistence: positive StopLossThreshold → negative
	if msg.Strategy.Params != nil {
		sanitizeStrategyParams(msg.Strategy.Params)
	}

	params, err := json.Marshal(msg.Strategy.Params)
	if err != nil {
		return nil, fmt.Errorf("marshal strategy params: %w", err)
	}

	strategy, err := h.Repo.GetStrategyByName(keyName)
	if err != nil {
		strategy = &order.Strategy{
			Name:         keyName,
			StrategyType: msg.StrategyType,
			Description:  msg.Strategy.Description,
			Params:       string(params),
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		strategy.ID = h.Repo.CreateStrategy(strategy)
	} else if strategy.Params != string(params) || strategy.Description != msg.Strategy.Description || strategy.StrategyType != msg.StrategyType {
		strategy.Params = string(params)
		strategy.Description = msg.Strategy.Description
		strategy.StrategyType = msg.StrategyType
		if err := h.Repo.UpdateStrategy(strategy); err != nil {
			return nil, err
		}
	}

	if _, err := h.Repo.GetStrategyAssetByNameAssetStrategy(keyName, msg.Symbol, strategy.ID); err != nil {
		h.Repo.CreateStrategyAsset(&order.StrategyAsset{
			Name:       keyName,
			Asset:      msg.Symbol,
			StrategyID: strategy.ID,
			PosType:    msg.PosType,
			Sort:       1,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}

	us, err := h.Repo.GetUserStrategyByUserNameStrategy(msg.UserID, keyName, strategy.ID)
	if err != nil {
		us = &order.UserStrategy{
			UserID:           msg.UserID,
			Name:             keyName,
			Exchange:         msg.Signal.Exchange,
			ValidBefore:      msg.Strategy.ValidBefore.Time,
			Cash:             msg.Strategy.Cash,
			Parts:            msg.Strategy.Parts,
			Status:           1,
			StrategyID:       strategy.ID,
			RiskStrategyType: msg.RiskStrategyType,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if us.RiskStrategyType == "" {
			us.RiskStrategyType = order.RiskStrategyTypeTraditional
		}
		us.ID = h.Repo.CreateUserStrategy(us)
		return us, nil
	}

	us.Exchange = msg.Signal.Exchange
	us.ValidBefore = msg.Strategy.ValidBefore.Time
	us.Cash = msg.Strategy.Cash
	us.Parts = msg.Strategy.Parts
	if msg.RiskStrategyType != "" {
		us.RiskStrategyType = msg.RiskStrategyType
	}
	if us.Status == 0 {
		us.Status = 1
	}
	if err := h.Repo.UpdateUserStrategy(us); err != nil {
		return nil, err
	}
	return us, nil
}

// ValidateSignal checks signal fields for opening positions.
// For limit orders, TriggerPrice must be positive.
// For market orders, TriggerPrice can be zero (will use market price).
func ValidateSignal(sig Signal) error {
	if sig.UserID == 0 {
		return fmt.Errorf("UserID is required")
	}
	if sig.UserStrategyID == 0 {
		return fmt.Errorf("UserStrategyID is required")
	}
	if sig.Exchange == "" {
		return fmt.Errorf("Exchange is required")
	}
	if sig.Symbol == "" {
		return fmt.Errorf("Symbol is required")
	}
	// Only require TriggerPrice for limit orders
	if sig.OrderType == orderTypeLimit && sig.TriggerPrice <= 0 {
		return fmt.Errorf("TriggerPrice must be positive for limit orders")
	}
	return nil
}

// HandleOpen opens a new position.
func (h *Handler) HandleOpen(sig Signal) error {
	if err := ValidateSignal(sig); err != nil {
		return fmt.Errorf("invalid signal: %w", err)
	}
	// Deribit options: Quantity is required, Cash can be 0
	// Futures: Cash is required for quantity calculation
	if err := h.validateSignalQuantity(sig); err != nil {
		return err
	}

	// Validate that signal exchange matches user's exchange
	user, err := h.Repo.GetUserByID(sig.UserID)
	if err != nil {
		return fmt.Errorf("user %d not found: %w", sig.UserID, err)
	}
	if user.Exchange != sig.Exchange {
		return fmt.Errorf("signal exchange '%s' does not match user %d exchange '%s'", sig.Exchange, sig.UserID, user.Exchange)
	}

	us, err := h.Repo.GetUserStrategyByID(sig.UserStrategyID)
	if err != nil {
		return fmt.Errorf("user strategy %d not found: %w", sig.UserStrategyID, err)
	}

	// Check if strategy has expired (valid_before < now)
	now := time.Now()
	if !us.ValidBefore.IsZero() && now.After(us.ValidBefore) {
		log.Printf("Strategy %d expired: valid_before=%s, now=%s", us.ID, us.ValidBefore.Format(time.RFC3339), now.Format(time.RFC3339))
		return fmt.Errorf("strategy expired: valid_before passed")
	}

	if us.Status != 1 {
		return fmt.Errorf("strategy %d not active (status=%d)", us.ID, us.Status)
	}
	if sig.Cash > us.Cash {
		return fmt.Errorf("signal cash %.2f exceeds strategy cash %.2f", sig.Cash, us.Cash)
	}
	if err := h.checkPartsLimit(us); err != nil {
		return err
	}

	// Invalidate active rules for this strategy (new position means old rules are stale)
	if h.positionQueryClient != nil {
		if err := h.positionQueryClient.InvalidateRulesForStrategy(context.Background(), sig.UserStrategyID); err != nil {
			log.Printf("Warning: failed to invalidate rules for strategy %d: %v", sig.UserStrategyID, err)
			// Don't fail the order creation, just log the warning
		} else {
			log.Printf("Invalidated active rules for strategy %d", sig.UserStrategyID)
		}
	}

	// For options (pos_type=2), use symbol as-is without adding quote currency suffix
	// Options have special format like "BTC-24JUL26-64000-P" that must not be modified
	var symbol string
	baseAsset := normalizeBaseAsset(sig.Symbol)
	if sig.PosType == int(order.PosTypeOptions) {
		symbol = sig.Symbol
	} else {
		symbol = toExchangeSymbolWithExchange(baseAsset, sig.Exchange)
	}
	leverage := sig.Leverage
	if leverage <= 0 {
		leverage = 1
	}
	log.Printf("[HandleOpen] Using leverage: sig.Leverage=%d, effective leverage=%d, userStrategyID=%d", sig.Leverage, leverage, sig.UserStrategyID)

	// Determine execution price based on order type
	executionPrice := h.determineExecutionPrice(sig, symbol)
	log.Printf("[HandleOpen] Execution price: %.4f, symbol=%s, cash=%.2f, calculated_qty=%.4f", executionPrice, symbol, sig.Cash, order.CalculateCashToQuantity(sig.Cash, executionPrice, leverage))

	// All calculations below remain UNCHANGED
	// 限价单不应用滑点（用户指定的精确价格），仅市价单需要滑点调整
	adjPrice := executionPrice
	if sig.OrderType == orderTypeMarket {
		adjPrice = order.ApplySlippage(executionPrice, sig.Slippage, order.Side(sig.Side))
	}
	qty := h.calculateOrderQuantity(sig, executionPrice, leverage)
	filters := h.Repo.ListExchangeSymbolFilters(sig.Exchange, order.PosType(sig.PosType), symbol)
	log.Printf("[HandleOpen] Before truncation: price=%.4f, qty=%.4f, filters_count=%d", adjPrice, qty, len(filters))
	for i, f := range filters {
		if f != nil {
			log.Printf("[HandleOpen] Filter %d: type=%s, min_qty=%.8f, max_qty=%.8f, step_size=%.8f, tick_size=%.8f",
				i, f.FilterType, f.MinQty, f.MaxQty, f.StepSize, f.TickSize)
		}
	}
	adjPrice, qty = order.TruncateToExchangeSymbolFilters(filters, adjPrice, qty)
	log.Printf("[HandleOpen] After truncation: price=%.4f, qty=%.4f", adjPrice, qty)
	if err := order.VerifyCreateOrderInfo(order.CreateOrderRequest{OrderType: sig.OrderType, Price: adjPrice, Quantity: qty}); err != nil {
		return err
	}
	if err := order.ValidateExchangeSymbolFilters(filters, adjPrice, qty); err != nil {
		return err
	}

	userOrder := &order.UserOrder{
		UserID:         sig.UserID,
		UserStrategyID: sig.UserStrategyID,
		PosType:        order.PosType(sig.PosType),
		Exchange:       sig.Exchange,
		ValidBefore:    sig.ValidBefore,
		BaseAsset:      baseAsset,
		QuoteAsset:     defaultQuoteForExchange(sig.Exchange),
		Cash:           sig.Cash,
		TriggerPrice:   sig.TriggerPrice,
		Slippage:       sig.Slippage,
		Side:           order.Side(sig.Side),
		OrderType:      sig.OrderType,
		Status:         1, // NEW
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	orderID := h.Repo.CreateUserOrder(userOrder)
	log.Printf("Created user order %d for strategy %d (symbol=%s, cash=%.2f, side=%d, orderType=%d)", orderID, us.ID, symbol, sig.Cash, sig.Side, sig.OrderType)

	exReq := exchange.CreateOrderRequest{
		Symbol:       symbol,
		Side:         mapOrderSide(sig.Side),
		OrderType:    mapOrderType(sig.OrderType),
		Quantity:     qty,
		Price:        adjPrice,
		PositionSide: mapPositionSide(sig.Side),
		UserID:       sig.UserID,
		RelationID:   orderID,
		RelationType: order.RelationTypeUserOrders,
	}

		// Create exchange instance with user credentials (dynamic, per-request)
		ex, err := h.createExchangeForUser(user)
		if err != nil {
			// Mark order as FAILED when exchange creation fails
			h.Repo.UpdateUserOrderStatus(orderID, 3, nil, time.Now())
			log.Printf("Create exchange failed for user order %d: %v", orderID, err)
			return fmt.Errorf("create exchange for user %d: %w", sig.UserID, err)
		}

	// Set leverage BEFORE placing the order (for futures)
	if sig.PosType == int(order.PosTypeFutures) {
		log.Printf("[HandleOpen] Setting leverage for futures: symbol=%s, leverage=%d", symbol, leverage)
		if err := ex.SetLeverage(symbol, leverage); err != nil {
			log.Printf("warn: set leverage failed: %v", err)
		} else {
			log.Printf("[HandleOpen] Successfully set leverage to %d for symbol=%s", leverage, symbol)
		}
		lc := &order.LeverageConfig{
			UserID: sig.UserID, Asset: baseAsset, Quote: defaultQuoteForExchange(sig.Exchange),
			Leverage: leverage, Exchange: sig.Exchange, Status: 1,
			PosType:   order.PosType(sig.PosType),
			CreatedAt: now, UpdatedAt: now,
		}
		h.Repo.UpsertLeverageConfig(lc)
		log.Printf("[HandleOpen] Saved leverage config: userID=%d, asset=%s, leverage=%d", sig.UserID, baseAsset, leverage)
	}

	exResp, err := ex.CreateOrder(exReq)
	if err != nil {
		// REST API failed → mark order as FAILED (status=3)
		h.Repo.UpdateUserOrderStatus(orderID, 3, nil, time.Now())
		log.Printf("Exchange order failed for user order %d: %v", orderID, err)
		return fmt.Errorf("exchange create order: %w", err)
	}
	log.Printf("Exchange order created: exchangeOrderID=%d, userOrderID=%d, status=%s, price=%.4f, qty=%.4f",
		exResp.OrderID, orderID, exResp.Status, exResp.Price, exResp.Quantity)

	// Create uprunning_order via RPC to PMS (centralized creation)
	if h.positionQueryClient != nil {
		uprunningOrderID, err := h.createUprunningOrderViaRPC(sig, orderID, exResp, symbol, now)
		if err != nil {
			// 记录到标准日志
			log.Printf("ERROR: 3 次 RPC 重试后创建 uprunning_order 失败: userID=%d, orderID=%d, exchangeOrderID=%d, error=%v",
				sig.UserID, orderID, exResp.OrderID, err)

			// 记录到专门的缺失订单日志文件
			orderData := map[string]interface{}{
				"user_id":              sig.UserID,
				"relation_id":          orderID,
				"relation_type":        order.RelationTypeUserOrders,
				"exchange":             sig.Exchange,
				"symbol":               symbol,
				"pos_type":             sig.PosType,
				"exchange_order_id":    exResp.OrderID,
				"exchange_order_status": string(exResp.Status),
				"exchange_order_price": exResp.Price,
				"exchange_order_qty":   exResp.Quantity,
				"side":                 sig.Side,
			}

			if h.missingOrderLogger != nil {
				if logErr := h.missingOrderLogger.LogMissingOrder(orderData); logErr != nil {
					log.Printf("ERROR: 记录缺失订单到日志文件失败: %v", logErr)
				} else {
					log.Printf("缺失订单已记录到 logs/missing_uprunning_orders.log")
				}
			}
			// 不失败整个订单 - 交易所下单已成功
		} else {
			log.Printf("Created uprunning_order via RPC: uprunningOrderID=%d, userOrderID=%d, exchangeOrderID=%d, exchange=%s, symbol=%s, status=%s",
				uprunningOrderID, orderID, exResp.OrderID, sig.Exchange, symbol, exResp.Status)
		}
	} else {
		// Fallback for testing without RPC client
		uprunningOrderID := h.Repo.CreateUprunningOrder(&order.UprunningOrder{
			UserID:              sig.UserID,
			RelationID:          orderID,
			RelationType:        order.RelationTypeUserOrders,
			Exchange:            sig.Exchange,
			Symbol:              symbol,
			PosType:             order.PosType(sig.PosType),
			ExchangeOrderID:     exResp.OrderID,
			ExchangeOrderStatus: string(exResp.Status),
			ExchangeOrderPrice:  exResp.Price,
			ExchangeOrderQty:    exResp.Quantity,
			ExchangeUpdateTime:  &now,
			Side:                order.Side(sig.Side),
			CreatedAt:           now,
			UpdatedAt:           now,
		})
		log.Printf("Created uprunning_order locally (no RPC client): uprunningOrderID=%d, userOrderID=%d, exchangeOrderID=%d",
			uprunningOrderID, orderID, exResp.OrderID)
	}

	// NOTE: Open-order notifications moved to PMS FILLED handling.
	// Keep this reference block disabled because adjPrice/qty are planned values,
	// not actual fill price/quantity.
	// if h.notifier != nil {
	// 	if err := h.notifier.SendOpenOrder(&notification.OpenOrderMessage{
	// 		Symbol:       symbol,
	// 		StrategyName: us.Name,
	// 		Side:         notification.GetOpenOrderSide(sig.Side == int(order.SideLong)),
	// 		Price:        adjPrice,
	// 		Quantity:     qty,
	// 	}); err != nil {
	// 		log.Printf("warn: send open order notification failed: %v", err)
	// 	}
	// }

	// OrdersNum is no longer manually incremented.
	// It is computed on-the-fly via checkPartsLimit reconciliation.
	return nil
}

func (h *Handler) HandleCloseSignal(sig Signal) error {
	// 1. Verify position exists before creating close rule
	asset := toExchangeSymbolWithExchange(sig.Symbol, sig.Exchange)
	hasPosition, err := h.hasActivePosition(context.Background(), sig.UserStrategyID, sig.Side, asset, sig.PosType)
	if err != nil {
		log.Printf("HandleCloseSignal: FAILED to check position for strategyID=%d, error=%v", sig.UserStrategyID, err)
		return fmt.Errorf("check position: %w", err)
	}

	// 2. Position doesn't exist, log and return success (skip)
	if !hasPosition {
		log.Printf("HandleCloseSignal: SKIP - no active position found for strategyID=%d, side=%d, asset=%s", sig.UserStrategyID, sig.Side, asset)
		return nil
	}

	// 3. Position exists, create close rule
	if h.closeRuleWriter == nil {
		log.Printf("HandleCloseSignal: ERROR - close rule writer is not configured for strategyID=%d", sig.UserStrategyID)
		return fmt.Errorf("close rule writer is not configured")
	}

	log.Printf("HandleCloseSignal: creating close rule for strategyID=%d, side=%d, asset=%s", sig.UserStrategyID, sig.Side, asset)
	ruleID, err := h.closeRuleWriter.AppendImmediateCloseRule(context.Background(), CloseRuleRequest{
		UserStrategyID: sig.UserStrategyID,
		QuantityPct:    1.0,
		OrderType:      orderTypeMarket,
	})
	if err != nil {
		log.Printf("HandleCloseSignal: FAILED to create close rule for strategyID=%d, error=%v", sig.UserStrategyID, err)
		return err
	}

	log.Printf("HandleCloseSignal: SUCCESS created close rule ID=%d for strategyID=%d", ruleID, sig.UserStrategyID)
	return nil
}

func (h *Handler) HandleReverseSignal(ctx context.Context, sig Signal, action Action) error {
	asset := toExchangeSymbolWithExchange(sig.Symbol, sig.Exchange)

	// 1. Check if position exists
	hasPosition, err := h.hasActivePosition(ctx, sig.UserStrategyID, sig.Side, asset, sig.PosType)
	if err != nil {
		log.Printf("HandleReverseSignal: FAILED to check position for strategyID=%d, error=%v", sig.UserStrategyID, err)
		return fmt.Errorf("check position: %w", err)
	}

	// 2. Position doesn't exist, directly open
	if !hasPosition {
		log.Printf("HandleReverseSignal: NO POSITION - proceeding directly to open for strategyID=%d, action=%s", sig.UserStrategyID, action)
		openSide, ok := action.GetOpenSide()
		if !ok {
			return fmt.Errorf("invalid reverse action: %s", action)
		}
		openSig := sig
		openSig.Side = int(openSide)
		return h.HandleOpen(openSig)
	}

	// 3. Position exists, execute close + open flow
	log.Printf("HandleReverseSignal: POSITION EXISTS - closing then opening for strategyID=%d, action=%s", sig.UserStrategyID, action)

	if err := h.HandleCloseSignal(sig); err != nil {
		return fmt.Errorf("reverse close rule: %w", err)
	}
	if err := h.waitForSideClosed(ctx, sig); err != nil {
		return fmt.Errorf("reverse wait: %w", err)
	}
	openSide, ok := action.GetOpenSide()
	if !ok {
		return fmt.Errorf("invalid reverse action: %s", action)
	}
	reverseSig := sig
	reverseSig.Side = int(openSide)
	return h.HandleOpen(reverseSig)
}

// hasActivePosition checks if an active position exists for the given side.
func (h *Handler) hasActivePosition(ctx context.Context, userStrategyID uint64, side int, asset string, posType int) (bool, error) {
	if h.positionQueryClient == nil {
		return false, fmt.Errorf("position query client is not configured")
	}

	active := true
	resp, err := h.positionQueryClient.QueryUserOrderPositions(ctx, rpc.QueryUserOrderPositionsRequest{
		UserStrategyID: userStrategyID,
		Side:           &side,
		Active:         &active,
		Asset:          asset,
		PosType:        &posType,
	})
	if err != nil {
		return false, fmt.Errorf("query positions: %w", err)
	}

	return resp.Count > 0, nil
}

func (h *Handler) waitForSideClosed(ctx context.Context, sig Signal) error {
	if h.positionQueryClient == nil {
		return fmt.Errorf("position query client is not configured")
	}
	active := true
	side := sig.Side
	posType := sig.PosType
	asset := toExchangeSymbolWithExchange(sig.Symbol, sig.Exchange)
	for i := 0; i < pollIterations; i++ {
		resp, err := h.positionQueryClient.QueryUserOrderPositions(ctx, rpc.QueryUserOrderPositionsRequest{
			UserStrategyID: sig.UserStrategyID,
			Side:           &side,
			Active:         &active,
			Asset:          asset,
			PosType:        &posType,
		})
		if err != nil {
			return err
		}
		if resp.Count == 0 {
			return nil
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("timeout waiting for side %d positions closed", sig.Side)
}

// DEPRECATED: The following functions are disabled because they directly close positions
// via exchange API, which is not the intended system flow. The system uses HandleCloseSignal
// and HandleReverseSignal instead, which create close rules for PMS execution.
//
// Disabled functions:
//   - HandleClose(sig Signal) error
//   - HandleCloseAll(sig Signal) error
//   - HandleReverse(sig Signal) error
//
// Use HandleCloseSignal for close operations and HandleReverseSignal for reverse operations.

// waitForPositionClosed polls for position to be closed (deleted=1).
func (h *Handler) waitForPositionClosed(sig Signal) error {
	for i := 0; i < pollIterations; i++ {
		time.Sleep(pollInterval)
		if h.Repo.CountActivePositionsByStrategy(sig.UserStrategyID) == 0 {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for position closed: %d active positions remain",
		h.Repo.CountActivePositionsByStrategy(sig.UserStrategyID))
}

// checkPartsLimit verifies that pending + active positions < parts.
func (h *Handler) checkPartsLimit(us *order.UserStrategy) error {
	if us.Parts <= 0 {
		return nil
	}

	// Query PMS for real-time active positions count via RPC
	// This ensures we get the latest state from PMS memory (e.g., deleted=1 for closed positions)
	active := 0
	if h.positionQueryClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		activeBool := true
		resp, err := h.positionQueryClient.QueryUserOrderPositions(ctx, rpc.QueryUserOrderPositionsRequest{
			UserStrategyID: us.ID,
			Active:         &activeBool, // Only count active positions (deleted=0)
		})
		if err != nil {
			log.Printf("warn: RPC query active positions failed: %v, falling back to local count", err)
			active = h.Repo.CountActivePositionsByStrategy(us.ID)
		} else {
			active = resp.Count
		}
	} else {
		// Fallback to local memory if RPC client not available
		active = h.Repo.CountActivePositionsByStrategy(us.ID)
	}

	pending := h.Repo.CountUserOrdersByStrategyAndStatus(us.ID, 1)
	if pending+active >= us.Parts {
		return fmt.Errorf("strategy %d reached max parts (%d): %d pending + %d active",
			us.ID, us.Parts, pending, active)
	}
	return nil
}

func mapOrderSide(side int) exchange.OrderSide {
	if side == int(order.SideLong) {
		return exchange.OrderSideBuy
	}
	return exchange.OrderSideSell
}

// determineExecutionPrice returns the execution price for an order.
// For market orders, it attempts to fetch real-time price via RPC.
// For limit orders or when RPC fails, it falls back to trigger_price.
func (h *Handler) determineExecutionPrice(sig Signal, symbol string) float64 {
	// Limit orders always use trigger_price
	if sig.OrderType != orderTypeMarket {
		return sig.TriggerPrice
	}

	// Market orders: try RPC first
	if h.positionQueryClient == nil {
		return sig.TriggerPrice
	}

	priceResp, err := h.positionQueryClient.GetMarketPrice(context.Background(), rpc.GetMarketPriceRequest{
		Exchange: sig.Exchange,
		Symbol:   symbol,
	})
	if err != nil {
		// Fallback to trigger_price on RPC failure
		log.Printf("HandleOpen: RPC get market price FAILED for strategyID=%d, exchange=%s, symbol=%s, error=%v, FALLBACK to trigger_price=%.2f",
			sig.UserStrategyID, sig.Exchange, symbol, err, sig.TriggerPrice)
		return sig.TriggerPrice
	}

	// RPC success: use real-time price
	log.Printf("HandleOpen: RPC get market price SUCCESS for strategyID=%d, exchange=%s, symbol=%s, price=%.2f",
		sig.UserStrategyID, sig.Exchange, symbol, priceResp.Price)
	return priceResp.Price
}

// createUprunningOrderViaRPC retries RPC call to create uprunning_order with 3 attempts.
func (h *Handler) createUprunningOrderViaRPC(sig Signal, orderID uint64, exResp *exchange.CreateOrderResponse, symbol string, now time.Time) (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := rpc.CreateUprunningOrderRequest{
		UserID:              sig.UserID,
		RelationID:          orderID,
		RelationType:        order.RelationTypeUserOrders,
		Exchange:            sig.Exchange,
		Symbol:              symbol,
		PosType:             sig.PosType,
		ExchangeOrderID:     exResp.OrderID,
		ExchangeOrderStatus: string(exResp.Status),
		ExchangeOrderPrice:  exResp.Price,
		ExchangeOrderQty:    exResp.Quantity,
		Side:                sig.Side,
	}

	for attempt := 0; attempt < 3; attempt++ {
		resp, err := h.positionQueryClient.CreateUprunningOrder(ctx, req)
		if err == nil {
			return resp.UprunningOrderID, nil
		}
		log.Printf("RPC create uprunning_order attempt %d failed: %v", attempt+1, err)
		if attempt < 2 {
			time.Sleep(time.Second * time.Duration(attempt+1)) // exponential backoff: 1s, 2s
		}
	}
	return 0, fmt.Errorf("RPC create uprunning_order failed after 3 retries")
}

func mapOrderType(orderType int) exchange.OrderType {
	if orderType == orderTypeMarket {
		return exchange.OrderTypeMarket
	}
	return exchange.OrderTypeLimit
}

func mapPositionSide(side int) exchange.PositionSide {
	if side == int(order.SideLong) {
		return exchange.PositionSideLong
	}
	return exchange.PositionSideShort
}

func oppositeSide(side int) int {
	if side == int(order.SideLong) {
		return int(order.SideShort)
	}
	return int(order.SideLong)
}

// sanitizeStrategyParams ensures StopLossThreshold is always negative.
// If the signal provides a positive value, it is negated before persistence.
func sanitizeStrategyParams(params map[string]interface{}) {
	if v, ok := params["StopLossThreshold"]; ok {
		if threshold, ok := v.(float64); ok && threshold > 0 {
			params["StopLossThreshold"] = -threshold
			log.Printf("StopLossThreshold 为正数 %.4f，已自动转为负数 %.4f", threshold, -threshold)
		}
	}
}

// validateSignalQuantity validates that signals have appropriate quantity/cash values
// based on the position type (pos_type).
func (h *Handler) validateSignalQuantity(sig Signal) error {
	// 期权 (pos_type=3) 使用 quantity
	if order.PosType(sig.PosType) == order.PosTypeOptions || sig.Exchange == ExchangeDeribit {
		if sig.Quantity <= 0 {
			return fmt.Errorf("quantity must be positive for options, got %f", sig.Quantity)
		}
		return nil
	}
	// 期货/现货使用 cash
	if sig.Cash <= 0 {
		return fmt.Errorf("cash must be positive, got %f", sig.Cash)
	}
	return nil
}

// calculateOrderQuantity calculates the order quantity based on position type.
// Options use quantity directly; futures calculate from cash/price.
func (h *Handler) calculateOrderQuantity(sig Signal, executionPrice float64, leverage int) float64 {
	// 期权 (pos_type=3) 直接使用 quantity
	if order.PosType(sig.PosType) == order.PosTypeOptions || sig.Exchange == ExchangeDeribit {
		return sig.Quantity
	}
	// 期货/现货从 cash 计算数量
	return order.CalculateCashToQuantity(sig.Cash, executionPrice, leverage)
}
