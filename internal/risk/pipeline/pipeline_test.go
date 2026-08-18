package pipeline

import (
	"testing"

	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
)

func TestRiskPipeline_Run(t *testing.T) {
	pipeline := NewRiskPipeline()
	cfg := &config.Config{Rules: []risk.Rule{{ID: 1, UserStrategyID: 1000, ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Status: "active", Action: "reduce"}}}
	state := &risk.GlobalState{Version: 1, Positions: []*risk.UserPosition{{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, Quantity: 1.0, TotalMargin: 50000.0, Leverage: 10, CurrentPrice: 45000.0}}, Snapshot: &risk.MarketSnapshot{Prices: map[string]map[string]float64{"binance": {"BTCUSDT": 45000.0}}}}
	results := pipeline.Run(state, cfg)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestRiskPipeline_BuildContext(t *testing.T) {
	pipeline := NewRiskPipeline()
	pos := &risk.UserPosition{UserStrategyID: 1000, Symbol: "BTCUSDT", Quantity: 1.0, TotalMargin: 50000.0}
	state := &risk.GlobalState{Positions: []*risk.UserPosition{pos}, Snapshot: &risk.MarketSnapshot{Prices: map[string]map[string]float64{"binance": {"BTCUSDT": 50000.0}}}, Metrics: &risk.GlobalMetrics{BTCVolatility: 0.05}}
	ctx := pipeline.BuildContext(pos, state)
	if ctx == nil || ctx.Position == nil || ctx.Market == nil {
		t.Error("expected non-nil context with Position and Market")
	}
}

func TestRiskPipeline_CalculateMetrics(t *testing.T) {
	pipeline := NewRiskPipeline()
	pos := &risk.UserPosition{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, PosType: risk.PosTypeFutures, Quantity: 1.0, TotalMargin: 50000.0, Leverage: 10, CurrentPrice: 50000.0}
	state := &risk.GlobalState{Positions: []*risk.UserPosition{pos}, Snapshot: &risk.MarketSnapshot{Prices: map[string]map[string]float64{"binance": {"BTCUSDT": 55000.0}}}}
	ctx := pipeline.BuildContext(pos, state)
	if ctx.Local.PnL == 0 {
		t.Error("expected PnL to be calculated")
	}
}

func TestRiskPipeline_MultiplePositions(t *testing.T) {
	pipeline := NewRiskPipeline()
	cfg := &config.Config{Rules: []risk.Rule{{ID: 1, UserStrategyID: 1000, ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Status: "active", Action: "reduce"}, {ID: 2, UserStrategyID: 2000, ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Status: "active", Action: "reduce"}}}
	state := &risk.GlobalState{Positions: []*risk.UserPosition{{UserStrategyID: 1000, Symbol: "BTCUSDT", Quantity: 1.0, TotalMargin: 50000.0, CurrentPrice: 45000.0}, {UserStrategyID: 2000, Symbol: "ETHUSDT", Quantity: 10.0, TotalMargin: 50000.0, CurrentPrice: 45000.0}}, Snapshot: &risk.MarketSnapshot{Prices: map[string]map[string]float64{"binance": {"BTCUSDT": 45000.0, "ETHUSDT": 45000.0}}}}
	results := pipeline.Run(state, cfg)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestRiskPipeline_EmptyPositions(t *testing.T) {
	pipeline := NewRiskPipeline()
	results := pipeline.Run(&risk.GlobalState{Positions: []*risk.UserPosition{}}, &config.Config{})
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestRiskPipeline_ActivePositionsOnly(t *testing.T) {
	pipeline := NewRiskPipeline()
	state := &risk.GlobalState{Positions: []*risk.UserPosition{{UserStrategyID: 1000, Symbol: "BTCUSDT", Deleted: 0, CurrentPrice: 45000.0}, {UserStrategyID: 1001, Symbol: "ETHUSDT", Deleted: 1}}}
	active := pipeline.FilterActivePositions(state)
	if len(active) != 1 {
		t.Errorf("expected 1 active position, got %d", len(active))
	}
}

func TestRiskPipeline_ResultStructure(t *testing.T) {
	pipeline := NewRiskPipeline()
	cfg := &config.Config{Rules: []risk.Rule{{ID: 1, UserStrategyID: 1000, ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Status: "active", Action: "reduce"}}}
	state := &risk.GlobalState{Version: 42, Positions: []*risk.UserPosition{{UserStrategyID: 1000, Symbol: "BTCUSDT", Quantity: 1.0, TotalMargin: 50000.0, CurrentPrice: 45000.0}}, Snapshot: &risk.MarketSnapshot{Prices: map[string]map[string]float64{"binance": {"BTCUSDT": 45000.0}}}}
	results := pipeline.Run(state, cfg)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Version != 42 || results[0].Position == nil || results[0].Context == nil {
		t.Error("expected Version, Position, Context in result")
	}
}

func TestRiskPipeline_SkipsPositionWhenCurrentPriceZero(t *testing.T) {
	pipeline := NewRiskPipeline()
	// Position with CurrentPrice=0 and no snapshot price - should be skipped
	pos := &risk.UserPosition{UserStrategyID: 1000, Symbol: "BTC-31JUL26-66000-P", Exchange: "deribit", Quantity: 1.0, TotalMargin: 50000.0, CurrentPrice: 0, PnL: 0}
	state := &risk.GlobalState{Positions: []*risk.UserPosition{pos}, Snapshot: &risk.MarketSnapshot{Prices: map[string]map[string]float64{}}}
	active := pipeline.FilterActivePositions(state)
	if len(active) != 0 {
		t.Errorf("expected 0 active positions when CurrentPrice=0, got %d", len(active))
	}
}

func TestRiskPipeline_IncludesPositionWhenCurrentPriceAvailable(t *testing.T) {
	pipeline := NewRiskPipeline()
	pos := &risk.UserPosition{UserStrategyID: 1000, Symbol: "BTCUSDT", Exchange: "binance", Quantity: 1.0, TotalMargin: 50000.0, CurrentPrice: 45000.0}
	state := &risk.GlobalState{Positions: []*risk.UserPosition{pos}, Snapshot: &risk.MarketSnapshot{Prices: map[string]map[string]float64{"binance": {"BTCUSDT": 45000.0}}}}
	active := pipeline.FilterActivePositions(state)
	if len(active) != 1 {
		t.Errorf("expected 1 active position when CurrentPrice>0, got %d", len(active))
	}
}

func TestRiskPipeline_NoTriggeredRules(t *testing.T) {
	pipeline := NewRiskPipeline()
	cfg := &config.Config{Rules: []risk.Rule{{ID: 1, UserStrategyID: 1000, ConditionName: "roi", Operator: "<=", Value: -0.02, Sort: 1, Status: "active", Action: "reduce"}}}
	state := &risk.GlobalState{Positions: []*risk.UserPosition{{UserStrategyID: 1000, Symbol: "BTCUSDT", Quantity: 1.0, TotalMargin: 50000.0, Leverage: 10, CurrentPrice: 55000.0}}, Snapshot: &risk.MarketSnapshot{Prices: map[string]map[string]float64{"binance": {"BTCUSDT": 55000.0}}}}
	results := pipeline.Run(state, cfg)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if len(results[0].Rules) != 0 {
		t.Errorf("expected 0 triggered rules, got %d", len(results[0].Rules))
	}
}
