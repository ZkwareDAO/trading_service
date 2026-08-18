package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk"
	"trading-service/internal/risk/config"
)

// PositionAPIHandler handles position API requests.
type PositionAPIHandler struct {
	repo      *persistence.StateRepository
	ruleStore *config.RuleStore
	resolver  exchangeResolver
}

// NewPositionAPIHandler creates a new position API handler.
func NewPositionAPIHandler(repo *persistence.StateRepository, ruleStore *config.RuleStore, resolver exchangeResolver) *PositionAPIHandler {
	return &PositionAPIHandler{
		repo:      repo,
		ruleStore: ruleStore,
		resolver:  resolver,
	}
}

// ServeHTTP routes requests to appropriate handlers.
func (h *PositionAPIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Route based on path
	if strings.HasPrefix(path, "/api/v1/user-order-positions") {
		h.handleUserOrderPositions(w, r)
	} else if strings.HasPrefix(path, "/api/v1/user-positions") {
		h.handleUserPositions(w, r)
	} else if path == "/api/v1/positions/close-all" && r.Method == http.MethodPost {
		h.handleCloseAllPositions(w, r)
	} else if path == "/api/v1/positions/close-partial" && r.Method == http.MethodPost {
		h.handleClosePartialPosition(w, r)
	} else if path == "/api/v1/exchange/positions" && r.Method == http.MethodPost {
		h.handleGetExchangePositions(w, r)
	} else {
		http.NotFound(w, r)
	}
}

// ===== User Order Positions API =====

func (h *PositionAPIHandler) handleUserOrderPositions(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path if present
	path := r.URL.Path
	if path == "/api/v1/user-order-positions" {
		h.listUserOrderPositions(w, r)
	} else {
		// Extract ID from /api/v1/user-order-positions/:id
		idStr := strings.TrimPrefix(path, "/api/v1/user-order-positions/")
		if idStr == "" || strings.Contains(idStr, "/") {
			http.NotFound(w, r)
			return
		}
		h.getUserOrderPositionByID(w, r, idStr)
	}
}

func (h *PositionAPIHandler) listUserOrderPositions(w http.ResponseWriter, r *http.Request) {
	// Log incoming request
	log.Printf("[/api/v1/user-order-positions] Received request: method=%s, remote=%s, user-agent=%s, query=%s",
		r.Method, r.RemoteAddr, r.Header.Get("User-Agent"), r.URL.RawQuery)

	query := r.URL.Query()

	// Parse query parameters with priority logic
	var filter persistence.UserOrderPositionFilter

	// Priority 1: user_strategy_id
	if userStrategyID := query.Get("user_strategy_id"); userStrategyID != "" {
		id, err := strconv.ParseUint(userStrategyID, 10, 64)
		if err != nil {
			writeAPIError(w, 1001, "Invalid user_strategy_id")
			return
		}
		filter.UserStrategyID = id
	} else if userIDStr := query.Get("user_id"); userIDStr != "" && query.Get("strategy_name") == "" {
		// Priority 1.5: user_id only (without strategy_name)
		userID, err := strconv.ParseUint(userIDStr, 10, 64)
		if err != nil {
			writeAPIError(w, 1001, "Invalid user_id")
			return
		}
		filter.UserID = userID
	}

	// Priority 2: user_id + strategy_name / strategy_name only
	ids, adaptedSymbol, ok := h.resolveStrategyNameIDs(w, query)
	if !ok {
		return
	}
	// strategy_name was provided but matched no user_strategy: return empty
	// rather than silently dropping the filter and returning all positions.
	// (Guarded so user_strategy_id, when also supplied, still takes effect.)
	if query.Get("strategy_name") != "" && query.Get("user_strategy_id") == "" && len(ids) == 0 {
		writeEmptyPositionsResponse(w)
		return
	}
	if ids != nil {
		filter.UserStrategyIDs = ids
		if adaptedSymbol != "" {
			filter.Asset = adaptedSymbol
		}
	}

	// Priority 3-5: user_name based queries
	userName := query.Get("user_name")
	exchange := query.Get("exchange")
	if userName != "" {
		userIDs, err := h.repo.FindUserIDsByName(userName, exchange)
		if err != nil {
			writeAPIError(w, 1001, fmt.Sprintf("User '%s' not found", userName))
			return
		}
		filter.UserIDs = userIDs
		if exchange != "" {
			filter.Exchange = exchange
		}
	} else if exchange != "" {
		// Priority 5: only exchange
		filter.Exchange = exchange
	}

	// Additional filters
	if asset := query.Get("asset"); asset != "" {
		filter.Asset = asset
	}

	if sideStr := query.Get("side"); sideStr != "" {
		side, err := strconv.Atoi(sideStr)
		if err != nil || (side != 0 && side != 1) {
			writeAPIError(w, 1001, "Invalid side")
			return
		}
		s := order.Side(side)
		filter.Side = &s
	}

	if deletedStr := query.Get("deleted"); deletedStr != "" {
		deleted, err := strconv.Atoi(deletedStr)
		if err != nil || (deleted != 0 && deleted != 1) {
			writeAPIError(w, 1001, "Invalid deleted")
			return
		}
		active := deleted == 0
		filter.Active = &active
	}

	if posTypeStr := query.Get("pos_type"); posTypeStr != "" {
		posType, err := strconv.Atoi(posTypeStr)
		if err != nil || (posType != 1 && posType != 2) {
			writeAPIError(w, 1001, "Invalid pos_type")
			return
		}
		pt := order.PosType(posType)
		filter.PosType = &pt
	}

	// Time range filters
	if !h.parseRFC3339Param(w, query, "created_from", &filter.CreatedAtFrom) {
		return
	}
	if !h.parseRFC3339Param(w, query, "created_to", &filter.CreatedAtTo) {
		return
	}
	if !h.parseRFC3339Param(w, query, "close_from", &filter.CloseTimeFrom) {
		return
	}
	if !h.parseRFC3339Param(w, query, "close_to", &filter.CloseTimeTo) {
		return
	}

	// Query data
	positions := h.repo.ListUserOrderPositionsByFilter(filter)

	// Sort by id descending (newest position first)
	sort.SliceStable(positions, func(i, j int) bool {
		return positions[i].ID > positions[j].ID
	})

	// Pagination
	page := 1
	pageSize := 0 // 0 means no pagination

	if pageStr := query.Get("page"); pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil || p < 1 {
			writeAPIError(w, 1001, "Invalid page")
			return
		}
		page = p
	}

	if pageSizeStr := query.Get("page_size"); pageSizeStr != "" {
		ps, err := strconv.Atoi(pageSizeStr)
		if err != nil || ps < 1 || ps > 100 {
			writeAPIError(w, 1001, "Invalid page_size (must be 1-100)")
			return
		}
		pageSize = ps
	}

	total := len(positions)

	// Apply pagination if page_size is set
	if pageSize > 0 {
		start := (page - 1) * pageSize
		end := start + pageSize
		if start >= total {
			positions = []*order.UserOrderPosition{}
		} else {
			if end > total {
				end = total
			}
			positions = positions[start:end]
		}
	}

	// Enrich with user_name and strategy_name
	userNames, strategyNames := h.buildNameLookupMaps()
	enrichedList := make([]UserOrderPositionResponse, len(positions))
	for i, pos := range positions {
		enrichedList[i] = UserOrderPositionResponse{
			UserOrderPosition: *pos,
			UserName:          userNames[pos.UserID],
			StrategyName:      strategyNames[pos.UserStrategyID],
		}
	}

	// Response
	writeAPISuccess(w, PaginatedListResponse{
		List:     enrichedList,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (h *PositionAPIHandler) getUserOrderPositionByID(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeAPIError(w, 1001, "Invalid id")
		return
	}

	pos, err := h.repo.GetUserOrderPositionByID(id)
	if err != nil {
		writeAPIError(w, 5001, fmt.Sprintf("user_order_position %d not found", id))
		return
	}

	writeAPISuccess(w, h.enrichOrderPosition(pos))
}

// ===== User Positions API (Aggregated) =====

func (h *PositionAPIHandler) handleUserPositions(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/api/v1/user-positions" {
		h.listUserPositions(w, r)
	} else {
		idStr := strings.TrimPrefix(path, "/api/v1/user-positions/")
		if idStr == "" || strings.Contains(idStr, "/") {
			http.NotFound(w, r)
			return
		}
		h.getUserPositionByID(w, r, idStr)
	}
}

func (h *PositionAPIHandler) listUserPositions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	var filter persistence.UserPositionFilter

	// Priority 1: user_strategy_id
	if userStrategyID := query.Get("user_strategy_id"); userStrategyID != "" {
		id, err := strconv.ParseUint(userStrategyID, 10, 64)
		if err != nil {
			writeAPIError(w, 1001, "Invalid user_strategy_id")
			return
		}
		filter.UserStrategyID = id
	} else if userIDStr := query.Get("user_id"); userIDStr != "" && query.Get("strategy_name") == "" {
		// Priority 1.5: user_id only (without strategy_name)
		userID, err := strconv.ParseUint(userIDStr, 10, 64)
		if err != nil {
			writeAPIError(w, 1001, "Invalid user_id")
			return
		}
		filter.UserID = userID
	}

	// Priority 2: user_id + strategy_name / strategy_name only
	ids, _, ok := h.resolveStrategyNameIDs(w, query)
	if !ok {
		return
	}
	// strategy_name was provided but matched no user_strategy: return empty
	// rather than silently dropping the filter and returning all positions.
	// (Guarded so user_strategy_id, when also supplied, still takes effect.)
	if query.Get("strategy_name") != "" && query.Get("user_strategy_id") == "" && len(ids) == 0 {
		writeEmptyUserPositionsResponse(w)
		return
	}
	if ids != nil {
		filter.UserStrategyIDs = ids
	}

	// Priority 3-5: user_name based queries
	userName := query.Get("user_name")
	exchange := query.Get("exchange")
	if userName != "" {
		userIDs, err := h.repo.FindUserIDsByName(userName, exchange)
		if err != nil {
			writeAPIError(w, 1001, fmt.Sprintf("User '%s' not found", userName))
			return
		}
		filter.UserIDs = userIDs
		if exchange != "" {
			filter.Exchange = exchange
		}
	} else if exchange != "" {
		filter.Exchange = exchange
	}

	// Additional filters
	if deletedStr := query.Get("deleted"); deletedStr != "" {
		deleted, err := strconv.Atoi(deletedStr)
		if err != nil || (deleted != 0 && deleted != 1) {
			writeAPIError(w, 1001, "Invalid deleted")
			return
		}
		filter.Deleted = &deleted
	}

	if posTypeStr := query.Get("pos_type"); posTypeStr != "" {
		posType, err := strconv.Atoi(posTypeStr)
		if err != nil || (posType != 1 && posType != 2) {
			writeAPIError(w, 1001, "Invalid pos_type")
			return
		}
		pt := order.PosType(posType)
		filter.PosType = &pt
	}

	// Time range filters
	if !h.parseRFC3339Param(w, query, "created_from", &filter.CreatedAtFrom) {
		return
	}
	if !h.parseRFC3339Param(w, query, "created_to", &filter.CreatedAtTo) {
		return
	}
	if !h.parseRFC3339Param(w, query, "close_from", &filter.CloseTimeFrom) {
		return
	}
	if !h.parseRFC3339Param(w, query, "close_to", &filter.CloseTimeTo) {
		return
	}

	// Query data
	positions := h.repo.ListUserPositionsByFilter(filter)

	// Sort by id descending (newest position first)
	sort.SliceStable(positions, func(i, j int) bool {
		return positions[i].ID > positions[j].ID
	})

	// Pagination (default enabled for user_positions)
	page := 1
	pageSize := 10

	if pageStr := query.Get("page"); pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil || p < 1 {
			writeAPIError(w, 1001, "Invalid page")
			return
		}
		page = p
	}

	if pageSizeStr := query.Get("page_size"); pageSizeStr != "" {
		ps, err := strconv.Atoi(pageSizeStr)
		if err != nil || ps < 1 || ps > 100 {
			writeAPIError(w, 1001, "Invalid page_size (must be 1-100)")
			return
		}
		pageSize = ps
	}

	total := len(positions)

	// Apply pagination
	start := (page - 1) * pageSize
	end := start + pageSize
	if start >= total {
		positions = []*order.UserPosition{}
	} else {
		if end > total {
			end = total
		}
		positions = positions[start:end]
	}

	// Enrich with user_name and strategy_name
	userNames, strategyNames := h.buildNameLookupMaps()
	enrichedList := make([]UserPositionResponse, len(positions))
	for i, pos := range positions {
		enrichedList[i] = UserPositionResponse{
			UserPosition: *pos,
			UserName:     userNames[pos.UserID],
			StrategyName: strategyNames[pos.UserStrategyID],
		}
	}

	writeAPISuccess(w, PaginatedListResponse{
		List:     enrichedList,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (h *PositionAPIHandler) getUserPositionByID(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeAPIError(w, 1001, "Invalid id")
		return
	}

	pos, err := h.repo.GetUserPositionByID(id)
	if err != nil {
		writeAPIError(w, 5001, fmt.Sprintf("user_position %d not found", id))
		return
	}

	writeAPISuccess(w, h.enrichPosition(pos))
}

// ===== Helper Functions =====

// buildNameLookupMaps 批量构建 userID→userName 和 strategyID→strategyName 查找表
func (h *PositionAPIHandler) buildNameLookupMaps() (userNames map[uint64]string, strategyNames map[uint64]string) {
	userNames = make(map[uint64]string)
	strategyNames = make(map[uint64]string)

	for _, u := range h.repo.ListUsers() {
		userNames[u.ID] = u.Name
	}
	for _, us := range h.repo.ListUserStrategies() {
		strategyNames[us.ID] = us.Name
	}
	return
}

// UserOrderPositionResponse enriches UserOrderPosition with user_name and strategy_name for API responses.
type UserOrderPositionResponse struct {
	order.UserOrderPosition
	UserName     string `json:"user_name"`
	StrategyName string `json:"strategy_name"`
}

// UserPositionResponse enriches UserPosition with user_name and strategy_name for API responses.
type UserPositionResponse struct {
	order.UserPosition
	UserName     string `json:"user_name"`
	StrategyName string `json:"strategy_name"`
}

// PaginatedListResponse is a generic paginated list response envelope.
type PaginatedListResponse struct {
	List     interface{} `json:"list"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

// lookupNames resolves user_name and strategy_name by ID.
func (h *PositionAPIHandler) lookupNames(userID, strategyID uint64) (userName, strategyName string) {
	if u, err := h.repo.GetUserByID(userID); err == nil {
		userName = u.Name
	}
	if us, err := h.repo.GetUserStrategyByID(strategyID); err == nil {
		strategyName = us.Name
	}
	return
}

// enrichOrderPosition enriches a single UserOrderPosition with user_name and strategy_name.
func (h *PositionAPIHandler) enrichOrderPosition(pos *order.UserOrderPosition) UserOrderPositionResponse {
	userName, strategyName := h.lookupNames(pos.UserID, pos.UserStrategyID)
	return UserOrderPositionResponse{
		UserOrderPosition: *pos,
		UserName:          userName,
		StrategyName:      strategyName,
	}
}

// enrichPosition enriches a single UserPosition with user_name and strategy_name.
func (h *PositionAPIHandler) enrichPosition(pos *order.UserPosition) UserPositionResponse {
	userName, strategyName := h.lookupNames(pos.UserID, pos.UserStrategyID)
	return UserPositionResponse{
		UserPosition: *pos,
		UserName:     userName,
		StrategyName: strategyName,
	}
}

func writeAPISuccess(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    0,
		"message": "success",
		"data":    data,
	})
}

// ===== Agent Supplementary Close Interface =====

type CloseAllPositionsRequest struct {
	UserName string `json:"user_name"`
	Exchange string `json:"exchange"`
	PosType  int    `json:"pos_type"`
	Asset    string `json:"asset"`
}

type ClosePartialPositionRequest struct {
	UserName    string  `json:"user_name"`
	Exchange    string  `json:"exchange"`
	PosType     int     `json:"pos_type"`
	Asset       string  `json:"asset"`
	Price       float64 `json:"price"`
	QuantityPct float64 `json:"quantity_pct"`
	TriggerType string  `json:"trigger_type"`
}

type GetExchangePositionsRequest struct {
	UserName string `json:"user_name"`
	Exchange string `json:"exchange"`
}

type GetExchangePositionsResponse struct {
	Positions []exchange.PositionInfo `json:"positions"`
	Exchange  string                  `json:"exchange"`
	User      string                  `json:"user"`
}

func (h *PositionAPIHandler) handleCloseAllPositions(w http.ResponseWriter, r *http.Request) {
	var req CloseAllPositionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, 40001, "Invalid request body")
		return
	}

	if req.UserName == "" || req.Exchange == "" || req.Asset == "" {
		writeAPIError(w, 40001, "user_name, exchange, and asset are required")
		return
	}

	if !isValidExchange(req.Exchange) {
		writeAPIError(w, 40001, fmt.Sprintf("Invalid exchange: %s", req.Exchange))
		return
	}
	// Persisted users/positions store the exchange lowercase; normalize so the
	// user lookup and the position filter below are case-insensitive.
	req.Exchange = normalizeExchange(req.Exchange)

	if req.PosType != 0 && req.PosType != 1 && req.PosType != 2 {
		writeAPIError(w, 40004, "pos_type must be 0 (any), 1 (spot), or 2 (futures)")
		return
	}

	userID, ok := h.findUserByName(w, req.UserName, req.Exchange)
	if !ok {
		return
	}

	symbol := buildSymbol(req.Exchange, req.Asset)
	filter := persistence.UserOrderPositionFilter{
		UserIDs:  []uint64{userID},
		Exchange: req.Exchange,
		Asset:    symbol,
		Active:   boolPtr(true),
	}

	if req.PosType > 0 {
		pt := order.PosType(req.PosType)
		filter.PosType = &pt
	}

	positions := h.repo.ListUserOrderPositionsByFilter(filter)
	if len(positions) == 0 {
		writeAPIError(w, 40003, "No active positions")
		return
	}

	userStrategyIDs := make(map[uint64]bool)
	for _, pos := range positions {
		userStrategyIDs[pos.UserStrategyID] = true
	}

	ruleIDs := []int{}
	failedUserStrategies := []uint64{}
	now := time.Now()

	for usID := range userStrategyIDs {
		rule := risk.Rule{
			UserStrategyID: usID,
			ConditionName:  "always",
			Operator:       "==",
			Value:          "true",
			Sort:           1,
			Status:         config.RuleStatusActive,
			Action:         "reduce",
			Params: map[string]interface{}{
				"order_type":   OrderTypeMarket,
				"quantity_pct": 1.0,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := h.ruleStore.CreateRule(&rule); err != nil {
			log.Printf("handleCloseAllPositions: failed for user_strategy %d: %v", usID, err)
			failedUserStrategies = append(failedUserStrategies, usID)
			continue
		}

		ruleIDs = append(ruleIDs, rule.ID)
		log.Printf("handleCloseAllPositions: created rule ID=%d for user_strategy=%d", rule.ID, usID)
	}

	if len(failedUserStrategies) > 0 {
		writeAPIError(w, 5001, fmt.Sprintf("Failed to create close rules for %d strategies: %v",
			len(failedUserStrategies), failedUserStrategies))
		return
	}

	writeAPISuccess(w, map[string]interface{}{
		"rule_ids":       ruleIDs,
		"closed_count":   len(positions),
		"strategy_count": len(userStrategyIDs),
	})
}

func (h *PositionAPIHandler) handleClosePartialPosition(w http.ResponseWriter, r *http.Request) {
	var req ClosePartialPositionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, 40001, "Invalid request body")
		return
	}

	if req.UserName == "" || req.Exchange == "" || req.Asset == "" || req.Price <= 0 {
		writeAPIError(w, 40001, "user_name, exchange, asset, and price are required")
		return
	}

	if req.QuantityPct <= 0 || req.QuantityPct > 1.0 {
		writeAPIError(w, 40004, "quantity_pct must be between 0 and 1.0")
		return
	}

	if req.TriggerType != "take_profit" && req.TriggerType != "stop_loss" {
		writeAPIError(w, 40001, "trigger_type must be 'take_profit' or 'stop_loss'")
		return
	}

	if !isValidExchange(req.Exchange) {
		writeAPIError(w, 40001, fmt.Sprintf("Invalid exchange: %s", req.Exchange))
		return
	}
	// Match the persisted lowercase exchange (see handleCloseAllPositions).
	req.Exchange = normalizeExchange(req.Exchange)

	userID, ok := h.findUserByName(w, req.UserName, req.Exchange)
	if !ok {
		return
	}

	symbol := buildSymbol(req.Exchange, req.Asset)
	filter := persistence.UserOrderPositionFilter{
		UserIDs:  []uint64{userID},
		Exchange: req.Exchange,
		Asset:    symbol,
		Active:   boolPtr(true),
	}

	if req.PosType > 0 {
		pt := order.PosType(req.PosType)
		filter.PosType = &pt
	}

	positions := h.repo.ListUserOrderPositionsByFilter(filter)
	if len(positions) == 0 {
		writeAPIError(w, 40003, "Position not found")
		return
	}

	position := positions[0]
	operator := determineOperator(int(position.Side), req.TriggerType)
	conditionName := determineConditionName(req.Asset)

	now := time.Now()
	rule := risk.Rule{
		UserStrategyID: position.UserStrategyID,
		ConditionName:  conditionName,
		Operator:       operator,
		Value:          req.Price,
		Sort:           1,
		Status:         config.RuleStatusActive,
		Action:         "reduce",
		Params: map[string]interface{}{
			"order_type":   OrderTypeMarket,
			"quantity_pct": req.QuantityPct,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.ruleStore.CreateRule(&rule); err != nil {
		writeAPIError(w, 5001, fmt.Sprintf("Failed to create close rule: %v", err))
		return
	}

	log.Printf("handleClosePartialPosition: created rule ID=%d", rule.ID)
	writeAPISuccess(w, map[string]interface{}{
		"rule_id":      rule.ID,
		"condition":    fmt.Sprintf("%s %s %v", conditionName, operator, req.Price),
		"quantity_pct": req.QuantityPct,
	})
}

func (h *PositionAPIHandler) handleGetExchangePositions(w http.ResponseWriter, r *http.Request) {
	var req GetExchangePositionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, 40001, "Invalid request body")
		return
	}

	if req.UserName == "" || req.Exchange == "" {
		writeAPIError(w, 40001, "user_name and exchange are required")
		return
	}

	if !isValidExchange(req.Exchange) {
		writeAPIError(w, 40001, fmt.Sprintf("Invalid exchange: %s", req.Exchange))
		return
	}
	// Normalize for the user lookup and ResolveExchange, both of which compare
	// the exchange name case-sensitively. req.Exchange itself is left alone so
	// the response below still echoes back exactly what the caller sent.
	exchangeName := normalizeExchange(req.Exchange)

	userID, ok := h.findUserByName(w, req.UserName, exchangeName)
	if !ok {
		return
	}

	if h.resolver == nil {
		writeAPIError(w, 5001, "Exchange resolver not configured")
		return
	}

	ex, err := h.resolver.ResolveExchange(userID, exchangeName)
	if err != nil {
		writeAPIError(w, 40002, fmt.Sprintf("Failed to resolve exchange: %v", err))
		return
	}

	positions, err := ex.GetPositions()
	if err != nil {
		writeAPIError(w, 5001, fmt.Sprintf("Failed to query positions: %v", err))
		return
	}

	writeAPISuccess(w, GetExchangePositionsResponse{
		Positions: positions,
		Exchange:  req.Exchange,
		User:      req.UserName,
	})
}

func (h *PositionAPIHandler) findUserByName(w http.ResponseWriter, userName, exchange string) (uint64, bool) {
	userIDs, err := h.repo.FindUserIDsByName(userName, exchange)
	if err != nil || len(userIDs) == 0 {
		writeAPIError(w, 40002, "User not found")
		return 0, false
	}
	return userIDs[0], true
}

// buildSymbol resolves the request `asset` into the stored position symbol.
// Callers may pass either a base asset ("XRP") or a full pair ("XRPUSDC"), so
// an existing quote suffix is swapped rather than appended a second time.
//
// Unlike adaptSymbolForExchange, a suffix is only stripped when something
// remains after it: for the base asset "USDC" the whole string is the suffix,
// and stripping it would yield "USDT" instead of the real pair "USDCUSDT".
// Symbols with no quote suffix at all (e.g. deribit options) are returned
// unchanged. Input carrying two stacked quote suffixes ("XRPUSDTUSDC") is not a
// valid asset and is passed through as-is rather than guessed at.
func buildSymbol(exchange, asset string) string {
	quote, ok := quoteForExchange(exchange)
	if !ok {
		return asset
	}
	if strings.HasSuffix(asset, quote) {
		return asset
	}
	for _, other := range []string{quoteUSDT, quoteUSDC} {
		if base := strings.TrimSuffix(asset, other); base != asset && base != "" {
			return base + quote
		}
	}
	return asset + quote
}

// quoteForExchange returns the quote currency an exchange denominates its perp
// symbols in. ok is false for exchanges with no single quote (e.g. deribit,
// whose option symbols carry no quote suffix).
func quoteForExchange(exchange string) (quote string, ok bool) {
	switch normalizeExchange(exchange) {
	case "hyperliquid":
		return quoteUSDC, true
	case "binance":
		return quoteUSDT, true
	default:
		return "", false
	}
}

func determineOperator(side int, triggerType string) string {
	if side == 0 { // Long
		if triggerType == "take_profit" {
			return ">="
		}
		return "<="
	}
	// Short
	if triggerType == "take_profit" {
		return "<="
	}
	return ">="
}

func determineConditionName(asset string) string {
	base := asset
	for _, suffix := range []string{quoteUSDT, quoteUSDC} {
		base = strings.TrimSuffix(base, suffix)
	}
	return "price_" + strings.ToLower(base)
}

// adaptSymbolForExchange adapts symbol suffix based on exchange
func adaptSymbolForExchange(symbol, exchange string) string {
	base := symbol
	for _, suffix := range []string{quoteUSDT, quoteUSDC} {
		base = strings.TrimSuffix(base, suffix)
	}

	switch strings.ToLower(exchange) {
	case "hyperliquid":
		return base + quoteUSDC
	case "binance":
		return base + quoteUSDT
	default:
		return symbol
	}
}

// adaptStrategyNameForExchange adapts strategy name suffix based on exchange
func adaptStrategyNameForExchange(strategyName, exchange string) string {
	parts := strings.Split(strategyName, "_")
	if len(parts) < 2 {
		return strategyName
	}

	lastPart := parts[len(parts)-1]
	if !strings.HasSuffix(lastPart, quoteUSDT) && !strings.HasSuffix(lastPart, quoteUSDC) {
		return strategyName
	}

	parts[len(parts)-1] = adaptSymbolForExchange(lastPart, exchange)
	return strings.Join(parts, "_")
}

// writeAPIError writes an error envelope. Business codes are grouped by their
// leading digit: 4xxx/40xxx are client errors (400), 5xxx are server errors
// (500). Comparing the raw code against 5000 would misclassify 40001-40004 as
// server errors, so normalize to the leading digit instead.
func writeAPIError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatusForCode(code))
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    code,
		"message": message,
		"data":    nil,
	})
}

// httpStatusForCode maps a business error code to an HTTP status by its leading
// digit, so that codes of differing widths compare alike (40003 and 4003 are
// both client errors; 5001 is a server error).
func httpStatusForCode(code int) int {
	if strings.HasPrefix(strconv.Itoa(code), "5") {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

// RegisterPositionAPIHandlers registers position API handlers on the given mux.
func RegisterPositionAPIHandlers(mux *http.ServeMux, repo *persistence.StateRepository, ruleStore *config.RuleStore, resolver exchangeResolver) {
	handler := NewPositionAPIHandler(repo, ruleStore, resolver)
	mux.Handle("/api/v1/user-order-positions", handler)
	mux.Handle("/api/v1/user-order-positions/", handler)
	mux.Handle("/api/v1/user-positions", handler)
	mux.Handle("/api/v1/user-positions/", handler)
	mux.HandleFunc("/api/v1/positions/close-all", handler.handleCloseAllPositions)
	mux.HandleFunc("/api/v1/positions/close-partial", handler.handleClosePartialPosition)
	mux.HandleFunc("/api/v1/exchange/positions", handler.handleGetExchangePositions)
	log.Println("Position API handlers registered")
}

// ===== Constants and Helper Functions =====

const (
	quoteUSDT = "USDT"
	quoteUSDC = "USDC"

	OrderTypeLimit  = 0
	OrderTypeMarket = 1
)

// normalizeExchange returns the canonical (lowercase) exchange name used in
// persisted users.csv / user_order_positions.csv rows.
func normalizeExchange(exchange string) string {
	return strings.ToLower(exchange)
}

// isValidExchange validates exchange name
func isValidExchange(exchange string) bool {
	switch normalizeExchange(exchange) {
	case "binance", "hyperliquid", "deribit":
		return true
	default:
		return false
	}
}

// resolveStrategyNameIDs returns user_strategy IDs for the given strategy_name.
// - user_id + strategy_name: adapts suffix by exchange, finds IDs scoped to user
// - strategy_name only: finds IDs across all users (no suffix adaptation)
// Returns (ids, adaptedSymbol, ok). ok=false means an error was written to w.
func (h *PositionAPIHandler) resolveStrategyNameIDs(w http.ResponseWriter, query url.Values) (ids []uint64, adaptedSymbol string, ok bool) {
	strategyName := query.Get("strategy_name")
	if strategyName == "" {
		return nil, "", true
	}

	userIDStr := query.Get("user_id")
	if userIDStr != "" {
		userID, err := strconv.ParseUint(userIDStr, 10, 64)
		if err != nil {
			writeAPIError(w, 1001, "Invalid user_id")
			return nil, "", false
		}
		user, err := h.repo.GetUserByID(userID)
		if err != nil {
			writeAPIError(w, 1001, fmt.Sprintf("User %d not found", userID))
			return nil, "", false
		}
		adaptedName := adaptStrategyNameForExchange(strategyName, user.Exchange)
		ids = h.repo.FindUserStrategyIDsByUserAndName(userID, adaptedName)

		if symbol := query.Get("symbol"); symbol != "" {
			adaptedSymbol = adaptSymbolForExchange(symbol, user.Exchange)
		}
	} else {
		ids = h.repo.FindUserStrategyIDsByName(strategyName)
	}
	return ids, adaptedSymbol, true
}

// parseRFC3339Param parses an RFC3339 query parameter into target.
// If absent, target is left unchanged. Returns false if an error was written to w.
func (h *PositionAPIHandler) parseRFC3339Param(w http.ResponseWriter, query url.Values, key string, target **time.Time) bool {
	val := query.Get(key)
	if val == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		writeAPIError(w, 1001, fmt.Sprintf("Invalid %s format, use RFC3339", key))
		return false
	}
	*target = &t
	return true
}

// boolPtr returns pointer to bool
func boolPtr(v bool) *bool {
	return &v
}

// writeEmptyPositionsResponse returns empty positions response
func writeEmptyPositionsResponse(w http.ResponseWriter) {
	writeAPISuccess(w, PaginatedListResponse{
		List:     []*order.UserOrderPosition{},
		Total:    0,
		Page:     1,
		PageSize: 0,
	})
}

// writeEmptyUserPositionsResponse returns empty user positions response
func writeEmptyUserPositionsResponse(w http.ResponseWriter) {
	writeAPISuccess(w, PaginatedListResponse{
		List:     []*order.UserPosition{},
		Total:    0,
		Page:     1,
		PageSize: 10,
	})
}
