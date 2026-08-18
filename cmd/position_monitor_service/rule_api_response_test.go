package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk/config"
)

// TestCreateRuleResponseFormat 测试 POST /api/v1/rules 响应格式符合项目标准
// 项目标准格式: {"code": 0, "message": "success", "data": {...}}
func TestCreateRuleResponseFormat(t *testing.T) {
	// Setup test data directory
	dataDir := t.TempDir()
	ruleStore, err := config.NewRuleStore(dataDir)
	if err != nil {
		t.Fatalf("Failed to create RuleStore: %v", err)
	}

	// Create test repository with user and strategy
	_, repo := setupTestRepoWithPositions(t)

	// Create API handler
	timeStopHours := 72
	handler := NewAPIHandler(ruleStore, repo, timeStopHours)

	// Create a user strategy for testing
	now := time.Now()
	userID := repo.CreateUser(&order.User{
		Name:      "test_user",
		Exchange:  "binance",
		CreatedAt: now,
		UpdatedAt: now,
	})

	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:           userID,
		Name:             "TEST_STRATEGY",
		Exchange:         "binance",
		Cash:             1000,
		Parts:            5,
		Status:           1,
		RiskStrategyType: order.RiskStrategyTypeTraditional,
		CreatedAt:        now,
		UpdatedAt:        now,
	})

	// Create a position for the strategy
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserStrategyID: usID,
		Exchange:       "binance",
		PosType:        1,
		Asset:          "BTC",
		Quantity:       1.0,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Prepare request
	ruleReq := RegisterRuleRequest{
		UserStrategyID: usID,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat(-0.02),
		Action:         "reduce",
		QuantityPct:    1.0,
		Sort:           1,
	}

	body, _ := json.Marshal(ruleReq)
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// Execute handler
	handler.handleCreateRule(w, req)

	// Verify HTTP status code
	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	// Parse response
	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify standard response format: {code, message, data}
	// This test should FAIL initially because current format is {id, msg}
	t.Logf("Response: %+v", response)

	// Check for "code" field
	if _, ok := response["code"]; !ok {
		t.Errorf("Response missing 'code' field. Got: %v", response)
	}

	// Check for "message" field
	if _, ok := response["message"]; !ok {
		t.Errorf("Response missing 'message' field. Got: %v", response)
	}

	// Check for "data" field
	if _, ok := response["data"]; !ok {
		t.Errorf("Response missing 'data' field. Got: %v", response)
	}

	// Verify code value is 0 (success)
	if code, ok := response["code"].(float64); ok {
		if code != 0 {
			t.Errorf("Expected code=0, got %v", code)
		}
	}

	// Verify message value is "success"
	if msg, ok := response["message"].(string); ok {
		if msg != "success" {
			t.Errorf("Expected message='success', got %v", msg)
		}
	}

	// Verify data contains rule information
	if data, ok := response["data"].(map[string]interface{}); ok {
		if _, ok := data["id"]; !ok {
			t.Errorf("Data missing 'id' field: %v", data)
		}
		if _, ok := data["user_strategy_id"]; !ok {
			t.Errorf("Data missing 'user_strategy_id' field: %v", data)
		}
		if _, ok := data["condition_name"]; !ok {
			t.Errorf("Data missing 'condition_name' field: %v", data)
		}
		if _, ok := data["status"]; !ok {
			t.Errorf("Data missing 'status' field: %v", data)
		}
	} else {
		t.Errorf("Expected data to be an object, got: %v", response["data"])
	}
}

// TestCreateRuleErrorFormat 测试错误响应格式符合项目标准
// 项目标准错误格式: {"code": <error_code>, "message": "<error_msg>", "data": null}
func TestCreateRuleErrorFormat(t *testing.T) {
	dataDir := t.TempDir()
	ruleStore, err := config.NewRuleStore(dataDir)
	if err != nil {
		t.Fatalf("Failed to create RuleStore: %v", err)
	}

	_, repo := setupTestRepo(t)
	handler := NewAPIHandler(ruleStore, repo, 72)

	// Test case: missing required field user_strategy_id
	ruleReq := RegisterRuleRequest{
		ConditionName: "roi",
		Operator:      "<=",
		Value:         ptrFloat(-0.02),
	}

	body, _ := json.Marshal(ruleReq)
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.handleCreateRule(w, req)

	// Should return 400 Bad Request
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	t.Logf("Error response: %+v", response)

	// Check for error format: {code, message, data}
	if _, ok := response["code"]; !ok {
		t.Errorf("Error response missing 'code' field")
	}
	if _, ok := response["message"]; !ok {
		t.Errorf("Error response missing 'message' field")
	}
	// data should be null for errors
	if response["data"] != nil {
		t.Errorf("Error response 'data' should be null, got: %v", response["data"])
	}
}

// Helper functions

func setupTestRepo(t *testing.T) (string, *persistence.StateRepository) {
	dataDir := t.TempDir()

	// Create CSV files
	usersFile := dataDir + "/users.csv"
	userStrategiesFile := dataDir + "/user_strategies.csv"
	rulesFile := dataDir + "/rules.csv"

	// Write empty CSV headers
	os.WriteFile(usersFile, []byte("id,name,exchange,api_key,api_secret,api_password,created_at,updated_at\n"), 0644)
	os.WriteFile(userStrategiesFile, []byte("id,user_id,name,exchange,cash,parts,status,risk_strategy_type,orders_num,valid_before,created_at,updated_at\n"), 0644)
	os.WriteFile(rulesFile, []byte("id,user_strategy_id,condition_name,operator,value,sort,status,action,params,created_at,updated_at\n"), 0644)

	// Create GlobalState and StateRepository
	gs, err := persistence.NewGlobalState(dataDir)
	if err != nil {
		t.Fatalf("Failed to create GlobalState: %v", err)
	}
	repo := persistence.NewStateRepository(gs)

	return dataDir, repo
}

// ptrFloat is defined in api_test.go, removed duplicate here

// ============================================
// Fallback Rule Tests (止盈回落配置)
// ============================================

// TestCreateRule_WithFallbackRule 测试创建止盈规则时自动创建回落规则
func TestCreateRule_WithFallbackRule(t *testing.T) {
	dataDir := t.TempDir()
	ruleStore, err := config.NewRuleStore(dataDir)
	if err != nil {
		t.Fatalf("Failed to create RuleStore: %v", err)
	}

	_, repo := setupTestRepoWithPositions(t)
	handler := NewAPIHandler(ruleStore, repo, 72)

	// Create test user and strategy
	now := time.Now()
	userID := repo.CreateUser(&order.User{
		Name:      "test_fallback_user",
		Exchange:  "binance",
		CreatedAt: now,
		UpdatedAt: now,
	})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:           userID,
		Name:             "TEST_FALLBACK",
		Exchange:         "binance",
		Cash:             1000,
		Parts:            5,
		Status:           1,
		RiskStrategyType: order.RiskStrategyTypeTraditional,
		CreatedAt:        now,
		UpdatedAt:        now,
	})

	// Create a position for the strategy
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserStrategyID: usID,
		Exchange:       "binance",
		PosType:        1,
		Asset:          "BTC",
		Quantity:       1.0,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Request with fallback_rule (指定回落百分比30%)
	ruleReq := RegisterRuleRequest{
		UserStrategyID: usID,
		ConditionName:  "roi",
		Operator:       ">=",
		Value:          ptrFloat(0.3),
		Action:         "reduce",
		QuantityPct:    1.0,
		Sort:           1,
		FallbackRule: &FallbackRuleConfig{
			Value: 0.3, // 指定回落30%
		},
	}

	body, _ := json.Marshal(ruleReq)
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.handleCreateRule(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var response CreateRuleResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// 验证主规则
	if response.Code != 0 {
		t.Errorf("Expected code=0, got %d", response.Code)
	}
	if response.Data.ConditionName != "roi" {
		t.Errorf("Expected condition_name=roi, got %s", response.Data.ConditionName)
	}
	if response.Data.Value != 0.3 {
		t.Errorf("Expected value=0.3, got %f", response.Data.Value)
	}

	// 验证回落规则存在
	if response.Data.FallbackRule == nil {
		t.Fatal("Expected fallback_rule in response, got nil")
	}
	fallback := response.Data.FallbackRule
	if fallback.ConditionName != "profit_drawdown_pct" {
		t.Errorf("Expected fallback condition_name=profit_drawdown_pct, got %s", fallback.ConditionName)
	}
	if fallback.Value != 0.3 {
		t.Errorf("Expected fallback value=0.3, got %f", fallback.Value)
	}
	if fallback.Status != "inactive" {
		t.Errorf("Expected fallback status=inactive, got %s", fallback.Status)
	}

	// 验证主规则的action指向回落规则ID
	expectedAction := fmt.Sprintf("%d", fallback.ID)
	if response.Data.Action != expectedAction {
		t.Errorf("Expected main rule action='%s', got '%s'", expectedAction, response.Data.Action)
	}

	// 验证两条规则都存在于store中
	mainRule, ok := ruleStore.GetRule(int(response.Data.ID))
	if !ok {
		t.Error("Main rule not found in store")
	}
	fallbackRule, ok := ruleStore.GetRule(int(fallback.ID))
	if !ok {
		t.Error("Fallback rule not found in store")
	}

	// 验证回落规则的Sort和Params
	if fallbackRule.Sort != mainRule.Sort+1 {
		t.Errorf("Expected fallback sort=%d, got %d", mainRule.Sort+1, fallbackRule.Sort)
	}
	if fallbackRule.Params["quantity_pct"].(float64) != 1.0 {
		t.Errorf("Expected fallback quantity_pct=1.0, got %v", fallbackRule.Params["quantity_pct"])
	}
}

// TestCreateRule_WithFallbackRule_DefaultValue 测试不指定回落百分比时使用默认值0.05
func TestCreateRule_WithFallbackRule_DefaultValue(t *testing.T) {
	dataDir := t.TempDir()
	ruleStore, _ := config.NewRuleStore(dataDir)
	_, repo := setupTestRepoWithPositions(t)
	handler := NewAPIHandler(ruleStore, repo, 72)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test_default", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "TEST_DEFAULT", Exchange: "binance", Cash: 1000, Parts: 5,
		Status: 1, RiskStrategyType: order.RiskStrategyTypeTraditional, CreatedAt: now, UpdatedAt: now,
	})

	// Create a position
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserStrategyID: usID,
		Exchange:       "binance",
		PosType:        1,
		Asset:          "BTC",
		Quantity:       1.0,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Request with empty fallback_rule (使用默认值)
	ruleReq := RegisterRuleRequest{
		UserStrategyID: usID,
		ConditionName:  "roi",
		Operator:       ">=",
		Value:          ptrFloat(0.3),
		FallbackRule:   &FallbackRuleConfig{}, // Value=0，应该使用默认值0.05
	}

	body, _ := json.Marshal(ruleReq)
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.handleCreateRule(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var response CreateRuleResponse
	json.Unmarshal(w.Body.Bytes(), &response)

	// 验证回落规则使用默认值0.05
	if response.Data.FallbackRule.Value != 0.05 {
		t.Errorf("Expected fallback value=0.05 (default), got %f", response.Data.FallbackRule.Value)
	}
}

// TestCreateRule_WithoutFallbackRule 测试不传fallback_rule时正常创建单条规则
func TestCreateRule_WithoutFallbackRule(t *testing.T) {
	dataDir := t.TempDir()
	ruleStore, _ := config.NewRuleStore(dataDir)
	_, repo := setupTestRepoWithPositions(t)
	handler := NewAPIHandler(ruleStore, repo, 72)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test_no_fallback", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "TEST_NO_FALLBACK", Exchange: "binance", Cash: 1000, Parts: 5,
		Status: 1, RiskStrategyType: order.RiskStrategyTypeTraditional, CreatedAt: now, UpdatedAt: now,
	})

	// Create a position
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserStrategyID: usID,
		Exchange:       "binance",
		PosType:        1,
		Asset:          "BTC",
		Quantity:       1.0,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Request WITHOUT fallback_rule
	ruleReq := RegisterRuleRequest{
		UserStrategyID: usID,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat(-0.3),
		Action:         "reduce",
		QuantityPct:    1.0,
		Sort:           1,
		// No FallbackRule
	}

	body, _ := json.Marshal(ruleReq)
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.handleCreateRule(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var response CreateRuleResponse
	json.Unmarshal(w.Body.Bytes(), &response)

	// 验证响应中不包含fallback_rule
	if response.Data.FallbackRule != nil {
		t.Error("Expected no fallback_rule in response when not provided in request")
	}

	// 验证只有一条规则
	rules := ruleStore.GetRulesByUserStrategy(usID)
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(rules))
	}
}

// TestCreateRule_FallbackRuleInheritsQuantityPct 测试回落规则继承主规则的quantity_pct
func TestCreateRule_FallbackRuleInheritsQuantityPct(t *testing.T) {
	dataDir := t.TempDir()
	ruleStore, _ := config.NewRuleStore(dataDir)
	_, repo := setupTestRepoWithPositions(t)
	handler := NewAPIHandler(ruleStore, repo, 72)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test_inherit", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "TEST_INHERIT", Exchange: "binance", Cash: 1000, Parts: 5,
		Status: 1, RiskStrategyType: order.RiskStrategyTypeTraditional, CreatedAt: now, UpdatedAt: now,
	})

	// Create a position
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserStrategyID: usID,
		Exchange:       "binance",
		PosType:        1,
		Asset:          "BTC",
		Quantity:       1.0,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// 主规则 quantity_pct=0.5
	ruleReq := RegisterRuleRequest{
		UserStrategyID: usID,
		ConditionName:  "roi",
		Operator:       ">=",
		Value:          ptrFloat(0.3),
		QuantityPct:    0.5, // 主规则平仓50%
		FallbackRule:   &FallbackRuleConfig{Value: 0.2},
	}

	body, _ := json.Marshal(ruleReq)
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.handleCreateRule(w, req)

	var response CreateRuleResponse
	json.Unmarshal(w.Body.Bytes(), &response)

	// 验证回落规则继承了quantity_pct
	fallbackRule, _ := ruleStore.GetRule(int(response.Data.FallbackRule.ID))
	if fallbackRule.Params["quantity_pct"].(float64) != 0.5 {
		t.Errorf("Expected fallback to inherit quantity_pct=0.5, got %v", fallbackRule.Params["quantity_pct"])
	}
}

// TestUpdateRule_WithNewFallbackRule 测试为已存在的规则添加回落规则
func TestUpdateRule_WithNewFallbackRule(t *testing.T) {
	dataDir := t.TempDir()
	ruleStore, _ := config.NewRuleStore(dataDir)
	_, repo := setupTestRepoWithPositions(t)
	handler := NewAPIHandler(ruleStore, repo, 72)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test_update_fallback", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "TEST_UPDATE_FALLBACK", Exchange: "binance", Cash: 1000, Parts: 5,
		Status: 1, RiskStrategyType: order.RiskStrategyTypeTraditional, CreatedAt: now, UpdatedAt: now,
	})

	// 创建一个active position
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserStrategyID: usID,
		Exchange:       "binance",
		PosType:        1,
		Asset:          "BTC",
		Quantity:       1.0,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// 第一步：创建一个没有fallback的规则
	ruleReq1 := RegisterRuleRequest{
		UserStrategyID: usID,
		ConditionName:  "roi",
		Operator:       ">=",
		Value:          ptrFloat(0.3),
		Action:         "reduce",
		QuantityPct:    1.0,
		Sort:           1,
		// No FallbackRule
	}

	body1, _ := json.Marshal(ruleReq1)
	req1 := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(body1))
	w1 := httptest.NewRecorder()
	handler.handleCreateRule(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w1.Code, w1.Body.String())
	}

	var response1 CreateRuleResponse
	json.Unmarshal(w1.Body.Bytes(), &response1)
	originalRuleID := response1.Data.ID

	// 验证初始状态：没有fallback_rule
	if response1.Data.FallbackRule != nil {
		t.Error("Expected no fallback_rule initially")
	}

	// 第二步：更新规则，添加fallback_rule
	ruleReq2 := RegisterRuleRequest{
		UserStrategyID: usID,
		ConditionName:  "roi",
		Operator:       ">=",
		Value:          ptrFloat(0.6), // 修改value
		QuantityPct:    1.0,
		Sort:           1,
		FallbackRule: &FallbackRuleConfig{
			Value: 0.03, // 添加回落规则
		},
	}

	body2, _ := json.Marshal(ruleReq2)
	req2 := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(body2))
	w2 := httptest.NewRecorder()
	handler.handleCreateRule(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("Expected 200 for update, got %d: %s", w2.Code, w2.Body.String())
	}

	var response2 CreateRuleResponse
	json.Unmarshal(w2.Body.Bytes(), &response2)

	// 验证：更新后应该有fallback_rule
	if response2.Data.FallbackRule == nil {
		t.Fatal("Expected fallback_rule after update, got nil")
	}

	// 验证fallback_rule的值
	if response2.Data.FallbackRule.Value != 0.03 {
		t.Errorf("Expected fallback value=0.03, got %f", response2.Data.FallbackRule.Value)
	}

	// 验证主规则的action指向fallback规则的ID
	expectedAction := fmt.Sprintf("%d", response2.Data.FallbackRule.ID)
	if response2.Data.Action != expectedAction {
		t.Errorf("Expected main rule action='%s', got '%s'", expectedAction, response2.Data.Action)
	}

	// 验证主规则ID未变（是更新而不是新建）
	if response2.Data.ID != originalRuleID {
		t.Errorf("Expected same rule ID %d, got %d", originalRuleID, response2.Data.ID)
	}

	// 验证value已更新
	if response2.Data.Value != 0.6 {
		t.Errorf("Expected updated value=0.6, got %f", response2.Data.Value)
	}
}

// TestUpdateRule_WithExistingFallbackRule 测试更新已有fallback_rule的规则
func TestUpdateRule_WithExistingFallbackRule(t *testing.T) {
	dataDir := t.TempDir()
	ruleStore, _ := config.NewRuleStore(dataDir)
	_, repo := setupTestRepoWithPositions(t)
	handler := NewAPIHandler(ruleStore, repo, 72)

	now := time.Now()
	userID := repo.CreateUser(&order.User{Name: "test_update_existing", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "TEST_UPDATE_EXISTING", Exchange: "binance", Cash: 1000, Parts: 5,
		Status: 1, RiskStrategyType: order.RiskStrategyTypeTraditional, CreatedAt: now, UpdatedAt: now,
	})

	// 创建一个active position
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserStrategyID: usID,
		Exchange:       "binance",
		PosType:        1,
		Asset:          "BTC",
		Quantity:       1.0,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// 第一步：创建带fallback_rule的规则
	ruleReq1 := RegisterRuleRequest{
		UserStrategyID: usID,
		ConditionName:  "roi",
		Operator:       ">=",
		Value:          ptrFloat(0.3),
		QuantityPct:    1.0,
		Sort:           1,
		FallbackRule: &FallbackRuleConfig{
			Value: 0.05, // 初始回落5%
		},
	}

	body1, _ := json.Marshal(ruleReq1)
	req1 := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(body1))
	w1 := httptest.NewRecorder()
	handler.handleCreateRule(w1, req1)

	var response1 CreateRuleResponse
	json.Unmarshal(w1.Body.Bytes(), &response1)
	originalFallbackID := response1.Data.FallbackRule.ID

	// 第二步：更新规则，修改fallback_rule的值
	ruleReq2 := RegisterRuleRequest{
		UserStrategyID: usID,
		ConditionName:  "roi",
		Operator:       ">=",
		Value:          ptrFloat(0.3),
		QuantityPct:    1.0,
		Sort:           1,
		FallbackRule: &FallbackRuleConfig{
			Value: 0.03, // 修改回落3%
		},
	}

	body2, _ := json.Marshal(ruleReq2)
	req2 := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(body2))
	w2 := httptest.NewRecorder()
	handler.handleCreateRule(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("Expected 200 for update, got %d: %s", w2.Code, w2.Body.String())
	}

	var response2 CreateRuleResponse
	json.Unmarshal(w2.Body.Bytes(), &response2)

	// 验证：fallback_rule应该已更新
	if response2.Data.FallbackRule == nil {
		t.Fatal("Expected fallback_rule after update, got nil")
	}

	// 验证fallback_rule的值已更新
	if response2.Data.FallbackRule.Value != 0.03 {
		t.Errorf("Expected updated fallback value=0.03, got %f", response2.Data.FallbackRule.Value)
	}

	// 验证fallback_rule的ID保持不变（是更新而不是新建）
	if response2.Data.FallbackRule.ID != originalFallbackID {
		t.Errorf("Expected same fallback ID %d, got %d", originalFallbackID, response2.Data.FallbackRule.ID)
	}
}

func setupTestRepoWithPositions(t *testing.T) (string, *persistence.StateRepository) {
	dataDir := t.TempDir()

	// Create CSV files
	usersFile := dataDir + "/users.csv"
	userStrategiesFile := dataDir + "/user_strategies.csv"
	rulesFile := dataDir + "/rules.csv"
	userOrderPositionsFile := dataDir + "/user_order_positions.csv"

	// Write empty CSV headers
	os.WriteFile(usersFile, []byte("id,name,exchange,api_key,api_secret,api_password,created_at,updated_at\n"), 0644)
	os.WriteFile(userStrategiesFile, []byte("id,user_id,name,exchange,cash,parts,status,risk_strategy_type,orders_num,valid_before,created_at,updated_at\n"), 0644)
	os.WriteFile(rulesFile, []byte("id,user_strategy_id,condition_name,operator,value,sort,status,action,params,created_at,updated_at\n"), 0644)
	os.WriteFile(userOrderPositionsFile, []byte("id,user_strategy_id,exchange,pos_type,asset,quantity,deleted,created_at,updated_at\n"), 0644)

	// Create GlobalState and StateRepository
	gs, err := persistence.NewGlobalState(dataDir)
	if err != nil {
		t.Fatalf("Failed to create GlobalState: %v", err)
	}
	repo := persistence.NewStateRepository(gs)

	return dataDir, repo
}
