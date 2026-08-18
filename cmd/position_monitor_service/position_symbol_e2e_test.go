package main

import (
	"testing"

	"trading-service/internal/risk"
	"trading-service/internal/risk/engine"
)

// TestPositionSymbolRegression_ROI 测试position功能不影响ROI规则
func TestPositionSymbolRegression_ROI(t *testing.T) {
	// 创建RuleEngine
	eng := engine.NewRuleEngine()

	// 创建ROI规则
	rules := []risk.Rule{
		{
			ID:             1,
			UserStrategyID: 100,
			ConditionName:  "roi",
			Operator:       "<=",
			Value:          -0.02,
			Status:         "active",
			Action:         "reduce",
			Params:         map[string]interface{}{"quantity_pct": 1.0},
		},
	}

	// 创建RiskContext with negative ROI
	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{
			UserStrategyID: 100,
		},
		Local: risk.LocalMetrics{
			ROI: -0.03, // 触发ROI止损
		},
	}

	// 执行规则评估
	triggered := eng.EvaluateRules(rules, ctx)

	// 验证ROI规则触发
	if len(triggered) == 0 {
		t.Error("ROI rule should trigger when ROI=-0.03")
	}

	if triggered[0].ConditionName != "roi" {
		t.Errorf("expected roi rule, got %s", triggered[0].ConditionName)
	}

	t.Logf("✅ ROI规则正常触发，position功能不影响现有风控")
}

// TestPositionSymbolRegression_HoldingTime 测试position功能不影响holding_time规则
func TestPositionSymbolRegression_HoldingTime(t *testing.T) {
	eng := engine.NewRuleEngine()

	rules := []risk.Rule{
		{
			ID:             1,
			UserStrategyID: 100,
			ConditionName:  "holding_time",
			Operator:       ">",
			Value:          259200, // 72小时
			Status:         "active",
			Action:         "reduce",
		},
	}

	// 持仓超过72小时
	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{
			UserStrategyID: 100,
		},
		Local: risk.LocalMetrics{
			DurationSec: 300000, // 83小时
		},
	}

	triggered := eng.EvaluateRules(rules, ctx)

	if len(triggered) == 0 {
		t.Error("holding_time rule should trigger when duration > 72h")
	}

	t.Logf("✅ holding_time规则正常触发")
}
