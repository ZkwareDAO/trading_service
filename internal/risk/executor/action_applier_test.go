package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
)

type recordingNotifier struct {
	closeMessages []*notification.CloseOrderMessage
	err           error
}

func (n *recordingNotifier) SendOpenOrder(*notification.OpenOrderMessage) error {
	return nil
}

func (n *recordingNotifier) SendCloseOrder(msg *notification.CloseOrderMessage) error {
	n.closeMessages = append(n.closeMessages, msg)
	return n.err
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

type staticExchangeResolver struct {
	ex exchange.Exchange
}

func (r *staticExchangeResolver) ResolveExchange(uint64, string) (exchange.Exchange, error) {
	return r.ex, nil
}

func setupRiskActionApplierTest(t *testing.T, notifier notification.Notifier) (*RiskActionApplier, *exchange.MockExchange, uint64, uint64) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rule.csv"), []byte("id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n1,100,roi,<=,-0.02,1,active,reduce,{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	state, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(state.Shutdown)

	repo := persistence.NewStateRepository(state)
	now := time.Now()
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:    1,
		Name:      "CTA_TEST",
		Exchange:  "mock",
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	})
	positionID := repo.CreateUserPosition(&order.UserPosition{
		UserID:         1,
		UserStrategyID: userStrategyID,
		Exchange:       "mock",
		PosType:        order.PosTypeFutures,
		Quantity:       2,
		ROI:            0.12,
		PnL:            24,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         1,
		UserStrategyID: userStrategyID,
		Exchange:       "mock",
		PosType:        order.PosTypeFutures,
		Asset:          "BTCUSDT",
		Quantity:       2,
		Side:           order.SideLong,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	ruleStore, err := config.NewRuleStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	mockExchange := exchange.NewMockExchange()
	applier := NewRiskActionApplier(repo, ruleStore, &staticExchangeResolver{ex: mockExchange}, notifier, nil)
	return applier, mockExchange, positionID, userStrategyID
}

func TestRiskActionApplier_ApplyReduceDoesNotSendCloseNotificationBeforeFilled(t *testing.T) {
	notifier := &recordingNotifier{}
	applier, mockExchange, positionID, userStrategyID := setupRiskActionApplierTest(t, notifier)

	err := applier.ApplyReduce(&ActionResult{
		ActionType:      "reduce",
		UserID:          1,
		UserStrategyID:  userStrategyID,
		RuleID:          1,
		UserPositionID:  positionID,
		Symbol:          "BTCUSDT",
		Side:            risk.SideLong,
		Quantity:        2,
		QuantityPercent: 1,
		OrderType:       1,
	})
	if err != nil {
		t.Fatalf("ApplyReduce: %v", err)
	}
	if len(mockExchange.CreatedOrders) != 1 {
		t.Fatalf("expected one exchange order, got %d", len(mockExchange.CreatedOrders))
	}
	if len(notifier.closeMessages) != 0 {
		t.Fatalf("expected no close notification before FILLED, got %d", len(notifier.closeMessages))
	}
}

func TestRiskActionApplier_ApplyReduceAllowsNilNotifier(t *testing.T) {
	applier, _, positionID, userStrategyID := setupRiskActionApplierTest(t, nil)

	err := applier.ApplyReduce(&ActionResult{
		ActionType:     "reduce",
		UserID:         1,
		UserStrategyID: userStrategyID,
		RuleID:         1,
		UserPositionID: positionID,
		Symbol:         "BTCUSDT",
		Side:           risk.SideLong,
		Quantity:       2,
		OrderType:      1,
	})
	if err != nil {
		t.Fatalf("ApplyReduce: %v", err)
	}
}

func TestRiskActionApplier_ApplyReduceIgnoresNotificationError(t *testing.T) {
	notifier := &recordingNotifier{err: fmt.Errorf("notification failed")}
	applier, _, positionID, userStrategyID := setupRiskActionApplierTest(t, notifier)

	err := applier.ApplyReduce(&ActionResult{
		ActionType:     "reduce",
		UserID:         1,
		UserStrategyID: userStrategyID,
		RuleID:         1,
		UserPositionID: positionID,
		Symbol:         "BTCUSDT",
		Side:           risk.SideLong,
		Quantity:       2,
		OrderType:      1,
	})
	if err != nil {
		t.Fatalf("ApplyReduce should ignore notification errors: %v", err)
	}
	if len(notifier.closeMessages) != 0 {
		t.Fatalf("expected no close notification attempt before FILLED, got %d", len(notifier.closeMessages))
	}
}
