package api

import (
	"bytes"
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

func setupServer(t *testing.T) (*httptest.Server, *persistence.GlobalState, *signal.Handler) {
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

func TestHealthEndpoint(t *testing.T) {
	srv, gs, _ := setupServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "healthy" {
		t.Errorf("expected 'healthy', got '%s'", body["status"])
	}
}

func TestStateEndpoint(t *testing.T) {
	srv, gs, _ := setupServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	resp, err := http.Get(srv.URL + "/api/v1/state")
	if err != nil {
		t.Fatalf("GET /api/v1/state: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if _, ok := body["users"]; !ok {
		t.Error("expected 'users' field in response")
	}
}

func TestListUsersEndpoint(t *testing.T) {
	srv, gs, h := setupServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	resp, err := http.Get(srv.URL + "/api/v1/users")
	if err != nil {
		t.Fatalf("GET /api/v1/users: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body []interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body) != 0 {
		t.Errorf("expected empty users list, got %d", len(body))
	}

	// Create a user and check again
	now := time.Now()
	h.Repo.CreateUser(&order.User{
		Name: "test_user", CreatedAt: now, UpdatedAt: now,
	})
	gs.Shutdown() // Wait for async writes

	resp, err = http.Get(srv.URL + "/api/v1/users")
	if err != nil {
		t.Fatalf("GET /api/v1/users (2nd): %v", err)
	}
	defer resp.Body.Close()

	json.NewDecoder(resp.Body).Decode(&body)
	if len(body) != 1 {
		t.Errorf("expected 1 user, got %d", len(body))
	}
}

func TestPostSignalEndpoint_MissingStrategy(t *testing.T) {
	srv, gs, _ := setupServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	body, err := json.Marshal(SignalRequest{
		UserID: 1, UserStrategyID: 999, Symbol: "BTC",
		Exchange: "mock", Cash: 100, TriggerPrice: 50000,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(srv.URL+"/api/v1/signals", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/signals: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for missing strategy, got %d", resp.StatusCode)
	}
}

func TestPostSignalInvalidJSON(t *testing.T) {
	srv, gs, _ := setupServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	resp, err := http.Post(srv.URL+"/api/v1/signals", "application/json",
		bytes.NewReader([]byte("invalid json")))
	if err != nil {
		t.Fatalf("POST /api/v1/signals: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for invalid JSON, got %d", resp.StatusCode)
	}
}

func TestPostSignal_CreateOrder(t *testing.T) {
	srv, gs, h := setupServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	now := time.Now()
	userID := h.Repo.CreateUser(&order.User{
		Name: "test_user", CreatedAt: now, UpdatedAt: now,
	})
	usID := h.Repo.CreateUserStrategy(&order.UserStrategy{
		UserID: userID, Name: "ICT_1H", Exchange: "binance",
		Cash: 1000, Parts: 5, Status: 1,
		RiskStrategyType: order.RiskStrategyTypeCtaIntraday,
		CreatedAt:        now, UpdatedAt: now,
	})

	sigReq := SignalRequest{
		UserID: userID, UserStrategyID: usID, Symbol: "BTC",
		PosType: 2, Exchange: "mock", Cash: 100, TriggerPrice: 50000,
		Slippage: 0.01, Side: 0, OrderType: 0, Leverage: 10,
	}

	bodyBytes, err := json.Marshal(sigReq)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(srv.URL+"/api/v1/signals", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /api/v1/signals: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errMsg map[string]string
		json.NewDecoder(resp.Body).Decode(&errMsg)
		t.Errorf("expected 200, got %d: %v", resp.StatusCode, errMsg)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	if result["message"] != "success" {
		t.Errorf("expected 'success', got '%s'", result["message"])
	}
}
