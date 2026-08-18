package reporter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientTimeout(t *testing.T) {
	// 测试HTTP客户端应该有超时配置
	// 创建一个延迟35秒的服务器（超过预期的30秒超时）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(35 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	start := time.Now()
	_, err := FetchExchangePositions(server.URL+"/api/v1/exchange/positions", "test", "binance")
	elapsed := time.Since(start)

	// 应该在31秒内返回（30秒超时 + 1秒缓冲）
	if elapsed > 31*time.Second {
		t.Errorf("HTTP client should timeout in 30s, but took %v", elapsed)
	}

	if err == nil {
		t.Error("Expected timeout error for request exceeding 30s")
	}
}

func TestStringBuilderefficiency(t *testing.T) {
	// 测试strings.Builder效率
	positions := make([]PositionData, 100)
	for i := range positions {
		positions[i] = PositionData{
			Symbol:     "BTCUSDT",
			Side:       "LONG",
			Quantity:   0.1,
			EntryPrice: 45000.0,
			MarkPrice:  45500.0,
			PnL:        50.0,
			Leverage:   10,
			ROI:        1.11,
		}
	}

	start := time.Now()
	msg := FormatWeChatMessage(positions, "test", "binance")
	elapsed := time.Since(start)

	// 应该在1秒内完成
	if elapsed > 1*time.Second {
		t.Errorf("FormatWeChatMessage too slow for 100 positions: %v", elapsed)
	}

	if len(msg) == 0 {
		t.Error("Message should not be empty")
	}
}

func TestSideTranslationMap(t *testing.T) {
	// 测试side翻译map不应该每次创建
	positions := []PositionData{
		{Symbol: "BTC", Side: "LONG"},
		{Symbol: "ETH", Side: "SHORT"},
		{Symbol: "SOL", Side: "UNKNOWN"},
	}

	msg := FormatWeChatMessage(positions, "test", "binance")

	// 验证翻译正确
	if !strings.Contains(msg, "多单") {
		t.Error("Should contain 多单 for LONG")
	}
	if !strings.Contains(msg, "空单") {
		t.Error("Should contain 空单 for SHORT")
	}
	if !strings.Contains(msg, "UNKNOWN") {
		t.Error("Should contain UNKNOWN for unknown side")
	}
}