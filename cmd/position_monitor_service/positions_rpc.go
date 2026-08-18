package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

type QueryUserOrderPositionsRequest struct {
	UserStrategyID   uint64 `json:"user_strategy_id"`
	Side             *int   `json:"side,omitempty"`
	Active           *bool  `json:"active,omitempty"`
	Asset            string `json:"asset,omitempty"`
	PosType          *int   `json:"pos_type,omitempty"`
	IncludePositions bool   `json:"include_positions,omitempty"`
}

type QueryUserOrderPositionsResponse struct {
	UserStrategyID uint64                 `json:"user_strategy_id"`
	Side           *int                   `json:"side,omitempty"`
	Active         *bool                  `json:"active,omitempty"`
	Asset          string                 `json:"asset,omitempty"`
	PosType        *int                   `json:"pos_type,omitempty"`
	Count          int                    `json:"count"`
	Positions      []UserOrderPositionDTO `json:"positions,omitempty"`
}

type UserOrderPositionDTO struct {
	ID             uint64  `json:"id"`
	UserID         uint64  `json:"user_id"`
	UserOrderID    uint64  `json:"user_order_id"`
	UserStrategyID uint64  `json:"user_strategy_id"`
	Exchange       string  `json:"exchange"`
	PosType        int     `json:"pos_type"`
	Asset          string  `json:"asset"`
	Side           int     `json:"side"`
	Quantity       float64 `json:"quantity"`
	PosPrice       float64 `json:"pos_price"`
	CurrentPrice   float64 `json:"current_price"`
	Leverage       int     `json:"leverage"`
	Deleted        int     `json:"deleted"`
}

type PositionQueryHandler struct {
	repo          *persistence.StateRepository
	priceRuntimes []PriceRuntime
}

func NewPositionQueryHandler(repo *persistence.StateRepository, priceRuntimes []PriceRuntime) *PositionQueryHandler {
	return &PositionQueryHandler{repo: repo, priceRuntimes: priceRuntimes}
}

func (h *PositionQueryHandler) HandleQueryUserOrderPositions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req QueryUserOrderPositionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.UserStrategyID == 0 {
		http.Error(w, "user_strategy_id is required", http.StatusBadRequest)
		return
	}

	filter := persistence.UserOrderPositionFilter{UserStrategyID: req.UserStrategyID, Asset: req.Asset}
	if req.Side != nil {
		side := order.Side(*req.Side)
		filter.Side = &side
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	filter.Active = &active
	if req.PosType != nil {
		posType := order.PosType(*req.PosType)
		filter.PosType = &posType
	}

	positions := h.repo.ListUserOrderPositionsByFilter(filter)
	resp := QueryUserOrderPositionsResponse{
		UserStrategyID: req.UserStrategyID,
		Side:           req.Side,
		Active:         &active,
		Asset:          req.Asset,
		PosType:        req.PosType,
		Count:          len(positions),
	}
	if req.IncludePositions {
		resp.Positions = make([]UserOrderPositionDTO, 0, len(positions))
		for _, pos := range positions {
			resp.Positions = append(resp.Positions, toUserOrderPositionDTO(pos))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func toUserOrderPositionDTO(pos *order.UserOrderPosition) UserOrderPositionDTO {
	return UserOrderPositionDTO{
		ID:             pos.ID,
		UserID:         pos.UserID,
		UserOrderID:    pos.UserOrderID,
		UserStrategyID: pos.UserStrategyID,
		Exchange:       pos.Exchange,
		PosType:        int(pos.PosType),
		Asset:          pos.Asset,
		Side:           int(pos.Side),
		Quantity:       pos.Quantity,
		PosPrice:       pos.PosPrice,
		CurrentPrice:   pos.CurrentPrice,
		Leverage:       pos.Leverage,
		Deleted:        pos.Deleted,
	}
}

// GetMarketPriceRequest requests current market price for a symbol.
type GetMarketPriceRequest struct {
	Exchange string `json:"exchange"`
	Symbol   string `json:"symbol"`
}

// GetMarketPriceResponse contains the current market price.
type GetMarketPriceResponse struct {
	Exchange string  `json:"exchange"`
	Symbol   string  `json:"symbol"`
	Price    float64 `json:"price"`
}

// HandleGetMarketPrice returns current market price for a symbol.
func (h *PositionQueryHandler) HandleGetMarketPrice(w http.ResponseWriter, r *http.Request) {
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

	// Try to get price from PriceRuntimes first (real-time WebSocket prices)
	for _, runtime := range h.priceRuntimes {
		if runtime.ExchangeName() == req.Exchange {
			snapshot := runtime.Snapshot()
			if price, ok := snapshot[req.Symbol]; ok && price > 0 {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(GetMarketPriceResponse{Exchange: req.Exchange, Symbol: req.Symbol, Price: price})
				return
			}
		}
	}

	// Fallback: Try to get price from active positions
	positions := h.repo.ListUserOrderPositionsByFilter(persistence.UserOrderPositionFilter{Active: &[]bool{true}[0]})
	for _, pos := range positions {
		if pos.Exchange == req.Exchange && pos.Asset == req.Symbol && pos.CurrentPrice > 0 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(GetMarketPriceResponse{Exchange: req.Exchange, Symbol: req.Symbol, Price: pos.CurrentPrice})
			return
		}
	}

	// No price found
	http.Error(w, "price not found for exchange/symbol", http.StatusNotFound)
}

// HandleCreateUprunningOrder handles RPC requests to create an uprunning_order.
func (h *PositionQueryHandler) HandleCreateUprunningOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req rpc.CreateUprunningOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.UserID == 0 || req.RelationID == 0 || req.RelationType == "" {
		http.Error(w, "user_id, relation_id, and relation_type are required", http.StatusBadRequest)
		return
	}

	now := time.Now()
	uo := &order.UprunningOrder{
		UserID:              req.UserID,
		RelationID:          req.RelationID,
		RelationType:        req.RelationType,
		RiskCtrlStratID:     req.RiskCtrlStratID,
		UserOrderPositionID: req.UserOrderPositionID,
		UserPositionID:      req.UserPositionID,
		Exchange:            req.Exchange,
		Symbol:              req.Symbol,
		PosType:             order.PosType(req.PosType),
		ExchangeOrderID:     req.ExchangeOrderID,
		ExchangeOrderStatus: req.ExchangeOrderStatus,
		ExchangeOrderPrice:  req.ExchangeOrderPrice,
		ExchangeOrderQty:    req.ExchangeOrderQty,
		ExchangeUpdateTime:  &now,
		Side:                order.Side(req.Side),
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	uprunningOrderID := h.repo.CreateUprunningOrder(uo)
	log.Printf("RPC: created uprunning_order via RPC: uprunningOrderID=%d, relationID=%d, relationType=%s, exchangeOrderID=%d",
		uprunningOrderID, req.RelationID, req.RelationType, req.ExchangeOrderID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rpc.CreateUprunningOrderResponse{UprunningOrderID: uprunningOrderID})
}
