package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/exchange/deribit"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

const (
	positionKeyFormat  = "%s_%s"
	quantityTolerance  = 0.0001
	defaultValidBefore = "2030-12-31T08:00:00Z"
	defaultCash        = 1000.0
	defaultParts       = 3
)

// PositionToSync represents a position that needs to be synced
type PositionToSync struct {
	Symbol       string
	PositionSide exchange.PositionSide
	Quantity     float64
	EntryPrice   float64
}

// positionKey creates a unique key for position aggregation
func positionKey(symbol string, side string) string {
	return fmt.Sprintf(positionKeyFormat, symbol, side)
}

// sideToString converts order.Side to string representation
func sideToString(side order.Side) string {
	if side == order.SideLong {
		return "LONG"
	}
	return "SHORT"
}

// toOrderSide converts exchange.PositionSide to order.Side
func toOrderSide(side exchange.PositionSide) order.Side {
	if side == exchange.PositionSideLong {
		return order.SideLong
	}
	return order.SideShort
}

// filterPositionsToSync compares exchange positions with local positions and returns positions that need syncing
func filterPositionsToSync(repo *persistence.StateRepository, userID uint64, exchangePositions []exchange.PositionInfo) []PositionToSync {
	active := true
	localPositions := repo.ListUserOrderPositionsByFilter(persistence.UserOrderPositionFilter{
		UserID: userID,
		Active: &active,
	})

	// Aggregate local positions by symbol+side
	localAggregated := make(map[string]float64)
	for _, pos := range localPositions {
		key := positionKey(pos.Asset, sideToString(pos.Side))
		localAggregated[key] += pos.Quantity
	}

	// Find positions that need syncing
	var toSync []PositionToSync
	for _, exPos := range exchangePositions {
		key := positionKey(exPos.Symbol, string(exPos.PositionSide))
		localQty := localAggregated[key]

		if math.Abs(localQty-exPos.Quantity) > quantityTolerance {
			toSync = append(toSync, PositionToSync{
				Symbol:       exPos.Symbol,
				PositionSide: exPos.PositionSide,
				Quantity:     exPos.Quantity,
				EntryPrice:   exPos.EntryPrice,
			})
		}
	}

	return toSync
}

// syncPosition creates strategy and position records for a new position.
// Reuses existing Strategy/StrategyAsset/UserStrategy if they already exist.
func syncPosition(repo *persistence.StateRepository, userID uint64, pos PositionToSync) error {
	now := time.Now()
	strategyName := fmt.Sprintf("SYNC_%s", pos.Symbol)

	// 1. Check if strategy exists, create if not
	strategy, err := repo.GetStrategyByName(strategyName)
	if err != nil {
		strategy = &order.Strategy{
			Name:         strategyName,
			StrategyType: "MANUAL_SYNC",
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		strategy.ID = repo.CreateStrategy(strategy)
	}

	// 2. Check if strategy asset exists, create if not
	_, err = repo.GetStrategyAssetByNameAssetStrategy(strategyName, pos.Symbol, strategy.ID)
	if err != nil {
		repo.CreateStrategyAsset(&order.StrategyAsset{
			Name:       strategyName,
			Asset:      pos.Symbol,
			StrategyID: strategy.ID,
			PosType:    order.PosTypeOptions,
			Sort:       1,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}

	// 3. Check if user strategy exists, create if not
	us, err := repo.GetUserStrategyByUserNameStrategy(userID, strategyName, strategy.ID)
	if err != nil {
		validBefore, _ := time.Parse(time.RFC3339, defaultValidBefore)
		us = &order.UserStrategy{
			UserID:           userID,
			Name:             strategyName,
			Exchange:         "deribit",
			ValidBefore:      validBefore,
			Cash:             defaultCash,
			Parts:            defaultParts,
			Status:           1,
			StrategyID:       strategy.ID,
			RiskStrategyType: order.RiskStrategyTypeTraditional,
			OrdersNum:        0,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		us.ID = repo.CreateUserStrategy(us)
	}

	// 4. Always create UserOrderPosition (no dedup by design)
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: us.ID,
		Asset:          pos.Symbol,
		Side:           toOrderSide(pos.PositionSide),
		Quantity:       pos.Quantity,
		Exchange:       "deribit",
		PosType:        order.PosTypeOptions,
		PosPrice:       pos.EntryPrice,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	return nil
}

func main() {
	// Get credentials from environment
	apiKey := os.Getenv("DERIBIT_API_KEY")
	apiSecret := os.Getenv("DERIBIT_API_SECRET")
	apiPwd := os.Getenv("DERIBIT_API_PWD")

	if apiKey == "" || apiSecret == "" {
		log.Fatal("请设置环境变量: DERIBIT_API_KEY, DERIBIT_API_SECRET, DERIBIT_API_PWD")
	}

	// Get user ID from command line
	if len(os.Args) < 2 {
		log.Fatal("Usage: sync_deribit_positions <user_id>")
	}
	userIDStr := os.Args[1]
	var userID uint64
	if _, err := fmt.Sscanf(userIDStr, "%d", &userID); err != nil {
		log.Fatalf("无效的 user_id: %s", userIDStr)
	}

	// Initialize repository
	dataDir := "./data"
	gs, err := persistence.NewGlobalState(dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize state: %v", err)
	}
	repo := persistence.NewStateRepository(gs)

	// Create Deribit client
	testnet := true // TODO: make configurable
	client, err := deribit.NewDeribit(apiKey, apiSecret, apiPwd, testnet)
	if err != nil {
		log.Fatalf("Failed to create Deribit client: %v", err)
	}

	// Connect
	fmt.Println("正在连接 Deribit...")
	if err := client.Connect(); err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer client.Close()

	// Query positions
	fmt.Println("正在查询期权仓位...")
	positions, err := client.GetPositions()
	if err != nil {
		log.Fatalf("查询仓位失败: %v", err)
	}

	fmt.Printf("找到 %d 个期权仓位\n", len(positions))

	// Filter positions to sync
	toSync := filterPositionsToSync(repo, userID, positions)

	if len(toSync) == 0 {
		fmt.Println("✅ 所有仓位已同步，无需更新")
		return
	}

	fmt.Printf("需要同步 %d 个仓位:\n", len(toSync))
	for _, pos := range toSync {
		fmt.Printf("  - %s %s %.4f\n", pos.Symbol, pos.PositionSide, pos.Quantity)
	}

	// Sync positions
	for _, pos := range toSync {
		fmt.Printf("正在同步: %s %s\n", pos.Symbol, pos.PositionSide)
		if err := syncPosition(repo, userID, pos); err != nil {
			log.Printf("❌ 同步失败: %v", err)
			continue
		}
		fmt.Printf("✅ 同步成功: %s\n", pos.Symbol)
	}

	// Flush data to CSV files
	fmt.Println("正在保存数据...")
	gs.Shutdown()

	fmt.Println("同步完成")
}
