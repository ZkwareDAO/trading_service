package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
)

func TestHandleQueryRules_ByUserID(t *testing.T) {
	// Setup: create temp dir with CSV files
	tmpDir := t.TempDir()
	setupTestCSVs(t, tmpDir)

	// Create repo and rule store
	positionState, err := persistence.NewGlobalState(tmpDir)
	if err != nil {
		t.Fatalf("failed to create GlobalState: %v", err)
	}
	repo := persistence.NewStateRepository(positionState)

	ruleStore, err := config.NewRuleStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create rule store: %v", err)
	}

	// Add test data: user 1 has strategy 100, user 2 has strategy 200
	// Strategy 100 has rule 1, strategy 200 has rule 2
	addTestUserStrategies(t, positionState, []*order.UserStrategy{
		{ID: 100, UserID: 1, Name: "Strategy1"},
		{ID: 200, UserID: 2, Name: "Strategy2"},
	})
	addTestRules(t, ruleStore, []risk.Rule{
		{ID: 1, UserStrategyID: 100, ConditionName: "roi", Status: "active"},
		{ID: 2, UserStrategyID: 200, ConditionName: "price", Status: "active"},
	})

	// Create APIHandler with repo
	handler := NewAPIHandler(ruleStore, repo, 72)

	// Test: query rules for user_id=1
	req := httptest.NewRequest("GET", "/api/v1/rules?user_id=1", nil)
	rr := httptest.NewRecorder()
	handler.HandleRegisterRule(rr, req)

	// Verify response
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("expected data array in response")
	}

	// Should only return rule for user 1
	if len(data) != 1 {
		t.Errorf("expected 1 rule for user_id=1, got %d", len(data))
	}
}

func TestHandleQueryRules_ByUserName(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestCSVs(t, tmpDir)

	gs, err := persistence.NewGlobalState(tmpDir)
	if err != nil {
		t.Fatalf("failed to create global state: %v", err)
	}
	repo := persistence.NewStateRepository(gs)
	ruleStore, err := config.NewRuleStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create rule store: %v", err)
	}

	// Add test users and strategies
	addTestUsers(t, gs, []*order.User{
		{ID: 1, Name: "alice"},
		{ID: 2, Name: "bob"},
	})
	addTestUserStrategies(t, gs, []*order.UserStrategy{
		{ID: 100, UserID: 1, Name: "Strategy1"},
		{ID: 200, UserID: 2, Name: "Strategy2"},
	})
	addTestRules(t, ruleStore, []risk.Rule{
		{ID: 1, UserStrategyID: 100, ConditionName: "roi", Status: "active"},
		{ID: 2, UserStrategyID: 200, ConditionName: "price", Status: "active"},
	})

	handler := NewAPIHandler(ruleStore, repo, 72)

	// Test: query rules for user_name=alice
	req := httptest.NewRequest("GET", "/api/v1/rules?user_name=alice", nil)
	rr := httptest.NewRecorder()
	handler.HandleRegisterRule(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)

	data := resp["data"].([]interface{})
	if len(data) != 1 {
		t.Errorf("expected 1 rule for user_name=alice, got %d", len(data))
	}
}

func TestHandleQueryRules_WithStrategyFilter(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestCSVs(t, tmpDir)

	gs, err := persistence.NewGlobalState(tmpDir)
	if err != nil {
		t.Fatalf("failed to create global state: %v", err)
	}
	repo := persistence.NewStateRepository(gs)
	ruleStore, _ := config.NewRuleStore(tmpDir)

	addTestUserStrategies(t, gs, []*order.UserStrategy{
		{ID: 100, UserID: 1, Name: "BTC Strategy"},
		{ID: 101, UserID: 1, Name: "ETH Strategy"},
	})
	addTestRules(t, ruleStore, []risk.Rule{
		{ID: 1, UserStrategyID: 100, ConditionName: "roi", Status: "active"},
		{ID: 2, UserStrategyID: 101, ConditionName: "price", Status: "active"},
	})

	handler := NewAPIHandler(ruleStore, repo, 72)

	// Test: query rules for user_id=1 with strategy_name filter
	req := httptest.NewRequest("GET", "/api/v1/rules?user_id=1&strategy_name=BTC", nil)
	rr := httptest.NewRecorder()
	handler.HandleRegisterRule(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)

	data := resp["data"].([]interface{})
	// Should only return rules for strategies containing "BTC"
	if len(data) != 1 {
		t.Errorf("expected 1 rule for BTC strategy, got %d", len(data))
	}
}

// Helper functions

func setupTestCSVs(t *testing.T, dir string) {
	t.Helper()
	// Create minimal CSV files with headers
	files := map[string]string{
		"rule.csv":            "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n",
		"user_strategies.csv": "id,user_id,name,exchange,api_key,api_secret,api_password,strategy_id,created_at,updated_at\n",
		"users.csv":           "id,name,exchange,api_key,api_secret,api_password,created_at,updated_at\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func addTestUsers(t *testing.T, repo *persistence.GlobalState, users []*order.User) {
	t.Helper()
	// Add users directly to CSV for testing
	for _, u := range users {
		repo.Users[u.ID] = u
	}
}

func addTestUserStrategies(t *testing.T, repo *persistence.GlobalState, strategies []*order.UserStrategy) {
	t.Helper()
	// Add strategies directly to GlobalState for testing
	for _, s := range strategies {
		repo.UserStrategies[s.ID] = s
	}
}

func addTestRules(t *testing.T, store *config.RuleStore, rules []risk.Rule) {
	t.Helper()
	for _, r := range rules {
		store.AddRules([]risk.Rule{r})
	}
}

// TDD RED: Test querying rules by user_strategy_id
func TestHandleQueryRules_ByUserStrategyID(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestCSVs(t, tmpDir)

	gs, err := persistence.NewGlobalState(tmpDir)
	if err != nil {
		t.Fatalf("failed to create global state: %v", err)
	}
	repo := persistence.NewStateRepository(gs)
	ruleStore, err := config.NewRuleStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create rule store: %v", err)
	}

	// Add test users and strategies
	addTestUsers(t, gs, []*order.User{
		{ID: 1, Name: "alice"},
		{ID: 2, Name: "bob"},
	})
	addTestUserStrategies(t, gs, []*order.UserStrategy{
		{ID: 100, UserID: 1, Name: "Strategy1"},
		{ID: 200, UserID: 2, Name: "Strategy2"},
	})
	addTestRules(t, ruleStore, []risk.Rule{
		{ID: 1, UserStrategyID: 100, ConditionName: "roi", Status: "active"},
		{ID: 2, UserStrategyID: 100, ConditionName: "price", Status: "active"},
		{ID: 3, UserStrategyID: 200, ConditionName: "pnl", Status: "active"},
	})

	handler := NewAPIHandler(ruleStore, repo, 72)

	// Test: query rules for user_strategy_id=100
	req := httptest.NewRequest("GET", "/api/v1/rules?user_strategy_id=100", nil)
	rr := httptest.NewRecorder()
	handler.HandleRegisterRule(rr, req)

	// Verify response
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("expected data array in response")
	}

	// Should return 2 rules for strategy 100
	if len(data) != 2 {
		t.Errorf("expected 2 rules for user_strategy_id=100, got %d", len(data))
	}

	// Verify all rules belong to strategy 100
	for i, item := range data {
		rule, ok := item.(map[string]interface{})
		if !ok {
			t.Errorf("rule[%d] is not a map", i)
			continue
		}
		usID, ok := rule["user_strategy_id"].(float64)
		if !ok {
			t.Errorf("rule[%d] missing user_strategy_id field: %v", i, rule)
			continue
		}
		if usID != 100 {
			t.Errorf("rule[%d] expected user_strategy_id=100, got %v", i, usID)
		}
	}
}

// TDD RED: Test invalid user_strategy_id format
func TestHandleQueryRules_InvalidUserStrategyID(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestCSVs(t, tmpDir)

	gs, err := persistence.NewGlobalState(tmpDir)
	if err != nil {
		t.Fatalf("failed to create global state: %v", err)
	}
	repo := persistence.NewStateRepository(gs)
	ruleStore, err := config.NewRuleStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create rule store: %v", err)
	}

	handler := NewAPIHandler(ruleStore, repo, 72)

	// Test: query with invalid user_strategy_id
	req := httptest.NewRequest("GET", "/api/v1/rules?user_strategy_id=invalid", nil)
	rr := httptest.NewRecorder()
	handler.HandleRegisterRule(rr, req)

	// Should return error
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid user_strategy_id, got %d", rr.Code)
	}
}
