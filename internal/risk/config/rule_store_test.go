package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"trading-service/internal/risk"
)

func setupRuleStore(t *testing.T) (*RuleStore, func()) {
	t.Helper()
	dir := t.TempDir()

	// Write initial rules to CSV
	rules := []risk.Rule{
		{ID: 1, UserStrategyID: 100, ConditionName: "roi", Operator: "<=", Value: -0.02, Status: "active", Action: "reduce"},
		{ID: 2, UserStrategyID: 100, ConditionName: "roi", Operator: ">=", Value: 0.05, Status: "active", Action: "3"},
		{ID: 3, UserStrategyID: 100, ConditionName: "profit_drawdown_pct", Operator: "<=", Value: 0.05, Status: "inactive", Action: "reduce"},
		{ID: 4, UserStrategyID: 200, ConditionName: "roi", Operator: "<=", Value: -0.02, Status: "active", Action: "reduce"},
	}

	// Write CSV manually
	f, err := os.Create(filepath.Join(dir, "rule.csv"))
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n")
	for _, r := range rules {
		f.WriteString(fmt.Sprintf("%d,%d,%s,%s,%v,1,%s,%s,\n", r.ID, r.UserStrategyID, r.ConditionName, r.Operator, r.Value, r.Status, r.Action))
	}
	f.Close()

	loader := NewConfigLoader(dir)
	cfg, err := loader.LoadAll()
	if err != nil {
		t.Fatal(err)
	}

	ruleMap := make(map[int]*risk.Rule)
	for i := range cfg.Rules {
		ruleMap[cfg.Rules[i].ID] = &cfg.Rules[i]
	}

	store := &RuleStore{dataDir: dir, rules: ruleMap}
	return store, func() {}
}

func TestSnapshotConfigRefreshesInMemoryRules(t *testing.T) {
	store, _ := setupRuleStore(t)

	path := filepath.Join(store.dataDir, "rule.csv")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("24,100,always,==,true,1,active,reduce,{}\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := store.SnapshotConfig()
	found := false
	for _, rule := range cfg.Rules {
		if rule.ID == 24 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected SnapshotConfig to load externally appended rule 24")
	}

	if err := store.UpdateRuleStatus(24, RuleStatusInUse); err != nil {
		t.Fatalf("expected in-memory rules to include rule 24 after SnapshotConfig: %v", err)
	}
	updated, ok := store.GetRule(24)
	if !ok {
		t.Fatal("expected rule 24 after update")
	}
	if updated.Status != RuleStatusInUse {
		t.Fatalf("expected rule 24 status in_use, got %s", updated.Status)
	}
}

func TestResetRulesForStrategy_ResetsAllActiveRules(t *testing.T) {
	store, _ := setupRuleStore(t)

	// Strategy 100 has rules 1 (active), 2 (active), 3 (inactive)
	err := store.ResetRulesForStrategy(100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All rules for strategy 100 should now be inactive
	for id, rule := range store.rules {
		if rule.UserStrategyID == 100 && rule.Status != "inactive" {
			t.Errorf("rule %d should be inactive, got %s", id, rule.Status)
		}
	}

	// Strategy 200 rule should be unchanged
	if store.rules[4].Status != "active" {
		t.Errorf("rule 4 (strategy 200) should remain active, got %s", store.rules[4].Status)
	}
}

func TestResetRulesForStrategy_NoopWhenAllInactive(t *testing.T) {
	store, _ := setupRuleStore(t)

	// First reset
	store.ResetRulesForStrategy(100)

	// Second reset should not error
	err := store.ResetRulesForStrategy(100)
	if err != nil {
		t.Fatalf("second reset should not error: %v", err)
	}
}

func TestResetRulesForStrategy_StrategyNotFound(t *testing.T) {
	store, _ := setupRuleStore(t)

	// Reset non-existent strategy — should not error, just do nothing
	err := store.ResetRulesForStrategy(999)
	if err != nil {
		t.Fatalf("non-existent strategy should not error: %v", err)
	}
}

func TestCreateRule_Concurrent(t *testing.T) {
	store, _ := setupRuleStore(t)

	// Create 20 rules concurrently
	const numGoroutines = 20
	done := make(chan int, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			rule := &risk.Rule{
				UserStrategyID: uint64(1000 + idx),
				ConditionName:  "roi",
				Operator:       "<=",
				Value:          -0.02,
				Status:         "active",
				Action:         "reduce",
			}
			err := store.CreateRule(rule)
			if err != nil {
				t.Errorf("CreateRule failed: %v", err)
				done <- -1
				return
			}
			done <- rule.ID
		}(i)
	}

	// Collect all IDs
	ids := make(map[int]bool)
	for i := 0; i < numGoroutines; i++ {
		id := <-done
		if id == -1 {
			continue
		}
		if ids[id] {
			t.Errorf("duplicate ID detected: %d", id)
		}
		ids[id] = true
	}

	// Verify all IDs are unique and contiguous
	if len(ids) != numGoroutines {
		t.Errorf("expected %d unique IDs, got %d", numGoroutines, len(ids))
	}

	// Verify all rules persisted
	rules := store.ListRules()
	// Initial 4 + 20 new = 24 total
	if len(rules) != 4+numGoroutines {
		t.Errorf("expected %d rules total, got %d", 4+numGoroutines, len(rules))
	}
}
