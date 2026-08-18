package engine

import (
	"log"
	"sort"
	"strconv"
	"strings"

	"trading-service/internal/persistence"
	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
)

// RuleEngine evaluates rules and returns triggered actions.
//
// Concurrency Safety:
// RuleEngine is NOT thread-safe when created with NewRuleEngineWithRepo.
// The repository pointer is used for position queries during rule evaluation.
// If multiple goroutines need to evaluate rules concurrently, create separate
// RuleEngine instances or use external synchronization.
//
// Current Usage Pattern:
// RiskPipeline creates one RuleEngine instance and uses it to evaluate all
// positions sequentially in Run(). This is safe because there's no concurrent
// access - each position is processed one at a time in the main loop.
type RuleEngine struct {
	repo *persistence.StateRepository
}

// NewRuleEngine creates a new rule engine without repository dependency.
// Use this for rule evaluation that doesn't require position queries.
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{}
}

// NewRuleEngineWithRepo creates a rule engine with repository for position queries.
//
// Usage: For position_xxx conditions that need to query position state.
// Note: The returned RuleEngine is not thread-safe. Do not share across goroutines.
func NewRuleEngineWithRepo(repo *persistence.StateRepository) *RuleEngine {
	return &RuleEngine{repo: repo}
}

// IsValidPositionSymbol validates position symbol format (case-insensitive).
// Spot: ends with USDT/USDC (e.g., BTCUSDT, btcusdt)
// Option: UNDERLYING-DDMMMYY-STRIKE-TYPE (e.g., BTC-7AUG26-64000-P, btc-7aug26-64000-c)
func IsValidPositionSymbol(symbol string) bool {
	upper := strings.ToUpper(symbol)

	// Spot format: USDT/USDC suffix
	if strings.HasSuffix(upper, "USDT") || strings.HasSuffix(upper, "USDC") {
		return len(symbol) > 4 // at least one coin prefix
	}

	// Option format validation
	parts := strings.Split(symbol, "-")
	if len(parts) != 4 {
		return false
	}

	// Validate underlying (BTC/ETH/SOL, etc.)
	if parts[0] == "" {
		return false
	}

	// Validate date format (DMMMYY or DDMMMYY, e.g., 7AUG26 or 25DEC26)
	dateStr := strings.ToUpper(parts[1])
	if len(dateStr) < 6 || len(dateStr) > 7 {
		return false
	}

	// Extract day part (1-2 digits at the beginning)
	dayEnd := len(dateStr) - 5 // 1 digit day or 2 digit day
	if dayEnd < 1 {
		return false
	}

	// Validate day is all digits
	if !allDigits(dateStr[:dayEnd]) {
		return false
	}

	// Validate month (3 letters)
	if len(dateStr) < dayEnd+3 {
		return false
	}
	month := dateStr[dayEnd : dayEnd+3]
	validMonths := map[string]bool{
		"JAN": true, "FEB": true, "MAR": true, "APR": true, "MAY": true, "JUN": true,
		"JUL": true, "AUG": true, "SEP": true, "OCT": true, "NOV": true, "DEC": true,
	}
	if !validMonths[month] {
		return false
	}

	// Validate year (2 digits at the end)
	if !allDigits(dateStr[len(dateStr)-2:]) {
		return false
	}

	// Validate strike price (non-empty)
	if parts[2] == "" {
		return false
	}

	// Validate option type (C=Call, P=Put, case-insensitive)
	optType := strings.ToUpper(parts[3])
	return optType == "C" || optType == "P"
}

// NormalizePositionSymbol normalizes position symbol to standard uppercase format.
// Option: "btc-7aug26-64000-c" → "BTC-7AUG26-64000-C"
// Spot: "btcusdt" → "BTCUSDT"
// Invalid format: returned as-is.
func NormalizePositionSymbol(symbol string) string {
	upper := strings.ToUpper(symbol)

	// Spot format
	if strings.HasSuffix(upper, "USDT") || strings.HasSuffix(upper, "USDC") {
		return upper
	}

	// Option format
	parts := strings.Split(symbol, "-")
	if len(parts) != 4 {
		return symbol // invalid, return as-is
	}

	return strings.ToUpper(parts[0]) + "-" +
		strings.ToUpper(parts[1]) + "-" +
		parts[2] + "-" + // strike price stays as-is (numeric)
		strings.ToUpper(parts[3])
}

// allDigits checks if a string contains only digits.
func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// EvaluateRules evaluates all active rules for a position, returns triggered rules sorted by priority.
func (e *RuleEngine) EvaluateRules(rules []risk.Rule, ctx *risk.RiskContext) []risk.Rule {
	if ctx.Position == nil || len(rules) == 0 {
		return nil
	}

	var triggered []risk.Rule
	for _, rule := range rules {
		if rule.Status != "active" {
			continue
		}
		if e.EvaluateCondition(rule, ctx) {
			triggered = append(triggered, rule)
		}
	}

	sort.Slice(triggered, func(i, j int) bool {
		return triggered[i].Sort < triggered[j].Sort
	})

	return triggered
}

// EvaluateCondition evaluates a single rule's condition against context.
func (e *RuleEngine) EvaluateCondition(rule risk.Rule, ctx *risk.RiskContext) bool {
	// Special handling for position_xxx conditions
	if strings.HasPrefix(rule.ConditionName, "position_") {
		return e.evaluatePositionCondition(rule, ctx)
	}

	value := e.getFieldValue(rule.ConditionName, ctx)
	if value == nil {
		return false
	}

	switch val := value.(type) {
	case bool:
		condValue, ok := rule.Value.(bool)
		if !ok {
			return false
		}
		switch rule.Operator {
		case "==":
			return val == condValue
		case "!=":
			return val != condValue
		default:
			return false
		}
	case *float64:
		if val == nil {
			return false
		}
		return e.evaluateFloat(*val, rule)
	case float64:
		return e.evaluateFloat(val, rule)
	default:
		return false
	}
}

// evaluatePositionCondition evaluates position_xxx conditions.
// Returns true if:
// 1. Symbol format is valid
// 2. Active position count matches the condition
// 3. Deleted position record exists (user had this position before)
func (e *RuleEngine) evaluatePositionCondition(rule risk.Rule, ctx *risk.RiskContext) bool {
	symbol := strings.TrimPrefix(rule.ConditionName, "position_")
	if !IsValidPositionSymbol(symbol) {
		return false
	}

	if e.repo == nil || ctx.Position == nil {
		return false
	}

	// Query positions for this symbol
	positions := e.repo.ListUserOrderPositionsByFilter(persistence.UserOrderPositionFilter{
		UserID: ctx.Position.UserID,
		Asset:  symbol,
	})

	// Count active and deleted positions in one pass
	var activeCount int
	var hasDeletedRecord bool
	for _, pos := range positions {
		if pos.Deleted == 0 {
			activeCount++
		} else {
			hasDeletedRecord = true
		}
	}

	result := e.evaluateFloat(float64(activeCount), rule) && hasDeletedRecord
	log.Printf("[DEBUG] evaluatePositionCondition: ruleID=%d, symbol=%s, userID=%d, positions_found=%d, activeCount=%d, hasDeletedRecord=%v, operator=%s, value=%s, result=%v",
		rule.ID, symbol, ctx.Position.UserID, len(positions), activeCount, hasDeletedRecord, rule.Operator, rule.Value, result)
	return result
}

func (e *RuleEngine) evaluateFloat(val float64, rule risk.Rule) bool {
	condValue := e.toFloat(rule.Value)
	if condValue == nil {
		return false
	}
	cv := *condValue
	switch rule.Operator {
	case "<":
		return val < cv
	case "<=":
		return val <= cv
	case ">":
		return val > cv
	case ">=":
		return val >= cv
	case "==", "=":
		return val == cv
	case "!=":
		return val != cv
	default:
		return false
	}
}

func (e *RuleEngine) getFieldValue(name string, ctx *risk.RiskContext) interface{} {
	switch name {
	case "always":
		return true
	case "roi":
		if ctx.Position != nil && ctx.Position.Leverage > 0 {
			return ctx.Local.ROI / float64(ctx.Position.Leverage)
		}
		return ctx.Local.ROI
	case "pnl":
		return ctx.Local.PnL
	case "profit_drawdown_pct":
		return ctx.Local.ProfitDrawdownPct
	case "max_profit_pct":
		return ctx.Local.MaxProfitPct
	case "max_drawdown_pct":
		return ctx.Local.MaxDrawdownPct
	case "unrealized_pnl":
		return ctx.Local.UnrealizedPnL
	case "realized_pnl":
		return ctx.Local.RealizedPnL
	case "price_btc":
		return e.getMarketPrice("BTCUSDT", ctx)
	case "price_eth":
		return e.getMarketPrice("ETHUSDT", ctx)
	// holding_time: 持仓时长（秒），用于时间止损
	// 用法示例: condition_name="holding_time", operator=">", value=259200 (72小时)
	// 含义: 持仓超过 72 小时仍未盈利，触发平仓
	// 适用场景: 长时间不赚钱的仓位大概率也赚不到钱（Hyperliquid 实践）
	case "holding_time":
		return float64(ctx.Local.DurationSec)
	default:
		// 通用价格查询: price_xxx → XXXUSDT (如 price_sol → SOLUSDT, price_sui → SUIUSDT)
		if strings.HasPrefix(name, "price_") {
			symbol := strings.ToUpper(strings.TrimPrefix(name, "price_")) + "USDT"
			return e.getMarketPrice(symbol, ctx)
		}
		return nil
	}
}

func (e *RuleEngine) getMarketPrice(symbol string, ctx *risk.RiskContext) *float64 {
	if ctx.Market != nil {
		for _, exPrices := range ctx.Market.Prices {
			if price, ok := exPrices[symbol]; ok {
				return &price
			}
		}
	}
	return nil
}

func (e *RuleEngine) toFloat(value interface{}) *float64 {
	switch v := value.(type) {
	case float64:
		return &v
	case float32:
		f := float64(v)
		return &f
	case int:
		f := float64(v)
		return &f
	case int64:
		f := float64(v)
		return &f
	case bool:
		if v {
			f := 1.0
			return &f
		}
		f := 0.0
		return &f
	default:
		return nil
	}
}

// ============================================
// RuleScheduler - activates/deactivates rules
// ============================================

// RuleScheduler handles rule status transitions.
type RuleScheduler struct {
	rules map[int]*risk.Rule
}

// NewRuleScheduler creates a new rule scheduler.
func NewRuleScheduler(rules []risk.Rule) *RuleScheduler {
	rs := &RuleScheduler{rules: make(map[int]*risk.Rule)}
	for i := range rules {
		rs.rules[rules[i].ID] = &rules[i]
	}
	return rs
}

// ActivateRule sets a rule to active.
func (rs *RuleScheduler) ActivateRule(ruleID int) {
	if r, ok := rs.rules[ruleID]; ok {
		r.Status = "active"
	}
}

// InUseRule sets an active rule to in_use (prevents duplicate triggering).
func (rs *RuleScheduler) InUseRule(ruleID int) {
	if r, ok := rs.rules[ruleID]; ok {
		if r.Status == "active" {
			r.Status = "in_use"
		}
	}
}

// DeactivateRule sets a rule to inactive.
func (rs *RuleScheduler) DeactivateRule(ruleID int) {
	if r, ok := rs.rules[ruleID]; ok {
		r.Status = "inactive"
	}
}

// DeactivateAllForStrategy deactivates all rules for a strategy.
func (rs *RuleScheduler) DeactivateAllForStrategy(strategyID uint64) {
	for _, r := range rs.rules {
		if r.UserStrategyID == strategyID {
			r.Status = "inactive"
		}
	}
}

// ============================================
// DefaultRuleGenerator - generates default rules for traditional strategies
// ============================================

// DefaultRuleGenerator generates default stop-loss and profit rules.
type DefaultRuleGenerator struct {
	nextID int
}

// NewDefaultRuleGenerator creates a new generator.
func NewDefaultRuleGenerator(startID int) *DefaultRuleGenerator {
	return &DefaultRuleGenerator{nextID: startID}
}

// GenerateDefaultRules creates default stop-loss and profit rules for a traditional strategy.
//
// 规则链逻辑（回落止盈 = Trailing Stop）:
//
//  1. Stop-Loss (ID=N, active):  ROI <= -0.02 → 全仓减仓
//     → 止损触发，直接 reduce 平仓
//
//  2. Profit-Trigger (ID=N+1, active): ROI >= 0.05 → 激活 Profit-FollowUp
//     → 当盈利达到 5% 时，激活回落止盈规则（chain action）
//
//  3. Profit-FollowUp (ID=N+2, inactive): profit_drawdown_pct >= 0.05 → 全仓减仓
//     → 回落止盈：当盈利从最高点回落超过 5% 时触发平仓
//     → profit_drawdown_pct = (max_profit_pct - current_roi) / max_profit_pct
//     → 此规则初始为 inactive，由 Profit-Trigger 的 chain action 激活
//     → 激活后监控 profit_drawdown_pct，回落超过阈值即平仓
//
// 示例流程:
//
//	入场 → ROI 从 0% 涨到 10% (Profit-Trigger 激活 Profit-FollowUp)
//	     → ROI 从 10% 回落到 7% (profit_drawdown = (0.10-0.07)/0.10 = 30% > 5% → 触发平仓)
func (g *DefaultRuleGenerator) GenerateDefaultRules(strategyID uint64) []risk.Rule {
	stopLossID := g.nextID
	g.nextID++
	profitTriggerID := g.nextID
	g.nextID++
	profitFollowUpID := g.nextID
	g.nextID++

	return []risk.Rule{
		{
			ID:             stopLossID,
			UserStrategyID: strategyID,
			ConditionName:  "roi",
			Operator:       "<=",
			Value:          -0.02,
			Sort:           1,
			Status:         "active",
			Action:         "reduce",
			Params:         map[string]interface{}{"order_type": 1, "quantity_pct": 1.0},
		},
		{
			ID:             profitTriggerID,
			UserStrategyID: strategyID,
			ConditionName:  "roi",
			Operator:       ">=",
			Value:          0.05,
			Sort:           2,
			Status:         "active",
			Action:         strconv.Itoa(profitFollowUpID), // chain to follow-up rule
			Params:         map[string]interface{}{"order_type": 1, "quantity_pct": 1.0},
		},
		{
			ID:             profitFollowUpID,
			UserStrategyID: strategyID,
			ConditionName:  "profit_drawdown_pct",
			Operator:       ">=",
			Value:          0.05,
			Sort:           1,
			Status:         "inactive", // activated by profitTrigger
			Action:         "reduce",
			Params:         map[string]interface{}{"order_type": 1, "quantity_pct": 1.0},
		},
	}
}

// EnsureDefaultRules checks if a strategy has rules, generates defaults if missing.
func EnsureDefaultRules(cfg *config.Config, strategyID uint64, nextID *int) {
	stratInfo := cfg.GetStrategyInfo(strategyID)
	if stratInfo == nil {
		return
	}
	if stratInfo.RiskStrategyType != "traditional" {
		return
	}

	// Check if any rules exist for this strategy
	for _, r := range cfg.Rules {
		if r.UserStrategyID == strategyID {
			return // already has rules
		}
	}

	gen := NewDefaultRuleGenerator(*nextID)
	defaultRules := gen.GenerateDefaultRules(strategyID)
	cfg.Rules = append(cfg.Rules, defaultRules...)
	*nextID = gen.nextID
}
