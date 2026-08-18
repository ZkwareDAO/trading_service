package rpc

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"trading-service/internal/order"
)

// TestHandleGetOrCreateStrategy_ConcurrentCreation tests concurrent strategy creation
func TestHandleGetOrCreateStrategy_ConcurrentCreation(t *testing.T) {
	s, repo, gs := setupRPCServer(t)
	defer gs.Shutdown()

	const numGoroutines = 10
	var wg sync.WaitGroup
	results := make([]GetOrCreateStrategyResponse, numGoroutines)

	// Launch multiple goroutines to create the same strategy concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			body, _ := json.Marshal(GetOrCreateStrategyRequest{
				Name:         "SYNC_BTC-concurrent",
				StrategyType: "MANUAL_SYNC",
			})
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/rpc/v1/strategy/get-or-create", bytes.NewReader(body))
			s.Handle().ServeHTTP(w, r)

			if w.Code != 200 {
				t.Errorf("goroutine %d: expected 200, got %d", idx, w.Code)
				return
			}

			var resp GetOrCreateStrategyResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Errorf("goroutine %d: failed to decode response: %v", idx, err)
				return
			}
			results[idx] = resp
		}(i)
	}

	wg.Wait()

	// Verify only one strategy was created
	strategies := repo.ListStrategies()
	if len(strategies) != 1 {
		t.Errorf("expected 1 strategy, got %d", len(strategies))
	}

	// All responses should have the same strategy ID
	expectedID := results[0].StrategyID
	for i, resp := range results {
		if resp.StrategyID != expectedID {
			t.Errorf("goroutine %d: expected strategy_id %d, got %d", i, expectedID, resp.StrategyID)
		}
	}

	// Count how many reported "created=true"
	createdCount := 0
	for _, resp := range results {
		if resp.Created {
			createdCount++
		}
	}

	// With proper locking, exactly one should report created=true
	if createdCount != 1 {
		t.Errorf("expected exactly 1 goroutine to report created=true, got %d", createdCount)
	}
}

// TestHandleGetOrCreateUserStrategy_ConcurrentCreation tests concurrent user strategy creation
func TestHandleGetOrCreateUserStrategy_ConcurrentCreation(t *testing.T) {
	s, repo, gs := setupRPCServer(t)
	defer gs.Shutdown()

	// Create a strategy first
	strategyID := repo.CreateStrategy(&order.Strategy{
		Name:         "SYNC_BTC-concurrent",
		StrategyType: "MANUAL_SYNC",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})

	const numGoroutines = 10
	var wg sync.WaitGroup
	results := make([]GetOrCreateUserStrategyResponse, numGoroutines)

	// Launch multiple goroutines to create the same user strategy concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			body, _ := json.Marshal(GetOrCreateUserStrategyRequest{
				UserID:           1,
				Name:             "SYNC_BTC-concurrent",
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
				t.Errorf("goroutine %d: expected 200, got %d", idx, w.Code)
				return
			}

			var resp GetOrCreateUserStrategyResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Errorf("goroutine %d: failed to decode response: %v", idx, err)
				return
			}
			results[idx] = resp
		}(i)
	}

	wg.Wait()

	// Verify only one user strategy was created
	userStrategies := repo.ListUserStrategies()
	count := 0
	for _, us := range userStrategies {
		if us.Name == "SYNC_BTC-concurrent" && us.UserID == 1 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 user strategy, got %d", count)
	}

	// All responses should have the same user strategy ID
	expectedID := results[0].UserStrategyID
	for i, resp := range results {
		if resp.UserStrategyID != expectedID {
			t.Errorf("goroutine %d: expected user_strategy_id %d, got %d", i, expectedID, resp.UserStrategyID)
		}
	}

	// Count how many reported "created=true"
	createdCount := 0
	for _, resp := range results {
		if resp.Created {
			createdCount++
		}
	}

	// With proper locking, exactly one should report created=true
	if createdCount != 1 {
		t.Errorf("expected exactly 1 goroutine to report created=true, got %d", createdCount)
	}
}