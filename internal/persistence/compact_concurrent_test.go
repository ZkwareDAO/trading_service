package persistence

import (
	"sync"
	"testing"
	"time"

	"trading-service/internal/order"
)

// TestCompactConcurrentWithAppend reproduces the bug where Compact and AppendRow
// running concurrently can corrupt CSV data.
//
// BUG SCENARIO:
// 1. CompactAll() starts with RLock
// 2. AppendRow goroutine opens file for writing
// 3. Compact creates temp file, writes, calls os.Rename
// 4. AppendRow goroutine writes to old file (stale inode)
// 5. Result: compact data + append data both exist but inconsistent
func TestCompactConcurrentWithAppend(t *testing.T) {
	dir := setupTempDir(t)

	const iterations = 100

	for iter := 0; iter < iterations; iter++ {
		gs, err := NewGlobalState(dir)
		if err != nil {
			t.Fatal(err)
		}
		repo := NewStateRepository(gs)

		now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		// Create initial user
		user := &order.User{
			ID:        1,
			Name:      "initial_user",
			Exchange:  "binance",
			CreatedAt: now,
			UpdatedAt: now,
		}
		repo.CreateUser(user)

		var wg sync.WaitGroup
		wg.Add(2)

		
		

		// Goroutine 1: Run CompactAll
		go func() {
			defer wg.Done()
			_ =gs.CompactAll()
		}()

		// Goroutine 2: Concurrent AppendRow
		go func() {
			defer wg.Done()
			time.Sleep(1 * time.Microsecond) // Tiny delay to increase race window
			newUser := &order.User{
				ID:        2,
				Name:      "concurrent_user",
				Exchange:  "binance",
				CreatedAt: now,
				UpdatedAt: now,
			}
			_ =gs.persister.AppendRow("users.csv", newUser)
		}()

		wg.Wait()

		// Wait for any pending writes
		gs.Shutdown()

		// Verify data integrity
		records, err := gs.persister.ReadAllCSV("users.csv")
		if err != nil {
			t.Errorf("iter %d: ReadAllCSV error (corruption): %v", iter, err)
			gs.Shutdown()
			continue
		}

		// Should have at least 1 user (initial), possibly 2 if append succeeded
		if len(records) < 1 {
			t.Errorf("iter %d: expected at least 1 record, got %d", iter, len(records))
		}

		// Check no corruption in fields
		for i, rec := range records {
			if rec["name"] == "" {
				t.Errorf("iter %d: record %d has empty name (corruption)", iter, i)
			}
			if rec["exchange"] == "" {
				t.Errorf("iter %d: record %d has empty exchange (corruption)", iter, i)
			}
		}

		gs.Shutdown()
	}
}

// TestCompactWaitsForPendingWrites verifies that CompactAll waits for all
// pending writes before starting compact.
func TestCompactWaitsForPendingWrites(t *testing.T) {
	dir := setupTempDir(t)

	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create a user (this spawns a goroutine to write)
	user := &order.User{
		ID:        1,
		Name:      "test_user",
		Exchange:  "binance",
		CreatedAt: now,
		UpdatedAt: now,
	}
	repo.CreateUser(user)

	// Immediately run compact (before write goroutine completes)
	if err := gs.CompactAll(); err != nil {
		t.Fatalf("CompactAll error: %v", err)
	}

	// Verify: user should be in compacted file
	records, err := gs.persister.ReadAllCSV("users.csv")
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 1 {
		t.Errorf("expected 1 record after compact, got %d", len(records))
	}

	if len(records) > 0 && records[0]["name"] != "test_user" {
		t.Errorf("expected name 'test_user', got '%s'", records[0]["name"])
	}

	gs.Shutdown()
}
