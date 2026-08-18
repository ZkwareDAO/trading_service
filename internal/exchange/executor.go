package exchange

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

// rpcClient is the interface for notifying user_order_service of status changes.
type rpcClient interface {
	UpdateUserOrderStatusFILLED(ctx context.Context, orderID uint64) error
	UpdateUserOrderStatusFailed(ctx context.Context, orderID uint64) error
	QueryOrderPositionMetadata(ctx context.Context, userOrderID uint64) (*rpc.QueryOrderPositionMetadataResponse, error)
}

type ruleStatusUpdater interface {
	UpdateRuleStatus(id int, status string) error
	ResetRulesForStrategy(strategyID uint64) error
}

// OrderExecutor handles order execution with running order and position persistence.
type OrderExecutor struct {
	repo        *persistence.StateRepository
	exchange    Exchange
	rpc         rpcClient // nil if RPC not configured
	ruleUpdater ruleStatusUpdater
	notifier    notification.Notifier

	pendingOrders map[uint64]*order.UprunningOrder // exchangeOrderID → UprunningOrder
	pendingMu     sync.Mutex
}

// NewOrderExecutor creates a new order executor.
func NewOrderExecutor(repo *persistence.StateRepository, ex Exchange) *OrderExecutor {
	return &OrderExecutor{
		repo:          repo,
		exchange:      ex,
		pendingOrders: make(map[uint64]*order.UprunningOrder),
	}
}

// SetRPCClient sets the RPC client for notifying user_order_service.
func (e *OrderExecutor) SetRPCClient(client rpcClient) {
	e.rpc = client
}

func (e *OrderExecutor) SetRuleStatusUpdater(updater ruleStatusUpdater) {
	e.ruleUpdater = updater
}

func (e *OrderExecutor) SetNotifier(notifier notification.Notifier) {
	e.notifier = notifier
}

// FindPendingOrderByExchangeID looks up a pending order by exchange order ID from the in-memory cache.
func (e *OrderExecutor) FindPendingOrderByExchangeID(exchangeOrderID uint64) *order.UprunningOrder {
	e.pendingMu.Lock()
	defer e.pendingMu.Unlock()
	return e.pendingOrders[exchangeOrderID]
}

// CreateRunningOrder creates a running order record (without calling exchange).
func (e *OrderExecutor) CreateRunningOrder(uo *order.UprunningOrder) uint64 {
	now := time.Now()
	uo.CreatedAt = now
	uo.UpdatedAt = now
	uo.ExchangeOrderStatus = "NEW"
	return e.repo.CreateUprunningOrder(uo)
}

// CreateOrder creates a running order AND calls the exchange.
func (e *OrderExecutor) CreateOrder(uo *order.UprunningOrder, side OrderSide, orderType OrderType, positionSide PositionSide) uint64 {
	now := time.Now()
	uo.CreatedAt = now
	uo.UpdatedAt = now
	uo.ExchangeOrderStatus = "NEW"
	uoID := e.repo.CreateUprunningOrder(uo)

	exReq := CreateOrderRequest{
		Symbol:       uo.Symbol,
		Side:         side,
		OrderType:    orderType,
		Quantity:     uo.ExchangeOrderQty,
		Price:        uo.ExchangeOrderPrice,
		PositionSide: positionSide,
		UserID:       uo.UserID,
		RelationID:   uo.RelationID,
		RelationType: uo.RelationType,
	}

	exResp, err := e.exchange.CreateOrder(exReq)
	if err != nil {
		return uoID // still return local ID even if exchange fails
	}

	// Cache in memory immediately so WS handler can find it before DB update completes
	uo.ExchangeOrderID = exResp.OrderID
	e.pendingMu.Lock()
	e.pendingOrders[exResp.OrderID] = uo
	e.pendingMu.Unlock()

	// Set exchange order ID back on running order
	uo.ExchangeOrderID = exResp.OrderID
	uo.ExchangeOrderStatus = string(exResp.Status)
	uo.ExchangeOrderPrice = exResp.Price
	uo.ExchangeOrderQty = exResp.Quantity
	uo.ExchangeUpdateTime = &now
	uo.UpdatedAt = now
	// Re-save with exchange order ID
	e.repo.UpdateUprunningOrder(uo)

	return uoID
}

// HandleOrderFilled processes a FILLED order update.
func (e *OrderExecutor) HandleOrderFilled(update *OrderUpdate) error {
	// Reload user strategies from CSV to pick up newly created strategies
	if err := e.repo.ReloadUserStrategies(); err != nil {
		log.Printf("warn: reload user strategies: %v", err)
	}

	log.Printf("order executor: processing FILLED order %d (symbol=%s, avgPrice=%.8f, execQty=%.8f)",
		update.OrderID, update.Symbol, update.AvgPrice, update.ExecutedQty)

	updateTime := time.Now()
	if err := e.repo.UpdateUprunningOrderFilled(update.OrderID, update.AvgPrice, update.ExecutedQty, &updateTime); err != nil {
		return fmt.Errorf("update running order filled: %w", err)
	}

	uo, err := e.repo.GetUprunningOrderByID(update.OrderID)
	if err != nil {
		return fmt.Errorf("get uprunning order %d: %w", update.OrderID, err)
	}

	switch uo.RelationType {
	case order.RelationTypeUserOrders:
		return e.handleUserOrderFilled(update, uo, updateTime)
	case order.RelationTypeRiskControlStrategy:
		return e.handleRiskControlStrategyFilled(update, uo, updateTime)
	default:
		return e.handleLegacyCloseFilled(update, updateTime)
	}
}

func (e *OrderExecutor) handleUserOrderFilled(update *OrderUpdate, uo *order.UprunningOrder, updateTime time.Time) error {
	log.Printf("order executor: handling user_order FILLED order %d (relationID=%d)", update.OrderID, update.RelationID)

	metadata, err := e.queryPositionMetadata(update.RelationID)
	if err != nil {
		return err
	}
	entryPrice := update.AvgPrice
	if entryPrice <= 0 {
		entryPrice = metadata.FallbackPrice
	}
	if entryPrice <= 0 {
		return fmt.Errorf("missing entry price for user_order %d", update.RelationID)
	}
	leverage := normalizeLeverage(metadata.Leverage)
	posValue := entryPrice * update.ExecutedQty

	position := &order.UserOrderPosition{
		UserID:           update.UserID,
		UprunningOrderID: update.OrderID,
		UserOrderID:      update.RelationID,
		UserStrategyID:   metadata.UserStrategyID,
		Exchange:         uo.Exchange,
		PosType:          update.PosType,
		Asset:            update.Symbol,
		CurrentPrice:     entryPrice,
		Quantity:         update.ExecutedQty,
		PosValue:         posValue,
		Leverage:         leverage,
		InitMargin:       calculateInitialMargin(posValue),
		PosPrice:         entryPrice,
		Side:             mapPositionSide(update.PositionSide),
		Deleted:          0,
		CreatedAt:        updateTime,
		UpdatedAt:        updateTime,
	}

	positionID, created, err := e.repo.CreateUserOrderPositionIfAbsent(position)
	if err != nil {
		return err
	}

	if created {
		log.Printf("order executor: created user_order_position for order %d (userOrderID=%d, strategyID=%d, entryPrice=%.8f, quantity=%.8f)",
			update.OrderID, update.RelationID, metadata.UserStrategyID, entryPrice, update.ExecutedQty)
		e.sendOpenFilledNotification(update, uo, metadata.UserStrategyID, entryPrice)
	} else {
		log.Printf("order executor: position already exists for order %d (positionID=%d), skipping notification", update.OrderID, positionID)
	}

	// Notify user_order_service via RPC with retries
	if e.rpc != nil {
		go e.notifyUOSFilled(update.RelationID)
	}

	return nil
}

func (e *OrderExecutor) handleRiskControlStrategyFilled(update *OrderUpdate, uo *order.UprunningOrder, updateTime time.Time) error {
	log.Printf("order executor: handling risk_control_strategy FILLED order %d (riskCtrlStratID=%d, userOrderPositionID=%d, userPositionID=%d)",
		update.OrderID, uo.RiskCtrlStratID, uo.UserOrderPositionID, uo.UserPositionID)

	if uo.RiskCtrlStratID == 0 {
		return fmt.Errorf("risk_control_strategy uprunning_order %d missing risk_control_strategy_id", uo.ID)
	}
	if uo.UserOrderPositionID == 0 {
		return fmt.Errorf("risk_control_strategy uprunning_order %d missing user_order_position_id", uo.ID)
	}
	if uo.UserPositionID == 0 {
		return fmt.Errorf("risk_control_strategy uprunning_order %d missing user_position_id", uo.ID)
	}
	orderPosition, err := e.repo.GetUserOrderPositionByID(uo.UserOrderPositionID)
	if err != nil {
		return err
	}
	userPosition, err := e.repo.GetUserPositionByID(uo.UserPositionID)
	if err != nil {
		return err
	}
	if _, err := e.repo.CloseAndCreateRemainingUserOrderPosition(uo.UserOrderPositionID, update.ExecutedQty, uo.RiskCtrlStratID, updateTime); err != nil {
		return err
	}
	if _, err := e.repo.CloseAndCreateRemainingUserPosition(uo.UserPositionID, update.ExecutedQty, uo.RiskCtrlStratID, updateTime); err != nil {
		return err
	}
	log.Printf("order executor: closed user_position for order %d (userPositionID=%d, riskCtrlStratID=%d, execQty=%.8f)",
		update.OrderID, uo.UserPositionID, uo.RiskCtrlStratID, update.ExecutedQty)

	e.sendRiskCloseFilledNotification(update, uo, userPosition, orderPosition)
	if e.ruleUpdater != nil {
		pos, err := e.repo.GetUserOrderPositionByID(uo.UserOrderPositionID)
		if err == nil && pos != nil {
			_ = e.ruleUpdater.ResetRulesForStrategy(pos.UserStrategyID)
		}
	}
	return nil
}

func (e *OrderExecutor) sendOpenFilledNotification(update *OrderUpdate, uo *order.UprunningOrder, strategyID uint64, entryPrice float64) {
	if e.notifier == nil {
		return
	}
	userName := e.lookupUserName(uo.UserID)
	strategyName := e.lookupStrategyName(strategyID)
	if err := e.notifier.SendOpenOrder(&notification.OpenOrderMessage{
		UserName:     userName,
		EventName:    "FutureOrder",
		Symbol:       uo.Symbol,
		StrategyName: strategyName,
		Side:         notification.GetOpenOrderSide(uo.Side == order.SideLong),
		Price:        entryPrice,
		Quantity:     update.ExecutedQty,
	}); err != nil {
		log.Printf("warn: send open filled notification failed: %v", err)
	}
}

func (e *OrderExecutor) sendRiskCloseFilledNotification(update *OrderUpdate, uo *order.UprunningOrder, userPosition *order.UserPosition, orderPosition *order.UserOrderPosition) {
	if e.notifier == nil {
		return
	}
	userName := e.lookupUserName(uo.UserID)
	strategyName := e.lookupStrategyName(userPosition.UserStrategyID)
	if err := e.notifier.SendCloseOrder(&notification.CloseOrderMessage{
		UserName:         userName,
		EventName:        "新风控下单",
		Symbol:           uo.Symbol,
		StrategyName:     strategyName,
		Side:             notification.GetCloseOrderSide(orderPosition.Side == order.SideLong),
		Price:            update.AvgPrice,
		Quantity:         update.ExecutedQty,
		Profit:           userPosition.PnL,
		ProfitPercentage: userPosition.ROI,
	}); err != nil {
		log.Printf("warn: send risk close filled notification failed: %v", err)
	}
}

func (e *OrderExecutor) lookupUserName(userID uint64) string {
	user, err := e.repo.GetUserByID(userID)
	if err != nil || user == nil || user.Name == "" {
		return fmt.Sprintf("user_%d", userID)
	}
	return user.Name
}

func (e *OrderExecutor) lookupStrategyName(strategyID uint64) string {
	strategy, err := e.repo.GetUserStrategyByID(strategyID)
	if err != nil || strategy == nil || strategy.Name == "" {
		return fmt.Sprintf("strategy_%d", strategyID)
	}
	return strategy.Name
}

func (e *OrderExecutor) handleLegacyCloseFilled(update *OrderUpdate, updateTime time.Time) error {
	positions := e.repo.ListActivePositions()
	for _, pos := range positions {
		if pos.Asset == update.Symbol {
			return e.repo.ClosePosition(pos.ID, updateTime)
		}
	}
	return nil
}

// HandleOrderStatusUpdate processes a non-FILLED status update (e.g. CANCELLED, PARTIAL).
func (e *OrderExecutor) HandleOrderStatusUpdate(orderID uint64, status string, avgPrice, executedQty float64, updateTime *time.Time) error {
	if updateTime == nil {
		t := time.Now()
		updateTime = &t
	}

	// Look up relation type BEFORE updating status
	uo, err := e.repo.GetUprunningOrderByID(orderID)
	if err != nil {
		return fmt.Errorf("get uprunning order %d: %w", orderID, err)
	}

	if err := e.repo.UpdateUprunningOrderStatus(orderID, status, updateTime); err != nil {
		return fmt.Errorf("update running order status: %w", err)
	}

	if (status == "CANCELLED" || status == "CANCELED" || status == "FAILED") && uo.RelationType == order.RelationTypeRiskControlStrategy && e.ruleUpdater != nil {
		if err := e.ruleUpdater.UpdateRuleStatus(int(uo.RiskCtrlStratID), "active"); err != nil {
			return err
		}
	}

	// CANCELLED on user_orders → RPC update to FAILED
	if status == "CANCELLED" && uo.RelationType == order.RelationTypeUserOrders && e.rpc != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := e.rpc.UpdateUserOrderStatusFailed(ctx, uo.RelationID); err != nil {
				log.Printf("order executor: RPC update user_order %d CANCELLED failed: %v", uo.RelationID, err)
			}
		}()
	}

	return nil
}

// CancelOrder cancels an order on the exchange.
func (e *OrderExecutor) CancelOrder(orderID uint64) error {
	if err := e.exchange.CancelOrder(orderID); err != nil {
		return fmt.Errorf("exchange cancel order: %w", err)
	}
	updateTime := time.Now()
	return e.repo.UpdateUprunningOrderStatus(orderID, "CANCELLED", &updateTime)
}

func (e *OrderExecutor) queryPositionMetadata(userOrderID uint64) (*rpc.QueryOrderPositionMetadataResponse, error) {
	if e.rpc == nil {
		return nil, fmt.Errorf("metadata rpc client is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	metadata, err := e.rpc.QueryOrderPositionMetadata(ctx, userOrderID)
	if err != nil {
		return nil, fmt.Errorf("query position metadata for user_order %d: %w", userOrderID, err)
	}
	if metadata.UserStrategyID == 0 {
		return nil, fmt.Errorf("metadata for user_order %d missing user_strategy_id", userOrderID)
	}
	if metadata.Leverage <= 0 {
		return nil, fmt.Errorf("metadata for user_order %d missing leverage", userOrderID)
	}
	return metadata, nil
}

func normalizeLeverage(leverage int) int {
	if leverage > 0 {
		return leverage
	}
	return 1
}

func calculateInitialMargin(posValue float64) float64 {
	return posValue
}
func mapPositionSide(side string) order.Side {
	if side == "LONG" || side == "0" {
		return order.SideLong
	}
	return order.SideShort
}

// OrderUpdate represents a WebSocket order status update.
type OrderUpdate struct {
	OrderID      uint64
	Symbol       string
	Status       string
	AvgPrice     float64
	ExecutedQty  float64
	PositionSide string
	UserID       uint64
	PosType      order.PosType
	RelationID   uint64
}

// notifyUOSFilled retries RPC notification to UOS with 3 attempts and reloads strategies on success.
func (e *OrderExecutor) notifyUOSFilled(userOrderID uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	for attempt := 0; attempt < 3; attempt++ {
		if err := e.rpc.UpdateUserOrderStatusFILLED(ctx, userOrderID); err == nil {
			if err := e.repo.ReloadUserStrategies(); err != nil {
				log.Printf("order executor: failed to reload user_strategies: %v", err)
			}
			return
		}
		if attempt < 2 {
			time.Sleep(time.Second)
		}
	}
	log.Printf("order executor: RPC update user_order %d FILLED failed after 3 retries", userOrderID)
}
