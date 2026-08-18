package persistence

import (
	"testing"
	"time"

	"trading-service/internal/order"
)

func TestEncodeUserToCSVRow(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	user := &order.User{
		ID:        1,
		Name:      "test_user",
		Exchange:  "binance",
		APIKey:    "key123",
		APISecret: "secret456",
		CreatedAt: now,
		UpdatedAt: now,
	}

	row, err := EncodeToCSVRow(user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(row) == 0 {
		t.Fatal("expected non-empty row")
	}

	headers := GetCSVHeaders(user)
	if len(headers) == 0 {
		t.Fatal("expected non-empty headers")
	}

	foundID, foundName := false, false
	for _, h := range headers {
		if h == "id" {
			foundID = true
		}
		if h == "name" {
			foundName = true
		}
	}
	if !foundID {
		t.Error("expected 'id' header")
	}
	if !foundName {
		t.Error("expected 'name' header")
	}
}

func TestEncodeStrategyToCSVRow(t *testing.T) {
	s := &order.Strategy{
		ID:           1,
		Name:         "ICT_1H_V1_BTCUSDT",
		StrategyType: "FutureCTAV2Strategy",
		Params:       `{"StopLossThreshold":-0.1}`,
	}

	row, err := EncodeToCSVRow(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(row) == 0 {
		t.Fatal("expected non-empty row")
	}
}

func TestEncodeUserOrderToCSVRow(t *testing.T) {
	finishedAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	o := &order.UserOrder{
		ID:             1,
		UserID:         1,
		UserStrategyID: 1,
		PosType:        order.PosTypeFutures,
		Exchange:       "binance",
		BaseAsset:      "BTC",
		QuoteAsset:     "USDT",
		Quantity:       0.1,
		Cash:           100,
		TriggerPrice:   50000,
		Slippage:       0.01,
		Side:           order.SideLong,
		OrderType:      0,
		Status:         2,
		FinishedAt:     &finishedAt,
	}

	row, err := EncodeToCSVRow(o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(row) == 0 {
		t.Fatal("expected non-empty row")
	}
}

func TestEncodeUserOrderNilFinishedAt(t *testing.T) {
	o := &order.UserOrder{
		ID:         1,
		UserID:     1,
		FinishedAt: nil,
	}

	row, err := EncodeToCSVRow(o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(row) == 0 {
		t.Fatal("expected non-empty row")
	}
}

func TestGetCSVHeadersFromStruct(t *testing.T) {
	tests := []struct {
		name           string
		obj            interface{}
		expectedFields []string
	}{
		{
			"user", &order.User{},
			[]string{"id", "name", "exchange", "api_key", "api_secret", "api_password", "created_at", "updated_at"},
		},
		{
			"strategy", &order.Strategy{},
			[]string{"id", "name", "strategy_type", "model_name", "description", "params", "created_at", "updated_at"},
		},
		{
			"leverage_config", &order.LeverageConfig{},
			[]string{"id", "user_id", "asset", "quote", "leverage", "exchange", "status", "pos_type", "created_at", "updated_at"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := GetCSVHeaders(tt.obj)
			if len(headers) != len(tt.expectedFields) {
				t.Errorf("expected %d headers, got %d: %v", len(tt.expectedFields), len(headers), headers)
				return
			}
			for i, expected := range tt.expectedFields {
				if headers[i] != expected {
					t.Errorf("header[%d]: expected '%s', got '%s'", i, expected, headers[i])
				}
			}
		})
	}
}

func TestEncodeToCSVRowConsistency(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	user := &order.User{
		ID:        1,
		Name:      "test",
		Exchange:  "binance",
		CreatedAt: now,
		UpdatedAt: now,
	}

	headers := GetCSVHeaders(user)
	row, err := EncodeToCSVRow(user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(headers) != len(row) {
		t.Errorf("header count (%d) != row count (%d)", len(headers), len(row))
	}
}
