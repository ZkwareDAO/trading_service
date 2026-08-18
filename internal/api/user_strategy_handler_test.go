package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/signal"
)

// setupUserStrategyServer 创建测试服务器
func setupUserStrategyServer(t *testing.T) (*httptest.Server, *persistence.GlobalState, *signal.Handler) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	mock := exchange.NewMockExchange()
	mock.SetPrice("BTCUSDT", 50000)
	h := signal.NewHandler(repo, mock)

	srv := NewServer(repo, h)
	return srv, gs, h
}

func TestListUserStrategies_EmptyResult(t *testing.T) {
	srv, gs, _ := setupUserStrategyServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	resp, err := http.Get(srv.URL + "/api/v1/user-strategies")
	if err != nil {
		t.Fatalf("GET /api/v1/user-strategies: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if result["code"].(float64) != 0 {
		t.Errorf("expected code 0, got %v", result["code"])
	}

	data := result["data"].([]interface{})
	if len(data) != 0 {
		t.Errorf("expected empty data array, got %d items", len(data))
	}
}

func TestListUserStrategies_AllStrategies(t *testing.T) {
	srv, gs, h := setupUserStrategyServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	// 创建测试数据
	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name: "test_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now,
	})

	// 创建多个策略
	h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "ICT_1H", Exchange: "binance",
		Cash: 1000, Parts: 5, Status: 1, RiskStrategyType: order.RiskStrategyTypeTraditional,
		CreatedAt: now, UpdatedAt: now,
	})

	h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "ICT_4H", Exchange: "binance",
		Cash: 2000, Parts: 3, Status: 1, RiskStrategyType: order.RiskStrategyTypeTraditional,
		CreatedAt: now, UpdatedAt: now,
	})

	gs.Shutdown() // 等待异步写入

	resp, err := http.Get(srv.URL + "/api/v1/user-strategies")
	if err != nil {
		t.Fatalf("GET /api/v1/user-strategies: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	data := result["data"].([]interface{})
	if len(data) != 2 {
		t.Errorf("expected 2 strategies, got %d", len(data))
	}
}

func TestListUserStrategies_FilterByUserID(t *testing.T) {
	srv, gs, h := setupUserStrategyServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	now := time.Now()
	userID1 := h.Repo.CreateUser(&order.User{
		Name: "user1", Exchange: "binance", CreatedAt: now, UpdatedAt: now,
	})
	userID2 := h.Repo.CreateUser(&order.User{
		Name: "user2", Exchange: "binance", CreatedAt: now, UpdatedAt: now,
	})

	h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID1, Name: "Strategy1", Exchange: "binance",
		Cash: 1000, Parts: 5, Status: 1, RiskStrategyType: order.RiskStrategyTypeTraditional,
		CreatedAt: now, UpdatedAt: now,
	})

	h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID2, Name: "Strategy2", Exchange: "binance",
		Cash: 2000, Parts: 3, Status: 1, RiskStrategyType: order.RiskStrategyTypeTraditional,
		CreatedAt: now, UpdatedAt: now,
	})

	gs.Shutdown()

	// 按user_id过滤
	resp, err := http.Get(srv.URL + "/api/v1/user-strategies?user_id=1")
	if err != nil {
		t.Fatalf("GET /api/v1/user-strategies?user_id=1: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	data := result["data"].([]interface{})
	if len(data) != 1 {
		t.Errorf("expected 1 strategy for user_id=1, got %d", len(data))
	}

	firstItem := data[0].(map[string]interface{})
	if firstItem["user_id"].(float64) != float64(userID1) {
		t.Errorf("expected user_id %d, got %v", userID1, firstItem["user_id"])
	}
}

func TestListUserStrategies_FilterByStrategyName(t *testing.T) {
	srv, gs, h := setupUserStrategyServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name: "test_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now,
	})

	h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "ICT_1H", Exchange: "binance",
		Cash: 1000, Parts: 5, Status: 1, RiskStrategyType: order.RiskStrategyTypeTraditional,
		CreatedAt: now, UpdatedAt: now,
	})

	h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "SMA_CROSS", Exchange: "binance",
		Cash: 2000, Parts: 3, Status: 1, RiskStrategyType: order.RiskStrategyTypeTraditional,
		CreatedAt: now, UpdatedAt: now,
	})

	gs.Shutdown()

	// 按strategy_name过滤
	resp, err := http.Get(srv.URL + "/api/v1/user-strategies?strategy_name=ICT_1H")
	if err != nil {
		t.Fatalf("GET /api/v1/user-strategies?strategy_name=ICT_1H: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	data := result["data"].([]interface{})
	if len(data) != 1 {
		t.Errorf("expected 1 strategy with name ICT_1H, got %d", len(data))
	}

	firstItem := data[0].(map[string]interface{})
	if firstItem["name"] != "ICT_1H" {
		t.Errorf("expected name ICT_1H, got %v", firstItem["name"])
	}
}

func TestListUserStrategies_ExcludeSensitiveFields(t *testing.T) {
	srv, gs, h := setupUserStrategyServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name: "test_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now,
	})

	h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "TestStrategy", Exchange: "binance",
		Cash: 1000, Parts: 5, Status: 1,
		RiskStrategyType: order.RiskStrategyTypeTraditional,
		StrategyID:       999, // 敏感字段
		CreatedAt:        now, UpdatedAt: now,
	})

	gs.Shutdown()

	resp, err := http.Get(srv.URL + "/api/v1/user-strategies")
	if err != nil {
		t.Fatalf("GET /api/v1/user-strategies: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	data := result["data"].([]interface{})
	if len(data) == 0 {
		t.Fatal("expected at least 1 strategy")
	}

	firstItem := data[0].(map[string]interface{})

	// 检查应该包含的安全字段
	if _, ok := firstItem["id"]; !ok {
		t.Error("expected 'id' field in response")
	}
	if _, ok := firstItem["name"]; !ok {
		t.Error("expected 'name' field in response")
	}
	if _, ok := firstItem["cash"]; !ok {
		t.Error("expected 'cash' field in response")
	}

	// 检查不应该包含的敏感字段
	if _, ok := firstItem["strategy_id"]; ok {
		t.Error("expected 'strategy_id' to be excluded from response")
	}
}
