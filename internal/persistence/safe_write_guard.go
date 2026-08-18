package persistence

import (
	"fmt"
	"os"
	"path/filepath"
)

// validateBeforeCompact checks that compact won't cause data loss.
// Returns error if memory is empty but CSV file has data.
func validateBeforeCompact(p *DualPersister, tableName string, memoryCount int) error {
	filePath := filepath.Join(p.dataDir, tableName)

	stat, err := os.Stat(filePath)
	if os.IsNotExist(err) || stat.Size() == 0 {
		return nil // Safe: file doesn't exist or is empty
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", tableName, err)
	}

	// CRITICAL: Memory empty but file has data - would lose data!
	if memoryCount == 0 {
		records, err := p.ReadAllCSV(tableName)
		if err != nil {
			return fmt.Errorf("read %s: %w", tableName, err)
		}
		if len(records) > 0 {
			return fmt.Errorf("CRITICAL: %s has %d records but memory empty - would lose data!", tableName, len(records))
		}
	}

	return nil
}

// validateAllTablesBeforeCompact checks all critical tables.
func validateAllTablesBeforeCompact(p *DualPersister, state *GlobalState) error {
	// All tables that CompactAll writes (must match exactly)
	tables := []struct {
		name  string
		count int
	}{
		{"users.csv", len(state.Users)},
		{"strategies.csv", len(state.Strategies)},
		{"strategy_assets.csv", len(state.StrategyAssets)},
		{"user_strategies.csv", len(state.UserStrategies)},
		{"user_orders.csv", len(state.UserOrders)},
		{"leverage_configs.csv", len(state.LeverageConfigs)},
		{"exchange_symbol_filters.csv", len(state.ExchangeSymbolFilters)},
		{"uprunning_orders.csv", len(state.UprunningOrders)},
		{"user_order_positions.csv", len(state.UserOrderPositions)},
		{"user_positions.csv", len(state.UserPositions)},
	}

	for _, t := range tables {
		if err := validateBeforeCompact(p, t.name, t.count); err != nil {
			return err // Error already descriptive
		}
	}
	return nil
}
