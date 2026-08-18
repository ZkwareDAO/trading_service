package binance

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"trading-service/internal/exchange"

	"github.com/adshao/go-binance/v2/futures"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBinanceFutures_ImplementsExchange(t *testing.T) {
	var _ exchange.Exchange = &BinanceFutures{}
}

func TestBinanceFutures_Name(t *testing.T) {
	b := &BinanceFutures{}
	if b.Name() != "binance" {
		t.Errorf("expected 'binance', got '%s'", b.Name())
	}
}

func TestBinanceFutures_CreateOrder_NeedsCredentials(t *testing.T) {
	b := &BinanceFutures{}
	_, err := b.CreateOrder(exchange.CreateOrderRequest{
		Symbol:    "BTCUSDT",
		Side:      exchange.OrderSideBuy,
		OrderType: exchange.OrderTypeLimit,
		Quantity:  0.001,
		Price:     50000,
	})
	if err == nil {
		t.Error("expected error when no credentials configured")
	}
}

func TestBinanceFutures_GetPrice_WithoutCredentials(t *testing.T) {
	b := &BinanceFutures{}
	_, err := b.GetPrice("BTCUSDT")
	if err == nil {
		t.Error("expected error when no credentials configured")
	}
}

func TestNewBinanceFutures(t *testing.T) {
	b := NewBinanceFutures("test_key", "test_secret", false)
	if b == nil {
		t.Fatal("expected non-nil")
	}
	if b.Name() != "binance" {
		t.Errorf("expected name 'binance', got '%s'", b.Name())
	}
}

func TestBinanceFutures_SyncSymbolFilters_UsesNonNilContext(t *testing.T) {
	client := futures.NewClient("test_key", "test_secret")
	client.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			select {
			case <-req.Context().Done():
				t.Fatal("request context should not be canceled")
			default:
			}

			body := `{"symbols":[{"symbol":"BTCUSDT","contractType":"PERPETUAL","filters":[{"filterType":"PRICE_FILTER","minPrice":"0.1","maxPrice":"1000000","tickSize":"0.1"},{"filterType":"LOT_SIZE","minQty":"0.001","maxQty":"1000","stepSize":"0.001"},{"filterType":"MIN_NOTIONAL","notional":"5"}]}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	}

	b := &BinanceFutures{client: client}

	filters, err := b.SyncSymbolFilters()
	if err != nil {
		t.Fatalf("SyncSymbolFilters: %v", err)
	}
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(filters))
	}
	if filters[0].Symbol != "BTCUSDT" || filters[0].StepSize != 0.001 {
		t.Fatalf("unexpected filter: %+v", filters[0])
	}
}

func TestBinanceFutures_SetLeverage_NeedsCredentials(t *testing.T) {
	b := &BinanceFutures{}
	err := b.SetLeverage("BTCUSDT", 10)
	if err == nil {
		t.Error("expected error when no credentials")
	}
}

func TestBinanceFutures_CancelOrder_NeedsCredentials(t *testing.T) {
	b := &BinanceFutures{}
	err := b.CancelOrder(123)
	if err == nil {
		t.Error("expected error when no credentials")
	}
}

func TestBinanceFutures_GetOrder_NeedsCredentials(t *testing.T) {
	b := &BinanceFutures{}
	_, err := b.GetOrder(123, "NEARUSDT")
	if err == nil {
		t.Error("expected error when no credentials")
	}
}
