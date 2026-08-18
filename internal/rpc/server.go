package rpc

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

// Server is the RPC server for user_order_service.
type Server struct {
	repo         *persistence.StateRepository
	mux          *http.ServeMux
	strategyMu   sync.RWMutex // Protects strategy creation
	userStrategy sync.RWMutex // Protects user_strategy creation
}

// NewServer creates a new RPC server.
func NewServer(repo *persistence.StateRepository) *Server {
	s := &Server{
		repo: repo,
		mux:  http.NewServeMux(),
	}
	s.mux.HandleFunc("/rpc/v1/order/status/update", s.handleUpdateOrderStatus)
	s.mux.HandleFunc("/rpc/v1/order/position-metadata", s.handleQueryOrderPositionMetadata)
	s.mux.HandleFunc("/rpc/v1/filters/reload", s.handleReloadFilters)

		// Strategy management RPC endpoints
		s.mux.HandleFunc("/rpc/v1/strategy/get-or-create", s.handleGetOrCreateStrategy)
		s.mux.HandleFunc("/rpc/v1/strategy-asset/get-or-create", s.handleGetOrCreateStrategyAsset)
		s.mux.HandleFunc("/rpc/v1/user-strategy/get-or-create", s.handleGetOrCreateUserStrategy)
	return s
}

// QueryOrderPositionMetadataRequest is the RPC request body for position metadata.
type QueryOrderPositionMetadataRequest struct {
	UserOrderID uint64 `json:"user_order_id"`
}

// QueryOrderPositionMetadataResponse contains UOS-owned data needed by PM when creating a position.
type QueryOrderPositionMetadataResponse struct {
	UserOrderID    uint64  `json:"user_order_id"`
	UserStrategyID uint64  `json:"user_strategy_id"`
	Leverage       int     `json:"leverage"`
	FallbackPrice  float64 `json:"fallback_price"`
}

// UpdateUserOrderStatusRequest is the RPC request body for updating order status.
type UpdateUserOrderStatusRequest struct {
	UserOrderID uint64  `json:"user_order_id"`
	Status      int     `json:"status"`      // 2=FILLED, 3=FAILED
	FinishedAt  *string `json:"finished_at"` // optional
}

func (s *Server) handleQueryOrderPositionMetadata(w http.ResponseWriter, r *http.Request) {
	var req QueryOrderPositionMetadataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.UserOrderID == 0 {
		http.Error(w, "user_order_id is required", http.StatusBadRequest)
		return
	}

	userOrder, err := s.repo.GetUserOrderByID(req.UserOrderID)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	leverage := 1
	if lc, err := s.repo.FindLeverageConfig(userOrder.UserID, userOrder.BaseAsset, userOrder.PosType, userOrder.Exchange); err == nil && lc != nil && lc.Leverage > 0 {
		leverage = lc.Leverage
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(QueryOrderPositionMetadataResponse{
		UserOrderID:    req.UserOrderID,
		UserStrategyID: userOrder.UserStrategyID,
		Leverage:       leverage,
		FallbackPrice:  userOrder.TriggerPrice,
	})
}

func (s *Server) handleUpdateOrderStatus(w http.ResponseWriter, r *http.Request) {
	var req UpdateUserOrderStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	order, err := s.repo.GetUserOrderByID(req.UserOrderID)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	// Copy before mutating to avoid data race on shared pointer
	updated := *order
	updated.Status = req.Status
	updated.UpdatedAt = time.Now()
	if req.FinishedAt != nil {
		t, err := time.Parse(time.RFC3339, *req.FinishedAt)
		if err != nil {
			http.Error(w, "invalid finished_at format", http.StatusBadRequest)
			return
		}
		updated.FinishedAt = &t
	}

	if err := s.repo.UpdateUserOrder(&updated); err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Handle returns the HTTP handler for the server.
func (s *Server) Handle() http.Handler {
	return s.mux
}

// handleReloadFilters handles reload exchange_symbol_filters from CSV.
// This endpoint is called by PMS after it updates the filters.
func (s *Server) handleReloadFilters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := s.repo.ReloadExchangeSymbolFilters(); err != nil {
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleGetOrCreateStrategy handles GetOrCreateStrategy RPC requests.
func (s *Server) handleGetOrCreateStrategy(w http.ResponseWriter, r *http.Request) {
	var req GetOrCreateStrategyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// First check with read lock
	s.strategyMu.RLock()
	strategy, err := s.repo.GetStrategyByName(req.Name)
	s.strategyMu.RUnlock()

	if err == nil {
		// Strategy already exists
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetOrCreateStrategyResponse{
			StrategyID:   strategy.ID,
			Name:         strategy.Name,
			StrategyType: strategy.StrategyType,
			Created:      false,
		})
		return
	}

	// Acquire write lock for creation
	s.strategyMu.Lock()
	defer s.strategyMu.Unlock()

	// Double-check: another goroutine might have created it
	strategy, err = s.repo.GetStrategyByName(req.Name)
	if err == nil {
		// Strategy was created by another goroutine
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetOrCreateStrategyResponse{
			StrategyID:   strategy.ID,
			Name:         strategy.Name,
			StrategyType: strategy.StrategyType,
			Created:      false,
		})
		return
	}

	// Create new strategy
	now := time.Now()
	strategy = &order.Strategy{
		Name:         req.Name,
		StrategyType: req.StrategyType,
		ModelName:    req.ModelName,
		Description:  req.Description,
		Params:       req.Params,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	strategy.ID = s.repo.CreateStrategy(strategy)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetOrCreateStrategyResponse{
		StrategyID:   strategy.ID,
		Name:         strategy.Name,
		StrategyType: strategy.StrategyType,
		Created:      true,
	})
}

// handleGetOrCreateStrategyAsset handles GetOrCreateStrategyAsset RPC requests.
func (s *Server) handleGetOrCreateStrategyAsset(w http.ResponseWriter, r *http.Request) {
	var req GetOrCreateStrategyAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Asset == "" || req.StrategyID == 0 {
		http.Error(w, "name, asset, and strategy_id are required", http.StatusBadRequest)
		return
	}

	// Check if strategy asset exists
	asset, err := s.repo.GetStrategyAssetByNameAssetStrategy(req.Name, req.Asset, req.StrategyID)
	if err == nil {
		// Strategy asset already exists
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetOrCreateStrategyAssetResponse{
			StrategyAssetID: asset.ID,
			Created:         false,
		})
		return
	}

	// Create new strategy asset
	now := time.Now()
	asset = &order.StrategyAsset{
		Name:       req.Name,
		Asset:      req.Asset,
		StrategyID: req.StrategyID,
		PosType:    order.PosType(req.PosType),
		Sort:       req.Sort,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	asset.ID = s.repo.CreateStrategyAsset(asset)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetOrCreateStrategyAssetResponse{
		StrategyAssetID: asset.ID,
		Created:         true,
	})
}

// handleGetOrCreateUserStrategy handles GetOrCreateUserStrategy RPC requests.
func (s *Server) handleGetOrCreateUserStrategy(w http.ResponseWriter, r *http.Request) {
	var req GetOrCreateUserStrategyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.UserID == 0 || req.Name == "" || req.StrategyID == 0 {
		http.Error(w, "user_id, name, and strategy_id are required", http.StatusBadRequest)
		return
	}

	// First check with read lock
	s.userStrategy.RLock()
	us, err := s.repo.GetUserStrategyByUserNameStrategy(req.UserID, req.Name, req.StrategyID)
	s.userStrategy.RUnlock()

	if err == nil {
		// User strategy already exists
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetOrCreateUserStrategyResponse{
			UserStrategyID: us.ID,
			Created:        false,
		})
		return
	}

	// Parse valid_before time
	validBefore, err := time.Parse(time.RFC3339, req.ValidBefore)
	if err != nil {
		http.Error(w, "invalid valid_before format", http.StatusBadRequest)
		return
	}

	// Acquire write lock for creation
	s.userStrategy.Lock()
	defer s.userStrategy.Unlock()

	// Double-check: another goroutine might have created it
	us, err = s.repo.GetUserStrategyByUserNameStrategy(req.UserID, req.Name, req.StrategyID)
	if err == nil {
		// User strategy was created by another goroutine
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetOrCreateUserStrategyResponse{
			UserStrategyID: us.ID,
			Created:        false,
		})
		return
	}

	// Create new user strategy
	now := time.Now()
	us = &order.UserStrategy{
		UserID:           req.UserID,
		Name:             req.Name,
		Exchange:         req.Exchange,
		ValidBefore:      validBefore,
		Cash:             req.Cash,
		Parts:            req.Parts,
		Status:           req.Status,
		StrategyID:       req.StrategyID,
		RiskStrategyType: req.RiskStrategyType,
		OrdersNum:        req.OrdersNum,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	us.ID = s.repo.CreateUserStrategy(us)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetOrCreateUserStrategyResponse{
		UserStrategyID: us.ID,
		Created:        true,
	})
}
