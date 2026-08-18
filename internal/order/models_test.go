package order

import (
	"testing"
	"time"
)

func TestUserFields(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	user := &User{
		Name:      "test_user",
		Exchange:  "binance",
		APIKey:    "key123",
		APISecret: "secret456",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if user.Name != "test_user" {
		t.Errorf("expected Name 'test_user', got '%s'", user.Name)
	}
	if user.Exchange != "binance" {
		t.Errorf("expected Exchange 'binance', got '%s'", user.Exchange)
	}
	if user.APIKey != "key123" {
		t.Errorf("expected APIKey 'key123', got '%s'", user.APIKey)
	}
	if user.ID != 0 {
		t.Errorf("expected zero-value ID, got %d", user.ID)
	}
}

func TestStrategyFields(t *testing.T) {
	s := &Strategy{
		Name:         "ICT_1H_V1_BTCUSDT",
		StrategyType: "FutureCTAV2Strategy",
		Description:  "ICT strategy 1H",
		Params:       `{"StopLossThreshold":-0.1}`,
	}

	if s.Name != "ICT_1H_V1_BTCUSDT" {
		t.Errorf("unexpected name: %s", s.Name)
	}
	if s.StrategyType != "FutureCTAV2Strategy" {
		t.Errorf("unexpected strategy type: %s", s.StrategyType)
	}
	if s.Params != `{"StopLossThreshold":-0.1}` {
		t.Errorf("unexpected params: %s", s.Params)
	}
}

func TestUserStrategyFields(t *testing.T) {
	validBefore := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	us := &UserStrategy{
		UserID:           1,
		Name:             "ICT_1H_V1_BTCUSDT",
		Exchange:         "binance",
		ValidBefore:      validBefore,
		Cash:             1000,
		Parts:            5,
		Status:           1,
		StrategyID:       1,
		RiskStrategyType: "traditional",
		OrdersNum:        0,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if us.Cash != 1000 {
		t.Errorf("expected Cash 1000, got %f", us.Cash)
	}
	if us.Parts != 5 {
		t.Errorf("expected Parts 5, got %d", us.Parts)
	}
	if us.Status != 1 {
		t.Errorf("expected Status 1, got %d", us.Status)
	}
	if us.RiskStrategyType != "traditional" {
		t.Errorf("expected RiskStrategyType 'traditional', got '%s'", us.RiskStrategyType)
	}
}

func TestUserOrderFields(t *testing.T) {
	o := &UserOrder{
		UserID:         1,
		UserStrategyID: 1,
		PosType:        PosTypeFutures,
		Exchange:       "binance",
		BaseAsset:      "BTC",
		QuoteAsset:     "USDT",
		Quantity:       0,
		Cash:           100,
		TriggerPrice:   50000,
		Slippage:       0.01,
		Side:           SideLong,
		OrderType:      0,
		Status:         1,
	}

	if o.ID != 0 {
		t.Errorf("expected zero-value ID, got %d", o.ID)
	}
	if o.Side != SideLong {
		t.Errorf("expected Side Long, got %d", o.Side)
	}
	if o.Status != 1 {
		t.Errorf("expected Status 1 (NEW), got %d", o.Status)
	}
}

func TestLeverageConfigFields(t *testing.T) {
	lc := &LeverageConfig{
		UserID:   1,
		Asset:    "BTC",
		Quote:    "USDT",
		Leverage: 10,
		Exchange: "binance",
		Status:   1,
		PosType:  PosTypeFutures,
	}

	if lc.Leverage != 10 {
		t.Errorf("expected Leverage 10, got %d", lc.Leverage)
	}
	if lc.Asset != "BTC" {
		t.Errorf("expected Asset 'BTC', got '%s'", lc.Asset)
	}
}

func TestUserOrderPositionNilCloseTime(t *testing.T) {
	activePos := &UserOrderPosition{
		Deleted: 0,
	}
	if activePos.CloseTime != nil {
		t.Error("expected nil CloseTime for active position")
	}
}

func TestUserOrderPositionWithCloseTime(t *testing.T) {
	closeTime := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	pos := &UserOrderPosition{
		UserID:       1,
		Exchange:     "binance",
		PosType:      PosTypeFutures,
		Asset:        "BTC",
		CurrentPrice: 50000,
		Quantity:     0.1,
		PosValue:     5000,
		Leverage:     10,
		Deleted:      1,
		InitMargin:   500,
		PosPrice:     50000,
		PnLValue:     0,
		Side:         SideLong,
		CloseTime:    &closeTime,
	}

	if pos.Deleted != 1 {
		t.Errorf("expected Deleted 1, got %d", pos.Deleted)
	}
	if pos.Side != SideLong {
		t.Errorf("expected Side Long, got %d", pos.Side)
	}
	if pos.CloseTime == nil {
		t.Fatal("expected CloseTime to be set")
	}
	if !pos.CloseTime.Equal(closeTime) {
		t.Errorf("expected CloseTime %v, got %v", closeTime, pos.CloseTime)
	}
}

func TestRiskControlStrategyFields(t *testing.T) {
	risk := &RiskControlStrategy{
		UserID:         1,
		Symbol:         "BTCUSDT",
		RiskType:       "CloseAllPosition",
		OrderType:      1,
		Threshold:      0,
		Price:          0,
		Status:         0,
		PosType:        PosTypeFutures,
		Side:           SideLong,
		UserStrategyID: 1,
	}

	if risk.RiskType != "CloseAllPosition" {
		t.Errorf("expected RiskType 'CloseAllPosition', got '%s'", risk.RiskType)
	}
	if risk.Status != 0 {
		t.Errorf("expected Status 0 (not executed), got %d", risk.Status)
	}
}

func TestExchangeSymbolFilterFields(t *testing.T) {
	filter := &ExchangeSymbolFilter{
		Exchange:    "binance",
		PosType:     PosTypeFutures,
		Symbol:      "BTCUSDT",
		FilterType:  "PRICE_FILTER",
		MinPrice:    0.1,
		MaxPrice:    1000000,
		TickSize:    0.1,
		MinQty:      0.001,
		MaxQty:      1000,
		StepSize:    0.001,
		MinNotional: 5,
	}

	if filter.Symbol != "BTCUSDT" {
		t.Errorf("expected Symbol 'BTCUSDT', got '%s'", filter.Symbol)
	}
	if filter.FilterType != "PRICE_FILTER" {
		t.Errorf("expected FilterType 'PRICE_FILTER', got '%s'", filter.FilterType)
	}
}

func TestRelationTypeConstants(t *testing.T) {
	if RelationTypeUserOrders != "user_orders" {
		t.Errorf("expected RelationTypeUserOrders user_orders, got %s", RelationTypeUserOrders)
	}
	if RelationTypeRiskControlStrategy != "risk_control_strategy" {
		t.Errorf("expected RelationTypeRiskControlStrategy risk_control_strategy, got %s", RelationTypeRiskControlStrategy)
	}
}

func TestStrategyAssetFields(t *testing.T) {
	sa := &StrategyAsset{
		Name:       "ICT_1H_V1_BTCUSDT",
		Asset:      "BTC",
		StrategyID: 1,
		PosType:    PosTypeFutures,
		Sort:       1,
	}

	if sa.Asset != "BTC" {
		t.Errorf("expected Asset 'BTC', got '%s'", sa.Asset)
	}
	if sa.StrategyID != 1 {
		t.Errorf("expected StrategyID 1, got %d", sa.StrategyID)
	}
}
