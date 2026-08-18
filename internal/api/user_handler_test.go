package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"trading-service/internal/order"
)

func TestCreateUser_Success(t *testing.T) {
	srv, gs, _ := setupUserStrategyServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	reqBody := CreateUserRequest{
		Name:        "test_user",
		Exchange:    "binance",
		APIKey:      "test_api_key",
		APISecret:   "test_api_secret",
		APIPassword: "test_password",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(srv.URL+"/api/v1/users/create", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /api/v1/users/create: %v", err)
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

	data := result["data"].(map[string]interface{})
	if data["name"] != "test_user" {
		t.Errorf("expected name 'test_user', got %v", data["name"])
	}
	if data["exchange"] != "binance" {
		t.Errorf("expected exchange 'binance', got %v", data["exchange"])
	}

	// Verify sensitive fields are not returned
	if _, ok := data["api_key"]; ok {
		t.Error("api_key should not be returned in response")
	}
	if _, ok := data["api_secret"]; ok {
		t.Error("api_secret should not be returned in response")
	}
}

func TestCreateUser_MissingRequiredFields(t *testing.T) {
	srv, gs, _ := setupUserStrategyServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	tests := []struct {
		name    string
		request CreateUserRequest
	}{
		{
			name: "Missing name",
			request: CreateUserRequest{
				Exchange:  "binance",
				APIKey:    "test_key",
				APISecret: "test_secret",
			},
		},
		{
			name: "Missing exchange",
			request: CreateUserRequest{
				Name:      "test_user",
				APIKey:    "test_key",
				APISecret: "test_secret",
			},
		},
		{
			name: "Missing api_key",
			request: CreateUserRequest{
				Name:      "test_user",
				Exchange:  "binance",
				APISecret: "test_secret",
			},
		},
		{
			name: "Missing api_secret",
			request: CreateUserRequest{
				Name:     "test_user",
				Exchange: "binance",
				APIKey:   "test_key",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tc.request)
			resp, err := http.Post(srv.URL+"/api/v1/users/create", "application/json", bytes.NewReader(bodyBytes))
			if err != nil {
				t.Fatalf("POST /api/v1/users/create: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 400 {
				t.Errorf("expected 400 for %s, got %d", tc.name, resp.StatusCode)
			}
		})
	}
}

func TestCreateUser_DuplicateUser(t *testing.T) {
	srv, gs, h := setupUserStrategyServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	// Create first user
	now := time.Now()
	h.Repo.CreateUser(&order.User{
		Name: "existing_user", Exchange: "binance",
		CreatedAt: now, UpdatedAt: now,
	})
	gs.Shutdown()

	// Try to create duplicate
	reqBody := CreateUserRequest{
		Name:      "existing_user",
		Exchange:  "binance",
		APIKey:    "test_key",
		APISecret: "test_secret",
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := http.Post(srv.URL+"/api/v1/users/create", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /api/v1/users/create: %v", err)
	}
	defer resp.Body.Close()

	// Should return error for duplicate
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for duplicate user, got %d", resp.StatusCode)
	}
}

func TestCreateUser_InvalidJSON(t *testing.T) {
	srv, gs, _ := setupUserStrategyServer(t)
	defer srv.Close()
	defer gs.Shutdown()

	resp, err := http.Post(srv.URL+"/api/v1/users/create", "application/json", bytes.NewReader([]byte("invalid json")))
	if err != nil {
		t.Fatalf("POST /api/v1/users/create: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for invalid JSON, got %d", resp.StatusCode)
	}
}
