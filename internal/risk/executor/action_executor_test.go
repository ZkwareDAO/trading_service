package executor

import (
	"testing"

	"trading-service/internal/risk"
)

func makeCtx(pos *risk.UserPosition) *risk.RiskContext {
	return &risk.RiskContext{Position: pos}
}

func TestActionExecutor_ClosePosition(t *testing.T) {
	exec := NewActionExecutor()
	ctx := makeCtx(&risk.UserPosition{
		UserID: 1, UserStrategyID: 100, Symbol: "BTCUSDT",
		Side: risk.SideLong, Quantity: 1.0, TotalMargin: 500,
	})
	rule := &risk.Rule{Action: "reduce", Params: map[string]interface{}{"quantity_pct": 1.0, "order_type": 1}}
	result := exec.Execute(rule, ctx)

	if result.ActionType != "reduce" {
		t.Errorf("expected 'reduce', got '%s'", result.ActionType)
	}
	if result.UserID != 1 {
		t.Errorf("expected UserID 1, got %d", result.UserID)
	}
	if result.UserStrategyID != 100 {
		t.Errorf("expected UserStrategyID 100, got %d", result.UserStrategyID)
	}
	if result.Symbol != "BTCUSDT" {
		t.Errorf("expected 'BTCUSDT', got '%s'", result.Symbol)
	}
	if result.Side != risk.SideLong {
		t.Errorf("expected SideLong, got %d", result.Side)
	}
	if result.Quantity != 1.0 {
		t.Errorf("expected Quantity 1.0, got %f", result.Quantity)
	}
	if result.QuantityPercent != 1.0 {
		t.Errorf("expected QuantityPercent 1.0, got %f", result.QuantityPercent)
	}
	if result.RemainingQuantity != 0 {
		t.Errorf("expected RemainingQuantity 0, got %f", result.RemainingQuantity)
	}
	if result.OrderType != 1 {
		t.Errorf("expected OrderType 1 (market), got %d", result.OrderType)
	}
}

func TestActionExecutor_ReducePosition(t *testing.T) {
	exec := NewActionExecutor()
	ctx := makeCtx(&risk.UserPosition{Quantity: 1.0})
	rule := &risk.Rule{Action: "reduce", Params: map[string]interface{}{"quantity_pct": 0.5}}
	result := exec.Execute(rule, ctx)

	if result.Quantity != 0.5 {
		t.Errorf("expected Quantity 0.5, got %f", result.Quantity)
	}
	if result.RemainingQuantity != 0.5 {
		t.Errorf("expected RemainingQuantity 0.5, got %f", result.RemainingQuantity)
	}
}

func TestActionExecutor_DefaultParams(t *testing.T) {
	exec := NewActionExecutor()
	ctx := makeCtx(&risk.UserPosition{Quantity: 1.0})
	rule := &risk.Rule{Action: "reduce"}
	result := exec.Execute(rule, ctx)

	if result.Quantity != 1.0 {
		t.Errorf("expected full quantity with default params, got %f", result.Quantity)
	}
	if result.OrderType != 1 {
		t.Errorf("expected default order_type=1 (market), got %d", result.OrderType)
	}
}

func TestActionExecutor_NilPosition(t *testing.T) {
	exec := NewActionExecutor()
	ctx := &risk.RiskContext{}
	rule := &risk.Rule{Action: "reduce"}
	result := exec.Execute(rule, ctx)
	if result != nil {
		t.Error("expected nil for nil position")
	}
}

func TestActionExecutor_UnknownAction(t *testing.T) {
	exec := NewActionExecutor()
	ctx := makeCtx(&risk.UserPosition{Quantity: 1.0})
	rule := &risk.Rule{Action: "unknown_action"}
	result := exec.Execute(rule, ctx)
	if result != nil {
		t.Error("expected nil for unknown action")
	}
}

func TestActionExecutor_ChainAction(t *testing.T) {
	exec := NewActionExecutor()
	ctx := makeCtx(&risk.UserPosition{Quantity: 1.0})
	rule := &risk.Rule{Action: "3"} // chain to rule ID 3
	result := exec.Execute(rule, ctx)
	if result != nil {
		t.Error("expected nil for chain action (handled by scheduler)")
	}
}

func TestActionExecutor_Timestamp(t *testing.T) {
	exec := NewActionExecutor()
	ctx := makeCtx(&risk.UserPosition{Quantity: 1.0})
	rule := &risk.Rule{Action: "reduce", Params: map[string]interface{}{"quantity_pct": 1.0}}
	result := exec.Execute(rule, ctx)
	if result.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestActionExecutor_PartialClose(t *testing.T) {
	exec := NewActionExecutor()
	ctx := makeCtx(&risk.UserPosition{Quantity: 10.0})
	rule := &risk.Rule{Action: "reduce", Params: map[string]interface{}{"quantity_pct": 0.3}}
	result := exec.Execute(rule, ctx)

	if result.Quantity != 3.0 {
		t.Errorf("expected Quantity 3.0, got %f", result.Quantity)
	}
	if result.RemainingQuantity != 7.0 {
		t.Errorf("expected RemainingQuantity 7.0, got %f", result.RemainingQuantity)
	}
}

func TestActionExecutor_LimitOrder(t *testing.T) {
	exec := NewActionExecutor()
	ctx := makeCtx(&risk.UserPosition{Quantity: 1.0})
	rule := &risk.Rule{Action: "reduce", Params: map[string]interface{}{"quantity_pct": 1.0, "order_type": 0}}
	result := exec.Execute(rule, ctx)

	if result.OrderType != 0 {
		t.Errorf("expected OrderType 0 (limit), got %d", result.OrderType)
	}
}
