package persistence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"trading-service/internal/order"
)

func TestCompactPreservesHeaderForEmptyTables(t *testing.T) {
	// Create temp dir
	dir := t.TempDir()

	// Create persister
	p := NewDualPersister(dir)

	// Test 1: Compact with empty rows should write header
	err := p.Compact("user_order_positions.csv", []interface{}{})
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Read file content
	content, err := os.ReadFile(filepath.Join(dir, "user_order_positions.csv"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 1 {
		t.Errorf("Expected 1 line (header), got %d lines", len(lines))
	}

	if lines[0] == "" {
		t.Error("Header is empty")
	}

	// Verify header contains expected fields
	if !strings.Contains(lines[0], "id,user_id") {
		t.Errorf("Header missing expected fields: %s", lines[0])
	}

	t.Logf("Empty table header: %s", lines[0])
}

func TestCompactPreservesHeaderForExchangeSymbolFilters(t *testing.T) {
	dir := t.TempDir()
	p := NewDualPersister(dir)

	// Compact with empty rows
	err := p.Compact("exchange_symbol_filters.csv", []interface{}{})
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "exchange_symbol_filters.csv"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 1 {
		t.Errorf("Expected 1 line (header), got %d lines", len(lines))
	}

	if lines[0] == "" {
		t.Error("Header is empty")
	}

	// Verify header contains expected fields
	if !strings.Contains(lines[0], "id,exchange,pos_type") {
		t.Errorf("Header missing expected fields: %s", lines[0])
	}

	t.Logf("Exchange symbol filters header: %s", lines[0])
}

func TestCompactWithDataThenEmpty(t *testing.T) {
	dir := t.TempDir()
	p := NewDualPersister(dir)

	// First compact with data
	pos := &order.UserOrderPosition{ID: 1, UserID: 100}
	err := p.Compact("user_order_positions.csv", []interface{}{pos})
	if err != nil {
		t.Fatalf("Compact with data failed: %v", err)
	}

	// Verify data was written
	content, err := os.ReadFile(filepath.Join(dir, "user_order_positions.csv"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 lines (header + data), got %d", len(lines))
	}

	// Now compact with empty (simulating all positions closed)
	err = p.Compact("user_order_positions.csv", []interface{}{})
	if err != nil {
		t.Fatalf("Compact empty failed: %v", err)
	}

	// Verify header is still preserved
	content, err = os.ReadFile(filepath.Join(dir, "user_order_positions.csv"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	lines = strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 1 {
		t.Errorf("Expected 1 line (header) after clearing, got %d lines", len(lines))
	}

	if lines[0] == "" {
		t.Error("Header is empty after clearing data")
	}

	t.Logf("Header preserved after clearing: %s", lines[0])
}