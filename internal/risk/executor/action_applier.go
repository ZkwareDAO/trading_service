package executor

import (
	"fmt"
	"log"
	"sort"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/exchange/deribit"
	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
)

// SpreadTooWideError indicates bid/ask spread is too wide for automatic closing.
// Rule remains active for retry in next risk control cycle.
type SpreadTooWideError struct {
	RuleID    uint64
	Symbol    string
	Spread    float64
	Threshold float64
}

func (e *SpreadTooWideError) Error() string {
	return fmt.Sprintf("spread too wide (ruleID=%d, symbol=%s): %.6f > %.6f, rule remains active",
		e.RuleID, e.Symbol, e.Spread, e.Threshold)
}

// ExchangeResolver resolves a user exchange by name.
type ExchangeResolver interface {
	ResolveExchange(userID uint64, name string) (exchange.Exchange, error)
}

// RuleStatusUpdater updates rule status in a RuleStore.
type RuleStatusUpdater interface {
	UpdateRuleStatus(id int, status string) error
	GetRule(id int) (*risk.Rule, bool)
}

// RiskActionApplier applies risk action results to exchanges and persistence.
type RiskActionApplier struct {
	repo          *persistence.StateRepository
	rules         RuleStatusUpdater
	resolver      ExchangeResolver
	notifier      notification.Notifier
	spreadChecker *deribit.SpreadChecker
}

func NewRiskActionApplier(repo *persistence.StateRepository, rules RuleStatusUpdater, resolver ExchangeResolver, notifier notification.Notifier, spreadChecker *deribit.SpreadChecker) *RiskActionApplier {
	return &RiskActionApplier{repo: repo, rules: rules, resolver: resolver, notifier: notifier, spreadChecker: spreadChecker}
}

func (a *RiskActionApplier) ApplyReduce(action *ActionResult) error {
	if action == nil {
		return fmt.Errorf("risk action is nil")
	}
	if action.ActionType != "reduce" {
		return fmt.Errorf("unsupported risk action: %s", action.ActionType)
	}
	if action.RuleID == 0 {
		return fmt.Errorf("risk action missing rule id")
	}
	if action.UserPositionID == 0 {
		return fmt.Errorf("risk action missing user_position id")
	}
	if action.Side != risk.SideLong && action.Side != risk.SideShort {
		return fmt.Errorf("unsupported risk action side: %d", action.Side)
	}

	log.Printf("Risk reduce started: ruleID=%d, userPositionID=%d, symbol=%s, side=%d, quantity=%.4f",
		action.RuleID, action.UserPositionID, action.Symbol, action.Side, action.Quantity)

	rule, ok := a.rules.GetRule(int(action.RuleID))
	if !ok {
		return fmt.Errorf("rule %d not found", action.RuleID)
	}
	if rule.Status != config.RuleStatusActive {
		return fmt.Errorf("rule %d is not active: %s", action.RuleID, rule.Status)
	}

	userPosition, err := a.repo.GetUserPositionByID(action.UserPositionID)
	if err != nil {
		return err
	}
	selectedPositions, err := a.selectOrderPositions(action, userPosition)
	if err != nil {
		return err
	}

	log.Printf("Risk reduce: selected %d order_positions to close (total quantity=%.4f)", len(selectedPositions), action.Quantity)

	remainingQty := action.Quantity
	for _, orderPosition := range selectedPositions {
		// Skip if remainingQty is 0 (position already fully closed)
		if remainingQty <= 0 {
			log.Printf("Risk reduce: skipping orderPositionID=%d, remaining quantity is 0", orderPosition.ID)
			continue
		}

		ex, err := a.resolver.ResolveExchange(orderPosition.UserID, orderPosition.Exchange)
		if err != nil {
			return fmt.Errorf("resolve exchange %s: %w", orderPosition.Exchange, err)
		}

		// Deribit spread check before closing position
		if orderPosition.Exchange == "deribit" && a.spreadChecker != nil {
			if deribitEx, ok := ex.(*deribit.Deribit); ok {
				spread, threshold, err := a.spreadChecker.CheckSpreadBeforeClose(
					deribitEx,
					action.Symbol,
					action.UserID,
					action.UserStrategyID,
				)
				if err != nil {
					log.Printf("Risk reduce: Deribit spread check failed for orderPositionID=%d: %v",
						orderPosition.ID, err)
					// Return special error - rule status remains active for retry
					return &SpreadTooWideError{
						RuleID:    action.RuleID,
						Symbol:    action.Symbol,
						Spread:    spread,
						Threshold: threshold,
					}
				}
			}
		}

		closeQty := orderPosition.Quantity
		if closeQty > remainingQty {
			closeQty = remainingQty
		}

		log.Printf("Risk reduce: placing close order for orderPositionID=%d, exchange=%s, symbol=%s, closeQty=%.4f",
			orderPosition.ID, orderPosition.Exchange, action.Symbol, closeQty)

		req := exchange.CreateOrderRequest{
			Symbol:       action.Symbol,
			Side:         closeOrderSide(int(action.Side)),
			OrderType:    mapRiskOrderType(action.OrderType),
			Quantity:     closeQty,
			PositionSide: closePositionSide(int(action.Side)),
			ReduceOnly:   true, // Risk control orders should only reduce position, never open new
			UserID:       action.UserID,
			RelationID:   action.RuleID,
			RelationType: order.RelationTypeRiskControlStrategy,
		}
		exResp, err := ex.CreateOrder(req)
		if err != nil {
			log.Printf("Risk reduce: exchange order failed for orderPositionID=%d: %v", orderPosition.ID, err)
			return fmt.Errorf("exchange risk reduce order: %w", err)
		}

		log.Printf("Risk reduce: exchange order created for orderPositionID=%d, exchangeOrderID=%d, status=%s, price=%.4f, qty=%.4f",
			orderPosition.ID, exResp.OrderID, exResp.Status, exResp.Price, exResp.Quantity)

		now := time.Now()
		uprunningOrderID := a.repo.CreateUprunningOrder(&order.UprunningOrder{
			UserID:              action.UserID,
			RelationID:          action.RuleID,
			RelationType:        order.RelationTypeRiskControlStrategy,
			RiskCtrlStratID:     action.RuleID,
			UserOrderPositionID: orderPosition.ID,
			UserPositionID:      userPosition.ID,
			Exchange:            orderPosition.Exchange,
			Symbol:              action.Symbol,
			PosType:             orderPosition.PosType,
			ExchangeOrderID:     exResp.OrderID,
			ExchangeOrderStatus: string(exResp.Status),
			ExchangeOrderPrice:  exResp.Price,
			ExchangeOrderQty:    exResp.Quantity,
			ExchangeUpdateTime:  &now,
			Side:                order.Side(action.Side),
			CreatedAt:           now,
			UpdatedAt:           now,
		})
		log.Printf("Risk reduce: created uprunning_orderID=%d for risk close (ruleID=%d, orderPositionID=%d, exchangeOrderID=%d)",
			uprunningOrderID, action.RuleID, orderPosition.ID, exResp.OrderID)
		remainingQty -= closeQty
	}

	if err := a.rules.UpdateRuleStatus(int(action.RuleID), config.RuleStatusInUse); err != nil {
		return fmt.Errorf("mark rule in_use: %w", err)
	}
	log.Printf("Risk reduce: rule status updated to in_use (ruleID=%d)", action.RuleID)

	log.Printf("Risk reduce completed: ruleID=%d, closed %d positions", action.RuleID, len(selectedPositions))

	// NOTE: Risk close notifications moved to PMS FILLED handling.
	// Keep this reference block disabled because price/quantity here are planned values,
	// not actual fill price/quantity.
	// strategyName := ""
	// if userStrategy, err := a.repo.GetUserStrategyByID(action.UserStrategyID); err == nil {
	// 	strategyName = userStrategy.Name
	// } else {
	// 	log.Printf("warn: risk reduce notification missing strategy %d: %v", action.UserStrategyID, err)
	// }
	// if a.notifier != nil {
	// 	if err := a.notifier.SendCloseOrder(&notification.CloseOrderMessage{
	// 		Symbol:           action.Symbol,
	// 		StrategyName:     strategyName,
	// 		Side:             notification.GetCloseOrderSide(action.Side == risk.SideLong),
	// 		Price:            0, // Filled price will be logged separately if needed
	// 		Quantity:         action.Quantity,
	// 		Profit:           userPosition.PnL,
	// 		ProfitPercentage: userPosition.ROI,
	// 	}); err != nil {
	// 		log.Printf("warn: risk reduce notification failed: %v", err)
	// 	}
	// }

	return nil
}

func (a *RiskActionApplier) selectOrderPositions(action *ActionResult, userPosition *order.UserPosition) ([]*order.UserOrderPosition, error) {
	positions := a.repo.ListActivePositions()
	sort.Slice(positions, func(i, j int) bool { return positions[i].ID < positions[j].ID })

	remainingQty := action.Quantity
	selected := make([]*order.UserOrderPosition, 0)
	for _, pos := range positions {
		if pos.UserID != action.UserID || pos.UserStrategyID != action.UserStrategyID {
			continue
		}
		if pos.Exchange != userPosition.Exchange || pos.PosType != userPosition.PosType {
			continue
		}
		if action.Symbol != "" && pos.Asset != action.Symbol {
			continue
		}
		selected = append(selected, pos)
		remainingQty -= pos.Quantity
		if remainingQty <= 1e-12 {
			return selected, nil
		}
	}
	return nil, fmt.Errorf("insufficient active user_order_position quantity for risk action rule %d", action.RuleID)
}

func closeOrderSide(side int) exchange.OrderSide {
	if side == int(order.SideLong) {
		return exchange.OrderSideSell
	}
	return exchange.OrderSideBuy
}

func closePositionSide(side int) exchange.PositionSide {
	if side == int(order.SideLong) {
		return exchange.PositionSideLong
	}
	return exchange.PositionSideShort
}

func mapRiskOrderType(orderType int) exchange.OrderType {
	if orderType == 0 {
		return exchange.OrderTypeLimit
	}
	return exchange.OrderTypeMarket
}
