package state

import (
	"sync"
	"testing"

	"trading-service/internal/risk"
)

// Test 1: StateStore 应该能够存储和读取 GlobalState
func TestStateStore_GetAndSet(t *testing.T) {
	store := NewStateStore()

	initial := &risk.GlobalState{
		Version: 1,
		Snapshot: &risk.MarketSnapshot{
			Prices:  map[string]map[string]float64{"binance": {"BTCUSDT": 50000.0}},
			Funding: map[string]float64{},
		},
	}

	store.Set(initial)

	got := store.Get()
	if got.Version != 1 {
		t.Errorf("expected Version 1, got %d", got.Version)
	}
	if got.Snapshot.Prices["binance"]["BTCUSDT"] != 50000.0 {
		t.Errorf("expected BTCUSDT price 50000.0, got %f", got.Snapshot.Prices["binance"]["BTCUSDT"])
	}
}

// Test 2: StateStore 应该支持原子更新
func TestStateStore_Update(t *testing.T) {
	store := NewStateStore()

	initial := &risk.GlobalState{
		Version: 1,
		Positions: []*risk.UserPosition{
			{ID: 1, Symbol: "BTCUSDT"},
		},
	}
	store.Set(initial)

	// 原子更新
	store.Update(func(s *risk.GlobalState) *risk.GlobalState {
		return &risk.GlobalState{
			Version:   s.Version + 1,
			Positions: append(s.Positions, &risk.UserPosition{ID: 2, Symbol: "ETHUSDT"}),
		}
	})

	got := store.Get()
	if got.Version != 2 {
		t.Errorf("expected Version 2, got %d", got.Version)
	}
	if len(got.Positions) != 2 {
		t.Errorf("expected 2 positions, got %d", len(got.Positions))
	}
}

// Test 3: StateStore 应该是并发安全的
func TestStateStore_ConcurrentAccess(t *testing.T) {
	store := NewStateStore()

	store.Set(&risk.GlobalState{Version: 0})

	var wg sync.WaitGroup
	const goroutines = 100

	// 并发更新
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Update(func(s *risk.GlobalState) *risk.GlobalState {
				return &risk.GlobalState{
					Version: s.Version + 1,
				}
			})
		}()
	}

	// 并发读取
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.Get()
		}()
	}

	wg.Wait()

	// 最终版本应该是 goroutines（每次更新+1）
	got := store.Get()
	if got.Version != int64(goroutines) {
		t.Errorf("expected Version %d, got %d", goroutines, got.Version)
	}
}

// Test 4: StateStore 应该返回状态的副本，避免外部修改
func TestStateStore_ReturnsCopy(t *testing.T) {
	store := NewStateStore()

	store.Set(&risk.GlobalState{
		Version: 1,
		Positions: []*risk.UserPosition{
			{ID: 1, Symbol: "BTCUSDT"},
		},
	})

	// 获取状态并修改
	got1 := store.Get()
	got1.Positions[0].Symbol = "MODIFIED"

	// 再次获取，应该不受影响
	got2 := store.Get()
	if got2.Positions[0].Symbol == "MODIFIED" {
		t.Error("Get() should return a copy, external modification affected internal state")
	}
}

// Test 5: StateStore 应该支持版本检查
func TestStateStore_VersionCheck(t *testing.T) {
	store := NewStateStore()

	store.Set(&risk.GlobalState{Version: 5})

	if store.Version() != 5 {
		t.Errorf("expected Version 5, got %d", store.Version())
	}
}

// Test 6: StateStore 应该支持初始化空状态
func TestStateStore_EmptyInitial(t *testing.T) {
	store := NewStateStore()

	got := store.Get()
	if got == nil {
		t.Error("Get() should not return nil, should return empty state")
	}
	if got.Version != 0 {
		t.Errorf("expected initial Version 0, got %d", got.Version)
	}
}