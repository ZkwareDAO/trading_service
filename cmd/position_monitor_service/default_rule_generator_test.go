package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk"
	"trading-service/internal/risk/aggregator"
	"trading-service/internal/risk/config"
)

// requireRuleValue asserts the value of the rule identified by condition name and
// operator. Rules are looked up by identity rather than by slice position, because
// the order of SnapshotConfig().Rules follows CSV row order and is not guaranteed.
func requireRuleValue(t *testing.T, rules []risk.Rule, conditionName, operator string, expected float64) {
	t.Helper()
	for _, rule := range rules {
		if rule.ConditionName == conditionName && rule.Operator == operator {
			if rule.Value != expected {
				t.Errorf("expected %s %s value %v, got %v", conditionName, operator, expected, rule.Value)
			}
			return
		}
	}
	t.Errorf("rule %s %s not found in %+v", conditionName, operator, rules)
}

func setupTestRuleStore(t *testing.T) (*config.RuleStore, func()) {
	t.Helper()
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, "rule.csv"))
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n")
	f.Close()

	store, err := config.NewRuleStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store, func() {}
}

func TestGenerateForMissingStrategies_GeneratesWhenExistingRulesAllInactive(t *testing.T) {
	rules, cleanup := setupTestRuleStore(t)
	defer cleanup()

	if err := rules.AddRules([]risk.Rule{
		{ID: 5, UserStrategyID: 885, ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Status: config.RuleStatusInactive, Action: "reduce"},
		{ID: 6, UserStrategyID: 885, ConditionName: "roi", Operator: ">=", Value: 0.05, Sort: 2, Status: config.RuleStatusInactive, Action: "7"},
		{ID: 7, UserStrategyID: 885, ConditionName: "profit_drawdown_pct", Operator: ">=", Value: 0.05, Sort: 1, Status: config.RuleStatusInactive, Action: "reduce"},
	}); err != nil {
		t.Fatal(err)
	}

	gen := NewDefaultRuleGeneratorWithRepo(rules, nil)
	gen.GenerateForMissingStrategies([]*aggregator.PositionWithMetrics{{
		Position: &risk.UserPosition{
			UserStrategyID: 885,
			Exchange:       "binance", // Must set exchange to not be skipped
		},
	}})

	// Use ListRules() instead of SnapshotConfig() to avoid CSV reload issues
	allRules := rules.ListRules()
	if len(allRules) != 6 {
		t.Fatalf("expected existing inactive rules plus 3 new defaults, got %d", len(allRules))
	}

	var generated []risk.Rule
	for _, rule := range allRules {
		if rule.UserStrategyID == 885 && rule.ID > 7 {
			generated = append(generated, rule)
		}
	}
	if len(generated) != 3 {
		t.Fatalf("expected 3 generated rules with IDs > 7, got %d: %+v", len(generated), generated)
	}
	if generated[0].ID != 8 || generated[1].ID != 9 || generated[2].ID != 10 {
		t.Fatalf("expected generated IDs 8,9,10 got %d,%d,%d", generated[0].ID, generated[1].ID, generated[2].ID)
	}
	if generated[2].Operator != ">=" {
		t.Fatalf("expected generated profit_drawdown_pct operator >=, got %s", generated[2].Operator)
	}
}

func TestGenerateForStrategy_UsesSignalParams(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	now := time.Now()

	paramsJSON, _ := json.Marshal(map[string]interface{}{
		"StopLossThreshold":                -0.05,
		"TakeProfitBackThreshold":          0.03,
		"TakeProfitBackDynamicFallPercent": 0.1,
	})
	stratID := repo.CreateStrategy(&order.Strategy{
		Name:         "TEST_STRAT",
		StrategyType: "CTAFutureFactory",
		Description:  "Test",
		Params:       string(paramsJSON),
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     1,
		Name:       "TEST_STRAT",
		Exchange:   "binance",
		Status:     1,
		StrategyID: stratID,
		CreatedAt:  now,
		UpdatedAt:  now,
	})

	rules, cleanup := setupTestRuleStore(t)
	defer cleanup()

	gen := NewDefaultRuleGeneratorWithRepo(rules, repo)
	gen.GenerateForStrategy(usID)

	ruleCfg := rules.SnapshotConfig()
	if len(ruleCfg.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(ruleCfg.Rules))
	}

	requireRuleValue(t, ruleCfg.Rules, "roi", "<=", -0.05)
	requireRuleValue(t, ruleCfg.Rules, "roi", ">=", 0.03)
	requireRuleValue(t, ruleCfg.Rules, "profit_drawdown_pct", ">=", 0.1)
}

func TestGenerateForStrategy_FallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	now := time.Now()

	stratID := repo.CreateStrategy(&order.Strategy{
		Name:         "EMPTY_STRAT",
		StrategyType: "CTAFutureFactory",
		Description:  "Test",
		Params:       `{}`,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     1,
		Name:       "EMPTY_STRAT",
		Exchange:   "binance",
		Status:     1,
		StrategyID: stratID,
		CreatedAt:  now,
		UpdatedAt:  now,
	})

	rules, cleanup := setupTestRuleStore(t)
	defer cleanup()

	gen := NewDefaultRuleGeneratorWithRepo(rules, repo)
	gen.GenerateForStrategy(usID)

	ruleCfg := rules.SnapshotConfig()
	if len(ruleCfg.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(ruleCfg.Rules))
	}

	requireRuleValue(t, ruleCfg.Rules, "roi", "<=", -0.02)
	requireRuleValue(t, ruleCfg.Rules, "roi", ">=", 0.05)
	requireRuleValue(t, ruleCfg.Rules, "profit_drawdown_pct", ">=", 0.05)
}

func TestGenerateForStrategy_NoRepoUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	now := time.Now()

	paramsJSON, _ := json.Marshal(map[string]interface{}{
		"StopLossThreshold": -0.08,
	})
	stratID := repo.CreateStrategy(&order.Strategy{
		Name:         "NOREPO_STRAT",
		StrategyType: "CTAFutureFactory",
		Description:  "Test",
		Params:       string(paramsJSON),
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     1,
		Name:       "NOREPO_STRAT",
		Exchange:   "binance",
		Status:     1,
		StrategyID: stratID,
		CreatedAt:  now,
		UpdatedAt:  now,
	})

	rules, cleanup := setupTestRuleStore(t)
	defer cleanup()

	gen := NewDefaultRuleGeneratorWithRepo(rules, nil)
	gen.GenerateForStrategy(usID)

	ruleCfg := rules.SnapshotConfig()
	if len(ruleCfg.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(ruleCfg.Rules))
	}

	requireRuleValue(t, ruleCfg.Rules, "roi", "<=", -0.02)
}

// TestGenerateForMissingStrategies_SkipDeribit verifies that default rules
// are NOT generated for Deribit positions.
func TestGenerateForMissingStrategies_SkipDeribit(t *testing.T) {
	rules, cleanup := setupTestRuleStore(t)
	defer cleanup()

	gen := NewDefaultRuleGeneratorWithRepo(rules, nil)

	// Deribit position - should skip default rule generation
	gen.GenerateForMissingStrategies([]*aggregator.PositionWithMetrics{{
		Position: &risk.UserPosition{
			UserStrategyID: 999,
			Exchange:       "deribit",
		},
	}})

	// Verify no rules were generated for Deribit
	ruleCfg := rules.SnapshotConfig()
	if len(ruleCfg.Rules) != 0 {
		t.Errorf("expected 0 rules for deribit position, got %d", len(ruleCfg.Rules))
	}
}

// TestGenerateForMissingStrategies_GenerateForBinance verifies that default rules
// ARE generated for Binance positions (non-Deribit).
func TestGenerateForMissingStrategies_GenerateForBinance(t *testing.T) {
	rules, cleanup := setupTestRuleStore(t)
	defer cleanup()

	gen := NewDefaultRuleGeneratorWithRepo(rules, nil)

	// Binance position - should generate default rules
	gen.GenerateForMissingStrategies([]*aggregator.PositionWithMetrics{{
		Position: &risk.UserPosition{
			UserStrategyID: 1001,
			Exchange:       "binance",
		},
	}})

	// Verify 3 default rules were generated
	ruleCfg := rules.SnapshotConfig()
	if len(ruleCfg.Rules) != 3 {
		t.Errorf("expected 3 rules for binance position, got %d", len(ruleCfg.Rules))
	}

	// Verify all rules belong to the correct strategy
	for _, rule := range ruleCfg.Rules {
		if rule.UserStrategyID != 1001 {
			t.Errorf("expected rule for strategy 1001, got %d", rule.UserStrategyID)
		}
	}
}

// newRepoWithUserStrategy creates a repo holding one user strategy with the given
// risk_strategy_type, returning the repo and the generated user_strategy id.
func newRepoWithUserStrategy(t *testing.T, exchange, riskStrategyType string) (*persistence.StateRepository, uint64) {
	t.Helper()
	gs, err := persistence.NewGlobalState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gs.Shutdown)
	repo := persistence.NewStateRepository(gs)

	now := time.Now()
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:           1,
		Name:             "SIGNAL_STRAT",
		Exchange:         exchange,
		Status:           1,
		RiskStrategyType: riskStrategyType,
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	return repo, usID
}

// TestGenerateForMissingStrategies_RiskStrategyType verifies which strategies are
// excluded from default rule generation: deribit positions and signal_close
// strategies. Strategies that cannot be resolved keep generating rules as before.
func TestGenerateForMissingStrategies_RiskStrategyType(t *testing.T) {
	testCases := []struct {
		name             string
		exchange         string
		riskStrategyType string
		useUnknownID     bool
		expectedRules    int
	}{
		{
			name:             "signal_close skips generation",
			exchange:         "binance",
			riskStrategyType: order.RiskStrategyTypeSignalClose,
			expectedRules:    0,
		},
		{
			name:             "traditional still generates",
			exchange:         "binance",
			riskStrategyType: order.RiskStrategyTypeTraditional,
			expectedRules:    3,
		},
		{
			name:             "deribit skips even when signal_close",
			exchange:         "deribit",
			riskStrategyType: order.RiskStrategyTypeSignalClose,
			expectedRules:    0,
		},
		{
			name:             "unresolved strategy still generates",
			exchange:         "binance",
			riskStrategyType: order.RiskStrategyTypeSignalClose,
			useUnknownID:     true,
			expectedRules:    3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rules, cleanup := setupTestRuleStore(t)
			defer cleanup()

			repo, usID := newRepoWithUserStrategy(t, tc.exchange, tc.riskStrategyType)
			if tc.useUnknownID {
				usID = 987654 // not present in repo
			}

			gen := NewDefaultRuleGeneratorWithRepo(rules, repo)
			gen.GenerateForMissingStrategies([]*aggregator.PositionWithMetrics{{
				Position: &risk.UserPosition{
					UserStrategyID: usID,
					Exchange:       tc.exchange,
				},
			}})

			if got := len(rules.ListRules()); got != tc.expectedRules {
				t.Errorf("expected %d rules, got %d", tc.expectedRules, got)
			}
		})
	}
}

// TestGenerateForMissingStrategies_MixedExchanges verifies behavior
// with mixed exchanges (Deribit + Binance).
func TestGenerateForMissingStrategies_MixedExchanges(t *testing.T) {
	rules, cleanup := setupTestRuleStore(t)
	defer cleanup()

	gen := NewDefaultRuleGeneratorWithRepo(rules, nil)

	// Mixed: 1 Deribit (skip) + 1 Binance (generate)
	gen.GenerateForMissingStrategies([]*aggregator.PositionWithMetrics{
		{
			Position: &risk.UserPosition{
				UserStrategyID: 2001,
				Exchange:       "deribit",
			},
		},
		{
			Position: &risk.UserPosition{
				UserStrategyID: 2002,
				Exchange:       "binance",
			},
		},
	})

	// Verify only 3 rules generated (for Binance, not Deribit)
	ruleCfg := rules.SnapshotConfig()
	if len(ruleCfg.Rules) != 3 {
		t.Errorf("expected 3 rules (only for binance), got %d", len(ruleCfg.Rules))
	}

	// Verify all rules belong to Binance strategy
	for _, rule := range ruleCfg.Rules {
		if rule.UserStrategyID != 2002 {
			t.Errorf("expected all rules for strategy 2002 (binance), got strategy %d", rule.UserStrategyID)
		}
	}
}
