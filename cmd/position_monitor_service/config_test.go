package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadPMConfig_Defaults(t *testing.T) {
	os.Unsetenv("POSITION_MONITOR_CONFIG")
	cfg, err := LoadPMConfig()
	if err != nil {
		t.Fatalf("LoadPMConfig: %v", err)
	}
	if cfg.Defaults.StopLossPct != -0.02 {
		t.Errorf("expected stop_loss_pct -0.02, got %f", cfg.Defaults.StopLossPct)
	}
	if cfg.Defaults.ProfitDrawdownPct != 0.05 {
		t.Errorf("expected profit_drawdown_pct 0.05, got %f", cfg.Defaults.ProfitDrawdownPct)
	}
	if cfg.Defaults.TrailingActivationPct != 0.05 {
		t.Errorf("expected trailing_activation_pct 0.05, got %f", cfg.Defaults.TrailingActivationPct)
	}
	if cfg.Defaults.TimeStopHours != 72 {
		t.Errorf("expected time_stop_hours 72, got %d", cfg.Defaults.TimeStopHours)
	}
	if cfg.Runtime.PriceSnapshotInterval != 10*time.Second {
		t.Errorf("expected price_snapshot_interval 10s, got %v", cfg.Runtime.PriceSnapshotInterval)
	}
}

func TestLoadPMConfig_FromYAML(t *testing.T) {
	dir, err := os.MkdirTemp("", "pm-config-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	yamlContent := `defaults:
  stop_loss_pct: -0.05
  profit_drawdown_pct: 0.10
  trailing_activation_pct: 0.10
  time_stop_hours: 48
runtime:
  price_snapshot_interval: 3s
notification:
  enabled: true
  open_url: "https://example.invalid/open"
  close_url: "https://example.invalid/close"
  test_url: "https://example.invalid/test"
`
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("POSITION_MONITOR_CONFIG", yamlPath)
	defer os.Unsetenv("POSITION_MONITOR_CONFIG")

	cfg, err := LoadPMConfig()
	if err != nil {
		t.Fatalf("LoadPMConfig: %v", err)
	}
	if cfg.Defaults.StopLossPct != -0.05 {
		t.Errorf("expected stop_loss_pct -0.05, got %f", cfg.Defaults.StopLossPct)
	}
	if cfg.Defaults.ProfitDrawdownPct != 0.10 {
		t.Errorf("expected profit_drawdown_pct 0.10, got %f", cfg.Defaults.ProfitDrawdownPct)
	}
	if cfg.Defaults.TrailingActivationPct != 0.10 {
		t.Errorf("expected trailing_activation_pct 0.10, got %f", cfg.Defaults.TrailingActivationPct)
	}
	if cfg.Defaults.TimeStopHours != 48 {
		t.Errorf("expected time_stop_hours 48, got %d", cfg.Defaults.TimeStopHours)
	}
	if cfg.Runtime.PriceSnapshotInterval != 3*time.Second {
		t.Errorf("expected price_snapshot_interval 3s, got %v", cfg.Runtime.PriceSnapshotInterval)
	}
	if !cfg.Notification.Enabled {
		t.Error("expected notification enabled")
	}
	if cfg.Notification.OpenURL != "https://example.invalid/open" {
		t.Errorf("expected notification open_url, got %s", cfg.Notification.OpenURL)
	}
	if cfg.Notification.CloseURL != "https://example.invalid/close" {
		t.Errorf("expected notification close_url, got %s", cfg.Notification.CloseURL)
	}
	if cfg.Notification.TestURL != "https://example.invalid/test" {
		t.Errorf("expected notification test_url, got %s", cfg.Notification.TestURL)
	}
	// Verify default spread threshold when not configured
	if cfg.DeribitSpreadThreshold != 0.005 {
		t.Errorf("expected default deribit_spread_threshold 0.005, got %f", cfg.DeribitSpreadThreshold)
	}
}

func TestLoadPMConfig_FromEnv(t *testing.T) {
	os.Unsetenv("POSITION_MONITOR_CONFIG")
	os.Setenv("POSITION_MONITOR_TIME_STOP_HOURS", "24")
	defer os.Unsetenv("POSITION_MONITOR_TIME_STOP_HOURS")

	cfg, err := LoadPMConfig()
	if err != nil {
		t.Fatalf("LoadPMConfig: %v", err)
	}
	if cfg.Defaults.TimeStopHours != 24 {
		t.Errorf("expected time_stop_hours 24, got %d", cfg.Defaults.TimeStopHours)
	}
}

// TestLoadPMConfig_DeribitSpreadThreshold_FromYAML tests that deribit spread threshold
// can be configured via YAML and defaults to 0.005 when not specified.
func TestLoadPMConfig_DeribitSpreadThreshold_FromYAML(t *testing.T) {
	tests := []struct {
		name           string
		yamlContent    string
		expectedValue  float64
	}{
		{
			name: "with explicit deribit.spread_threshold",
			yamlContent: `deribit:
  spread_threshold: 0.01
`,
			expectedValue: 0.01,
		},
		{
			name: "without deribit section (use default)",
			yamlContent: `defaults:
  stop_loss_pct: -0.02
`,
			expectedValue: 0.005,
		},
		{
			name: "with deribit section but no spread_threshold (use default)",
			yamlContent: `deribit:
  other_config: value
`,
			expectedValue: 0.005,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "pm-config-*")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)

			yamlPath := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(yamlPath, []byte(tt.yamlContent), 0644); err != nil {
				t.Fatal(err)
			}

			os.Setenv("POSITION_MONITOR_CONFIG", yamlPath)
			defer os.Unsetenv("POSITION_MONITOR_CONFIG")

			cfg, err := LoadPMConfig()
			if err != nil {
				t.Fatalf("LoadPMConfig: %v", err)
			}

			if cfg.DeribitSpreadThreshold != tt.expectedValue {
				t.Errorf("expected deribit_spread_threshold %f, got %f", tt.expectedValue, cfg.DeribitSpreadThreshold)
			}
		})
	}
}

func TestLoadPMConfig_PriceSnapshotIntervalFromEnv(t *testing.T) {
	os.Unsetenv("POSITION_MONITOR_CONFIG")
	os.Setenv("POSITION_MONITOR_PRICE_SNAPSHOT_INTERVAL", "5s")
	defer os.Unsetenv("POSITION_MONITOR_PRICE_SNAPSHOT_INTERVAL")

	cfg, err := LoadPMConfig()
	if err != nil {
		t.Fatalf("LoadPMConfig: %v", err)
	}
	if cfg.Runtime.PriceSnapshotInterval != 5*time.Second {
		t.Errorf("expected price_snapshot_interval 5s, got %v", cfg.Runtime.PriceSnapshotInterval)
	}
}

func TestLoadPMConfig_ExchangeConfigFromEnv(t *testing.T) {
	os.Unsetenv("POSITION_MONITOR_CONFIG")
	os.Setenv("BINANCE_TESTNET", "true")
	os.Setenv("HYPERLIQUID_TESTNET", "true")
	os.Setenv("DERIBIT_TESTNET", "true")
	defer func() {
		os.Unsetenv("BINANCE_TESTNET")
		os.Unsetenv("HYPERLIQUID_TESTNET")
		os.Unsetenv("DERIBIT_TESTNET")
	}()

	cfg, err := LoadPMConfig()
	if err != nil {
		t.Fatalf("LoadPMConfig: %v", err)
	}
	if !cfg.Exchange.BinanceTestnet {
		t.Error("expected BinanceTestnet=true, got false")
	}
	if !cfg.Exchange.HyperliquidTestnet {
		t.Error("expected HyperliquidTestnet=true, got false")
	}
	if !cfg.Exchange.DeribitTestnet {
		t.Error("expected DeribitTestnet=true, got false")
	}
}

func TestLoadPMConfig_ExchangeConfigDefaultsToFalse(t *testing.T) {
	os.Unsetenv("POSITION_MONITOR_CONFIG")
	os.Unsetenv("BINANCE_TESTNET")
	os.Unsetenv("HYPERLIQUID_TESTNET")
	os.Unsetenv("DERIBIT_TESTNET")

	cfg, err := LoadPMConfig()
	if err != nil {
		t.Fatalf("LoadPMConfig: %v", err)
	}
	if cfg.Exchange.BinanceTestnet {
		t.Error("expected BinanceTestnet=false by default, got true")
	}
	if cfg.Exchange.HyperliquidTestnet {
		t.Error("expected HyperliquidTestnet=false by default, got true")
	}
	if cfg.Exchange.DeribitTestnet {
		t.Error("expected DeribitTestnet=false by default, got true")
	}
}

func TestLoadPMConfig_ExchangeConfigFromYAML(t *testing.T) {
	dir, err := os.MkdirTemp("", "pm-config-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	yamlContent := `defaults:
  stop_loss_pct: -0.02
runtime:
  price_snapshot_interval: 10s
exchange:
  binance_testnet: true
  hyperliquid_testnet: true
  deribit_testnet: true
`
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("POSITION_MONITOR_CONFIG", yamlPath)
	os.Unsetenv("BINANCE_TESTNET")
	os.Unsetenv("HYPERLIQUID_TESTNET")
	os.Unsetenv("DERIBIT_TESTNET")
	defer os.Unsetenv("POSITION_MONITOR_CONFIG")

	cfg, err := LoadPMConfig()
	if err != nil {
		t.Fatalf("LoadPMConfig: %v", err)
	}
	if !cfg.Exchange.BinanceTestnet {
		t.Error("expected BinanceTestnet=true from YAML, got false")
	}
	if !cfg.Exchange.HyperliquidTestnet {
		t.Error("expected HyperliquidTestnet=true from YAML, got false")
	}
	// All three exchanges must behave consistently: deribit_testnet was previously
	// missing from the YAML parser, silently leaving Deribit on mainnet.
	if !cfg.Exchange.DeribitTestnet {
		t.Error("expected DeribitTestnet=true from YAML, got false")
	}
}

func TestLoadPMConfig_EnvOverridesYAMLExchange(t *testing.T) {
	dir, err := os.MkdirTemp("", "pm-config-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	yamlContent := `exchange:
  binance_testnet: false
  hyperliquid_testnet: false
  deribit_testnet: false
`
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("POSITION_MONITOR_CONFIG", yamlPath)
	os.Setenv("BINANCE_TESTNET", "true")
	os.Setenv("HYPERLIQUID_TESTNET", "true")
	os.Setenv("DERIBIT_TESTNET", "true")
	defer func() {
		os.Unsetenv("POSITION_MONITOR_CONFIG")
		os.Unsetenv("BINANCE_TESTNET")
		os.Unsetenv("HYPERLIQUID_TESTNET")
		os.Unsetenv("DERIBIT_TESTNET")
	}()

	cfg, err := LoadPMConfig()
	if err != nil {
		t.Fatalf("LoadPMConfig: %v", err)
	}
	if !cfg.Exchange.BinanceTestnet {
		t.Error("expected BINANCE_TESTNET env to override YAML to true, got false")
	}
	if !cfg.Exchange.HyperliquidTestnet {
		t.Error("expected HYPERLIQUID_TESTNET env to override YAML to true, got false")
	}
	if !cfg.Exchange.DeribitTestnet {
		t.Error("expected DERIBIT_TESTNET env to override YAML to true, got false")
	}
}

func TestGenerateDefaultRules(t *testing.T) {
	cfg := &PMConfig{
		Defaults: PMDefaults{
			StopLossPct:           -0.02,
			ProfitDrawdownPct:     0.05,
			TrailingActivationPct: 0.05,
		},
	}
	nextID := new(int)
	*nextID = 100
	rules := GenerateDefaultRules(1000, cfg, nextID)

	if len(rules) != 3 {
		t.Fatalf("expected 3 rules (stop-loss + profit-trigger + profit-followup), got %d", len(rules))
	}

	// Rule 1: Stop-loss (roi <= -0.02, active)
	if rules[0].ConditionName != "roi" || rules[0].Operator != "<=" || rules[0].Value != -0.02 {
		t.Errorf("rule 1: expected roi <= -0.02, got %s %s %v", rules[0].ConditionName, rules[0].Operator, rules[0].Value)
	}
	if rules[0].Status != "active" {
		t.Errorf("rule 1: expected active, got %s", rules[0].Status)
	}

	// Rule 2: Profit trigger (roi >= 0.05, active, chains to rule 3)
	if rules[1].ConditionName != "roi" || rules[1].Operator != ">=" || rules[1].Value != 0.05 {
		t.Errorf("rule 2: expected roi >= 0.05, got %s %s %v", rules[1].ConditionName, rules[1].Operator, rules[1].Value)
	}

	// Rule 3: Profit follow-up (profit_drawdown_pct >= 0.05, inactive)
	if rules[2].ConditionName != "profit_drawdown_pct" || rules[2].Operator != ">=" || rules[2].Value != 0.05 {
		t.Errorf("rule 3: expected profit_drawdown_pct >= 0.05, got %s %s %v", rules[2].ConditionName, rules[2].Operator, rules[2].Value)
	}
	if rules[2].Status != "inactive" {
		t.Errorf("rule 3: expected inactive, got %s", rules[2].Status)
	}

	// Verify IDs are sequential
	if rules[0].ID != 100 || rules[1].ID != 101 || rules[2].ID != 102 {
		t.Errorf("expected IDs 100,101,102, got %d,%d,%d", rules[0].ID, rules[1].ID, rules[2].ID)
	}
	if *nextID != 103 {
		t.Errorf("expected nextID 103, got %d", *nextID)
	}
}
