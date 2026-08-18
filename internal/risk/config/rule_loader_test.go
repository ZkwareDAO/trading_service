package config

import (
	"os"
	"path/filepath"
	"testing"

	"trading-service/internal/risk"
)

func TestLoadRules_NewFormat(t *testing.T) {
	dir, _ := os.MkdirTemp("", "config-test-*")
	defer os.RemoveAll(dir)

	ruleCSV := "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n" +
		"1,1000,roi,<=,-0.02,1,active,reduce,\n" +
		"2,1000,roi,>=,0.05,2,active,3,\n"
	os.WriteFile(filepath.Join(dir, "rule.csv"), []byte(ruleCSV), 0644)

	loader := NewConfigLoader(dir)
	cfg, err := loader.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(cfg.Rules))
	}
	r := cfg.Rules[0]
	if r.ConditionName != "roi" || r.Operator != "<=" || r.Status != "active" || r.Action != "reduce" {
		t.Errorf("unexpected rule: %+v", r)
	}
}

func TestLoadRules_NoFile(t *testing.T) {
	dir, _ := os.MkdirTemp("", "config-test-*")
	defer os.RemoveAll(dir)
	loader := NewConfigLoader(dir)
	cfg, err := loader.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(cfg.Rules))
	}
}

func TestGetRulesByStrategy(t *testing.T) {
	cfg := &Config{
		Rules: []risk.Rule{
			{ID: 1, UserStrategyID: 1000, Status: "active"},
			{ID: 2, UserStrategyID: 1000, Status: "inactive"},
			{ID: 3, UserStrategyID: 2000, Status: "active"},
		},
	}
	rules := cfg.GetRulesByStrategy(1000)
	if len(rules) != 1 {
		t.Errorf("expected 1 active rule for 1000, got %d", len(rules))
	}
}

func TestGetRuleByID(t *testing.T) {
	cfg := &Config{
		Rules: []risk.Rule{
			{ID: 1, Status: "active"},
			{ID: 2, Status: "inactive"},
		},
	}
	rule := cfg.GetRuleByID(2)
	if rule == nil || rule.Status != "inactive" {
		t.Error("expected rule 2 inactive")
	}
	if cfg.GetRuleByID(999) != nil {
		t.Error("expected nil for non-existent")
	}
}

func TestLoadRules_ValueParsing(t *testing.T) {
	dir, _ := os.MkdirTemp("", "config-test-*")
	defer os.RemoveAll(dir)

	ruleCSV := "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n" +
		"1,1000,roi,<=,-0.02,1,active,reduce,\n" +
		"2,1000,price_btc,>=,72000,2,active,reduce,\n" +
		"3,1000,holding_time,>=,2026-06-19,1,active,reduce,\n"
	os.WriteFile(filepath.Join(dir, "rule.csv"), []byte(ruleCSV), 0644)

	loader := NewConfigLoader(dir)
	cfg, err := loader.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := cfg.Rules[0].Value.(float64); !ok || f != -0.02 {
		t.Errorf("expected float -0.02, got %v", cfg.Rules[0].Value)
	}
	// 72000 parses as float64 since it comes from CSV
	if f, ok := cfg.Rules[1].Value.(float64); !ok || f != 72000 {
		t.Errorf("expected float 72000, got %v", cfg.Rules[1].Value)
	}
}

func TestLoadRules_BoolValueParsing(t *testing.T) {
	dir, _ := os.MkdirTemp("", "config-test-*")
	defer os.RemoveAll(dir)

	ruleCSV := "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n" +
		"1,1000,always,==,true,1,active,reduce,\n"
	os.WriteFile(filepath.Join(dir, "rule.csv"), []byte(ruleCSV), 0644)

	loader := NewConfigLoader(dir)
	cfg, err := loader.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := cfg.Rules[0].Value.(bool); !ok || !b {
		t.Errorf("expected bool true, got %T %v", cfg.Rules[0].Value, cfg.Rules[0].Value)
	}
}

func TestLoadRules_DefaultParams(t *testing.T) {
	dir, _ := os.MkdirTemp("", "config-test-*")
	defer os.RemoveAll(dir)

	ruleCSV := "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n" +
		"1,1000,roi,<=,-0.02,1,active,reduce,\n"
	os.WriteFile(filepath.Join(dir, "rule.csv"), []byte(ruleCSV), 0644)

	loader := NewConfigLoader(dir)
	cfg, err := loader.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(cfg.Rules))
	}
	rule := cfg.Rules[0]
	if rule.Action != "reduce" {
		t.Errorf("expected action 'reduce', got '%s'", rule.Action)
	}
}
