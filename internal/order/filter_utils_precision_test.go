package order

import (
	"math"
	"testing"
)

func TestTruncateToDecimalPlaces_FloatingPointPrecision(t *testing.T) {
	tests := []struct {
		name          string
		input         float64
		decimalPlaces int
		want          float64
		description   string
	}{
		{
			name:          "HYPEUSDT_148.64_should_not_truncate_to_148.63",
			input:         148.64,
			decimalPlaces: 2,
			want:          148.64,
			description:   "Bug case: 148.64 should stay 148.64, not become 148.63 due to float precision",
		},
		{
			name:          "HYPEUSDT_73.57_should_not_truncate_to_73.56",
			input:         73.57,
			decimalPlaces: 2,
			want:          73.57,
			description:   "Bug case: 73.57 should stay 73.57, not become 73.56",
		},
		{
			name:          "SOLUSDT_32.73_should_not_truncate_to_32.72",
			input:         32.73,
			decimalPlaces: 2,
			want:          32.73,
			description:   "Bug case: 32.73 should stay 32.73, not become 32.72",
		},
		{
			name:          "HYPEUSDT_69.59_normal_case",
			input:         69.59,
			decimalPlaces: 2,
			want:          69.59,
			description:   "Normal case: 69.59 works correctly",
		},
		{
			name:          "SOLUSDT_32.57_normal_case",
			input:         32.57,
			decimalPlaces: 2,
			want:          32.57,
			description:   "Normal case: 32.57 works correctly",
		},
		{
			name:          "truncate_3_decimal_places",
			input:         2.747,
			decimalPlaces: 3,
			want:          2.747,
			description:   "ETHUSDT case: 3 decimal places should work",
		},
		{
			name:          "truncate_4_decimal_places",
			input:         0.0381,
			decimalPlaces: 4,
			want:          0.0381,
			description:   "BTCUSDT case: 4 decimal places should work",
		},
		{
			name:          "already_truncated",
			input:         100.50,
			decimalPlaces: 2,
			want:          100.50,
			description:   "Already truncated value should stay same",
		},
		{
			name:          "need_actual_truncation",
			input:         100.999,
			decimalPlaces: 2,
			want:          100.99,
			description:   "Value needing truncation should be truncated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateToDecimalPlaces(tt.input, tt.decimalPlaces)
			
			// Use tolerance for floating-point comparison
			diff := math.Abs(got - tt.want)
			tolerance := 1e-10
			
			if diff > tolerance {
				t.Errorf("TruncateToDecimalPlaces(%.15f, %d) = %.15f, want %.15f (diff=%.15f)\n%s",
					tt.input, tt.decimalPlaces, got, tt.want, diff, tt.description)
			}
		})
	}
}

func TestTruncateToDecimalPlaces_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		input         float64
		decimalPlaces int
		want          float64
	}{
		{"zero", 0.0, 2, 0.0},
		{"negative", -148.64, 2, -148.64},
		{"very_small", 0.0000001, 2, 0.0},
		{"very_large", 999999999.99, 2, 999999999.99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateToDecimalPlaces(tt.input, tt.decimalPlaces)
			diff := math.Abs(got - tt.want)
			if diff > 1e-10 {
				t.Errorf("TruncateToDecimalPlaces(%f, %d) = %f, want %f",
					tt.input, tt.decimalPlaces, got, tt.want)
			}
		})
	}
}
