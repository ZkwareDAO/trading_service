package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

// TestUserOrderPositions_QueryByUserIDOnly tests Priority 1.5:
// Query positions by user_id only (without strategy_name)
func TestUserOrderPositions_QueryByUserIDOnly(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Create two users to ensure filtering works
	now := time.Now()
	user1ID := repo.CreateUser(&order.User{
		Name:      "user1",
		Exchange:  "binance",
		CreatedAt: now,
		UpdatedAt: now,
	})

	user2ID := repo.CreateUser(&order.User{
		Name:      "user2",
		Exchange:  "binance",
		CreatedAt: now,
		UpdatedAt: now,
	})

	// Create test strategies for each user
	us1ID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:    user1ID,
		Name:      "STRAT1",
		Exchange:  "binance",
		Cash:      1000,
		Parts:     5,
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	})

	us2ID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:    user2ID,
		Name:      "STRAT2",
		Exchange:  "binance",
		Cash:      2000,
		Parts:     3,
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	})

	// Create test positions for user1
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         user1ID,
		UserStrategyID: us1ID,
		Exchange:       "binance",
		Asset:          "BTCUSDT",
		Side:           order.SideLong,
		Quantity:       0.1,
		Deleted:        0, // active
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         user1ID,
		UserStrategyID: us1ID,
		Exchange:       "binance",
		Asset:          "ETHUSDT",
		Side:           order.SideShort,
		Quantity:       1.0,
		Deleted:        0, // active
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Create test position for user2 (should NOT appear in user1's results)
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         user2ID,
		UserStrategyID: us2ID,
		Exchange:       "binance",
		Asset:          "SOLUSDT",
		Side:           order.SideLong,
		Quantity:       5.0,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	// Setup handler
	handler := NewPositionAPIHandler(repo, nil, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/user-order-positions", handler)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test: Query by user1_id only - should get only user1's positions
	req, err := http.NewRequest("GET", server.URL+"/api/v1/user-order-positions?user_id="+strconv.FormatUint(user1ID, 10), nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}

	// Verify we got ONLY user1's positions (2 positions, not 3)
	data := result["data"].(map[string]interface{})
	list := data["list"].([]interface{})

	if len(list) != 2 {
		t.Errorf("Expected 2 positions for user_id=%d (user2 has 1 position that should NOT appear), got %d", user1ID, len(list))
	}

	// Verify total count
	total := int(data["total"].(float64))
	if total != 2 {
		t.Errorf("Expected total=2, got %d", total)
	}
}

// TestUserOrderPositions_UserIDWithFilters tests user_id combined with other filters
func TestUserOrderPositions_UserIDWithFilters(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Create test user
	now := time.Now()
	userID := repo.CreateUser(&order.User{
		Name:      "test_user",
		Exchange:  "binance",
		CreatedAt: now,
		UpdatedAt: now,
	})

	// Create strategy
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:    userID,
		Name:      "TEST_STRAT",
		Exchange:  "binance",
		Cash:      1000,
		Parts:     5,
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	})

	// Create positions with different sides
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: usID,
		Exchange:       "binance",
		Asset:          "BTCUSDT",
		Side:           order.SideLong,
		Quantity:       0.1,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: usID,
		Exchange:       "binance",
		Asset:          "ETHUSDT",
		Side:           order.SideShort,
		Quantity:       1.0,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	handler := NewPositionAPIHandler(repo, nil, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/user-order-positions", handler)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test: user_id + side filter
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/user-order-positions?user_id="+strconv.FormatUint(userID, 10)+"&side=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	data := result["data"].(map[string]interface{})
	list := data["list"].([]interface{})

	// Should only get Long positions
	if len(list) != 1 {
		t.Errorf("Expected 1 Long position, got %d", len(list))
	}
}

// TestUserPositions_QueryByUserIDOnly tests user_positions endpoint with user_id only
func TestUserPositions_QueryByUserIDOnly(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Create user
	now := time.Now()
	userID := repo.CreateUser(&order.User{
		Name:      "test_user",
		Exchange:  "binance",
		CreatedAt: now,
		UpdatedAt: now,
	})

	// Create strategy
	usID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:    userID,
		Name:      "TEST_STRAT",
		Exchange:  "binance",
		Cash:      1000,
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	})

	// Create user_position
	repo.CreateUserPosition(&order.UserPosition{
		UserID:         userID,
		UserStrategyID: usID,
		Exchange:       "binance",
		PosType:        order.PosTypeFutures,
		Quantity:       0.1,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	handler := NewPositionAPIHandler(repo, nil, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/user-positions", handler)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test: Query by user_id only
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/user-positions?user_id="+strconv.FormatUint(userID, 10), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	data := result["data"].(map[string]interface{})
	list := data["list"].([]interface{})

	if len(list) != 1 {
		t.Errorf("Expected 1 user_position for user_id=%d, got %d", userID, len(list))
	}
}

// TestUserIDPriority tests that user_strategy_id takes priority over user_id
func TestUserIDPriority(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Create two users
	now := time.Now()
	user1ID := repo.CreateUser(&order.User{Name: "user1", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	user2ID := repo.CreateUser(&order.User{Name: "user2", Exchange: "binance", CreatedAt: now, UpdatedAt: now})

	// Create strategies
	us1ID := repo.CreateUserStrategy(&order.UserStrategy{UserID: user1ID, Name: "STRAT1", Exchange: "binance", Cash: 1000, Status: 1, CreatedAt: now, UpdatedAt: now})
	us2ID := repo.CreateUserStrategy(&order.UserStrategy{UserID: user2ID, Name: "STRAT2", Exchange: "binance", Cash: 1000, Status: 1, CreatedAt: now, UpdatedAt: now})

	// Create positions for both
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: user1ID, UserStrategyID: us1ID, Exchange: "binance", Asset: "BTCUSDT",
		Side: order.SideLong, Quantity: 0.1, Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})

	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID: user2ID, UserStrategyID: us2ID, Exchange: "binance", Asset: "ETHUSDT",
		Side: order.SideLong, Quantity: 1.0, Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})

	handler := NewPositionAPIHandler(repo, nil, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/user-order-positions", handler)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test: Both user_strategy_id and user_id provided - user_strategy_id should win
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/user-order-positions?user_strategy_id="+strconv.FormatUint(us1ID, 10)+"&user_id="+strconv.FormatUint(user2ID, 10), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	data := result["data"].(map[string]interface{})
	list := data["list"].([]interface{})

	// Should get user1's position (from user_strategy_id), not user2's
	if len(list) != 1 {
		t.Errorf("Expected 1 position (from user_strategy_id), got %d", len(list))
	}
}
