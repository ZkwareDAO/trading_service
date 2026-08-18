package engine

import (
	"os"
	"path/filepath"
	"testing"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk"
)

// TestPositionCondition_TriggerAfterClose 测试平仓后触发规则
func TestPositionCondition_TriggerAfterClose(t *testing.T) {
	// 创建临时目录和CSV文件
	tmpDir := t.TempDir()
	createTestCSVFiles(tmpDir)

	gs, err := persistence.NewGlobalState(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)

	// 创建平仓记录（deleted=1）
	pos := &order.UserOrderPosition{
		UserID:         100,
		UserStrategyID: 1,
		Exchange:       "binance",
		Asset:          "BTCUSDT",
		CurrentPrice:   50000.0,
		Quantity:       0.1,
		Deleted:        1, // 已平仓
	}
	repo.CreateUserOrderPosition(pos)

	// 创建RuleEngine with repo
	eng := NewRuleEngineWithRepo(repo)

	// 创建position_BTCUSDT规则：active_count == 0时触发
	rule := risk.Rule{
		ID:             1,
		UserStrategyID: 1,
		ConditionName:  "position_BTCUSDT",
		Operator:       "==",
		Value:          float64(0), // active count = 0
		Status:         "active",
		Action:         "reduce",
	}

	// 创建RiskContext
	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{
			UserID:         100,
			UserStrategyID: 1,
		},
	}

	// 执行规则评估
	triggered := eng.EvaluateCondition(rule, ctx)

	// 验证规则触发
	if !triggered {
		t.Error("position rule should trigger when active_count=0 and has_deleted_record=true")
	}

	t.Logf("✅ 平仓后规则正确触发: active_count=0, has_deleted_record=true")
}

// TestPositionCondition_NotTriggerWhenHolding 测试持仓时不触发
func TestPositionCondition_NotTriggerWhenHolding(t *testing.T) {
	tmpDir := t.TempDir()
	createTestCSVFiles(tmpDir)

	gs, err := persistence.NewGlobalState(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)

	// 创建活跃仓位（deleted=0）
	pos := &order.UserOrderPosition{
		UserID:         100,
		UserStrategyID: 1,
		Exchange:       "binance",
		Asset:          "BTCUSDT",
		CurrentPrice:   50000.0,
		Quantity:       0.1,
		Deleted:        0, // 活跃仓位
	}
	repo.CreateUserOrderPosition(pos)

	eng := NewRuleEngineWithRepo(repo)

	rule := risk.Rule{
		ID:             1,
		UserStrategyID: 1,
		ConditionName:  "position_BTCUSDT",
		Operator:       "==",
		Value:          float64(0),
		Status:         "active",
		Action:         "reduce",
	}

	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{
			UserID:         100,
			UserStrategyID: 1,
		},
	}

	triggered := eng.EvaluateCondition(rule, ctx)

	// 验证规则不触发（active_count=1 != 0）
	if triggered {
		t.Error("position rule should NOT trigger when active_count > 0")
	}

	t.Logf("✅ 持仓时规则不触发: active_count=1, value=0 (condition not met)")
}

// TestPositionCondition_MultiplePositions 测试多仓位场景
func TestPositionCondition_MultiplePositions(t *testing.T) {
	tmpDir := t.TempDir()
	createTestCSVFiles(tmpDir)

	gs, err := persistence.NewGlobalState(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)

	// 创建2个仓位：1个平仓，1个活跃
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:       100,
		Exchange:     "binance",
		Asset:        "BTCUSDT",
		CurrentPrice: 50000.0,
		Deleted:      1, // 已平仓
	})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:       100,
		Exchange:     "binance",
		Asset:        "BTCUSDT",
		CurrentPrice: 51000.0,
		Deleted:      0, // 仍活跃
	})

	eng := NewRuleEngineWithRepo(repo)

	rule := risk.Rule{
		ConditionName: "position_BTCUSDT",
		Operator:      "==",
		Value:         float64(0),
		Status:        "active",
	}

	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{UserID: 100},
	}

	triggered := eng.EvaluateCondition(rule, ctx)

	// active_count=1, value=0，不触发
	if triggered {
		t.Error("rule should NOT trigger when active_count=1 (not equal to 0)")
	}

	t.Logf("✅ 多仓位场景: active_count=1, has_deleted_record=true, 但条件不满足")
}

// TestPositionCondition_NoPosition 测试无仓位不触发
func TestPositionCondition_NoPosition(t *testing.T) {
	tmpDir := t.TempDir()
	createTestCSVFiles(tmpDir)

	gs, err := persistence.NewGlobalState(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)

	// 无任何仓位数据

	eng := NewRuleEngineWithRepo(repo)

	rule := risk.Rule{
		ConditionName: "position_ETHUSDT",
		Operator:      "==",
		Value:         float64(0),
		Status:        "active",
	}

	ctx := &risk.RiskContext{
		Position: &risk.UserPosition{UserID: 100},
	}

	triggered := eng.EvaluateCondition(rule, ctx)

	// 无仓位记录，不触发（has_deleted_record=false）
	if triggered {
		t.Error("rule should NOT trigger when no position record exists")
	}

	t.Logf("✅ 无仓位记录不触发: has_deleted_record=false")
}

// createTestCSVFiles 创建测试所需的CSV文件
func createTestCSVFiles(tmpDir string) {
	// 创建必要的CSV文件头
	files := map[string]string{
		"user_order_positions.csv": "id,user_id,uprunning_order_id,user_order_id,user_strategy_id,risk_control_strategy_id,exchange,pos_type,asset,current_price,quantity,pos_value,leverage,deleted,init_margin,pos_price,pnl_value,side,close_time,created_at,updated_at\n",
		"user_strategies.csv":      "id,user_id,name,exchange,valid_before,cash,parts,status,strategy_id,risk_strategy_type,orders_num,created_at,updated_at\n",
	}

	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		os.WriteFile(path, []byte(content), 0644)
	}
}
