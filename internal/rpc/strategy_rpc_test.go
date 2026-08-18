package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetOrCreateStrategyRequest_New tests creating a new strategy via RPC
func TestGetOrCreateStrategyRequest_New(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc/v1/strategy/get-or-create" {
			t.Errorf("expected path /rpc/v1/strategy/get-or-create, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"strategy_id":100,"name":"SYNC_BTC-test","strategy_type":"MANUAL_SYNC","created":true}`))
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	resp, err := client.GetOrCreateStrategy(context.Background(), GetOrCreateStrategyRequest{
		Name:         "SYNC_BTC-test",
		StrategyType: "MANUAL_SYNC",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StrategyID != 100 {
		t.Errorf("expected strategy_id 100, got %d", resp.StrategyID)
	}
	if resp.Name != "SYNC_BTC-test" {
		t.Errorf("expected name SYNC_BTC-test, got %s", resp.Name)
	}
	if resp.StrategyType != "MANUAL_SYNC" {
		t.Errorf("expected strategy_type MANUAL_SYNC, got %s", resp.StrategyType)
	}
	if !resp.Created {
		t.Error("expected created to be true")
	}
}

// TestGetOrCreateStrategyRequest_Existing tests querying an existing strategy
func TestGetOrCreateStrategyRequest_Existing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"strategy_id":100,"name":"SYNC_BTC-test","strategy_type":"MANUAL_SYNC","created":false}`))
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	resp, err := client.GetOrCreateStrategy(context.Background(), GetOrCreateStrategyRequest{
		Name:         "SYNC_BTC-test",
		StrategyType: "MANUAL_SYNC",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Created {
		t.Error("expected created to be false for existing strategy")
	}
}

// TestGetOrCreateStrategyAssetRequest tests creating a strategy asset
func TestGetOrCreateStrategyAssetRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc/v1/strategy-asset/get-or-create" {
			t.Errorf("expected path /rpc/v1/strategy-asset/get-or-create, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"strategy_asset_id":200,"created":true}`))
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	resp, err := client.GetOrCreateStrategyAsset(context.Background(), GetOrCreateStrategyAssetRequest{
		Name:       "SYNC_BTC-test",
		Asset:      "BTC",
		StrategyID: 100,
		PosType:    3,
		Sort:       1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StrategyAssetID != 200 {
		t.Errorf("expected strategy_asset_id 200, got %d", resp.StrategyAssetID)
	}
}

// TestGetOrCreateUserStrategyRequest tests creating a user strategy
func TestGetOrCreateUserStrategyRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc/v1/user-strategy/get-or-create" {
			t.Errorf("expected path /rpc/v1/user-strategy/get-or-create, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"user_strategy_id":300,"created":true}`))
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	resp, err := client.GetOrCreateUserStrategy(context.Background(), GetOrCreateUserStrategyRequest{
		UserID:           1,
		Name:             "SYNC_BTC-test",
		StrategyID:       100,
		Exchange:         "deribit",
		ValidBefore:      "2030-12-31T00:00:00Z",
		Cash:             1000.0,
		Parts:            3,
		Status:           1,
		RiskStrategyType: "traditional",
		OrdersNum:        0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.UserStrategyID != 300 {
		t.Errorf("expected user_strategy_id 300, got %d", resp.UserStrategyID)
	}
}