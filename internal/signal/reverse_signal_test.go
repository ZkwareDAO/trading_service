package signal

import (
	"context"
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

func TestHandleReverseSignal_WritesCloseRuleThenOpensWhenOldSideClosed(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)
	mock := exchange.NewMockExchange()

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "reverse_user", Exchange: "mock", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "reverse_strategy", CreatedAt: now, UpdatedAt: now})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{UserID: userID, Name: "reverse_strategy", Exchange: "mock", Cash: 1000, Parts: 5, Status: 1, StrategyID: strategyID, CreatedAt: now, UpdatedAt: now})

	positionClient := &fakePositionQueryClient{response: &rpc.QueryUserOrderPositionsResponse{Count: 0}}
	h := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, false, positionClient, nil)

	err = h.HandleReverseSignal(context.Background(), Signal{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Symbol:         "BTC",
		PosType:        int(order.PosTypeFutures),
		Exchange:       "mock",
		Cash:           100,
		TriggerPrice:   50000,
		Side:           int(order.SideShort),
		OrderType:      1,
		Leverage:       10,
	}, ActionReverseLong)
	if err != nil {
		t.Fatalf("HandleReverseSignal: %v", err)
	}

	records := readRuleCSV(t, dir)
	if len(records) != 2 {
		t.Fatalf("expected header + close rule, got %d records", len(records))
	}
	if records[1][2] != "always" || records[1][4] != "true" {
		t.Fatalf("unexpected close rule: %v", records[1])
	}
	if positionClient.lastRequest.Side == nil || *positionClient.lastRequest.Side != int(order.SideShort) {
		t.Fatalf("expected reverse_long to wait for short side closed, got request %+v", positionClient.lastRequest)
	}
	if len(mock.CreatedOrders) != 1 {
		t.Fatalf("expected one open order after old side closed, got %d", len(mock.CreatedOrders))
	}
	if mock.CreatedOrders[0].Side != exchange.OrderSideBuy {
		t.Fatalf("expected reverse_long open BUY, got %+v", mock.CreatedOrders[0])
	}
}
