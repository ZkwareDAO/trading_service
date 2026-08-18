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
)

func TestQueryUserOrderPositionsRPC_FiltersActiveByStrategyAndSide(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: 1, UserStrategyID: 1000, Asset: "BTCUSDT", PosType: order.PosTypeFutures, Side: order.SideShort, Quantity: 0.1, Deleted: 0, CreatedAt: now, UpdatedAt: now})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: 1, UserStrategyID: 1000, Asset: "BTCUSDT", PosType: order.PosTypeFutures, Side: order.SideLong, Quantity: 0.2, Deleted: 0, CreatedAt: now, UpdatedAt: now})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: 1, UserStrategyID: 1000, Asset: "BTCUSDT", PosType: order.PosTypeFutures, Side: order.SideShort, Quantity: 0.3, Deleted: 1, CreatedAt: now, UpdatedAt: now})

	h := NewPositionQueryHandler(repo, nil)
	body := bytes.NewReader(mustMarshal(map[string]interface{}{
		"user_strategy_id": 1000,
		"side":             int(order.SideShort),
		"active":           true,
		"asset":            "BTCUSDT",
		"pos_type":         int(order.PosTypeFutures),
	}))
	req := httptest.NewRequest(http.MethodPost, "/rpc/v1/user-order-positions/query", body)
	w := httptest.NewRecorder()

	h.HandleQueryUserOrderPositions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp QueryUserOrderPositionsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("expected 1 active short BTCUSDT position, got %d", resp.Count)
	}
}

func TestQueryUserOrderPositionsRPC_RequiresUserStrategyID(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)
	h := NewPositionQueryHandler(repo, nil)

	req := httptest.NewRequest(http.MethodPost, "/rpc/v1/user-order-positions/query", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()

	h.HandleQueryUserOrderPositions(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
