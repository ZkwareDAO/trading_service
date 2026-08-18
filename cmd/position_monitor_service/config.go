package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"trading-service/internal/persistence"
	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
	"trading-service/internal/risk/engine"
)

// PMDefaults holds default values for rule generation.
type PMDefaults struct {
	StopLossPct           float64 // ROI 止损（新仓位自动生成）
	ProfitDrawdownPct     float64 // 回落止盈触发阈值（新仓位自动生成）
	TrailingActivationPct float64 // 盈利达到多少后启动回落止盈
	TimeStopHours         int     // 时间止损默认值（仅 API 注册 holding_time 时使用）
}

type PMRuntime struct {
	PriceSnapshotInterval time.Duration
}

// PMExchangeConfig controls per-exchange testnet flags.
type PMExchangeConfig struct {
	BinanceTestnet     bool
	HyperliquidTestnet bool
	DeribitTestnet     bool
}

// PMNotificationConfig holds webhook notification settings.
type PMNotificationConfig struct {
	Enabled  bool
	OpenURL  string
	CloseURL string
	TestURL  string
}

// DeribitPositionSyncConfig holds Deribit position sync settings.
type DeribitPositionSyncConfig struct {
	Enabled  bool
	Interval time.Duration
}

// PMConfig holds Position Monitor configuration.
type PMConfig struct {
	Defaults               PMDefaults
	Runtime                PMRuntime
	Exchange               PMExchangeConfig
	Notification           PMNotificationConfig
	FilterSyncInterval     time.Duration // how often to sync exchange_symbol_filters
	DeribitSpreadThreshold float64       // Deribit close position spread threshold (absolute value)
	DeribitPositionSync    DeribitPositionSyncConfig
}

// LoadPMConfig loads configuration from env vars with optional YAML override.
func LoadPMConfig() (*PMConfig, error) {
	cfg := &PMConfig{
		Defaults: PMDefaults{
			StopLossPct:           -0.02,
			ProfitDrawdownPct:     0.05,
			TrailingActivationPct: 0.05,
			TimeStopHours:         72,
		},
		Runtime: PMRuntime{
			PriceSnapshotInterval: 10 * time.Second,
		},
	FilterSyncInterval:     240 * time.Hour, // default 10 days
		DeribitSpreadThreshold: 0.005,           // default 0.005 absolute spread
		DeribitPositionSync: DeribitPositionSyncConfig{
			Enabled:  false,           // disabled by default
			Interval: 10 * time.Minute, // default 10 minutes
		},
	}

	// Try loading from YAML if POSITION_MONITOR_CONFIG is set
	if yamlPath := os.Getenv("POSITION_MONITOR_CONFIG"); yamlPath != "" {
		if err := loadPMConfigFromYAML(yamlPath, cfg); err != nil {
			return nil, err
		}
	}

	// Environment variables override YAML values
	if hours := os.Getenv("POSITION_MONITOR_TIME_STOP_HOURS"); hours != "" {
		h, err := strconv.Atoi(hours)
		if err == nil {
			cfg.Defaults.TimeStopHours = h
		}
	}
	if interval := os.Getenv("POSITION_MONITOR_PRICE_SNAPSHOT_INTERVAL"); interval != "" {
		d, err := time.ParseDuration(interval)
		if err == nil && d > 0 {
			cfg.Runtime.PriceSnapshotInterval = d
		}
	}
	if interval := os.Getenv("FILTER_SYNC_INTERVAL"); interval != "" {
		d, err := time.ParseDuration(interval)
		if err == nil && d > 0 {
			cfg.FilterSyncInterval = d
		}
	}

	// Exchange testnet flags from env
	if os.Getenv("BINANCE_TESTNET") == "true" {
		cfg.Exchange.BinanceTestnet = true
	}
	if os.Getenv("HYPERLIQUID_TESTNET") == "true" {
		cfg.Exchange.HyperliquidTestnet = true
	}
	if os.Getenv("DERIBIT_TESTNET") == "true" {
		cfg.Exchange.DeribitTestnet = true
	}

	return cfg, nil
}

// loadPMConfigFromYAML is a simple key-value YAML parser (same pattern as internal/config/config.go).
func loadPMConfigFromYAML(path string, cfg *PMConfig) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	section := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && strings.HasSuffix(line, ":") {
			section = strings.TrimSuffix(line, ":")
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"")

		switch section {
		case "defaults":
			switch key {
			case "stop_loss_pct":
				if v, err := strconv.ParseFloat(value, 64); err == nil {
					cfg.Defaults.StopLossPct = v
				}
			case "profit_drawdown_pct":
				if v, err := strconv.ParseFloat(value, 64); err == nil {
					cfg.Defaults.ProfitDrawdownPct = v
				}
			case "trailing_activation_pct":
				if v, err := strconv.ParseFloat(value, 64); err == nil {
					cfg.Defaults.TrailingActivationPct = v
				}
			case "time_stop_hours":
				if v, err := strconv.Atoi(value); err == nil {
					cfg.Defaults.TimeStopHours = v
				}
			}
		case "runtime":
			switch key {
			case "price_snapshot_interval":
				if v, err := time.ParseDuration(value); err == nil && v > 0 {
					cfg.Runtime.PriceSnapshotInterval = v
				}
			}
		case "exchange":
			switch key {
			case "binance_testnet":
				if value == "true" {
					cfg.Exchange.BinanceTestnet = true
				}
			case "hyperliquid_testnet":
				if value == "true" {
					cfg.Exchange.HyperliquidTestnet = true
				}
			case "deribit_testnet":
				if value == "true" {
					cfg.Exchange.DeribitTestnet = true
				}
			}
		case "deribit":
			switch key {
			case "spread_threshold":
				if v, err := strconv.ParseFloat(value, 64); err == nil && v > 0 {
					cfg.DeribitSpreadThreshold = v
				}
			}
		case "notification":
			switch key {
			case "enabled":
				cfg.Notification.Enabled = value == "true"
			case "open_url":
				cfg.Notification.OpenURL = value
			case "close_url":
				cfg.Notification.CloseURL = value
			case "test_url":
				cfg.Notification.TestURL = value
			}
		case "deribit_position_sync":
			switch key {
			case "enabled":
				cfg.DeribitPositionSync.Enabled = value == "true"
			case "interval":
				if v, err := time.ParseDuration(value); err == nil && v > 0 {
					cfg.DeribitPositionSync.Interval = v
				}
			}
		}
	}

	return nil
}

// GenerateDefaultRules creates default stop-loss + trailing-stop rules for a new position.
//
// 规则链逻辑（回落止盈 = Trailing Stop）:
//  1. Stop-Loss (active):  ROI <= stop_loss_pct → reduce (止损)
//  2. Profit-Trigger (active): ROI >= trailing_activation_pct → activate followup
//  3. Profit-FollowUp (inactive): profit_drawdown_pct >= profit_drawdown_pct → reduce (回落止盈)
//
// 注意: 时间止损 (holding_time) 不自动生成，用户通过 API 指定。
// 注意: 单独 ROI 止盈也不自动生成，用户通过 API 指定。
func GenerateDefaultRules(strategyID uint64, pmCfg *PMConfig, nextID *int) []risk.Rule {
	stopLossID := *nextID
	*nextID++
	profitTriggerID := *nextID
	*nextID++
	profitFollowUpID := *nextID
	*nextID++

	return []risk.Rule{
		{
			ID:             stopLossID,
			UserStrategyID: strategyID,
			ConditionName:  "roi",
			Operator:       "<=",
			Value:          pmCfg.Defaults.StopLossPct,
			Sort:           1,
			Status:         "active",
			Action:         "reduce",
			Params:         risk.DefaultParams,
		},
		{
			ID:             profitTriggerID,
			UserStrategyID: strategyID,
			ConditionName:  "roi",
			Operator:       ">=",
			Value:          pmCfg.Defaults.TrailingActivationPct,
			Sort:           2,
			Status:         "active",
			Action:         strconv.Itoa(profitFollowUpID), // chain to follow-up rule
			Params:         risk.DefaultParams,
		},
		{
			ID:             profitFollowUpID,
			UserStrategyID: strategyID,
			ConditionName:  "profit_drawdown_pct",
			Operator:       ">=",
			Value:          pmCfg.Defaults.ProfitDrawdownPct,
			Sort:           1,
			Status:         "inactive", // activated by profitTrigger
			Action:         "reduce",
			Params:         risk.DefaultParams,
		},
	}
}

// ============================================
// API Types
// ============================================

// FallbackRuleConfig configures the fallback (drawdown) rule for profit-taking.
// When creating a profit-taking rule (roi >= X), this automatically creates
// a fallback rule that triggers when profit_drawdown_pct >= Y.
type FallbackRuleConfig struct {
	Value float64 `json:"value"` // Drawdown percentage (e.g., 0.3 = 30%)
}

// RegisterRuleRequest is the input for POST /api/rules.
type RegisterRuleRequest struct {
	UserStrategyID uint64              `json:"user_strategy_id"`        // required
	ConditionName  string              `json:"condition_name"`          // required: "roi", "price_btc", "holding_time", "price_sol" etc.
	Operator       string              `json:"operator"`                // required: "<=", ">=", ">", "<", "==", "!="
	Value          *float64            `json:"value,omitempty"`         // optional: uses default if nil (holding_time only)
	Action         string              `json:"action"`                  // "reduce"
	QuantityPct    float64             `json:"quantity_pct"`            // close ratio (1.0 = full position)
	Sort           int                 `json:"sort"`                    // priority
	FallbackRule   *FallbackRuleConfig `json:"fallback_rule,omitempty"` // optional: creates fallback drawdown rule
}

// CreateRuleResponse follows the standard API response format.
type CreateRuleResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    *CreateRuleData `json:"data,omitempty"`
}

// CreateRuleData contains the created rule details.
type CreateRuleData struct {
	ID             uint64            `json:"id"`
	UserStrategyID uint64            `json:"user_strategy_id"`
	ConditionName  string            `json:"condition_name"`
	Operator       string            `json:"operator"`
	Value          float64           `json:"value"`
	Action         string            `json:"action"`
	QuantityPct    float64           `json:"quantity_pct"`
	Sort           int               `json:"sort"`
	Status         string            `json:"status"`
	CreatedAt      string            `json:"created_at"`
	FallbackRule   *FallbackRuleData `json:"fallback_rule,omitempty"` // optional: fallback rule details
}

// FallbackRuleData contains the fallback rule details in the response.
type FallbackRuleData struct {
	ID             uint64  `json:"id"`
	UserStrategyID uint64  `json:"user_strategy_id"`
	ConditionName  string  `json:"condition_name"`
	Operator       string  `json:"operator"`
	Value          float64 `json:"value"`
	Sort           int     `json:"sort"`
	Status         string  `json:"status"`
}

// QueryRuleData is the snake_case response DTO for GET /api/v1/rules.
type QueryRuleData struct {
	ID             uint64                 `json:"id"`
	UserStrategyID uint64                 `json:"user_strategy_id"`
	ConditionName  string                 `json:"condition_name"`
	Operator       string                 `json:"operator"`
	Value          interface{}            `json:"value"`
	Sort           int                    `json:"sort"`
	Status         string                 `json:"status"`
	Action         string                 `json:"action"`
	Params         map[string]interface{} `json:"params"`
	CreatedAt      string                 `json:"created_at"`
	UpdatedAt      string                 `json:"updated_at"`
}

// rulesToQueryData converts []risk.Rule to []QueryRuleData for snake_case JSON output.
func rulesToQueryData(rules []risk.Rule) []QueryRuleData {
	result := make([]QueryRuleData, len(rules))
	for i, r := range rules {
		result[i] = QueryRuleData{
			ID:             uint64(r.ID),
			UserStrategyID: r.UserStrategyID,
			ConditionName:  r.ConditionName,
			Operator:       r.Operator,
			Value:          r.Value,
			Sort:           r.Sort,
			Status:         r.Status,
			Action:         r.Action,
			Params:         r.Params,
			CreatedAt:      r.CreatedAt.Format(time.RFC3339),
			UpdatedAt:      r.UpdatedAt.Format(time.RFC3339),
		}
	}
	return result
}

var validOperators = map[string]bool{
	"<": true, "<=": true, ">": true, ">=": true, "==": true, "!=": true,
}

// APIHandler handles HTTP requests for rule management.
type APIHandler struct {
	ruleStore     *config.RuleStore
	repo          *persistence.StateRepository
	timeStopHours int // from PMConfig.Defaults.TimeStopHours
}

// NewAPIHandler creates a new API handler.
func NewAPIHandler(ruleStore *config.RuleStore, repo *persistence.StateRepository, timeStopHours int) *APIHandler {
	return &APIHandler{
		ruleStore:     ruleStore,
		repo:          repo,
		timeStopHours: timeStopHours,
	}
}

// HandleRegisterRule handles POST /api/rules — uses RuleStore for concurrent-safe writes.
func (h *APIHandler) HandleRegisterRule(w http.ResponseWriter, r *http.Request) {
	// Support both GET (query) and POST (create)
	if r.Method == http.MethodGet {
		h.handleQueryRules(w, r)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.handleCreateRule(w, r)
}

// handleCreateRule handles POST /api/v1/rules to create or update a rule (upsert).
func (h *APIHandler) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var req RegisterRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, 1001, "invalid JSON")
		return
	}

	// Validate required fields
	if err := h.validateRuleRequest(&req); err != nil {
		writeAPIError(w, 1001, err.Error())
		return
	}

	// Validate user_strategy_id exists
	if _, err := h.repo.GetUserStrategyByID(req.UserStrategyID); err != nil {
		writeAPIError(w, 4004, fmt.Sprintf("user_strategy_id %d not found", req.UserStrategyID))
		return
	}

	// Validate active position exists (all rules are for closing positions)
	if h.repo.CountActivePositionsByStrategy(req.UserStrategyID) == 0 {
		writeAPIError(w, 4005, fmt.Sprintf("no active position found for strategy %d", req.UserStrategyID))
		return
	}

	// Resolve value (use default for holding_time)
	value := h.resolveRuleValue(&req)

	// Set defaults
	h.setRuleDefaults(&req)

	// Check if rule with same condition exists (regardless of status)
	existingRule := h.ruleStore.FindRuleByCondition(
		req.UserStrategyID,
		req.ConditionName,
		req.Operator,
	)

	// Handle based on existing rule status
	if existingRule != nil {
		switch existingRule.Status {
		case config.RuleStatusInUse:
			writeAPIError(w, 4003, fmt.Sprintf("rule %d is in use by risk control, cannot update", existingRule.ID))
		case config.RuleStatusActive:
			h.updateExistingRule(w, existingRule, value, req)
		default:
			// inactive or other status - create new
			h.createNewRule(w, req, value)
		}
		return
	}

	// Rule doesn't exist - create new
	h.createNewRule(w, req, value)
}

// updateExistingRule updates an existing active rule
func (h *APIHandler) updateExistingRule(w http.ResponseWriter, existingRule *risk.Rule, value float64, req RegisterRuleRequest) {
	now := time.Now()

	// Check if existing rule has a fallback rule (action is a rule ID)
	var existingFallbackID int
	if fallbackID, err := strconv.Atoi(existingRule.Action); err == nil && fallbackID > 0 {
		existingFallbackID = fallbackID
	}

	// Handle fallback_rule parameter
	var fallbackRule *risk.Rule
	if req.FallbackRule != nil {
		if existingFallbackID > 0 {
			// Update existing fallback rule
			fallbackValue := req.FallbackRule.Value
			if fallbackValue == 0 {
				fallbackValue = 0.05 // Default 5% drawdown
			}
			fallbackUpdates := map[string]interface{}{
				"value":      fallbackValue,
				"updated_at": now,
			}
			if err := h.ruleStore.UpdateRuleFields(existingFallbackID, fallbackUpdates); err != nil {
				writeAPIError(w, 5001, fmt.Sprintf("failed to update fallback rule %d: %v", existingFallbackID, err))
				return
			}
			updatedFallback, _ := h.ruleStore.GetRule(existingFallbackID)
			fallbackRule = updatedFallback
		} else {
			// Create new fallback rule
			var err error
			fallbackRule, err = h.createFallbackRuleIfNeeded(&req)
			if err != nil {
				writeAPIError(w, 5001, err.Error())
				return
			}
		}
	} else if existingFallbackID > 0 {
		// No fallback_rule in request, but rule has one - keep existing fallback
		fallbackRule, _ = h.ruleStore.GetRule(existingFallbackID)
	}

	// Update main rule fields
	updates := map[string]interface{}{
		"value":      value,
		"sort":       req.Sort,
		"updated_at": now,
	}

	// Update action if fallback rule changed
	if req.FallbackRule != nil && fallbackRule != nil {
		updates["action"] = strconv.Itoa(fallbackRule.ID)
	}

	if err := h.ruleStore.UpdateRuleFields(existingRule.ID, updates); err != nil {
		writeAPIError(w, 5001, fmt.Sprintf("failed to update rule %d: %v", existingRule.ID, err))
		return
	}

	// Update params (quantity_pct)
	if req.QuantityPct > 0 {
		updatedRule, _ := h.ruleStore.GetRule(existingRule.ID)
		updatedRule.Params = h.buildRuleParams(req.QuantityPct)
		h.ruleStore.AddRules([]risk.Rule{*updatedRule})

		// Also update fallback rule params if exists
		if fallbackRule != nil {
			fallbackRule.Params = h.buildRuleParams(req.QuantityPct)
			h.ruleStore.AddRules([]risk.Rule{*fallbackRule})
		}
	}

	// Get updated rule
	updatedRule, _ := h.ruleStore.GetRule(existingRule.ID)

	// Return 200 OK for update
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(CreateRuleResponse{
		Code:    0,
		Message: "success",
		Data:    h.ruleToResponseDataWithFallback(*updatedRule, fallbackRule, req.QuantityPct),
	})
}

// createNewRule creates a new rule (original logic)
func (h *APIHandler) createNewRule(w http.ResponseWriter, req RegisterRuleRequest, value float64) {
	// Create fallback rule if provided
	fallbackRule, err := h.createFallbackRuleIfNeeded(&req)
	if err != nil {
		writeAPIError(w, 5001, err.Error())
		return
	}

	// Create main rule
	now := time.Now()
	mainRule := risk.Rule{
		UserStrategyID: req.UserStrategyID,
		ConditionName:  req.ConditionName,
		Operator:       req.Operator,
		Value:          value,
		Sort:           req.Sort,
		Status:         "active",
		Action:         h.mainRuleAction(req.Action, fallbackRule),
		Params:         h.buildRuleParams(req.QuantityPct),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// Atomically assign ID and persist main rule
	if err := h.ruleStore.CreateRule(&mainRule); err != nil {
		// Compensation: delete fallback rule if main rule creation fails
		if fallbackRule != nil {
			h.ruleStore.DeleteRule(fallbackRule.ID)
		}
		writeAPIError(w, 5001, fmt.Sprintf("failed to create rule for strategy %d: %v", req.UserStrategyID, err))
		return
	}

	// Return 201 Created for new rule
	h.writeRuleResponseWithFallback(w, mainRule, fallbackRule, req.QuantityPct)
}

// validateRuleRequest validates required fields
func (h *APIHandler) validateRuleRequest(req *RegisterRuleRequest) error {
	if req.UserStrategyID == 0 {
		return fmt.Errorf("user_strategy_id is required")
	}
	if req.ConditionName == "" {
		return fmt.Errorf("condition_name is required")
	}
	if req.Operator == "" {
		return fmt.Errorf("operator is required")
	}
	if !validOperators[req.Operator] {
		return fmt.Errorf("invalid operator")
	}
	if req.Value == nil && req.ConditionName != "holding_time" {
		return fmt.Errorf("value is required for this condition")
	}

	// Validate position_xxx condition
	if strings.HasPrefix(req.ConditionName, "position_") {
		return h.validatePositionSymbolRule(req)
	}

	return nil
}

// validatePositionSymbolRule validates position_xxx rule creation.
// Normalizes symbol to uppercase before validation and updates req.ConditionName.
func (h *APIHandler) validatePositionSymbolRule(req *RegisterRuleRequest) error {
	symbol := strings.TrimPrefix(req.ConditionName, "position_")

	// Normalize symbol to standard uppercase format
	normalized := engine.NormalizePositionSymbol(symbol)
	req.ConditionName = "position_" + normalized

	if !engine.IsValidPositionSymbol(normalized) {
		return fmt.Errorf("invalid position symbol format: %s", symbol)
	}

	us, err := h.repo.GetUserStrategyByID(req.UserStrategyID)
	if err != nil {
		return fmt.Errorf("user_strategy_id %d not found", req.UserStrategyID)
	}

	// Check if active position exists (use normalized symbol for lookup)
	positions := h.repo.ListUserOrderPositionsByFilter(persistence.UserOrderPositionFilter{
		UserID: us.UserID,
		Asset:  normalized,
	})

	for _, pos := range positions {
		if pos.Deleted == 0 {
			return nil // Found active position
		}
	}

	return fmt.Errorf("no active position found for symbol %s, cannot create position monitoring rule", normalized)
}

// resolveRuleValue resolves the value, using default for holding_time if not provided
func (h *APIHandler) resolveRuleValue(req *RegisterRuleRequest) float64 {
	if req.Value != nil {
		return *req.Value
	}
	// holding_time without value → use default
	return float64(h.timeStopHours) * 3600
}

// setRuleDefaults sets default values for optional fields
func (h *APIHandler) setRuleDefaults(req *RegisterRuleRequest) {
	if req.Action == "" {
		req.Action = "reduce"
	}
	if req.QuantityPct <= 0 {
		req.QuantityPct = 1.0
	}
	if req.Sort <= 0 {
		req.Sort = 1
	}
}

// createFallbackRuleIfNeeded creates a fallback rule if fallback_rule is provided.
// Returns nil if no fallback rule is needed.
func (h *APIHandler) createFallbackRuleIfNeeded(req *RegisterRuleRequest) (*risk.Rule, error) {
	if req.FallbackRule == nil {
		return nil, nil
	}

	// Resolve fallback value (default to 0.05 if not specified)
	fallbackValue := req.FallbackRule.Value
	if fallbackValue == 0 {
		fallbackValue = 0.05 // Default 5% drawdown
	}

	// Create fallback rule
	now := time.Now()
	fallbackRule := &risk.Rule{
		UserStrategyID: req.UserStrategyID,
		ConditionName:  "profit_drawdown_pct",
		Operator:       ">=",
		Value:          fallbackValue,
		Sort:           req.Sort + 1, // Sort follows main rule
		Status:         "inactive",   // Activated when main rule triggers
		Action:         "reduce",
		Params:         h.buildRuleParams(req.QuantityPct),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// Atomically assign ID and persist fallback rule
	if err := h.ruleStore.CreateRule(fallbackRule); err != nil {
		return nil, fmt.Errorf("failed to create fallback rule: %w", err)
	}

	return fallbackRule, nil
}

// buildRuleParams creates standard rule params
func (h *APIHandler) buildRuleParams(quantityPct float64) map[string]interface{} {
	return map[string]interface{}{
		"order_type":   1,
		"quantity_pct": quantityPct,
	}
}

// mainRuleAction determines the action for main rule
func (h *APIHandler) mainRuleAction(action string, fallbackRule *risk.Rule) string {
	if fallbackRule != nil {
		return strconv.Itoa(fallbackRule.ID)
	}
	return action
}

// ruleToResponseData converts a rule to response data format
func (h *APIHandler) ruleToResponseData(rule risk.Rule, quantityPct float64) *CreateRuleData {
	return &CreateRuleData{
		ID:             uint64(rule.ID),
		UserStrategyID: rule.UserStrategyID,
		ConditionName:  rule.ConditionName,
		Operator:       rule.Operator,
		Value:          rule.Value.(float64),
		Action:         rule.Action,
		QuantityPct:    quantityPct,
		Sort:           rule.Sort,
		Status:         rule.Status,
		CreatedAt:      rule.CreatedAt.Format(time.RFC3339),
	}
}

// ruleToResponseDataWithFallback converts a rule to response data format with optional fallback rule
func (h *APIHandler) ruleToResponseDataWithFallback(rule risk.Rule, fallbackRule *risk.Rule, quantityPct float64) *CreateRuleData {
	data := &CreateRuleData{
		ID:             uint64(rule.ID),
		UserStrategyID: rule.UserStrategyID,
		ConditionName:  rule.ConditionName,
		Operator:       rule.Operator,
		Value:          rule.Value.(float64),
		Action:         rule.Action,
		QuantityPct:    quantityPct,
		Sort:           rule.Sort,
		Status:         rule.Status,
		CreatedAt:      rule.CreatedAt.Format(time.RFC3339),
	}

	// Add fallback rule to response if exists
	if fallbackRule != nil {
		data.FallbackRule = &FallbackRuleData{
			ID:             uint64(fallbackRule.ID),
			UserStrategyID: fallbackRule.UserStrategyID,
			ConditionName:  fallbackRule.ConditionName,
			Operator:       fallbackRule.Operator,
			Value:          fallbackRule.Value.(float64),
			Sort:           fallbackRule.Sort,
			Status:         fallbackRule.Status,
		}
	}

	return data
}

// writeRuleResponse writes the standard API response
func (h *APIHandler) writeRuleResponse(w http.ResponseWriter, rule risk.Rule, quantityPct float64) {
	response := CreateRuleResponse{
		Code:    0,
		Message: "success",
		Data:    h.ruleToResponseData(rule, quantityPct),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// writeRuleResponseWithFallback writes the standard API response with optional fallback rule
func (h *APIHandler) writeRuleResponseWithFallback(w http.ResponseWriter, mainRule risk.Rule, fallbackRule *risk.Rule, quantityPct float64) {
	response := CreateRuleResponse{
		Code:    0,
		Message: "success",
		Data: &CreateRuleData{
			ID:             uint64(mainRule.ID),
			UserStrategyID: mainRule.UserStrategyID,
			ConditionName:  mainRule.ConditionName,
			Operator:       mainRule.Operator,
			Value:          mainRule.Value.(float64),
			Action:         mainRule.Action,
			QuantityPct:    quantityPct,
			Sort:           mainRule.Sort,
			Status:         mainRule.Status,
			CreatedAt:      mainRule.CreatedAt.Format(time.RFC3339),
		},
	}

	// Add fallback rule to response if exists
	if fallbackRule != nil {
		response.Data.FallbackRule = &FallbackRuleData{
			ID:             uint64(fallbackRule.ID),
			UserStrategyID: fallbackRule.UserStrategyID,
			ConditionName:  fallbackRule.ConditionName,
			Operator:       fallbackRule.Operator,
			Value:          fallbackRule.Value.(float64),
			Sort:           fallbackRule.Sort,
			Status:         fallbackRule.Status,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// handleQueryRules handles GET /api/v1/rules
func (h *APIHandler) handleQueryRules(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	userIDStr := query.Get("user_id")
	userName := query.Get("user_name")
	strategyName := query.Get("strategy_name")
	userStrategyIDStr := query.Get("user_strategy_id")

	log.Printf("[GET /api/v1/rules] request: user_id=%s, user_name=%s, strategy_name=%s, user_strategy_id=%s",
		userIDStr, userName, strategyName, userStrategyIDStr)

	// Validate: at least one identifier required
	if userIDStr == "" && userName == "" && userStrategyIDStr == "" {
		writeAPIError(w, 1001, "user_id, user_name, or user_strategy_id required")
		return
	}

	// Fast path: direct query by user_strategy_id
	if userStrategyIDStr != "" {
		userStrategyID, err := strconv.ParseUint(userStrategyIDStr, 10, 64)
		if err != nil {
			writeAPIError(w, 1001, "invalid user_strategy_id format")
			return
		}

		// Query all active rules and filter by user_strategy_id
		rules := h.ruleStore.ListActiveRules()
		filtered := make([]risk.Rule, 0)
		for _, rule := range rules {
			if rule.UserStrategyID == userStrategyID {
				filtered = append(filtered, rule)
			}
		}

		log.Printf("[GET /api/v1/rules] SUCCESS: user_strategy_id=%d, found %d rules", userStrategyID, len(filtered))
		writeAPISuccess(w, rulesToQueryData(filtered))
		return
	}

	// Resolve user_id
	var userID uint64
	var err error
	if userIDStr != "" {
		userID, err = strconv.ParseUint(userIDStr, 10, 64)
		if err != nil {
			writeAPIError(w, 1001, "invalid user_id format")
			return
		}
	} else {
		// Find user by name
		userID, err = h.repo.FindUserIDByName(userName, "")
		if err != nil {
			writeAPIError(w, 1001, fmt.Sprintf("User '%s' not found", userName))
			return
		}
	}

	// Query all active rules
	rules := h.ruleStore.ListActiveRules()

	// Get user's strategies to filter rules by UserStrategyID
	userStrategies := h.repo.ListUserStrategiesByUser(userID)

	// Build map of strategy IDs for this user
	strategyIDs := make(map[uint64]bool)
	for _, us := range userStrategies {
		if strategyName == "" || strings.Contains(us.Name, strategyName) {
			strategyIDs[us.ID] = true
		}
	}

	// Filter rules by UserStrategyID
	filtered := make([]risk.Rule, 0)
	for _, rule := range rules {
		if strategyIDs[rule.UserStrategyID] {
			filtered = append(filtered, rule)
		}
	}

	// Return standard response
	log.Printf("[GET /api/v1/rules] SUCCESS: user_id=%d, user_name=%s, strategy_name=%s, found %d rules",
		userID, userName, strategyName, len(filtered))
	writeAPISuccess(w, rulesToQueryData(filtered))
}

func mustMarshalJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// HandleRPCCreateRule handles POST /rpc/v1/rules/create for UOS to create rules via RPC
func (h *APIHandler) HandleRPCCreateRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserStrategyID uint64                 `json:"user_strategy_id"`
		ConditionName  string                 `json:"condition_name"`
		Operator       string                 `json:"operator"`
		Value          interface{}            `json:"value"`
		Sort           int                    `json:"sort"`
		Action         string                 `json:"action"`
		Params         map[string]interface{} `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("[POST /rpc/v1/rules/create] request: user_strategy_id=%d, condition_name=%s, operator=%s, action=%s",
		req.UserStrategyID, req.ConditionName, req.Operator, req.Action)

	// Validate required fields
	if req.UserStrategyID == 0 {
		writeAPIError(w, 1001, "user_strategy_id required")
		return
	}
	if req.ConditionName == "" {
		writeAPIError(w, 1001, "condition_name required")
		return
	}
	if req.Action == "" {
		writeAPIError(w, 1001, "action required")
		return
	}

	// Create rule (ID will be assigned atomically)
	rule := risk.Rule{
		UserStrategyID: req.UserStrategyID,
		ConditionName:  req.ConditionName,
		Operator:       req.Operator,
		Value:          req.Value,
		Sort:           req.Sort,
		Status:         "active",
		Action:         req.Action,
		Params:         req.Params,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := h.ruleStore.CreateRule(&rule); err != nil {
		writeAPIError(w, 5000, fmt.Sprintf("failed to create rule: %v", err))
		return
	}

	// Return success response
	log.Printf("[POST /rpc/v1/rules/create] SUCCESS: rule_id=%d, user_strategy_id=%d", rule.ID, req.UserStrategyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"rule_id": rule.ID,
	})
}

// HandleRPCInvalidateRulesForStrategy handles POST /rpc/v1/rules/invalidate-for-strategy
// Called by UOS when opening a new position to invalidate stale rules.
func (h *APIHandler) HandleRPCInvalidateRulesForStrategy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserStrategyID uint64 `json:"user_strategy_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("[POST /rpc/v1/rules/invalidate-for-strategy] request: user_strategy_id=%d", req.UserStrategyID)

	// Reuse existing GetRulesByUserStrategy + UpdateRuleStatus
	rules := h.ruleStore.GetRulesByUserStrategy(req.UserStrategyID)
	count := 0
	for _, rule := range rules {
		if rule.Status == config.RuleStatusActive {
			if err := h.ruleStore.UpdateRuleStatus(rule.ID, config.RuleStatusInactive); err != nil {
				log.Printf("Warning: failed to invalidate rule %d: %v", rule.ID, err)
			} else {
				count++
			}
		}
	}

	log.Printf("[POST /rpc/v1/rules/invalidate-for-strategy] SUCCESS: invalidated %d rules for strategy %d", count, req.UserStrategyID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":          true,
		"invalidated_count": count,
	})
}
