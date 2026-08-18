package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
)

// TDD RED: Test PMS RPC InvalidateRulesForStrategy endpoint
func TestRPCInvalidateRulesForStrategy_Success(t *testing.T) {
	tmpDir := t.TempDir()

	// Create minimal CSV files
	files := map[string]string{
		"rule.csv": "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n",
	}
	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	gs, repo, ruleStore := setupTestServices(t, tmpDir)
	defer gs.Shutdown()

	// Create test rules: 2 active, 1 inactive
	rule1 := &risk.Rule{
		UserStrategyID: 100,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          -0.02,
		Status:         config.RuleStatusActive,
		Action:         "reduce",
	}
	rule2 := &risk.Rule{
		UserStrategyID: 100,
		ConditionName:  "holding_time",
		Operator:       ">",
		Value:          3600,
		Status:         config.RuleStatusActive,
		Action:         "reduce",
	}
	rule3 := &risk.Rule{
		UserStrategyID: 100,
		ConditionName:  "profit_trigger",
		Operator:       ">=",
		Value:          0.1,
		Status:         config.RuleStatusInactive, // Already inactive
		Action:         "reduce",
	}
	ruleStore.CreateRule(rule1)
	ruleStore.CreateRule(rule2)
	ruleStore.CreateRule(rule3)

	handler := NewAPIHandler(ruleStore, repo, 72)

	// Create RPC request
	reqBody := map[string]interface{}{
		"user_strategy_id": float64(100),
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/rpc/v1/rules/invalidate-for-strategy", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.HandleRPCInvalidateRulesForStrategy(rr, req)

	// Verify response
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}

	// Should invalidate 2 active rules (rule3 was already inactive)
	if resp["invalidated_count"].(float64) != 2 {
		t.Errorf("expected invalidated_count=2, got %v", resp["invalidated_count"])
	}

	// Verify rules are now inactive
	rules := ruleStore.GetRulesByUserStrategy(100)
	activeCount := 0
	for _, r := range rules {
		if r.Status == config.RuleStatusActive {
			activeCount++
		}
	}
	if activeCount != 0 {
		t.Errorf("expected 0 active rules, got %d", activeCount)
	}
}

// TestRPCInvalidateRulesForStrategy_NoRules tests strategy with no rules
func TestRPCInvalidateRulesForStrategy_NoRules(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"rule.csv": "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n",
	}
	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	gs, repo, ruleStore := setupTestServices(t, tmpDir)
	defer gs.Shutdown()

	handler := NewAPIHandler(ruleStore, repo, 72)

	reqBody := map[string]interface{}{
		"user_strategy_id": float64(999), // No rules for this strategy
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/rpc/v1/rules/invalidate-for-strategy", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.HandleRPCInvalidateRulesForStrategy(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp["invalidated_count"].(float64) != 0 {
		t.Errorf("expected invalidated_count=0, got %v", resp["invalidated_count"])
	}
}

// TestRPCInvalidateRulesForStrategy_InvalidJSON tests error handling
func TestRPCInvalidateRulesForStrategy_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"rule.csv": "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n",
	}
	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	gs, repo, ruleStore := setupTestServices(t, tmpDir)
	defer gs.Shutdown()

	handler := NewAPIHandler(ruleStore, repo, 72)

	req := httptest.NewRequest("POST", "/rpc/v1/rules/invalidate-for-strategy", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.HandleRPCInvalidateRulesForStrategy(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}
}
