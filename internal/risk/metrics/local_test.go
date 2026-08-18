package metrics

import (
	"math"
	"testing"

	"trading-service/internal/risk"
)

// Test 1: LocalMetricsBuilder 应该计算正确的 ROI
func TestLocalMetricsBuilder_ROI(t *testing.T) {
	builder := NewLocalMetricsBuilder()

	pos := &risk.UserPosition{
		TotalMargin: 10000.0,
		Leverage:    10,
	}

	// 假设 PnL = 1500, TotalMargin = 10000, Leverage = 10
	// ROI = PnL / TotalMargin * Leverage = 1500 / 10000 * 10 = 1.5 (150%)
	metrics := builder.Build(pos, 1500.0)

	expectedROI := 1500.0 / 10000.0 * 10.0
	if math.Abs(metrics.ROI-expectedROI) > 0.0001 {
		t.Errorf("expected ROI %f, got %f", expectedROI, metrics.ROI)
	}
}

// Test 2: 计算 PnL - 现货（价格增值）
func TestLocalMetricsBuilder_PnL_Spot(t *testing.T) {
	builder := NewLocalMetricsBuilder()

	pos := &risk.UserPosition{
		PosType:  risk.PosTypeSpot,
		Side:    risk.SideLong,
		Quantity: 1.0,
	}

	// 现货: PnL = CurrentPrice * Quantity - EntryPrice * Quantity
	// EntryPrice = 50000, CurrentPrice = 55000, Quantity = 1.0
	// PnL = 55000 * 1.0 - 50000 * 1.0 = 5000
	entryPrice := 50000.0
	currentPrice := 55000.0

	pnl := builder.CalculatePnL(pos, entryPrice, currentPrice)
	expectedPnL := 55000.0 - 50000.0

	if math.Abs(pnl-expectedPnL) > 0.01 {
		t.Errorf("expected PnL %f, got %f", expectedPnL, pnl)
	}
}

// Test 3: 计算 PnL - 合约多单（价格上涨盈利）
func TestLocalMetricsBuilder_PnL_FuturesLong(t *testing.T) {
	builder := NewLocalMetricsBuilder()

	pos := &risk.UserPosition{
		PosType:  risk.PosTypeFutures,
		Side:     risk.SideLong,
		Quantity: 1.0,
	}

	// 合约多单: PnL = (CurrentPrice - EntryPrice) * Quantity
	// EntryPrice = 50000, CurrentPrice = 55000, Quantity = 1.0
	// PnL = (55000 - 50000) * 1.0 = 5000
	entryPrice := 50000.0
	currentPrice := 55000.0

	pnl := builder.CalculatePnL(pos, entryPrice, currentPrice)
	expectedPnL := (55000.0 - 50000.0) * 1.0

	if math.Abs(pnl-expectedPnL) > 0.01 {
		t.Errorf("expected PnL %f, got %f", expectedPnL, pnl)
	}
}

// Test 4: 计算 PnL - 合约空单（价格下跌盈利）
func TestLocalMetricsBuilder_PnL_FuturesShort(t *testing.T) {
	builder := NewLocalMetricsBuilder()

	pos := &risk.UserPosition{
		PosType:  risk.PosTypeFutures,
		Side:     risk.SideShort,
		Quantity: 1.0,
	}

	// 合约空单: PnL = (EntryPrice - CurrentPrice) * Quantity
	// EntryPrice = 50000, CurrentPrice = 45000, Quantity = 1.0
	// PnL = (50000 - 45000) * 1.0 = 5000
	entryPrice := 50000.0
	currentPrice := 45000.0

	pnl := builder.CalculatePnL(pos, entryPrice, currentPrice)
	expectedPnL := (50000.0 - 45000.0) * 1.0

	if math.Abs(pnl-expectedPnL) > 0.01 {
		t.Errorf("expected PnL %f, got %f", expectedPnL, pnl)
	}
}

// Test 5: 计算 MaxProfit 和 MaxDrawdown
func TestLocalMetricsBuilder_MaxProfitDrawdown(t *testing.T) {
	builder := NewLocalMetricsBuilder()

	pos := &risk.UserPosition{
		TotalMargin: 10000.0,
		Leverage:    10,
	}

	// 模拟一系列 ROI 变化
	roiHistory := []float64{0.05, 0.10, 0.15, 0.12, 0.08, -0.02, -0.05}

	var maxProfit, maxDrawdown float64
	for _, roi := range roiHistory {
		metrics := builder.BuildWithHistory(pos, roi, maxProfit, maxDrawdown)
		if metrics.MaxProfitPct > maxProfit {
			maxProfit = metrics.MaxProfitPct
		}
		if metrics.MaxDrawdownPct < maxDrawdown {
			maxDrawdown = metrics.MaxDrawdownPct
		}
	}

	// 最大盈利应该是 0.15，最大亏损应该是 -0.05
	if math.Abs(maxProfit-0.15) > 0.0001 {
		t.Errorf("expected MaxProfit 0.15, got %f", maxProfit)
	}
	if math.Abs(maxDrawdown-(-0.05)) > 0.0001 {
		t.Errorf("expected MaxDrawdown -0.05, got %f", maxDrawdown)
	}
}

// Test 6: 计算 UnrealizedPnL 和 RealizedPnL
func TestLocalMetricsBuilder_UnrealizedRealized(t *testing.T) {
	builder := NewLocalMetricsBuilder()

	pos := &risk.UserPosition{
		PosType:     risk.PosTypeFutures,
		Side:        risk.SideLong,
		Quantity:    1.0,
		TotalMargin: 50000.0,
	}

	entryPrice := 50000.0
	currentPrice := 55000.0

	metrics := builder.BuildWithPrices(pos, entryPrice, currentPrice)

	// UnrealizedPnL = (CurrentPrice - EntryPrice) * Quantity = 5000
	expectedUnrealized := (55000.0 - 50000.0) * 1.0
	if math.Abs(metrics.UnrealizedPnL-expectedUnrealized) > 0.01 {
		t.Errorf("expected UnrealizedPnL %f, got %f", expectedUnrealized, metrics.UnrealizedPnL)
	}
}

// Test 7: 计算 DurationSec
func TestLocalMetricsBuilder_Duration(t *testing.T) {
	builder := NewLocalMetricsBuilder()

	pos := &risk.UserPosition{}

	// 持续时间 = 当前时间 - 入场时间
	openTime := int64(3600) // 1小时前
	metrics := builder.BuildWithDuration(pos, openTime)

	if metrics.DurationSec != 3600 {
		t.Errorf("expected DurationSec 3600, got %d", metrics.DurationSec)
	}
}

// Test 8: Build 应该返回完整的 LocalMetrics
func TestLocalMetricsBuilder_Build(t *testing.T) {
	builder := NewLocalMetricsBuilder()

	pos := &risk.UserPosition{
		Quantity:     1.0,
		TotalMargin:  50000.0,
		Leverage:     10,
		CurrentPrice: 55000.0,
	}

	pnl := 5000.0
	metrics := builder.Build(pos, pnl)

	if metrics.PnL != pnl {
		t.Errorf("expected PnL %f, got %f", pnl, metrics.PnL)
	}
	if metrics.MarkPrice != 55000.0 {
		t.Errorf("expected MarkPrice 55000.0, got %f", metrics.MarkPrice)
	}
}

// Test 9: 处理零值 TotalMargin（避免除零）
func TestLocalMetricsBuilder_ZeroMargin(t *testing.T) {
	builder := NewLocalMetricsBuilder()

	pos := &risk.UserPosition{
		TotalMargin: 0,
		Leverage:    10,
	}

	// 应该返回 ROI = 0 而不是 panic
	metrics := builder.Build(pos, 1000.0)

	if metrics.ROI != 0 {
		t.Errorf("expected ROI 0 for zero margin, got %f", metrics.ROI)
	}
}

// Test 10: 计算 MarkPrice (CurrentPrice)
func TestLocalMetricsBuilder_MarkPrice(t *testing.T) {
	builder := NewLocalMetricsBuilder()

	pos := &risk.UserPosition{
		CurrentPrice: 55000.0,
	}

	metrics := builder.Build(pos, 1000.0)

	if metrics.MarkPrice != 55000.0 {
		t.Errorf("expected MarkPrice 55000.0, got %f", metrics.MarkPrice)
	}
}