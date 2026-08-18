package binance

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/order"

	"github.com/adshao/go-binance/v2/futures"
)

// BinanceFutures implements exchange.Exchange for Binance Futures.
// Note: All API calls use context.Background(). For production, consider
// wrapping the futures.Client with a custom HTTP client that has timeouts.
type BinanceFutures struct {
	apiKey       string
	apiSecret    string
	client       *futures.Client
	filterSource exchange.FilterSource // Added for precision validation
}

// NewBinanceFutures creates a Binance Futures exchange adapter.
func NewBinanceFutures(apiKey, apiSecret string, testnet bool) *BinanceFutures {
	client := futures.NewClient(apiKey, apiSecret)
	if testnet {
		client.BaseURL = "https://testnet.binancefuture.com"
	}
	return &BinanceFutures{
		apiKey:    apiKey,
		apiSecret: apiSecret,
		client:    client,
	}
}

// SetFilterSource implements exchange.PrecisionValidator interface.
// This allows BinanceFutures to validate and format order parameters with proper precision.
func (b *BinanceFutures) SetFilterSource(source exchange.FilterSource) {
	b.filterSource = source
}

func (b *BinanceFutures) Name() string { return "binance" }

func (b *BinanceFutures) CreateOrder(req exchange.CreateOrderRequest) (*exchange.CreateOrderResponse, error) {
	if b.client == nil {
		return nil, fmt.Errorf("binance client not initialized")
	}

	// Validate and format price/quantity precision for Binance API
	// This follows the same logic as cta_trading_service project
	var priceStr, qtyStr string
	if b.filterSource != nil {
		filters := b.filterSource.ListExchangeSymbolFilters("binance", order.PosTypeFutures, req.Symbol)
		verified, err := order.VerifyOrderParamsForBinance(filters, req.Price, req.Quantity, req.Symbol, string(req.OrderType))
		if err != nil {
			return nil, fmt.Errorf("binance order validation: %w", err)
		}
		priceStr = verified.PriceStr
		qtyStr = verified.QuantityStr
	} else {
		// Fallback: use default formatting (may cause precision issues)
		qtyStr = strconv.FormatFloat(req.Quantity, 'f', -1, 64)
		priceStr = strconv.FormatFloat(req.Price, 'f', -1, 64)
	}

	service := b.client.NewCreateOrderService().
		Symbol(req.Symbol).
		Side(futures.SideType(req.Side)).
		Type(futures.OrderType(req.OrderType)).
		Quantity(qtyStr).
		PositionSide(futures.PositionSideType(req.PositionSide))
	// Note: ReduceOnly is NOT used in Hedge Mode (dual position side mode)
	// PositionSide (LONG/SHORT) already distinguishes opening vs closing positions
	// Binance API error -1106 if reduceOnly is sent in Hedge Mode

	if req.OrderType == exchange.OrderTypeLimit {
		service = service.
			Price(priceStr).
			TimeInForce(futures.TimeInForceTypeGTC)
	}

	orderResp, err := service.Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("binance create order: %w", err)
	}

	price, err := strconv.ParseFloat(orderResp.Price, 64)
	if err != nil {
		return nil, fmt.Errorf("parse price: %w", err)
	}
	qty, err := strconv.ParseFloat(orderResp.OrigQuantity, 64)
	if err != nil {
		return nil, fmt.Errorf("parse quantity: %w", err)
	}

	return &exchange.CreateOrderResponse{
		OrderID:    uint64(orderResp.OrderID),
		Symbol:     orderResp.Symbol,
		Side:       mapSide(string(orderResp.Side), orderResp.OrigQuantity),
		Status:     exchange.OrderStatus("NEW"), // Always return NEW for consistent Scanner processing
		Price:      price,
		Quantity:   qty,
		ExecutedAt: time.Now(),
	}, nil
}

func (b *BinanceFutures) CancelOrder(orderID uint64) error {
	if b.client == nil {
		return fmt.Errorf("binance client not initialized")
	}
	_, err := b.client.NewCancelOrderService().
		OrderID(int64(orderID)).
		Do(context.Background())
	return err
}

func (b *BinanceFutures) GetOrder(orderID uint64, symbol string) (*exchange.OrderInfo, error) {
	if b.client == nil {
		return nil, fmt.Errorf("binance client not initialized")
	}
	order, err := b.client.NewGetOrderService().
		Symbol(symbol).
		OrderID(int64(orderID)).
		Do(context.Background())
	if err != nil {
		return nil, err
	}

	price, err := strconv.ParseFloat(order.Price, 64)
	if err != nil {
		return nil, fmt.Errorf("parse price: %w", err)
	}
	qty, err := strconv.ParseFloat(order.OrigQuantity, 64)
	if err != nil {
		return nil, fmt.Errorf("parse quantity: %w", err)
	}
	filled, err := strconv.ParseFloat(order.ExecutedQuantity, 64)
	if err != nil {
		return nil, fmt.Errorf("parse filled: %w", err)
	}

	// Parse average execution price (actual fill price)
	avgPrice := price // default to order price if avgPrice is missing or zero
	if order.AvgPrice != "" {
		ap, err := strconv.ParseFloat(order.AvgPrice, 64)
		if err == nil && ap > 0 {
			avgPrice = ap
		}
	}

	return &exchange.OrderInfo{
		OrderID:  uint64(order.OrderID),
		Symbol:   order.Symbol,
		Side:     mapSide(string(order.Side), order.OrigQuantity),
		Status:   exchange.OrderStatus(order.Status),
		Price:    price,
		Qty:      qty,
		Filled:   filled,
		AvgPrice: avgPrice,
	}, nil
}

func (b *BinanceFutures) SetLeverage(symbol string, leverage int) error {
	if b.client == nil {
		return fmt.Errorf("binance client not initialized")
	}
	_, err := b.client.NewChangeLeverageService().
		Symbol(symbol).
		Leverage(leverage).
		Do(context.Background())
	return err
}

func (b *BinanceFutures) GetLeverage(symbol string) (int, error) {
	if b.client == nil {
		return 0, fmt.Errorf("binance client not initialized")
	}
	positions, err := b.client.NewGetPositionRiskService().Do(context.Background())
	if err != nil {
		return 0, err
	}
	for _, pos := range positions {
		if pos.Symbol == symbol {
			l, err := strconv.Atoi(pos.Leverage)
			if err != nil {
				return 0, fmt.Errorf("parse leverage: %w", err)
			}
			return l, nil
		}
	}
	return 0, fmt.Errorf("leverage not found for %s", symbol)
}

func (b *BinanceFutures) GetPrice(symbol string) (float64, error) {
	if b.client == nil {
		return 0, fmt.Errorf("binance client not initialized")
	}
	tickers, err := b.client.NewListPriceChangeStatsService().Symbol(symbol).Do(context.Background())
	if err != nil {
		return 0, err
	}
	if len(tickers) == 0 {
		return 0, fmt.Errorf("no price for %s", symbol)
	}
	return strconv.ParseFloat(tickers[0].LastPrice, 64)
}

func (b *BinanceFutures) GetPositions() ([]exchange.PositionInfo, error) {
	if b.client == nil {
		return nil, fmt.Errorf("binance client not initialized")
	}

	positions, err := b.client.NewGetPositionRiskService().Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("get position risk: %w", err)
	}

	var result []exchange.PositionInfo
	for _, pos := range positions {
		qty, err := strconv.ParseFloat(pos.PositionAmt, 64)
		if err != nil || qty == 0 {
			continue // Skip invalid or zero positions
		}

		entryPrice, _ := strconv.ParseFloat(pos.EntryPrice, 64)
		markPrice, _ := strconv.ParseFloat(pos.MarkPrice, 64)
		unrealizedPnl, _ := strconv.ParseFloat(pos.UnRealizedProfit, 64)
		leverage, _ := strconv.Atoi(pos.Leverage)

		result = append(result, exchange.MakePositionInfo(
			pos.Symbol, qty, entryPrice, markPrice, unrealizedPnl, leverage,
		))
	}

	return result, nil
}

func (b *BinanceFutures) Connect() error { return nil }
func (b *BinanceFutures) Close() error   { return nil }

func (b *BinanceFutures) SubscribeOrders(callback exchange.OrderCallback) error {
	return nil // handled by WebSocket manager
}

func mapSide(side string, origQty string) exchange.OrderSide {
	if side == "BUY" {
		return exchange.OrderSideBuy
	}
	return exchange.OrderSideSell
}
