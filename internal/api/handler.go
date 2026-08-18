package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"

	"trading-service/internal/persistence"
	"trading-service/internal/signal"
)

// Server is the HTTP server for the order service.
type Server struct {
	handler      *signal.Handler
	repo         *persistence.StateRepository
	orderHandler *signal.Handler // For agent order handler
}

// RegisterHandlers registers all HTTP handlers on the given ServeMux.
func RegisterHandlers(mux *http.ServeMux, repo *persistence.StateRepository, h *signal.Handler) {
	s := &Server{handler: h, repo: repo, orderHandler: h}
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/api/v1/state", s.stateHandler)
	mux.HandleFunc("/api/v1/users", s.listUsersHandler)         // GET - list users
	mux.HandleFunc("/api/v1/users/create", s.createUserHandler) // POST - create user
	mux.HandleFunc("/api/v1/user-strategies", s.listUserStrategiesHandler)
	mux.HandleFunc("/api/v1/signals", s.signalHandler)
	mux.HandleFunc("/api/v1/orders", s.handleAgentOrder) // Agent专用订单接口
}

// NewServer creates an HTTP test server.
func NewServer(repo *persistence.StateRepository, h *signal.Handler) *httptest.Server {
	mux := http.NewServeMux()
	RegisterHandlers(mux, repo, h)
	return httptest.NewServer(mux)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
	})
}

func (s *Server) stateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"users": len(s.repo.ListUsers()),
	})
}

func (s *Server) listUsersHandler(w http.ResponseWriter, r *http.Request) {
	// Log all access to this sensitive endpoint
	log.Printf("ACCESS: path=/api/v1/users, method=%s, remote=%s, user-agent=%s",
		r.Method, r.RemoteAddr, r.Header.Get("User-Agent"))

	w.Header().Set("Content-Type", "application/json")
	users := s.repo.ListUsers()

	// Only return safe fields: name, exchange, created_at, updated_at
	// Hide sensitive fields: id, api_key, api_secret, api_password
	safeUsers := make([]map[string]interface{}, len(users))
	for i, u := range users {
		safeUsers[i] = map[string]interface{}{
			"name":       u.Name,
			"exchange":   u.Exchange,
			"created_at": u.CreatedAt,
			"updated_at": u.UpdatedAt,
		}
	}
	json.NewEncoder(w).Encode(safeUsers)
}

func (s *Server) signalHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("signal handler panic recovered: %v", err)
			http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
		}
	}()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeBadRequest(w, "invalid body")
		return
	}

	log.Printf("Raw signal received: %s", string(body))

	var nested signal.StrategySignal
	if err := json.Unmarshal(body, &nested); err != nil {
		writeBadRequest(w, "invalid JSON")
		return
	}

	if nested.Signal.Action != "" {
		log.Printf("Routing to HandleStrategySignal: action=%s, userID=%d, symbol=%s", nested.Signal.Action, nested.UserID, nested.Symbol)
		if _, err := s.handler.HandleStrategySignal(nested); err != nil {
			writeBadRequest(w, err.Error())
			return
		}
	} else {
		var req SignalRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeBadRequest(w, "invalid JSON")
			return
		}

		log.Printf("Routing to HandleSignal: userID=%d, strategyID=%d, symbol=%s, exchange=%s, side=%d", req.UserID, req.UserStrategyID, req.Symbol, req.Exchange, req.Side)

		sig := signal.Signal{
			UserID:         req.UserID,
			Symbol:         req.Symbol,
			UserStrategyID: req.UserStrategyID,
			PosType:        req.PosType,
			Exchange:       req.Exchange,
			Cash:           req.Cash,
			Quantity:       req.Quantity,
			TriggerPrice:   req.TriggerPrice,
			Slippage:       req.Slippage,
			Side:           req.Side,
			OrderType:      req.OrderType,
			Leverage:       req.Leverage,
		}

		if err := s.handler.HandleSignal(sig); err != nil {
			writeBadRequest(w, err.Error())
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "success"})
}

func writeBadRequest(w http.ResponseWriter, msg string) {
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// SignalRequest for API input.
type SignalRequest struct {
	UserID         uint64  `json:"user_id"`
	UserStrategyID uint64  `json:"user_strategy_id"`
	Symbol         string  `json:"symbol"`
	PosType        int     `json:"pos_type"`
	Exchange       string  `json:"exchange"`
	Cash           float64 `json:"cash"`
	Quantity       float64 `json:"quantity"`
	TriggerPrice   float64 `json:"trigger_price"`
	Slippage       float64 `json:"slippage"`
	Side           int     `json:"side"`
	OrderType      int     `json:"order_type"`
	Leverage       int     `json:"leverage"`
}
