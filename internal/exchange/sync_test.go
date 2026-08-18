package exchange

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

// mockSyncExchange implements SyncableExchange for testing.
type mockSyncExchange struct {
	filters []*order.ExchangeSymbolFilter
	err     error
}

func (m *mockSyncExchange) Name() string { return "mocksync" }
func (m *mockSyncExchange) CreateOrder(req CreateOrderRequest) (*CreateOrderResponse, error) {
	return nil, nil
}
func (m *mockSyncExchange) CancelOrder(orderID uint64) error { return nil }
func (m *mockSyncExchange) GetOrder(orderID uint64, symbol string) (*OrderInfo, error) {
	return nil, nil
}
func (m *mockSyncExchange) GetPositions() ([]PositionInfo, error) { return nil, nil }
func (m *mockSyncExchange) SetLeverage(symbol string, leverage int) error { return nil }
func (m *mockSyncExchange) GetLeverage(symbol string) (int, error)        { return 0, nil }
func (m *mockSyncExchange) GetPrice(symbol string) (float64, error)       { return 0, nil }
func (m *mockSyncExchange) Connect() error                                { return nil }
func (m *mockSyncExchange) Close() error                                  { return nil }
func (m *mockSyncExchange) SubscribeOrders(callback OrderCallback) error  { return nil }
func (m *mockSyncExchange) SyncSymbolFilters() ([]*order.ExchangeSymbolFilter, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.filters, nil
}

func TestSyncFiltersOnce_SkipsNonSyncableExchanges(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	factory := NewExchangeFactory()
	factory.Register("mock", NewMockExchange()) // mock is NOT syncable

	// Should not crash, should just skip
	syncFiltersOnce(context.Background(), factory, repo)

	// Verify no CSV was created (since mock was skipped)
	_, err = os.Stat(filepath.Join(dir, "exchange_symbol_filters.csv"))
	if !os.IsNotExist(err) {
		t.Error("expected no CSV file when no syncable exchanges exist")
	}
}

func TestSyncFiltersOnce_SyncsSyncableExchange(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	factory := NewExchangeFactory()
	factory.Register("binance", &mockSyncExchange{
		filters: []*order.ExchangeSymbolFilter{
			{Exchange: "binance", PosType: order.PosTypeFutures, Symbol: "BTCUSDT", FilterType: "LOT_SIZE", MinQty: 0.001, StepSize: 0.001},
			{Exchange: "binance", PosType: order.PosTypeFutures, Symbol: "ETHUSDT", FilterType: "LOT_SIZE", MinQty: 0.01, StepSize: 0.01},
		},
	})

	syncFiltersOnce(context.Background(), factory, repo)

	liveBTC := repo.ListExchangeSymbolFilters("binance", order.PosTypeFutures, "BTCUSDT")
	if len(liveBTC) != 1 {
		t.Fatalf("expected live repo to contain 1 BTCUSDT filter after sync, got %d", len(liveBTC))
	}

	// Reload state from disk to verify persistence
	gs2, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs2.Shutdown()
	repo2 := persistence.NewStateRepository(gs2)

	btc := repo2.ListExchangeSymbolFilters("binance", order.PosTypeFutures, "BTCUSDT")
	if len(btc) != 1 {
		t.Fatalf("expected 1 BTCUSDT filter, got %d", len(btc))
	}
	if btc[0].MinQty != 0.001 || btc[0].StepSize != 0.001 {
		t.Errorf("unexpected BTC filter values: %+v", btc[0])
	}

	eth := repo2.ListExchangeSymbolFilters("binance", order.PosTypeFutures, "ETHUSDT")
	if len(eth) != 1 {
		t.Fatalf("expected 1 ETHUSDT filter, got %d", len(eth))
	}
	if eth[0].MinQty != 0.01 || eth[0].StepSize != 0.01 {
		t.Errorf("unexpected ETH filter values: %+v", eth[0])
	}
}

func TestStartFilterSync_ContextCancel_Stops(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	factory := NewExchangeFactory()
	factory.Register("binance", &mockSyncExchange{
		filters: []*order.ExchangeSymbolFilter{
			{Exchange: "binance", PosType: order.PosTypeFutures, Symbol: "BTCUSDT", FilterType: "LOT_SIZE"},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		StartFilterSync(ctx, factory, repo, time.Hour) // long interval so it won't fire again
		close(done)
	}()

	// Wait a moment for the initial sync to complete
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait for the goroutine to exit
	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("StartFilterSync did not stop after context cancellation")
	}
}

func TestStartFilterSync_ZeroInterval_Disables(t *testing.T) {
	factory := NewExchangeFactory()
	factory.Register("mock", NewMockExchange())

	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	// Should return immediately
	StartFilterSync(context.Background(), factory, repo, 0)

	// Nothing should have happened
	_, err = os.Stat(filepath.Join(dir, "exchange_symbol_filters.csv"))
	if !os.IsNotExist(err) {
		t.Error("expected no CSV file when interval is 0")
	}
}
