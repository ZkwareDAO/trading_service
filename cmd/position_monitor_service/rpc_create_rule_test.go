package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"trading-service/internal/persistence"
	"trading-service/internal/risk/config"
)

func setupTestServices(t *testing.T, tmpDir string) (*persistence.GlobalState, *persistence.StateRepository, *config.RuleStore) {
	gs, err := persistence.NewGlobalState(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	ruleStore, err := config.NewRuleStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	return gs, repo, ruleStore
}

// TDD RED: Test PMS RPC CreateRule endpoint
func TestRPCCreateRule_Success(t *testing.T) {
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

	handler := NewAPIHandler(ruleStore, repo, 72)

	// Create RPC request
	reqBody := map[string]interface{}{
		"user_strategy_id": float64(100),
		"condition_name":   "always",
		"operator":         "==",
		"value":            "true",
		"sort":             1,
		"action":           "reduce",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/rpc/v1/rules/create", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.HandleRPCCreateRule(rr, req)

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

	if resp["rule_id"] == nil {
		t.Error("expected rule_id in response")
	}
}
