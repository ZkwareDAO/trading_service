package config

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"trading-service/internal/risk"
)

const (
	RuleStatusActive   = "active"
	RuleStatusInactive = "inactive"
	RuleStatusInUse    = "in_use"
)

// RuleStore manages risk rule state in memory and rule.csv.
type RuleStore struct {
	mu      sync.RWMutex
	dataDir string
	rules   map[int]*risk.Rule
}

func NewRuleStore(dataDir string) (*RuleStore, error) {
	loader := NewConfigLoader(dataDir)
	cfg, err := loader.LoadAll()
	if err != nil {
		return nil, err
	}
	rules := make(map[int]*risk.Rule, len(cfg.Rules))
	for i := range cfg.Rules {
		rule := cfg.Rules[i]
		rules[rule.ID] = &rule
	}
	return &RuleStore{dataDir: dataDir, rules: rules}, nil
}

func (s *RuleStore) GetRule(id int) (*risk.Rule, bool) {
	s.mu.RLock()
	rule, ok := s.rules[id]
	s.mu.RUnlock()
	if ok {
		copyRule := copyRule(rule)
		return &copyRule, true
	}

	// Fallback: reload from CSV in case the rule was added externally
	// (e.g., buy_close signal appending an immediate reduce rule).
	loader := NewConfigLoader(s.dataDir)
	cfg, err := loader.LoadAll()
	if err != nil {
		return nil, false
	}
	for i := range cfg.Rules {
		if cfg.Rules[i].ID == id {
			return &cfg.Rules[i], true
		}
	}
	return nil, false
}

func (s *RuleStore) ListRules() []risk.Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]risk.Rule, 0, len(s.rules))
	for _, rule := range s.rules {
		result = append(result, copyRule(rule))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *RuleStore) ListActiveRules() []risk.Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]risk.Rule, 0)
	for _, rule := range s.rules {
		if rule.Status == RuleStatusActive {
			result = append(result, copyRule(rule))
		}
	}
	return result
}

// NextID returns the next available rule ID (auto-increment).
// It checks both in-memory rules and CSV to avoid ID conflicts across services.
// Uses write lock to prevent concurrent ID generation.
// Deprecated: Use CreateRule() for atomic ID allocation + insertion.
func (s *RuleStore) NextID() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	nextID := s.nextIDLocked()
	log.Printf("RuleStore.NextID: returning nextID=%d", nextID)
	return nextID
}

// CreateRule atomically assigns a new ID and adds the rule to the store.
// This prevents race conditions between ID allocation and rule insertion.
// The rule's ID field will be set by this method.
func (s *RuleStore) CreateRule(rule *risk.Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Atomically: allocate ID + insert rule + persist
	rule.ID = s.nextIDLocked()
	s.rules[rule.ID] = rule
	return s.writeLocked()
}

// nextIDLocked returns the next available ID (must be called with lock held).
func (s *RuleStore) nextIDLocked() int {
	maxID := 0
	for id := range s.rules {
		if id > maxID {
			maxID = id
		}
	}

	// Also check CSV for rules added by other services
	loader := NewConfigLoader(s.dataDir)
	cfg, err := loader.LoadAll()
	if err == nil {
		csvMaxID := 0
		for _, rule := range cfg.Rules {
			if rule.ID > csvMaxID {
				csvMaxID = rule.ID
			}
		}
		if csvMaxID > maxID {
			maxID = csvMaxID
		}
	}

	return maxID + 1
}

func (s *RuleStore) UpdateRuleStatus(id int, status string) error {
	if !isValidRuleStatus(status) {
		return fmt.Errorf("invalid rule status: %s", status)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	rule, ok := s.rules[id]
	if !ok {
		return fmt.Errorf("rule %d not found", id)
	}
	if rule.Status == status {
		return nil
	}
	rule.Status = status

	// Cascade update: update all rules with same (user_strategy_id, condition_name, operator, value)
	for _, otherRule := range s.rules {
		if otherRule.ID != id &&
			otherRule.UserStrategyID == rule.UserStrategyID &&
			otherRule.ConditionName == rule.ConditionName &&
			otherRule.Operator == rule.Operator &&
			fmt.Sprintf("%v", otherRule.Value) == fmt.Sprintf("%v", rule.Value) {
			otherRule.Status = status
		}
	}

	return s.writeLocked()
}

// AddRules adds new rules to the store and persists to CSV.
func (s *RuleStore) AddRules(rules []risk.Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range rules {
		s.rules[rules[i].ID] = &rules[i]
	}
	return s.writeLocked()
}

// HasRulesForStrategy checks if any rules exist for a given strategy.
func (s *RuleStore) HasRulesForStrategy(strategyID uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rule := range s.rules {
		if rule.UserStrategyID == strategyID {
			return true
		}
	}
	return false
}

func (s *RuleStore) HasActiveRulesForStrategy(strategyID uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rule := range s.rules {
		if rule.UserStrategyID == strategyID && rule.Status == RuleStatusActive {
			return true
		}
	}
	return false
}

// DeleteRule removes a rule from the store.
// Used for compensation when rollback is needed.
func (s *RuleStore) DeleteRule(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.rules[id]; !ok {
		return fmt.Errorf("rule %d not found", id)
	}

	delete(s.rules, id)
	return s.writeLocked()
}

// GetRulesByUserStrategy returns all rules for a given user_strategy_id.
func (s *RuleStore) GetRulesByUserStrategy(strategyID uint64) []risk.Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]risk.Rule, 0)
	for _, rule := range s.rules {
		if rule.UserStrategyID == strategyID {
			result = append(result, copyRule(rule))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// FindActiveRuleByCondition finds an active rule matching the condition.
// Returns nil if not found.
func (s *RuleStore) FindActiveRuleByCondition(strategyID uint64, conditionName, operator string) *risk.Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, rule := range s.rules {
		if rule.UserStrategyID == strategyID &&
			rule.ConditionName == conditionName &&
			rule.Operator == operator &&
			rule.Status == RuleStatusActive {
			copy := copyRule(rule)
			return &copy
		}
	}
	return nil
}

// FindRuleByCondition finds a rule matching the condition regardless of status.
// Returns the rule and its status, or nil if not found.
func (s *RuleStore) FindRuleByCondition(strategyID uint64, conditionName, operator string) *risk.Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, rule := range s.rules {
		if rule.UserStrategyID == strategyID &&
			rule.ConditionName == conditionName &&
			rule.Operator == operator {
			copy := copyRule(rule)
			return &copy
		}
	}
	return nil
}

// UpdateRuleFields updates specific fields of an existing rule.
// Does NOT update status field (preserves risk execution state).
func (s *RuleStore) UpdateRuleFields(id int, updates map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rule, ok := s.rules[id]
	if !ok {
		return fmt.Errorf("rule %d not found", id)
	}

	// Update only allowed fields
	for key, value := range updates {
		switch key {
		case "value":
			if v, ok := value.(float64); ok {
				rule.Value = v
			}
		case "sort":
			if v, ok := value.(int); ok {
				rule.Sort = v
			}
		case "action":
			if v, ok := value.(string); ok {
				rule.Action = v
			}
		case "updated_at":
			if v, ok := value.(time.Time); ok {
				rule.UpdatedAt = v
			}
		}
	}

	return s.writeLocked()
}

// ResetRulesForStrategy sets all rules for the given strategy to "inactive".
// This is called when a risk control order is filled, resetting the strategy's
// rule monitoring cycle.
func (s *RuleStore) ResetRulesForStrategy(strategyID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, rule := range s.rules {
		if rule.UserStrategyID == strategyID && rule.Status != "inactive" {
			rule.Status = "inactive"
			changed = true
		}
	}
	if changed {
		return s.writeLocked()
	}
	return nil
}

func (s *RuleStore) replaceRulesLocked(rules []risk.Rule) {
	next := make(map[int]*risk.Rule, len(rules))
	for i := range rules {
		rule := rules[i]
		next[rule.ID] = &rule
	}
	s.rules = next
}

func (s *RuleStore) SnapshotConfig() *Config {
	// Reload from CSV each time so manual edits take effect immediately.
	// Other operations (AddRules, UpdateRuleStatus, GetRule) still use the
	// in-memory map, so programmatic updates remain consistent.
	loader := NewConfigLoader(s.dataDir)
	cfg, err := loader.LoadAll()
	if err != nil {
		// Fallback to memory if file is unreadable.
		s.mu.RLock()
		defer s.mu.RUnlock()
		rules := make([]risk.Rule, 0, len(s.rules))
		for _, rule := range s.rules {
			rules = append(rules, copyRule(rule))
		}
		return &Config{Rules: rules, UserStrategies: make(map[uint64]*UserStrategyInfo)}
	}
	s.mu.Lock()
	s.replaceRulesLocked(cfg.Rules)
	s.mu.Unlock()
	return &Config{Rules: cfg.Rules, UserStrategies: make(map[uint64]*UserStrategyInfo)}
}

func (s *RuleStore) writeLocked() error {
	path := filepath.Join(s.dataDir, "rule.csv")
	lockPath := path + ".lock"

	// Acquire file lock for cross-process synchronization
	lockFile, err := os.Create(lockPath)
	if err != nil {
		return fmt.Errorf("create lock file: %w", err)
	}
	defer func() {
		lockFile.Close()
		os.Remove(lockPath)
	}()

	// Exclusive lock (blocks until acquired)
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	log.Printf("RuleStore.writeLocked: acquired lock, memory rules=%d", len(s.rules))

	// Read existing rules from CSV to merge with in-memory state
	// This prevents overwriting rules added by other services
	loader := NewConfigLoader(s.dataDir)
	cfg, err := loader.LoadAll()
	if err == nil && len(cfg.Rules) > 0 {
		log.Printf("RuleStore.writeLocked: CSV has %d rules, merging", len(cfg.Rules))
		mergedCount := 0
		// Merge: prefer in-memory rules, but add any new rules from CSV
		for _, csvRule := range cfg.Rules {
			if _, exists := s.rules[csvRule.ID]; !exists {
				s.rules[csvRule.ID] = &csvRule
				mergedCount++
			}
		}
		log.Printf("RuleStore.writeLocked: merged %d new rules from CSV, total rules=%d", mergedCount, len(s.rules))
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	// Updated header with timestamp fields
	if err := writer.Write([]string{"id", "user_strategy_id", "condition_name", "operator", "value", "sort", "status", "action", "params", "created_at", "updated_at"}); err != nil {
		return err
	}
	ruleIDs := make([]int, 0, len(s.rules))
	for id := range s.rules {
		ruleIDs = append(ruleIDs, id)
	}
	sort.Ints(ruleIDs)
	for _, id := range ruleIDs {
		rule := s.rules[id]
		params := ""
		if len(rule.Params) > 0 {
			b, err := json.Marshal(rule.Params)
			if err != nil {
				return err
			}
			params = string(b)
		}
		// Format timestamps as RFC3339
		createdAt := ""
		if !rule.CreatedAt.IsZero() {
			createdAt = rule.CreatedAt.Format(time.RFC3339)
		}
		updatedAt := ""
		if !rule.UpdatedAt.IsZero() {
			updatedAt = rule.UpdatedAt.Format(time.RFC3339)
		}
		if err := writer.Write([]string{
			strconv.Itoa(rule.ID),
			strconv.FormatUint(rule.UserStrategyID, 10),
			rule.ConditionName,
			rule.Operator,
			formatRuleValue(rule.Value),
			strconv.Itoa(rule.Sort),
			rule.Status,
			rule.Action,
			params,
			createdAt,
			updatedAt,
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func isValidRuleStatus(status string) bool {
	switch status {
	case RuleStatusActive, RuleStatusInactive, RuleStatusInUse:
		return true
	default:
		return false
	}
}

func copyRule(rule *risk.Rule) risk.Rule {
	copyRule := *rule
	if rule.Params != nil {
		copyRule.Params = make(map[string]interface{}, len(rule.Params))
		for key, value := range rule.Params {
			copyRule.Params[key] = value
		}
	}
	return copyRule
}

func formatRuleValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}
