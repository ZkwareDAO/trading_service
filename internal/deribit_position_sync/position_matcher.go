package deribit_position_sync

import (
	"fmt"
	"math"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
)

// PositionMatchResult contains the result of position matching.
type PositionMatchResult struct {
	ToCreate []PositionToCreate
	ToDelete []PositionToDelete
	ToAdjust []PositionToAdjust
}

// PositionToCreate represents a position that needs to be created.
type PositionToCreate struct {
	Symbol     string
	Side       order.Side
	Quantity   float64
	EntryPrice float64
}

// PositionToDelete represents a position that needs to be deleted.
type PositionToDelete struct {
	PositionID uint64
	Symbol     string
	Side       order.Side
}

// PositionToAdjust represents a position that needs to be adjusted (delta quantity).
type PositionToAdjust struct {
	Symbol   string
	Side     order.Side
	DeltaQty float64 // positive = add, negative = remove
	NewPrice float64 // calculated average price
}

const quantityTolerance = 0.0001

// MatchPositions compares exchange positions with local positions and returns matching result.
func MatchPositions(
	exchangePositions []exchange.PositionInfo,
	localPositions []*order.UserOrderPosition,
) *PositionMatchResult {
	result := &PositionMatchResult{}

	// Aggregate local positions by symbol+side
	localMap := make(map[string][]*order.UserOrderPosition)
	for _, pos := range localPositions {
		key := positionKey(pos.Asset, pos.Side)
		localMap[key] = append(localMap[key], pos)
	}

	// Check exchange positions
	for _, exPos := range exchangePositions {
		key := positionKey(exPos.Symbol, exPos.PositionSide)
		localGroup := localMap[key]

		if len(localGroup) == 0 {
			// Scenario C: exchange exists, local doesn't
			result.ToCreate = append(result.ToCreate, PositionToCreate{
				Symbol:     exPos.Symbol,
				Side:       toOrderSide(exPos.PositionSide),
				Quantity:   exPos.Quantity,
				EntryPrice: exPos.EntryPrice,
			})
		} else {
			// Scenario A: both exist, check quantity
			localQty := aggregateQuantity(localGroup)
			deltaQty := exPos.Quantity - localQty

			if math.Abs(deltaQty) > quantityTolerance {
				if deltaQty > 0 {
					// Local < Exchange: need to add delta quantity
					avgPrice := calculateAveragePrice(exPos, localGroup, deltaQty)
					result.ToAdjust = append(result.ToAdjust, PositionToAdjust{
						Symbol:   exPos.Symbol,
						Side:     toOrderSide(exPos.PositionSide),
						DeltaQty: deltaQty,
						NewPrice: avgPrice,
					})
				} else {
					// Local > Exchange: close all local positions, then create new
					for _, pos := range localGroup {
						result.ToDelete = append(result.ToDelete, PositionToDelete{
							PositionID: pos.ID,
							Symbol:     pos.Asset,
							Side:       pos.Side,
						})
					}
					// Create new position with exchange quantity
					result.ToCreate = append(result.ToCreate, PositionToCreate{
						Symbol:     exPos.Symbol,
						Side:       toOrderSide(exPos.PositionSide),
						Quantity:   exPos.Quantity,
						EntryPrice: exPos.EntryPrice,
					})
				}
			}
		}

		// Mark as matched
		delete(localMap, key)
	}

	// Scenario B: exchange doesn't exist, local does
	for _, localGroup := range localMap {
		for _, pos := range localGroup {
			result.ToDelete = append(result.ToDelete, PositionToDelete{
				PositionID: pos.ID,
				Symbol:     pos.Asset,
				Side:       pos.Side,
			})
		}
	}

	return result
}

// positionKey creates a unique key for position aggregation.
func positionKey(symbol string, side interface{}) string {
	var sideStr string
	switch s := side.(type) {
	case order.Side:
		if s == order.SideLong {
			sideStr = "LONG"
		} else {
			sideStr = "SHORT"
		}
	case exchange.PositionSide:
		sideStr = string(s)
	default:
		sideStr = fmt.Sprintf("%v", s)
	}
	return fmt.Sprintf("%s_%s", symbol, sideStr)
}

// toOrderSide converts exchange.PositionSide to order.Side.
func toOrderSide(side exchange.PositionSide) order.Side {
	if side == exchange.PositionSideLong {
		return order.SideLong
	}
	return order.SideShort
}

// aggregateQuantity sums up quantities from multiple positions.
func aggregateQuantity(positions []*order.UserOrderPosition) float64 {
	var total float64
	for _, pos := range positions {
		total += pos.Quantity
	}
	return total
}

// calculateAveragePrice calculates weighted average price for delta quantity.
// Formula: (exchange_total_cost - local_total_cost) / delta_qty
func calculateAveragePrice(
	exPos exchange.PositionInfo,
	localGroup []*order.UserOrderPosition,
	deltaQty float64,
) float64 {
	// Calculate local total cost
	localTotalCost := 0.0
	for _, pos := range localGroup {
		localTotalCost += pos.PosPrice * pos.Quantity
	}

	// Calculate exchange total cost
	exchangeTotalCost := exPos.EntryPrice * exPos.Quantity

	// Calculate delta cost
	deltaTotalCost := exchangeTotalCost - localTotalCost

	// Calculate average price
	if deltaQty > 0 {
		return deltaTotalCost / deltaQty
	}
	return exPos.EntryPrice
}
