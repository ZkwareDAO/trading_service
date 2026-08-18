package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
)

// These tests cover the asset/exchange normalization of the Agent close
// interfaces (close-all, close-partial). The `asset` field is documented as a
// base asset ("XRP"), but callers routinely pass the full trading pair
// ("XRPUSDC"). Both must resolve to the same stored position, otherwise the
// handler wrongly reports "No active positions".

// closeTestFixture creates a handler plus one active position for the given
// exchange/asset and returns the handler and the created user name.
func closeTestFixture(t *testing.T, exchange, asset string, posType order.PosType) (*PositionAPIHandler, string) {
	t.Helper()
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userName := "close_test_user"
	userID := repo.CreateUser(&order.User{
		Name:      userName,
		Exchange:  exchange,
		CreatedAt: time.Now(),
	})

	strategyID := repo.CreateStrategy(&order.Strategy{
		Name:         "SAR_SNT3_V3_8H_3_" + asset,
		StrategyType: "cta_intraday",
		CreatedAt:    time.Now(),
	})
	userStrategyID := repo.CreateUserStrategy(&order.UserStrategy{
		UserID:     userID,
		StrategyID: strategyID,
		Status:     1,
		CreatedAt:  time.Now(),
	})

	now := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: userStrategyID,
		Exchange:       exchange,
		PosType:        posType,
		Asset:          asset,
		CurrentPrice:   1.01425,
		Quantity:       198,
		Deleted:        0,
		Side:           order.SideShort,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	return handler, userName
}

func postJSON(t *testing.T, handler *PositionAPIHandler, path, body string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response %q: %v", w.Body.String(), err)
	}
	return w.Code, resp
}

// ===== httpStatusForCode unit tests =====

func TestHTTPStatusForCode(t *testing.T) {
	testCases := []struct {
		name     string
		code     int
		expected int
	}{
		// Client errors: every width in use must map to 400. Codes 40001-40004
		// are the regression this guards — they exceed 5000 numerically and were
		// previously misreported as HTTP 500.
		{"5-digit client error", 40003, http.StatusBadRequest},
		{"5-digit client error upper", 40004, http.StatusBadRequest},
		{"4-digit client error", 4003, http.StatusBadRequest},
		{"legacy 1001", 1001, http.StatusBadRequest},
		// Server errors
		{"server error 5001", 5001, http.StatusInternalServerError},
		{"server error 5000", 5000, http.StatusInternalServerError},
		// Degenerate inputs must not be classified as server errors
		{"zero", 0, http.StatusBadRequest},
		{"negative", -5001, http.StatusBadRequest},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := httpStatusForCode(tc.code); got != tc.expected {
				t.Errorf("httpStatusForCode(%d) = %d, want %d", tc.code, got, tc.expected)
			}
		})
	}
}

// ===== exchange/positions =====

// handleGetExchangePositions normalizes the exchange for the user lookup and
// for ResolveExchange, but must echo the caller's original spelling back.
func TestGetExchangePositions_ExchangeCaseInsensitive(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	userID := repo.CreateUser(&order.User{Name: "ex_user", Exchange: "binance", CreatedAt: time.Now()})
	resolver := &mockResolver{exchanges: map[string]exchange.Exchange{
		fmt.Sprintf("%d:binance", userID): &mockExchange{},
	}}
	handler := NewPositionAPIHandler(repo, ruleStore, resolver)

	code, resp := postJSON(t, handler, "/api/v1/exchange/positions",
		`{"user_name":"ex_user","exchange":"Binance"}`)

	if code != http.StatusOK || resp["code"] != float64(0) {
		t.Fatalf("expected success for mixed-case exchange, got %d %v", code, resp)
	}
	data := resp["data"].(map[string]interface{})
	if got := data["exchange"]; got != "Binance" {
		t.Errorf("response should echo the caller's spelling %q, got %v", "Binance", got)
	}
}

// ===== buildSymbol unit tests =====

func TestBuildSymbol_Idempotent(t *testing.T) {
	testCases := []struct {
		name     string
		exchange string
		asset    string
		expected string
	}{
		{"hyperliquid base asset", "hyperliquid", "XRP", "XRPUSDC"},
		{"hyperliquid full pair not double suffixed", "hyperliquid", "XRPUSDC", "XRPUSDC"},
		{"hyperliquid USDT pair adapted to USDC", "hyperliquid", "XRPUSDT", "XRPUSDC"},
		{"binance base asset", "binance", "BTC", "BTCUSDT"},
		{"binance full pair not double suffixed", "binance", "BTCUSDT", "BTCUSDT"},
		{"binance USDC pair adapted to USDT", "binance", "BTCUSDC", "BTCUSDT"},
		{"mixed case exchange", "Hyperliquid", "XRPUSDC", "XRPUSDC"},
		{"deribit option symbol untouched", "deribit", "BTC-21AUG26-63000-P", "BTC-21AUG26-63000-P"},
		// A base asset that is itself a quote currency must not be stripped to
		// empty: "USDC" on binance is the real pair USDCUSDT, not USDT.
		{"quote-like base asset on binance", "binance", "USDC", "USDCUSDT"},
		{"quote-like base asset on hyperliquid", "hyperliquid", "USDT", "USDTUSDC"},
		{"quote-like full pair on binance", "binance", "USDCUSDT", "USDCUSDT"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildSymbol(tc.exchange, tc.asset); got != tc.expected {
				t.Errorf("buildSymbol(%q, %q) = %q, want %q", tc.exchange, tc.asset, got, tc.expected)
			}
		})
	}
}

// ===== close-all =====

func TestCloseAllPositions_AssetWithQuoteSuffix(t *testing.T) {
	handler, userName := closeTestFixture(t, "hyperliquid", "XRPUSDC", order.PosTypeFutures)

	code, resp := postJSON(t, handler, "/api/v1/positions/close-all",
		`{"user_name":"`+userName+`","exchange":"hyperliquid","pos_type":2,"asset":"XRPUSDC"}`)

	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, resp)
	}
	if resp["code"] != float64(0) {
		t.Fatalf("expected code=0, got %v (message=%v)", resp["code"], resp["message"])
	}
	data := resp["data"].(map[string]interface{})
	if data["closed_count"] != float64(1) {
		t.Errorf("expected closed_count=1, got %v", data["closed_count"])
	}
}

func TestCloseAllPositions_AssetBaseOnly(t *testing.T) {
	handler, userName := closeTestFixture(t, "hyperliquid", "XRPUSDC", order.PosTypeFutures)

	code, resp := postJSON(t, handler, "/api/v1/positions/close-all",
		`{"user_name":"`+userName+`","exchange":"hyperliquid","pos_type":2,"asset":"XRP"}`)

	if code != http.StatusOK || resp["code"] != float64(0) {
		t.Fatalf("expected success, got %d %v", code, resp)
	}
}

func TestCloseAllPositions_BinanceAssetWithQuoteSuffix(t *testing.T) {
	handler, userName := closeTestFixture(t, "binance", "BTCUSDT", order.PosTypeFutures)

	code, resp := postJSON(t, handler, "/api/v1/positions/close-all",
		`{"user_name":"`+userName+`","exchange":"binance","pos_type":2,"asset":"BTCUSDT"}`)

	if code != http.StatusOK || resp["code"] != float64(0) {
		t.Fatalf("expected success, got %d %v", code, resp)
	}
}

func TestCloseAllPositions_ExchangeCaseInsensitive(t *testing.T) {
	handler, userName := closeTestFixture(t, "hyperliquid", "XRPUSDC", order.PosTypeFutures)

	code, resp := postJSON(t, handler, "/api/v1/positions/close-all",
		`{"user_name":"`+userName+`","exchange":"Hyperliquid","pos_type":2,"asset":"XRP"}`)

	if code != http.StatusOK || resp["code"] != float64(0) {
		t.Fatalf("expected success for mixed-case exchange, got %d %v", code, resp)
	}
}

func TestCloseAllPositions_NoActivePositionsWhenAllClosed(t *testing.T) {
	_, repo, ruleStore := setupTestState(t)
	handler := NewPositionAPIHandler(repo, ruleStore, nil)

	userID := repo.CreateUser(&order.User{Name: "closed_user", Exchange: "hyperliquid", CreatedAt: time.Now()})
	closeTime := time.Now()
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:    userID,
		Exchange:  "hyperliquid",
		PosType:   order.PosTypeFutures,
		Asset:     "XRPUSDC",
		Deleted:   1,
		CloseTime: &closeTime,
		CreatedAt: time.Now(),
	})

	code, resp := postJSON(t, handler, "/api/v1/positions/close-all",
		`{"user_name":"closed_user","exchange":"hyperliquid","pos_type":2,"asset":"XRPUSDC"}`)

	if code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %v", code, resp)
	}
	if resp["code"] != float64(40003) {
		t.Errorf("expected code=40003, got %v", resp["code"])
	}
	if resp["message"] != "No active positions" {
		t.Errorf("expected 'No active positions', got %v", resp["message"])
	}
}

func TestCloseAllPositions_PosTypeMismatchReturnsNoActive(t *testing.T) {
	handler, userName := closeTestFixture(t, "hyperliquid", "XRPUSDC", order.PosTypeFutures)

	// pos_type=1 (spot) while the stored position is futures
	code, resp := postJSON(t, handler, "/api/v1/positions/close-all",
		`{"user_name":"`+userName+`","exchange":"hyperliquid","pos_type":1,"asset":"XRPUSDC"}`)

	if code != http.StatusBadRequest || resp["code"] != float64(40003) {
		t.Fatalf("expected 40003 for pos_type mismatch, got %d %v", code, resp)
	}
}

func TestCloseAllPositions_PosTypeAnyMatchesFutures(t *testing.T) {
	handler, userName := closeTestFixture(t, "hyperliquid", "XRPUSDC", order.PosTypeFutures)

	code, resp := postJSON(t, handler, "/api/v1/positions/close-all",
		`{"user_name":"`+userName+`","exchange":"hyperliquid","pos_type":0,"asset":"XRPUSDC"}`)

	if code != http.StatusOK || resp["code"] != float64(0) {
		t.Fatalf("expected success for pos_type=0, got %d %v", code, resp)
	}
}

// ===== close-partial =====

func TestClosePartialPosition_AssetWithQuoteSuffix(t *testing.T) {
	handler, userName := closeTestFixture(t, "hyperliquid", "XRPUSDC", order.PosTypeFutures)

	code, resp := postJSON(t, handler, "/api/v1/positions/close-partial",
		`{"user_name":"`+userName+`","exchange":"hyperliquid","pos_type":2,"asset":"XRPUSDC",`+
			`"price":1.2,"quantity_pct":0.5,"trigger_type":"take_profit"}`)

	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, resp)
	}
	if resp["code"] != float64(0) {
		t.Fatalf("expected code=0, got %v (message=%v)", resp["code"], resp["message"])
	}
	data := resp["data"].(map[string]interface{})
	// Short position + take_profit => price_xrp <= 1.2
	if got := data["condition"]; got != "price_xrp <= 1.2" {
		t.Errorf("expected condition 'price_xrp <= 1.2', got %v", got)
	}
}

func TestClosePartialPosition_ExchangeCaseInsensitive(t *testing.T) {
	handler, userName := closeTestFixture(t, "binance", "BTCUSDT", order.PosTypeFutures)

	code, resp := postJSON(t, handler, "/api/v1/positions/close-partial",
		`{"user_name":"`+userName+`","exchange":"Binance","pos_type":2,"asset":"BTCUSDT",`+
			`"price":90000,"quantity_pct":0.25,"trigger_type":"stop_loss"}`)

	if code != http.StatusOK || resp["code"] != float64(0) {
		t.Fatalf("expected success for mixed-case exchange, got %d %v", code, resp)
	}
}

func TestClosePartialPosition_NotFoundKeepsErrorCode(t *testing.T) {
	handler, userName := closeTestFixture(t, "hyperliquid", "XRPUSDC", order.PosTypeFutures)

	code, resp := postJSON(t, handler, "/api/v1/positions/close-partial",
		`{"user_name":"`+userName+`","exchange":"hyperliquid","pos_type":2,"asset":"DOGE",`+
			`"price":1.2,"quantity_pct":0.5,"trigger_type":"take_profit"}`)

	if code != http.StatusBadRequest || resp["code"] != float64(40003) {
		t.Fatalf("expected 40003 for unknown asset, got %d %v", code, resp)
	}
}

func TestClosePartialPosition_InvalidExchange(t *testing.T) {
	handler, userName := closeTestFixture(t, "hyperliquid", "XRPUSDC", order.PosTypeFutures)

	// An unsupported exchange must be reported as such, not as a missing user.
	code, resp := postJSON(t, handler, "/api/v1/positions/close-partial",
		`{"user_name":"`+userName+`","exchange":"okx","pos_type":2,"asset":"XRP",`+
			`"price":1.2,"quantity_pct":0.5,"trigger_type":"take_profit"}`)

	if code != http.StatusBadRequest || resp["code"] != float64(40001) {
		t.Fatalf("expected 40001 invalid exchange, got %d %v", code, resp)
	}
}

// Regression guard: normalizing the exchange must not make the user lookup
// match a user registered on a different exchange.
func TestCloseAllPositions_UserNotFoundForWrongExchange(t *testing.T) {
	handler, userName := closeTestFixture(t, "hyperliquid", "XRPUSDC", order.PosTypeFutures)

	code, resp := postJSON(t, handler, "/api/v1/positions/close-all",
		`{"user_name":"`+userName+`","exchange":"binance","pos_type":2,"asset":"XRP"}`)

	if code != http.StatusBadRequest || resp["code"] != float64(40002) {
		t.Fatalf("expected 40002 user not found, got %d %v", code, resp)
	}
}
