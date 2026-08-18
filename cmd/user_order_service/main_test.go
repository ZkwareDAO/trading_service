package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

func TestRegisterHTTPHandlers_RegistersOrderPositionMetadataRPC(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	orderID := repo.CreateUserOrder(&order.UserOrder{
		UserID:         6,
		UserStrategyID: 12,
		PosType:        order.PosTypeFutures,
		Exchange:       "binance",
		BaseAsset:      "NEAR",
		QuoteAsset:     "USDT",
		TriggerPrice:   2.068,
		Status:         1,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	})
	repo.UpsertLeverageConfig(&order.LeverageConfig{
		UserID:    6,
		Asset:     "NEAR",
		Quote:     "USDT",
		Leverage:  5,
		Exchange:  "binance",
		Status:    1,
		PosType:   order.PosTypeFutures,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	mux := http.NewServeMux()
	registerHTTPHandlers(mux, repo, nil)

	body, err := json.Marshal(rpc.QueryOrderPositionMetadataRequest{UserOrderID: orderID})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/rpc/v1/order/position-metadata", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected metadata RPC route to return 200, got %d body=%q", w.Code, w.Body.String())
	}
	var resp rpc.QueryOrderPositionMetadataResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.UserStrategyID != 12 || resp.Leverage != 5 || resp.FallbackPrice != 2.068 {
		t.Fatalf("unexpected metadata response: %+v", resp)
	}
}

func TestRegisterHTTPHandlers_RegistersOrderStatusRPC(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	orderID := repo.CreateUserOrder(&order.UserOrder{
		UserID:         1,
		UserStrategyID: 1,
		Exchange:       "mock",
		Status:         1,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	})

	mux := http.NewServeMux()
	registerHTTPHandlers(mux, repo, nil)

	reqBody := rpc.UpdateUserOrderStatusRequest{
		UserOrderID: orderID,
		Status:      2,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/rpc/v1/order/status/update", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected RPC route to return 200, got %d body=%q", w.Code, w.Body.String())
	}

	updated, err := repo.GetUserOrderByID(orderID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != 2 {
		t.Fatalf("expected user_order status=2, got %d", updated.Status)
	}
}
