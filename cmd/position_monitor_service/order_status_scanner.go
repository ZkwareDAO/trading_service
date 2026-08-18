package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

// exchangeResolver resolves a user exchange by name.
type exchangeResolver interface {
	ResolveExchange(userID uint64, name string) (exchange.Exchange, error)
}

// rpcClient abstracts RPC notification to user_order_service.
type rpcClient interface {
	UpdateUserOrderStatusFILLED(ctx context.Context, orderID uint64) error
	UpdateUserOrderStatusFailed(ctx context.Context, orderID uint64) error
	QueryOrderPositionMetadata(ctx context.Context, userOrderID uint64) (*rpc.QueryOrderPositionMetadataResponse, error)
}

// OrderStatusScanner periodically scans NEW uprunning_orders and queries the exchange
// via REST API to detect status changes that WS may have missed.
type OrderStatusScanner struct {
	repo     *persistence.StateRepository
	resolver exchangeResolver
	rules    ruleStatusUpdater
	interval time.Duration
	rpc      rpcClient
	notifier notification.Notifier
}

func NewOrderStatusScanner(
	repo *persistence.StateRepository,
	resolver exchangeResolver,
	rules ruleStatusUpdater,
	interval time.Duration,
	rpc rpcClient,
	notifier notification.Notifier,
) *OrderStatusScanner {
	return &OrderStatusScanner{repo: repo, resolver: resolver, rules: rules, interval: interval, rpc: rpc, notifier: notifier}
}

// Start begins the periodic scan loop.
func (s *OrderStatusScanner) Start(ctx context.Context) {
	if s.interval <= 0 {
		s.interval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		log.Printf("Order status scanner started, interval=%v", s.interval)
		for {
			s.scan()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// scan checks all NEW/open orders against the exchange.
func (s *OrderStatusScanner) scan() {
	// Reload uprunning_orders from disk to pick up WS updates
	if err := s.repo.ReloadUprunningOrders(); err != nil {
		log.Printf("order scanner: reload uprunning_orders failed: %v", err)
	}

	// Scan both NEW (Binance) and open (Hyperliquid) orders for safety
	newOrders := s.repo.ListUprunningOrdersByExchangeStatus("NEW")
	openOrders := s.repo.ListUprunningOrdersByExchangeStatus("open")
	orders := append(newOrders, openOrders...)

	if len(orders) == 0 {
		return
	}

	log.Printf("order scanner: found %d orders to check (NEW=%d, open=%d)", len(orders), len(newOrders), len(openOrders))

	for _, uo := range orders {
		if uo.ExchangeOrderID == 0 {
			continue
		}

		ex, err := s.resolver.ResolveExchange(uo.UserID, uo.Exchange)
		if err != nil {
			log.Printf("order scanner: resolve exchange for user %d/%s: %v", uo.UserID, uo.Exchange, err)
			continue
		}

		info, err := ex.GetOrder(uo.ExchangeOrderID, uo.Symbol)
		if err != nil {
			// Order may not exist yet (just placed) — skip, will retry next scan
			log.Printf("order scanner: GetOrder %d: %v (will retry)", uo.ExchangeOrderID, err)
			continue
		}

		if uo.ExchangeOrderStatus == string(info.Status) {
			continue // no change
		}

		log.Printf("order scanner: order %d status changed: %s → %s", uo.ID, uo.ExchangeOrderStatus, info.Status)

		now := time.Now()
		switch {
		case strings.EqualFold(string(info.Status), "filled"):
			fillQty := resolvedFilledQty(uo, info)
			log.Printf("order scanner: processing FILLED order %d (symbol=%s, price=%.8f, fillQty=%.8f, relationType=%s)",
				uo.ID, uo.Symbol, info.Price, fillQty, uo.RelationType)

			if err := s.repo.UpdateUprunningOrderFilled(uo.ID, info.Price, fillQty, &now); err != nil {
				log.Printf("order scanner: UpdateUprunningOrderFilled %d: %v", uo.ID, err)
				continue
			}
			if err := s.handleFilled(uo, info, fillQty, &now); err != nil {
				log.Printf("order scanner: handle FILLED order %d failed: %v", uo.ID, err)
				continue
			}
		case strings.EqualFold(string(info.Status), "cancelled"), strings.EqualFold(string(info.Status), "failed"):
			log.Printf("order scanner: handling CANCELLED/FAILED status for order %d, relationType=%s", uo.ID, uo.RelationType)
			if err := s.repo.UpdateUprunningOrderStatus(uo.ID, string(info.Status), &now); err != nil {
				log.Printf("order scanner: UpdateUprunningOrderStatus %d: %v", uo.ID, err)
				continue
			}
			log.Printf("order scanner: updated uprunning_order status successfully")

			// Notify user_order_service for user orders
			if uo.RelationType == order.RelationTypeUserOrders {
				log.Printf("order scanner: relationType is user_orders, calling notifyUserOrderStatusFailed(%d)", uo.RelationID)
				s.notifyUserOrderStatusFailed(uo.RelationID)
			} else {
				log.Printf("order scanner: relationType is %s, not calling notifyUserOrderStatusFailed", uo.RelationType)
			}

			// Update rule status for risk control orders
			if uo.RelationType == order.RelationTypeRiskControlStrategy {
				s.updateRuleStatus(uo.RiskCtrlStratID, "active")
			}
		default:
			if err := s.repo.UpdateUprunningOrderStatus(uo.ID, string(info.Status), &now); err != nil {
				log.Printf("order scanner: UpdateUprunningOrderStatus %d: %v", uo.ID, err)
				continue
			}
		}
	}
}

func (s *OrderStatusScanner) handleFilled(uo *order.UprunningOrder, info *exchange.OrderInfo, fillQty float64, updateTime *time.Time) error {
	log.Printf("order scanner: handleFilled start for order %d (relationType=%s, relationID=%d)", uo.ID, uo.RelationType, uo.RelationID)

	// Reload user strategies from CSV to pick up newly created strategies (fallback for WS failure)
	if err := s.repo.ReloadUserStrategies(); err != nil {
		log.Printf("warn: scanner reload user strategies: %v", err)
	}

	switch uo.RelationType {
	case order.RelationTypeUserOrders:
		metadata, err := s.queryPositionMetadata(uo.RelationID)
		if err != nil {
			return err
		}

		// Determine entry price with fallback chain: AvgPrice → Price → FallbackPrice
		entryPrice := info.AvgPrice
		if entryPrice <= 0 {
			entryPrice = info.Price
		}
		if entryPrice <= 0 {
			entryPrice = metadata.FallbackPrice
		}
		if entryPrice <= 0 {
			return fmt.Errorf("missing entry price for user_order %d", uo.RelationID)
		}
		leverage := normalizeLeverage(metadata.Leverage)
		posValue := entryPrice * fillQty

		position := &order.UserOrderPosition{
			UserID:           uo.UserID,
			UprunningOrderID: uo.ID,
			UserOrderID:      uo.RelationID,
			UserStrategyID:   metadata.UserStrategyID,
			Exchange:         uo.Exchange,
			PosType:          uo.PosType,
			Asset:            uo.Symbol,
			CurrentPrice:     entryPrice,
			Quantity:         fillQty,
			PosValue:         posValue,
			Leverage:         leverage,
			InitMargin:       calculateInitialMargin(posValue),
			PosPrice:         entryPrice,
			Side:             uo.Side,
			Deleted:          0,
			CreatedAt:        *updateTime,
			UpdatedAt:        *updateTime,
		}
		positionID, created, err := s.repo.CreateUserOrderPositionIfAbsent(position)
		if err != nil {
			return err
		}

		if created {
			s.sendOpenFilledNotification(uo, metadata.UserStrategyID, entryPrice, fillQty)
			log.Printf("order scanner: created user_order_position for order %d (userOrderID=%d, strategyID=%d, entryPrice=%.8f, quantity=%.8f)",
				uo.ID, uo.RelationID, metadata.UserStrategyID, entryPrice, fillQty)
		} else {
			log.Printf("order scanner: position already exists for order %d (positionID=%d), skipping notification", uo.ID, positionID)
		}

		// Scanner fallback: If WS RPC failed, Scanner must notify UOS to ensure consistency
		if s.rpc != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.rpc.UpdateUserOrderStatusFILLED(ctx, uo.RelationID); err != nil {
					log.Printf("order scanner: RPC update user_order %d FILLED failed: %v", uo.RelationID, err)
				}
			}()
		}
		return nil
	case order.RelationTypeRiskControlStrategy:
		if uo.RiskCtrlStratID == 0 {
			return nil
		}
		orderPosition, err := s.repo.GetUserOrderPositionByID(uo.UserOrderPositionID)
		if err != nil {
			log.Printf("order scanner: get user_order_position %d: %v", uo.UserOrderPositionID, err)
			return nil
		}
		userPosition, err := s.repo.GetUserPositionByID(uo.UserPositionID)
		if err != nil {
			log.Printf("order scanner: get user_position %d: %v", uo.UserPositionID, err)
			return nil
		}
		if _, err := s.repo.CloseAndCreateRemainingUserOrderPosition(uo.UserOrderPositionID, fillQty, uo.RiskCtrlStratID, *updateTime); err != nil {
			log.Printf("order scanner: close user_order_position %d: %v", uo.UserOrderPositionID, err)
			return nil
		}
		if _, err := s.repo.CloseAndCreateRemainingUserPosition(uo.UserPositionID, fillQty, uo.RiskCtrlStratID, *updateTime); err != nil {
			log.Printf("order scanner: close user_position %d: %v", uo.UserPositionID, err)
		}
		s.sendRiskCloseFilledNotification(uo, userPosition, orderPosition, info.Price, fillQty)
		pos, _ := s.repo.GetUserOrderPositionByID(uo.UserOrderPositionID)
		if pos != nil {
			s.updateRulesForStrategy(pos.UserStrategyID)
		}
	}
	return nil
}

func (s *OrderStatusScanner) sendRiskCloseFilledNotification(uo *order.UprunningOrder, userPosition *order.UserPosition, orderPosition *order.UserOrderPosition, price, quantity float64) {
	if s.notifier == nil {
		return
	}
	userName := s.lookupUserName(uo.UserID)
	strategyName := s.lookupStrategyName(userPosition.UserStrategyID)
	if err := s.notifier.SendCloseOrder(&notification.CloseOrderMessage{
		UserName:         userName,
		EventName:        "新风控下单",
		Symbol:           uo.Symbol,
		StrategyName:     strategyName,
		Side:             notification.GetCloseOrderSide(orderPosition.Side == order.SideLong),
		Price:            price,
		Quantity:         quantity,
		Profit:           userPosition.PnL,
		ProfitPercentage: userPosition.ROI,
	}); err != nil {
		log.Printf("warn: order scanner risk close filled notification failed: %v", err)
	}
}

func (s *OrderStatusScanner) sendOpenFilledNotification(uo *order.UprunningOrder, strategyID uint64, price, quantity float64) {
	if s.notifier == nil {
		return
	}
	userName := s.lookupUserName(uo.UserID)
	strategyName := s.lookupStrategyName(strategyID)
	if err := s.notifier.SendOpenOrder(&notification.OpenOrderMessage{
		UserName:     userName,
		EventName:    "FutureOrder",
		Symbol:       uo.Symbol,
		StrategyName: strategyName,
		Side:         notification.GetOpenOrderSide(uo.Side == order.SideLong),
		Price:        price,
		Quantity:     quantity,
	}); err != nil {
		log.Printf("warn: order scanner open filled notification failed: %v", err)
	}
}

func (s *OrderStatusScanner) lookupUserName(userID uint64) string {
	user, err := s.repo.GetUserByID(userID)
	if err != nil || user == nil || user.Name == "" {
		return fmt.Sprintf("user_%d", userID)
	}
	return user.Name
}

func (s *OrderStatusScanner) lookupStrategyName(strategyID uint64) string {
	strategy, err := s.repo.GetUserStrategyByID(strategyID)
	if err != nil || strategy == nil || strategy.Name == "" {
		return fmt.Sprintf("strategy_%d", strategyID)
	}
	return strategy.Name
}

func resolvedFilledQty(uo *order.UprunningOrder, info *exchange.OrderInfo) float64 {
	fillQty := info.Filled
	if fillQty <= 0 && strings.EqualFold(uo.Exchange, "hyperliquid") && uo.ExchangeOrderQty > 0 {
		fillQty = uo.ExchangeOrderQty
	}
	return fillQty
}

func (s *OrderStatusScanner) queryPositionMetadata(userOrderID uint64) (*rpc.QueryOrderPositionMetadataResponse, error) {
	if s.rpc == nil {
		return nil, fmt.Errorf("metadata rpc client is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	metadata, err := s.rpc.QueryOrderPositionMetadata(ctx, userOrderID)
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

func (s *OrderStatusScanner) updateRuleStatus(ruleID uint64, status string) {
	if s.rules == nil || ruleID == 0 {
		return
	}
	if err := s.rules.UpdateRuleStatus(int(ruleID), status); err != nil {
		log.Printf("order scanner: update rule %d to %s: %v", ruleID, status, err)
	}
}

func (s *OrderStatusScanner) updateRulesForStrategy(strategyID uint64) {
	if s.rules == nil || strategyID == 0 {
		return
	}
	if err := s.rules.ResetRulesForStrategy(strategyID); err != nil {
		log.Printf("order scanner: reset rules for strategy %d: %v", strategyID, err)
	}
}

// notifyUserOrderStatusFailed sends async RPC to user_order_service to set status=3.
func (s *OrderStatusScanner) notifyUserOrderStatusFailed(userOrderID uint64) {
	if s.rpc == nil {
		log.Printf("order scanner: RPC client is nil, cannot notify user_order %d", userOrderID)
		return
	}
	log.Printf("order scanner: notifying user_order_service to mark order %d as FAILED", userOrderID)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.rpc.UpdateUserOrderStatusFailed(ctx, userOrderID); err != nil {
			log.Printf("order scanner: RPC update user_order %d FAILED failed: %v", userOrderID, err)
		} else {
			log.Printf("order scanner: RPC update user_order %d FAILED success", userOrderID)
		}
	}()
}
