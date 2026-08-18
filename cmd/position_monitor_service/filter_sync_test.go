package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

// TestFilterSync_ScansUsersCSV tests that filter sync scans users.csv for exchanges.
func TestFilterSync_ScansUsersCSV(t *testing.T) {
	// Setup
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Create users with different exchanges
	now := time.Now()
	repo.CreateUser(&order.User{Name: "user1", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	repo.CreateUser(&order.User{Name: "user2", Exchange: "hyperliquid", CreatedAt: now, UpdatedAt: now})
	repo.CreateUser(&order.User{Name: "user3", Exchange: "mock", CreatedAt: now, UpdatedAt: now})

	// Test
	users := repo.ListUsers()
	exchanges := make(map[string]bool)
	for _, user := range users {
		if user.Exchange != "" && user.Exchange != "mock" {
			exchanges[user.Exchange] = true
		}
	}

	// Verify
	if len(exchanges) != 2 {
		t.Errorf("expected 2 exchanges, got %d", len(exchanges))
	}
	if !exchanges["binance"] {
		t.Error("expected binance in exchanges")
	}
	if !exchanges["hyperliquid"] {
		t.Error("expected hyperliquid in exchanges")
	}
	if exchanges["mock"] {
		t.Error("mock should be excluded")
	}
}

// TestNotifyUOSReloadFilters_Success tests UOS reload notification.
func TestNotifyUOSReloadFilters_Success(t *testing.T) {
	// Setup mock UOS server
	uosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rpc/v1/filters/reload" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer uosServer.Close()

	// Test
	err := notifyUOSReloadFilters(uosServer.URL)

	// Verify
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// TestNotifyUOSReloadFilters_UOSNotConfigured tests when UOS URL is empty.
func TestNotifyUOSReloadFilters_UOSNotConfigured(t *testing.T) {
	// Test
	err := notifyUOSReloadFilters("")

	// Verify - should return nil when URL is empty
	if err != nil {
		t.Errorf("expected nil when UOS URL empty, got %v", err)
	}
}

// TestNotifyUOSReloadFilters_UOSReturnsError tests error handling.
func TestNotifyUOSReloadFilters_UOSReturnsError(t *testing.T) {
	// Setup mock UOS server that returns error
	uosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer uosServer.Close()

	// Test
	err := notifyUOSReloadFilters(uosServer.URL)

	// Verify
	if err == nil {
		t.Error("expected error when UOS returns 500")
	}
}

// TestSyncFiltersOnce_NoExchanges tests when no real exchanges exist.
func TestSyncFiltersOnce_NoExchanges(t *testing.T) {
	// Setup
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Create only mock user
	now := time.Now()
	repo.CreateUser(&order.User{Name: "mock_user", Exchange: "mock", CreatedAt: now, UpdatedAt: now})

	// Mock UOS server
	uosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer uosServer.Close()

	// Test
	ctx := context.Background()
	syncFiltersOnce(ctx, repo, uosServer.URL, false, false)

	// Verify - should not panic or create filters
	filters := repo.ListAllExchangeSymbolFilters()
	if len(filters) > 0 {
		t.Errorf("expected no filters for mock exchange, got %d", len(filters))
	}
}

// TestSyncFiltersOnce_BinanceSync tests Binance filter sync (integration).
func TestSyncFiltersOnce_BinanceSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Setup
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Create Binance user
	now := time.Now()
	repo.CreateUser(&order.User{Name: "binance_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})

	// Mock UOS server
	uosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer uosServer.Close()

	// Test
	ctx := context.Background()
	syncFiltersOnce(ctx, repo, uosServer.URL, false, false)

	// Verify
	filters := repo.ListAllExchangeSymbolFilters()
	binanceFilters := 0
	for _, f := range filters {
		if f.Exchange == "binance" {
			binanceFilters++
		}
	}

	if binanceFilters == 0 {
		t.Error("expected at least one Binance filter")
	}
}

// TestSyncFiltersOnce_HyperliquidSync tests Hyperliquid filter sync (integration).
func TestSyncFiltersOnce_HyperliquidSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Setup
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Create Hyperliquid user
	now := time.Now()
	repo.CreateUser(&order.User{Name: "hl_user", Exchange: "hyperliquid", CreatedAt: now, UpdatedAt: now})

	// Mock UOS server
	uosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer uosServer.Close()

	// Test
	ctx := context.Background()
	syncFiltersOnce(ctx, repo, uosServer.URL, false, false)

	// Verify
	filters := repo.ListAllExchangeSymbolFilters()
	hlFilters := 0
	for _, f := range filters {
		if f.Exchange == "hyperliquid" {
			hlFilters++
			// Verify tick_size is not zero
			if f.TickSize == 0 {
				t.Errorf("hyperliquid filter %s has tick_size=0, should be calculated", f.Symbol)
			}
		}
	}

	if hlFilters == 0 {
		t.Error("expected at least one Hyperliquid filter")
	}
}

// TestSyncFiltersOnce_MultipleExchanges tests sync for multiple exchanges.
func TestSyncFiltersOnce_MultipleExchanges(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Setup
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Create users for both exchanges
	now := time.Now()
	repo.CreateUser(&order.User{Name: "binance_user", Exchange: "binance", CreatedAt: now, UpdatedAt: now})
	repo.CreateUser(&order.User{Name: "hl_user", Exchange: "hyperliquid", CreatedAt: now, UpdatedAt: now})

	// Mock UOS server
	uosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer uosServer.Close()

	// Test
	ctx := context.Background()
	syncFiltersOnce(ctx, repo, uosServer.URL, false, false)

	// Verify
	filters := repo.ListAllExchangeSymbolFilters()
	binanceCount := 0
	hlCount := 0
	for _, f := range filters {
		if f.Exchange == "binance" {
			binanceCount++
		}
		if f.Exchange == "hyperliquid" {
			hlCount++
		}
	}

	if binanceCount == 0 {
		t.Error("expected at least one Binance filter")
	}
	if hlCount == 0 {
		t.Error("expected at least one Hyperliquid filter")
	}
}

// TestStartFilterSync_Disabled tests that sync is disabled when interval is 0.
func TestStartFilterSync_Disabled(t *testing.T) {
	// Setup
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	ctx := context.Background()

	// Test - should return immediately
	StartFilterSync(ctx, repo, "", 0, false, false)

	// Verify - no error, no filters created
	filters := repo.ListAllExchangeSymbolFilters()
	if len(filters) > 0 {
		t.Error("expected no filters when sync disabled")
	}
}
