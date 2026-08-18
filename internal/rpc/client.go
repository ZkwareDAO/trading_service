package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OrderServiceClient is the RPC client for calling user_order_service.
type OrderServiceClient struct {
	baseURL string
	client  *http.Client
}

type QueryUserOrderPositionsRequest struct {
	UserStrategyID   uint64 `json:"user_strategy_id"`
	Side             *int   `json:"side,omitempty"`
	Active           *bool  `json:"active,omitempty"`
	Asset            string `json:"asset,omitempty"`
	PosType          *int   `json:"pos_type,omitempty"`
	IncludePositions bool   `json:"include_positions,omitempty"`
}

type QueryUserOrderPositionsResponse struct {
	UserStrategyID uint64 `json:"user_strategy_id"`
	Side           *int   `json:"side,omitempty"`
	Active         *bool  `json:"active,omitempty"`
	Asset          string `json:"asset,omitempty"`
	PosType        *int   `json:"pos_type,omitempty"`
	Count          int    `json:"count"`
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

// CreateUprunningOrderRequest contains all fields needed to create an uprunning_order.
type CreateUprunningOrderRequest struct {
	UserID              uint64  `json:"user_id"`
	RelationID          uint64  `json:"relation_id"`
	RelationType        string  `json:"relation_type"`
	RiskCtrlStratID     uint64  `json:"risk_control_strategy_id,omitempty"`
	UserOrderPositionID uint64  `json:"user_order_position_id,omitempty"`
	UserPositionID      uint64  `json:"user_position_id,omitempty"`
	Exchange            string  `json:"exchange"`
	Symbol              string  `json:"symbol"`
	PosType             int     `json:"pos_type"`
	ExchangeOrderID     uint64  `json:"exchange_order_id"`
	ExchangeOrderStatus string  `json:"exchange_order_status"`
	ExchangeOrderPrice  float64 `json:"exchange_order_price"`
	ExchangeOrderQty    float64 `json:"exchange_order_quantity"`
	Side                int     `json:"side"`
}

// CreateUprunningOrderResponse contains the created uprunning_order ID.
type CreateUprunningOrderResponse struct {
	UprunningOrderID uint64 `json:"uprunning_order_id"`
}

// CreateRuleRequest contains fields needed to create a risk rule.
type CreateRuleRequest struct {
	UserStrategyID uint64                 `json:"user_strategy_id"`
	ConditionName  string                 `json:"condition_name"`
	Operator       string                 `json:"operator"`
	Value          interface{}            `json:"value"`
	Sort           int                    `json:"sort"`
	Action         string                 `json:"action"`
	Params         map[string]interface{} `json:"params"`
}

// CreateRuleResponse contains the created rule ID.
type CreateRuleResponse struct {
	Success bool `json:"success"`
	RuleID  int  `json:"rule_id"`
}

// GetOrCreateStrategyRequest - PMS查询或创建策略
// CSV字段: id,name,strategy_type,model_name,description,params,created_at,updated_at
type GetOrCreateStrategyRequest struct {
	Name         string `json:"name"`                   // 必填
	StrategyType string `json:"strategy_type"`          // 必填
	ModelName    string `json:"model_name,omitempty"`   // 可选
	Description  string `json:"description,omitempty"` // 可选
	Params       string `json:"params,omitempty"`       // JSON字符串，可选
}

// GetOrCreateStrategyResponse contains the strategy ID and creation status.
type GetOrCreateStrategyResponse struct {
	StrategyID   uint64 `json:"strategy_id"`
	Name         string `json:"name"`
	StrategyType string `json:"strategy_type"`
	Created      bool   `json:"created"` // true=新创建, false=已存在
}

// GetOrCreateStrategyAssetRequest - PMS查询或创建策略资产
// CSV字段: id,name,asset,strategy_id,pos_type,sort,created_at,updated_at
type GetOrCreateStrategyAssetRequest struct {
	Name       string `json:"name"`        // 必填
	Asset      string `json:"asset"`       // 必填
	StrategyID uint64 `json:"strategy_id"` // 必填
	PosType    int    `json:"pos_type"`    // 必填
	Sort       int    `json:"sort"`        // 必填
}

// GetOrCreateStrategyAssetResponse contains the strategy asset ID and creation status.
type GetOrCreateStrategyAssetResponse struct {
	StrategyAssetID uint64 `json:"strategy_asset_id"`
	Created         bool   `json:"created"`
}

// GetOrCreateUserStrategyRequest - PMS查询或创建用户策略
// CSV字段: id,user_id,name,exchange,valid_before,cash,parts,status,strategy_id,risk_strategy_type,orders_num,created_at,updated_at
// 注意：user_strategies.csv 没有 pos_type 字段！
type GetOrCreateUserStrategyRequest struct {
	UserID           uint64  `json:"user_id"`             // 必填
	Name             string  `json:"name"`                // 必填
	StrategyID       uint64  `json:"strategy_id"`         // 必填
	Exchange         string  `json:"exchange"`            // 必填
	ValidBefore      string  `json:"valid_before"`        // 必填，RFC3339格式
	Cash             float64 `json:"cash"`                // 必填
	Parts            int     `json:"parts"`               // 必填
	Status           int     `json:"status"`              // 必填
	RiskStrategyType string  `json:"risk_strategy_type"` // 必填
	OrdersNum        int     `json:"orders_num"`          // 可选，默认0
}

// GetOrCreateUserStrategyResponse contains the user strategy ID and creation status.
type GetOrCreateUserStrategyResponse struct {
	UserStrategyID uint64 `json:"user_strategy_id"`
	Created        bool   `json:"created"`
}

// NewOrderServiceClient creates a new RPC client.
func NewOrderServiceClient(baseURL string) *OrderServiceClient {
	return &OrderServiceClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// UpdateUserOrderStatus sends a request to update a user order's status.
func (c *OrderServiceClient) UpdateUserOrderStatus(ctx context.Context, orderID uint64, status int) error {
	reqBody := UpdateUserOrderStatusRequest{
		UserOrderID: orderID,
		Status:      status,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/rpc/v1/order/status/update"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func (c *OrderServiceClient) QueryUserOrderPositions(ctx context.Context, reqBody QueryUserOrderPositionsRequest) (*QueryUserOrderPositionsResponse, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/rpc/v1/user-order-positions/query"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result QueryUserOrderPositionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// QueryOrderPositionMetadata returns metadata needed by position_monitor_service to create a position.
func (c *OrderServiceClient) QueryOrderPositionMetadata(ctx context.Context, userOrderID uint64) (*QueryOrderPositionMetadataResponse, error) {
	reqBody := QueryOrderPositionMetadataRequest{UserOrderID: userOrderID}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/rpc/v1/order/position-metadata"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result QueryOrderPositionMetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// UpdateUserOrderStatusFILLED is a helper that sets status=2.
func (c *OrderServiceClient) UpdateUserOrderStatusFILLED(ctx context.Context, orderID uint64) error {
	return c.UpdateUserOrderStatus(ctx, orderID, 2)
}

// UpdateUserOrderStatusFailed is a helper that sets status=3.
func (c *OrderServiceClient) UpdateUserOrderStatusFailed(ctx context.Context, orderID uint64) error {
	return c.UpdateUserOrderStatus(ctx, orderID, 3)
}

// GetMarketPrice queries current market price from position_monitor_service.
func (c *OrderServiceClient) GetMarketPrice(ctx context.Context, reqBody GetMarketPriceRequest) (*GetMarketPriceResponse, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/rpc/v1/market-price/get"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result GetMarketPriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// CreateUprunningOrder creates an uprunning_order via RPC to position_monitor_service.
func (c *OrderServiceClient) CreateUprunningOrder(ctx context.Context, reqBody CreateUprunningOrderRequest) (*CreateUprunningOrderResponse, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/rpc/v1/uprunning-order/create"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result CreateUprunningOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// CreateRule creates a risk rule via RPC to position_monitor_service.
func (c *OrderServiceClient) CreateRule(ctx context.Context, reqBody CreateRuleRequest) (*CreateRuleResponse, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/rpc/v1/rules/create"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result CreateRuleResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// InvalidateRulesForStrategy calls PMS to invalidate all active rules for a strategy.
// Called by UOS when opening a new position.
func (c *OrderServiceClient) InvalidateRulesForStrategy(ctx context.Context, strategyID uint64) error {
	reqBody := struct {
		UserStrategyID uint64 `json:"user_strategy_id"`
	}{UserStrategyID: strategyID}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/rpc/v1/rules/invalidate-for-strategy"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// GetOrCreateStrategy queries or creates a strategy via RPC.
func (c *OrderServiceClient) GetOrCreateStrategy(ctx context.Context, req GetOrCreateStrategyRequest) (*GetOrCreateStrategyResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/rpc/v1/strategy/get-or-create"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result GetOrCreateStrategyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// GetOrCreateStrategyAsset queries or creates a strategy asset via RPC.
func (c *OrderServiceClient) GetOrCreateStrategyAsset(ctx context.Context, req GetOrCreateStrategyAssetRequest) (*GetOrCreateStrategyAssetResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/rpc/v1/strategy-asset/get-or-create"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result GetOrCreateStrategyAssetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// GetOrCreateUserStrategy queries or creates a user strategy via RPC.
func (c *OrderServiceClient) GetOrCreateUserStrategy(ctx context.Context, req GetOrCreateUserStrategyRequest) (*GetOrCreateUserStrategyResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/rpc/v1/user-strategy/get-or-create"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result GetOrCreateUserStrategyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
