package exchange

import (
	"testing"
	"time"

	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/rpc"
)

type recordingNotifier struct {
	openMessages  []*notification.OpenOrderMessage
	closeMessages []*notification.CloseOrderMessage
}

func (n *recordingNotifier) SendOpenOrder(msg *notification.OpenOrderMessage) error {
	n.openMessages = append(n.openMessages, msg)
	return nil
}

func (n *recordingNotifier) SendCloseOrder(msg *notification.CloseOrderMessage) error {
	n.closeMessages = append(n.closeMessages, msg)
	return nil
}

func (n *recordingNotifier) SendTest(*notification.TestMessage) error {
	return nil
}

func (n *recordingNotifier) SendManualCloseNotification(*notification.ManualCloseMessage) error {
	return nil
}

func (n *recordingNotifier) SendDeribitPositionNotification(*notification.DeribitPositionMessage) error {
	return nil
}

func TestHandleOrderFilled_SendsOpenNotification(t *testing.T) {
	exec, _, gs, repo := setupOrderHandlerTest(t)
	defer gs.Shutdown()

	notifier := &recordingNotifier{}
	exec.SetNotifier(notifier)

	now := time.Now()
	repo.CreateUser(&order.User{ID: 1, Name: "follow_prod", Exchange: "mock", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateUserStrategy(&order.UserStrategy{UserID: 1, Name: "OBVATR_4H_2_BTCUSDT", Exchange: "mock", CreatedAt: now, UpdatedAt: now})
	exec.SetRPCClient(&mockRPCClient{metadata: &rpc.QueryOrderPositionMetadataResponse{UserStrategyID: strategyID, Leverage: 5, FallbackPrice: 50000}})

	uo := &order.UprunningOrder{UserID: 1, RelationID: 100, RelationType: order.RelationTypeUserOrders, Symbol: "BTCUSDT", PosType: order.PosTypeFutures, Exchange: "mock", Side: order.SideLong}
	uoID := exec.CreateRunningOrder(uo)

	if err := exec.HandleOrderFilled(&OrderUpdate{OrderID: uoID, Symbol: "BTCUSDT", Status: "FILLED", AvgPrice: 50000, ExecutedQty: 0.123, PositionSide: "LONG", UserID: 1, PosType: order.PosTypeFutures, RelationID: 100}); err != nil {
		t.Fatalf("HandleOrderFilled: %v", err)
	}

	if len(notifier.openMessages) != 1 {
		t.Fatalf("expected one open notification, got %d", len(notifier.openMessages))
	}
	msg := notifier.openMessages[0]
	if msg.UserName != "follow_prod" || msg.EventName != "FutureOrder" || msg.StrategyName != "OBVATR_4H_2_BTCUSDT" {
		t.Fatalf("unexpected open notification identity: %+v", msg)
	}
	if msg.Price != 50000 || msg.Quantity != 0.123 || msg.Side != "开多挂单" {
		t.Fatalf("unexpected open notification fill data: %+v", msg)
	}
}

func TestHandleOrderFilled_SendsRiskCloseNotification(t *testing.T) {
	exec, _, gs, repo := setupOrderHandlerTest(t)
	defer gs.Shutdown()

	notifier := &recordingNotifier{}
	exec.SetNotifier(notifier)

	now := time.Now()
	repo.CreateUser(&order.User{ID: 1, Name: "machineLightGbm", Exchange: "mock", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateUserStrategy(&order.UserStrategy{UserID: 1, Name: "ICT_1D_3_XRPUSDT", Exchange: "mock", CreatedAt: now, UpdatedAt: now})
	posID := repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: 1, UserStrategyID: strategyID, Asset: "XRPUSDT", Exchange: "mock", PosType: order.PosTypeFutures, Side: order.SideShort, Quantity: 4776.4, PosPrice: 1.2, Deleted: 0})
	userPositionID := repo.CreateUserPosition(&order.UserPosition{UserID: 1, UserStrategyID: strategyID, Exchange: "mock", PosType: order.PosTypeFutures, Quantity: 4776.4, PnL: -44.898180, ROI: -0.044899, Deleted: 0})

	uo := &order.UprunningOrder{UserID: 1, RelationID: 200, RelationType: order.RelationTypeRiskControlStrategy, RiskCtrlStratID: 200, UserOrderPositionID: posID, UserPositionID: userPositionID, Symbol: "XRPUSDT", PosType: order.PosTypeFutures, Exchange: "mock", Side: order.SideShort}
	uoID := exec.CreateRunningOrder(uo)

	if err := exec.HandleOrderFilled(&OrderUpdate{OrderID: uoID, Symbol: "XRPUSDT", Status: "FILLED", AvgPrice: 1.1, ExecutedQty: 4776.4, PositionSide: "SHORT", UserID: 1, PosType: order.PosTypeFutures, RelationID: 200}); err != nil {
		t.Fatalf("HandleOrderFilled: %v", err)
	}

	if len(notifier.closeMessages) != 1 {
		t.Fatalf("expected one close notification, got %d", len(notifier.closeMessages))
	}
	msg := notifier.closeMessages[0]
	if msg.UserName != "machineLightGbm" || msg.EventName != "新风控下单" || msg.StrategyName != "ICT_1D_3_XRPUSDT" {
		t.Fatalf("unexpected close notification identity: %+v", msg)
	}
	if msg.Price != 1.1 || msg.Quantity != 4776.4 || msg.Side != "平空挂单" || msg.Profit != -44.898180 || msg.ProfitPercentage != -0.044899 {
		t.Fatalf("unexpected close notification data: %+v", msg)
	}
}
