package deribit

import (
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"trading-service/internal/notification"
)

// SpreadChecker checks bid/ask spread before closing positions.
// Used for Deribit options where wide spreads may require manual intervention.
type SpreadChecker struct {
	spreadThreshold      float64               // Absolute spread threshold (e.g., 0.005)
	notifier             notification.Notifier // Notification interface
	roiLookup            func(uint64) float64  // optional: returns current ROI by userStrategyID, nil disables ROI display
	lastNotificationTime map[string]time.Time  // symbol -> last notification time (rate limiting)
	notificationMutex    sync.RWMutex          // Protect concurrent access to lastNotificationTime
}

// NewSpreadChecker creates a spread checker with threshold and notifier.
// roiLookup is optional (pass nil to disable ROI display in notifications).
func NewSpreadChecker(spreadThreshold float64, notifier notification.Notifier, roiLookup func(uint64) float64) *SpreadChecker {
	return &SpreadChecker{
		spreadThreshold:      spreadThreshold,
		notifier:             notifier,
		roiLookup:            roiLookup,
		lastNotificationTime: make(map[string]time.Time),
	}
}

// CheckSpreadBeforeClose checks if bid/ask spread is acceptable before closing.
// Returns (spread, threshold, error) where error=nil means spread is acceptable.
// If error!=nil, spread is too wide and notification has been sent.
func (c *SpreadChecker) CheckSpreadBeforeClose(
	ex *Deribit,
	symbol string,
	userID uint64,
	userStrategyID uint64,
) (spread float64, threshold float64, err error) {
	// 1. Get bid/ask prices
	ticker, err := ex.GetTickerInfo(symbol)
	if err != nil {
		return 0, 0, fmt.Errorf("get ticker: %w", err)
	}

	// 2. Calculate absolute spread
	spread = math.Abs(ticker.BestAsk - ticker.BestBid)

	log.Printf("[Deribit] Spread check before close: symbol=%s, bid=%.6f, ask=%.6f, spread=%.6f, threshold=%.6f",
		symbol, ticker.BestBid, ticker.BestAsk, spread, c.spreadThreshold)

	// 3. Check if spread exceeds threshold
	if spread > c.spreadThreshold {
		log.Printf("[Deribit] Spread too wide (%.6f > %.6f), sending notification",
			spread, c.spreadThreshold)

		// Send notification (async, non-blocking)
		go c.sendSpreadTooWideNotification(symbol, userID, userStrategyID, ticker, spread)

		return spread, c.spreadThreshold, fmt.Errorf("spread too wide: %.6f > %.6f", spread, c.spreadThreshold)
	}

	// 4. Spread is acceptable
	log.Printf("[Deribit] Spread acceptable (%.6f <= %.6f), proceeding with close",
		spread, c.spreadThreshold)
	return spread, c.spreadThreshold, nil
}

func (c *SpreadChecker) sendSpreadTooWideNotification(
	symbol string,
	userID uint64,
	userStrategyID uint64,
	ticker *TickerInfo,
	spread float64,
) {
	if c.notifier == nil {
		return
	}

	// Rate limiting: check if notification was sent within last 10 minutes
	const notificationCooldown = 10 * time.Minute

	c.notificationMutex.RLock()
	lastTime, exists := c.lastNotificationTime[symbol]
	c.notificationMutex.RUnlock()

	if exists && time.Since(lastTime) < notificationCooldown {
		log.Printf("[Deribit] Spread notification skipped (rate limited): symbol=%s, last_sent=%v ago",
			symbol, time.Since(lastTime))
		return
	}

	// Update last notification time
	c.notificationMutex.Lock()
	c.lastNotificationTime[symbol] = time.Now()
	c.notificationMutex.Unlock()

	msg := &notification.ManualCloseMessage{
		Symbol:   symbol,
		BidPrice: ticker.BestBid,
		AskPrice: ticker.BestAsk,
		Spread:   spread,
		Message:  fmt.Sprintf("买一卖一差距悬殊（%.6f），需要手动操作", spread),
	}

	// Attach current ROI if lookup is available
	if c.roiLookup != nil {
		msg.ROI = c.roiLookup(userStrategyID)
	}

	if err := c.notifier.SendManualCloseNotification(msg); err != nil {
		log.Printf("[Deribit] Failed to send spread notification: %v", err)
	}
}