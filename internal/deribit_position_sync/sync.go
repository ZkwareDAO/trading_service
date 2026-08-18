package deribit_position_sync

import (
	"context"
	"fmt"
	"log"
	"time"

	"trading-service/internal/exchange/deribit"
	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
)

const (
	defaultValidBefore = "2030-12-31T08:00:00Z"
	defaultCash        = 1000.0
	defaultParts       = 3
)

// SyncDeribitPositions synchronizes Deribit positions for all users.
// Exported for use by DeribitOrderMonitor when order not found after retries.
func SyncDeribitPositions(
	rpcClient *rpc.OrderServiceClient,
	repo *persistence.StateRepository,
	testnet bool,
	notifier notification.Notifier,
) error {
	// 1. Get all Deribit users
	users := repo.ListUsers()
	if len(users) == 0 {
		log.Printf("[DeribitSync] No users found")
		return nil
	}

	syncedCount := 0
	for _, user := range users {
		if user.Exchange != "deribit" {
			continue
		}

		// 2. Create Deribit client with user's API keys
		client, err := deribit.NewDeribit(
			user.APIKey,
			user.APISecret,
			user.APIPassword,
			testnet,
		)
		if err != nil {
			log.Printf("[DeribitSync] Failed to create client for user %d: %v", user.ID, err)
			continue
		}

		// 3. Connect to Deribit
		if err := client.Connect(); err != nil {
			log.Printf("[DeribitSync] Failed to connect for user %d: %v", user.ID, err)
			client.Close()
			continue
		}

		// 4. Get exchange positions
		exchangePositions, err := client.GetPositions()
		if err != nil {
			log.Printf("[DeribitSync] Failed to get positions for user %d: %v", user.ID, err)
			client.Close()
			continue
		}

		// 5. Get local active positions (deleted=0)
		active := true
		localPositions := repo.ListUserOrderPositionsByFilter(persistence.UserOrderPositionFilter{
			UserID:   user.ID,
			Active:   &active,
			Exchange: "deribit",
		})

		// 6. Match and sync positions
		matchResult := MatchPositions(exchangePositions, localPositions)

		// 7. Handle discrepancies
		for _, pos := range matchResult.ToCreate {
			if err := createPositionWithRPC(repo, rpcClient, user.ID, pos); err != nil {
				log.Printf("[DeribitSync] Failed to create position %s: %v", pos.Symbol, err)
			} else {
				log.Printf("[DeribitSync] Created position: %s %s qty=%.4f",
					pos.Symbol, sideToString(pos.Side), pos.Quantity)
				syncedCount++
			}
		}

		for _, pos := range matchResult.ToDelete {
			if err := markPositionDeleted(repo, pos, notifier); err != nil {
				log.Printf("[DeribitSync] Failed to mark position %d deleted: %v", pos.PositionID, err)
			} else {
				log.Printf("[DeribitSync] Marked position deleted: ID=%d %s",
					pos.PositionID, pos.Symbol)
				syncedCount++
			}
		}

		for _, pos := range matchResult.ToAdjust {
			if err := createDeltaPositionWithRPC(repo, rpcClient, user.ID, pos); err != nil {
				log.Printf("[DeribitSync] Failed to create delta position %s: %v", pos.Symbol, err)
			} else {
				log.Printf("[DeribitSync] Created delta position: %s %s qty=%.4f",
					pos.Symbol, sideToString(pos.Side), pos.DeltaQty)
				syncedCount++
			}
		}

		client.Close()
	}

	if syncedCount > 0 {
		log.Printf("[DeribitSync] Sync completed: %d positions updated", syncedCount)
	}

	// Reload user_strategies from CSV so PMS picks up strategies just created
	// in UOS via RPC during this sync. Without this, POST /api/v1/rules would
	// fail with "user_strategy_id xxx not found" because PMS in-memory state
	// is stale. Mirrors order_status_scanner FILLED handler.
	if err := repo.ReloadUserStrategies(); err != nil {
		log.Printf("[DeribitSync] warn: reload user strategies after sync: %v", err)
	} else {
		log.Printf("[DeribitSync] Reloaded user strategies after sync")
	}
	return nil
}

// markPositionDeleted marks a position as deleted and sends notification.
func markPositionDeleted(repo *persistence.StateRepository, pos PositionToDelete, notifier notification.Notifier) error {
	now := time.Now()

	// Get user_order_position to find user_strategy_id
	orderPos, _ := repo.GetUserOrderPositionByID(pos.PositionID)

	// Close user_order_position
	if err := repo.ClosePosition(pos.PositionID, now); err != nil {
		return err
	}

	// Close corresponding user_position if found
	if orderPos != nil {
		closeUserPositionByStrategy(repo, orderPos.UserStrategyID, now)
	}

	// Send notification (async)
	if notifier != nil {
		go sendDeletionNotification(notifier, pos.Symbol, pos.PositionID)
	}

	return nil
}

// closeUserPositionByStrategy finds and closes the active user_position for a strategy.
func closeUserPositionByStrategy(repo *persistence.StateRepository, userStrategyID uint64, closeTime time.Time) {
	for _, up := range repo.ListActiveUserPositions() {
		if up.UserStrategyID == userStrategyID && up.Deleted == 0 {
			if _, err := repo.CloseAndCreateRemainingUserPosition(up.ID, up.Quantity, 0, closeTime); err != nil {
				log.Printf("[markPositionDeleted] Warning: failed to close user_position %d: %v", up.ID, err)
			} else {
				log.Printf("[markPositionDeleted] Closed user_position ID=%d for strategy %d", up.ID, userStrategyID)
			}
			return // Only close the first match
		}
	}
}

// sendDeletionNotification sends a manual close notification.
func sendDeletionNotification(notifier notification.Notifier, symbol string, positionID uint64) {
	msg := &notification.ManualCloseMessage{
		Symbol:  symbol,
		Message: fmt.Sprintf("交易所仓位不存在,本地仓位已标记删除 (ID=%d)", positionID),
	}
	if err := notifier.SendManualCloseNotification(msg); err != nil {
		log.Printf("[DeribitSync] Failed to send notification: %v", err)
	}
}

// sideToString converts order.Side to string representation.
func sideToString(side order.Side) string {
	if side == order.SideLong {
		return "LONG"
	}
	return "SHORT"
}

// createPositionWithRPC creates a complete position record using RPC client for strategy creation.
// This is the refactored version that avoids direct CSV manipulation from PMS.
func createPositionWithRPC(
	repo *persistence.StateRepository,
	rpcClient *rpc.OrderServiceClient,
	userID uint64,
	pos PositionToCreate,
) error {
	ctx := context.Background()
	now := time.Now()
	strategyName := fmt.Sprintf("SYNC_%s", pos.Symbol)

	// 1. Create Strategy via RPC
	strategyResp, err := rpcClient.GetOrCreateStrategy(ctx, rpc.GetOrCreateStrategyRequest{
		Name:         strategyName,
		StrategyType: "MANUAL_SYNC",
	})
	if err != nil {
		return fmt.Errorf("rpc create strategy: %w", err)
	}

	// 2. Create StrategyAsset via RPC
	_, err = rpcClient.GetOrCreateStrategyAsset(ctx, rpc.GetOrCreateStrategyAssetRequest{
		Name:       strategyName,
		Asset:      pos.Symbol,
		StrategyID: strategyResp.StrategyID,
		PosType:    int(order.PosTypeOptions),
		Sort:       1,
	})
	if err != nil {
		return fmt.Errorf("rpc create strategy-asset: %w", err)
	}

	// 3. Create UserStrategy via RPC
	usResp, err := rpcClient.GetOrCreateUserStrategy(ctx, rpc.GetOrCreateUserStrategyRequest{
		UserID:           userID,
		Name:             strategyName,
		StrategyID:       strategyResp.StrategyID,
		Exchange:         "deribit",
		ValidBefore:      defaultValidBefore,
		Cash:             defaultCash,
		Parts:            defaultParts,
		Status:           1,
		RiskStrategyType: order.RiskStrategyTypeTraditional,
		OrdersNum:        0,
	})
	if err != nil {
		return fmt.Errorf("rpc create user-strategy: %w", err)
	}

	// 4. Create UserOrderPosition locally (this is PMS-owned data)
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		UserID:         userID,
		UserStrategyID: usResp.UserStrategyID,
		Asset:          pos.Symbol,
		Side:           pos.Side,
		Quantity:       pos.Quantity,
		Exchange:       "deribit",
		PosType:        order.PosTypeOptions,
		PosPrice:       pos.EntryPrice,
		InitMargin:     pos.Quantity * pos.EntryPrice,
		Leverage:       1,
		Deleted:        0,
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	return nil
}

// createDeltaPositionWithRPC creates a position for delta quantity with calculated average price using RPC.
func createDeltaPositionWithRPC(
	repo *persistence.StateRepository,
	rpcClient *rpc.OrderServiceClient,
	userID uint64,
	pos PositionToAdjust,
) error {
	return createPositionWithRPC(repo, rpcClient, userID, PositionToCreate{
		Symbol:     pos.Symbol,
		Side:       pos.Side,
		Quantity:   pos.DeltaQty,
		EntryPrice: pos.NewPrice,
	})
}
