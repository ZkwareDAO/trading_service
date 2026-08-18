package signal

import (
	"trading-service/internal/risk"
)

// SignalHandler 处理行情更新信号
type SignalHandler struct {
	signalCh chan<- risk.RiskSignal
}

// NewSignalHandler 创建信号处理器
func NewSignalHandler(signalCh chan<- risk.RiskSignal) *SignalHandler {
	return &SignalHandler{
		signalCh: signalCh,
	}
}

// OnPriceUpdate 处理价格更新
func (h *SignalHandler) OnPriceUpdate(prices map[string]float64) {
	if h.signalCh == nil {
		return
	}
	select {
	case h.signalCh <- risk.RiskSignal{Version: 0}:
	default:
	}
}