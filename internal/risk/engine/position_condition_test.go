package engine

import (
	"testing"

	"trading-service/internal/risk"
)

func TestIsValidPositionSymbol(t *testing.T) {
	tests := []struct {
		name   string
		symbol string
		want   bool
	}{
		// 现货格式 - USDT
		{"spot_usdt_btc", "BTCUSDT", true},
		{"spot_usdt_eth", "ETHUSDT", true},
		{"spot_usdt_sol", "SOLUSDT", true},
		{"spot_usdt_sui", "SUIUSDT", true},

		// 现货格式 - USDC
		{"spot_usdc_btc", "BTCUSDC", true},
		{"spot_usdc_eth", "ETHUSDC", true},

		// 期权格式 - Deribit
		{"option_btc_call", "BTC-7AUG26-64000-P", true},
		{"option_eth_call", "ETH-25DEC26-2400-C", true},
		{"option_btc_put", "BTC-28MAR27-50000-P", true},

		// 期权格式 - 小写输入也应通过（大小写不敏感）
		{"option_lowercase_type", "BTC-7AUG26-64000-c", true},
		{"option_lowercase_underlying", "btc-7AUG26-64000-C", true},
		{"option_all_lowercase", "btc-7aug26-64000-c", true},
		{"spot_lowercase", "btcusdt", true},

		// 无效格式
		{"invalid_empty", "", false},
		{"invalid_no_suffix", "BTC", false},
		{"invalid_wrong_suffix", "BTCUSD", false},
		{"invalid_option_missing_parts", "BTC-7AUG26", false},
		{"invalid_option_wrong_month", "BTC-7XXX26-64000-P", false},
		{"invalid_option_wrong_type", "BTC-7AUG26-64000-X", false},
		{"invalid_usdt_too_short", "USDT", false},
		{"invalid_usdc_too_short", "USDC", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidPositionSymbol(tt.symbol); got != tt.want {
				t.Errorf("IsValidPositionSymbol(%q) = %v, want %v", tt.symbol, got, tt.want)
			}
		})
	}
}

func TestNormalizePositionSymbol(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// 期权 - 小写应规范化为大写
		{"option_lowercase_type", "btc-7aug26-64000-c", "BTC-7AUG26-64000-C"},
		{"option_mixed_case", "Btc-7Aug26-64000-c", "BTC-7AUG26-64000-C"},
		{"option_already_upper", "BTC-7AUG26-64000-C", "BTC-7AUG26-64000-C"},
		{"option_put_lowercase", "eth-25dec26-2400-p", "ETH-25DEC26-2400-P"},

		// 现货 - 小写应规范化为大写
		{"spot_lowercase", "btcusdt", "BTCUSDT"},
		{"spot_already_upper", "ETHUSDC", "ETHUSDC"},

		// 无效格式 - 原样返回
		{"invalid_empty", "", ""},
		{"invalid_format", "INVALID", "INVALID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizePositionSymbol(tt.input); got != tt.want {
				t.Errorf("NormalizePositionSymbol(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAllDigits(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"1", true},
		{"12", true},
		{"123", true},
		{"", true}, // empty string has no non-digits
		{"1a", false},
		{"a1", false},
		{"abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			if got := allDigits(tt.s); got != tt.want {
				t.Errorf("allDigits(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestEvaluatePositionCondition_InvalidSymbol(t *testing.T) {
	// Test case: invalid symbol format should return false
	engine := NewRuleEngine()

	rule := risk.Rule{
		ConditionName: "position_INVALID",
		Operator:      "==",
		Value:         float64(0),
	}

	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{
			UserID: 13,
		},
	}

	if engine.EvaluateCondition(rule, ctx) {
		t.Error("expected condition NOT to trigger for invalid symbol")
	}
}