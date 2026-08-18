package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk/config"
)

// Position API Tests
// Following TDD workflow: Tests written first, then implementation

func TestListUserOrderPositions_ByUserStrategyID(t *testing.T) {
	// Setup
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create test data: user, strategy, user_strategy, and positions
	userID := repo.CreateUser(&order.User{
		Name:      "test_user",
		Exchange:  "binance",
		CreatedAt: time.Now(),
	})

	strategyID := repo.CreateStrategy(&order.Strategy{
		Name:         "test_strategy",
		StrategyType: "cta_intraday",
		CreatedAt:    time.Now(),
	})

	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     userID,
		StrategyID: strategyID,
		Status:     1,
		CreatedAt:  time.Now(),
	})

	// Create a user_order_position
	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "BTCUSDT",
		CurrentPrice:   50000,
		Quantity:       0.1,
		Deleted:        0,
		Side:           order.SideLong,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Test: Query by user_strategy_id
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?user_strategy_id="+strconv.FormatUint(userStrategyID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["code"] != float64(0) {
		t.Errorf("Expected code=0, got %v", resp["code"])
	}

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("Expected 1 position, got %d", len(list))
	}
}

func TestListUserOrderPositions_ByUserNameAndExchange(t *testing.T) {
	// Setup
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create test data
	userID := repo.CreateUser(&order.User{
		Name:      "agent_user",
		Exchange:  "hyperliquid",
		CreatedAt: time.Now(),
	})

	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: 996,
		Exchange:       "hyperliquid",
		PosType:        order.PosTypeFutures,
		Asset:          "SOLUSDC",
		CurrentPrice:   80.5,
		Quantity:       10.0,
		Deleted:        0,
		Side:           order.SideLong,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Test: Query by user_name + exchange (Agent scenario)
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?user_name=agent_user&exchange=hyperliquid", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("Expected 1 position for agent_user@hyperliquid, got %d", len(list))
	}
}

func TestListUserOrderPositions_ByExchangeOnly(t *testing.T) {
	// Setup
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create 2 users on same exchange
	user1 := repo.CreateUser(&order.User{Name: "user1", Exchange: "binance"})
	user2 := repo.CreateUser(&order.User{Name: "user2", Exchange: "binance"})

	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         user1,
		UserStrategyID: 100,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "BTCUSDT",
		Deleted:        0,
		CreatedAt:      now,
	})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         user2,
		UserStrategyID: 200,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "ETHUSDT",
		Deleted:        0,
		CreatedAt:      now,
	})

	// Test: Query by only exchange
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?exchange=binance", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify: should return both users' positions
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 2 {
		t.Errorf("Expected 2 positions (all users on binance), got %d", len(list))
	}
}

func TestListUserOrderPositions_Pagination(t *testing.T) {
	// Setup
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create 15 positions
	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})
	now := time.Now()
	for i := 0; i < 15; i++ {
		repo.CreateUserOrderPosition(&order.UserOrderPosition{
			UserID:         userID,
			UserStrategyID: 996,
			Exchange:       "binance",
			PosType:        order.PosTypeFutures,
			Asset:          "BTCUSDT",
			Deleted:        0,
			CreatedAt:      now,
		})
	}

	// Test: page=1, page_size=10
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 10 {
		t.Errorf("Expected 10 items (page_size), got %d", len(list))
	}
	if data["total"] != float64(15) {
		t.Errorf("Expected total=15, got %v", data["total"])
	}
}

func TestListUserOrderPositions_StrategyNameOnlyReturnsEmpty(t *testing.T) {
	// strategy_name without user_id now returns empty data (not error)
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?strategy_name=test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != float64(0) {
		t.Errorf("Expected success code 0, got %v", resp["code"])
	}
}

// TestListUserOrderPositions_StrategyNameNoMatchReturnsEmpty verifies that when
// strategy_name is provided but matches no user_strategy, the API returns an
// empty list rather than silently dropping the filter and returning everything.
func TestListUserOrderPositions_StrategyNameNoMatchReturnsEmpty(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Seed positions that MUST NOT leak into the result when strategy_name misses.
	userID := repo.CreateUser(&order.User{Name: "alice", Exchange: "binance"})
	stratID := repo.CreateStrategy(&order.Strategy{Name: "DOLPHIN_USDT", StrategyType: "cta_intraday"})
	usID := repo.CreateUserStrategy(&order.UserStrategy{UserID: userID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})
	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: usID, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "BTC", Deleted: 0, CreatedAt: now, UpdatedAt: now})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: usID, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "ETH", Deleted: 0, CreatedAt: now, UpdatedAt: now})

	// strategy_name that matches no stored user_strategy.
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?strategy_name=NONEXISTENT_STRATEGY", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != float64(0) {
		t.Fatalf("Expected success code 0, got %v", resp["code"])
	}
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 0 {
		t.Errorf("Expected 0 positions for non-matching strategy_name, got %d", len(list))
	}
	if data["total"] != float64(0) {
		t.Errorf("Expected total=0, got %v", data["total"])
	}
}

// TestListUserOrderPositions_StrategyNameWithUserIDNoMatchReturnsEmpty covers
// the user_id + strategy_name path: a non-matching strategy_name must return
// empty instead of all positions across all users.
func TestListUserOrderPositions_StrategyNameWithUserIDNoMatchReturnsEmpty(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "alice", Exchange: "binance"})
	stratID := repo.CreateStrategy(&order.Strategy{Name: "DOLPHIN_USDT", StrategyType: "cta_intraday"})
	usID := repo.CreateUserStrategy(&order.UserStrategy{UserID: userID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})
	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: usID, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "BTC", Deleted: 0, CreatedAt: now, UpdatedAt: now})

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/user-order-positions?user_id=%d&strategy_name=NONEXISTENT_STRATEGY", userID), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 0 {
		t.Errorf("Expected 0 positions for non-matching strategy_name+user_id, got %d", len(list))
	}
}

// TestListUserPositions_StrategyNameNoMatchReturnsEmpty is the user-positions
// counterpart of the regression guard above.
func TestListUserPositions_StrategyNameNoMatchReturnsEmpty(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "alice", Exchange: "binance"})
	stratID := repo.CreateStrategy(&order.Strategy{Name: "DOLPHIN_USDT", StrategyType: "cta_intraday"})
	usID := repo.CreateUserStrategy(&order.UserStrategy{UserID: userID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})
	now := time.Now()
	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: usID, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 1, Deleted: 0, CreatedAt: now, UpdatedAt: now})

	req := httptest.NewRequest("GET", "/api/v1/user-positions?strategy_name=NONEXISTENT_STRATEGY", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 0 {
		t.Errorf("Expected 0 positions for non-matching strategy_name, got %d", len(list))
	}
	if data["total"] != float64(0) {
		t.Errorf("Expected total=0, got %v", data["total"])
	}
}

func TestListUserPositions_ByUserNameAndExchange(t *testing.T) {
	// Setup
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "hyperliquid"})
	now := time.Now()

	// Create UserPosition with required fields
	repo.CreateUserPosition(&order.UserPosition{
		UserID:         userID,
		UserStrategyID: 996,
		Exchange:       "hyperliquid",
		PosType:        order.PosTypeFutures,
		Quantity:       10.0,
		TotalMargin:    133.775,
		PnL:            7.65,
		ROI:            0.0347, // Make sure this field is set
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Test
	req := httptest.NewRequest("GET", "/api/v1/user-positions?user_name=test_user&exchange=hyperliquid", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("Expected 1 aggregated position, got %d: %v", len(list), list)
		return
	}

	pos := list[0].(map[string]interface{})
	// Check that position has expected fields and ROI value
	if roi, ok := pos["roi"]; !ok {
		t.Errorf("Expected roi field in position: %v", pos)
	} else {
		// Verify ROI is not zero
		roiFloat, ok := roi.(float64)
		if !ok {
			t.Errorf("Expected roi as float64, got: %v", roi)
		} else if roiFloat != 0.0347 {
			t.Errorf("Expected roi=0.0347, got: %v", roiFloat)
		}
	}
}

func TestGetUserPositionByID(t *testing.T) {
	// Setup
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	now := time.Now()
	posID := repo.CreateUserPosition(&order.UserPosition{
		UserID:         100,
		UserStrategyID: 996,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Quantity:       0.1,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Test
	req := httptest.NewRequest("GET", "/api/v1/user-positions/"+strconv.FormatUint(posID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	data := resp["data"].(map[string]interface{})
	if data == nil {
		t.Fatalf("data is nil, response: %v", resp)
	}

	// Check ID field (JSON unmarshals uint64 as float64)
	idVal := data["id"]
	if idVal == nil {
		t.Errorf("Expected id field in data, got: %v", data)
		return
	}

	idFloat, ok := idVal.(float64)
	if !ok {
		t.Errorf("Expected id field as float64, got type %T: %v", idVal, idVal)
		return
	}

	if uint64(idFloat) != posID {
		t.Errorf("Expected id=%d, got %v", posID, idFloat)
	}
}

// Helper function
func TestListUserOrderPositions_OrderByIDDesc(t *testing.T) {
	// Setup
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})
	now := time.Now()
	// Create 3 positions: id auto-increments 1,2,3 but CreatedAt decreases,
	// so id-desc order (C,B,A) differs from CreatedAt-desc order (A,B,C).
	// Asserting C,B,A proves sort is by id, not CreatedAt.
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: userID, UserStrategyID: 996, Exchange: "binance",
		PosType: order.PosTypeFutures, Asset: "A", Deleted: 0, CreatedAt: now,
	})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: userID, UserStrategyID: 996, Exchange: "binance",
		PosType: order.PosTypeFutures, Asset: "B", Deleted: 0, CreatedAt: now.Add(-time.Minute),
	})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: userID, UserStrategyID: 996, Exchange: "binance",
		PosType: order.PosTypeFutures, Asset: "C", Deleted: 0, CreatedAt: now.Add(-2 * time.Minute),
	})

	req := httptest.NewRequest("GET", "/api/v1/user-order-positions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})

	if len(list) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(list))
	}
	wantAssets := []string{"C", "B", "A"} // id-desc: C(3), B(2), A(1)
	for i, want := range wantAssets {
		item := list[i].(map[string]interface{})
		if item["asset"] != want {
			t.Errorf("position %d: expected asset %s (id-desc), got %v", i, want, item["asset"])
		}
	}
}

func TestListUserPositions_OrderByIDDesc(t *testing.T) {
	// Setup
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})
	now := time.Now()
	// id increments 1,2,3; CreatedAt decreases. Assert id-desc (C,B,A).
	repo.CreateUserPosition(&order.UserPosition{
		UserID: userID, UserStrategyID: 996, Exchange: "binance",
		PosType: order.PosTypeFutures, Deleted: 0, CreatedAt: now,
	})
	repo.CreateUserPosition(&order.UserPosition{
		UserID: userID, UserStrategyID: 997, Exchange: "binance",
		PosType: order.PosTypeFutures, Deleted: 0, CreatedAt: now.Add(-time.Minute),
	})
	repo.CreateUserPosition(&order.UserPosition{
		UserID: userID, UserStrategyID: 998, Exchange: "binance",
		PosType: order.PosTypeFutures, Deleted: 0, CreatedAt: now.Add(-2 * time.Minute),
	})

	req := httptest.NewRequest("GET", "/api/v1/user-positions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})

	if len(list) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(list))
	}
	// id-desc: strategy 998(3), 997(2), 996(1)
	wantStrategyIDs := []float64{998, 997, 996}
	for i, want := range wantStrategyIDs {
		item := list[i].(map[string]interface{})
		if item["user_strategy_id"] != want {
			t.Errorf("position %d: expected user_strategy_id %v (id-desc), got %v", i, want, item["user_strategy_id"])
		}
	}
}

func setupTestState(t *testing.T) (*persistence.GlobalState, *persistence.StateRepository, *config.RuleStore) {
	t.Helper()
	gs, err := persistence.NewGlobalState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gs.Shutdown() })
	repo := persistence.NewStateRepository(gs)

	// Create empty rule.csv with header
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "rule.csv")
	if err := os.WriteFile(csvPath, []byte("id,user_strategy_id,condition_name,operator,value,sort,status,action,params\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ruleStore, err := config.NewRuleStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	return gs, repo, ruleStore
}

// ===== Additional Tests for Complete Coverage =====

func TestGetUserOrderPositionByID(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "binance"})
	now := time.Now()
	posID := repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: 996,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "BTCUSDT",
		CurrentPrice:   50000,
		Quantity:       0.1,
		Deleted:        0,
		Side:           order.SideLong,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Test: Get by ID
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions/"+strconv.FormatUint(posID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	if data == nil {
		t.Fatalf("data is nil, response: %v", resp)
	}

	// Check ID field
	idVal := data["id"]
	if idVal == nil {
		t.Errorf("Expected id field in data, got: %v", data)
		return
	}

	idFloat, ok := idVal.(float64)
	if !ok {
		t.Errorf("Expected id field as float64, got type %T: %v", idVal, idVal)
		return
	}

	if uint64(idFloat) != posID {
		t.Errorf("Expected id=%d, got %v", posID, idFloat)
	}
}

func TestListUserPositions_ByUserStrategyID(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "binance"})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "test_strategy", StrategyType: "cta_intraday"})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     userID,
		StrategyID: strategyID,
		Name:       "my_strategy",
		Status:     1,
	})

	now := time.Now()
	repo.CreateUserPosition(&order.UserPosition{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Quantity:       10.0,
		TotalMargin:    100,
		PnL:            10,
		ROI:            0.1,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Test: Query by user_strategy_id
	req := httptest.NewRequest("GET", "/api/v1/user-positions?user_strategy_id="+strconv.FormatUint(userStrategyID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("Expected 1 position, got %d", len(list))
	}
}

func TestListUserPositions_ByExchangeOnly(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create 2 users on same exchange
	user1 := repo.CreateUser(&order.User{Name: "user1", Exchange: "binance"})
	user2 := repo.CreateUser(&order.User{Name: "user2", Exchange: "binance"})

	now := time.Now()
	repo.CreateUserPosition(&order.UserPosition{
		UserID:         user1,
		UserStrategyID: 100,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Quantity:       10,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	repo.CreateUserPosition(&order.UserPosition{
		UserID:         user2,
		UserStrategyID: 200,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Quantity:       20,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Test: Query by only exchange
	req := httptest.NewRequest("GET", "/api/v1/user-positions?exchange=binance", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 2 {
		t.Errorf("Expected 2 positions (all users on binance), got %d", len(list))
	}
}

func TestListUserPositions_Pagination(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})
	now := time.Now()
	for i := 0; i < 15; i++ {
		repo.CreateUserPosition(&order.UserPosition{
			UserID:         userID,
			UserStrategyID: 996,
			Exchange:       "binance",
			PosType:        order.PosTypeFutures,
			Quantity:       10,
			Deleted:        0,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}

	// Test: page=1, page_size=10
	req := httptest.NewRequest("GET", "/api/v1/user-positions?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 10 {
		t.Errorf("Expected 10 items (page_size), got %d", len(list))
	}
	if data["total"] != float64(15) {
		t.Errorf("Expected total=15, got %v", data["total"])
	}
}

func TestListUserOrderPositions_ByUserNameOnly(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create user on two exchanges
	userID1 := repo.CreateUser(&order.User{Name: "multi_exchange_user", Exchange: "binance"})
	userID2 := repo.CreateUser(&order.User{Name: "multi_exchange_user", Exchange: "hyperliquid"})

	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID1,
		UserStrategyID: 100,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "BTCUSDT",
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID2,
		UserStrategyID: 200,
		Exchange:       "hyperliquid",
		PosType:        order.PosTypeFutures,
		Asset:          "ETHUSDC",
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Test: Query by user_name only (should return positions from all exchanges)
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?user_name=multi_exchange_user", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 2 {
		t.Errorf("Expected 2 positions (both exchanges), got %d", len(list))
	}
}

func TestListUserPositions_ByUserNameOnly(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID1 := repo.CreateUser(&order.User{Name: "multi_user", Exchange: "binance"})
	userID2 := repo.CreateUser(&order.User{Name: "multi_user", Exchange: "hyperliquid"})

	now := time.Now()
	repo.CreateUserPosition(&order.UserPosition{
		UserID:         userID1,
		UserStrategyID: 100,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Quantity:       10,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	repo.CreateUserPosition(&order.UserPosition{
		UserID:         userID2,
		UserStrategyID: 200,
		Exchange:       "hyperliquid",
		PosType:        order.PosTypeFutures,
		Quantity:       20,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	req := httptest.NewRequest("GET", "/api/v1/user-positions?user_name=multi_user", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 2 {
		t.Errorf("Expected 2 positions (both exchanges), got %d", len(list))
	}
}

// ===== Tests for strategy_name-only query + time range filtering =====

func TestListUserOrderPositions_StrategyNameOnly(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	user1 := repo.CreateUser(&order.User{Name: "alice", Exchange: "binance"})
	user2 := repo.CreateUser(&order.User{Name: "bob", Exchange: "hyperliquid"})
	stratID := repo.CreateStrategy(&order.Strategy{Name: "DOLPHIN_USDT", StrategyType: "cta_intraday"})
	us1 := repo.CreateUserStrategy(&order.UserStrategy{UserID: user1, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})
	us2 := repo.CreateUserStrategy(&order.UserStrategy{UserID: user2, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})

	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: user1, UserStrategyID: us1, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "BTC", Deleted: 0, CreatedAt: now, UpdatedAt: now})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: user2, UserStrategyID: us2, Exchange: "hyperliquid", PosType: order.PosTypeFutures, Asset: "ETH", Deleted: 0, CreatedAt: now, UpdatedAt: now})

	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?strategy_name=DOLPHIN_USDT", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 2 {
		t.Errorf("Expected 2 positions for strategy_name only, got %d", len(list))
	}
}

func TestListUserPositions_StrategyNameOnly(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	user1 := repo.CreateUser(&order.User{Name: "alice", Exchange: "binance"})
	user2 := repo.CreateUser(&order.User{Name: "bob", Exchange: "hyperliquid"})
	stratID := repo.CreateStrategy(&order.Strategy{Name: "DOLPHIN_USDT", StrategyType: "cta_intraday"})
	us1 := repo.CreateUserStrategy(&order.UserStrategy{UserID: user1, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})
	us2 := repo.CreateUserStrategy(&order.UserStrategy{UserID: user2, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})

	now := time.Now()
	repo.CreateUserPosition(&order.UserPosition{UserID: user1, UserStrategyID: us1, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 1, Deleted: 0, CreatedAt: now, UpdatedAt: now})
	repo.CreateUserPosition(&order.UserPosition{UserID: user2, UserStrategyID: us2, Exchange: "hyperliquid", PosType: order.PosTypeFutures, Quantity: 2, Deleted: 0, CreatedAt: now, UpdatedAt: now})

	req := httptest.NewRequest("GET", "/api/v1/user-positions?strategy_name=DOLPHIN_USDT", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 2 {
		t.Errorf("Expected 2 positions for strategy_name only, got %d", len(list))
	}
}

func TestListUserOrderPositions_StrategyNameWithUserID_StillAdaptsSuffix(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "alice", Exchange: "binance"})
	stratID := repo.CreateStrategy(&order.Strategy{Name: "DOLPHIN_USDC", StrategyType: "cta_intraday"})
	usID := repo.CreateUserStrategy(&order.UserStrategy{UserID: userID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})

	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: usID, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "BTC", Deleted: 0, CreatedAt: now, UpdatedAt: now})

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/user-order-positions?user_id=%d&strategy_name=DOLPHIN_USDC", userID), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("Expected 1 position (suffix adapted), got %d", len(list))
	}
}

// TestListUserOrderPositions_UserIDAndStrategyName_MultipleStrategies verifies that
// when a user has multiple user_strategies with the same name, all matching positions are returned.
func TestListUserOrderPositions_UserIDAndStrategyName_MultipleStrategies(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "alice", Exchange: "binance"})
	stratID := repo.CreateStrategy(&order.Strategy{Name: "DOLPHIN_USDT", StrategyType: "cta_intraday"})

	// Same user, same strategy name → two user_strategy records
	usID1 := repo.CreateUserStrategy(&order.UserStrategy{UserID: userID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})
	usID2 := repo.CreateUserStrategy(&order.UserStrategy{UserID: userID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})

	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: usID1, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "BTC", Deleted: 0, CreatedAt: now, UpdatedAt: now})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: usID2, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "ETH", Deleted: 0, CreatedAt: now, UpdatedAt: now})

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/user-order-positions?user_id=%d&strategy_name=DOLPHIN_USDT", userID), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 2 {
		t.Errorf("Expected 2 positions (both user_strategy IDs), got %d", len(list))
	}
}

func TestListUserPositions_UserIDAndStrategyName_MultipleStrategies(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "bob", Exchange: "binance"})
	stratID := repo.CreateStrategy(&order.Strategy{Name: "DOLPHIN_USDT", StrategyType: "cta_intraday"})

	usID1 := repo.CreateUserStrategy(&order.UserStrategy{UserID: userID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})
	usID2 := repo.CreateUserStrategy(&order.UserStrategy{UserID: userID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})

	now := time.Now()
	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: usID1, Exchange: "binance", PosType: order.PosTypeFutures, Deleted: 0, CreatedAt: now, UpdatedAt: now})
	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: usID2, Exchange: "binance", PosType: order.PosTypeFutures, Deleted: 0, CreatedAt: now, UpdatedAt: now})

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/user-positions?user_id=%d&strategy_name=DOLPHIN_USDT", userID), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 2 {
		t.Errorf("Expected 2 positions (both user_strategy IDs), got %d", len(list))
	}
}

func TestListUserOrderPositions_CreatedFromTo(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})

	t1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 1, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "A", Deleted: 0, CreatedAt: t1, UpdatedAt: t1})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 2, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "B", Deleted: 0, CreatedAt: t2, UpdatedAt: t2})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 3, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "C", Deleted: 0, CreatedAt: t3, UpdatedAt: t3})

	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?user_id="+strconv.FormatUint(userID, 10)+"&created_from=2026-07-01T00:00:00Z&created_to=2026-07-31T23:59:59Z", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 2 {
		t.Errorf("Expected 2 positions in July, got %d", len(list))
	}
}

func TestListUserPositions_CreatedFromTo(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})

	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 1, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 1, Deleted: 0, CreatedAt: t1, UpdatedAt: t1})
	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 2, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 2, Deleted: 0, CreatedAt: t2, UpdatedAt: t2})

	req := httptest.NewRequest("GET", "/api/v1/user-positions?user_id="+strconv.FormatUint(userID, 10)+"&created_from=2026-06-15T00:00:00Z", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("Expected 1 position after June 15, got %d", len(list))
	}
}

func TestListUserOrderPositions_CloseFromTo(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})

	c1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	c2 := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	c3 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 1, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "A", Deleted: 1, CloseTime: &c1, CreatedAt: c1, UpdatedAt: c1})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 2, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "B", Deleted: 1, CloseTime: &c2, CreatedAt: c2, UpdatedAt: c2})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 3, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "C", Deleted: 1, CloseTime: &c3, CreatedAt: c3, UpdatedAt: c3})

	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?user_id="+strconv.FormatUint(userID, 10)+"&close_from=2026-07-01T00:00:00Z&close_to=2026-07-31T23:59:59Z", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 2 {
		t.Errorf("Expected 2 positions closed in July, got %d", len(list))
	}
}

func TestListUserOrderPositions_CloseFromExcludesActive(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})

	c1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// 一条已平仓、一条活跃（CloseTime==nil）
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 1, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "A", Deleted: 1, CloseTime: &c1, CreatedAt: c1, UpdatedAt: c1})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 2, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "B", Deleted: 0, CreatedAt: c1, UpdatedAt: c1})

	// 传 close_from 后，活跃仓位必须被排除，只返回 1 条已平仓
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?user_id="+strconv.FormatUint(userID, 10)+"&close_from=2026-06-01T00:00:00Z", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("Expected 1 closed position (active excluded) when close_from set, got %d", len(list))
	}
}

func TestListUserPositions_CloseFromTo(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})

	c1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c2 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 1, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 1, Deleted: 1, CloseTime: &c1, CreatedAt: c1, UpdatedAt: c1})
	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 2, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 2, Deleted: 1, CloseTime: &c2, CreatedAt: c2, UpdatedAt: c2})

	req := httptest.NewRequest("GET", "/api/v1/user-positions?user_id="+strconv.FormatUint(userID, 10)+"&close_from=2026-06-15T00:00:00Z&close_to=2026-07-31T23:59:59Z", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("Expected 1 position closed after June 15, got %d", len(list))
	}
}

func TestListUserPositions_CloseFromExcludesActive(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})

	c1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 1, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 1, Deleted: 1, CloseTime: &c1, CreatedAt: c1, UpdatedAt: c1})
	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 2, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 2, Deleted: 0, CreatedAt: c1, UpdatedAt: c1})

	// 传 close_from 后活跃仓位必须排除，只返回 1 条已平仓
	req := httptest.NewRequest("GET", "/api/v1/user-positions?user_id="+strconv.FormatUint(userID, 10)+"&close_from=2026-05-01T00:00:00Z", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("Expected 1 closed position (active excluded) when close_from set, got %d", len(list))
	}
}

func TestListUserOrderPositions_InvalidCloseFrom(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?close_from=not-a-date", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid close_from, got %d", w.Code)
	}
}

func TestListUserPositions_InvalidCloseTo(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	req := httptest.NewRequest("GET", "/api/v1/user-positions?close_to=bad-format", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid close_to, got %d", w.Code)
	}
}

func TestListUserOrderPositions_InvalidCreatedFrom(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?created_from=not-a-date", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid created_from, got %d", w.Code)
	}
}

func TestListUserPositions_InvalidCreatedTo(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	req := httptest.NewRequest("GET", "/api/v1/user-positions?created_to=bad-format", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid created_to, got %d", w.Code)
	}
}

// ===== Test user_id + strategy_name query =====

func TestListUserOrderPositions_ByUserIDAndStrategyName(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create test data
	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "binance"})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "my_strategy", StrategyType: "cta_intraday"})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     userID,
		StrategyID: strategyID,
		Name:       "my_strategy",
		Status:     1,
	})

	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "BTCUSDT",
		CurrentPrice:   50000,
		Quantity:       0.1,
		Deleted:        0,
		Side:           order.SideLong,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Test: Query by user_id + strategy_name
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/user-order-positions?user_id=%d&strategy_name=my_strategy", userID), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("Expected 1 position, got %d", len(list))
	}
}

func TestListUserPositions_ByUserIDAndStrategyName(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create test data
	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "binance"})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "my_strategy", StrategyType: "cta_intraday"})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     userID,
		StrategyID: strategyID,
		Name:       "my_strategy",
		Status:     1,
	})

	now := time.Now()
	repo.CreateUserPosition(&order.UserPosition{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Quantity:       10,
		TotalMargin:    100,
		PnL:            10,
		ROI:            0.1,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Test: Query by user_id + strategy_name
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/user-positions?user_id=%d&strategy_name=my_strategy", userID), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("Expected 1 position, got %d", len(list))
	}
}

func TestListUserPositions_UserIDAndStrategyNameNoMatchReturnsEmpty(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "binance"})

	// Query with non-existent strategy_name + user_id: contract is to return an
	// empty list (200), not an error.
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/user-positions?user_id=%d&strategy_name=nonexistent", userID), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != float64(0) {
		t.Fatalf("Expected success code 0, got %v", resp["code"])
	}
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 0 {
		t.Errorf("Expected 0 positions for non-matching strategy_name+user_id, got %d", len(list))
	}
}

func TestListUserOrderPositions_ErrorInvalidUserStrategyID(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Test: Invalid user_strategy_id format
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?user_strategy_id=invalid", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != float64(1001) {
		t.Errorf("Expected error code 1001, got %v", resp["code"])
	}
}

func TestListUserOrderPositions_ErrorInvalidPage(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Test: Invalid page number
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?page=-1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestListUserOrderPositions_ErrorInvalidPageSize(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Test: Invalid page_size (too large)
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?page_size=200", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestListUserOrderPositions_ErrorInvalidSide(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Test: Invalid side value
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?side=2", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestListUserOrderPositions_ErrorInvalidDeleted(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Test: Invalid deleted value
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?deleted=2", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestListUserOrderPositions_ErrorInvalidPosType(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Test: Invalid pos_type value
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?pos_type=3", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestGetUserOrderPositionByID_ErrorNotFound(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Test: Non-existent ID
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions/99999", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != float64(5001) {
		t.Errorf("Expected error code 5001, got %v", resp["code"])
	}
}

func TestGetUserPositionByID_ErrorNotFound(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Test: Non-existent ID
	req := httptest.NewRequest("GET", "/api/v1/user-positions/99999", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != float64(5001) {
		t.Errorf("Expected error code 5001, got %v", resp["code"])
	}
}

func TestListUserOrderPositions_ErrorUserNotFound(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Test: Non-existent user_name
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?user_name=nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify: an unknown user_name is reported as an error, not an empty result
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != float64(1001) {
		t.Errorf("Expected error code 1001, got %v", resp["code"])
	}
}

// ===== Tests for strategy_name and user_name enrichment =====

func TestListUserOrderPositions_ContainsStrategyNameAndUserName(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "alice", Exchange: "binance"})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "cta_BTCUSDT", StrategyType: "cta_intraday"})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     userID,
		StrategyID: strategyID,
		Name:       "cta_BTCUSDT",
		Status:     1,
	})

	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "BTCUSDT",
		Quantity:       0.1,
		Deleted:        0,
		Side:           order.SideLong,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?user_strategy_id="+strconv.FormatUint(userStrategyID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("Expected 1 position, got %d", len(list))
	}

	pos := list[0].(map[string]interface{})

	// Verify strategy_name field exists and has correct value
	if strategyName, ok := pos["strategy_name"]; !ok {
		t.Error("Missing 'strategy_name' field in user-order-positions list response")
	} else if strategyName != "cta_BTCUSDT" {
		t.Errorf("Expected strategy_name='cta_BTCUSDT', got '%v'", strategyName)
	}

	// Verify user_name field exists and has correct value
	if userName, ok := pos["user_name"]; !ok {
		t.Error("Missing 'user_name' field in user-order-positions list response")
	} else if userName != "alice" {
		t.Errorf("Expected user_name='alice', got '%v'", userName)
	}
}

func TestGetUserOrderPositionByID_ContainsStrategyNameAndUserName(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "bob", Exchange: "binance"})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "cta_ETHUSDT", StrategyType: "cta_intraday"})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     userID,
		StrategyID: strategyID,
		Name:       "cta_ETHUSDT",
		Status:     1,
	})

	now := time.Now()
	posID := repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "ETHUSDT",
		Quantity:       1.0,
		Deleted:        0,
		Side:           order.SideLong,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	req := httptest.NewRequest("GET", "/api/v1/user-order-positions/"+strconv.FormatUint(posID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})

	// Verify strategy_name field
	if strategyName, ok := data["strategy_name"]; !ok {
		t.Error("Missing 'strategy_name' field in user-order-position by-ID response")
	} else if strategyName != "cta_ETHUSDT" {
		t.Errorf("Expected strategy_name='cta_ETHUSDT', got '%v'", strategyName)
	}

	// Verify user_name field
	if userName, ok := data["user_name"]; !ok {
		t.Error("Missing 'user_name' field in user-order-position by-ID response")
	} else if userName != "bob" {
		t.Errorf("Expected user_name='bob', got '%v'", userName)
	}
}

func TestListUserPositions_ContainsStrategyNameAndUserName(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "charlie", Exchange: "binance"})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "cta_BTCUSDT", StrategyType: "cta_intraday"})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     userID,
		StrategyID: strategyID,
		Name:       "cta_BTCUSDT",
		Status:     1,
	})

	now := time.Now()
	repo.CreateUserPosition(&order.UserPosition{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Quantity:       0.5,
		TotalMargin:    100,
		PnL:            10,
		ROI:            0.1,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	req := httptest.NewRequest("GET", "/api/v1/user-positions?user_strategy_id="+strconv.FormatUint(userStrategyID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("Expected 1 position, got %d", len(list))
	}

	pos := list[0].(map[string]interface{})

	// Verify strategy_name field
	if strategyName, ok := pos["strategy_name"]; !ok {
		t.Error("Missing 'strategy_name' field in user-positions list response")
	} else if strategyName != "cta_BTCUSDT" {
		t.Errorf("Expected strategy_name='cta_BTCUSDT', got '%v'", strategyName)
	}

	// Verify user_name field
	if userName, ok := pos["user_name"]; !ok {
		t.Error("Missing 'user_name' field in user-positions list response")
	} else if userName != "charlie" {
		t.Errorf("Expected user_name='charlie', got '%v'", userName)
	}
}

func TestGetUserPositionByID_ContainsStrategyNameAndUserName(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "dave", Exchange: "binance"})
	strategyID := repo.CreateStrategy(&order.Strategy{Name: "cta_SOLUSDT", StrategyType: "cta_intraday"})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     userID,
		StrategyID: strategyID,
		Name:       "cta_SOLUSDT",
		Status:     1,
	})

	now := time.Now()
	posID := repo.CreateUserPosition(&order.UserPosition{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Quantity:       5.0,
		TotalMargin:    200,
		PnL:            20,
		ROI:            0.1,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	req := httptest.NewRequest("GET", "/api/v1/user-positions/"+strconv.FormatUint(posID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})

	// Verify strategy_name field
	if strategyName, ok := data["strategy_name"]; !ok {
		t.Error("Missing 'strategy_name' field in user-position by-ID response")
	} else if strategyName != "cta_SOLUSDT" {
		t.Errorf("Expected strategy_name='cta_SOLUSDT', got '%v'", strategyName)
	}

	// Verify user_name field
	if userName, ok := data["user_name"]; !ok {
		t.Error("Missing 'user_name' field in user-position by-ID response")
	} else if userName != "dave" {
		t.Errorf("Expected user_name='dave', got '%v'", userName)
	}
}

func TestListUserOrderPositions_EmptyNameWhenNotFound(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create position with user_strategy_id that has no matching UserStrategy
	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         9999, // non-existent user
		UserStrategyID: 8888, // non-existent strategy
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "BTCUSDT",
		Quantity:       0.1,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	req := httptest.NewRequest("GET", "/api/v1/user-order-positions?exchange=binance", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("Expected 1 position, got %d", len(list))
	}

	pos := list[0].(map[string]interface{})

	// When user/strategy not found, should return empty string (not missing field)
	if strategyName, ok := pos["strategy_name"]; !ok {
		t.Error("Missing 'strategy_name' field even when strategy not found")
	} else if strategyName != "" {
		t.Errorf("Expected empty strategy_name for unknown strategy, got '%v'", strategyName)
	}

	if userName, ok := pos["user_name"]; !ok {
		t.Error("Missing 'user_name' field even when user not found")
	} else if userName != "" {
		t.Errorf("Expected empty user_name for unknown user, got '%v'", userName)
	}
}

// ===== Repo Error Logging Tests =====

func TestGetUserOrderPositionByID_LogsWhenRepoError(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create position with non-existent user_id and strategy_id
	// enrichment should still succeed with empty user_name/strategy_name
	now := time.Now()
	posID := repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         9999, // non-existent
		UserStrategyID: 8888, // non-existent
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "ETHUSDT",
		Quantity:       1.0,
		Deleted:        0,
		Side:           order.SideLong,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	req := httptest.NewRequest("GET", "/api/v1/user-order-positions/"+strconv.FormatUint(posID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify enrichment returns empty strings for missing user/strategy
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	if data["user_name"] != "" {
		t.Errorf("Expected empty user_name for non-existent user, got: %v", data["user_name"])
	}
	if data["strategy_name"] != "" {
		t.Errorf("Expected empty strategy_name for non-existent strategy, got: %v", data["strategy_name"])
	}
}

func TestGetUserPositionByID_LogsWhenRepoError(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create position with non-existent user_id and strategy_id
	// enrichment should still succeed with empty user_name/strategy_name
	now := time.Now()
	posID := repo.CreateUserPosition(&order.UserPosition{
		UserID:         7777, // non-existent
		UserStrategyID: 6666, // non-existent
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Quantity:       0.5,
		TotalMargin:    100,
		PnL:            10,
		ROI:            0.1,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	req := httptest.NewRequest("GET", "/api/v1/user-positions/"+strconv.FormatUint(posID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify enrichment returns empty strings for missing user/strategy
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	if data["user_name"] != "" {
		t.Errorf("Expected empty user_name for non-existent user, got: %v", data["user_name"])
	}
	if data["strategy_name"] != "" {
		t.Errorf("Expected empty strategy_name for non-existent strategy, got: %v", data["strategy_name"])
	}
}

// ===== API Response Format Verification =====

func TestUserOrderPosition_ResponseFormat(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "binance"})
	now := time.Now()
	posID := repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: 996,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Asset:          "BTCUSDT",
		CurrentPrice:   50000.0,
		Quantity:       0.1,
		PosValue:       5000.0,
		Leverage:       10,
		Deleted:        0,
		InitMargin:     500.0,
		PosPrice:       49000.0,
		PnLValue:       100.0,
		Side:           order.SideLong,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Test: Get single position
	req := httptest.NewRequest("GET", "/api/v1/user-order-positions/"+strconv.FormatUint(posID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify response format
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Check envelope fields
	if resp["code"] != float64(0) {
		t.Errorf("Expected code=0, got %v", resp["code"])
	}
	if resp["message"] != "success" {
		t.Errorf("Expected message='success', got %v", resp["message"])
	}

	data := resp["data"].(map[string]interface{})

	// Check field names match documentation (snake_case)
	expectedFields := []string{
		"id", "user_id", "user_strategy_id", "exchange", "pos_type",
		"asset", "current_price", "quantity", "pos_value", "leverage",
		"deleted", "init_margin", "pos_price", "pnl_value", "side",
		"created_at", "updated_at",
	}

	for _, field := range expectedFields {
		if _, ok := data[field]; !ok {
			t.Errorf("Missing expected field '%s' in response", field)
		}
	}
}

func TestUserPosition_ResponseFormat(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "test_user", Exchange: "binance"})
	now := time.Now()
	posID := repo.CreateUserPosition(&order.UserPosition{
		UserID:                     userID,
		UserStrategyID:             996,
		Exchange:                   "binance",
		PosType:                    order.PosTypeFutures,
		Quantity:                   10.0,
		LatestMarketCapitalization: 1000.0,
		ROI:                        0.1,
		PnL:                        100.0,
		WinRate:                    0.5,
		MaximumDrawdown:            0.05,
		TotalMargin:                1000.0,
		MaxProfitPercentage:        0.15,
		MaxLossPercentage:          0.02,
		OpenTrades:                 2,
		ClosedTrades:               1,
		ProfitTrades:               1,
		LossTrades:                 0,
		Deleted:                    0,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	})

	// Test: Get single position
	req := httptest.NewRequest("GET", "/api/v1/user-positions/"+strconv.FormatUint(posID, 10), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify response format
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Check envelope fields
	if resp["code"] != float64(0) {
		t.Errorf("Expected code=0, got %v", resp["code"])
	}
	if resp["message"] != "success" {
		t.Errorf("Expected message='success', got %v", resp["message"])
	}

	data := resp["data"].(map[string]interface{})

	// Check field names match documentation (snake_case)
	expectedFields := []string{
		"id", "user_id", "user_strategy_id", "exchange", "pos_type",
		"quantity", "latest_market_capitalization", "roi", "pnl",
		"win_rate", "maximum_drawdown", "total_margin",
		"max_profit_percentage", "max_loss_percentage",
		"open_trades", "closed_trades", "profit_trades", "loss_trades",
		"deleted", "created_at", "updated_at",
	}

	for _, field := range expectedFields {
		if _, ok := data[field]; !ok {
			t.Errorf("Missing expected field '%s' in response", field)
		}
	}
}
