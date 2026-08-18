package rpc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

func setupRPCServer(t *testing.T) (*Server, *persistence.StateRepository, *persistence.GlobalState) {
	t.Helper()
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := persistence.NewStateRepository(gs)
	s := NewServer(repo)
	return s, repo, gs
}

func TestHandleQueryOrderPositionMetadata(t *testing.T) {
	s, repo, gs := setupRPCServer(t)
	defer gs.Shutdown()

	orderID := repo.CreateUserOrder(&order.UserOrder{
		UserID:         6,
		UserStrategyID: 9,
		PosType:        order.PosTypeFutures,
		Exchange:       "binance",
		BaseAsset:      "NEAR",
		QuoteAsset:     "USDT",
		TriggerPrice:   1.971,
		Status:         1,
		CreatedAt:      time.Now(), UpdatedAt: time.Now(),
	})
	repo.UpsertLeverageConfig(&order.LeverageConfig{
		UserID: 6, Asset: "NEAR", Quote: "USDT", Leverage: 5,
		Exchange: "binance", Status: 1, PosType: order.PosTypeFutures,
	})

	body, _ := json.Marshal(QueryOrderPositionMetadataRequest{UserOrderID: orderID})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/rpc/v1/order/position-metadata", bytes.NewReader(body))
	s.Handle().ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%q", w.Code, w.Body.String())
	}
	var resp QueryOrderPositionMetadataResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.UserStrategyID != 9 || resp.Leverage != 5 || resp.FallbackPrice != 1.971 {
		t.Fatalf("unexpected metadata response: %+v", resp)
	}
}

func TestHandleUpdateOrderStatus_FILLED(t *testing.T) {
	s, repo, gs := setupRPCServer(t)
	defer gs.Shutdown()

	// Create a user_order with status=1
	o := &order.UserOrder{
		UserID: 1, UserStrategyID: 1,
		Exchange: "mock", Status: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	orderID := repo.CreateUserOrder(o)

	reqBody := UpdateUserOrderStatusRequest{
		UserOrderID: orderID,
		Status:      2, // FILLED
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/rpc/v1/order/status/update", bytes.NewReader(body))
	s.handleUpdateOrderStatus(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify order status updated
	updated, err := repo.GetUserOrderByID(orderID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != 2 {
		t.Errorf("expected status=2, got %d", updated.Status)
	}
}

func TestHandleUpdateOrderStatus_FAILED(t *testing.T) {
	s, repo, gs := setupRPCServer(t)
	defer gs.Shutdown()

	o := &order.UserOrder{
		UserID: 1, UserStrategyID: 1,
		Exchange: "mock", Status: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	orderID := repo.CreateUserOrder(o)

	reqBody := UpdateUserOrderStatusRequest{
		UserOrderID: orderID,
		Status:      3, // FAILED
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/rpc/v1/order/status/update", bytes.NewReader(body))
	s.handleUpdateOrderStatus(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	updated, err := repo.GetUserOrderByID(orderID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != 3 {
		t.Errorf("expected status=3, got %d", updated.Status)
	}
}

func TestHandleUpdateOrderStatus_NotFound(t *testing.T) {
	s, _, gs := setupRPCServer(t)
	defer gs.Shutdown()

	reqBody := UpdateUserOrderStatusRequest{
		UserOrderID: 99999,
		Status:      2,
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/rpc/v1/order/status/update", bytes.NewReader(body))
	s.handleUpdateOrderStatus(w, r)

	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleUpdateOrderStatus_BadRequest(t *testing.T) {
	s, _, gs := setupRPCServer(t)
	defer gs.Shutdown()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/rpc/v1/order/status/update", bytes.NewReader([]byte("invalid json")))
	s.handleUpdateOrderStatus(w, r)

	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestServer_RegisterRoutes verifies that the server registers the endpoint correctly
func TestServer_RegisterRoutes(t *testing.T) {
	s, _, gs := setupRPCServer(t)
	defer gs.Shutdown()

	// Verify the handler is set
	if s.mux == nil {
		t.Error("expected mux to be initialized")
	}
}

// TestHandleReloadFilters_Success tests successful reload of filters.
func TestHandleReloadFilters_Success(t *testing.T) {
	s, repo, gs := setupRPCServer(t)
	defer gs.Shutdown()

	// Add a filter to CSV (simulating PMS update)
	filter := &order.ExchangeSymbolFilter{
		ID:         1,
		Exchange:   "hyperliquid",
		PosType:    order.PosTypeFutures,
		Symbol:     "BTCUSDC",
		FilterType: "LOT_SIZE",
		TickSize:   0.1,
		StepSize:   0.001,
		MinQty:     0.001,
		MaxQty:     1000,
	}
	repo.ReplaceExchangeSymbolFilters([]*order.ExchangeSymbolFilter{filter})

	// Verify initial state
	filters := repo.ListAllExchangeSymbolFilters()
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter after ReplaceExchangeSymbolFilters, got %d", len(filters))
	}

	// Create request
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/rpc/v1/filters/reload", nil)

	// Test
	s.handleReloadFilters(w, r)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Verify response status
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %s", resp["status"])
	}

	// Verify filter is still in memory
	filters = repo.ListAllExchangeSymbolFilters()
	if len(filters) != 1 {
		t.Errorf("expected 1 filter after reload, got %d", len(filters))
	}
	if filters[0].Symbol != "BTCUSDC" {
		t.Errorf("expected symbol BTCUSDC, got %s", filters[0].Symbol)
	}
}

// TestHandleReloadFilters_MethodNotAllowed tests wrong HTTP method.
func TestHandleReloadFilters_MethodNotAllowed(t *testing.T) {
	s, _, gs := setupRPCServer(t)
	defer gs.Shutdown()

	// Create request with GET instead of POST
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/rpc/v1/filters/reload", nil)

	// Test
	s.handleReloadFilters(w, r)

	// Verify
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestHandleReloadFilters_EmptyCSV tests reload with no filters.
func TestHandleReloadFilters_EmptyCSV(t *testing.T) {
	s, repo, gs := setupRPCServer(t)
	defer gs.Shutdown()

	// Create request
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/rpc/v1/filters/reload", nil)

	// Test
	s.handleReloadFilters(w, r)

	// Verify
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Verify no filters
	filters := repo.ListAllExchangeSymbolFilters()
	if len(filters) != 0 {
		t.Errorf("expected 0 filters, got %d", len(filters))
	}
}
