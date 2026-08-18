package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ReporterConfig holds position reporter settings.
type ReporterConfig struct {
	APIURL string // API endpoint for fetching positions, e.g. "http://localhost:8080/api/v1/exchange/positions"
}

// Config holds all service configuration.
type Config struct {
	Server             ServerConfig
	Storage            StorageConfig
	Defaults           DefaultConfig
	Notification       NotificationConfig
	Reporter           ReporterConfig
	Exchange           ExchangeConfig
	FilterSyncInterval       time.Duration // how often to sync exchange symbol filters
	PositionSyncInterval     time.Duration // how often to sync user_order_positions from CSV (UOS reads PMS updates)
}

// ExchangeConfig controls per-exchange testnet selection.
//
// The zero value means MAINNET for every exchange: testnet is always opt-in, so a
// missing or partial configuration never silently redirects orders to a network the
// operator did not ask for. Mirrors PMExchangeConfig in the position monitor service
// so both services interpret the same `exchange:` YAML section identically.
type ExchangeConfig struct {
	BinanceTestnet     bool
	HyperliquidTestnet bool
	DeribitTestnet     bool
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int
	Mode string // "release", "debug", "test"
}

// StorageConfig holds CSV storage settings.
type StorageConfig struct {
	Type    string // "csv"
	DataDir string
}

// DefaultConfig holds default trading parameters.
type DefaultConfig struct {
	Leverage int
	Slippage float64
}

// NotificationConfig holds webhook notification settings.
type NotificationConfig struct {
	Enabled  bool
	OpenURL  string // webhook URL for open orders (下单通知)
	CloseURL string // webhook URL for close orders (风控平仓通知)
	TestURL  string // webhook URL for test signals (测试通知)
}

// LoadConfig loads configuration from env vars with optional YAML override.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port: 8081,
			Mode: "release",
		},
		Storage: StorageConfig{
			Type:    "csv",
			DataDir: "./data",
		},
		Defaults: DefaultConfig{
			Leverage: 1,
			Slippage: 0.01,
		},
		Reporter: ReporterConfig{
			APIURL: "http://localhost:8080/api/v1/exchange/positions",
		},
		FilterSyncInterval:   240 * time.Hour, // 10 days
		PositionSyncInterval: 5 * time.Second, // 5 seconds (sync user_order_positions from PMS CSV updates)
	}

	// Load from YAML: env var path > config.yaml in current directory
	yamlPath := os.Getenv("ORDER_SERVICE_CONFIG")
	if yamlPath == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			yamlPath = "config.yaml"
		}
	}
	if yamlPath != "" {
		if err := loadFromYAML(yamlPath, cfg); err != nil {
			return nil, err
		}
	}

	// Environment variables override YAML values
	if port := os.Getenv("ORDER_SERVICE_PORT"); port != "" {
		p, err := strconv.Atoi(port)
		if err != nil {
			return nil, err
		}
		cfg.Server.Port = p
	}
	if mode := os.Getenv("ORDER_SERVICE_MODE"); mode != "" {
		cfg.Server.Mode = mode
	}
	if dataDir := os.Getenv("ORDER_SERVICE_DATA_DIR"); dataDir != "" {
		cfg.Storage.DataDir = dataDir
	}
	if leverage := os.Getenv("ORDER_SERVICE_DEFAULT_LEVERAGE"); leverage != "" {
		l, err := strconv.Atoi(leverage)
		if err != nil {
			return nil, err
		}
		cfg.Defaults.Leverage = l
	}

	// Testnet flags: env wins over YAML, and only the exact string "true" enables
	// testnet. Any other value (including unset) leaves the YAML/default choice,
	// which is mainnet unless explicitly configured otherwise.
	if os.Getenv("BINANCE_TESTNET") == "true" {
		cfg.Exchange.BinanceTestnet = true
	}
	if os.Getenv("HYPERLIQUID_TESTNET") == "true" {
		cfg.Exchange.HyperliquidTestnet = true
	}
	if os.Getenv("DERIBIT_TESTNET") == "true" {
		cfg.Exchange.DeribitTestnet = true
	}

	// Ensure data directory exists
	if err := os.MkdirAll(cfg.Storage.DataDir, 0755); err != nil {
		return nil, err
	}

	// Ensure .compact subdirectory exists
	if err := os.MkdirAll(filepath.Join(cfg.Storage.DataDir, ".compact"), 0755); err != nil {
		return nil, err
	}

	return cfg, nil
}

// loadFromYAML is a simple key-value YAML parser for config files.
func loadFromYAML(path string, cfg *Config) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	section := ""

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		isTopLevel := !strings.HasPrefix(rawLine, " ") && !strings.HasPrefix(rawLine, "\t")

		// Detect section headers (no leading whitespace, ends with colon)
		if isTopLevel && strings.HasSuffix(line, ":") {
			section = strings.TrimSuffix(line, ":")
			continue
		}

		// Parse key: value
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"")

		switch section {
		case "server":
			switch key {
			case "port":
				p, err := strconv.Atoi(value)
				if err == nil {
					cfg.Server.Port = p
				}
			case "mode":
				cfg.Server.Mode = value
			}
		case "storage":
			switch key {
			case "data_dir":
				cfg.Storage.DataDir = value
			case "type":
				cfg.Storage.Type = value
			}
		case "defaults":
			switch key {
			case "leverage":
				l, err := strconv.Atoi(value)
				if err == nil {
					cfg.Defaults.Leverage = l
				}
			case "slippage":
				s, err := strconv.ParseFloat(value, 64)
				if err == nil {
					cfg.Defaults.Slippage = s
				}
			}
		case "notification":
			switch key {
			case "enabled":
				cfg.Notification.Enabled = value == "true"
			case "open_url":
				cfg.Notification.OpenURL = value
			case "close_url":
				cfg.Notification.CloseURL = value
			case "test_url":
				cfg.Notification.TestURL = value
			}
		case "reporter":
			switch key {
			case "api_url":
				cfg.Reporter.APIURL = value
			}
		case "exchange":
			switch key {
			case "binance_testnet":
				cfg.Exchange.BinanceTestnet = value == "true"
			case "hyperliquid_testnet":
				cfg.Exchange.HyperliquidTestnet = value == "true"
			case "deribit_testnet":
				cfg.Exchange.DeribitTestnet = value == "true"
			}
		}

		if isTopLevel {
			// top-level keys
			switch key {
			case "filter_sync_interval":
				d, err := time.ParseDuration(value)
				if err == nil {
					cfg.FilterSyncInterval = d
				}
			case "position_sync_interval":
				d, err := time.ParseDuration(value)
				if err == nil {
					cfg.PositionSyncInterval = d
				}
			}
		}
	}

	return nil
}
