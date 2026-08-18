package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"trading-service/internal/persistence"
	"trading-service/internal/rpc"

	"github.com/stretchr/testify/require"
)

func TestHandleCreateUprunningOrder_Success(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	handler := NewPositionQueryHandler(repo, nil)

	reqBody := rpc.CreateUprunningOrderRequest{
		UserID:              100,
		RelationID:          200,
		RelationType:        "user_orders",
		Exchange:            "binance",
		Symbol:              "BTCUSDT",
		PosType:             2,
		ExchangeOrderID:     300,
		ExchangeOrderStatus: "NEW",
		ExchangeOrderPrice:  45000.50,
		ExchangeOrderQty:    0.001,
		Side:                0,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/rpc/v1/uprunning-order/create", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// Act
	handler.HandleCreateUprunningOrder(w, req)

	// Assert
	require.Equal(t, http.StatusOK, w.Code)

	var resp rpc.CreateUprunningOrderResponse
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	require.Greater(t, resp.UprunningOrderID, uint64(0))

	// 验证数据已持久化
	uo, err := repo.GetUprunningOrderByID(resp.UprunningOrderID)
	require.NoError(t, err)
	require.Equal(t, uint64(100), uo.UserID)
	require.Equal(t, uint64(200), uo.RelationID)
	require.Equal(t, "user_orders", uo.RelationType)
	require.Equal(t, "binance", uo.Exchange)
}

func TestHandleCreateUprunningOrder_MissingRequiredFields(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	handler := NewPositionQueryHandler(repo, nil)

	testCases := []struct {
		name   string
		body   rpc.CreateUprunningOrderRequest
		errMsg string
	}{
		{
			name: "missing user_id",
			body: rpc.CreateUprunningOrderRequest{
				RelationID:   200,
				RelationType: "user_orders",
			},
			errMsg: "user_id",
		},
		{
			name: "missing relation_id",
			body: rpc.CreateUprunningOrderRequest{
				UserID:       100,
				RelationType: "user_orders",
			},
			errMsg: "relation_id",
		},
		{
			name: "missing relation_type",
			body: rpc.CreateUprunningOrderRequest{
				UserID:     100,
				RelationID: 200,
			},
			errMsg: "relation_type",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			req := httptest.NewRequest(http.MethodPost, "/rpc/v1/uprunning-order/create", bytes.NewReader(body))
			w := httptest.NewRecorder()

			handler.HandleCreateUprunningOrder(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Contains(t, w.Body.String(), tc.errMsg)
		})
	}
}

func TestHandleCreateUprunningOrder_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	handler := NewPositionQueryHandler(repo, nil)

	req := httptest.NewRequest(http.MethodPost, "/rpc/v1/uprunning-order/create", bytes.NewReader([]byte(`invalid json`)))
	w := httptest.NewRecorder()

	// Act
	handler.HandleCreateUprunningOrder(w, req)

	// Assert
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid JSON")
}

func TestHandleCreateUprunningOrder_WrongMethod(t *testing.T) {
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	handler := NewPositionQueryHandler(repo, nil)

	req := httptest.NewRequest(http.MethodGet, "/rpc/v1/uprunning-order/create", nil)
	w := httptest.NewRecorder()

	// Act
	handler.HandleCreateUprunningOrder(w, req)

	// Assert
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHandleCreateUprunningOrder_RiskControlStrategy(t *testing.T) {
	// Arrange - 测试 risk_control_strategy 类型的订单
	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	defer gs.Shutdown()
	repo := persistence.NewStateRepository(gs)

	handler := NewPositionQueryHandler(repo, nil)

	reqBody := rpc.CreateUprunningOrderRequest{
		UserID:              100,
		RelationID:          500,
		RelationType:        "risk_control_strategy",
		RiskCtrlStratID:     500,
		UserOrderPositionID: 600,
		UserPositionID:      700,
		Exchange:            "binance",
		Symbol:              "BTCUSDT",
		PosType:             2,
		ExchangeOrderID:     300,
		ExchangeOrderStatus: "NEW",
		ExchangeOrderPrice:  45000.50,
		ExchangeOrderQty:    0.001,
		Side:                0,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/rpc/v1/uprunning-order/create", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// Act
	handler.HandleCreateUprunningOrder(w, req)

	// Assert
	require.Equal(t, http.StatusOK, w.Code)

	var resp rpc.CreateUprunningOrderResponse
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	// 验证 risk_control_strategy 字段已保存
	uo, err := repo.GetUprunningOrderByID(resp.UprunningOrderID)
	require.NoError(t, err)
	require.Equal(t, "risk_control_strategy", uo.RelationType)
	require.Equal(t, uint64(500), uo.RiskCtrlStratID)
	require.Equal(t, uint64(600), uo.UserOrderPositionID)
	require.Equal(t, uint64(700), uo.UserPositionID)
}
