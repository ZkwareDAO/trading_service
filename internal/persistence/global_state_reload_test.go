package persistence

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"trading-service/internal/order"
)

func TestStateRepository_ReloadUserStrategies(t *testing.T) {
	// Setup: create temp directory with initial CSV
	dir := t.TempDir()
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState: %v", err)
	}
	defer gs.Shutdown()

	repo := NewStateRepository(gs)
	now := time.Now()

	// Create initial strategy
	strategy1 := &order.UserStrategy{
		UserID:    1,
		Name:      "INITIAL_STRATEGY",
		Exchange:  "binance",
		Cash:      1000.0,
		Parts:     3,
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	repo.CreateUserStrategy(strategy1)

	// Verify initial state
	strategies := repo.ListUserStrategies()
	if len(strategies) != 1 {
		t.Fatalf("expected 1 strategy initially, got %d", len(strategies))
	}
	if strategies[0].Name != "INITIAL_STRATEGY" {
		t.Fatalf("expected name INITIAL_STRATEGY, got %s", strategies[0].Name)
	}

	// Simulate external modification: add new strategy directly to CSV
	// Wait for CSV to be written by CreateUserStrategy
	time.Sleep(100 * time.Millisecond)
	csvPath := filepath.Join(dir, "user_strategies.csv")

	// Read existing content and append new line
	existing, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	newLine := "2,1,NEW_STRATEGY_EXTERNAL,binance,2030-12-31T00:00:00Z,500,5,1,0,traditional,0,2026-07-02T00:00:00Z,2026-07-02T00:00:00Z\n"
	if err := os.WriteFile(csvPath, append(existing, []byte(newLine)...), 0644); err != nil {
		t.Fatalf("write CSV: %v", err)
	}

	// Before reload: memory still has old state
	strategiesBefore := repo.ListUserStrategies()
	if len(strategiesBefore) != 1 {
		t.Fatalf("expected 1 strategy before reload, got %d", len(strategiesBefore))
	}

	// Execute: reload strategies from CSV
	if err := repo.ReloadUserStrategies(); err != nil {
		t.Fatalf("ReloadUserStrategies: %v", err)
	}

	// Verify: memory now has both strategies
	strategiesAfter := repo.ListUserStrategies()
	if len(strategiesAfter) != 2 {
		t.Fatalf("expected 2 strategies after reload, got %d", len(strategiesAfter))
	}

	// Check new strategy is loaded
	newStrategy, err := repo.GetUserStrategyByID(2)
	if err != nil {
		t.Fatalf("GetUserStrategyByID(2): %v", err)
	}
	if newStrategy.Name != "NEW_STRATEGY_EXTERNAL" {
		t.Fatalf("expected name NEW_STRATEGY_EXTERNAL, got %s", newStrategy.Name)
	}
	if newStrategy.UserID != 1 {
		t.Fatalf("expected user_id 1, got %d", newStrategy.UserID)
	}
}

func TestStateRepository_ReloadUserStrategies_PreservesExisting(t *testing.T) {
	// Setup
	dir := t.TempDir()
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState: %v", err)
	}
	defer gs.Shutdown()

	repo := NewStateRepository(gs)
	now := time.Now()

	// Create user and strategy via API
	repo.CreateUser(&order.User{ID: 1, Name: "test_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	strategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:    1,
		Name:      "EXISTING_STRATEGY",
		Exchange:  "binance",
		Cash:      1000.0,
		CreatedAt: now,
		UpdatedAt: now,
	})

	// Wait for async write to complete
	time.Sleep(100 * time.Millisecond)

	// Reload should not lose existing strategies
	if err := repo.ReloadUserStrategies(); err != nil {
		t.Fatalf("ReloadUserStrategies: %v", err)
	}

	// Verify existing strategy still exists
	strategy, err := repo.GetUserStrategyByID(strategyID)
	if err != nil {
		t.Fatalf("GetUserStrategyByID: %v", err)
	}
	if strategy.Name != "EXISTING_STRATEGY" {
		t.Fatalf("expected name EXISTING_STRATEGY, got %s", strategy.Name)
	}
}

func TestStateRepository_ReloadUserStrategies_EmptyCSV(t *testing.T) {
	// Setup
	dir := t.TempDir()
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState: %v", err)
	}
	defer gs.Shutdown()

	repo := NewStateRepository(gs)

	// Empty CSV (just header)
	csvPath := filepath.Join(dir, "user_strategies.csv")
	header := "id,user_id,name,exchange,valid_before,cash,parts,status,strategy_id,risk_strategy_type,orders_num,created_at,updated_at\n"
	if err := os.WriteFile(csvPath, []byte(header), 0644); err != nil {
		t.Fatalf("write CSV: %v", err)
	}

	// Reload should not error
	if err := repo.ReloadUserStrategies(); err != nil {
		t.Fatalf("ReloadUserStrategies should not error on empty CSV: %v", err)
	}

	// Should have no strategies
	strategies := repo.ListUserStrategies()
	if len(strategies) != 0 {
		t.Fatalf("expected 0 strategies, got %d", len(strategies))
	}
}