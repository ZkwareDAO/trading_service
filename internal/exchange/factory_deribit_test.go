package exchange_test

import (
	"testing"

	"trading-service/internal/exchange"
	"trading-service/internal/exchange/deribit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// User Journey: As a system, I want to create Deribit exchange via factory
// =============================================================================

func TestExchangeFactory_CreateDeribit(t *testing.T) {
	factory := exchange.NewExchangeFactory()

	// Initially, "deribit" should not be available
	_, err := factory.Create("deribit")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported exchange")

	// Register Deribit with test config
	cfg := exchange.ExchangeConfig{
		APIKey:    "test_key",
		APISecret: "test_secret",
		APIPwd:    "test_pwd",
		Testnet:   true,
	}
	factory.SetConfig("deribit", cfg)

	// Create Deribit instance
	ex, err := deribit.NewDeribit(cfg.APIKey, cfg.APISecret, cfg.APIPwd, cfg.Testnet)
	require.NoError(t, err)

	factory.Register("deribit", ex)

	// Now "deribit" should be available
	created, err := factory.Create("deribit")
	require.NoError(t, err)

	// Verify it's a Deribit instance
	assert.Equal(t, "deribit", created.Name())

	// Verify config is stored
	retrievedCfg := factory.GetConfig("deribit")
	assert.Equal(t, cfg.APIKey, retrievedCfg.APIKey)
	assert.Equal(t, cfg.APISecret, retrievedCfg.APISecret)
	assert.Equal(t, cfg.APIPwd, retrievedCfg.APIPwd)
	assert.Equal(t, cfg.Testnet, retrievedCfg.Testnet)
}

func TestExchangeFactory_DeribitImplementsExchange(t *testing.T) {
	// Compile-time check
	var _ exchange.Exchange = (*deribit.Deribit)(nil)
}