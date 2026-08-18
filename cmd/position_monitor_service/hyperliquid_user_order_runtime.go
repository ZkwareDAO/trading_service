package main

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"trading-service/internal/exchange"
	hlexchange "trading-service/internal/exchange/hyperliquid"
	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"

	"github.com/sonirico/go-hyperliquid"
)

// HyperliquidUserOrderRuntime subscribes to order updates for a single Hyperliquid user
// and forwards them directly to OrderExecutor (same as Binance OrderMonitor).
type HyperliquidUserOrderRuntime struct {
	userID   string
	testnet  bool
	ws       *hyperliquid.WebsocketClient
	repo     *persistence.StateRepository
	executor *exchange.OrderExecutor
	mu       sync.Mutex
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewHyperliquidUserOrderRuntimeForTest creates a runtime with injected executor and repo.
func NewHyperliquidUserOrderRuntimeForTest(repo *persistence.StateRepository, executor *exchange.OrderExecutor) *HyperliquidUserOrderRuntime {
	return &HyperliquidUserOrderRuntime{repo: repo, executor: executor}
}

type HyperliquidUserOrderRuntimeFactory struct {
	repo            *persistence.StateRepository
	orderServiceURL string
	testnet         bool
	ruleUpdater     ruleStatusUpdater
	notifier        notification.Notifier
}

func NewHyperliquidUserOrderRuntimeFactory(repo *persistence.StateRepository, orderServiceURL string, testnet bool) *HyperliquidUserOrderRuntimeFactory {
	return &HyperliquidUserOrderRuntimeFactory{repo: repo, orderServiceURL: orderServiceURL, testnet: testnet}
}

func NewHyperliquidUserOrderRuntimeFactoryWithRuleUpdater(repo *persistence.StateRepository, orderServiceURL string, testnet bool, updater ruleStatusUpdater, notifier notification.Notifier) *HyperliquidUserOrderRuntimeFactory {
	return &HyperliquidUserOrderRuntimeFactory{repo: repo, orderServiceURL: orderServiceURL, testnet: testnet, ruleUpdater: updater, notifier: notifier}
}

func (f *HyperliquidUserOrderRuntimeFactory) NewUserOrderRuntime(user *order.User) (UserOrderRuntime, error) {
	if user == nil || user.Exchange != "hyperliquid" {
		return nil, nil
	}

	baseURL := hyperliquid.MainnetAPIURL
	if f.testnet {
		baseURL = hyperliquid.TestnetAPIURL
	}

	// No repo → no-op runtime (used in tests / stubs)
	if f.repo == nil {
		return &HyperliquidUserOrderRuntime{stopCh: make(chan struct{})}, nil
	}

	ex, err := hlexchange.NewHyperliquid(user.APISecret, user.APIKey, f.testnet)
	if err != nil {
		return nil, err
	}

	exec := exchange.NewOrderExecutor(f.repo, ex)
	if f.orderServiceURL != "" {
		exec.SetRPCClient(rpc.NewOrderServiceClient(f.orderServiceURL))
	}
	if f.ruleUpdater != nil {
		exec.SetRuleStatusUpdater(f.ruleUpdater)
	}
	if f.notifier != nil {
		exec.SetNotifier(f.notifier)
	}

	return &HyperliquidUserOrderRuntime{
		userID:   user.APIKey,
		testnet:  f.testnet,
		ws:       hyperliquid.NewWebsocketClient(baseURL),
		repo:     f.repo,
		executor: exec,
		stopCh:   make(chan struct{}),
	}, nil
}

func (r *HyperliquidUserOrderRuntime) Start(ctx context.Context) error {
	if r.ws == nil || r.executor == nil {
		return nil // no-op for test/no-repo cases
	}

	if err := r.ws.Connect(ctx); err != nil {
		return err
	}

	_, err := r.ws.OrderUpdates(
		hyperliquid.OrderUpdatesSubscriptionParams{User: r.userID},
		r.handleUpdates,
	)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		r.Stop()
	}()
	return nil
}

func (r *HyperliquidUserOrderRuntime) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopCh)
		if r.ws != nil {
			_ = r.ws.Close()
		}
	})
}

// handleUpdates processes incoming order updates from Hyperliquid WS.
//
// Same pattern as Binance OrderMonitor: parse WS event → look up local order ID → call OrderExecutor.
// OrderExecutor handles the relation_type branching (user_orders vs risk_control_strategy).
func (r *HyperliquidUserOrderRuntime) handleUpdates(orders []hyperliquid.WsOrder, err error) {
	if err != nil {
		log.Printf("hyperliquid order updates error: %v", err)
		return
	}
	if r.repo == nil || r.executor == nil {
		return // no-op for test/no-repo cases
	}

	for _, o := range orders {
		exchangeOrderID := uint64(o.Order.Oid)

		// Retry loop: WS may arrive before CreateOrder's UpdateUprunningOrder
		// has populated exchange_order_id in the database.
		// First check the pending cache in the executor.
		var uo *order.UprunningOrder
		var uoErr error
		if uo = r.executor.FindPendingOrderByExchangeID(exchangeOrderID); uo != nil {
			// Found in cache
		} else {
			for attempt := 0; attempt < 5; attempt++ {
				uo, uoErr = r.repo.FindUprunningOrderByExchangeID(exchangeOrderID)
				if uoErr == nil {
					break
				}
				if attempt < 4 {
					time.Sleep(300 * time.Millisecond)
				}
			}
		}
		if uoErr != nil {
			log.Printf("hyperliquid: running order not found for exchangeOrderID=%d: %v", exchangeOrderID, uoErr)
			continue
		}

		limitPrice, _ := strconv.ParseFloat(o.Order.LimitPx, 64)
		avgPrice := limitPrice
		execQty := 0.0
		if isHyperliquidOrderFilled(string(o.Status)) {
			qty, err := hyperliquidExecutedQtyFromWS(o.Order, uo.ExchangeOrderQty)
			if err != nil {
				log.Printf("hyperliquid: resolve executed qty failed for order %d: %v", uo.ID, err)
				continue
			}
			execQty = qty
			avgPrice = hyperliquidFilledPriceFromWS(o.Order, uo.ExchangeOrderPrice)
		}

		status := normalizeHyperliquidOrderStatus(string(o.Status))
		log.Printf("hyperliquid ws order update: local_order_id=%d exchange_order_id=%d relation_type=%s raw_status=%s status=%s coin=%s side=%s limit_px=%s avg_price=%.12f exchange_order_price=%.12f sz=%s orig_sz=%s exec_qty=%.12f exchange_order_qty=%.12f",
			uo.ID,
			exchangeOrderID,
			uo.RelationType,
			string(o.Status),
			status,
			o.Order.Coin,
			o.Order.Side,
			o.Order.LimitPx,
			avgPrice,
			uo.ExchangeOrderPrice,
			o.Order.Sz,
			o.Order.OrigSz,
			execQty,
			uo.ExchangeOrderQty)

		// FILLED → handle position creation/closure (relation_type branching is inside executor)
		if isHyperliquidOrderFilled(string(o.Status)) {
			update := &exchange.OrderUpdate{
				OrderID:      uo.ID,
				Symbol:       normalizeHyperliquidPriceSymbol(o.Order.Coin),
				Status:       string(o.Status),
				AvgPrice:     avgPrice,
				ExecutedQty:  execQty,
				PositionSide: hyperliquidPositionSide(o.Order.Side),
				UserID:       uo.UserID,
				PosType:      uo.PosType,
				RelationID:   uo.RelationID,
			}
			if err := r.executor.HandleOrderFilled(update); err != nil {
				log.Printf("hyperliquid filled update failed for order %d: %v", uo.ID, err)
			} else {
				log.Printf("hyperliquid: FILLED order %d processed successfully (symbol=%s, avgPrice=%.8f, execQty=%.8f)",
					uo.ID, update.Symbol, update.AvgPrice, update.ExecutedQty)
			}
			continue
		}

		// Non-FILLED status update — executor handles open/cancel/failed transitions.
		if err := r.executor.HandleOrderStatusUpdate(uo.ID, status, avgPrice, execQty, nil); err != nil {
			log.Printf("hyperliquid status update failed for order %d: %v", uo.ID, err)
			continue
		}
	}
}

func hyperliquidFilledPriceFromWS(order hyperliquid.WsBasicOrder, fallbackPrice float64) float64 {
	if fallbackPrice > 0 {
		return fallbackPrice
	}
	limitPrice, _ := strconv.ParseFloat(order.LimitPx, 64)
	return limitPrice
}

func hyperliquidExecutedQtyFromWS(order hyperliquid.WsBasicOrder, fallbackQty float64) (float64, error) {
	origQty, err := strconv.ParseFloat(order.OrigSz, 64)
	if err != nil {
		if fallbackQty > 0 {
			return fallbackQty, nil
		}
		return 0, err
	}
	remainingQty, err := strconv.ParseFloat(order.Sz, 64)
	if err != nil {
		if fallbackQty > 0 {
			return fallbackQty, nil
		}
		return 0, err
	}
	executedQty := origQty - remainingQty
	if executedQty > 0 {
		return executedQty, nil
	}
	if fallbackQty > 0 {
		return fallbackQty, nil
	}
	return 0, nil
}

func isHyperliquidOrderFilled(status string) bool {
	return strings.EqualFold(status, "filled")
}

func normalizeHyperliquidOrderStatus(status string) string {
	switch {
	case strings.EqualFold(status, "filled"):
		return "FILLED"
	case strings.EqualFold(status, "canceled"), strings.EqualFold(status, "cancelled"):
		return "CANCELED"
	case strings.EqualFold(status, "open"):
		return "NEW"
	default:
		return status
	}
}

func hyperliquidPositionSide(side string) string {
	if side == "B" || side == "BUY" || side == "buy" {
		return "LONG"
	}
	return "SHORT"
}
