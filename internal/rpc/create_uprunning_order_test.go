package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCreateUprunningOrder_Success(t *testing.T) {
	// Arrange - 创建 mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/rpc/v1/uprunning-order/create", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)

		// 返回成功响应
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"uprunning_order_id":123}`))
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	ctx := context.Background()

	req := CreateUprunningOrderRequest{
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

	// Act
	resp, err := client.CreateUprunningOrder(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, uint64(123), resp.UprunningOrderID)
}

func TestCreateUprunningOrder_ServerError(t *testing.T) {
	// Arrange - 创建返回错误的 server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	ctx := context.Background()

	req := CreateUprunningOrderRequest{
		UserID:       100,
		RelationID:   200,
		RelationType: "user_orders",
	}

	// Act
	resp, err := client.CreateUprunningOrder(ctx, req)

	// Assert
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "unexpected status code: 500")
}

func TestCreateUprunningOrder_NetworkError(t *testing.T) {
	// Arrange - 使用无效的 URL
	client := NewOrderServiceClient("http://invalid-host:9999")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := CreateUprunningOrderRequest{
		UserID:       100,
		RelationID:   200,
		RelationType: "user_orders",
	}

	// Act
	resp, err := client.CreateUprunningOrder(ctx, req)

	// Assert
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestCreateUprunningOrder_InvalidJSONResponse(t *testing.T) {
	// Arrange - 返回无效 JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`invalid json`))
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	ctx := context.Background()

	req := CreateUprunningOrderRequest{
		UserID:       100,
		RelationID:   200,
		RelationType: "user_orders",
	}

	// Act
	resp, err := client.CreateUprunningOrder(ctx, req)

	// Assert
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "decode response")
}