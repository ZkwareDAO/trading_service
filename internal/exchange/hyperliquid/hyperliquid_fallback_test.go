package hyperliquid

import (
	"context"
	"testing"

	hyperliquid "github.com/sonirico/go-hyperliquid"
)

// TestGetOrder_FallbackToUserFills 测试 GetOrder 使用 userFills fallback
func TestGetOrder_FallbackToUserFills(t *testing.T) {
	// Mock: QueryOrderByOid 返回 not found (模拟订单已成交)
	infoMock := &mockInfoClient{
		queryOrderFunc: func(ctx context.Context, user string, oid int64) (*hyperliquid.OrderQueryResult, error) {
			return nil, nil // not found
		},
		userFillsFunc: func(ctx context.Context, params hyperliquid.UserFillsParams) ([]hyperliquid.Fill, error) {
			// 返回包含目标订单的成交记录
			return []hyperliquid.Fill{
				{
					Oid:   486861639558,
					Coin:  "SOL",
					Price: "82.168",
					Size:  "12.18",
					Side:  "B",
				},
			}, nil
		},
	}

	h := &Hyperliquid{
		info:        infoMock,
		accountAddr: "0xtest",
	}

	// 测试 GetOrder
	info, err := h.GetOrder(486861639558, "SOL")
	if err != nil {
		t.Fatalf("GetOrder failed: %v", err)
	}

	// 验证返回的订单信息
	if info.OrderID != 486861639558 {
		t.Errorf("OrderID: got %d, want 486861639558", info.OrderID)
	}
	if info.Symbol != "SOL" {
		t.Errorf("Symbol: got %s, want SOL", info.Symbol)
	}
	if info.Status != "FILLED" {
		t.Errorf("Status: got %s, want FILLED", info.Status)
	}
	if info.Price != 82.168 {
		t.Errorf("Price: got %f, want 82.168", info.Price)
	}
	if info.Filled != 12.18 {
		t.Errorf("Filled: got %f, want 12.18", info.Filled)
	}
}