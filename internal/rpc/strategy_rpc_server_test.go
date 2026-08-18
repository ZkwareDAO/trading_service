package rpc

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"trading-service/internal/order"
)

// TestHandleGetOrCreateStrategy_New tests creating a new strategy via RPC server
func TestHandleGetOrCreateStrategy_New(t *testing.T) {
	s, repo, gs := setupRPCServer(t)
	defer gs.Shutdown()

	// Initially, no strategies exist
	if len(repo.ListStrategies()) != 0 {
		t.Fatalf("expected no strategies initially, got %d", len(repo.ListStrategies()))
	}

	// Create a new strategy via RPC
	body, _ := json.Marshal(GetOrCreateStrategyRequest{
		Name:         "SYNC_BTC-test",
		StrategyType: "MANUAL_SYNC",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/rpc/v1/strategy/get-or-create", bytes.NewReader(body))
	s.Handle().ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}

	var resp GetOrCreateStrategyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if !resp.Created {
		t.Error("expected created=true for new strategy")
	}
	if resp.Name != "SYNC_BTC-test" {
		t.Errorf("expected name SYNC_BTC-test, got %s", resp.Name)
	}
	if resp.StrategyType != "MANUAL_SYNC" {
		t.Errorf("expected strategy_type MANUAL_SYNC, got %s", resp.StrategyType)
	}

	// Verify strategy was persisted
	strategies := repo.ListStrategies()
	if len(strategies) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(strategies))
	}
}

// TestHandleGetOrCreateStrategy_Existing tests querying an existing strategy
func TestHandleGetOrCreateStrategy_Existing(t *testing.T) {
	s, repo, gs := setupRPCServer(t)
	defer gs.Shutdown()

	// Create a strategy directly
	strategyID := repo.CreateStrategy(&order.Strategy{
		Name:         "SYNC_BTC-test",
		StrategyType: "MANUAL_SYNC",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})

	// Query the same strategy via RPC
	body, _ := json.Marshal(GetOrCreateStrategyRequest{
		Name:         "SYNC_BTC-test",
		StrategyType: "MANUAL_SYNC",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/rpc/v1/strategy/get-or-create", bytes.NewReader(body))
	s.Handle().ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}

	var resp GetOrCreateStrategyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Created {
		t.Error("expected created=false for existing strategy")
	}
	if resp.StrategyID != strategyID {
		t.Errorf("expected strategy_id %d, got %d", strategyID, resp.StrategyID)
	}

	// Verify no new strategy was created
	strategies := repo.ListStrategies()
	if len(strategies) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(strategies))
	}
}

// TestHandleGetOrCreateStrategyAsset tests creating a strategy asset
func TestHandleGetOrCreateStrategyAsset(t *testing.T) {
	s, repo, gs := setupRPCServer(t)
	defer gs.Shutdown()

	// Create a strategy first
	strategyID := repo.CreateStrategy(&order.Strategy{
		Name:         "SYNC_BTC-test",
		StrategyType: "MANUAL_SYNC",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})

	// Create strategy asset via RPC
	body, _ := json.Marshal(GetOrCreateStrategyAssetRequest{
		Name:       "SYNC_BTC-test",
		Asset:      "BTC",
		StrategyID: strategyID,
		PosType:    int(order.PosTypeOptions),
		Sort:       1,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/rpc/v1/strategy-asset/get-or-create", bytes.NewReader(body))
	s.Handle().ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}

	var resp GetOrCreateStrategyAssetResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if !resp.Created {
		t.Error("expected created=true for new strategy asset")
	}
}

// TestHandleGetOrCreateUserStrategy tests creating a user strategy
func TestHandleGetOrCreateUserStrategy(t *testing.T) {
	s, repo, gs := setupRPCServer(t)
	defer gs.Shutdown()

	// Create a strategy first
	strategyID := repo.CreateStrategy(&order.Strategy{
		Name:         "SYNC_BTC-test",
		StrategyType: "MANUAL_SYNC",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})

	// Create user strategy via RPC
	body, _ := json.Marshal(GetOrCreateUserStrategyRequest{
		UserID:           1,
		Name:             "SYNC_BTC-test",
		StrategyID:       strategyID,
		Exchange:         "deribit",
		ValidBefore:      "2030-12-31T00:00:00Z",
		Cash:             1000.0,
		Parts:            3,
		Status:           1,
		RiskStrategyType: "traditional",
		OrdersNum:        0,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/rpc/v1/user-strategy/get-or-create", bytes.NewReader(body))
	s.Handle().ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}

	var resp GetOrCreateUserStrategyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if !resp.Created {
		t.Error("expected created=true for new user strategy")
	}
}