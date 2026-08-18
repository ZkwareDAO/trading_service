package scheduler

import (
	"context"

	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
	"trading-service/internal/risk/pipeline"
)

// RiskScheduler 风控调度器
type RiskScheduler struct {
	store    *risk.GlobalState
	config   *config.Config
	pipeline *pipeline.RiskPipeline
	signalCh chan risk.RiskSignal
}

// NewRiskScheduler 创建调度器
func NewRiskScheduler(initialState *risk.GlobalState, cfg *config.Config) *RiskScheduler {
	if initialState == nil {
		initialState = &risk.GlobalState{}
	}
	return &RiskScheduler{
		store:    initialState,
		config:   cfg,
		pipeline: pipeline.NewRiskPipeline(),
		signalCh: make(chan risk.RiskSignal, 100),
	}
}

// SignalChannel 返回信号通道
func (s *RiskScheduler) SignalChannel() chan<- risk.RiskSignal {
	return s.signalCh
}

// Start 启动调度器
func (s *RiskScheduler) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case signal := <-s.signalCh:
			s.processSignal(signal)
		}
	}
}

// processSignal 处理信号
func (s *RiskScheduler) processSignal(signal risk.RiskSignal) {
	_ = s.pipeline.Run(s.store, s.config)
}

// GetState 获取当前状态
func (s *RiskScheduler) GetState() *risk.GlobalState {
	return s.store
}

// UpdateState 更新状态
func (s *RiskScheduler) UpdateState(state *risk.GlobalState) {
	s.store = state
}

// RunPipeline 运行风控管道并返回结果
func (s *RiskScheduler) RunPipeline(state *risk.GlobalState, cfg *config.Config) []pipeline.PipelineResult {
	return s.pipeline.Run(state, cfg)
}