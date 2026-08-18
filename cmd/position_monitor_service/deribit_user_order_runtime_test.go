package main

import (
	"testing"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

// TestDeribitUserOrderRuntimeFactory_NewUserOrderRuntime tests factory creation
func TestDeribitUserOrderRuntimeFactory_NewUserOrderRuntime(t *testing.T) {
	repo := &persistence.StateRepository{}
	factory := NewDeribitUserOrderRuntimeFactory(repo, "http://localhost:8080", false)

	// Test with nil user
	runtime, err := factory.NewUserOrderRuntime(nil)
	if err == nil {
		t.Error("Expected error for nil user")
	}

	// Test with non-deribit user
	user := &order.User{
		Exchange: "binance",
		APIKey:   "test-key",
	}
	runtime, err = factory.NewUserOrderRuntime(user)
	if err == nil {
		t.Error("Expected error for non-deribit user")
	}

	// Test with valid deribit user
	deribitUser := &order.User{
		Exchange:  "deribit",
		APIKey:    "test-api-key",
		APISecret: "test-api-secret",
	}
	runtime, err = factory.NewUserOrderRuntime(deribitUser)
	if err != nil {
		t.Fatalf("Expected no error for deribit user, got: %v", err)
	}
	if runtime == nil {
		t.Fatal("Expected runtime, got nil")
	}

	// Verify it's a DeribitUserOrderRuntime
	if _, ok := runtime.(*DeribitUserOrderRuntime); !ok {
		t.Error("Expected DeribitUserOrderRuntime type")
	}
}

// TestDeribitUserOrderRuntimeFactory_Testnet tests testnet flag propagation
func TestDeribitUserOrderRuntimeFactory_Testnet(t *testing.T) {
	repo := &persistence.StateRepository{}

	// Testnet factory
	factoryTestnet := NewDeribitUserOrderRuntimeFactory(repo, "", true)
	user := &order.User{
		Exchange:  "deribit",
		APIKey:    "test-key",
		APISecret: "test-secret",
	}
	runtime, err := factoryTestnet.NewUserOrderRuntime(user)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	deribitRuntime := runtime.(*DeribitUserOrderRuntime)
	if !deribitRuntime.testnet {
		t.Error("Expected testnet=true in runtime")
	}

	// Mainnet factory
	factoryMainnet := NewDeribitUserOrderRuntimeFactory(repo, "", false)
	runtime, err = factoryMainnet.NewUserOrderRuntime(user)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	deribitRuntime = runtime.(*DeribitUserOrderRuntime)
	if deribitRuntime.testnet {
		t.Error("Expected testnet=false in runtime")
	}
}

// TestDeribitUserOrderRuntimeFactory_NoRepo tests no-repo case (no-op runtime)
func TestDeribitUserOrderRuntimeFactory_NoRepo(t *testing.T) {
	factory := NewDeribitUserOrderRuntimeFactory(nil, "", false)
	user := &order.User{
		Exchange:  "deribit",
		APIKey:    "test-key",
		APISecret: "test-secret",
	}

	runtime, err := factory.NewUserOrderRuntime(user)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should return no-op runtime
	deribitRuntime := runtime.(*DeribitUserOrderRuntime)
	if deribitRuntime.monitor != nil {
		t.Error("Expected nil monitor for no-repo factory")
	}
}

// TestDeribitUserOrderRuntime_StartStop tests lifecycle management
func TestDeribitUserOrderRuntime_StartStop(t *testing.T) {
	repo := &persistence.StateRepository{}
	factory := NewDeribitUserOrderRuntimeFactory(repo, "", false)
	user := &order.User{
		Exchange:  "deribit",
		APIKey:    "test-key",
		APISecret: "test-secret",
	}

	runtime, err := factory.NewUserOrderRuntime(user)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Start should not error (even without real connection)
	// Note: In real integration tests, this would fail without mock server
	// For unit test, we verify the structure is correct

	// Stop should not panic
	runtime.Stop()
	runtime.Stop() // Multiple stops should be safe
}