package metrics

import (
	"math"

	"trading-service/internal/risk"
)

// LocalMetricsBuilder 计算局部指标
type LocalMetricsBuilder struct{}

// NewLocalMetricsBuilder 创建指标计算器
func NewLocalMetricsBuilder() *LocalMetricsBuilder {
	return &LocalMetricsBuilder{}
}

// Build 构建 LocalMetrics
func (b *LocalMetricsBuilder) Build(pos *risk.UserPosition, pnl float64) risk.LocalMetrics {
	roi := b.calculateROI(pnl, pos.TotalMargin, pos.Leverage)

	return risk.LocalMetrics{
		ROI:        roi,
		PnL:        pnl,
		MarkPrice:  pos.CurrentPrice,
		EntryPrice: pos.CurrentPrice, // 简化处理，实际应该从仓位获取
	}
}

// BuildWithHistory 构建带历史记录的 LocalMetrics
func (b *LocalMetricsBuilder) BuildWithHistory(pos *risk.UserPosition, currentROI, maxProfit, maxDrawdown float64) risk.LocalMetrics {
	// 更新最大盈利和最大亏损
	if currentROI > maxProfit {
		maxProfit = currentROI
	}
	if currentROI < maxDrawdown {
		maxDrawdown = currentROI
	}

	return risk.LocalMetrics{
		ROI:            currentROI,
		MaxProfitPct:   maxProfit,
		MaxDrawdownPct: maxDrawdown,
		MarkPrice:      pos.CurrentPrice,
	}
}

// BuildWithPrices 根据入场价和当前价构建 LocalMetrics
func (b *LocalMetricsBuilder) BuildWithPrices(pos *risk.UserPosition, entryPrice, currentPrice float64) risk.LocalMetrics {
	unrealizedPnL := b.CalculatePnL(pos, entryPrice, currentPrice)
	roi := b.calculateROI(unrealizedPnL, pos.TotalMargin, pos.Leverage)

	return risk.LocalMetrics{
		ROI:            roi,
		PnL:            unrealizedPnL,
		UnrealizedPnL:  unrealizedPnL,
		EntryPrice:     entryPrice,
		MarkPrice:      currentPrice,
		MaxProfitPct:   0,
		MaxDrawdownPct: 0,
	}
}

// BuildWithDuration 构建带持续时间的 LocalMetrics
func (b *LocalMetricsBuilder) BuildWithDuration(pos *risk.UserPosition, openDurationSec int64) risk.LocalMetrics {
	return risk.LocalMetrics{
		DurationSec: openDurationSec,
		MarkPrice:   pos.CurrentPrice,
	}
}

// CalculatePnL 计算 PnL
// 根据 PosType 和 Side 使用不同公式
func (b *LocalMetricsBuilder) CalculatePnL(pos *risk.UserPosition, entryPrice, currentPrice float64) float64 {
	switch pos.PosType {
	case risk.PosTypeSpot:
		// 现货: PnL = CurrentValue - EntryValue
		return currentPrice*pos.Quantity - entryPrice*pos.Quantity

	case risk.PosTypeFutures:
		if pos.Side == risk.SideLong {
			// 合约多单: PnL = (CurrentPrice - EntryPrice) * Quantity
			return (currentPrice - entryPrice) * pos.Quantity
		} else {
			// 合约空单: PnL = (EntryPrice - CurrentPrice) * Quantity
			return (entryPrice - currentPrice) * pos.Quantity
		}

	default:
		return 0
	}
}

// calculateROI 计算 ROI
// ROI = PnL / TotalMargin * Leverage
func (b *LocalMetricsBuilder) calculateROI(pnl, totalMargin float64, leverage int) float64 {
	if totalMargin == 0 {
		return 0
	}
	return roundTo8Decimal(pnl / totalMargin * float64(leverage))
}

// roundTo8Decimal 四舍五入到8位小数
func roundTo8Decimal(value float64) float64 {
	return math.Round(value*1e8) / 1e8
}