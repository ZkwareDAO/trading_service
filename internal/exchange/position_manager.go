package exchange

import (
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"
)

// PositionManager manages positions: leverage, price query, active positions, closing.
type PositionManager struct {
	repo     *persistence.StateRepository
	exchange Exchange
}

// NewPositionManager creates a new position manager.
func NewPositionManager(repo *persistence.StateRepository, ex Exchange) *PositionManager {
	return &PositionManager{repo: repo, exchange: ex}
}

// SetLeverage sets leverage on exchange.
func (pm *PositionManager) SetLeverage(symbol string, leverage int) error {
	return pm.exchange.SetLeverage(symbol, leverage)
}

// GetPrice queries current price from exchange.
func (pm *PositionManager) GetPrice(symbol string) (float64, error) {
	return pm.exchange.GetPrice(symbol)
}

// ListActivePositions returns all open positions (deleted=0).
func (pm *PositionManager) ListActivePositions() []*order.UserOrderPosition {
	return pm.repo.ListActivePositions()
}

// ClosePosition marks a position as closed.
func (pm *PositionManager) ClosePosition(posID uint64) error {
	return pm.repo.ClosePosition(posID, time.Now())
}
