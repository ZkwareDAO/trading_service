package scheduler

import (
	"context"
	"testing"
	"time"

	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
)

// Test 1: NewRiskScheduler 应该创建调度器
func TestNewRiskScheduler_CreatesScheduler(t *testing.T) {
	cfg := &config.Config{}
	initialState := &risk.GlobalState{Version: 1}

	sched := NewRiskScheduler(initialState, cfg)

	if sched == nil {
		t.Fatal("expected scheduler to be created")
	}
	if sched.GetState().Version != 1 {
		t.Errorf("expected Version 1, got %d", sched.GetState().Version)
	}
}

// Test 2: NewRiskScheduler 应该处理 nil 初始状态
func TestNewRiskScheduler_HandlesNilState(t *testing.T) {
	cfg := &config.Config{}

	sched := NewRiskScheduler(nil, cfg)

	if sched == nil {
		t.Fatal("expected scheduler to be created")
	}
	if sched.GetState() == nil {
		t.Error("expected non-nil state")
	}
}

// Test 3: UpdateState 应该更新状态
func TestRiskScheduler_UpdateState(t *testing.T) {
	sched := NewRiskScheduler(&risk.GlobalState{Version: 1}, &config.Config{})

	newState := &risk.GlobalState{Version: 2}
	sched.UpdateState(newState)

	if sched.GetState().Version != 2 {
		t.Errorf("expected Version 2, got %d", sched.GetState().Version)
	}
}

// Test 4: Start 应该处理信号
func TestRiskScheduler_StartProcessesSignals(t *testing.T) {
	cfg := &config.Config{Rules: []risk.Rule{}}
	state := &risk.GlobalState{Positions: []*risk.UserPosition{}}

	sched := NewRiskScheduler(state, cfg)
	signalCh := sched.SignalChannel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sched.Start(ctx)

	select {
	case signalCh <- risk.RiskSignal{Version: 1}:
	case <-time.After(time.Second):
		t.Error("timeout sending signal")
	}

	time.Sleep(10 * time.Millisecond)
}

// Test 5: Start 应该在 context 取消时退出
func TestRiskScheduler_StartStopsOnContextCancel(t *testing.T) {
	sched := NewRiskScheduler(&risk.GlobalState{}, &config.Config{})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		sched.Start(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("scheduler did not stop on context cancel")
	}
}

// Test 6: SignalChannel 应该返回可写入的通道
func TestRiskScheduler_SignalChannel(t *testing.T) {
	sched := NewRiskScheduler(&risk.GlobalState{}, &config.Config{})

	ch := sched.SignalChannel()
	if ch == nil {
		t.Error("expected non-nil channel")
	}
}