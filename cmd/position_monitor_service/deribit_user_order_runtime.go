package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"trading-service/internal/exchange"
	"trading-service/internal/exchange/deribit"
	exchangews "trading-service/internal/exchange/ws"
	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

// DeribitUserOrderRuntime manages WebSocket connection for a single Deribit user.
// Mirrors BinanceUserOrderRuntime and HyperliquidUserOrderRuntime patterns.
type DeribitUserOrderRuntime struct {
	userID   uint64
	testnet  bool
	deribit  *deribit.Deribit
	monitor  *exchangews.DeribitOrderMonitor
	executor *exchange.OrderExecutor
	repo     *persistence.StateRepository
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewDeribitUserOrderRuntimeForTest creates a runtime with injected executor and repo.
func NewDeribitUserOrderRuntimeForTest(repo *persistence.StateRepository, executor *exchange.OrderExecutor) *DeribitUserOrderRuntime {
	return &DeribitUserOrderRuntime{repo: repo, executor: executor}
}

// DeribitUserOrderRuntimeFactory creates DeribitUserOrderRuntime instances.
type DeribitUserOrderRuntimeFactory struct {
	repo            *persistence.StateRepository
	orderServiceURL string
	testnet         bool
	ruleUpdater     ruleStatusUpdater
	notifier        notification.Notifier
}

// NewDeribitUserOrderRuntimeFactory creates a factory for mainnet.
func NewDeribitUserOrderRuntimeFactory(repo *persistence.StateRepository, orderServiceURL string, testnet bool) *DeribitUserOrderRuntimeFactory {
	return &DeribitUserOrderRuntimeFactory{repo: repo, orderServiceURL: orderServiceURL, testnet: testnet}
}

// NewDeribitUserOrderRuntimeFactoryWithRuleUpdater creates a factory with optional rule updater and notifier.
func NewDeribitUserOrderRuntimeFactoryWithRuleUpdater(repo *persistence.StateRepository, orderServiceURL string, testnet bool, updater ruleStatusUpdater, notifier notification.Notifier) *DeribitUserOrderRuntimeFactory {
	return &DeribitUserOrderRuntimeFactory{repo: repo, orderServiceURL: orderServiceURL, testnet: testnet, ruleUpdater: updater, notifier: notifier}
}

// NewUserOrderRuntime creates a runtime for a Deribit user.
func (f *DeribitUserOrderRuntimeFactory) NewUserOrderRuntime(user *order.User) (UserOrderRuntime, error) {
	if user == nil || user.Exchange != "deribit" {
		return nil, fmt.Errorf("not a deribit user")
	}

	log.Printf("deribit runtime factory: creating runtime for userID=%d testnet=%v", user.ID, f.testnet)

	// No repo → no-op runtime (used in tests / stubs)
	if f.repo == nil {
		log.Printf("deribit runtime factory: no repo, creating no-op runtime")
		return &DeribitUserOrderRuntime{
			userID:  user.ID,
			testnet: f.testnet,
			stopCh:  make(chan struct{}),
		}, nil
	}

	// Create Deribit exchange instance
	log.Printf("deribit runtime factory: creating Deribit exchange instance")
	ex, err := deribit.NewDeribit(user.APIKey, user.APISecret, user.APIPassword, f.testnet)
	if err != nil {
		return nil, fmt.Errorf("create deribit: %w", err)
	}

		// Create OrderExecutor
		exec := exchange.NewOrderExecutor(f.repo, ex)
		var rpcClient *rpc.OrderServiceClient
		if f.orderServiceURL != "" {
			rpcClient = rpc.NewOrderServiceClient(f.orderServiceURL)
			exec.SetRPCClient(rpcClient)
			log.Printf("deribit runtime factory: RPC client configured with URL=%s", f.orderServiceURL)
		}
		if f.ruleUpdater != nil {
			exec.SetRuleStatusUpdater(f.ruleUpdater)
		}
		if f.notifier != nil {
			exec.SetNotifier(f.notifier)
		}

		// Create DeribitOrderMonitor
		log.Printf("deribit runtime factory: creating order monitor")
		monitor := exchangews.NewDeribitOrderMonitor(exec, f.repo, rpcClient, ex, f.notifier, f.testnet, user.Name)

	log.Printf("deribit runtime factory: runtime created successfully for userID=%d", user.ID)
	return &DeribitUserOrderRuntime{
		userID:   user.ID,
		testnet:  f.testnet,
		deribit:  ex,
		monitor:  monitor,
		executor: exec,
		repo:     f.repo,
		stopCh:   make(chan struct{}),
	}, nil
}

// Start establishes WebSocket connection and starts monitoring.
func (r *DeribitUserOrderRuntime) Start(ctx context.Context) error {
	if r.monitor == nil || r.executor == nil {
		return nil // no-op for test/no-repo cases
	}

	// Connect to Deribit WebSocket
	if err := r.monitor.Connect(ctx); err != nil {
		return fmt.Errorf("connect deribit ws: %w", err)
	}

	log.Printf("deribit user order runtime started for userID=%d testnet=%v", r.userID, r.testnet)

	go func() {
		<-ctx.Done()
		r.Stop()
	}()

	return nil
}

// Stop closes the WebSocket connection.
func (r *DeribitUserOrderRuntime) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopCh)
		if r.monitor != nil {
			r.monitor.Stop()
		}
		log.Printf("deribit user order runtime stopped for userID=%d", r.userID)
	})
}
