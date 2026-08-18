package persistence

import (
	"testing"
	"time"

	"trading-service/internal/order"
)

// TestCompactRaceReproduction specifically tries to reproduce the race
// where Compact and concurrent AppendRow cause data corruption.
//
// This test forces the race by:
// 1. Creating many users with delayed writes
// 2. Running CompactAll concurrently
// 3. Checking for corrupted data
func TestCompactRaceReproduction(t *testing.T) {
	const iterations = 100

	for iter := 0; iter < iterations; iter++ {
		// Use a fresh directory for each iteration to avoid cross-contamination
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
			// Small delay to let writes start
			time.Sleep(10 * time.Microsecond)
			compactDone <- gs.CompactAll()
		}()

		// Start write goroutines
		const numWrites = 20
		for i := 0; i < numWrites; i++ {
			go func(idx int) {
				user := &order.User{
					ID:        uint64(idx + 1),
					Name:      "race_test_user",
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

		// Verify data
		records, err := gs.persister.ReadAllCSV("users.csv")
		if err != nil {
			t.Errorf("iter %d: CSV read error (CORRUPTION): %v", iter, err)
			continue
		}

		// All records should have valid fields
		for i, rec := range records {
			if rec["name"] != "race_test_user" {
				t.Errorf("iter %d: record %d corrupted name: %s", iter, i, rec["name"])
			}
			if rec["exchange"] != "test" {
				t.Errorf("iter %d: record %d corrupted exchange: %s", iter, i, rec["exchange"])
			}
		}
	}
}
