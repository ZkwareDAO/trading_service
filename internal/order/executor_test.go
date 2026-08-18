package order

import (
	"testing"
)

func TestCalculateCashToQuantity(t *testing.T) {
	qty := CalculateCashToQuantity(100, 50000, 10)
	// cash=100, leverage=10, price=50000 => qty = 100*10/50000 = 0.02
	if qty != 0.02 {
		t.Errorf("expected 0.02, got %f", qty)
	}
}

func TestCalculateCashToQuantity_ZeroPrice(t *testing.T) {
	qty := CalculateCashToQuantity(100, 0, 10)
	if qty != 0 {
		t.Errorf("expected 0 for zero price, got %f", qty)
	}
}

func TestCalculateCashToQuantity_ZeroLeverage(t *testing.T) {
	qty := CalculateCashToQuantity(100, 50000, 0)
	if qty != 0 {
		t.Errorf("expected 0 for zero leverage, got %f", qty)
	}
}

func TestCalculateCashToQuantity_SpotLeverage1(t *testing.T) {
	qty := CalculateCashToQuantity(100, 50000, 1)
	// cash=100, leverage=1, price=50000 => qty = 100*1/50000 = 0.002
	if qty != 0.002 {
		t.Errorf("expected 0.002, got %f", qty)
	}
}

func TestVerifyCreateOrderInfo(t *testing.T) {
	tests := []struct {
		name    string
		req     CreateOrderRequest
		wantErr bool
	}{
		{
			"valid limit order",
			CreateOrderRequest{
				OrderType: 0, // 0 = limit
				Price:     50000,
				Quantity:  0.01,
			},
			false,
		},
		{
			"valid market order",
			CreateOrderRequest{
				OrderType: 1, // 1 = market
				Quantity:  0.01,
			},
			false,
		},
		{
			"limit order missing price",
			CreateOrderRequest{
				OrderType: 0,
				Price:     0,
				Quantity:  0.01,
			},
			true,
		},
		{
			"zero quantity",
			CreateOrderRequest{
				OrderType: 0,
				Price:     50000,
				Quantity:  0,
			},
			true,
		},
		{
			"negative quantity",
			CreateOrderRequest{
				OrderType: 0,
				Price:     50000,
				Quantity:  -0.01,
			},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyCreateOrderInfo(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyCreateOrderInfo() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestApplySlippage(t *testing.T) {
	// Buy side: add slippage (higher price)
	price := ApplySlippage(50000, 0.01, SideLong)
	expected := 50000.0 * 1.01 // 50500
	if price != expected {
		t.Errorf("expected %f, got %f", expected, price)
	}

	// Sell side: subtract slippage (lower price)
	price = ApplySlippage(50000, 0.01, SideShort)
	expected = 50000.0 * 0.99 // 49500
	if price != expected {
		t.Errorf("expected %f, got %f", expected, price)
	}
}

func TestCalculatePnL(t *testing.T) {
	// Long position: price goes up
	pnl := CalculatePnL(50000, 55000, 0.1, SideLong)
	expected := (55000 - 50000) * 0.1 // 500
	if pnl != expected {
		t.Errorf("expected %f, got %f", expected, pnl)
	}

	// Short position: price goes down (profit)
	pnl = CalculatePnL(50000, 45000, 0.1, SideShort)
	expected = (50000 - 45000) * 0.1 // 500
	if pnl != expected {
		t.Errorf("expected %f, got %f", expected, pnl)
	}
}
