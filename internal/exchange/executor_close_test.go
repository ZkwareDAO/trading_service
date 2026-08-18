package exchange

import (
	"testing"

	"trading-service/internal/order"
)

func TestHandleOrderFilled_ClosesPositionOnCloseOrderFilled(t *testing.T) {
	exec, _, gs, repo := setupOrderHandlerTest(t)
	defer gs.Shutdown()

	// Create an open position directly
	pos := &order.UserOrderPosition{
		UserID:         1,
		UserStrategyID: 1,
		Exchange:       "mock",
		PosType:        order.PosTypeFutures,
		Asset:          "BTCUSDT",
		Side:           order.SideLong,
		Quantity:       0.1,
		Deleted:        0,
	}
	posID := repo.CreateUserOrderPosition(pos)
	userPositionID := repo.CreateUserPosition(&order.UserPosition{
		UserID:         1,
		UserStrategyID: 1,
		Exchange:       "mock",
		PosType:        order.PosTypeFutures,
		Quantity:       0.1,
		Deleted:        0,
	})

	// Create close order FILLED (relation_type is NOT user_orders)
	closeUo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          200,
		RelationType:        order.RelationTypeRiskControlStrategy,
		RiskCtrlStratID:     200,
		UserOrderPositionID: posID,
		UserPositionID:      userPositionID,
		Symbol:              "BTCUSDT",
		PosType:             order.PosTypeFutures,
		Exchange:            "mock",
		Side:                order.SideShort,
	}
	closeUoID := exec.CreateRunningOrder(closeUo)

	closeUpdate := &OrderUpdate{
		OrderID:      closeUoID,
		Status:       "FILLED",
		AvgPrice:     48000,
		ExecutedQty:  0.1,
		PositionSide: "LONG",
		UserID:       1,
		PosType:      order.PosTypeFutures,
		RelationID:   200,
		Symbol:       "BTCUSDT",
	}
	if err := exec.HandleOrderFilled(closeUpdate); err != nil {
		t.Fatalf("HandleOrderFilled: %v", err)
	}

	// Verify position is closed
	closedPos, err := repo.GetUserOrderPositionByID(posID)
	if err != nil {
		t.Fatal(err)
	}
	if closedPos.Deleted != 1 {
		t.Errorf("expected position deleted=1, got %d", closedPos.Deleted)
	}
	if closedPos.CloseTime == nil {
		t.Error("expected CloseTime to be set")
	}
}
