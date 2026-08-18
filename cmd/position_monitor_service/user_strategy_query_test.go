package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk/config"
)

// TestNewUserStrategyQuery tests that a newly created user_strategy
// can be immediately queried without restarting the service.
func TestNewUserStrategyQuery(t *testing.T) {
	// Setup test data directory
	dataDir := t.TempDir()

	// Create GlobalState
	gs, err := persistence.NewGlobalState(dataDir)
	if err != nil {
		t.Fatalf("Failed to create global state: %v", err)
	}

	// Create StateRepository
	repo := persistence.NewStateRepository(gs)

	// Create test user
	user := &order.User{
		Name:     "test_user",
		Exchange: "hyperliquid",
	}
	user.ID = repo.CreateUser(user)

	// Create test strategy
	strategy := &order.Strategy{
		Name:         "POSITIVE_1D_1_NEARUSDC",
		StrategyType: "positive",
	}
	strategy.ID = repo.CreateStrategy(strategy)

	// Create user_strategy
	userStrategy := &order.UserStrategy{
		UserID:     user.ID,
		StrategyID: strategy.ID,
		Name:       "POSITIVE_1D_1_NEARUSDC",
		Exchange:   "hyperliquid",
		Cash:       100,
		Parts:      3,
		Status:     1,
	}
	userStrategy.ID = repo.CreateUserStrategy(userStrategy)

	t.Logf("Created user_strategy: id=%d, user_id=%d, strategy_id=%d, name=%s",
		userStrategy.ID, user.ID, strategy.ID, userStrategy.Name)

	// Setup API handler
	ruleStore, err := config.NewRuleStore(dataDir)
	if err != nil {
		t.Fatalf("Failed to create rule store: %v", err)
	}
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	// Create test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Query the newly created user_strategy immediately
	url := fmt.Sprintf("%s/api/v1/user-order-positions?user_id=%d&strategy_name=POSITIVE_1D_1_NEARUSDC",
		server.URL, user.ID)

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	t.Logf("Response: %v", result)

	// Check if query succeeded (should not return "Strategy not found")
	if code, ok := result["code"].(float64); ok && code == 1001 {
		if msg, ok := result["message"].(string); ok && msg == "Strategy not found" {
			t.Errorf("BUG: Newly created user_strategy not found immediately after creation")
			t.Errorf("This indicates the bug is NOT fixed - service restart would be needed")
		}
	}

	// Verify the user_strategy exists in memory
	strategies := repo.ListStrategies()
	t.Logf("Total strategies in memory: %d", len(strategies))

	userStrategies := repo.ListUserStrategies()
	t.Logf("Total user_strategies in memory: %d", len(userStrategies))

	for _, us := range userStrategies {
		t.Logf("user_strategy: id=%d, user_id=%d, strategy_id=%d, name=%s",
			us.ID, us.UserID, us.StrategyID, us.Name)
	}
}
