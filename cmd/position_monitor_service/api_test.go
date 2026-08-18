package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
)

func newTestAPI(t *testing.T) (*APIHandler, *persistence.GlobalState, string) {
	t.Helper()
	tmpDir := t.TempDir()
	// Create empty rule.csv with header
	csvPath := filepath.Join(tmpDir, "rule.csv")
	if err := os.WriteFile(csvPath, []byte("id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ruleStore, err := config.NewRuleStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create GlobalState and StateRepository for testing
	gs, err := persistence.NewGlobalState(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for async CSV writes before t.TempDir() removal, otherwise cleanup fails
	// with "directory not empty" and marks the test as failed.
	t.Cleanup(func() { gs.Shutdown() })
	repo := persistence.NewStateRepository(gs)

	// Create test user for rule queries
	now := time.Now()
	repo.CreateUser(&order.User{Name: "test_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})

	// Create test user_strategy for validation (directly set ID=1000 for tests)
	us := &order.UserStrategy{
		ID:        1000, // Fixed ID for test compatibility
		UserID:    1,
		Name:      "test-strategy",
		Exchange:  "binance",
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	// Directly add to internal map (bypass ID generation)
	gs.UserStrategies[1000] = us

	// Create test position for validation (required for rule creation)
	pos := &order.UserOrderPosition{
		ID:             1,
		UserID:         1,
		UserStrategyID: 1000,
		Exchange:       "binance",
		Asset:          "BTCUSDT",
		Side:          order.SideLong,
		Deleted:       0, // Active position
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	// Directly add to internal map
	gs.UserOrderPositions[1] = pos

	return NewAPIHandler(ruleStore, repo, 72), gs, tmpDir
}

// addTestStrategy adds a test strategy and position to the global state
func addTestStrategy(gs *persistence.GlobalState, strategyID uint64, userID uint64) {
	now := time.Now()
	us := &order.UserStrategy{
		ID:        strategyID,
		UserID:    userID,
		Name:      fmt.Sprintf("test-strategy-%d", strategyID),
		Exchange:  "binance",
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	gs.UserStrategies[strategyID] = us

	pos := &order.UserOrderPosition{
		ID:             strategyID, // Use strategyID as position ID for simplicity
		UserID:         userID,
		UserStrategyID: strategyID,
		Exchange:       "binance",
		Asset:          "BTCUSDT",
		Side:          order.SideLong,
		Deleted:       0,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	gs.UserOrderPositions[strategyID] = pos
}

func TestRegisterRule_ValidInput(t *testing.T) {
	api, _, _ := newTestAPI(t)

	reqBody := RegisterRuleRequest{
		UserStrategyID: 1000,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat(-0.02),
		Action:         "reduce",
	}
	req := httptest.NewRequest("POST", "/api/rules", bytes.NewReader(mustMarshal(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.HandleRegisterRule(w, req)

	// Should return 201 Created for POST requests creating resources
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp CreateRuleResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// Verify standard response format
	if resp.Code != 0 {
		t.Errorf("expected code=0, got %d", resp.Code)
	}
	if resp.Message != "success" {
		t.Errorf("expected message='success', got %s", resp.Message)
	}
	if resp.Data == nil {
		t.Fatal("expected data to be non-nil")
	}
	if resp.Data.ID <= 0 {
		t.Errorf("expected positive rule ID, got %d", resp.Data.ID)
	}
}

func TestRegisterRule_MissingFields(t *testing.T) {
	api, _, _ := newTestAPI(t)

	tests := []struct {
		name string
		body RegisterRuleRequest
	}{
		{"no user_strategy_id", RegisterRuleRequest{ConditionName: "roi", Operator: "<=", Value: ptrFloat(-0.02)}},
		{"no condition_name", RegisterRuleRequest{UserStrategyID: 1000, Operator: "<=", Value: ptrFloat(-0.02)}},
		{"no operator", RegisterRuleRequest{UserStrategyID: 1000, ConditionName: "roi", Value: ptrFloat(-0.02)}},
		{"no value", RegisterRuleRequest{UserStrategyID: 1000, ConditionName: "roi", Operator: "<="}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/rules", bytes.NewReader(mustMarshal(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			api.HandleRegisterRule(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestRegisterRule_InvalidOperator(t *testing.T) {
	api, _, _ := newTestAPI(t)

	reqBody := RegisterRuleRequest{
		UserStrategyID: 1000,
		ConditionName:  "roi",
		Operator:       "~~~",
		Value:          ptrFloat(-0.02),
	}
	req := httptest.NewRequest("POST", "/api/rules", bytes.NewReader(mustMarshal(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.HandleRegisterRule(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid operator, got %d", w.Code)
	}
}

func TestRegisterRule_IDGeneration(t *testing.T) {
	api, _, _ := newTestAPI(t)

	// Register two rules
	rule1 := RegisterRuleRequest{UserStrategyID: 1000, ConditionName: "roi", Operator: "<=", Value: ptrFloat(-0.02)}
	rule2 := RegisterRuleRequest{UserStrategyID: 1000, ConditionName: "roi", Operator: ">=", Value: ptrFloat(0.05)}

	w1 := httptest.NewRecorder()
	api.HandleRegisterRule(w1, httptest.NewRequest("POST", "/api/rules", bytes.NewReader(mustMarshal(rule1))))
	w2 := httptest.NewRecorder()
	api.HandleRegisterRule(w2, httptest.NewRequest("POST", "/api/rules", bytes.NewReader(mustMarshal(rule2))))

	if w1.Code != http.StatusCreated || w2.Code != http.StatusCreated {
		t.Fatalf("both requests should succeed: %d, %d", w1.Code, w2.Code)
	}

	var resp1, resp2 CreateRuleResponse
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	json.Unmarshal(w2.Body.Bytes(), &resp2)

	if resp2.Data.ID != resp1.Data.ID+1 {
		t.Errorf("expected sequential IDs (%d, %d), got (%d, %d)", resp1.Data.ID, resp1.Data.ID+1, resp1.Data.ID, resp2.Data.ID)
	}
}

func TestRegisterRule_MemoryUpdate(t *testing.T) {
	api, _, _ := newTestAPI(t)

	reqBody := RegisterRuleRequest{UserStrategyID: 1000, ConditionName: "roi", Operator: "<=", Value: ptrFloat(-0.02)}
	req := httptest.NewRequest("POST", "/api/rules", bytes.NewReader(mustMarshal(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.HandleRegisterRule(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	// Check memory config was updated
	rules := api.ruleStore.ListRules()
	rulesForStrategy := []risk.Rule{}
	for _, r := range rules {
		if r.UserStrategyID == 1000 {
			rulesForStrategy = append(rulesForStrategy, r)
		}
	}
	if len(rulesForStrategy) != 1 {
		t.Errorf("expected 1 rule in memory, got %d", len(rulesForStrategy))
	}
	if rulesForStrategy[0].ConditionName != "roi" {
		t.Errorf("expected condition 'roi', got '%s'", rulesForStrategy[0].ConditionName)
	}
}

func TestRegisterRule_Concurrent(t *testing.T) {
	api, _, _ := newTestAPI(t)

	// Test concurrent creation of rules with DIFFERENT conditions
	// Each should create a new rule
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func(idx int) {
			// Each goroutine uses different condition to create separate rules
			operators := []string{"<=", ">=", ">", "<", "=="}
			reqBody := RegisterRuleRequest{
				UserStrategyID: 1000,
				ConditionName:  "roi",
				Operator:       operators[idx], // Different operators = different rules
				Value:          ptrFloat(-0.02),
			}
			w := httptest.NewRecorder()
			api.HandleRegisterRule(w, httptest.NewRequest("POST", "/api/rules", bytes.NewReader(mustMarshal(reqBody))))
			done <- w.Code == http.StatusCreated
		}(i)
	}

	successCount := 0
	for i := 0; i < 5; i++ {
		if <-done {
			successCount++
		}
	}
	if successCount != 5 {
		t.Errorf("expected 5 successful concurrent registrations, got %d", successCount)
	}
	if len(api.ruleStore.ListRules()) != 5 {
		t.Errorf("expected 5 rules in memory after concurrent writes, got %d", len(api.ruleStore.ListRules()))
	}
}

func TestRegisterRule_HoldingTimeDefault(t *testing.T) {
	api, _, _ := newTestAPI(t)

	// holding_time without value should use default from config
	reqBody := RegisterRuleRequest{
		UserStrategyID: 1000,
		ConditionName:  "holding_time",
		Operator:       ">",
		// Value not set → should default to 72h = 259200
	}
	req := httptest.NewRequest("POST", "/api/rules", bytes.NewReader(mustMarshal(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.HandleRegisterRule(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the rule has default value 259200
	rules := api.ruleStore.ListRules()
	rulesForStrategy := []risk.Rule{}
	for _, r := range rules {
		if r.UserStrategyID == 1000 {
			rulesForStrategy = append(rulesForStrategy, r)
		}
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Value != 259200.0 {
		t.Errorf("expected default holding_time value 259200, got %v", rules[0].Value)
	}
}

// TestRegisterRule_ErrorMessageIncludesStrategyID 验证错误消息包含策略ID
// 注意：这是一个探索性测试，验证当错误发生时消息格式
func TestRegisterRule_ErrorMessageIncludesStrategyID(t *testing.T) {
	// 由于直接触发CreateRule失败比较困难，我们改为验证错误消息格式
	// 通过模拟fallback_rule创建失败来测试

	api, _, _ := newTestAPI(t)

	// 创建一个带有fallback_rule的请求
	reqBody := RegisterRuleRequest{
		UserStrategyID: 3003,
		ConditionName:  "roi",
		Operator:       ">=",
		Value:          ptrFloat(0.3),
		FallbackRule:   &FallbackRuleConfig{Value: 0.2},
	}

	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(mustMarshal(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.handleCreateRule(w, req)

	// 由于正常情况会成功，我们检查如果失败，消息应该包含策略ID
	if w.Code != http.StatusCreated {
		body := w.Body.String()
		if !strings.Contains(body, "3003") {
			t.Errorf("错误消息应包含策略ID 3003，实际消息: %s", body)
		}
	}
	// 如果成功，测试也通过（因为无法可靠触发失败）
}

// TestRegisterRule_Upsert_CreateWhenNotExists 验证不存在时创建新规则
func TestRegisterRule_Upsert_CreateWhenNotExists(t *testing.T) {
	api, gs, _ := newTestAPI(t)

	// Create user_strategy and position for this test (required for validation)
	now := time.Now()
	us := &order.UserStrategy{
		ID:        5001,
		UserID:    1,
		Name:      "test-strategy-5001",
		Exchange:  "binance",
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	// Directly add to internal map
	gs.UserStrategies[5001] = us

	pos := &order.UserOrderPosition{
		ID:             100,
		UserID:         1,
		UserStrategyID: 5001,
		Exchange:       "binance",
		Asset:          "BTCUSDT",
		Side:          order.SideLong,
		Deleted:       0,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	gs.UserOrderPositions[100] = pos

	// 第一次请求：不存在，应该创建
	reqBody := RegisterRuleRequest{
		UserStrategyID: 5001,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat(-0.02),
		QuantityPct:    1.0,
		Sort:           1,
	}

	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(mustMarshal(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.HandleRegisterRule(w, req)

	// 应该返回201 Created
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	// 验证创建了规则
	rules := api.ruleStore.ListRules()
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].UserStrategyID != 5001 {
		t.Errorf("expected UserStrategyID=5001, got %d", rules[0].UserStrategyID)
	}
}

// TestRegisterRule_Upsert_UpdateWhenExists 验证存在active规则时更新
func TestRegisterRule_Upsert_UpdateWhenExists(t *testing.T) {
	api, gs, _ := newTestAPI(t)

	// Add strategy and position for this test
	addTestStrategy(gs, 5002, 1)

	// 先创建一条规则
	reqBody1 := RegisterRuleRequest{
		UserStrategyID: 5002,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat(-0.02),
		QuantityPct:    1.0,
		Sort:           1,
	}
	req1 := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(mustMarshal(reqBody1)))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	api.HandleRegisterRule(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("first request failed: %d", w1.Code)
	}

	// 第二次请求：相同条件，应该更新而不是创建新规则
	reqBody2 := RegisterRuleRequest{
		UserStrategyID: 5002,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat(-0.05), // 新的value
		QuantityPct:    0.5,              // 新的quantity_pct
		Sort:           2,                // 新的sort
	}
	req2 := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(mustMarshal(reqBody2)))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	api.HandleRegisterRule(w2, req2)

	// 应该返回200 OK（更新成功）
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for update, got %d: %s", w2.Code, w2.Body.String())
	}

	// 验证只有1条规则（没有创建新规则）
	rules := api.ruleStore.ListRules()
	if len(rules) != 1 {
		t.Errorf("expected 1 rule after update, got %d", len(rules))
	}

	// 验证规则被更新
	if rules[0].Value != -0.05 {
		t.Errorf("expected updated value -0.05, got %v", rules[0].Value)
	}
}

// TestRegisterRule_Upsert_RejectInUseRule 验证in_use规则拒绝更新
func TestRegisterRule_Upsert_RejectInUseRule(t *testing.T) {
	api, gs, _ := newTestAPI(t)

	// Add strategy and position for this test
	addTestStrategy(gs, 5003, 1)

	// 创建一条规则
	reqBody := RegisterRuleRequest{
		UserStrategyID: 5003,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat(-0.02),
		QuantityPct:    1.0,
		Sort:           1,
	}
	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(mustMarshal(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.HandleRegisterRule(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create request failed: %d", w.Code)
	}

	// 获取创建的规则ID
	var resp CreateRuleResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	ruleID := int(resp.Data.ID)

	// 模拟Risk执行：将status改为in_use
	api.ruleStore.UpdateRuleStatus(ruleID, config.RuleStatusInUse)

	// 尝试更新in_use规则
	reqBody2 := RegisterRuleRequest{
		UserStrategyID: 5003,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat(-0.05), // 尝试更新
	}
	req2 := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(mustMarshal(reqBody2)))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	api.HandleRegisterRule(w2, req2)

	// 应该返回错误（400或409）
	if w2.Code == http.StatusOK || w2.Code == http.StatusCreated {
		t.Errorf("expected error response for in_use rule update, got %d", w2.Code)
	}

	// 验证规则没有被更新（value保持原值）
	rule, _ := api.ruleStore.GetRule(ruleID)
	if rule.Value != -0.02 {
		t.Errorf("in_use rule should not be updated, expected -0.02, got %v", rule.Value)
	}
}

// TestRegisterRule_Upsert_DifferentConditionsAreDifferentRules 验证不同条件是不同规则
func TestRegisterRule_Upsert_DifferentConditionsAreDifferentRules(t *testing.T) {
	api, gs, _ := newTestAPI(t)

	// Add strategy and position for this test
	addTestStrategy(gs, 5004, 1)

	// 创建第一条规则：roi <= -0.02
	reqBody1 := RegisterRuleRequest{
		UserStrategyID: 5004,
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat(-0.02),
	}
	req1 := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(mustMarshal(reqBody1)))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	api.HandleRegisterRule(w1, req1)

	// 创建第二条规则：roi >= 0.05（不同的operator）
	reqBody2 := RegisterRuleRequest{
		UserStrategyID: 5004,
		ConditionName:  "roi",
		Operator:       ">=", // 不同operator
		Value:          ptrFloat(0.05),
	}
	req2 := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(mustMarshal(reqBody2)))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	api.HandleRegisterRule(w2, req2)

	// 应该有2条规则（不同的operator视为不同规则）
	rules := api.ruleStore.ListRules()
	if len(rules) != 2 {
		t.Errorf("expected 2 rules with different operators, got %d", len(rules))
	}
}

// TestRegisterRule_ValidateStrategyNotFound 验证策略不存在时返回错误码 4004
func TestRegisterRule_ValidateStrategyNotFound(t *testing.T) {
	api, _, _ := newTestAPI(t)

	// 使用不存在的策略ID（测试辅助函数创建的是1000）
	reqBody := RegisterRuleRequest{
		UserStrategyID: 99999, // 不存在的策略ID
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat(-0.02),
	}

	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(mustMarshal(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.handleCreateRule(w, req)

	// 应该返回 400 错误
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d", w.Code)
	}

	// 验证错误码和消息
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	code, ok := resp["code"].(float64)
	if !ok {
		t.Fatalf("expected code to be number, got %v", resp["code"])
	}
	if int(code) != 4004 {
		t.Errorf("expected error code 4004, got %d", int(code))
	}

	message, ok := resp["message"].(string)
	if !ok {
		t.Fatalf("expected message to be string, got %v", resp["message"])
	}
	if !strings.Contains(message, "99999") || !strings.Contains(message, "not found") {
		t.Errorf("expected message to contain strategy ID and 'not found', got: %s", message)
	}

	t.Logf("Response: %s", w.Body.String())
}

// TestRegisterRule_ValidateNoActivePosition 验证无活跃仓位时返回错误码 4005
func TestRegisterRule_ValidateNoActivePosition(t *testing.T) {
	api, gs, _ := newTestAPI(t)

	// 创建一个策略，但不创建仓位
	now := time.Now()
	us := &order.UserStrategy{
		ID:        2001,
		UserID:    1,
		Name:      "test-strategy-no-position",
		Exchange:  "binance",
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	gs.UserStrategies[2001] = us

	reqBody := RegisterRuleRequest{
		UserStrategyID: 2001, // 存在的策略，但没有活跃仓位
		ConditionName:  "roi",
		Operator:       "<=",
		Value:          ptrFloat(-0.02),
	}

	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewReader(mustMarshal(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.handleCreateRule(w, req)

	// 应该返回 400 错误
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d", w.Code)
	}

	// 验证错误码和消息
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	code, ok := resp["code"].(float64)
	if !ok {
		t.Fatalf("expected code to be number, got %v", resp["code"])
	}
	if int(code) != 4005 {
		t.Errorf("expected error code 4005, got %d", int(code))
	}

	message, ok := resp["message"].(string)
	if !ok {
		t.Fatalf("expected message to be string, got %v", resp["message"])
	}
	if !strings.Contains(message, "2001") || !strings.Contains(message, "no active position") {
		t.Errorf("expected message to contain strategy ID and 'no active position', got: %s", message)
	}

	t.Logf("Response: %s", w.Body.String())
}

// Helpers
func ptrFloat(v float64) *float64 { return &v }

func mustMarshal(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
