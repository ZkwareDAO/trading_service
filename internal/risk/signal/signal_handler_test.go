package signal

import (
	"testing"
	"time"

	"trading-service/internal/risk"
)

// Test 1: NewSignalHandler 应该创建处理器
func TestNewSignalHandler_CreatesHandler(t *testing.T) {
	ch := make(chan risk.RiskSignal, 10)
	handler := NewSignalHandler(ch)

	if handler == nil {
		t.Error("expected handler to be created")
	}
}

// Test 2: OnPriceUpdate 应该发送信号
func TestSignalHandler_OnPriceUpdateSendsSignal(t *testing.T) {
	ch := make(chan risk.RiskSignal, 10)
	handler := NewSignalHandler(ch)

	prices := map[string]float64{
		"BTCUSDT": 50000.0,
		"ETHUSDT": 3000.0,
	}

	handler.OnPriceUpdate(prices)

	select {
	case sig := <-ch:
		_ = sig
	case <-time.After(time.Second):
		t.Error("expected signal to be sent")
	}
}

// Test 3: OnPriceUpdate 应该处理 nil 通道
func TestSignalHandler_OnPriceUpdateHandlesNilChannel(t *testing.T) {
	handler := NewSignalHandler(nil)

	handler.OnPriceUpdate(map[string]float64{"BTCUSDT": 50000.0})
}

// Test 4: OnPriceUpdate 应该在通道满时跳过
func TestSignalHandler_OnPriceUpdateSkipsOnFullChannel(t *testing.T) {
	ch := make(chan risk.RiskSignal, 1)
	handler := NewSignalHandler(ch)

	ch <- risk.RiskSignal{Version: 1}

	done := make(chan struct{})
	go func() {
		handler.OnPriceUpdate(map[string]float64{"BTCUSDT": 50000.0})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("OnPriceUpdate blocked on full channel")
	}
}