package main

import (
	"context"
	"testing"

	"trading-service/internal/order"
)

type fakeListenKeyService struct {
	listenKey string
	started   int
	keptAlive int
}

func (f *fakeListenKeyService) Start(ctx context.Context) (string, error) {
	f.started++
	return f.listenKey, nil
}
func (f *fakeListenKeyService) Keepalive(ctx context.Context, listenKey string) error {
	f.keptAlive++
	return nil
}

type fakeOrderMonitorStarter struct {
	listenKey string
	started   int
	stopped   int
}

func (f *fakeOrderMonitorStarter) StartFuturesUserDataWS(listenKeyProvider func() (string, error)) {
	f.started++
	if key, err := listenKeyProvider(); err == nil {
		f.listenKey = key
	}
}
func (f *fakeOrderMonitorStarter) Stop() { f.stopped++ }

func TestBinanceUserOrderRuntime_StartsListenKeyAndMonitor(t *testing.T) {
	listenKeys := &fakeListenKeyService{listenKey: "listen-key"}
	monitor := &fakeOrderMonitorStarter{}
	runtime := NewBinanceUserOrderRuntimeForTest(listenKeys, monitor)
	var _ UserOrderRuntime = runtime

	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if listenKeys.started != 1 {
		t.Fatalf("expected listen key started once, got %d", listenKeys.started)
	}
	if monitor.started != 1 || monitor.listenKey != "listen-key" {
		t.Fatalf("expected monitor started with listen-key, got %+v", monitor)
	}
	runtime.Stop()
	if monitor.stopped != 1 {
		t.Fatalf("expected monitor stopped once, got %d", monitor.stopped)
	}
}

func TestBinanceUserOrderRuntimeFactory_CreatesRuntimeForBinanceUser(t *testing.T) {
	factory := NewBinanceUserOrderRuntimeFactory(nil, "http://localhost:8081", false)
	runtime, err := factory.NewUserOrderRuntime(&order.User{ID: 1, Exchange: "binance", APIKey: "key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("NewUserOrderRuntime: %v", err)
	}
	if runtime == nil {
		t.Fatal("expected runtime")
	}
}
