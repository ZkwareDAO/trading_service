package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"trading-service/internal/exchange/binance"
	"trading-service/internal/persistence"
)

func main() {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}

	// 加载用户数据
	state, err := persistence.NewGlobalState(dataDir)
	if err != nil {
		log.Fatalf("加载数据失败: %v", err)
	}

	reader := bufio.NewReader(os.Stdin)

	// 输入 user_id
	fmt.Print("请输入 user_id: ")
	userIDStr, _ := reader.ReadString('\n')
	userID, err := strconv.ParseUint(strings.TrimSpace(userIDStr), 10, 64)
	if err != nil {
		log.Fatalf("无效的 user_id: %v", err)
	}

	// 输入 exchange_order_id
	fmt.Print("请输入 exchange_order_id: ")
	orderIDStr, _ := reader.ReadString('\n')
	orderID, err := strconv.ParseUint(strings.TrimSpace(orderIDStr), 10, 64)
	if err != nil {
		log.Fatalf("无效的 exchange_order_id: %v", err)
	}

	// 输入 symbol
	fmt.Print("请输入 symbol (如 ETHUSDT): ")
	symbol, _ := reader.ReadString('\n')
	symbol = strings.TrimSpace(symbol)

	// 输入是否测试网
	fmt.Print("是否测试网? (y/n): ")
	testnetStr, _ := reader.ReadString('\n')
	testnet := strings.TrimSpace(strings.ToLower(testnetStr)) == "y"

	// 查找用户
	user, ok := state.Users[userID]
	if !ok {
		log.Fatalf("未找到 user_id=%d 的用户", userID)
	}

	fmt.Printf("\n查询信息:\n")
	fmt.Printf("  User ID: %d\n", userID)
	fmt.Printf("  Exchange: %s\n", user.Exchange)
	fmt.Printf("  Order ID: %d\n", orderID)
	fmt.Printf("  Symbol: %s\n", symbol)
	fmt.Printf("  Testnet: %v\n\n", testnet)

	// 根据交易所调用对应API
	switch user.Exchange {
	case "binance":
		queryBinanceOrder(user.APIKey, user.APISecret, testnet, orderID, symbol)
	case "hyperliquid":
		fmt.Println("暂不支持 hyperliquid，请联系开发者")
	case "deribit":
		fmt.Println("暂不支持 deribit，请联系开发者")
	default:
		fmt.Printf("不支持的交易所: %s\n", user.Exchange)
	}
}

func queryBinanceOrder(apiKey, apiSecret string, testnet bool, orderID uint64, symbol string) {
	client := binance.NewBinanceFutures(apiKey, apiSecret, testnet)

	order, err := client.GetOrder(orderID, symbol)
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}

	fmt.Println("========================================")
	fmt.Println("订单信息:")
	fmt.Println("========================================")
	fmt.Printf("订单ID:     %d\n", order.OrderID)
	fmt.Printf("交易对:     %s\n", order.Symbol)
	fmt.Printf("方向:       %s\n", order.Side)
	fmt.Printf("状态:       %s\n", formatStatus(string(order.Status)))
	fmt.Printf("订单数量:   %.8f\n", order.Qty)
	fmt.Printf("成交数量:   %.8f\n", order.Filled)
	fmt.Printf("订单价格:   %.8f\n", order.Price)
	fmt.Printf("平均成交价: %.8f\n", order.AvgPrice)
	fmt.Println("========================================")

	// 显示成交百分比
	if order.Qty > 0 {
		filledPercent := (order.Filled / order.Qty) * 100
		fmt.Printf("成交比例:   %.2f%%\n", filledPercent)
	}
}

func formatStatus(status string) string {
	statusMap := map[string]string{
		"NEW":              "未成交",
		"PARTIALLY_FILLED": "部分成交",
		"FILLED":           "完全成交",
		"CANCELED":         "已取消",
		"EXPIRED":          "已过期",
	}
	if cn, ok := statusMap[status]; ok {
		return fmt.Sprintf("%s (%s)", cn, status)
	}
	return status
}
