package main

import (
	"encoding/json"
	"log"
	"strconv"

	"trading-service/internal/exchange/deribit"
	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk"
	"trading-service/internal/risk/aggregator"
	"trading-service/internal/risk/config"
	riskexec "trading-service/internal/risk/executor"
)

// DefaultRuleGenerator wraps config.RuleStore to auto-generate default rules
// for strategies that have positions but no rules.
type DefaultRuleGenerator struct {
	rules  *config.RuleStore
	repo   *persistence.StateRepository
	nextID int
}

func NewDefaultRuleGenerator(rules *config.RuleStore) *DefaultRuleGenerator {
	return NewDefaultRuleGeneratorWithRepo(rules, nil)
}

func NewDefaultRuleGeneratorWithRepo(rules *config.RuleStore, repo *persistence.StateRepository) *DefaultRuleGenerator {
	nextID := 1
	for _, r := range rules.ListRules() {
		if r.ID >= nextID {
			nextID = r.ID + 1
		}
	}
	return &DefaultRuleGenerator{rules: rules, repo: repo, nextID: nextID}
}

// GenerateForMissingStrategies generates default rules for strategies that have
// positions but no existing rules.
// Note: Deribit positions and signal_close strategies are excluded from automatic rule generation.
func (g *DefaultRuleGenerator) GenerateForMissingStrategies(aggResults []*aggregator.PositionWithMetrics) {
	for _, agg := range aggResults {
		if agg.Position == nil {
			continue
		}
		// Deribit positions skip default rule generation
		if agg.Position.Exchange == "deribit" {
			log.Printf("skipping default rule generation for deribit strategy %d", agg.Position.UserStrategyID)
			continue
		}
		// signal_close strategies close via signal, not risk rules
		if g.isSignalCloseStrategy(agg.Position.UserStrategyID) {
			log.Printf("skipping default rule generation for signal_close strategy %d", agg.Position.UserStrategyID)
			continue
		}
		if !g.rules.HasRulesForStrategy(agg.Position.UserStrategyID) || !g.rules.HasActiveRulesForStrategy(agg.Position.UserStrategyID) {
			g.GenerateForStrategy(agg.Position.UserStrategyID)
		}
	}
}

// isSignalCloseStrategy reports whether the strategy is configured as signal_close.
// Returns false when the strategy cannot be resolved, so unresolved strategies keep
// generating default rules as before.
func (g *DefaultRuleGenerator) isSignalCloseStrategy(strategyID uint64) bool {
	if g.repo == nil {
		return false
	}
	us, err := g.repo.GetUserStrategyByID(strategyID)
	if err != nil || us == nil {
		return false
	}
	return us.RiskStrategyType == order.RiskStrategyTypeSignalClose
}

// GenerateForStrategy generates default rules for a specific strategy.
// Values are extracted from the strategy's signal params if available,
// falling back to hardcoded defaults.
func (g *DefaultRuleGenerator) GenerateForStrategy(strategyID uint64) {
	stopLossID := g.nextID
	g.nextID++
	profitTriggerID := g.nextID
	g.nextID++
	profitFollowUpID := g.nextID
	g.nextID++

	// Default values (hardcoded fallback)
	stopLoss := -0.02
	profitTrigger := 0.05
	drawdownVal := 0.05

	// Extract values from strategy params if available
	params := g.extractStrategyParams(strategyID)
	if v, ok := params["StopLossThreshold"].(float64); ok {
		stopLoss = v
	}
	if v, ok := params["TakeProfitBackThreshold"].(float64); ok {
		profitTrigger = v
	}
	if v, ok := params["TakeProfitBackDynamicFallPercent"].(float64); ok {
		drawdownVal = v
	}

	defaultRules := []risk.Rule{
		{
			ID:             stopLossID,
			UserStrategyID: strategyID,
			ConditionName:  "roi",
			Operator:       "<=",
			Value:          stopLoss,
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
			Value:          profitTrigger,
			Sort:           2,
			Status:         "active",
			Action:         strconv.Itoa(profitFollowUpID),
			Params:         map[string]interface{}{"order_type": 1, "quantity_pct": 1.0},
		},
		{
			ID:             profitFollowUpID,
			UserStrategyID: strategyID,
			ConditionName:  "profit_drawdown_pct",
			Operator:       ">=",
			Value:          drawdownVal,
			Sort:           1,
			Status:         "inactive",
			Action:         "reduce",
			Params:         map[string]interface{}{"order_type": 1, "quantity_pct": 1.0},
		},
	}
	if err := g.rules.AddRules(defaultRules); err != nil {
		log.Printf("failed to add default rules for strategy %d: %v", strategyID, err)
		return
	}
	log.Printf("generated %d default rules for strategy %d (stopLoss=%.4f trigger=%.4f drawdown=%.4f)",
		len(defaultRules), strategyID, stopLoss, profitTrigger, drawdownVal)
}

// extractStrategyParams extracts params from the strategy associated with the given user_strategy_id.
// Returns an empty map if the strategy is not found or has no params.
func (g *DefaultRuleGenerator) extractStrategyParams(strategyID uint64) map[string]interface{} {
	if g.repo == nil {
		return nil
	}

	us, err := g.repo.GetUserStrategyByID(strategyID)
	if err != nil || us.StrategyID == 0 {
		return nil
	}

	strat, err := g.repo.GetStrategyByID(us.StrategyID)
	if err != nil || strat.Params == "" {
		return nil
	}

	var params map[string]interface{}
	if err := json.Unmarshal([]byte(strat.Params), &params); err != nil {
		return nil
	}

	return params
}

// NewRiskActionApplierWithRuleStore creates a RiskActionApplier using config.RuleStore.
func NewRiskActionApplierWithRuleStore(
	repo *persistence.StateRepository,
	rules *config.RuleStore,
	resolver riskexec.ExchangeResolver,
	notifier notification.Notifier,
	spreadChecker *deribit.SpreadChecker,
) *riskexec.RiskActionApplier {
	return riskexec.NewRiskActionApplier(repo, rules, resolver, notifier, spreadChecker)
}

// Ensure config.RuleStore implements riskexec.RuleStatusUpdater interface.
var _ riskexec.RuleStatusUpdater = (*config.RuleStore)(nil)
