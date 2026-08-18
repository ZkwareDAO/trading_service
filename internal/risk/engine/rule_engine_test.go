package engine

import (
	"testing"

	"trading-service/internal/risk"
)

func TestEvaluateCondition_AlwaysTrue(t *testing.T) {
	engine := NewRuleEngine()
	ctx := &risk.RiskContext{}
	rule := risk.Rule{ConditionName: "always", Operator: "==", Value: true}
	if !engine.EvaluateCondition(rule, ctx) {
		t.Error("expected always == true to trigger")
	}
}

func TestEvaluateCondition_AlwaysNotEqualFalse(t *testing.T) {
	engine := NewRuleEngine()
	ctx := &risk.RiskContext{}
	rule := risk.Rule{ConditionName: "always", Operator: "!=", Value: true}
	if engine.EvaluateCondition(rule, ctx) {
		t.Error("expected always != true not to trigger")
	}
}

func TestEvaluateCondition_ROI(t *testing.T) {
	engine := NewRuleEngine()
	roi := -0.03
	ctx := &risk.RiskContext{Local: risk.LocalMetrics{ROI: roi}}
	rule := risk.Rule{ConditionName: "roi", Operator: "<=", Value: -0.02}
	if !engine.EvaluateCondition(rule, ctx) {
		t.Error("expected ROI -0.03 <= -0.02 to be true")
	}
}

func TestEvaluateCondition_PriceBTC(t *testing.T) {
	engine := NewRuleEngine()
	price := 75000.0
	ctx := &risk.RiskContext{
		Market: &risk.MarketSnapshot{Prices: map[string]map[string]float64{"binance": {"BTCUSDT": price}}},
	}
	rule := risk.Rule{ConditionName: "price_btc", Operator: ">=", Value: 72000}
	if !engine.EvaluateCondition(rule, ctx) {
		t.Error("expected BTC >= 72000 to be true")
	}
}

func TestEvaluateCondition_ProfitDrawdown(t *testing.T) {
	engine := NewRuleEngine()
	ctx := &risk.RiskContext{Local: risk.LocalMetrics{ProfitDrawdownPct: 0.03}}
	rule := risk.Rule{ConditionName: "profit_drawdown_pct", Operator: "<=", Value: 0.05}
	if !engine.EvaluateCondition(rule, ctx) {
		t.Error("expected profit_drawdown 0.03 <= 0.05 to be true")
	}
}

func TestEvaluateCondition_Operators(t *testing.T) {
	engine := NewRuleEngine()
	roi := 0.1
	tests := []struct {
		op       string
		value    float64
		expected bool
	}{
		{">", 0.05, true}, {">=", 0.1, true}, {">=", 0.15, false},
		{"<", 0.15, true}, {"<=", 0.1, true}, {"<=", 0.05, false},
		{"==", 0.1, true}, {"!=", 0.1, false}, {"!=", 0.15, true},
	}
	for _, tt := range tests {
		ctx := &risk.RiskContext{Local: risk.LocalMetrics{ROI: roi}}
		rule := risk.Rule{ConditionName: "roi", Operator: tt.op, Value: tt.value}
		if engine.EvaluateCondition(rule, ctx) != tt.expected {
			t.Errorf("ROI %.1f %s %.2f: expected %v", roi, tt.op, tt.value, tt.expected)
		}
	}
}

func TestEvaluateRules_SortByPriority(t *testing.T) {
	engine := NewRuleEngine()
	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{UserStrategyID: 1000},
		Local:    risk.LocalMetrics{ROI: -0.03},
	}
	rules := []risk.Rule{
		{ID: 2, UserStrategyID: 1000, Status: "active", ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 2, Action: "reduce"},
		{ID: 1, UserStrategyID: 1000, Status: "active", ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Action: "reduce"},
		{ID: 3, UserStrategyID: 1000, Status: "inactive", ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Action: "reduce"},
	}
	triggered := engine.EvaluateRules(rules, ctx)
	if len(triggered) != 2 {
		t.Fatalf("expected 2 triggered, got %d", len(triggered))
	}
	if triggered[0].ID != 1 {
		t.Errorf("expected first=ID1 (highest priority), got %d", triggered[0].ID)
	}
}

func TestEvaluateRules_DisabledRules(t *testing.T) {
	engine := NewRuleEngine()
	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{UserStrategyID: 1000},
		Local:    risk.LocalMetrics{ROI: -0.03},
	}
	rules := []risk.Rule{{ID: 1, UserStrategyID: 1000, Status: "inactive", ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Action: "reduce"}}
	if len(engine.EvaluateRules(rules, ctx)) != 0 {
		t.Error("expected 0 for inactive rule")
	}
}

func TestEvaluateRules_StrategyFilter(t *testing.T) {
	// Strategy filtering is done at Config.GetRulesByStrategy level.
	// Engine evaluates whatever rules are passed.
	engine := NewRuleEngine()
	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{UserStrategyID: 1000},
		Local:    risk.LocalMetrics{ROI: -0.03},
	}
	rules := []risk.Rule{{ID: 1, UserStrategyID: 1000, Status: "active", ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Action: "reduce"}}
	triggered := engine.EvaluateRules(rules, ctx)
	if len(triggered) != 1 {
		t.Errorf("expected 1 triggered rule, got %d", len(triggered))
	}
}

func TestEvaluateCondition_UnknownField(t *testing.T) {
	engine := NewRuleEngine()
	ctx := &risk.RiskContext{Local: risk.LocalMetrics{ROI: 0.1}}
	rule := risk.Rule{ConditionName: "unknown", Operator: ">", Value: 0}
	if engine.EvaluateCondition(rule, ctx) {
		t.Error("expected false for unknown field")
	}
}

func TestEvaluateRules_NilPosition(t *testing.T) {
	engine := NewRuleEngine()
	rules := []risk.Rule{{ID: 1, UserStrategyID: 1000, Status: "active"}}
	if len(engine.EvaluateRules(rules, &risk.RiskContext{})) != 0 {
		t.Error("expected 0 for nil position")
	}
}

func TestRuleScheduler_ActivateDeactivate(t *testing.T) {
	rules := []risk.Rule{{ID: 1, Status: "active"}, {ID: 2, Status: "inactive"}}
	sched := NewRuleScheduler(rules)
	sched.DeactivateRule(1)
	sched.ActivateRule(2)
	if sched.rules[1].Status != "inactive" {
		t.Error("expected rule 1 inactive")
	}
	if sched.rules[2].Status != "active" {
		t.Error("expected rule 2 active")
	}
}

func TestRuleScheduler_DeactivateAll(t *testing.T) {
	rules := []risk.Rule{
		{ID: 1, UserStrategyID: 1000, Status: "active"},
		{ID: 2, UserStrategyID: 1000, Status: "active"},
		{ID: 3, UserStrategyID: 2000, Status: "active"},
	}
	sched := NewRuleScheduler(rules)
	sched.DeactivateAllForStrategy(1000)
	if sched.rules[1].Status != "inactive" || sched.rules[2].Status != "inactive" {
		t.Error("expected rules 1,2 inactive")
	}
	if sched.rules[3].Status != "active" {
		t.Error("expected rule 3 still active")
	}
}

func TestDefaultRuleGenerator(t *testing.T) {
	gen := NewDefaultRuleGenerator(100)
	rules := gen.GenerateDefaultRules(1000)
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules (stop-loss, profit-trigger, profit-followup), got %d", len(rules))
	}
	if rules[0].ID != 100 || rules[1].ID != 101 || rules[2].ID != 102 {
		t.Errorf("expected IDs 100,101,102, got %d,%d,%d", rules[0].ID, rules[1].ID, rules[2].ID)
	}
	if rules[0].ConditionName != "roi" || rules[0].Action != "reduce" {
		t.Errorf("expected first rule: roi stop-loss, got %+v", rules[0])
	}
	if rules[1].Action != "102" {
		t.Errorf("expected profit-trigger to chain to rule 102, got '%s'", rules[1].Action)
	}
	if rules[2].Status != "inactive" || rules[2].ConditionName != "profit_drawdown_pct" || rules[2].Operator != ">=" {
		t.Errorf("expected follow-up rule: inactive, profit_drawdown_pct >=, got %+v", rules[2])
	}
}

// TestEvaluateRules_SkipInUse verifies that in_use rules are skipped
func TestEvaluateRules_SkipInUse(t *testing.T) {
	engine := NewRuleEngine()
	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{UserStrategyID: 1000},
		Local:    risk.LocalMetrics{ROI: -0.03},
	}
	rules := []risk.Rule{
		{ID: 1, UserStrategyID: 1000, Status: "active", ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Action: "reduce"},
		{ID: 2, UserStrategyID: 1000, Status: "in_use", ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Action: "reduce"},
	}
	triggered := engine.EvaluateRules(rules, ctx)
	if len(triggered) != 1 {
		t.Fatalf("expected 1 triggered (active only), got %d", len(triggered))
	}
	if triggered[0].ID != 1 {
		t.Errorf("expected triggered rule ID=1 (active), got ID=%d", triggered[0].ID)
	}
}

// TestRuleScheduler_InUseRule verifies the active → in_use transition
func TestRuleScheduler_InUseRule(t *testing.T) {
	rules := []risk.Rule{{ID: 1, Status: "active"}, {ID: 2, Status: "in_use"}, {ID: 3, Status: "inactive"}}
	sched := NewRuleScheduler(rules)

	// active → in_use
	sched.InUseRule(1)
	if sched.rules[1].Status != "in_use" {
		t.Errorf("expected rule 1 in_use, got %s", sched.rules[1].Status)
	}

	// in_use → in_use (should not change)
	sched.InUseRule(2)
	if sched.rules[2].Status != "in_use" {
		t.Errorf("expected rule 2 still in_use, got %s", sched.rules[2].Status)
	}

	// inactive → in_use (should not change)
	sched.InUseRule(3)
	if sched.rules[3].Status != "inactive" {
		t.Errorf("expected rule 3 still inactive, got %s", sched.rules[3].Status)
	}
}

// TestEvaluateCondition_PriceSOL verifies price_sol queries SOLUSDT price
func TestEvaluateCondition_PriceSOL(t *testing.T) {
	engine := NewRuleEngine()
	price := 150.0
	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{UserStrategyID: 1000},
		Market:   &risk.MarketSnapshot{Prices: map[string]map[string]float64{"binance": {"SOLUSDT": price}}},
	}
	rule := risk.Rule{ConditionName: "price_sol", Operator: ">=", Value: 100}
	if !engine.EvaluateCondition(rule, ctx) {
		t.Error("expected SOL >= 100 to be true")
	}
}

// TestEvaluateCondition_PriceGeneric verifies price_xxx maps to XXXUSDT
func TestEvaluateCondition_PriceGeneric(t *testing.T) {
	engine := NewRuleEngine()
	tests := []struct {
		field   string
		symbol  string
		value   float64
		op      string
		ruleVal float64
		expect  bool
	}{
		{"price_sui", "SUIUSDT", 3.5, ">=", 2.0, true},
		{"price_ada", "ADAUSDT", 0.4, "<", 1.0, true},
		{"price_doge", "DOGEUSDT", 0.15, "==", 0.15, true},
		{"price_dot", "DOTUSDT", 7.0, ">", 10.0, false},
	}
	for _, tt := range tests {
		ctx := &risk.RiskContext{
			Position: &risk.UserPosition{UserStrategyID: 1000},
			Market:   &risk.MarketSnapshot{Prices: map[string]map[string]float64{"binance": {tt.symbol: tt.value}}},
		}
		rule := risk.Rule{ConditionName: tt.field, Operator: tt.op, Value: tt.ruleVal}
		if engine.EvaluateCondition(rule, ctx) != tt.expect {
			t.Errorf("field=%s symbol=%s price=%.2f: expected %v", tt.field, tt.symbol, tt.value, tt.expect)
		}
	}
}

// TestEvaluateCondition_HoldingTime verifies holding_time (duration in seconds) condition
func TestEvaluateCondition_HoldingTime(t *testing.T) {
	engine := NewRuleEngine()
	// 72 hours = 259200 seconds
	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{UserStrategyID: 1000},
		Local:    risk.LocalMetrics{DurationSec: 260000}, // slightly over 72h
	}
	rule := risk.Rule{ConditionName: "holding_time", Operator: ">", Value: 259200}
	if !engine.EvaluateCondition(rule, ctx) {
		t.Error("expected holding_time > 259200 to be true (260000 > 259200)")
	}

	// Under 72h should not trigger
	ctx.Local.DurationSec = 250000
	if engine.EvaluateCondition(rule, ctx) {
		t.Error("expected holding_time > 259200 to be false (250000 < 259200)")
	}
}
