package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestHandleRegisterRule_PositionSymbol_InvalidFormat tests that creating a rule
// with invalid position_xxx symbol format returns error.
func TestHandleRegisterRule_PositionSymbol_InvalidFormat(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"rule.csv":                 "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n",
		"user_order_positions.csv": "id,user_id,uprunning_order_id,user_order_id,user_strategy_id,risk_control_strategy_id,exchange,pos_type,asset,current_price,quantity,pos_value,leverage,deleted,init_margin,pos_price,pnl_value,side,close_time,created_at,updated_at\n",
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

	// Try to create a rule with invalid position symbol format
	reqBody := RegisterRuleRequest{
		UserStrategyID: 100,
		ConditionName:  "position_INVALID_FORMAT",
		Operator:       "==",
		Value:          ptrFloat64(0),
		QuantityPct:    1.0,
		Sort:           1,
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.HandleRegisterRule(rr, req)

	// Should return error: invalid position symbol format
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return error code 1001 (validation error)
	if int(resp["code"].(float64)) != 1001 {
		t.Errorf("expected error code 1001, got %v", resp["code"])
	}
}

// TestHandleRegisterRule_PositionSymbol_NoActivePosition tests that creating a position_xxx rule
// fails when no active position exists for the symbol.
func TestHandleRegisterRule_PositionSymbol_NoActivePosition(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"rule.csv":                 "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n",
		"user_order_positions.csv": "id,user_id,uprunning_order_id,user_order_id,user_strategy_id,risk_control_strategy_id,exchange,pos_type,asset,current_price,quantity,pos_value,leverage,deleted,init_margin,pos_price,pnl_value,side,close_time,created_at,updated_at\n",
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

	// Try to create position_xxx rule without matching position
	reqBody := RegisterRuleRequest{
		UserStrategyID: 100,
		ConditionName:  "position_BTC-7AUG26-64000-P",
		Operator:       "==",
		Value:          ptrFloat64(0),
		QuantityPct:    1.0,
		Sort:           1,
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.HandleRegisterRule(rr, req)

	// Should return error
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestHandleRegisterRule_PositionSymbol_Valid tests that creating a position_xxx rule
// succeeds when active position exists for the symbol.
func TestHandleRegisterRule_PositionSymbol_Valid(t *testing.T) {
	tmpDir := t.TempDir()

	now := "2024-01-01T00:00:00Z"
	files := map[string]string{
		"rule.csv":                 "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n",
		"user_order_positions.csv": "id,user_id,uprunning_order_id,user_order_id,user_strategy_id,risk_control_strategy_id,exchange,pos_type,asset,current_price,quantity,pos_value,leverage,deleted,init_margin,pos_price,pnl_value,side,close_time,created_at,updated_at\n1,1,0,1,100,0,deribit,2,BTC-7AUG26-64000-P,50000.0,0.1,5000.0,10,0,500.0,50000.0,0,0,," + now + "," + now + "\n",
		"user_strategies.csv":      "id,user_id,name,exchange,valid_before,cash,parts,status,strategy_id,risk_strategy_type,orders_num,created_at,updated_at\n100,1,test,deribit,2030-01-01T00:00:00Z,100.0,1,active,1,cta_intraday,0,2024-01-01T00:00:00Z,2024-01-01T00:00:00Z\n",
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

	// Create position_xxx rule with matching active position
	reqBody := RegisterRuleRequest{
		UserStrategyID: 100,
		ConditionName:  "position_BTC-7AUG26-64000-P",
		Operator:       "==",
		Value:          ptrFloat64(0),
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

	if resp.Data.ConditionName != "position_BTC-7AUG26-64000-P" {
		t.Errorf("expected condition_name 'position_BTC-7AUG26-64000-P', got '%s'", resp.Data.ConditionName)
	}
}

// TestHandleRegisterRule_PositionSymbol_SpotFormat tests valid spot position symbol
func TestHandleRegisterRule_PositionSymbol_SpotFormat(t *testing.T) {
	tmpDir := t.TempDir()

	now := "2024-01-01T00:00:00Z"
	files := map[string]string{
		"rule.csv":                 "id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n",
		"user_order_positions.csv": "id,user_id,uprunning_order_id,user_order_id,user_strategy_id,risk_control_strategy_id,exchange,pos_type,asset,current_price,quantity,pos_value,leverage,deleted,init_margin,pos_price,pnl_value,side,close_time,created_at,updated_at\n1,1,0,1,100,0,binance,2,BTCUSDT,50000.0,0.1,5000.0,10,0,500.0,50000.0,0,0,," + now + "," + now + "\n",
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

	// Create position_xxx rule with spot symbol
	reqBody := RegisterRuleRequest{
		UserStrategyID: 100,
		ConditionName:  "position_BTCUSDT",
		Operator:       "==",
		Value:          ptrFloat64(0),
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
}