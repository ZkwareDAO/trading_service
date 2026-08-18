package signal

import (
	"encoding/json"
	"testing"

	"trading-service/internal/order"
)

func TestStrategySignal_NormalizeSymbolAndStrategyName(t *testing.T) {
	payload := []byte(`{
		"SignalID":"id-1",
		"SignalTimestamp":"2026-04-15 12:16:05",
		"symbol":"BTCUSDT",
		"pos_type":2,
		"strategy_type":"CTAFutureFactory",
		"risk_strategy_type":"traditional",
		"strategy":{"name":"OBVATRV2","leverage":2,"version":"2","internal":"4h","valid_before":"2030-12-31 08:00:00","cash":1000,"parts":1},
		"user_id":1,
		"signal":{"action":"buy","exchange":"binance","valid_before":"2030-06-30 20:16:05","quantity":null,"cash":100,"trigger_price":87315.1,"slippage":0,"order_type":1}
	}`)

	var msg StrategySignal
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("unmarshal strategy signal: %v", err)
	}
	msg.NormalizeSymbol()

	if msg.Symbol != "BTC" {
		t.Fatalf("expected normalized symbol BTC, got %s", msg.Symbol)
	}
	if msg.Signal.Quantity != 0 {
		t.Fatalf("expected null quantity to decode as zero, got %f", msg.Signal.Quantity)
	}
	if got := msg.Strategy.ExtractStrategyName(msg.Symbol, msg.Signal.Exchange); got != "OBVATRV2_4H_2_BTCUSDT" {
		t.Fatalf("unexpected strategy name: %s", got)
	}
}

func TestStrategySignal_ExtractStrategyNameUsesExchangeQuote(t *testing.T) {
	strategy := StrategyConfig{Name: "OBVATR", Version: "2", Interval: "4h"}

	if got := strategy.ExtractStrategyName("NEARUSDC", "hyperliquid"); got != "OBVATR_4H_2_NEARUSDC" {
		t.Fatalf("expected hyperliquid strategy name with USDC quote, got %s", got)
	}
	if got := strategy.ExtractStrategyName("NEARUSDC", "binance"); got != "OBVATR_4H_2_NEARUSDT" {
		t.Fatalf("expected binance strategy name with USDT quote, got %s", got)
	}
	if got := strategy.ExtractStrategyName("NEARUSDC", "other_exchange"); got != "OBVATR_4H_2_NEARUSDC" {
		t.Fatalf("expected other exchange strategy name to preserve signal symbol, got %s", got)
	}
}

func TestActionSideMapping(t *testing.T) {
	tests := []struct {
		action    Action
		openSide  order.Side
		closeSide order.Side
		hasOpen   bool
		hasClose  bool
	}{
		{action: ActionBuy, openSide: order.SideLong, hasOpen: true},
		{action: ActionSell, openSide: order.SideShort, hasOpen: true},
		{action: ActionBuyClose, closeSide: order.SideShort, hasClose: true},
		{action: ActionSellClose, closeSide: order.SideLong, hasClose: true},
		{action: ActionReverseLong, openSide: order.SideLong, closeSide: order.SideShort, hasOpen: true, hasClose: true},
		{action: ActionReverseShort, openSide: order.SideShort, closeSide: order.SideLong, hasOpen: true, hasClose: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			openSide, hasOpen := tt.action.GetOpenSide()
			if hasOpen != tt.hasOpen || (hasOpen && openSide != tt.openSide) {
				t.Fatalf("open side got (%d,%v), want (%d,%v)", openSide, hasOpen, tt.openSide, tt.hasOpen)
			}
			closeSide, hasClose := tt.action.GetCloseSide()
			if hasClose != tt.hasClose || (hasClose && closeSide != tt.closeSide) {
				t.Fatalf("close side got (%d,%v), want (%d,%v)", closeSide, hasClose, tt.closeSide, tt.hasClose)
			}
		})
	}
}
