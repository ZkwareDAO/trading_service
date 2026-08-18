package risk_test

import (
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"testing"

	"trading-service/internal/risk"
	"trading-service/internal/risk/aggregator"
	"trading-service/internal/risk/config"
	"trading-service/internal/risk/pipeline"
)

const (
	testUserID         = uint64(100)
	testUserStrategyID = uint64(1000)
	testSymbol         = "BTCUSDT"
	marketOrderType    = 1
	floatEpsilon       = 1e-6
)

func TestE2ERiskFlow_BTCLongPriceDropTriggersReduce(t *testing.T) {
	cfg := loadTestRiskConfig(t)
	positions := aggregateTestBTCOrderPositions(t)

	state := &risk.GlobalState{
		Version: 1,
		Snapshot: &risk.MarketSnapshot{
			Prices:  map[string]map[string]float64{"binance": {testSymbol: 49000.0}},
		},
		Positions: positions,
	}

	results := pipeline.NewRiskPipeline().Run(state, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 pipeline result, got %d", len(results))
	}

	result := results[0]
	if result.Version != 1 {
		t.Fatalf("expected version 1, got %d", result.Version)
	}
	assertFloatNear(t, result.Context.Local.MarkPrice, 49000.0, floatEpsilon)
	assertFloatNear(t, result.Context.Local.PnL, -1000.0, floatEpsilon)
	assertFloatNear(t, result.Context.Local.ROI, -2.0, floatEpsilon)

	if len(result.Rules) != 1 {
		t.Fatalf("expected 1 triggered rule, got %d", len(result.Rules))
	}
	if result.Rules[0].ID != 1 {
		t.Fatalf("expected triggered rule ID 1, got %d", result.Rules[0].ID)
	}
	if result.Rules[0].Action != "reduce" {
		t.Fatalf("expected triggered rule action reduce, got %s", result.Rules[0].Action)
	}

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 action result, got %d", len(result.Results))
	}

	action := result.Results[0]
	if action.ActionType != "reduce" {
		t.Fatalf("expected action type reduce, got %s", action.ActionType)
	}
	if action.UserID != testUserID {
		t.Fatalf("expected action UserID %d, got %d", testUserID, action.UserID)
	}
	if action.UserStrategyID != testUserStrategyID {
		t.Fatalf("expected action UserStrategyID %d, got %d", testUserStrategyID, action.UserStrategyID)
	}
	if action.Symbol != testSymbol {
		t.Fatalf("expected action symbol %s, got %s", testSymbol, action.Symbol)
	}
	if action.Side != risk.SideLong {
		t.Fatalf("expected action long side, got %d", action.Side)
	}
	assertFloatNear(t, action.Quantity, 1.0, floatEpsilon)
	assertFloatNear(t, action.QuantityPercent, 1.0, floatEpsilon)
	assertFloatNear(t, action.RemainingQuantity, 0.0, floatEpsilon)
	if action.OrderType != marketOrderType {
		t.Fatalf("expected market order type %d, got %d", marketOrderType, action.OrderType)
	}
}

func TestE2ERiskFlow_BTCLongStablePriceDoesNotTriggerReduce(t *testing.T) {
	cfg := loadTestRiskConfig(t)
	positions := aggregateTestBTCOrderPositions(t)

	state := &risk.GlobalState{
		Version: 2,
		Snapshot: &risk.MarketSnapshot{
			Prices:  map[string]map[string]float64{"binance": {testSymbol: 50000.0}},
		},
		Positions: positions,
	}

	results := pipeline.NewRiskPipeline().Run(state, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 pipeline result, got %d", len(results))
	}

	result := results[0]
	assertFloatNear(t, result.Context.Local.MarkPrice, 50000.0, floatEpsilon)
	assertFloatNear(t, result.Context.Local.PnL, 0.0, floatEpsilon)
	assertFloatNear(t, result.Context.Local.ROI, 0.0, floatEpsilon)
	if len(result.Rules) != 0 {
		t.Fatalf("expected no triggered rules, got %d", len(result.Rules))
	}
	if len(result.Results) != 0 {
		t.Fatalf("expected no action results, got %d", len(result.Results))
	}
}

func aggregateTestBTCOrderPositions(t *testing.T) []*risk.UserPosition {
	t.Helper()

	orderPositions := []risk.UserOrderPosition{
		{
			ID:             1,
			UserID:         testUserID,
			UserOrderID:    200,
			UserStrategyID: testUserStrategyID,
			Exchange:       "binance",
			PosType:        risk.PosTypeFutures,
			Symbol:         testSymbol,
			Side:           risk.SideLong,
			Quantity:       1.0,
			PosPrice:       50000.0,
			CurrentPrice:   50000.0,
			Leverage:       10,
			InitMargin:     5000.0,
			Deleted:        0,
		},
	}

	positions := aggregator.NewPositionAggregator().Aggregate(orderPositions)
	if len(positions) != 1 {
		t.Fatalf("expected 1 aggregated user position, got %d", len(positions))
	}

	pos := positions[0]
	if pos.UserID != testUserID {
		t.Fatalf("expected UserID %d, got %d", testUserID, pos.UserID)
	}
	if pos.UserStrategyID != testUserStrategyID {
		t.Fatalf("expected UserStrategyID %d, got %d", testUserStrategyID, pos.UserStrategyID)
	}
	if pos.Symbol != testSymbol {
		t.Fatalf("expected symbol %s, got %s", testSymbol, pos.Symbol)
	}
	if pos.Side != risk.SideLong {
		t.Fatalf("expected long side, got %d", pos.Side)
	}
	assertFloatNear(t, pos.Quantity, 1.0, floatEpsilon)
	assertFloatNear(t, pos.TotalMargin, 5000.0, floatEpsilon)
	assertFloatNear(t, pos.CurrentPrice, 50000.0, floatEpsilon)

	return positions
}

func loadTestRiskConfig(t *testing.T) *config.Config {
	t.Helper()

	ruleDir := t.TempDir()
	writeRiskRuleCSV(t, ruleDir)

	cfg, err := config.NewConfigLoader(ruleDir).LoadAll()
	if err != nil {
		t.Fatalf("load risk config: %v", err)
	}

	rules := cfg.GetRulesByStrategy(testUserStrategyID)
	if len(rules) != 1 {
		t.Fatalf("expected 1 active rule for strategy %d, got %d", testUserStrategyID, len(rules))
	}
	if rules[0].Action != "reduce" {
		t.Fatalf("expected reduce action, got %s", rules[0].Action)
	}

	return cfg
}

func writeRiskRuleCSV(t *testing.T, dir string) {
	t.Helper()

	file, err := os.Create(filepath.Join(dir, "rule.csv"))
	if err != nil {
		t.Fatalf("create rule.csv: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	records := [][]string{
		{"id", "user_strategy_id", "condition_name", "operator", "value", "sort", "status", "action", "params"},
		{"1", "1000", "roi", "<=", "-0.02", "1", "active", "reduce", `{"order_type":1,"quantity_pct":1.0}`},
	}
	if err := writer.WriteAll(records); err != nil {
		t.Fatalf("write rule.csv: %v", err)
	}
	if err := writer.Error(); err != nil {
		t.Fatalf("flush rule.csv: %v", err)
	}
}

func assertFloatNear(t *testing.T, got, want, epsilon float64) {
	t.Helper()

	if math.Abs(got-want) > epsilon {
		t.Fatalf("expected %.12f, got %.12f", want, got)
	}
}
