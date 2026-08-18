package main

import (
	"context"
	"testing"
	// "time" — only used by the commented-out loop test below; restore this
	// import if that test is re-enabled.

	exchangews "trading-service/internal/exchange/ws"
	"trading-service/internal/risk"
)

type fakePriceRuntime struct {
	prices   map[string]float64
	exchange string
	started  int
	stopped  int
}

func (f *fakePriceRuntime) Start(ctx context.Context) error { f.started++; return nil }
func (f *fakePriceRuntime) Snapshot() map[string]float64    { return f.prices }
func (f *fakePriceRuntime) ExchangeName() string            { return f.exchange }
func (f *fakePriceRuntime) Stop()                           { f.stopped++ }

func TestCollectNonMockExchanges(t *testing.T) {
	exchanges := CollectNonMockExchanges([]string{"mock", "binance", "binance", "hyperliquid", ""})
	if len(exchanges) != 2 || !exchanges["binance"] || !exchanges["hyperliquid"] {
		t.Fatalf("unexpected exchanges: %+v", exchanges)
	}
}

func TestBuildPriceRuntimes_BuildsEnabledExchangeRuntimes(t *testing.T) {
	repo := &mockDeribitPositionSource{}
	runtimes := BuildPriceRuntimes(map[string]bool{"binance": true, "hyperliquid": true, "mock": true}, true, repo)
	if len(runtimes) != 2 {
		t.Fatalf("expected 2 runtimes, got %d", len(runtimes))
	}
	if _, ok := runtimes[0].(*BinancePriceRuntime); !ok {
		t.Fatalf("expected first runtime BinancePriceRuntime, got %T", runtimes[0])
	}
	if _, ok := runtimes[1].(*HyperliquidPriceRuntime); !ok {
		t.Fatalf("expected second runtime HyperliquidPriceRuntime, got %T", runtimes[1])
	}
}

func TestBuildPriceRuntimes_BuildsOnlyEnabledExchanges(t *testing.T) {
	repo := &mockDeribitPositionSource{}
	runtimes := BuildPriceRuntimes(map[string]bool{"binance": true}, false, repo)
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}
	if _, ok := runtimes[0].(*BinancePriceRuntime); !ok {
		t.Fatalf("expected BinancePriceRuntime, got %T", runtimes[0])
	}
}

func TestBinancePriceRuntime_ImplementsPriceRuntimeAndSnapshots(t *testing.T) {
	manager := exchangews.NewBinanceWsPriceManager()
	manager.Manager.UpdateFuturesPrice("BTCUSDT", 50000)

	runtime := NewBinancePriceRuntime(manager)
	var _ PriceRuntime = runtime

	prices := runtime.Snapshot()
	if prices["BTCUSDT"] != 50000 {
		t.Fatalf("expected BTCUSDT price 50000, got %+v", prices)
	}
	runtime.Stop()
	runtime.Stop() // idempotent
}

func TestHyperliquidPriceRuntime_SnapshotFromWSManager(t *testing.T) {
	wsMgr := exchangews.NewHyperliquidWsPriceManager()
	wsMgr.Manager.UpdateFuturesPrice("BTC", 50000)
	wsMgr.Manager.UpdateFuturesPrice("ETH", 3000)

	runtime := NewHyperliquidPriceRuntimeFromWS(wsMgr)
	var _ PriceRuntime = runtime

	prices := runtime.Snapshot()
	if len(prices) != 2 {
		t.Fatalf("expected 2 prices, got %d", len(prices))
	}
	if prices["BTC"] != 50000 {
		t.Errorf("expected BTC=50000, got %v", prices["BTC"])
	}
	if prices["ETH"] != 3000 {
		t.Errorf("expected ETH=3000, got %v", prices["ETH"])
	}
}

func TestHyperliquidPriceRuntime_SnapshotReturnsCopy(t *testing.T) {
	wsMgr := exchangews.NewHyperliquidWsPriceManager()
	wsMgr.Manager.UpdateFuturesPrice("BTC", 50000)

	runtime := NewHyperliquidPriceRuntimeFromWS(wsMgr)

	snap1 := runtime.Snapshot()
	snap1["BTC"] = 99999 // mutate copy

	snap2 := runtime.Snapshot()
	if snap2["BTC"] != 50000 {
		t.Errorf("expected BTC=50000 (not mutated), got %v", snap2["BTC"])
	}
}

func TestHyperliquidPriceRuntime_StartWithoutWSManager(t *testing.T) {
	runtime := NewHyperliquidPriceRuntimeFromWS(nil)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("expected no error with nil WS manager, got %v", err)
	}
	runtime.Stop()
}

func TestHyperliquidPriceRuntime_StopIsIdempotent(t *testing.T) {
	runtime := NewHyperliquidPriceRuntimeFromWS(nil)
	runtime.Stop()
	runtime.Stop() // should not panic
}

func TestHyperliquidPriceRuntime_ImplementsPriceRuntime(t *testing.T) {
	runtime := NewHyperliquidPriceRuntimeFromWS(nil)
	var _ PriceRuntime = runtime
}

// Disabled together with StartPriceSnapshotLoop (see price_runtime.go), which
// this test is the only caller of.
//
// func TestStartPriceSnapshotLoop_StartsSyncsAndStopsRuntimes(t *testing.T) {
// 	ctx, cancel := context.WithCancel(context.Background())
// 	state := &risk.GlobalState{}
// 	runtime := &fakePriceRuntime{exchange: "binance", prices: map[string]float64{"BTCUSDT": 50000}}
//
// 	done, err := StartPriceSnapshotLoop(ctx, state, []PriceRuntime{runtime}, time.Millisecond)
// 	if err != nil {
// 		t.Fatalf("StartPriceSnapshotLoop: %v", err)
// 	}
// 	if runtime.started != 1 {
// 		t.Fatalf("expected runtime started once, got %d", runtime.started)
// 	}
// 	if state.Snapshot == nil || state.Snapshot.Prices["binance"]["BTCUSDT"] != 50000 {
// 		t.Fatalf("expected initial sync, got %+v", state.Snapshot)
// 	}
//
// 	runtime.prices["BTCUSDT"] = 51000
// 	for i := 0; i < 50 && state.Snapshot.Prices["binance"]["BTCUSDT"] != 51000; i++ {
// 		time.Sleep(time.Millisecond)
// 	}
// 	if state.Snapshot.Prices["binance"]["BTCUSDT"] != 51000 {
// 		t.Fatalf("expected tick sync to update price, got %+v", state.Snapshot.Prices)
// 	}
//
// 	cancel()
// 	<-done
// 	if runtime.stopped != 1 {
// 		t.Fatalf("expected runtime stopped once, got %d", runtime.stopped)
// 	}
// }

func TestSyncPriceSnapshots_MergesRuntimePricesIntoRiskState(t *testing.T) {
	state := &risk.GlobalState{Snapshot: &risk.MarketSnapshot{Prices: map[string]map[string]float64{"binance": {"OLDUSDT": 1}}}}
	runtimes := []PriceRuntime{
		&fakePriceRuntime{exchange: "binance", prices: map[string]float64{"BTCUSDT": 50000}},
		&fakePriceRuntime{exchange: "hyperliquid", prices: map[string]float64{"ETHUSDT": 3000}},
	}

	SyncPriceSnapshots(state, runtimes)

	if state.Snapshot.Prices["binance"]["BTCUSDT"] != 50000 || state.Snapshot.Prices["hyperliquid"]["ETHUSDT"] != 3000 {
		t.Fatalf("prices not merged: %+v", state.Snapshot.Prices)
	}
	if state.Snapshot.Prices["binance"]["OLDUSDT"] != 1 {
		t.Fatalf("existing price should be preserved, got %+v", state.Snapshot.Prices)
	}
}

func TestSyncPriceSnapshots_LaterRuntimeOverwritesSameSymbol(t *testing.T) {
	state := &risk.GlobalState{Snapshot: &risk.MarketSnapshot{Prices: map[string]map[string]float64{}}}
	SyncPriceSnapshots(state, []PriceRuntime{
		&fakePriceRuntime{exchange: "binance", prices: map[string]float64{"BTCUSDT": 50000}},
		&fakePriceRuntime{exchange: "binance", prices: map[string]float64{"BTCUSDT": 51000}},
	})
	if state.Snapshot.Prices["binance"]["BTCUSDT"] != 51000 {
		t.Fatalf("expected later runtime to overwrite BTCUSDT, got %+v", state.Snapshot.Prices)
	}
}

func TestSyncPriceSnapshots_InitializesNilSnapshot(t *testing.T) {
	state := &risk.GlobalState{}
	SyncPriceSnapshots(state, []PriceRuntime{&fakePriceRuntime{exchange: "binance", prices: map[string]float64{"BTCUSDT": 50000}}})
	if state.Snapshot == nil || state.Snapshot.Prices["binance"]["BTCUSDT"] != 50000 {
		t.Fatalf("expected initialized snapshot with BTC price, got %+v", state.Snapshot)
	}
}
