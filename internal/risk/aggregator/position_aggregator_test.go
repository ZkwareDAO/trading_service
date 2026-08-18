package aggregator

import (
	"testing"
	"time"

	"trading-service/internal/risk"
)

// Test 1: PositionAggregator 应该能够聚合订单仓位到策略仓位
func TestPositionAggregator_BasicAggregation(t *testing.T) {
	aggregator := NewPositionAggregator()

	orderPositions := []risk.UserOrderPosition{
		{
			ID:             1,
			UserID:         100,
			UserStrategyID: 1000,
			Exchange:       "binance",
			PosType:        risk.PosTypeFutures,
			Symbol:         "BTCUSDT",
			Side:           risk.SideLong,
			Quantity:       0.5,
			PosPrice:       49000.0,
			Leverage:       10,
			InitMargin:     2450.0,
			CurrentPrice:   50000.0,
			Deleted:        0,
		},
		{
			ID:             2,
			UserID:         100,
			UserStrategyID: 1000,
			Exchange:       "binance",
			PosType:        risk.PosTypeFutures,
			Symbol:         "BTCUSDT",
			Side:           risk.SideLong,
			Quantity:       0.3,
			PosPrice:       48000.0,
			Leverage:       10,
			InitMargin:     1440.0,
			CurrentPrice:   50000.0,
			Deleted:        0,
		},
	}

	positions := aggregator.Aggregate(orderPositions)

	if len(positions) != 1 {
		t.Errorf("expected 1 position, got %d", len(positions))
	}

	pos := positions[0]
	if pos.UserStrategyID != 1000 {
		t.Errorf("expected UserStrategyID 1000, got %d", pos.UserStrategyID)
	}
	if pos.Symbol != "BTCUSDT" {
		t.Errorf("expected Symbol BTCUSDT, got %s", pos.Symbol)
	}
	if pos.Side != risk.SideLong {
		t.Errorf("expected Side Long, got %d", pos.Side)
	}
}

// Test 2: 聚合应该正确计算 Quantity（累加）
func TestPositionAggregator_QuantitySum(t *testing.T) {
	aggregator := NewPositionAggregator()

	orderPositions := []risk.UserOrderPosition{
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, Quantity: 0.5, CurrentPrice: 50000.0},
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, Quantity: 0.3, CurrentPrice: 50000.0},
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, Quantity: 0.2, CurrentPrice: 50000.0},
	}

	positions := aggregator.Aggregate(orderPositions)

	expectedQty := 0.5 + 0.3 + 0.2
	if positions[0].Quantity != expectedQty {
		t.Errorf("expected Quantity %f, got %f", expectedQty, positions[0].Quantity)
	}
}

// Test 3: 聚合应该正确计算 TotalMargin（累加）
func TestPositionAggregator_MarginSum(t *testing.T) {
	aggregator := NewPositionAggregator()

	orderPositions := []risk.UserOrderPosition{
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, InitMargin: 2450.0, Leverage: 10},
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, InitMargin: 1440.0, Leverage: 10},
	}

	positions := aggregator.Aggregate(orderPositions)

	expectedMargin := 2450.0 + 1440.0
	if positions[0].TotalMargin != expectedMargin {
		t.Errorf("expected TotalMargin %f, got %f", expectedMargin, positions[0].TotalMargin)
	}
}

// Test 4: 聚合应该按 UserStrategyID + Symbol + Side 分组
func TestPositionAggregator_Grouping(t *testing.T) {
	aggregator := NewPositionAggregator()

	orderPositions := []risk.UserOrderPosition{
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, Quantity: 0.5},
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideShort, Quantity: 0.3},
		{UserStrategyID: 1000, Symbol: "ETHUSDT", Side: risk.SideLong, Quantity: 2.0},
		{UserStrategyID: 2000, Symbol: "BTCUSDT", Side: risk.SideLong, Quantity: 1.0},
	}

	positions := aggregator.Aggregate(orderPositions)

	// 应该产生 4 个不同的策略仓位
	if len(positions) != 4 {
		t.Errorf("expected 4 positions (different groups), got %d", len(positions))
	}
}

// Test 5: 聚合应该正确设置 CurrentPrice
func TestPositionAggregator_CurrentPrice(t *testing.T) {
	aggregator := NewPositionAggregator()

	orderPositions := []risk.UserOrderPosition{
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, CurrentPrice: 50000.0, Quantity: 0.5},
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, CurrentPrice: 50000.0, Quantity: 0.3},
	}

	positions := aggregator.Aggregate(orderPositions)

	if positions[0].CurrentPrice != 50000.0 {
		t.Errorf("expected CurrentPrice 50000.0, got %f", positions[0].CurrentPrice)
	}
}

// Test 6: 聚合应该正确处理空输入
func TestPositionAggregator_EmptyInput(t *testing.T) {
	aggregator := NewPositionAggregator()

	positions := aggregator.Aggregate([]risk.UserOrderPosition{})

	if len(positions) != 0 {
		t.Errorf("expected 0 positions for empty input, got %d", len(positions))
	}
}

// Test 7: 聚合应该设置正确的时间戳
func TestPositionAggregator_Timestamps(t *testing.T) {
	aggregator := NewPositionAggregator()

	now := time.Now()
	orderPositions := []risk.UserOrderPosition{
		{
			UserStrategyID: 1000,
			Symbol:         "BTCUSDT",
			Side:           risk.SideLong,
			Quantity:       0.5,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}

	positions := aggregator.Aggregate(orderPositions)

	// CloseTime 应该设置为未来时间（默认100年后）
	if positions[0].CloseTime == nil {
		t.Error("expected CloseTime to be set")
	}
	if positions[0].CloseTime.Before(now) {
		t.Error("expected CloseTime to be in the future")
	}
}

// Test 8: 聚合应该正确处理 Deleted 状态（只聚合活跃仓位）
func TestPositionAggregator_FilterDeleted(t *testing.T) {
	aggregator := NewPositionAggregator()

	orderPositions := []risk.UserOrderPosition{
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, Quantity: 0.5, Deleted: 0},
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, Quantity: 0.3, Deleted: 1}, // 已平仓
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, Quantity: 0.2, Deleted: 0},
	}

	positions := aggregator.Aggregate(orderPositions)

	// 只聚合 Deleted=0 的仓位
	expectedQty := 0.5 + 0.2
	if positions[0].Quantity != expectedQty {
		t.Errorf("expected Quantity %f (excluding deleted), got %f", expectedQty, positions[0].Quantity)
	}
}

// Test 9: 聚合应该继承 Exchange 和 PosType
func TestPositionAggregator_InheritFields(t *testing.T) {
	aggregator := NewPositionAggregator()

	orderPositions := []risk.UserOrderPosition{
		{
			UserStrategyID: 1000,
			UserID:         100,
			Exchange:       "binance",
			PosType:        risk.PosTypeFutures,
			Symbol:         "BTCUSDT",
			Side:           risk.SideLong,
			Quantity:       0.5,
		},
	}

	positions := aggregator.Aggregate(orderPositions)

	if positions[0].UserID != 100 {
		t.Errorf("expected UserID 100, got %d", positions[0].UserID)
	}
	if positions[0].Exchange != "binance" {
		t.Errorf("expected Exchange binance, got %s", positions[0].Exchange)
	}
	if positions[0].PosType != risk.PosTypeFutures {
		t.Errorf("expected PosType Futures, got %d", positions[0].PosType)
	}
}

// Test 10: 聚合应该正确处理不同 Leverage（取第一个）
func TestPositionAggregator_Leverage(t *testing.T) {
	aggregator := NewPositionAggregator()

	orderPositions := []risk.UserOrderPosition{
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, Leverage: 10, Quantity: 0.5},
		{UserStrategyID: 1000, Symbol: "BTCUSDT", Side: risk.SideLong, Leverage: 5, Quantity: 0.3},
	}

	positions := aggregator.Aggregate(orderPositions)

	// Leverage 取第一个订单的值
	if positions[0].Leverage != 10 {
		t.Errorf("expected Leverage 10 (first order), got %d", positions[0].Leverage)
	}
}
