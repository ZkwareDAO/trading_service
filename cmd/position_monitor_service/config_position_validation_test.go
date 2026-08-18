package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

// TestHandleRegisterRule_NoActivePosition tests that creating a rule
// without an active position returns error 4005.
func TestHandleRegisterRule_NoActivePosition(t *testing.T) {
	tmpDir := t.TempDir()

	// Create minimal CSV files (no user_order_positions)
	files := map[string]string{
		"rule.csv":                 "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n",
		"user_order_positions.csv": "id,user_id,uprunning_order_id,user_order_id,user_strategy_id,risk_control_strategy_id,exchange,pos_type,asset,current_price,quantity,pos_value,leverage,deleted,init_margin,pos_price,pnl_value,side,close_time,created_at,updated_at\n",
		"user_strategies.csv":      "id,user_id,name,exchange,valid_before,cash,parts,status,strategy_id,risk_strategy_type,orders_num,created_at,updated_at\n999,1,test,binance,2030-01-01T00:00:00Z,100.0,1,active,1,cta_intraday,0,2024-01-01T00:00:00Z,2024-01-01T00:00:00Z\n",
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

	// Try to create a rule for a strategy without active positions
	reqBody := RegisterRuleRequest{
		UserStrategyID: 999, // No positions for this strategy
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat64(-0.02),
		QuantityPct:    1.0,
		Sort:           1,
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.HandleRegisterRule(rr, req)

	// Should return error 4005: no active position found
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if int(resp["code"].(float64)) != 4005 {
		t.Errorf("expected error code 4005, got %v", resp["code"])
	}

	expectedMsg := "no active position found for strategy 999"
	if resp["message"] != expectedMsg {
		t.Errorf("expected message '%s', got '%s'", expectedMsg, resp["message"])
	}
}

// TestHandleRegisterRule_WithActivePosition tests that creating a rule
// succeeds when the strategy has an active position.
func TestHandleRegisterRule_WithActivePosition(t *testing.T) {
	tmpDir := t.TempDir()

	now := "2024-01-01T00:00:00Z"
	files := map[string]string{
		"rule.csv":                 "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n",
		"user_order_positions.csv": "id,user_id,uprunning_order_id,user_order_id,user_strategy_id,risk_control_strategy_id,exchange,pos_type,asset,current_price,quantity,pos_value,leverage,deleted,init_margin,pos_price,pnl_value,side,close_time,created_at,updated_at\n1,1,0,1,100,0,binance,2,BTC,50000.0,0.1,5000.0,10,0,500.0,50000.0,0,0,," + now + "," + now + "\n",
		"user_strategies.csv":      "id,user_id,name,exchange,valid_before,cash,parts,status,strategy_id,risk_strategy_type,orders_num,created_at,updated_at\n100,1,test,binance,2030-01-01T00:00:00Z,100.0,1,active,1,cta_intraday,0,2024-01-01T00:00:00Z,2024-01-01T00:00:00Z\n",
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

	// Create a rule for strategy 100 (has active position)
	reqBody := RegisterRuleRequest{
		UserStrategyID: 100,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat64(-0.02),
		QuantityPct:    1.0,
		Sort:           1,
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.HandleRegisterRule(rr, req)

	// Should succeed
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Errorf("expected status 200 or 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp CreateRuleResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("expected code 0 (success), got %d", resp.Code)
	}

	if resp.Data.ID == 0 {
		t.Error("expected rule ID to be assigned")
	}
}

// TestHandleRegisterRule_PositionDeleted tests that creating a rule
// fails when the position is deleted (deleted=1).
func TestHandleRegisterRule_PositionDeleted(t *testing.T) {
	tmpDir := t.TempDir()

	now := "2024-01-01T00:00:00Z"
	files := map[string]string{
		"rule.csv":                 "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n",
		"user_order_positions.csv": "id,user_id,uprunning_order_id,user_order_id,user_strategy_id,risk_control_strategy_id,exchange,pos_type,asset,current_price,quantity,pos_value,leverage,deleted,init_margin,pos_price,pnl_value,side,close_time,created_at,updated_at\n1,1,0,1,200,0,binance,2,BTC,50000.0,0.1,5000.0,10,1,500.0,50000.0,0,0,," + now + "," + now + "\n",
		"user_strategies.csv":      "id,user_id,name,exchange,valid_before,cash,parts,status,strategy_id,risk_strategy_type,orders_num,created_at,updated_at\n200,1,test,binance,2030-01-01T00:00:00Z,100.0,1,active,1,cta_intraday,0,2024-01-01T00:00:00Z,2024-01-01T00:00:00Z\n",
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

	// Try to create a rule for strategy 200 (position is deleted)
	reqBody := RegisterRuleRequest{
		UserStrategyID: 200,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat64(-0.02),
		QuantityPct:    1.0,
		Sort:           1,
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.HandleRegisterRule(rr, req)

	// Should return error 4005: no active position found
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)

	if int(resp["code"].(float64)) != 4005 {
		t.Errorf("expected error code 4005, got %v", resp["code"])
	}
}

// Helper function to create float64 pointer
func ptrFloat64(f float64) *float64 {
	return &f
}

// Helper to create position in test
func createTestPosition(repo *persistence.StateRepository, strategyID uint64, deleted int) {
	pos := &order.UserOrderPosition{
		UserID:         1,
		UserOrderID:    1,
		UserStrategyID: strategyID,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "BTC",
		Side:           order.SideLong,
		Quantity:       0.1,
		PosPrice:       50000.0,
		CurrentPrice:   50000.0,
		Leverage:       10,
		Deleted:        deleted,
	}
	repo.CreateUserOrderPosition(pos)
}