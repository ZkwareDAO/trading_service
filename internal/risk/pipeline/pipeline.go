package pipeline

import (
	"fmt"
	"log"
	"time"

	"trading-service/internal/persistence"
	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
	"trading-service/internal/risk/engine"
	"trading-service/internal/risk/executor"
	"trading-service/internal/risk/metrics"
)

// PipelineResult 管道执行结果
type PipelineResult struct {
	Version  int64
	Position *risk.UserPosition
	Context  *risk.RiskContext
	Rules    []risk.Rule
	Results  []*executor.ActionResult
}

// RiskPipeline 风控管道，编排完整的风控流程
type RiskPipeline struct {
	metricsBuilder *metrics.LocalMetricsBuilder
	ruleEngine     *engine.RuleEngine
	actionExecutor *executor.ActionExecutor
}

// NewRiskPipeline creates a risk pipeline without repository dependency.
// The created pipeline cannot evaluate position_xxx conditions.
func NewRiskPipeline() *RiskPipeline {
	return &RiskPipeline{
		metricsBuilder: metrics.NewLocalMetricsBuilder(),
		ruleEngine:     engine.NewRuleEngine(),
		actionExecutor: executor.NewActionExecutor(),
	}
}

// NewRiskPipelineWithRepo creates a risk pipeline with repository support.
//
// The pipeline creates a single RuleEngine instance that is reused for all
// position evaluations in Run(). This is safe because positions are processed
// sequentially (not concurrently) in the main evaluation loop.
//
// Parameters:
//   - repo: StateRepository for position queries (required for position_xxx conditions)
func NewRiskPipelineWithRepo(repo *persistence.StateRepository) *RiskPipeline {
	return &RiskPipeline{
		metricsBuilder: metrics.NewLocalMetricsBuilder(),
		ruleEngine:     engine.NewRuleEngineWithRepo(repo),
		actionExecutor: executor.NewActionExecutor(),
	}
}

// Run 运行完整的风控流程
func (p *RiskPipeline) Run(state *risk.GlobalState, cfg *config.Config) []PipelineResult {
	if state == nil || len(state.Positions) == 0 {
		return nil
	}

	log.Printf("Risk pipeline started: version=%d, positions=%d", state.Version, len(state.Positions))

	// 过滤活跃仓位
	activePositions := p.FilterActivePositions(state)
	if len(activePositions) == 0 {
		log.Printf("Risk pipeline: no active positions to evaluate")
		return nil
	}

	log.Printf("Risk pipeline: evaluating %d active positions", len(activePositions))

	var results []PipelineResult

	// 遍历每个仓位执行风控
	for _, pos := range activePositions {
		result := p.ProcessPosition(pos, state, cfg)
		results = append(results, result)
	}

	triggeredCount := 0
	for _, r := range results {
		if len(r.Rules) > 0 {
			triggeredCount++
		}
	}
	log.Printf("Risk pipeline completed: evaluated=%d, triggered=%d", len(activePositions), triggeredCount)

	return results
}

// ProcessPosition 处理单个仓位
func (p *RiskPipeline) ProcessPosition(pos *risk.UserPosition, state *risk.GlobalState, cfg *config.Config) PipelineResult {
	ctx := p.BuildContext(pos, state)

	rules := cfg.GetRulesByStrategy(pos.UserStrategyID)
	if len(rules) == 0 {
		return PipelineResult{Version: state.Version, Position: pos, Context: ctx}
	}

	log.Printf("Risk evaluation: strategyID=%d, positionID=%d, symbol=%s, evaluating %d rules, ROI=%.4f, PnL=%.4f, TotalMargin=%.4f, leverage=%d, CurrentPrice=%.4f, MarkPrice=%.4f, MaxProfit=%.4f, ProfitDrawdown=%.4f",
		pos.UserStrategyID, pos.ID, pos.Symbol, len(rules), ctx.Local.ROI, ctx.Local.PnL, pos.TotalMargin, pos.Leverage, pos.CurrentPrice, ctx.Local.MarkPrice, ctx.Local.MaxProfitPct, ctx.Local.ProfitDrawdownPct)

	triggeredRules := p.ruleEngine.EvaluateRules(rules, ctx)

	if len(triggeredRules) > 0 {
		log.Printf("Rules triggered: strategyID=%d, positionID=%d, count=%d", pos.UserStrategyID, pos.ID, len(triggeredRules))
		for _, rule := range triggeredRules {
			// Log rule with actual metric value at trigger time
			actualValue := ""
			switch rule.ConditionName {
			case "roi":
				actualValue = fmt.Sprintf("%.4f", ctx.Local.ROI)
			case "pnl":
				actualValue = fmt.Sprintf("%.4f", ctx.Local.PnL)
			case "profit_drawdown_pct":
				actualValue = fmt.Sprintf("%.4f", ctx.Local.ProfitDrawdownPct)
			case "duration":
				actualValue = fmt.Sprintf("%ds", ctx.Local.DurationSec)
			case "unrealized_pnl":
				actualValue = fmt.Sprintf("%.4f", ctx.Local.UnrealizedPnL)
			case "win_rate":
				actualValue = fmt.Sprintf("%.4f", ctx.Local.UnrealizedPnL/pos.TotalMargin) // Calculate from available data
			case "maximum_drawdown":
				actualValue = fmt.Sprintf("%.4f", ctx.Local.MaxDrawdownPct)
			case "total_margin":
				actualValue = fmt.Sprintf("%.4f", pos.TotalMargin)
			default:
				actualValue = "N/A"
			}
			log.Printf("Rule triggered: id=%d, condition=%s, operator=%s, threshold=%s, actualValue=%s, action=%s, positionID=%d",
				rule.ID, rule.ConditionName, rule.Operator, rule.Value, actualValue, rule.Action, pos.ID)
		}
	}

	var actionResults []*executor.ActionResult
	for _, rule := range triggeredRules {
		result := p.actionExecutor.Execute(&rule, ctx)
		if result != nil {
			actionResults = append(actionResults, result)
			log.Printf("Action executed: ruleID=%d, actionType=%s, userID=%d, symbol=%s", rule.ID, result.ActionType, result.UserID, result.Symbol)
		}
	}

	return PipelineResult{
		Version:  state.Version,
		Position: pos,
		Context:  ctx,
		Rules:    triggeredRules,
		Results:  actionResults,
	}
}

// BuildContext 为仓位构建 RiskContext
func (p *RiskPipeline) BuildContext(pos *risk.UserPosition, state *risk.GlobalState) *risk.RiskContext {
	local := p.metricsBuilder.Build(pos, 0)

	// Use pre-computed PnL and ROI from aggregator (stored in user_positions table)
	// The aggregator already calculated these correctly using PosPrice vs current price
	if pos.PnL != 0 {
		local.PnL = pos.PnL
		local.ROI = pos.ROI
		local.MarkPrice = pos.CurrentPrice
	} else {
		// Fallback: only calculate if PnL not yet computed
		if state.Snapshot != nil {
			if markPrice, ok := findPriceInSnapshot(state.Snapshot, pos.Exchange, pos.Symbol); ok {
				local.MarkPrice = markPrice
				entryPrice := pos.CurrentPrice
				if entryPrice == 0 {
					entryPrice = markPrice
				}
				pnl := p.metricsBuilder.CalculatePnL(pos, entryPrice, markPrice)
				local.PnL = pnl
				local.ROI = p.calculateROI(pnl, pos.TotalMargin, pos.Leverage)

				log.Printf("WARN: PnL/ROI calculated in pipeline for position %d (should use pre-computed values from aggregator)", pos.ID)
			} else {
				if pos.CurrentPrice > 0 {
					log.Printf("WARN: price not found in snapshot for %s/%s, using position current price %.4f", pos.Exchange, pos.Symbol, pos.CurrentPrice)
					local.MarkPrice = pos.CurrentPrice
				} else {
					log.Printf("WARN: no price available for position %d (%s/%s), ROI calculation skipped", pos.ID, pos.Exchange, pos.Symbol)
				}
			}
		} else {
			log.Printf("WARN: market snapshot is nil, cannot calculate ROI for position %d", pos.ID)
		}
	}

	// profit_drawdown_pct: 基于ROI（含杠杆）
	if pos.MaxProfitPct > 0 && local.ROI < pos.MaxProfitPct {
		local.ProfitDrawdownPct = (pos.MaxProfitPct - local.ROI) / pos.MaxProfitPct
	}

	var global risk.GlobalMetrics
	if state.Metrics != nil {
		global = *state.Metrics
	}

	local.DurationSec = int64(time.Since(pos.CreatedAt).Seconds())

	return &risk.RiskContext{
		Position: pos,
		Local:    local,
		Global:   global,
		Market:   state.Snapshot,
	}
}

// FilterActivePositions 过滤活跃仓位 (Deleted=0, CurrentPrice>0)
// CurrentPrice=0 的仓位跳过风控评估，避免错误 PnL/ROI 误触发风控动作。
func (p *RiskPipeline) FilterActivePositions(state *risk.GlobalState) []*risk.UserPosition {
	var active []*risk.UserPosition
	for _, pos := range state.Positions {
		if pos.Deleted == 0 && pos.CurrentPrice > 0 {
			active = append(active, pos)
		} else if pos.Deleted == 0 && pos.CurrentPrice == 0 {
			log.Printf("[DEBUG] FilterActivePositions: skipped strategyID=%d, symbol=%s, current_price=0", pos.UserStrategyID, pos.Symbol)
		}
	}
	return active
}

// calculateROI 计算 ROI
func (p *RiskPipeline) calculateROI(pnl, totalMargin float64, leverage int) float64 {
	if totalMargin == 0 {
		return 0
	}
	return pnl / totalMargin * float64(leverage)
}

// findPriceInSnapshot looks up a price by exchange+symbol, falling back to searching all exchanges.
func findPriceInSnapshot(snap *risk.MarketSnapshot, exchange, symbol string) (float64, bool) {
	if snap == nil {
		return 0, false
	}

	// Try exact exchange first
	if exPrices, ok := snap.Prices[exchange]; ok {
		if price, ok := lookupPrice(exPrices, symbol); ok {
			return price, true
		}
	}

	// Fall back to searching all exchanges
	for _, exPrices := range snap.Prices {
		if price, ok := lookupPrice(exPrices, symbol); ok {
			return price, true
		}
	}

	return 0, false
}

// lookupPrice finds the best-matching price within an exchange's price map.
func lookupPrice(exPrices map[string]float64, asset string) (float64, bool) {
	if price, ok := exPrices[asset]; ok {
		return price, true
	}
	// Strip quote suffix for Hyperliquid (e.g., "NEARUSDC" -> "NEAR")
	switch {
	case len(asset) > 4 && (asset[len(asset)-4:] == "USDT" || asset[len(asset)-4:] == "USDC"):
		coin := asset[:len(asset)-4]
		if price, ok := exPrices[coin]; ok {
			return price, true
		}
	}
	return 0, false
}
