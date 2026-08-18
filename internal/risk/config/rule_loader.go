package config

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"trading-service/internal/risk"
)

// Config 聚合所有配置（单一 rule.csv + user_strategies 只读）
type Config struct {
	Rules          []risk.Rule
	UserStrategies map[uint64]*UserStrategyInfo
}

// UserStrategyInfo 用户策略信息
type UserStrategyInfo struct {
	ID               uint64
	UserID           uint64
	Name             string
	Exchange         string
	Status           int    // 1=active
	RiskStrategyType string // "traditional" or "cta_intraday"
}

// GetRulesByStrategy returns active rules for a strategy, sorted by Sort ascending (1=highest priority).
func (c *Config) GetRulesByStrategy(strategyID uint64) []risk.Rule {
	var result []risk.Rule
	for _, r := range c.Rules {
		if r.UserStrategyID == strategyID && r.Status == "active" {
			result = append(result, r)
		}
	}
	return result
}

// GetRuleByID returns a rule by ID.
func (c *Config) GetRuleByID(id int) *risk.Rule {
	for i := range c.Rules {
		if c.Rules[i].ID == id {
			return &c.Rules[i]
		}
	}
	return nil
}

// GetStrategyInfo returns strategy info.
func (c *Config) GetStrategyInfo(strategyID uint64) *UserStrategyInfo {
	return c.UserStrategies[strategyID]
}

// ConfigLoader loads configuration from CSV files.
type ConfigLoader struct {
	dataDir string
}

// NewConfigLoader creates a new config loader.
func NewConfigLoader(dataDir string) *ConfigLoader {
	return &ConfigLoader{dataDir: dataDir}
}

// LoadAll loads all configuration.
func (l *ConfigLoader) LoadAll() (*Config, error) {
	rules, err := l.loadRules()
	if err != nil {
		return nil, fmt.Errorf("load rules: %w", err)
	}

	userStrats, err := l.loadUserStrategies()
	if err != nil {
		return nil, fmt.Errorf("load user_strategies: %w", err)
	}

	return &Config{
		Rules:          rules,
		UserStrategies: userStrats,
	}, nil
}

// loadRules loads rule.csv (new format: id,user_strategy_id,condition_name,operator,value,sort,status,action,params,created_at,updated_at)
func (l *ConfigLoader) loadRules() ([]risk.Rule, error) {
	file := filepath.Join(l.dataDir, "rule.csv")
	f, err := os.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open rule.csv: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read rule.csv: %w", err)
	}

	if len(records) < 2 {
		return nil, nil
	}

	var allRules []risk.Rule
	for _, record := range records[1:] {
		if len(record) < 9 {
			continue
		}
		id, _ := strconv.Atoi(record[0])
		userStrategyID, _ := strconv.ParseUint(record[1], 10, 64)
		conditionName := strings.TrimSpace(record[2])
		operator := strings.TrimSpace(record[3])
		value := parseRuleValue(strings.TrimSpace(record[4]))
		sort, _ := strconv.Atoi(record[5])
		status := strings.TrimSpace(record[6])
		action := strings.TrimSpace(record[7])
		paramsRaw := strings.TrimSpace(record[8])

		params := make(map[string]interface{})
		if paramsRaw != "" {
			if err := json.Unmarshal([]byte(paramsRaw), &params); err != nil {
				params = make(map[string]interface{})
			}
		}

		// Parse timestamps (backward compatible: use defaults if missing)
		var createdAt, updatedAt time.Time
		if len(record) >= 10 {
			createdAt, _ = time.Parse(time.RFC3339, strings.TrimSpace(record[9]))
		}
		if len(record) >= 11 {
			updatedAt, _ = time.Parse(time.RFC3339, strings.TrimSpace(record[10]))
		}
		// If timestamps are missing, use current time as default
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		if updatedAt.IsZero() {
			updatedAt = createdAt
		}

		allRules = append(allRules, risk.Rule{
			ID:             id,
			UserStrategyID: userStrategyID,
			ConditionName:  conditionName,
			Operator:       operator,
			Value:          value,
			Sort:           sort,
			Status:         status,
			Action:         action,
			Params:         params,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		})
	}

	// Deduplicate: keep the LAST rule for each (user_strategy_id, condition_name, operator, value) combination
	// This ensures newer rules (with higher IDs) override older rules
	seen := make(map[string]int) // key: "strategyID|condition|operator|value" -> latest rule index
	for i, rule := range allRules {
		key := fmt.Sprintf("%d|%s|%s|%v", rule.UserStrategyID, rule.ConditionName, rule.Operator, rule.Value)
		seen[key] = i // Always update to latest index
	}

	// Build deduplicated list from seen map
	var rules []risk.Rule
	for _, idx := range seen {
		rules = append(rules, allRules[idx])
	}
	return rules, nil
}

// loadUserStrategies loads user_strategies.csv for risk_strategy_type lookup.
func (l *ConfigLoader) loadUserStrategies() (map[uint64]*UserStrategyInfo, error) {
	file := filepath.Join(l.dataDir, "user_strategies.csv")
	f, err := os.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[uint64]*UserStrategyInfo), nil
		}
		return nil, fmt.Errorf("open user_strategies.csv: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read user_strategies.csv: %w", err)
	}

	if len(records) < 2 {
		return make(map[uint64]*UserStrategyInfo), nil
	}

	result := make(map[uint64]*UserStrategyInfo)
	for _, record := range records[1:] {
		if len(record) < 12 {
			continue
		}
		id, _ := strconv.ParseUint(record[0], 10, 64)
		userID, _ := strconv.ParseUint(record[1], 10, 64)
		status, _ := strconv.Atoi(record[8])

		result[id] = &UserStrategyInfo{
			ID:               id,
			UserID:           userID,
			Name:             record[2],
			Exchange:         record[3],
			Status:           status,
			RiskStrategyType: record[10],
		}
	}

	return result, nil
}

// parseRuleValue parses a rule.csv value field.
func parseRuleValue(raw string) interface{} {
	if raw == "" {
		return ""
	}
	if b, err := strconv.ParseBool(strings.ToLower(raw)); err == nil {
		return b
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f
	}
	if i, err := strconv.Atoi(raw); err == nil {
		return i
	}
	return strings.Trim(raw, "\"'")
}
