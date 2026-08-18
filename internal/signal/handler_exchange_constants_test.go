package signal

import (
	"testing"
)

// TestExchangeConstantsExist verifies that exchange name constants are defined
// and have the correct values. This prevents typos and improves maintainability.
func TestExchangeConstantsExist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{
			name:     "Deribit constant has correct value",
			constant: ExchangeDeribit,
			expected: "deribit",
		},
		{
			name:     "Binance constant has correct value",
			constant: ExchangeBinance,
			expected: "binance",
		},
		{
			name:     "Hyperliquid constant has correct value",
			constant: ExchangeHyperliquid,
			expected: "hyperliquid",
		},
		{
			name:     "Mock constant has correct value",
			constant: ExchangeMock,
			expected: "mock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

// TestExchangeConstantsAreUsed verifies that the constants can be used
// in comparisons (i.e., they work as string constants should).
func TestExchangeConstantsAreUsed(t *testing.T) {
	t.Parallel()

	// Each constant should work correctly in string comparisons
	if ExchangeDeribit != "deribit" {
		t.Error("ExchangeDeribit does not match expected value")
	}
	if ExchangeBinance != "binance" {
		t.Error("ExchangeBinance does not match expected value")
	}
	if ExchangeHyperliquid != "hyperliquid" {
		t.Error("ExchangeHyperliquid does not match expected value")
	}
	if ExchangeMock != "mock" {
		t.Error("ExchangeMock does not match expected value")
	}
}