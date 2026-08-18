package risk

import "time"

// ============================================
// 核心数据结构
// ============================================

// GlobalState 唯一真相源，包含版本化的全局状态
type GlobalState struct {
	Version   int64
	Snapshot  *MarketSnapshot
	Metrics   *GlobalMetrics
	Positions []*UserPosition
}

// MarketSnapshot 行情快照
type MarketSnapshot struct {
	Prices  map[string]map[string]float64 // exchange -> symbol -> price
	Funding map[string]float64            // symbol -> funding rate
}

// GlobalMetrics 全局指标
type GlobalMetrics struct {
	BTCVolatility float64
	MarketTrend   float64
}

// LocalMetrics 局部指标，实时计算，不持久化
type LocalMetrics struct {
	ROI               float64
	PnL               float64
	MaxProfitPct      float64
	MaxDrawdownPct    float64
	ProfitDrawdownPct float64 // 盈利回落百分比 = (max_profit_pct - roi) / max_profit_pct
	UnrealizedPnL     float64
	RealizedPnL       float64
	EntryPrice        float64
	MarkPrice         float64
	DurationSec       int64
}

// RiskContext 风控输入上下文
type RiskContext struct {
	Position *UserPosition
	Local    LocalMetrics
	Global   GlobalMetrics
	Market   *MarketSnapshot
}

// RiskSignal 风控信号，只传版本号
type RiskSignal struct {
	Version int64
}

// ============================================
// 仓位数据结构
// ============================================

// UserPosition 聚合仓位（策略层）- 原 UserSubPosition
type UserPosition struct {
	ID                    uint64
	UserID                uint64
	UserStrategyID        uint64
	RiskControlStrategyID uint64
	Exchange              string
	PosType               PosType
	Symbol                string
	Side                  Side
	CurrentPrice          float64
	Quantity              float64
	TotalMargin           float64
	Leverage              int
	PnL                   float64 // 未实现盈亏 (from aggregator)
	ROI                   float64 // 收益率 (含杠杆, from aggregator)
	MaxProfitPct          float64 // 最大盈利百分比（用于profit_drawdown计算，基于ROI含杠杆）
	Deleted               int     // 0: 活跃, 1: 已平仓
	CloseTime             *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// UserOrderPosition 订单层仓位
type UserOrderPosition struct {
	ID               uint64
	UserID           uint64
	UserOrderID      uint64
	UserStrategyID   uint64
	Exchange         string
	PosType          PosType
	Symbol           string
	Side             Side
	Quantity         float64
	PosPrice         float64
	CurrentPrice     float64
	Leverage         int
	InitMargin       float64
	Deleted          int // 0: 活跃, 1: 已平仓
	UprunningOrderID uint64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ============================================
// 类型枚举
// ============================================

// PosType 仓位类型
type PosType int

const (
	PosTypeSpot    PosType = 1 // 现货
	PosTypeFutures PosType = 2 // 合约
	PosTypeOption  PosType = 3 // 期权
)

// Side 仓位方向
type Side int

const (
	SideLong  Side = 0 // 多单
	SideShort Side = 1 // 空单
)

// ============================================
// 配置模型 - 单一 rule.csv
// ============================================

// Rule 风控规则（从 rule.csv 加载，自包含条件和行动）
type Rule struct {
	ID             int
	UserStrategyID uint64
	ConditionName  string      // price_btc, price_eth, roi, profit_drawdown_pct, holding_time
	Operator       string      // <, >, <=, >=, ==, !=
	Value          interface{} // 比较值
	Sort           int         // 1=最高优先级
	Status         string      // "active" 或 "inactive"
	Action         string      // "reduce" 或 rule_id（字符串，激活另一条规则）
	Params         map[string]interface{}
	CreatedAt      time.Time   // 创建时间
	UpdatedAt      time.Time   // 更新时间
}

// DefaultParams 默认 params
var DefaultParams = map[string]interface{}{
	"order_type":    1, // 1=市价
	"quantity_pct":  1.0,
}
