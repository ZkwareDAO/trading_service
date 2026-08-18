package main

import (
	"fmt"
	"log"
	"os"

	"trading-service/internal/exchange/deribit"
)

func main() {
	// 从环境变量获取凭证
	apiKey := os.Getenv("DERIBIT_API_KEY")
	apiSecret := os.Getenv("DERIBIT_API_SECRET")
	apiPwd := os.Getenv("DERIBIT_API_PWD")

	if apiKey == "" || apiSecret == "" {
		log.Fatal("请设置环境变量: DERIBIT_API_KEY, DERIBIT_API_SECRET, DERIBIT_API_PWD (可选)")
	}

	// 创建 Deribit 客户端 (测试网)
	testnet := true
	client, err := deribit.NewDeribit(apiKey, apiSecret, apiPwd, testnet)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}

	// 认证连接
	fmt.Println("正在连接 Deribit 测试网...")
	if err := client.Connect(); err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer client.Close()

	fmt.Println("✅ 连接成功!")
	fmt.Println()

	// 查询仓位
	fmt.Println("正在查询期权仓位...")
	positions, err := client.GetPositions()
	if err != nil {
		log.Fatalf("查询仓位失败: %v", err)
	}

	if len(positions) == 0 {
		fmt.Println("ℹ️  没有找到期权仓位")
		return
	}

	// 显示仓位信息
	fmt.Printf("找到 %d 个期权仓位:\n", len(positions))
	fmt.Println("==================================================")
	for i, pos := range positions {
		fmt.Printf("\n仓位 #%d:\n", i+1)
		fmt.Printf("  标的:      %s\n", pos.Symbol)
		fmt.Printf("  方向:      %s\n", pos.PositionSide)
		fmt.Printf("  数量:      %.2f\n", pos.Quantity)
		fmt.Printf("  入场价格:  %.6f\n", pos.EntryPrice)
		if pos.UnrealizedPnl != 0 {
			fmt.Printf("  未实现盈亏: %.6f\n", pos.UnrealizedPnl)
		}
	}
}