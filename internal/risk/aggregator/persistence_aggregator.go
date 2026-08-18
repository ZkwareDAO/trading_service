package aggregator

import (
	"fmt"
	"log"
	"math"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/risk"
)

// PersistencePositionSource provides read access to user_order_positions.
type PersistencePositionSource interface {
	ListActivePositions() []*order.UserOrderPosition
	ListActiveUserPositions() []*order.UserPosition
}

// AggregateFromPersistence reads order positions from persistence layer,
// applies latest WS prices, computes PnL/ROI, and outputs risk.UserPosition.
func AggregateFromPersistence(src PersistencePositionSource, latestPrices map[string]map[string]float64) []*risk.UserPosition {
	subPositions := src.ListActivePositions()
	if len(subPositions) == 0 {
		return nil
	}

	groups := make(map[uint64][]*order.UserOrderPosition)
	for _, pos := range subPositions {
		if pos.Deleted != 0 {
			continue
		}
		groups[pos.UserStrategyID] = append(groups[pos.UserStrategyID], pos)
	}

	existing := make(map[uint64]*order.UserPosition)
	for _, up := range src.ListActiveUserPositions() {
		existing[up.UserStrategyID] = up
	}

	var result []*risk.UserPosition
	futureTime := time.Now().AddDate(100, 0, 0)

	for stratID, components := range groups {
		if len(components) == 0 {
			continue
		}

		var totalQty float64
		var totalMargin float64
		var pnl float64

		for _, sub := range components {
			totalQty += sub.Quantity
			totalMargin += math.Abs(sub.InitMargin)

			price := sub.CurrentPrice
			if exPrices, ok := latestPrices[sub.Exchange]; ok {
				if wsPrice, ok := lookupPrice(exPrices, sub.Asset); ok {
					price = wsPrice
				}
			}
			subPnl := calculatePositionPnL(sub.Side, sub.PosPrice, price, sub.Quantity)
			pnl += subPnl
		}

		first := components[0]
		currentPrice := first.CurrentPrice
		if exPrices, ok := latestPrices[first.Exchange]; ok {
			if wsPrice, ok := lookupPrice(exPrices, first.Asset); ok {
				currentPrice = wsPrice
			}
		}

		var roi float64
		if totalMargin > 0 {
			roi = calculatePositionROI(pnl, totalMargin, first.Leverage)
		}

		var maxProfitPct, maxLossPct float64
		if roi > 0 {
			maxProfitPct = roi
		} else {
			maxLossPct = roi
		}

		if ep, ok := existing[stratID]; ok {
			if ep.MaxProfitPercentage > maxProfitPct {
				maxProfitPct = ep.MaxProfitPercentage
			}
			if ep.MaxLossPercentage < maxLossPct {
				maxLossPct = ep.MaxLossPercentage
			}
		}

		userPos := &risk.UserPosition{
			ID:             getExistingID(existing, stratID),
			UserID:         first.UserID,
			UserStrategyID: stratID,
			Exchange:       first.Exchange,
			PosType:        risk.PosType(first.PosType),
			Symbol:         first.Asset,
			Side:           risk.Side(first.Side),
			CurrentPrice:   currentPrice,
			Quantity:       totalQty,
			TotalMargin:    totalMargin,
			Leverage:       first.Leverage,
			PnL:            pnl,
			ROI:            roi,
			MaxProfitPct:   maxProfitPct,
			Deleted:        0,
			CloseTime:      &futureTime,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		// Debug: log aggregated PnL/ROI
		if pnl != 0 || roi != 0 {
			fmt.Printf("Aggregator: strategyID=%d, symbol=%s, PnL=%.4f, ROI=%.4f, PosPrice=%.4f, CurrentPrice=%.4f, Quantity=%.4f\n",
				stratID, first.Asset, pnl, roi, first.PosPrice, currentPrice, totalQty)
		}

		result = append(result, userPos)
	}

	return result
}

func calculatePositionPnL(side order.Side, entryPrice, currentPrice, quantity float64) float64 {
	if side == order.SideLong {
		return (currentPrice - entryPrice) * quantity
	}
	return (entryPrice - currentPrice) * quantity
}

func calculatePositionROI(pnl, totalMargin float64, leverage int) float64 {
	if totalMargin == 0 {
		return 0
	}
	return (pnl / totalMargin) * float64(leverage)
}

func getExistingID(existing map[uint64]*order.UserPosition, stratID uint64) uint64 {
	if ep, ok := existing[stratID]; ok {
		return ep.ID
	}
	return 0
}

// hasActiveSubPositions checks if a user_strategy has active sub-positions (user_order_positions with deleted=0).
func hasActiveSubPositions(src PersistencePositionSource, userStrategyID uint64) bool {
	for _, pos := range src.ListActivePositions() {
		if pos.UserStrategyID == userStrategyID && pos.Deleted == 0 {
			return true
		}
	}
	return false
}

// UserPositionSyncer syncs aggregated risk.UserPosition back to order.UserPosition in the DB.
func UserPositionSyncer(repo *persistence.StateRepository, src PersistencePositionSource, latestPrices map[string]map[string]float64) error {
	positionMetrics := AggregateFromPersistenceWithMetrics(src, latestPrices)

	existing := make(map[uint64]*order.UserPosition)
	for _, up := range src.ListActiveUserPositions() {
		existing[up.ID] = up
		log.Printf("[UserPositionSyncer] existing user_position: ID=%d, UserStrategyID=%d, UserID=%d, Exchange=%s, Deleted=%d",
			up.ID, up.UserStrategyID, up.UserID, up.Exchange, up.Deleted)
	}

	now := time.Now()
	syncedKeys := make(map[uint64]bool)

	log.Printf("[UserPositionSyncer] aggregating %d position metrics", len(positionMetrics))
	for _, metrics := range positionMetrics {
		if metrics == nil || metrics.Position == nil {
			continue
		}
		rp := metrics.Position
		syncedKeys[rp.ID] = true
		log.Printf("[UserPositionSyncer] aggregated position: ID=%d, UserStrategyID=%d, UserID=%d, Exchange=%s, Symbol=%s, Qty=%.4f",
			rp.ID, rp.UserStrategyID, rp.UserID, rp.Exchange, rp.Symbol, rp.Quantity)

		if existingPos, ok := existing[rp.ID]; ok {
			existingPos.CurrentPrice = rp.CurrentPrice
			existingPos.Quantity = rp.Quantity
			existingPos.LatestMarketCapitalization = rp.CurrentPrice * rp.Quantity
			existingPos.TotalMargin = rp.TotalMargin
			existingPos.PnL = metrics.PnL
			existingPos.ROI = metrics.ROI
			if metrics.ROI > existingPos.MaxProfitPercentage {
				existingPos.MaxProfitPercentage = metrics.ROI
			}
			if metrics.ROI < existingPos.MaxLossPercentage {
				existingPos.MaxLossPercentage = metrics.ROI
			}
			existingPos.UpdatedAt = now
		} else {
			maxProfit := 0.0
			maxLoss := 0.0
			if metrics.ROI > 0 {
				maxProfit = metrics.ROI
			}
			if metrics.ROI < 0 {
				maxLoss = metrics.ROI
			}
			log.Printf("[UserPositionSyncer] CREATE user_position: UserStrategyID=%d, UserID=%d, Exchange=%s, Symbol=%s, Qty=%.4f, Reason=not found in existing map (rp.ID=%d)",
				rp.UserStrategyID, rp.UserID, rp.Exchange, rp.Symbol, rp.Quantity, rp.ID)
			repo.CreateUserPosition(&order.UserPosition{
				UserID:                     rp.UserID,
				UserStrategyID:             rp.UserStrategyID,
				Exchange:                   rp.Exchange,
				PosType:                    order.PosType(rp.PosType),
				CurrentPrice:               rp.CurrentPrice,
				Quantity:                   rp.Quantity,
				LatestMarketCapitalization: rp.CurrentPrice * rp.Quantity,
				TotalMargin:                rp.TotalMargin,
				PnL:                        metrics.PnL,
				ROI:                        metrics.ROI,
				MaxProfitPercentage:        maxProfit,
				MaxLossPercentage:          maxLoss,
				Deleted:                    0,
				UpdatedAt:                  now,
			})
		}
	}

	for _, existingPos := range existing {
		if existingPos.Deleted != 0 {
			continue
		}
		if !syncedKeys[existingPos.ID] {
			// Check if this position was skipped due to price=0 (has active sub-positions but no price)
			if hasActiveSubPositions(src, existingPos.UserStrategyID) {
				log.Printf("[UserPositionSyncer] SKIP close user_position: ID=%d, UserStrategyID=%d, Reason=price unavailable, preserving existing data",
					existingPos.ID, existingPos.UserStrategyID)
				continue
			}
			log.Printf("[UserPositionSyncer] CLOSE orphan user_position: ID=%d, UserStrategyID=%d, UserID=%d, Exchange=%s, Quantity=%.4f, Reason=not in syncedKeys",
				existingPos.ID, existingPos.UserStrategyID, existingPos.UserID, existingPos.Exchange, existingPos.Quantity)
			// Pass full quantity as closedQty to ensure remainingQty=0, preventing new record creation
			if _, err := repo.CloseAndCreateRemainingUserPosition(existingPos.ID, existingPos.Quantity, 0, now); err != nil {
				return fmt.Errorf("close orphan user_position %d: %w", existingPos.ID, err)
			}
		}
	}

	return nil
}

// PositionWithMetrics wraps a risk.UserPosition with computed metrics for rule evaluation.
type PositionWithMetrics struct {
	Position       *risk.UserPosition
	ROI            float64
	PnL            float64
	MaxProfitPct   float64
	MaxDrawdownPct float64
	OpenTrades     int
	ClosedTrades   int
	ProfitTrades   int
	LossTrades     int
}

// AggregateFromPersistenceWithMetrics aggregates sub-positions and computes trade counts
// for rule evaluation.
func AggregateFromPersistenceWithMetrics(src PersistencePositionSource, latestPrices map[string]map[string]float64) []*PositionWithMetrics {
	subPositions := src.ListActivePositions()
	if len(subPositions) == 0 {
		return nil
	}

	groups := make(map[uint64][]*order.UserOrderPosition)
	for _, pos := range subPositions {
		if pos.Deleted != 0 {
			continue
		}
		groups[pos.UserStrategyID] = append(groups[pos.UserStrategyID], pos)
	}

	existing := make(map[uint64]*order.UserPosition)
	for _, up := range src.ListActiveUserPositions() {
		existing[up.UserStrategyID] = up
	}

	futureTime := time.Now().AddDate(100, 0, 0)
	var result []*PositionWithMetrics

	for stratID, components := range groups {
		if len(components) == 0 {
			continue
		}

		first := components[0]
		currentPrice := first.CurrentPrice
		if exPrices, ok := latestPrices[first.Exchange]; ok {
			if wsPrice, ok := lookupPrice(exPrices, first.Asset); ok {
				currentPrice = wsPrice
			}
		}

		// Skip position when no price available (price=0 causes incorrect PnL/ROI)
		if currentPrice <= 0 {
			log.Printf("WARN: skipping position group strategy=%d (%s/%s): no price available", stratID, first.Exchange, first.Asset)
			continue
		}

		var totalQty float64
		var totalMargin float64
		var pnl float64
		var openTrades, closedTrades, profitTrades, lossTrades int

		for _, sub := range components {
			totalQty += sub.Quantity
			totalMargin += math.Abs(sub.InitMargin)

			subPnl := calculatePositionPnL(sub.Side, sub.PosPrice, currentPrice, sub.Quantity)
			pnl += subPnl

			openTrades++
			if sub.Deleted != 0 {
				closedTrades++
			} else if subPnl > 0 {
				profitTrades++
			} else if subPnl < 0 {
				lossTrades++
			}
		}

		var roi float64
		if totalMargin > 0 {
			roi = calculatePositionROI(pnl, totalMargin, first.Leverage)
		}

		var maxProfitPct, maxLossPct float64
		if roi > 0 {
			maxProfitPct = roi
		} else {
			maxLossPct = roi
		}

		if ep, ok := existing[stratID]; ok {
			if ep.MaxProfitPercentage > maxProfitPct {
				maxProfitPct = ep.MaxProfitPercentage
			}
			if ep.MaxLossPercentage < maxLossPct {
				maxLossPct = ep.MaxLossPercentage
			}
		}

		userPos := &risk.UserPosition{
			ID:             getExistingID(existing, stratID),
			UserID:         first.UserID,
			UserStrategyID: stratID,
			Exchange:       first.Exchange,
			PosType:        risk.PosType(first.PosType),
			Symbol:         first.Asset,
			Side:           risk.Side(first.Side),
			CurrentPrice:   currentPrice,
			Quantity:       totalQty,
			TotalMargin:    totalMargin,
			Leverage:       first.Leverage,
			PnL:            pnl,
			ROI:            roi,
			MaxProfitPct:   maxProfitPct,
			Deleted:        0,
			CloseTime:      &futureTime,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		result = append(result, &PositionWithMetrics{
			Position:       userPos,
			ROI:            roi,
			PnL:            pnl,
			MaxProfitPct:   maxProfitPct,
			MaxDrawdownPct: maxLossPct,
			OpenTrades:     openTrades,
			ClosedTrades:   closedTrades,
			ProfitTrades:   profitTrades,
			LossTrades:     lossTrades,
		})
	}

	return result
}

// lookupPrice finds the best-matching price for a given asset within an exchange's price map.
func lookupPrice(exPrices map[string]float64, asset string) (float64, bool) {
	if price, ok := exPrices[asset]; ok {
		return price, true
	}
	// Strip quote suffix for Hyperliquid (e.g., "NEARUSDC" → "NEAR")
	switch {
	case len(asset) > 4 && (asset[len(asset)-4:] == "USDT" || asset[len(asset)-4:] == "USDC"):
		coin := asset[:len(asset)-4]
		if price, ok := exPrices[coin]; ok {
			return price, true
		}
	}
	return 0, false
}
