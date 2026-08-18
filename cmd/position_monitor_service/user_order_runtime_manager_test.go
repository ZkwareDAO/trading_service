package main

import (
	"context"
	"testing"
	"time"

	"trading-service/internal/order"
)

type fakeUserOrderRuntime struct {
	started int
	stopped int
}

func (r *fakeUserOrderRuntime) Start(ctx context.Context) error { r.started++; return nil }
func (r *fakeUserOrderRuntime) Stop()                           { r.stopped++ }

type fakeUserOrderRuntimeFactory struct {
	created map[uint64]*fakeUserOrderRuntime
}

func (f *fakeUserOrderRuntimeFactory) NewUserOrderRuntime(user *order.User) (UserOrderRuntime, error) {
	if f.created == nil {
		f.created = make(map[uint64]*fakeUserOrderRuntime)
	}
	runtime := &fakeUserOrderRuntime{}
	f.created[user.ID] = runtime
	return runtime, nil
}

func TestUserOrderRuntimeManager_ReconcileUsersStartsNonMockCredentialedUsers(t *testing.T) {
	factory := &fakeUserOrderRuntimeFactory{}
	manager := NewUserOrderRuntimeManager(factory)
	ctx := context.Background()

	err := manager.ReconcileUsers(ctx, []*order.User{
		{ID: 1, Exchange: "mock"},
		{ID: 2, Exchange: "binance", APIKey: "", APISecret: "secret"},
		{ID: 3, Exchange: "binance", APIKey: "key", APISecret: "secret"},
		{ID: 4, Exchange: "hyperliquid", APISecret: "private"},
	})
	if err != nil {
		t.Fatalf("ReconcileUsers: %v", err)
	}
	if manager.Count() != 2 {
		t.Fatalf("expected 2 active runtimes, got %d", manager.Count())
	}
	if factory.created[3].started != 1 || factory.created[4].started != 1 {
		t.Fatalf("expected users 3 and 4 started, got %+v", factory.created)
	}
}

func TestUserOrderRuntimeManager_ReconcileUsersNoDuplicateAndRestartsCredentialChange(t *testing.T) {
	factory := &fakeUserOrderRuntimeFactory{}
	manager := NewUserOrderRuntimeManager(factory)
	ctx := context.Background()

	user := &order.User{ID: 3, Exchange: "binance", APIKey: "key", APISecret: "secret"}
	if err := manager.ReconcileUsers(ctx, []*order.User{user}); err != nil {
		t.Fatal(err)
	}
	first := factory.created[3]
	if err := manager.ReconcileUsers(ctx, []*order.User{user}); err != nil {
		t.Fatal(err)
	}
	if first.started != 1 || first.stopped != 0 {
		t.Fatalf("unchanged user should not restart: %+v", first)
	}

	changed := &order.User{ID: 3, Exchange: "binance", APIKey: "key2", APISecret: "secret"}
	if err := manager.ReconcileUsers(ctx, []*order.User{changed}); err != nil {
		t.Fatal(err)
	}
	if first.stopped != 1 {
		t.Fatalf("expected old runtime stopped after credential change, got %+v", first)
	}
	if factory.created[3] == first || factory.created[3].started != 1 {
		t.Fatalf("expected new runtime started after credential change")
	}
}

type fakeUserLoader struct {
	calls int
	users [][]*order.User
}

func (l *fakeUserLoader) ListUsers() []*order.User {
	if l.calls >= len(l.users) {
		return nil
	}
	users := l.users[l.calls]
	l.calls++
	return users
}

func TestStartUserOrderRuntimeReconcileLoop_ReconcilesUntilContextCancel(t *testing.T) {
	factory := &fakeUserOrderRuntimeFactory{}
	manager := NewUserOrderRuntimeManager(factory)
	loader := &fakeUserLoader{users: [][]*order.User{
		{{ID: 3, Exchange: "binance", APIKey: "key", APISecret: "secret"}},
		nil,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := StartUserOrderRuntimeReconcileLoop(ctx, manager, loader, time.Millisecond)

	for i := 0; i < 50 && loader.calls < 2; i++ {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if loader.calls < 2 {
		t.Fatalf("expected at least 2 reconcile calls, got %d", loader.calls)
	}
	if factory.created[3].stopped != 1 {
		t.Fatalf("expected runtime stopped after user removed, got %+v", factory.created[3])
	}
}

func TestUserOrderRuntimeManager_ReconcileUsersStopsRemovedUsers(t *testing.T) {
	factory := &fakeUserOrderRuntimeFactory{}
	manager := NewUserOrderRuntimeManager(factory)
	ctx := context.Background()

	if err := manager.ReconcileUsers(ctx, []*order.User{{ID: 3, Exchange: "binance", APIKey: "key", APISecret: "secret"}}); err != nil {
		t.Fatal(err)
	}
	runtime := factory.created[3]
	if err := manager.ReconcileUsers(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if runtime.stopped != 1 {
		t.Fatalf("expected removed user runtime stopped, got %+v", runtime)
	}
	if manager.Count() != 0 {
		t.Fatalf("expected no active runtimes, got %d", manager.Count())
	}
}
