package exchange

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

// mockRPCClientForRetry tracks RPC calls with fail-count semantics for retry testing.
type mockRPCClientForRetry struct {
	callCount int64
	failCount int64 // fail first N calls
}

func (m *mockRPCClientForRetry) UpdateUserOrderStatusFILLED(ctx context.Context, orderID uint64) error {
	count := atomic.AddInt64(&m.callCount, 1)
	if count <= atomic.LoadInt64(&m.failCount) {
		return &mockRPCError{msg: "unexpected status code: 404"}
	}
	return nil
}

func (m *mockRPCClientForRetry) UpdateUserOrderStatusFailed(ctx context.Context, orderID uint64) error {
	return nil
}

func (m *mockRPCClientForRetry) QueryOrderPositionMetadata(ctx context.Context, orderID uint64) (*rpc.QueryOrderPositionMetadataResponse, error) {
	return &rpc.QueryOrderPositionMetadataResponse{UserOrderID: orderID, UserStrategyID: 9, Leverage: 5, FallbackPrice: 2.5}, nil
}

type mockRPCError struct{ msg string }

func (e *mockRPCError) Error() string { return e.msg }

// TestExecutor_RPCRetry verifies that RPC notification retries 3 times on failure.
func TestExecutor_RPCRetry(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState: %v", err)
	}
	t.Cleanup(func() { gs.Shutdown() })
	repo := persistence.NewStateRepository(gs)

	mock := NewMockExchange()
	mockRPC := &mockRPCClientForRetry{failCount: 2} // fail first 2, succeed on 3rd

	exec := NewOrderExecutor(repo, mock)
	exec.SetRPCClient(mockRPC)

	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          100,
		RelationType:        order.RelationTypeUserOrders,
		Exchange:            "mock",
		Symbol:              "NEARUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     100,
		ExchangeOrderStatus: "NEW",
		Side:                order.SideLong,
	}
	repo.CreateUprunningOrder(uo)

	update := &OrderUpdate{
		OrderID: uo.ID, Symbol: "NEARUSDT", Status: "FILLED",
		AvgPrice: 2.5, ExecutedQty: 100, PositionSide: "LONG",
		UserID: 1, PosType: order.PosTypeFutures, RelationID: 100,
	}

	if err := exec.HandleOrderFilled(update); err != nil {
		t.Fatalf("HandleOrderFilled failed: %v", err)
	}

	// Wait for async RPC retries (max 3 calls, 1s each sleep)
	time.Sleep(3 * time.Second)

	count := atomic.LoadInt64(&mockRPC.callCount)
	if count != 3 {
		t.Errorf("expected 3 RPC calls (2 fails + 1 success), got %d", count)
	}
}

// TestExecutor_RPCRetryAllFail verifies that after 3 failures, no more calls are made.
func TestExecutor_RPCRetryAllFail(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState: %v", err)
	}
	t.Cleanup(func() { gs.Shutdown() })
	repo := persistence.NewStateRepository(gs)

	mock := NewMockExchange()
	mockRPC := &mockRPCClientForRetry{failCount: 10} // always fail

	exec := NewOrderExecutor(repo, mock)
	exec.SetRPCClient(mockRPC)

	uo := &order.UprunningOrder{
		UserID:              1,
		RelationID:          200,
		RelationType:        order.RelationTypeUserOrders,
		Exchange:            "mock",
		Symbol:              "BTCUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     200,
		ExchangeOrderStatus: "NEW",
		Side:                order.SideLong,
	}
	repo.CreateUprunningOrder(uo)

	update := &OrderUpdate{
		OrderID: uo.ID, Symbol: "BTCUSDT", Status: "FILLED",
		AvgPrice: 50000, ExecutedQty: 0.1, PositionSide: "LONG",
		UserID: 1, PosType: order.PosTypeFutures, RelationID: 200,
	}

	if err := exec.HandleOrderFilled(update); err != nil {
		t.Fatalf("HandleOrderFilled failed: %v", err)
	}

	time.Sleep(3 * time.Second)

	count := atomic.LoadInt64(&mockRPC.callCount)
	if count != 3 {
		t.Errorf("expected exactly 3 RPC calls, got %d", count)
	}
}
