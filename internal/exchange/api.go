package exchange

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

// ExchangeAPI is the HTTP API for the exchange service.
type ExchangeAPI struct {
	repo     *persistence.StateRepository
	exchange Exchange
	executor *OrderExecutor
	manager  *PositionManager
}

// NewExchangeServer creates an HTTP test server.
func NewExchangeServer(repo *persistence.StateRepository, ex Exchange) *httptest.Server {
	api := &ExchangeAPI{
		repo:     repo,
		exchange: ex,
		executor: NewOrderExecutor(repo, ex),
		manager:  NewPositionManager(repo, ex),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", api.healthHandler)
	mux.HandleFunc("/api/v1/orders", api.createOrderHandler)
	mux.HandleFunc("/api/v1/orders/", api.cancelOrderHandler) // DELETE /api/v1/orders/:id
	mux.HandleFunc("/api/v1/prices", api.getPriceHandler)
	mux.HandleFunc("/api/v1/positions", api.listPositionsHandler)
	mux.HandleFunc("/api/v1/leverage", api.setLeverageHandler)

	return httptest.NewServer(mux)
}

// APIResponse is the standard API response envelope.
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// CreateOrderAPIRequest for API input.
type CreateOrderAPIRequest struct {
	UserID       uint64  `json:"user_id"`
	Symbol       string  `json:"symbol"`
	Side         int     `json:"side"`
	OrderType    int     `json:"order_type"`
	Quantity     float64 `json:"quantity"`
	Price        float64 `json:"price"`
	PosType      int     `json:"pos_type"`
	PositionSide string  `json:"position_side"`
	RelationID   uint64  `json:"relation_id"`
	RelationType string  `json:"relation_type"`
}

// SetLeverageRequest for API input.
type SetLeverageRequest struct {
	Symbol   string `json:"symbol"`
	Leverage int    `json:"leverage"`
}

func (a *ExchangeAPI) healthHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(APIResponse{
		Code:    0,
		Message: "healthy",
	})
}

func (a *ExchangeAPI) createOrderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateOrderAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIResponse{Code: 1, Message: "invalid JSON"})
		return
	}

	uo := &order.UprunningOrder{
		UserID:             req.UserID,
		RelationID:         req.RelationID,
		RelationType:       req.RelationType,
		Symbol:             req.Symbol,
		PosType:            order.PosType(req.PosType),
		Exchange:           "binance",
		Side:               order.Side(req.Side),
		ExchangeOrderPrice: req.Price,
		ExchangeOrderQty:   req.Quantity,
	}

	// Map request params to exchange types
	var side OrderSide = OrderSideBuy
	if req.Side == 1 {
		side = OrderSideSell
	}
	var orderType OrderType = OrderTypeLimit
	if req.OrderType == 1 {
		orderType = OrderTypeMarket
	}
	var positionSide PositionSide = PositionSideLong
	if req.PositionSide == "SHORT" {
		positionSide = PositionSideShort
	}

	uoID := a.executor.CreateOrder(uo, side, orderType, positionSide)

	json.NewEncoder(w).Encode(APIResponse{
		Code:    0,
		Message: "success",
		Data: map[string]interface{}{
			"order_id": uoID,
			"symbol":   req.Symbol,
			"status":   "NEW",
			"price":    req.Price,
			"quantity": req.Quantity,
		},
	})
}

func (a *ExchangeAPI) cancelOrderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse order ID from URL path: /api/v1/orders/:id
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIResponse{Code: 1, Message: "order ID required"})
		return
	}
	orderIDStr := parts[5]
	orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIResponse{Code: 1, Message: "invalid order ID"})
		return
	}

	if err := a.executor.CancelOrder(orderID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIResponse{Code: 1, Message: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(APIResponse{
		Code:    0,
		Message: "success",
	})
}

func (a *ExchangeAPI) getPriceHandler(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIResponse{Code: 1, Message: "symbol required"})
		return
	}

	price, err := a.manager.GetPrice(symbol)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIResponse{Code: 1, Message: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(APIResponse{
		Code:    0,
		Message: "success",
		Data: map[string]string{
			symbol: strconv.FormatFloat(price, 'f', 2, 64),
		},
	})
}

func (a *ExchangeAPI) listPositionsHandler(w http.ResponseWriter, r *http.Request) {
	positions := a.manager.ListActivePositions()
	json.NewEncoder(w).Encode(APIResponse{
		Code:    0,
		Message: "success",
		Data:    positions,
	})
}

func (a *ExchangeAPI) setLeverageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SetLeverageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIResponse{Code: 1, Message: "invalid JSON"})
		return
	}

	if err := a.manager.SetLeverage(req.Symbol, req.Leverage); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIResponse{Code: 1, Message: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(APIResponse{
		Code:    0,
		Message: "success",
	})
}
