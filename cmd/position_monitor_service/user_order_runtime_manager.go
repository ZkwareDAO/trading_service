package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"trading-service/internal/order"
)

type UserOrderRuntime interface {
	Start(ctx context.Context) error
	Stop()
}

type ruleStatusUpdater interface {
	UpdateRuleStatus(id int, status string) error
	ResetRulesForStrategy(strategyID uint64) error
}

type UserOrderRuntimeFactory interface {
	NewUserOrderRuntime(user *order.User) (UserOrderRuntime, error)
}

type UserLoader interface {
	ListUsers() []*order.User
}

type userRuntimeEntry struct {
	fingerprint string
	runtime     UserOrderRuntime
}

type UserOrderRuntimeManager struct {
	mu       sync.Mutex
	factory  UserOrderRuntimeFactory
	runtimes map[uint64]*userRuntimeEntry
}

func NewUserOrderRuntimeManager(factory UserOrderRuntimeFactory) *UserOrderRuntimeManager {
	return &UserOrderRuntimeManager{factory: factory, runtimes: make(map[uint64]*userRuntimeEntry)}
}

func (m *UserOrderRuntimeManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runtimes)
}

func (m *UserOrderRuntimeManager) ReconcileUsers(ctx context.Context, users []*order.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	seen := make(map[uint64]bool, len(users))
	for _, user := range users {
		if !shouldStartUserOrderRuntime(user) {
			continue
		}
		seen[user.ID] = true
		fingerprint := userRuntimeFingerprint(user)
		if existing := m.runtimes[user.ID]; existing != nil {
			if existing.fingerprint == fingerprint {
				continue
			}
			existing.runtime.Stop()
			delete(m.runtimes, user.ID)
		}

		runtime, err := m.factory.NewUserOrderRuntime(user)
		if err != nil {
			return fmt.Errorf("create user order runtime for user %d: %w", user.ID, err)
		}
		if err := runtime.Start(ctx); err != nil {
			return fmt.Errorf("start user order runtime for user %d: %w", user.ID, err)
		}
		m.runtimes[user.ID] = &userRuntimeEntry{fingerprint: fingerprint, runtime: runtime}
	}

	for userID, entry := range m.runtimes {
		if !seen[userID] {
			entry.runtime.Stop()
			delete(m.runtimes, userID)
		}
	}
	return nil
}

func StartUserOrderRuntimeReconcileLoop(ctx context.Context, manager *UserOrderRuntimeManager, loader UserLoader, interval time.Duration) <-chan struct{} {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		run := func() {
			if err := manager.ReconcileUsers(ctx, loader.ListUsers()); err != nil {
				log.Printf("user order runtime reconcile failed: %v", err)
			}
		}
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = manager.ReconcileUsers(ctx, nil)
				return
			case <-ticker.C:
				run()
			}
		}
	}()
	return done
}

func shouldStartUserOrderRuntime(user *order.User) bool {
	if user == nil || user.ID == 0 || user.Exchange == "" || user.Exchange == "mock" {
		return false
	}
	switch user.Exchange {
	case "binance":
		return user.APIKey != "" && user.APISecret != ""
	case "hyperliquid":
		return user.APISecret != ""
	case "deribit":
		return user.APIKey != "" && user.APISecret != ""
	default:
		return false
	}
}

func userRuntimeFingerprint(user *order.User) string {
	return user.Exchange + "|" + user.APIKey + "|" + user.APISecret + "|" + user.APIPassword
}
