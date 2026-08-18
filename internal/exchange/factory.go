package exchange

import "fmt"

// ExchangeConfig holds exchange connection parameters.
type ExchangeConfig struct {
	APIKey    string
	APISecret string
	APIPwd    string
	BaseURL   string
	Testnet   bool
}

// ExchangeFactory creates exchange instances by name.
type ExchangeFactory struct {
	exchanges map[string]Exchange
	configs   map[string]ExchangeConfig
}

// NewExchangeFactory creates a factory with mock pre-registered.
func NewExchangeFactory() *ExchangeFactory {
	return &ExchangeFactory{
		exchanges: map[string]Exchange{"mock": NewMockExchange()},
		configs:   make(map[string]ExchangeConfig),
	}
}

// Create returns an exchange instance by name.
func (f *ExchangeFactory) Create(name string) (Exchange, error) {
	ex, ok := f.exchanges[name]
	if !ok {
		return nil, fmt.Errorf("unsupported exchange: %s", name)
	}
	return ex, nil
}

// Register adds a custom exchange instance.
func (f *ExchangeFactory) Register(name string, ex Exchange) {
	f.exchanges[name] = ex
}

// SetConfig stores config for an exchange.
func (f *ExchangeFactory) SetConfig(name string, cfg ExchangeConfig) {
	f.configs[name] = cfg
}

// GetConfig retrieves config for an exchange.
func (f *ExchangeFactory) GetConfig(name string) ExchangeConfig {
	return f.configs[name]
}
