package state

import (
	"sync/atomic"

	"trading-service/internal/risk"
)

// StateStore 原子化的全局状态存储
// 使用 atomic.Value 实现无锁读取，保证并发安全
type StateStore struct {
	value atomic.Value // 存储 *GlobalState
}

// NewStateStore 创建新的状态存储，初始化空状态
func NewStateStore() *StateStore {
	s := &StateStore{}
	s.value.Store(&risk.GlobalState{})
	return s
}

// Get 获取当前状态的副本
// 返回副本以避免外部修改影响内部状态
func (s *StateStore) Get() *risk.GlobalState {
	state := s.value.Load().(*risk.GlobalState)
	return s.deepcopy(state)
}

// Set 设置新状态
func (s *StateStore) Set(state *risk.GlobalState) {
	if state == nil {
		state = &risk.GlobalState{}
	}
	s.value.Store(s.deepcopy(state))
}

// Update 原子更新状态
// updater 函数接收当前状态，返回新状态
func (s *StateStore) Update(updater func(*risk.GlobalState) *risk.GlobalState) {
	for {
		old := s.value.Load().(*risk.GlobalState)
		newState := updater(s.deepcopy(old))
		if newState == nil {
			return // 更新器返回 nil 表示放弃更新
		}
		if s.value.CompareAndSwap(old, newState) {
			return // CAS 成功，更新完成
		}
		// CAS 失败，重试（其他 goroutine 已更新）
	}
}

// Version 获取当前版本号
func (s *StateStore) Version() int64 {
	return s.value.Load().(*risk.GlobalState).Version
}

// deepcopy 创建 GlobalState 的深拷贝
func (s *StateStore) deepcopy(state *risk.GlobalState) *risk.GlobalState {
	if state == nil {
		return &risk.GlobalState{}
	}

	copy := &risk.GlobalState{
		Version: state.Version,
	}

	// 深拷贝 Snapshot
	if state.Snapshot != nil {
		copy.Snapshot = &risk.MarketSnapshot{
			Prices:  make(map[string]map[string]float64),
			Funding: make(map[string]float64),
		}
		for ex, prices := range state.Snapshot.Prices {
			copy.Snapshot.Prices[ex] = make(map[string]float64)
			for k, v := range prices {
				copy.Snapshot.Prices[ex][k] = v
			}
		}
		for k, v := range state.Snapshot.Funding {
			copy.Snapshot.Funding[k] = v
		}
	}

	// 深拷贝 Metrics
	if state.Metrics != nil {
		copy.Metrics = &risk.GlobalMetrics{
			BTCVolatility: state.Metrics.BTCVolatility,
			MarketTrend:   state.Metrics.MarketTrend,
		}
	}

	// 深拷贝 Positions
	if state.Positions != nil {
		copy.Positions = make([]*risk.UserPosition, len(state.Positions))
		for i, p := range state.Positions {
			if p != nil {
				posCopy := *p
				copy.Positions[i] = &posCopy
			}
		}
	}

	return copy
}