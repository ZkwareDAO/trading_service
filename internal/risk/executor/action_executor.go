package executor

import (
	"time"

	"trading-service/internal/risk"
)

// ActionResult 动作执行结果
type ActionResult struct {
	ActionType        string
	UserID            uint64
	UserStrategyID    uint64
	RuleID            uint64
	UserPositionID    uint64
	Symbol            string
	Side              risk.Side
	Quantity          float64
	QuantityPercent   float64
	RemainingQuantity float64
	HedgeSymbol       string
	HedgeRatio        float64
	Timestamp         time.Time
	OrderType         int // 0=limit, 1=market
}

// ActionExecutor 动作执行器
type ActionExecutor struct{}

// NewActionExecutor 创建动作执行器
func NewActionExecutor() *ActionExecutor {
	return &ActionExecutor{}
}

// Execute 执行规则动作，返回执行结果
func (e *ActionExecutor) Execute(rule *risk.Rule, ctx *risk.RiskContext) *ActionResult {
	if ctx.Position == nil {
		return nil
	}

	switch rule.Action {
	case "reduce":
		return e.executeReduce(rule, ctx)
	default:
		return nil
	}
}

func (e *ActionExecutor) executeReduce(rule *risk.Rule, ctx *risk.RiskContext) *ActionResult {
	params := rule.Params
	if params == nil {
		params = risk.DefaultParams
	}

	qtyPercent := e.getParamFloat(params, "quantity_pct", 1.0)
	orderType := e.getParamInt(params, "order_type", 1)
	qty := ctx.Position.Quantity * qtyPercent

	return &ActionResult{
		ActionType:        "reduce",
		UserID:            ctx.Position.UserID,
		UserStrategyID:    ctx.Position.UserStrategyID,
		RuleID:            uint64(rule.ID),
		UserPositionID:    ctx.Position.ID,
		Symbol:            ctx.Position.Symbol,
		Side:              ctx.Position.Side,
		Quantity:          qty,
		QuantityPercent:   qtyPercent,
		RemainingQuantity: ctx.Position.Quantity - qty,
		OrderType:         orderType,
		Timestamp:         time.Now(),
	}
}

func (e *ActionExecutor) getParamFloat(params map[string]interface{}, key string, defaultValue float64) float64 {
	if val, ok := params[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		}
	}
	return defaultValue
}

func (e *ActionExecutor) getParamInt(params map[string]interface{}, key string, defaultValue int) int {
	if val, ok := params[key]; ok {
		switch v := val.(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
	}
	return defaultValue
}
