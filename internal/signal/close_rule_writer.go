package signal

import (
	"context"
	"log"
	"time"

	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
	"trading-service/internal/rpc"
)

type CloseRuleRequest struct {
	UserStrategyID uint64
	QuantityPct    float64
	OrderType      int
}

// CloseRuleWriter creates close rules for strategies.
// It supports two modes:
// 1. RPC mode (recommended): Calls PMS via RPC to create rules (PMS manages rule.csv)
// 2. Direct mode (legacy): Writes directly to RuleStore (for backward compatibility)
type CloseRuleWriter struct {
	ruleStore *config.RuleStore         // legacy mode
	rpcClient *rpc.OrderServiceClient   // recommended mode
}

// NewCloseRuleWriterWithRPC creates a CloseRuleWriter that uses RPC to call PMS.
// This is the recommended approach for UOS to create rules.
func NewCloseRuleWriterWithRPC(rpcClient *rpc.OrderServiceClient) *CloseRuleWriter {
	return &CloseRuleWriter{
		rpcClient: rpcClient,
	}
}

// NewCloseRuleWriterWithStore creates a CloseRuleWriter that writes directly to RuleStore.
// This is kept for backward compatibility and tests.
func NewCloseRuleWriterWithStore(ruleStore *config.RuleStore) *CloseRuleWriter {
	return &CloseRuleWriter{
		ruleStore: ruleStore,
	}
}

// AppendImmediateCloseRule creates an immediate close rule.
// If RPC client is configured, it calls PMS via RPC (recommended).
// Otherwise, it writes directly to RuleStore (legacy).
func (w *CloseRuleWriter) AppendImmediateCloseRule(ctx context.Context, req CloseRuleRequest) (int, error) {
	// Prefer RPC mode if available
	if w.rpcClient != nil {
		return w.appendImmediateCloseRuleViaRPC(ctx, req)
	}
	// Fallback to direct mode
	return w.appendImmediateCloseRuleDirect(req)
}

// appendImmediateCloseRuleViaRPC creates a close rule by calling PMS via RPC.
// This ensures PMS centrally manages all rule.csv writes.
func (w *CloseRuleWriter) appendImmediateCloseRuleViaRPC(ctx context.Context, req CloseRuleRequest) (int, error) {
	if req.UserStrategyID == 0 {
		return 0, ErrUserStrategyIDRequired
	}
	if req.QuantityPct <= 0 {
		req.QuantityPct = 1.0
	}
	if req.OrderType == 0 {
		req.OrderType = orderTypeMarket
	}

	log.Printf("CloseRuleWriter: creating immediate close rule via RPC: userStrategyID=%d", req.UserStrategyID)

	resp, err := w.rpcClient.CreateRule(ctx, rpc.CreateRuleRequest{
		UserStrategyID: req.UserStrategyID,
		ConditionName:  "always",
		Operator:       "==",
		Value:          "true",
		Sort:           1,
		Action:         "reduce",
		Params: map[string]interface{}{
			"order_type":   req.OrderType,
			"quantity_pct": req.QuantityPct,
		},
	})
	if err != nil {
		log.Printf("CloseRuleWriter: FAILED to create rule via RPC: error=%v", err)
		return 0, err
	}

	log.Printf("CloseRuleWriter: SUCCESS created rule via RPC: ID=%d, userStrategyID=%d", resp.RuleID, req.UserStrategyID)
	return resp.RuleID, nil
}

// appendImmediateCloseRuleDirect creates a close rule by writing directly to RuleStore.
// This is the legacy approach where UOS writes directly to rule.csv.
func (w *CloseRuleWriter) appendImmediateCloseRuleDirect(req CloseRuleRequest) (int, error) {
	if req.UserStrategyID == 0 {
		return 0, ErrUserStrategyIDRequired
	}
	if req.QuantityPct <= 0 {
		req.QuantityPct = 1.0
	}
	if req.OrderType == 0 {
		req.OrderType = orderTypeMarket
	}

	now := time.Now()
	rule := risk.Rule{
		UserStrategyID: req.UserStrategyID,
		ConditionName:  "always",
		Operator:       "==",
		Value:          "true",
		Sort:           1,
		Status:         config.RuleStatusActive,
		Action:         "reduce",
		Params: map[string]interface{}{
			"order_type":   req.OrderType,
			"quantity_pct": req.QuantityPct,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	log.Printf("CloseRuleWriter: creating immediate close rule (direct): userStrategyID=%d, condition=always, status=active", rule.UserStrategyID)

	if err := w.ruleStore.CreateRule(&rule); err != nil {
		log.Printf("CloseRuleWriter: FAILED to create rule: error=%v", err)
		return 0, err
	}

	log.Printf("CloseRuleWriter: SUCCESS added rule (direct): ID=%d, userStrategyID=%d", rule.ID, rule.UserStrategyID)
	return rule.ID, nil
}

// Legacy error for backward compatibility
var ErrUserStrategyIDRequired = &CloseRuleError{Message: "user_strategy_id is required"}

type CloseRuleError struct {
	Message string
}

func (e *CloseRuleError) Error() string {
	return e.Message
}
