package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/signal"
)

// AgentOrderRequest - trading agent专用的简化订单请求
type AgentOrderRequest struct {
	UserName     string  `json:"user_name"`
	Symbol       string  `json:"symbol"`
	PosType      int     `json:"pos_type"`
	Exchange     string  `json:"exchange"`
	Cash         float64 `json:"cash"`
	Quantity     float64 `json:"quantity"`
	TriggerPrice float64 `json:"trigger_price"`
	Slippage     float64 `json:"slippage"`
	Side         int     `json:"side"`
	OrderType    int     `json:"order_type"`
	Leverage     int     `json:"leverage"`
}

// AgentOrderResponse - agent订单响应
type AgentOrderResponse struct {
	Code    int                     `json:"code"`
	Message string                  `json:"message"`
	Data    *AgentOrderResponseData `json:"data,omitempty"`
}

type AgentOrderResponseData struct {
	UserName       string  `json:"user_name"`
	UserID         uint64  `json:"user_id"`
	UserStrategyID uint64  `json:"user_strategy_id"`
	Symbol         string  `json:"symbol"`
	BaseAsset      string  `json:"base_asset"`
	StrategyName   string  `json:"strategy_name"`
	Exchange       string  `json:"exchange"`
	Side           int     `json:"side"`
	PosType        int     `json:"pos_type"`
	Cash           float64 `json:"cash,omitempty"`
	Leverage       int     `json:"leverage,omitempty"`
	Quantity       float64 `json:"quantity,omitempty"`
	StrategyCash   int     `json:"strategy_cash"`
	StrategyParts  int     `json:"strategy_parts"`
}

// normalizeExchange - B/H/D → binance/hyperliquid/deribit
func normalizeExchange(exchange string) string {
	upper := strings.ToUpper(exchange)
	switch upper {
	case "B":
		return "binance"
	case "H":
		return "hyperliquid"
	case "D":
		return "deribit"
	default:
		return strings.ToLower(exchange)
	}
}

// adaptSymbol - 强制标准化符号：先去掉后缀再添加正确后缀
// 注意：期权符号不做转换，直接使用原生格式
func adaptSymbol(symbol, exchange string) string {
	// 期权符号格式：BTC-24JUL26-64000-P (包含日期和行权价)
	// 识别：包含4个部分，最后一部分是P或C
	parts := strings.Split(symbol, "-")
	if len(parts) == 4 {
		lastPart := parts[3]
		if lastPart == "P" || lastPart == "C" {
			// 这是期权符号，不做任何转换
			return symbol
		}
	}

	// 1. 强制去掉所有可能的quote asset后缀
	baseAsset := symbol
	baseAsset = strings.TrimSuffix(baseAsset, "USDT")
	baseAsset = strings.TrimSuffix(baseAsset, "USDC")
	baseAsset = strings.TrimSuffix(baseAsset, "USD")
	baseAsset = strings.TrimSuffix(baseAsset, "BUSD")

	// 2. 标准化交易所并添加正确后缀
	normalizedExchange := normalizeExchange(exchange)
	switch normalizedExchange {
	case "binance":
		return baseAsset + "USDT"
	case "hyperliquid":
		return baseAsset + "USDC"
	case "deribit":
		return baseAsset + "USD"
	default:
		return baseAsset + "USDT"
	}
}

// generateAgentStrategyName - POSITIVE_1D_1_{symbol}
func generateAgentStrategyName(adaptedSymbol string) string {
	return fmt.Sprintf("POSITIVE_1D_1_%s", adaptedSymbol)
}

// handleAgentOrder - 处理agent订单请求
func (s *Server) handleAgentOrder(w http.ResponseWriter, r *http.Request) {
	// Log all requests to this endpoint
	log.Printf("[/api/v1/orders] Received request: method=%s, remote=%s, user-agent=%s",
		r.Method, r.RemoteAddr, r.Header.Get("User-Agent"))

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AgentOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid JSON")
		return
	}

	// Log the request body
	log.Printf("[/api/v1/orders] Request body: user_name=%s, symbol=%s, exchange=%s, pos_type=%d, cash=%.2f, trigger_price=%.4f, slippage=%.4f, side=%d, order_type=%d, leverage=%d",
		req.UserName, req.Symbol, req.Exchange, req.PosType, req.Cash, req.TriggerPrice, req.Slippage, req.Side, req.OrderType, req.Leverage)

	// 参数验证
	if req.UserName == "" {
		writeBadRequest(w, "user_name is required")
		return
	}
	if req.Symbol == "" {
		writeBadRequest(w, "symbol is required")
		return
	}
	if req.Exchange == "" {
		writeBadRequest(w, "exchange is required")
		return
	}
	if err := validateOrderQuantity(&req); err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	// 限价单(order_type=0)需要验证trigger_price，市价单(order_type=1)不需要
	if req.OrderType == 0 && req.TriggerPrice <= 0 {
		writeBadRequest(w, "trigger_price must be positive for limit orders")
		return
	}

	// 交易所标准化
	normalizedExchange := normalizeExchange(req.Exchange)

	// 根据user_name查找user_id
	userID, err := s.repo.FindUserIDByName(req.UserName, normalizedExchange)
	if err != nil {
		writeBadRequest(w, fmt.Sprintf("user '%s' not found on '%s'", req.UserName, normalizedExchange))
		return
	}

	// 符号适配
	adaptedSymbol := adaptSymbol(req.Symbol, normalizedExchange)
	baseAsset := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(adaptedSymbol, "USDT"), "USDC"), "USD")

	// 策略名称
	strategyName := generateAgentStrategyName(adaptedSymbol)

	// 默认值
	if req.Slippage == 0 {
		req.Slippage = 0.001
	}
	if req.Leverage == 0 {
		req.Leverage = 1
	}

	// 构造StrategySignal
	now := time.Now()
	msg := signal.StrategySignal{
		UserID:           userID,
		Symbol:           adaptedSymbol,
		PosType:          order.PosType(req.PosType),
		StrategyType:     "positive",
		RiskStrategyType: "traditional",
		Strategy: signal.StrategyConfig{
			Name:        "POSITIVE_1D_1",
			Interval:    "1D",
			Version:     "1",
			Cash:        1000, // agent固定值
			Parts:       3,    // agent固定值
			ValidBefore: signal.CustomTime{Time: now.AddDate(1, 0, 0)},
			Leverage:    req.Leverage,
			Description: "Agent auto-created strategy",
		},
		Signal: signal.SignalOrderConfig{
			Exchange:     normalizedExchange,
			Cash:         req.Cash,
			Quantity:     req.Quantity,
			TriggerPrice: req.TriggerPrice,
			Slippage:     req.Slippage,
			OrderType:    req.OrderType,
			ValidBefore:  signal.CustomTime{Time: now.Add(24 * time.Hour)},
		},
	}

	// 设置action
	if req.Side == 0 {
		msg.Signal.Action = signal.ActionBuy
	} else {
		msg.Signal.Action = signal.ActionSell
	}

	// 调用HandleStrategySignal
	userStrategyID, err := s.orderHandler.HandleStrategySignal(msg)
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}

	// 返回响应
	response := AgentOrderResponse{
		Code:    0,
		Message: "success",
		Data: &AgentOrderResponseData{
			UserName:       req.UserName,
			UserID:         userID,
			UserStrategyID: userStrategyID,
			Symbol:         adaptedSymbol,
			BaseAsset:      baseAsset,
			StrategyName:   strategyName,
			Exchange:       normalizedExchange,
			Side:           req.Side,
			PosType:        req.PosType,
			StrategyCash:   1000,
			StrategyParts:  3,
		},
	}

	// 根据pos_type填充不同字段
	if req.PosType == 3 {
		response.Data.Quantity = req.Quantity
	} else {
		response.Data.Cash = req.Cash
		response.Data.Leverage = req.Leverage
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// validateOrderQuantity validates quantity/cash based on pos_type
func validateOrderQuantity(req *AgentOrderRequest) error {
	if req.PosType == 3 { // Options
		if req.Quantity <= 0 {
			return fmt.Errorf("quantity must be positive for options")
		}
		return nil
	}
	// Futures/Spot
	if req.Cash <= 0 {
		return fmt.Errorf("cash must be positive")
	}
	return nil
}
