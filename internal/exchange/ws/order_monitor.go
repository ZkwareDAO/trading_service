package ws

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"

	"github.com/adshao/go-binance/v2/futures"
)

// OrderMonitor monitors order status changes via WebSocket.
type OrderMonitor struct {
	executor *exchange.OrderExecutor
	repo     *persistence.StateRepository
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewOrderMonitor creates a new order monitor.
func NewOrderMonitor(exec *exchange.OrderExecutor, repo *persistence.StateRepository) *OrderMonitor {
	return &OrderMonitor{
		executor: exec,
		repo:     repo,
		stopCh:   make(chan struct{}),
	}
}

// HandleOrderUpdate handles an order status update from WebSocket.
func (m *OrderMonitor) HandleOrderUpdate(event *futures.WsUserDataEvent) {
	if event == nil || event.Event != futures.UserDataEventTypeOrderTradeUpdate {
		return
	}

	update := event.OrderTradeUpdate
	status := string(update.Status)
	exchangeOrderID := uint64(update.ID)

	// Retry loop: WS may arrive before CreateOrder's UpdateUprunningOrder
	// has populated exchange_order_id in the database.
	// First check the pending cache in the executor.
	var uo *order.UprunningOrder
	var err error

	if uo = m.executor.FindPendingOrderByExchangeID(exchangeOrderID); uo != nil {
		// Found in cache
	} else {
		// Fall back to DB retry
		for attempt := 0; attempt < 5; attempt++ {
			uo, err = m.repo.FindUprunningOrderByExchangeID(exchangeOrderID)
			if err == nil {
				break
			}
			if attempt < 4 {
				time.Sleep(300 * time.Millisecond)
			}
		}
		if err != nil {
			log.Printf("order monitor: running order not found for exchangeOrderID=%d: %v", exchangeOrderID, err)
			return
		}
	}

	if uo.ExchangeOrderStatus == status {
		return
	}

	avgPrice, _ := parseFloat64(update.AveragePrice)
	// WsOrderTradeUpdate has OriginalQty and AccumulatedFilledQty
	execQty, _ := parseFloat64(update.AccumulatedFilledQty)

	if status == "FILLED" {
		m.handleFilledOrder(&update, uo)
		return
	}

	updateTime := time.Unix(0, update.TradeTime*int64(time.Millisecond))
	if err := m.executor.HandleOrderStatusUpdate(uo.ID, status, avgPrice, execQty, &updateTime); err != nil {
		log.Printf("order monitor: failed to update order %d: %v", uo.ID, err)
		return
	}

	log.Printf("order monitor: order %d status updated: %s -> %s", uo.ID, uo.ExchangeOrderStatus, status)
}

func (m *OrderMonitor) handleFilledOrder(update *futures.WsOrderTradeUpdate, uo *order.UprunningOrder) {
	avgPrice, _ := parseFloat64(update.AveragePrice)
	execQty, _ := parseFloat64(update.AccumulatedFilledQty)
	positionSide := string(update.PositionSide)

	orderUpdate := &exchange.OrderUpdate{
		OrderID:      uo.ID,
		Symbol:       uo.Symbol,
		Status:       "FILLED",
		AvgPrice:     avgPrice,
		ExecutedQty:  execQty,
		PositionSide: positionSide,
		UserID:       uo.UserID,
		PosType:      uo.PosType,
		RelationID:   uo.RelationID,
	}

	if err := m.executor.HandleOrderFilled(orderUpdate); err != nil {
		log.Printf("order monitor: failed to handle FILLED order %d: %v", uo.ID, err)
		return
	}

	log.Printf("order monitor: position created for FILLED order %d", uo.ID)
}

// Stop stops the order monitor (safe to call multiple times).
func (m *OrderMonitor) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
}

// StartFuturesUserDataWS starts futures user data WebSocket with auto-reconnect.
// listenKeyProvider is called to get a fresh listenKey on each (re)connection.
func (m *OrderMonitor) StartFuturesUserDataWS(listenKeyProvider func() (string, error)) {
	go func() {
		reconnectCount := 0
		for {
			select {
			case <-m.stopCh:
				log.Printf("futures user data WS: stop requested")
				return
			default:
			}

			listenKey, err := listenKeyProvider()
			if err != nil {
				log.Printf("futures user data WS: failed to get listenKey: %v, retrying in 5s", err)
				time.Sleep(5 * time.Second)
				continue
			}

			reconnectCount++
			log.Printf("futures user data WS: connecting (attempt %d, listenKey=%s)...", reconnectCount, listenKey[:min(20, len(listenKey))])

			handler := func(event *futures.WsUserDataEvent) {
				if event != nil {
					log.Printf("futures user data WS: received event type=%s", event.Event)
				}
				m.HandleOrderUpdate(event)
			}

			errHandler := func(err error) {
				log.Printf("futures user data WS: error callback: %v", err)
			}

			startTime := time.Now()
			doneC, _, err := futures.WsUserDataServe(listenKey, handler, errHandler)
			if err != nil {
				log.Printf("futures user data WS: WsUserDataServe returned error immediately: %v, retrying in 5s", err)
				time.Sleep(5 * time.Second)
				continue
			}

			log.Printf("futures user data WS: connection established")

			select {
			case <-m.stopCh:
				log.Printf("futures user data WS: stop requested while connected")
				return
			case <-doneC:
				elapsed := time.Since(startTime)
				log.Printf("futures user data WS: disconnected after %s, reconnecting...", elapsed.Round(time.Second))
				time.Sleep(5 * time.Second)
			}
		}
	}()
}

// StartKeepalive starts periodic listenKey keepalive (every 25 minutes).
func (m *OrderMonitor) StartKeepalive(ctx context.Context, keepaliveFunc func(listenKey string) error, listenKey string) {
	ticker := time.NewTicker(25 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := keepaliveFunc(listenKey); err != nil {
				log.Printf("listenKey keepalive failed: %v", err)
			}
		}
	}
}

func parseFloat64(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
