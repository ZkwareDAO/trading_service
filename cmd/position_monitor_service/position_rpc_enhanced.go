package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

// PositionRPCHandler handles RPC requests from signal handler and scanner.
type PositionRPCHandler struct {
	repo          *persistence.StateRepository
	priceRuntimes []PriceRuntime
	resolver      exchangeResolver // 用于直接从交易所API获取价格
}

func NewPositionRPCHandler(repo *persistence.StateRepository, priceRuntimes []PriceRuntime, resolver exchangeResolver) *PositionRPCHandler {
	return &PositionRPCHandler{repo: repo, priceRuntimes: priceRuntimes, resolver: resolver}
}

// HandleGetMarketPriceEnhanced returns current market price with enhanced fallback chain.
// POST /rpc/v1/market-price/get
// Fallback chain: PriceRuntime (WS) → Exchange REST API (GetPrice) → Active positions
// Fixes: 市价开单时没有持仓导致无法获取价格
func (h *PositionRPCHandler) HandleGetMarketPriceEnhanced(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GetMarketPriceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Exchange == "" || req.Symbol == "" {
		http.Error(w, "exchange and symbol are required", http.StatusBadRequest)
		return
	}

	price, err := h.fetchMarketPrice(req.Exchange, req.Symbol)
	if err != nil {
		log.Printf("RPC: get market price failed for %s/%s: %v", req.Exchange, req.Symbol, err)
		http.Error(w, fmt.Sprintf("price not found: %v", err), http.StatusNotFound)
		return
	}

	log.Printf("RPC: get market price SUCCESS for %s/%s: price=%.4f (source=%s)", req.Exchange, req.Symbol, price.Price, price.Source)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(price)
}

type MarketPriceResponse struct {
	Exchange string  `json:"exchange"`
	Symbol   string  `json:"symbol"`
	Price    float64 `json:"price"`
	Source   string  `json:"source"` // "ws", "rest_api", "active_position"
}

// fetchMarketPrice implements fallback chain: WS → REST API (GetPrice) → Active positions
//
// IMPORTANT: Hyperliquid WebSocket pushes bare coin names (e.g., "NEAR")
// while signal handler requests full symbols (e.g., "NEARUSDC").
// We convert the symbol format for WebSocket lookup.
func (h *PositionRPCHandler) fetchMarketPrice(exchange, symbol string) (*MarketPriceResponse, error) {
	// 1. Try PriceRuntimes first (real-time WebSocket prices)
	wsSymbol := convertToWebSocketSymbol(exchange, symbol)

	for _, runtime := range h.priceRuntimes {
		if runtime.ExchangeName() == exchange {
			snapshot := runtime.Snapshot()
			if wsPrice, ok := snapshot[wsSymbol]; ok && wsPrice > 0 {
				return &MarketPriceResponse{Exchange: exchange, Symbol: symbol, Price: wsPrice, Source: "ws"}, nil
			}
		}
	}

	// 2. Fallback: Query exchange REST API directly via GetPrice method
	// This is critical for market orders when no position exists yet
	if h.resolver != nil {
		userID := findUserWithExchange(h.repo, exchange)
		if userID > 0 {
			ex, err := h.resolver.ResolveExchange(userID, exchange)
			if err == nil {
				restPrice, err := ex.GetPrice(symbol)
				if err == nil && restPrice > 0 {
					return &MarketPriceResponse{Exchange: exchange, Symbol: symbol, Price: restPrice, Source: "rest_api"}, nil
				}
				log.Printf("RPC: exchange GetPrice failed for %s/%s: %v", exchange, symbol, err)
			} else {
				log.Printf("RPC: resolve exchange failed for user %d/%s: %v", userID, exchange, err)
			}
		}
	}

	// 3. Fallback: Try to get price from active positions
	active := true
	positions := h.repo.ListUserOrderPositionsByFilter(persistence.UserOrderPositionFilter{Active: &active})
	for _, pos := range positions {
		if pos.Exchange == exchange && pos.Asset == symbol && pos.CurrentPrice > 0 {
			return &MarketPriceResponse{Exchange: exchange, Symbol: symbol, Price: pos.CurrentPrice, Source: "active_position"}, nil
		}
	}

	return nil, fmt.Errorf("no price source available for %s/%s (ws/rest_api/active_position all failed)", exchange, symbol)
}

// convertToWebSocketSymbol converts symbol to WebSocket format.
// Hyperliquid WS pushes bare coin names ("NEAR"), not full symbols ("NEARUSDC").
func convertToWebSocketSymbol(exchange, symbol string) string {
	if exchange == "hyperliquid" {
		return strings.TrimSuffix(symbol, "USDC")
	}
	return symbol
}

// findUserWithExchange finds any user with the specified exchange for API credentials.
func findUserWithExchange(repo *persistence.StateRepository, exchange string) uint64 {
	users := repo.ListUsers()
	for _, u := range users {
		if u.Exchange == exchange {
			return u.ID
		}
	}
	return 0
}

// HandleQueryOrderPositionMetadata handles metadata query for user_order.
// POST /rpc/v1/order-position-metadata/query
func (h *PositionRPCHandler) HandleQueryOrderPositionMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req rpc.QueryOrderPositionMetadataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.UserOrderID == 0 {
		http.Error(w, "user_order_id is required", http.StatusBadRequest)
		return
	}

	// Query user_order to get user_strategy_id and other metadata
	userOrder, err := h.repo.GetUserOrderByID(req.UserOrderID)
	if err != nil {
		http.Error(w, fmt.Sprintf("user_order %d not found: %v", req.UserOrderID, err), http.StatusNotFound)
		return
	}

	// Query user_strategy to get leverage
	userStrategy, err := h.repo.GetUserStrategyByID(userOrder.UserStrategyID)
	if err != nil {
		http.Error(w, fmt.Sprintf("user_strategy %d not found: %v", userOrder.UserStrategyID, err), http.StatusNotFound)
		return
	}

	// Use trigger_price as fallback price for market orders
	fallbackPrice := userOrder.TriggerPrice
	if fallbackPrice <= 0 {
		// Fallback to current market price
		symbol := normalizeSymbol(userOrder.BaseAsset, userOrder.Exchange)
		priceResp, err := h.fetchMarketPrice(userOrder.Exchange, symbol)
		if err == nil && priceResp.Price > 0 {
			fallbackPrice = priceResp.Price
		}
	}

	// Get leverage from user_order's strategy params or default to 1
	leverage := 1 // Default leverage
	strategy, err := h.repo.GetStrategyByID(userStrategy.StrategyID)
	if err == nil && strategy.Params != "" {
		// Try to parse leverage from strategy params JSON
		var params map[string]interface{}
		if json.Unmarshal([]byte(strategy.Params), &params) == nil {
			if lev, ok := params["leverage"].(float64); ok && lev > 0 {
				leverage = int(lev)
			}
		}
	}

	resp := rpc.QueryOrderPositionMetadataResponse{
		UserStrategyID: userOrder.UserStrategyID,
		Leverage:       leverage,
		FallbackPrice:  fallbackPrice,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// normalizeSymbol converts baseAsset to exchange-specific symbol format
// e.g., "BTC" + "binance" -> "BTCUSDT", "BTC" + "hyperliquid" -> "BTCUSDC"
func normalizeSymbol(baseAsset, exchange string) string {
	suffix := "USDT"
	if strings.EqualFold(exchange, "hyperliquid") {
		suffix = "USDC"
	}
	return strings.ToUpper(baseAsset) + suffix
}
