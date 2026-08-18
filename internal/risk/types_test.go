package risk

import (
	"testing"
	"time"
)

// Test 1: GlobalState 应该包含 Version、Snapshot、Metrics、Positions
func TestGlobalState_Structure(t *testing.T) {
	state := GlobalState{
		Version: 1,
		Snapshot: &MarketSnapshot{
			Prices:  map[string]map[string]float64{"binance": {"BTCUSDT": 50000.0}},
			Funding: map[string]float64{"BTCUSDT": 0.0001},
		},
		Metrics: &GlobalMetrics{
			BTCVolatility: 0.05,
			MarketTrend:   0.02,
		},
		Positions: []*UserPosition{
			{
				ID:             1,
				UserID:         100,
				UserStrategyID: 1000,
				Symbol:         "BTCUSDT",
				Side:           SideLong,
				Quantity:       1.0,
				Deleted:        0,
			},
		},
	}

	if state.Version != 1 {
		t.Errorf("expected Version 1, got %d", state.Version)
	}
	if state.Snapshot == nil {
		t.Error("expected Snapshot not to be nil")
	}
	if state.Metrics == nil {
		t.Error("expected Metrics not to be nil")
	}
	if len(state.Positions) != 1 {
		t.Errorf("expected 1 position, got %d", len(state.Positions))
	}
}

// Test 2: UserPosition 应该包含所有必要字段
func TestUserPosition_Structure(t *testing.T) {
	now := time.Now()
	pos := UserPosition{
		ID:                    1,
		UserID:                100,
		UserStrategyID:        1000,
		RiskControlStrategyID: 0,
		Exchange:              "binance",
		PosType:               PosTypeFutures,
		Symbol:                "BTCUSDT",
		Side:                  SideLong,
		CurrentPrice:          50000.0,
		Quantity:              1.0,
		TotalMargin:           50000.0,
		Deleted:               0,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	if pos.Symbol != "BTCUSDT" {
		t.Errorf("expected BTCUSDT, got %s", pos.Symbol)
	}
	if pos.Side != SideLong {
		t.Errorf("expected SideLong (0), got %d", pos.Side)
	}
	if pos.PosType != PosTypeFutures {
		t.Errorf("expected PosTypeFutures (2), got %d", pos.PosType)
	}
}

// Test 3: UserOrderPosition 应该包含订单层仓位字段
func TestUserOrderPosition_Structure(t *testing.T) {
	pos := UserOrderPosition{
		ID:              1,
		UserID:          100,
		UserOrderID:     500,
		UserStrategyID:  1000,
		Exchange:        "binance",
		PosType:         PosTypeFutures,
		Symbol:          "BTCUSDT",
		Side:            SideLong,
		Quantity:        0.5,
		PosPrice:        49000.0,
		Leverage:        10,
		InitMargin:      24500.0,
		Deleted:         0,
		UprunningOrderID: 12345,
	}

	if pos.UserOrderID != 500 {
		t.Errorf("expected UserOrderID 500, got %d", pos.UserOrderID)
	}
	if pos.Leverage != 10 {
		t.Errorf("expected Leverage 10, got %d", pos.Leverage)
	}
}

// Test 4: RiskSignal 应该只包含 Version
func TestRiskSignal_Structure(t *testing.T) {
	signal := RiskSignal{
		Version: 42,
	}

	if signal.Version != 42 {
		t.Errorf("expected Version 42, got %d", signal.Version)
	}
}

// Test 5: RiskContext 应该聚合 Position、Local、Global、Market
func TestRiskContext_Structure(t *testing.T) {
	ctx := RiskContext{
		Position: &UserPosition{
			ID:     1,
			Symbol: "BTCUSDT",
		},
		Local: LocalMetrics{
			ROI:           0.15,
			PnL:           7500.0,
			MaxProfitPct:  0.20,
			MaxDrawdownPct: 0.05,
		},
		Global: GlobalMetrics{
			BTCVolatility: 0.05,
		},
		Market: &MarketSnapshot{
			Prices: map[string]map[string]float64{"binance": {"BTCUSDT": 50000.0}},
		},
	}

	if ctx.Position == nil {
		t.Error("expected Position not to be nil")
	}
	if ctx.Local.ROI != 0.15 {
		t.Errorf("expected ROI 0.15, got %f", ctx.Local.ROI)
	}
}

// Test 6: LocalMetrics 应该包含实时计算的指标
func TestLocalMetrics_Structure(t *testing.T) {
	metrics := LocalMetrics{
		ROI:            0.15,
		PnL:            7500.0,
		MaxProfitPct:   0.20,
		MaxDrawdownPct: 0.05,
		UnrealizedPnL:  5000.0,
		RealizedPnL:    2500.0,
		EntryPrice:     45000.0,
		MarkPrice:      50000.0,
		DurationSec:    3600,
	}

	if metrics.ROI != 0.15 {
		t.Errorf("expected ROI 0.15, got %f", metrics.ROI)
	}
	if metrics.MarkPrice != 50000.0 {
		t.Errorf("expected MarkPrice 50000.0, got %f", metrics.MarkPrice)
	}
}

// Test 7: Rule 配置模型（新格式）
func TestConfigModels_Structure(t *testing.T) {
	rule := Rule{
		ID:             1,
		UserStrategyID: 1000,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          -0.02,
		Sort:           1,
		Status:         "active",
		Action:         "reduce",
		Params:         map[string]interface{}{"order_type": 1, "quantity_pct": 1.0},
	}

	if rule.ID != 1 {
		t.Errorf("expected ID 1, got %d", rule.ID)
	}
	if rule.ConditionName != "roi" {
		t.Errorf("expected 'roi', got '%s'", rule.ConditionName)
	}
	if rule.Action != "reduce" {
		t.Errorf("expected 'reduce', got '%s'", rule.Action)
	}
	if rule.Params["quantity_pct"] != 1.0 {
		t.Errorf("expected quantity_pct 1.0, got %v", rule.Params["quantity_pct"])
	}
}