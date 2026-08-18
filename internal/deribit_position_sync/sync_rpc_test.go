package deribit_position_sync

import (
	"net/http/httptest"
	"testing"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

// TestCreatePosition_UsesRPC verifies that createPosition uses RPC client for strategy creation
func TestCreatePosition_UsesRPC(t *testing.T) {
	// Setup RPC server
	_, repo, gs, testServer := setupRPCTestServer(t)
	defer gs.Shutdown()
	defer testServer.Close()

	// Create test user
	repo.CreateUser(&order.User{
		ID:          1,
		Exchange:    "deribit",
		APIKey:      "test-key",
		APISecret:   "test-secret",
		APIPassword: "test-password",
	})

	// Create RPC client pointing to test server
	client := rpc.NewOrderServiceClient(testServer.URL)

	// Test position creation via RPC
	pos := PositionToCreate{
		Symbol:     "BTC-PERPETUAL",
		Side:       order.SideLong,
		Quantity:   0.5,
		EntryPrice: 50000.0,
	}

	// Call createPosition with RPC client
	err := createPositionWithRPC(repo, client, 1, pos)
	if err != nil {
		t.Fatalf("createPositionWithRPC failed: %v", err)
	}

	// Verify strategy was created via RPC
	strategies := repo.ListStrategies()
	if len(strategies) != 1 {
		t.Errorf("expected 1 strategy, got %d", len(strategies))
	}
	if strategies[0].Name != "SYNC_BTC-PERPETUAL" {
		t.Errorf("expected strategy name SYNC_BTC-PERPETUAL, got %s", strategies[0].Name)
	}

	// Verify user_strategy was created via RPC
	userStrategies := repo.ListUserStrategies()
	if len(userStrategies) != 1 {
		t.Errorf("expected 1 user_strategy, got %d", len(userStrategies))
	}

	// Verify user_order_position was created locally
	active := true
	positions := repo.ListUserOrderPositionsByFilter(persistence.UserOrderPositionFilter{
		UserID:   1,
		Active:   &active,
		Exchange: "deribit",
	})
	if len(positions) != 1 {
		t.Errorf("expected 1 user_order_position, got %d", len(positions))
	}
}

// setupRPCTestServer creates a test RPC server with real repository
func setupRPCTestServer(t *testing.T) (*rpc.Server, *persistence.StateRepository, *persistence.GlobalState, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	server := rpc.NewServer(repo)
	testServer := httptest.NewServer(server.Handle())
	return server, repo, gs, testServer
}
