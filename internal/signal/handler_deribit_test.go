package signal

import (
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// User Journey: As a UOS user, I want to trade options on Deribit exchange
// =============================================================================

// TestDeribitAdapterExists verifies that NewDeribitAdapter function exists
// and returns a valid Deribit exchange instance.
func TestDeribitAdapterExists(t *testing.T) {
	// This test verifies the adapter factory function exists
	// RED: Function doesn't exist yet
	adapter, err := NewDeribitAdapter("test_key", "test_secret", "test_password", true)
	require.NoError(t, err)
	require.NotNil(t, adapter)

	// Verify it implements the Exchange interface
	assert.Equal(t, "deribit", adapter.Name())
}

// TestHandlerCreatesDeribitExchange verifies that Handler.createExchangeForUser
// correctly creates a Deribit exchange instance for users with exchange="deribit".
func TestHandlerCreatesDeribitExchange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)
	repo.SetSyncInterval(24 * 3600 * time.Second) // Prevent CSV reload during test

	// Create handler with testnetDeribit=true
	h := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, true, nil, nil)

	// Create a user with Deribit credentials
	user := &order.User{
		Name:        "deribit_test_user",
		Exchange:    "deribit",
		APIKey:      "test_api_key",
		APISecret:   "test_api_secret",
		APIPassword: "test_api_password",
	}

	// Verify createExchangeForUser returns a Deribit instance
	ex, err := h.createExchangeForUser(user)
	require.NoError(t, err)
	require.NotNil(t, ex)
	assert.Equal(t, "deribit", ex.Name())
}

// TestHandlerRejectsDeribitUserWithoutCredentials verifies that Handler
// returns an error when a Deribit user is missing required credentials.
func TestHandlerRejectsDeribitUserWithoutCredentials(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)
	repo.SetSyncInterval(24 * 3600 * time.Second)

	h := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, true, nil, nil)

	tests := []struct {
		name    string
		user    *order.User
		wantErr string
	}{
		{
			name: "missing APIKey",
			user: &order.User{
				Name:        "deribit_user",
				Exchange:    "deribit",
				APISecret:   "secret",
				APIPassword: "password",
			},
			wantErr: "missing deribit credentials",
		},
		{
			name: "missing APISecret",
			user: &order.User{
				Name:        "deribit_user",
				Exchange:    "deribit",
				APIKey:      "key",
				APIPassword: "password",
			},
			wantErr: "missing deribit credentials",
		},
		{
			name: "missing APIPassword",
			user: &order.User{
				Name:      "deribit_user",
				Exchange:  "deribit",
				APIKey:    "key",
				APISecret: "secret",
			},
			wantErr: "missing deribit credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.createExchangeForUser(tt.user)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestHandlerTestnetDeribitField verifies that the Handler correctly stores
// and uses the testnetDeribit field.
func TestHandlerTestnetDeribitField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)

	// Create handler with testnetDeribit=true
	h1 := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, true, nil, nil)
	assert.True(t, h1.testnetDeribit)
	assert.False(t, h1.testnetBinance)
	assert.False(t, h1.testnetHyperliquid)

	// Create handler with testnetDeribit=false
	h2 := NewHandlerWithDataDirAndTestnetConfig(repo, dir, true, true, false, nil, nil)
	assert.False(t, h2.testnetDeribit)
	assert.True(t, h2.testnetBinance)
	assert.True(t, h2.testnetHyperliquid)
}

// TestDeribitAdapterImplementsExchange verifies compile-time interface compliance.
func TestDeribitAdapterImplementsExchange(t *testing.T) {
	// This is a compile-time check - if it compiles, the test passes
	var _ exchange.Exchange = (*exchange.MockExchange)(nil)
	// After implementation, we can add:
	// var _ exchange.Exchange = (*deribit.Deribit)(nil)
}
