package signal

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"trading-service/internal/risk/config"
)

func TestCloseRuleWriter_AppendsImmediateReduceRule(t *testing.T) {
	dir := t.TempDir()
	ruleStore, err := config.NewRuleStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	writer := NewCloseRuleWriterWithStore(ruleStore)

	ctx := context.Background()
	ruleID, err := writer.AppendImmediateCloseRule(ctx, CloseRuleRequest{
		UserStrategyID: 1000,
		QuantityPct:    1.0,
		OrderType:      1,
	})
	if err != nil {
		t.Fatalf("AppendImmediateCloseRule: %v", err)
	}
	if ruleID != 1 {
		t.Fatalf("expected first rule ID 1, got %d", ruleID)
	}

	records := readRuleCSV(t, dir)
	if len(records) != 2 {
		t.Fatalf("expected header + 1 rule, got %d records", len(records))
	}
	rule := records[1]
	assertRuleField(t, rule, 1, "1000")
	assertRuleField(t, rule, 2, "always")
	assertRuleField(t, rule, 3, "==")
	assertRuleField(t, rule, 4, "true")
	assertRuleField(t, rule, 5, "1")
	assertRuleField(t, rule, 6, "active")
	assertRuleField(t, rule, 7, "reduce")

	var params map[string]interface{}
	if err := json.Unmarshal([]byte(rule[8]), &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["order_type"].(float64) != 1 || params["quantity_pct"].(float64) != 1.0 {
		t.Fatalf("unexpected params: %v", params)
	}
	if _, ok := params["source"]; ok {
		t.Fatalf("params should not include source: %v", params)
	}
	if _, ok := params["signal_action"]; ok {
		t.Fatalf("params should not include signal_action: %v", params)
	}
}

func TestCloseRuleWriter_GeneratesNextID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rule.csv")
	content := "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n" +
		"1,1000,roi,<=,-0.02,1,active,reduce,{}\n" +
		"9,1000,roi,>=,0.05,1,active,reduce,{}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ruleStore, err := config.NewRuleStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	writer := NewCloseRuleWriterWithStore(ruleStore)
	ctx := context.Background()
	ruleID, err := writer.AppendImmediateCloseRule(ctx, CloseRuleRequest{UserStrategyID: 1000, QuantityPct: 1, OrderType: 1})
	if err != nil {
		t.Fatalf("AppendImmediateCloseRule: %v", err)
	}
	if ruleID != 10 {
		t.Fatalf("expected next rule ID 10, got %d", ruleID)
	}
}

func TestCloseRuleWriter_CreatesHeaderWhenMissing(t *testing.T) {
	dir := t.TempDir()
	ruleStore, err := config.NewRuleStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	writer := NewCloseRuleWriterWithStore(ruleStore)
	ctx := context.Background()
	if _, err := writer.AppendImmediateCloseRule(ctx, CloseRuleRequest{UserStrategyID: 1000, QuantityPct: 1, OrderType: 1}); err != nil {
		t.Fatalf("AppendImmediateCloseRule: %v", err)
	}

	records := readRuleCSV(t, dir)
	if got := records[0][0]; got != "id" {
		t.Fatalf("expected header row, got first field %s", got)
	}
}

func readRuleCSV(t *testing.T, dir string) [][]string {
	t.Helper()
	file, err := os.Open(filepath.Join(dir, "rule.csv"))
	if err != nil {
		t.Fatalf("open rule.csv: %v", err)
	}
	defer file.Close()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("read rule.csv: %v", err)
	}
	return records
}

func assertRuleField(t *testing.T, record []string, index int, want string) {
	t.Helper()
	if record[index] != want {
		t.Fatalf("field[%d] got %s, want %s", index, record[index], want)
	}
}
