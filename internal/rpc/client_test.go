package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_QueryOrderPositionMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc/v1/order/position-metadata" {
			t.Errorf("expected metadata path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"user_order_id":123,"user_strategy_id":9,"leverage":5,"fallback_price":1.971}`))
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	resp, err := client.QueryOrderPositionMetadata(context.Background(), 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.UserStrategyID != 9 || resp.Leverage != 5 || resp.FallbackPrice != 1.971 {
		t.Fatalf("unexpected metadata: %+v", resp)
	}
}

func TestClient_UpdateUserOrderStatusFILLED(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/rpc/v1/order/status/update" {
			t.Errorf("expected path /rpc/v1/order/status/update, got %s", r.URL.Path)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	err := client.UpdateUserOrderStatusFILLED(context.Background(), 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected server to be called")
	}
}

func TestClient_UpdateUserOrderStatusFailed(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	err := client.UpdateUserOrderStatusFailed(context.Background(), 456)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected server to be called")
	}
}

func TestClient_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	err := client.UpdateUserOrderStatusFILLED(context.Background(), 123)
	if err == nil {
		t.Error("expected error for 500 response")
	}
}
