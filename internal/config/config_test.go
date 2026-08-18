package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear any env vars
	os.Unsetenv("ORDER_SERVICE_PORT")
	os.Unsetenv("ORDER_SERVICE_DATA_DIR")
	os.Unsetenv("ORDER_SERVICE_MODE")
	os.Unsetenv("ORDER_SERVICE_CONFIG")
	os.Unsetenv("ORDER_SERVICE_DEFAULT_LEVERAGE")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Server.Port != 8081 {
		t.Errorf("expected default port 8081, got %d", cfg.Server.Port)
	}
	if cfg.Server.Mode != "release" {
		t.Errorf("expected default mode 'release', got '%s'", cfg.Server.Mode)
	}
	if cfg.Defaults.Leverage != 1 {
		t.Errorf("expected default leverage 1, got %d", cfg.Defaults.Leverage)
	}
	if cfg.Defaults.Slippage != 0.01 {
		t.Errorf("expected default slippage 0.01, got %f", cfg.Defaults.Slippage)
	}
	if cfg.FilterSyncInterval != 240*time.Hour {
		t.Errorf("expected default filter sync interval 240h, got %v", cfg.FilterSyncInterval)
	}
}

func TestLoadConfig_FromEnv(t *testing.T) {
	os.Setenv("ORDER_SERVICE_PORT", "9090")
	os.Setenv("ORDER_SERVICE_DATA_DIR", "/tmp/order-data-test")
	os.Setenv("ORDER_SERVICE_MODE", "debug")
	os.Setenv("ORDER_SERVICE_DEFAULT_LEVERAGE", "10")
	defer func() {
		os.Unsetenv("ORDER_SERVICE_PORT")
		os.Unsetenv("ORDER_SERVICE_DATA_DIR")
		os.Unsetenv("ORDER_SERVICE_MODE")
		os.Unsetenv("ORDER_SERVICE_DEFAULT_LEVERAGE")
	}()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.Mode != "debug" {
		t.Errorf("expected mode 'debug', got '%s'", cfg.Server.Mode)
	}
	if cfg.Storage.DataDir != "/tmp/order-data-test" {
		t.Errorf("expected data_dir '/tmp/order-data-test', got '%s'", cfg.Storage.DataDir)
	}
	if cfg.Defaults.Leverage != 10 {
		t.Errorf("expected leverage 10, got %d", cfg.Defaults.Leverage)
	}
}

func TestLoadConfig_FromYAML(t *testing.T) {
	dir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	yamlContent := `server:
  port: 3000
  mode: debug
storage:
  type: csv
  data_dir: ./mydata
defaults:
  leverage: 5
  slippage: 0.02
filter_sync_interval: 48h
`
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("ORDER_SERVICE_CONFIG", yamlPath)
	defer os.Unsetenv("ORDER_SERVICE_CONFIG")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Server.Port != 3000 {
		t.Errorf("expected port 3000, got %d", cfg.Server.Port)
	}
	if cfg.Server.Mode != "debug" {
		t.Errorf("expected mode 'debug', got '%s'", cfg.Server.Mode)
	}
	if cfg.Defaults.Leverage != 5 {
		t.Errorf("expected leverage 5, got %d", cfg.Defaults.Leverage)
	}
	if cfg.Defaults.Slippage != 0.02 {
		t.Errorf("expected slippage 0.02, got %f", cfg.Defaults.Slippage)
	}
	if cfg.FilterSyncInterval != 48*time.Hour {
		t.Errorf("expected filter sync interval 48h, got %v", cfg.FilterSyncInterval)
	}
}

func TestLoadConfig_FromYAMLWithNotification(t *testing.T) {
	dir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	yamlContent := `server:
  port: 3000
  mode: debug
storage:
  type: csv
  data_dir: ./mydata
defaults:
  leverage: 5
  slippage: 0.02
notification:
  enabled: true
  open_url: https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=OPEN_KEY
  close_url: https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=CLOSE_KEY
  test_url: https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=TEST_KEY
`
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("ORDER_SERVICE_CONFIG", yamlPath)
	defer os.Unsetenv("ORDER_SERVICE_CONFIG")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if !cfg.Notification.Enabled {
		t.Error("expected notification enabled")
	}
	if cfg.Notification.OpenURL == "" || cfg.Notification.CloseURL == "" {
		t.Error("expected notification URLs to be set")
	}
}

func TestConfig_DataDirCreated(t *testing.T) {
	dir, err := os.MkdirTemp("", "config-data-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	dataDir := filepath.Join(dir, "order-data")
	os.Setenv("ORDER_SERVICE_DATA_DIR", dataDir)
	defer os.Unsetenv("ORDER_SERVICE_DATA_DIR")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	// Ensure data dir path exists on disk
	info, err := os.Stat(cfg.Storage.DataDir)
	if err != nil {
		t.Fatalf("data dir does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("data dir is not a directory")
	}

	// Ensure .compact subdirectory exists
	compactDir := filepath.Join(cfg.Storage.DataDir, ".compact")
	info, err = os.Stat(compactDir)
	if err != nil {
		t.Fatalf(".compact dir does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Error(".compact dir is not a directory")
	}
}

// unsetTestnetEnv clears the testnet env vars so YAML behaviour can be asserted
// without interference from the ambient environment.
func unsetTestnetEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"BINANCE_TESTNET", "HYPERLIQUID_TESTNET", "DERIBIT_TESTNET"} {
		os.Unsetenv(k)
	}
}

func TestLoadConfig_ExchangeTestnetDefaultsToMainnet(t *testing.T) {
	os.Unsetenv("ORDER_SERVICE_CONFIG")
	unsetTestnetEnv(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	// Unconfigured must mean MAINNET. This is a money-safety default: the flags are
	// opt-in everywhere else in the codebase, so silently defaulting to testnet here
	// would let a user believe they are on testnet when trading real funds.
	if cfg.Exchange.BinanceTestnet {
		t.Error("expected BinanceTestnet=false (mainnet) by default, got true")
	}
	if cfg.Exchange.HyperliquidTestnet {
		t.Error("expected HyperliquidTestnet=false (mainnet) by default, got true")
	}
	if cfg.Exchange.DeribitTestnet {
		t.Error("expected DeribitTestnet=false (mainnet) by default, got true")
	}
}

func TestLoadConfig_ExchangeTestnetFromYAML(t *testing.T) {
	dir, err := os.MkdirTemp("", "config-exchange-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	yamlContent := `server:
  port: 8081
exchange:
  binance_testnet: true
  hyperliquid_testnet: true
  deribit_testnet: true
`
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("ORDER_SERVICE_CONFIG", yamlPath)
	defer os.Unsetenv("ORDER_SERVICE_CONFIG")
	unsetTestnetEnv(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if !cfg.Exchange.BinanceTestnet {
		t.Error("expected BinanceTestnet=true from YAML, got false")
	}
	if !cfg.Exchange.HyperliquidTestnet {
		t.Error("expected HyperliquidTestnet=true from YAML, got false")
	}
	if !cfg.Exchange.DeribitTestnet {
		t.Error("expected DeribitTestnet=true from YAML, got false")
	}
}

func TestLoadConfig_ExchangeTestnetEnvOverridesYAML(t *testing.T) {
	dir, err := os.MkdirTemp("", "config-exchange-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// YAML says mainnet; env must be able to force testnet on.
	yamlContent := `exchange:
  binance_testnet: false
  hyperliquid_testnet: false
  deribit_testnet: false
`
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("ORDER_SERVICE_CONFIG", yamlPath)
	os.Setenv("BINANCE_TESTNET", "true")
	os.Setenv("HYPERLIQUID_TESTNET", "true")
	os.Setenv("DERIBIT_TESTNET", "true")
	defer func() {
		os.Unsetenv("ORDER_SERVICE_CONFIG")
		unsetTestnetEnv(t)
	}()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
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

func TestLoadConfig_ExchangeTestnetPartialYAML(t *testing.T) {
	dir, err := os.MkdirTemp("", "config-exchange-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Only one exchange configured: the others must stay on mainnet, not inherit.
	yamlContent := `exchange:
  hyperliquid_testnet: true
`
	yamlPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("ORDER_SERVICE_CONFIG", yamlPath)
	defer os.Unsetenv("ORDER_SERVICE_CONFIG")
	unsetTestnetEnv(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if !cfg.Exchange.HyperliquidTestnet {
		t.Error("expected HyperliquidTestnet=true from YAML, got false")
	}
	if cfg.Exchange.BinanceTestnet {
		t.Error("expected BinanceTestnet to stay false when unset in YAML, got true")
	}
	if cfg.Exchange.DeribitTestnet {
		t.Error("expected DeribitTestnet to stay false when unset in YAML, got true")
	}
}
