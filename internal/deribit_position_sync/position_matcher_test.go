package deribit_position_sync

import (
	"math"
	"testing"

	"trading-service/internal/exchange"
	"trading-service/internal/order"
)

// TestMatchPositions_ScenarioA_DeltaPositive tests when exchange qty > local qty
// Exchange: qty=10, price=100
// Local: qty=3, price=95
// Expected: delta_qty=7, avg_price=(1000-285)/7=102.14
func TestMatchPositions_ScenarioA_DeltaPositive(t *testing.T) {
	// Arrange
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "BTC-28JUN24-50000-C",
			PositionSide: exchange.PositionSideLong,
			Quantity:     10.0,
			EntryPrice:   100.0,
		},
	}

	localPositions := []*order.UserOrderPosition{
		{
			ID:       1,
			UserID:   100,
			Asset:    "BTC-28JUN24-50000-C",
			Side:     order.SideLong,
			Quantity: 3.0,
			PosPrice: 95.0,
			Deleted:  0,
		},
	}

	// Act
	result := MatchPositions(exchangePositions, localPositions)

	// Assert
	if len(result.ToCreate) != 0 {
		t.Errorf("Expected no ToCreate, got %d", len(result.ToCreate))
	}
	if len(result.ToDelete) != 0 {
		t.Errorf("Expected no ToDelete, got %d", len(result.ToDelete))
	}
	if len(result.ToAdjust) != 1 {
		t.Fatalf("Expected 1 ToAdjust, got %d", len(result.ToAdjust))
	}

	adjust := result.ToAdjust[0]
	if adjust.Symbol != "BTC-28JUN24-50000-C" {
		t.Errorf("Expected symbol BTC-28JUN24-50000-C, got %s", adjust.Symbol)
	}
	if adjust.Side != order.SideLong {
		t.Errorf("Expected side LONG, got %d", adjust.Side)
	}
	expectedDelta := 7.0
	if math.Abs(adjust.DeltaQty-expectedDelta) > 0.001 {
		t.Errorf("Expected delta_qty %.2f, got %.2f", expectedDelta, adjust.DeltaQty)
	}
	// Expected avg_price = (100*10 - 95*3) / 7 = (1000 - 285) / 7 = 102.14
	expectedAvgPrice := 102.142857
	if math.Abs(adjust.NewPrice-expectedAvgPrice) > 0.001 {
		t.Errorf("Expected avg_price %.2f, got %.2f", expectedAvgPrice, adjust.NewPrice)
	}
}

// TestMatchPositions_ScenarioA_DeltaNegative tests when exchange qty < local qty
// Exchange: qty=3, price=100
// Local: qty=10, price=95
// Expected: close all local positions, create new with qty=3
func TestMatchPositions_ScenarioA_DeltaNegative(t *testing.T) {
	// Arrange
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "BTC-28JUN24-50000-C",
			PositionSide: exchange.PositionSideLong,
			Quantity:     3.0,
			EntryPrice:   100.0,
		},
	}

	localPositions := []*order.UserOrderPosition{
		{
			ID:       1,
			UserID:   100,
			Asset:    "BTC-28JUN24-50000-C",
			Side:     order.SideLong,
			Quantity: 10.0,
			PosPrice: 95.0,
			Deleted:  0,
		},
	}

	// Act
	result := MatchPositions(exchangePositions, localPositions)

	// Assert - should close local and create new
	if len(result.ToDelete) != 1 {
		t.Fatalf("Expected 1 ToDelete, got %d", len(result.ToDelete))
	}
	if len(result.ToCreate) != 1 {
		t.Fatalf("Expected 1 ToCreate, got %d", len(result.ToCreate))
	}

	// Verify delete
	delete := result.ToDelete[0]
	if delete.PositionID != 1 {
		t.Errorf("Expected position ID 1, got %d", delete.PositionID)
	}

	// Verify create
	create := result.ToCreate[0]
	if create.Quantity != 3.0 {
		t.Errorf("Expected quantity 3.0, got %.2f", create.Quantity)
	}
	if create.EntryPrice != 100.0 {
		t.Errorf("Expected entry price 100.0, got %.2f", create.EntryPrice)
	}
}

// TestMatchPositions_ScenarioB_ExchangeNotExist tests when exchange has no position
// Exchange: empty
// Local: qty=5
// Expected: ToDelete with position ID
func TestMatchPositions_ScenarioB_ExchangeNotExist(t *testing.T) {
	// Arrange
	exchangePositions := []exchange.PositionInfo{}

	localPositions := []*order.UserOrderPosition{
		{
			ID:       1,
			UserID:   100,
			Asset:    "BTC-28JUN24-50000-C",
			Side:     order.SideLong,
			Quantity: 5.0,
			PosPrice: 100.0,
			Deleted:  0,
		},
	}

	// Act
	result := MatchPositions(exchangePositions, localPositions)

	// Assert
	if len(result.ToDelete) != 1 {
		t.Fatalf("Expected 1 ToDelete, got %d", len(result.ToDelete))
	}
	if len(result.ToCreate) != 0 {
		t.Errorf("Expected no ToCreate, got %d", len(result.ToCreate))
	}

	delete := result.ToDelete[0]
	if delete.PositionID != 1 {
		t.Errorf("Expected position ID 1, got %d", delete.PositionID)
	}
	if delete.Symbol != "BTC-28JUN24-50000-C" {
		t.Errorf("Expected symbol BTC-28JUN24-50000-C, got %s", delete.Symbol)
	}
}

// TestMatchPositions_ScenarioC_LocalNotExist tests when local has no position
// Exchange: qty=10
// Local: empty
// Expected: ToCreate with full quantity
func TestMatchPositions_ScenarioC_LocalNotExist(t *testing.T) {
	// Arrange
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "BTC-28JUN24-50000-C",
			PositionSide: exchange.PositionSideLong,
			Quantity:     10.0,
			EntryPrice:   100.0,
		},
	}

	localPositions := []*order.UserOrderPosition{}

	// Act
	result := MatchPositions(exchangePositions, localPositions)

	// Assert
	if len(result.ToCreate) != 1 {
		t.Fatalf("Expected 1 ToCreate, got %d", len(result.ToCreate))
	}
	if len(result.ToDelete) != 0 {
		t.Errorf("Expected no ToDelete, got %d", len(result.ToDelete))
	}

	create := result.ToCreate[0]
	if create.Symbol != "BTC-28JUN24-50000-C" {
		t.Errorf("Expected symbol BTC-28JUN24-50000-C, got %s", create.Symbol)
	}
	if create.Quantity != 10.0 {
		t.Errorf("Expected quantity 10.0, got %.2f", create.Quantity)
	}
	if create.EntryPrice != 100.0 {
		t.Errorf("Expected entry price 100.0, got %.2f", create.EntryPrice)
	}
}

// TestMatchPositions_ShortPositions tests handling of SHORT positions
func TestMatchPositions_ShortPositions(t *testing.T) {
	// Arrange
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "BTC-28JUN24-50000-P",
			PositionSide: exchange.PositionSideShort,
			Quantity:     5.0,
			EntryPrice:   50.0,
		},
	}

	localPositions := []*order.UserOrderPosition{}

	// Act
	result := MatchPositions(exchangePositions, localPositions)

	// Assert
	if len(result.ToCreate) != 1 {
		t.Fatalf("Expected 1 ToCreate, got %d", len(result.ToCreate))
	}

	create := result.ToCreate[0]
	if create.Side != order.SideShort {
		t.Errorf("Expected side SHORT, got %d", create.Side)
	}
}

// TestMatchPositions_MultipleSymbols tests handling multiple different symbols
func TestMatchPositions_MultipleSymbols(t *testing.T) {
	// Arrange
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "BTC-28JUN24-50000-C",
			PositionSide: exchange.PositionSideLong,
			Quantity:     10.0,
			EntryPrice:   100.0,
		},
		{
			Symbol:       "ETH-28JUN24-3000-C",
			PositionSide: exchange.PositionSideLong,
			Quantity:     5.0,
			EntryPrice:   20.0,
		},
	}

	localPositions := []*order.UserOrderPosition{}

	// Act
	result := MatchPositions(exchangePositions, localPositions)

	// Assert
	if len(result.ToCreate) != 2 {
		t.Fatalf("Expected 2 ToCreate, got %d", len(result.ToCreate))
	}
}

// TestMatchPositions_QuantityMatch tests when quantities match exactly
func TestMatchPositions_QuantityMatch(t *testing.T) {
	// Arrange
	exchangePositions := []exchange.PositionInfo{
		{
			Symbol:       "BTC-28JUN24-50000-C",
			PositionSide: exchange.PositionSideLong,
			Quantity:     10.0,
			EntryPrice:   100.0,
		},
	}

	localPositions := []*order.UserOrderPosition{
		{
			ID:       1,
			UserID:   100,
			Asset:    "BTC-28JUN24-50000-C",
			Side:     order.SideLong,
			Quantity: 10.0,
			PosPrice: 95.0,
			Deleted:  0,
		},
	}

	// Act
	result := MatchPositions(exchangePositions, localPositions)

	// Assert - should have no actions when quantities match
	if len(result.ToCreate) != 0 {
		t.Errorf("Expected no ToCreate, got %d", len(result.ToCreate))
	}
	if len(result.ToDelete) != 0 {
		t.Errorf("Expected no ToDelete, got %d", len(result.ToDelete))
	}
	if len(result.ToAdjust) != 0 {
		t.Errorf("Expected no ToAdjust, got %d", len(result.ToAdjust))
	}
}
