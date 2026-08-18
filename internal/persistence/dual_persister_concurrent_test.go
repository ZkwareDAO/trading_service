package persistence

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"trading-service/internal/order"
)

// TestConcurrentAppendRowTwoGoroutinesSameFile reproduces the bug:
// Two goroutines writing to the same file concurrently can corrupt data
// because os.O_APPEND with multiple file handles doesn't guarantee atomicity.
func TestConcurrentAppendRowTwoGoroutinesSameFile(t *testing.T) {
	dir := setupTempDir(t)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	const iterations = 100

	for iter := 0; iter < iterations; iter++ {
		// Each iteration: fresh file
		filePath := filepath.Join(dir, "users.csv")
		os.Remove(filePath)

		p := NewDualPersister(dir)

		var wg sync.WaitGroup
		wg.Add(2)

		var err1, err2 error

		// Goroutine 1: write user 1
		go func() {
			defer wg.Done()
			user := &order.User{
				ID:        1,
				Name:      "user_one_with_long_name_to_hit_buffer_boundary",
				Exchange:  "binance",
				CreatedAt: now,
				UpdatedAt: now,
			}
			err1 = p.AppendRow("users.csv", user)
		}()

		// Goroutine 2: write user 2
		go func() {
			defer wg.Done()
			user := &order.User{
				ID:        2,
				Name:      "user_two_with_long_name_to_hit_buffer_boundary",
				Exchange:  "binance",
				CreatedAt: now,
				UpdatedAt: now,
			}
			err2 = p.AppendRow("users.csv", user)
		}()

		wg.Wait()

		if err1 != nil {
			t.Errorf("iter %d: goroutine 1 error: %v", iter, err1)
		}
		if err2 != nil {
			t.Errorf("iter %d: goroutine 2 error: %v", iter, err2)
		}

		// Read and verify no corruption
		file, err := os.Open(filePath)
		if err != nil {
			t.Fatalf("iter %d: open failed: %v", iter, err)
		}

		reader := csv.NewReader(file)
		records, err := reader.ReadAll()
		file.Close()

		if err != nil {
			// This is the corruption bug: CSV read fails
			t.Errorf("iter %d: CSV read error (corruption): %v", iter, err)
			continue
		}

		// Should have header + 2 data rows = 3 rows
		if len(records) != 3 {
			t.Errorf("iter %d: expected 3 rows, got %d", iter, len(records))
		}
	}
}

// TestConcurrentAppendRowSameFileHighConcurrency tests with many goroutines.
func TestConcurrentAppendRowSameFileHighConcurrency(t *testing.T) {
	dir := setupTempDir(t)
	p := NewDualPersister(dir)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	const numGoroutines = 20
	const recordsEach = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < recordsEach; i++ {
				user := &order.User{
					ID:        uint64(gid*recordsEach + i + 1),
					Name:      "concurrent_user_test",
					Exchange:  "test",
					CreatedAt: now,
					UpdatedAt: now,
				}
				if err := p.AppendRow("users.csv", user); err != nil {
					t.Errorf("AppendRow error: %v", err)
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify no corruption
	records, err := p.ReadAllCSV("users.csv")
	if err != nil {
		t.Fatalf("ReadAllCSV error (corruption): %v", err)
	}

	expectedCount := numGoroutines * recordsEach
	if len(records) != expectedCount {
		t.Errorf("expected %d records, got %d", expectedCount, len(records))
	}

	// Check for data corruption
	for i, rec := range records {
		if rec["name"] != "concurrent_user_test" {
			t.Errorf("record %d: name corrupted: %s", i, rec["name"])
		}
	}
}

// TestAppendRowPerformance verifies serialization performance.
func TestAppendRowPerformance(t *testing.T) {
	dir := setupTempDir(t)
	p := NewDualPersister(dir)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	const numOps = 1000
	start := time.Now()

	for i := 0; i < numOps; i++ {
		user := &order.User{
			ID:        uint64(i + 1),
			Name:      "perf_test",
			Exchange:  "binance",
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := p.AppendRow("users.csv", user); err != nil {
			t.Fatalf("AppendRow failed: %v", err)
		}
	}

	elapsed := time.Since(start)
	avgMs := float64(elapsed.Milliseconds()) / numOps

	t.Logf("AppendRow: %d ops in %v (%.2f ms/op)", numOps, elapsed, avgMs)

	if elapsed > 10*time.Second {
		t.Errorf("too slow: %v for %d ops", elapsed, numOps)
	}
}
