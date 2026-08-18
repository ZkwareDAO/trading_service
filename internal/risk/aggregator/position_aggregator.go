package aggregator

import (
	"math"
	"time"

	"trading-service/internal/risk"
)

// PositionAggregator 聚合订单仓位到策略仓位
type PositionAggregator struct{}

// NewPositionAggregator 创建聚合器
func NewPositionAggregator() *PositionAggregator {
	return &PositionAggregator{}
}

// Aggregate 将订单层仓位聚合为策略层仓位
// 按 UserStrategyID + Symbol + Side 分组聚合
func (a *PositionAggregator) Aggregate(orderPositions []risk.UserOrderPosition) []*risk.UserPosition {
	if len(orderPositions) == 0 {
		return nil
	}

	// 分组 key: UserStrategyID + Symbol + Side
	type groupKey struct {
		UserStrategyID uint64
		Symbol         string
		Side           risk.Side
	}

	groups := make(map[groupKey][]risk.UserOrderPosition)

	// 按 Deleted=0 过滤并分组
	for _, pos := range orderPositions {
		if pos.Deleted != 0 {
			continue // 跳过已平仓
		}
		key := groupKey{
			UserStrategyID: pos.UserStrategyID,
			Symbol:         pos.Symbol,
			Side:           pos.Side,
		}
		groups[key] = append(groups[key], pos)
	}

	// 聚合每组
	var result []*risk.UserPosition
	for key, group := range groups {
		userPos := a.aggregateGroup(key, group)
		result = append(result, userPos)
	}

	return result
}

// aggregateGroup 聚合单个分组
func (a *PositionAggregator) aggregateGroup(key struct {
	UserStrategyID uint64
	Symbol         string
	Side           risk.Side
}, positions []risk.UserOrderPosition) *risk.UserPosition {
	if len(positions) == 0 {
		return nil
	}

	// 使用第一个订单作为基础
	first := positions[0]

	// 初始化 CloseTime 为100年后（活跃仓位）
	futureTime := time.Now().AddDate(100, 0, 0)

	userPos := &risk.UserPosition{
		UserStrategyID: key.UserStrategyID,
		UserID:         first.UserID,
		Exchange:       first.Exchange,
		PosType:        first.PosType,
		Symbol:         key.Symbol,
		Side:           key.Side,
		Leverage:       first.Leverage, // 取第一个订单的杠杆
		Deleted:        0,
		CloseTime:      &futureTime,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// 累加字段
	var totalQuantity float64
	var totalMargin float64
	var currentPrice float64

	for _, pos := range positions {
		totalQuantity += math.Abs(pos.Quantity)
		totalMargin += math.Abs(pos.InitMargin)
		currentPrice = pos.CurrentPrice // 使用最新价格
	}

	userPos.Quantity = totalQuantity
	userPos.TotalMargin = totalMargin
	userPos.CurrentPrice = currentPrice

	return userPos
}
