package persistence

import (
	"sync/atomic"
	"testing"
	"time"

	"trading-service/internal/order"
)

// TestCompactStressHighConcurrency tests with higher concurrency
// to ensure no corruption under extreme load.
func TestCompactStressHighConcurrency(t *testing.T) {
	const iterations = 50
	const numWritesPerIter = 50 // Increased from 20

	for iter := 0; iter < iterations; iter++ {
		dir := setupTempDir(t)

		gs, err := NewGlobalState(dir)
		if err != nil {
			t.Fatal(err)
		}
		repo := NewStateRepository(gs)

		now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		// Start compact goroutine
		compactDone := make(chan error, 1)
		go func() {
			time.Sleep(5 * time.Microsecond) // Tighter race window
			compactDone <- gs.CompactAll()
		}()

		// Start write goroutines with higher concurrency
		var writeStarted int32
		for i := 0; i < numWritesPerIter; i++ {
			go func(idx int) {
				atomic.AddInt32(&writeStarted, 1)
				user := &order.User{
					ID:        uint64(idx + 1),
					Name:      "stress_test_user",
					Exchange:  "test",
					CreatedAt: now,
					UpdatedAt: now,
				}
				repo.CreateUser(user)
			}(i)
		}

		// Wait for compact
		if err := <-compactDone; err != nil {
			t.Errorf("iter %d: CompactAll error: %v", iter, err)
		}

		// Wait for all writes
		gs.writeWg.Wait()
		gs.Shutdown()

		// Verify data integrity
		records, err := gs.persister.ReadAllCSV("users.csv")
		if err != nil {
			t.Fatalf("iter %d: CSV read error (CORRUPTION): %v", iter, err)
		}

		// Verify no corrupted fields
		for i, rec := range records {
			if rec["name"] != "stress_test_user" {
				t.Errorf("iter %d: record %d corrupted name: %s", iter, i, rec["name"])
			}
			if rec["exchange"] != "test" {
				t.Errorf("iter %d: record %d corrupted exchange: %s", iter, i, rec["exchange"])
			}
			if rec["id"] == "" {
				t.Errorf("iter %d: record %d has empty id", iter, i)
			}
		}
	}
}
