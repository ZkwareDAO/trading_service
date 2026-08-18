package exchange

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"trading-service/internal/persistence"
)

func setupExchangeServer(t *testing.T) (*httptest.Server, *MockExchange, *persistence.GlobalState) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	mock := NewMockExchange()
	mock.SetPrice("BTCUSDT", 50000)

	srv := NewExchangeServer(repo, mock)
	return srv, mock, gs
}

func TestExchangeHealthEndpoint(t *testing.T) {
	srv, _, gs := setupExchangeServer(t)
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
}

func TestExchangeCreateOrder(t *testing.T) {
	srv, _, gs := setupExchangeServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	reqBody := CreateOrderAPIRequest{
		UserID:       1,
		Symbol:       "BTCUSDT",
		Side:         0,
		OrderType:    0,
		Quantity:     0.1,
		Price:        50000,
		PosType:      2,
		PositionSide: "LONG",
		RelationID:   100,
		RelationType: "user_orders",
	}

	body, _ := json.Marshal(reqBody)
	resp, err := http.Post(srv.URL+"/api/v1/orders", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/orders: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestExchangeGetPrice(t *testing.T) {
	srv, mock, gs := setupExchangeServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	mock.SetPrice("BTCUSDT", 50000)

	resp, err := http.Get(srv.URL + "/api/v1/prices?symbol=BTCUSDT")
	if err != nil {
		t.Fatalf("GET /api/v1/prices: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestExchangeListPositions(t *testing.T) {
	srv, _, gs := setupExchangeServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	resp, err := http.Get(srv.URL + "/api/v1/positions")
	if err != nil {
		t.Fatalf("GET /api/v1/positions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestExchangeSetLeverage(t *testing.T) {
	srv, _, gs := setupExchangeServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	reqBody := SetLeverageRequest{
		Symbol:   "BTCUSDT",
		Leverage: 10,
	}

	body, _ := json.Marshal(reqBody)
	resp, err := http.Post(srv.URL+"/api/v1/leverage", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/leverage: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

