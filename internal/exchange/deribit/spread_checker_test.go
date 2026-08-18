package deribit

import (
	"errors"
	"sync"
	"testing"
	"time"

	"trading-service/internal/notification"
)

// MockNotifier implements notification.Notifier for testing
type MockNotifier struct {
	mu           sync.RWMutex
	sentMessages []*notification.ManualCloseMessage
	shouldError  bool
}

func (m *MockNotifier) SendManualCloseNotification(msg *notification.ManualCloseMessage) error {
	if m.shouldError {
		return errors.New("mock notification error")
	}
	m.mu.Lock()
	m.sentMessages = append(m.sentMessages, msg)
	m.mu.Unlock()
	return nil
}

// GetSentMessages returns a copy of sent messages (thread-safe)
func (m *MockNotifier) GetSentMessages() []*notification.ManualCloseMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*notification.ManualCloseMessage, len(m.sentMessages))
	copy(result, m.sentMessages)
	return result
}

// ClearMessages clears sent messages (thread-safe)
func (m *MockNotifier) ClearMessages() {
	m.mu.Lock()
	m.sentMessages = nil
	m.mu.Unlock()
}

func (m *MockNotifier) SendOpenOrder(msg *notification.OpenOrderMessage) error {
	return nil
}

func (m *MockNotifier) SendCloseOrder(msg *notification.CloseOrderMessage) error {
	return nil
}

func (m *MockNotifier) SendTest(msg *notification.TestMessage) error {
	return nil
}

func (m *MockNotifier) SendDeribitPositionNotification(*notification.DeribitPositionMessage) error {
	return nil
}

// TestSpreadChecker_CheckSpreadLogic tests the core spread checking logic
// without depending on real Deribit client
func TestSpreadChecker_CheckSpreadLogic(t *testing.T) {
	tests := []struct {
		name       string
		bid        float64
		ask        float64
		threshold  float64
		wantOK     bool
		wantSpread float64
	}{
		{
			name:       "spread equals threshold",
			bid:        0.003,
			ask:        0.008,
			threshold:  0.005,
			wantOK:     true,
			wantSpread: 0.005,
		},
		{
			name:       "spread below threshold",
			bid:        0.005,
			ask:        0.008,
			threshold:  0.005,
			wantOK:     true,
			wantSpread: 0.003,
		},
		{
			name:       "spread above threshold - should send notification",
			bid:        0.001,
			ask:        0.010,
			threshold:  0.005,
			wantOK:     false,
			wantSpread: 0.009,
		},
		{
			name:       "zero threshold (disabled check)",
			bid:        0.001,
			ask:        0.010,
			threshold:  0,
			wantOK:     true,
			wantSpread: 0.009,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockNotifier := &MockNotifier{}
			checker := NewSpreadChecker(tt.threshold, mockNotifier, nil)

			// Test the spread calculation and notification logic
			ticker := &TickerInfo{
				BestBid: tt.bid,
				BestAsk: tt.ask,
			}

			// Simulate the CheckSpreadBeforeClose logic
			spread, err := testCheckSpread(checker, ticker, "BTC-24JUL26-64000-P", 1, 100)

			// Use approximate comparison for float
			const epsilon = 1e-9
			if abs(spread-tt.wantSpread) > epsilon {
				t.Errorf("expected spread %f, got %f", tt.wantSpread, spread)
			}

			// For zero threshold, should always be OK
			if tt.threshold == 0 {
				if err != nil {
					t.Errorf("zero threshold should allow all spreads, got error: %v", err)
				}
				// Skip notification checks for zero threshold
				return
			}

			if tt.wantOK && err != nil {
				t.Errorf("expected OK (err=nil), got error: %v", err)
			}

			if !tt.wantOK && err == nil {
				t.Errorf("expected error (spread too wide), got nil")
			}

			// Verify notification was sent when spread too wide
			if !tt.wantOK && tt.threshold > 0 {
				// Wait for async notification
				time.Sleep(100 * time.Millisecond)

				if len(mockNotifier.GetSentMessages()) != 1 {
					t.Errorf("expected 1 notification sent, got %d", len(mockNotifier.GetSentMessages()))
				} else {
					msg := mockNotifier.GetSentMessages()[0]
					if msg.Symbol != "BTC-24JUL26-64000-P" {
						t.Errorf("expected symbol BTC-24JUL26-64000-P, got %s", msg.Symbol)
					}
					if msg.BidPrice != tt.bid {
						t.Errorf("expected bid %f, got %f", tt.bid, msg.BidPrice)
					}
					if msg.AskPrice != tt.ask {
						t.Errorf("expected ask %f, got %f", tt.ask, msg.AskPrice)
					}
					if abs(msg.Spread-tt.wantSpread) > epsilon {
						t.Errorf("expected spread %f, got %f", tt.wantSpread, msg.Spread)
					}
					// Verify message contains expected Chinese text
					if msg.Message == "" {
						t.Error("expected non-empty message")
					}
				}
			}

			// Verify no notification when spread acceptable
			if tt.wantOK && len(mockNotifier.GetSentMessages()) != 0 {
				t.Errorf("expected no notification, but %d sent", len(mockNotifier.GetSentMessages()))
			}
		})
	}
}

// testCheckSpread extracts the core logic for testing without Deribit dependency
func testCheckSpread(checker *SpreadChecker, ticker *TickerInfo, symbol string, userID, userStrategyID uint64) (float64, error) {
	// Calculate spread
	spread := abs(ticker.BestAsk - ticker.BestBid)

	// Zero threshold means check is disabled (always allow)
	if checker.spreadThreshold == 0 {
		return spread, nil
	}

	// Check threshold
	if spread > checker.spreadThreshold {
		// Send notification (async)
		go checker.sendSpreadTooWideNotification(symbol, userID, userStrategyID, ticker, spread)
		return spread, errors.New("spread too wide")
	}

	return spread, nil
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestSpreadChecker_NilNotifier tests that nil notifier doesn't crash
func TestSpreadChecker_NilNotifier(t *testing.T) {
	checker := NewSpreadChecker(0.005, nil, nil)

	ticker := &TickerInfo{
		BestBid: 0.001,
		BestAsk: 0.010,
	}

	// Should not panic
	spread, err := testCheckSpread(checker, ticker, "BTC-24JUL26-64000-P", 1, 100)

	const epsilon = 1e-9
	if abs(spread-0.009) > epsilon {
		t.Errorf("expected spread 0.009, got %f", spread)
	}

	// Should still return error for wide spread
	if err == nil {
		t.Error("expected error for wide spread, got nil")
	}
}

// TestSpreadChecker_NotificationError tests that notification errors are handled gracefully
func TestSpreadChecker_NotificationError(t *testing.T) {
	mockNotifier := &MockNotifier{shouldError: true}
	checker := NewSpreadChecker(0.005, mockNotifier, nil)

	ticker := &TickerInfo{
		BestBid: 0.001,
		BestAsk: 0.010, // spread = 0.009 > 0.005
	}

	// Should still return error for wide spread even if notification fails
	spread, err := testCheckSpread(checker, ticker, "BTC-24JUL26-64000-P", 1, 100)

	if err == nil {
		t.Error("expected error for wide spread, got nil")
	}

	const epsilon = 1e-9
	if abs(spread-0.009) > epsilon {
		t.Errorf("expected spread 0.009, got %f", spread)
	}

	// Wait for async notification attempt
	time.Sleep(100 * time.Millisecond)

	// Notification attempt was made but failed (logged, not returned)
	if len(mockNotifier.GetSentMessages()) != 0 {
		t.Errorf("expected no successful notifications, got %d", len(mockNotifier.GetSentMessages()))
	}
}

// TestSpreadChecker_NotificationRateLimit tests notification frequency limiting.
// When spread exceeds threshold repeatedly, notification should be sent at most once per 10 minutes.
func TestSpreadChecker_NotificationRateLimit(t *testing.T) {
	mockNotifier := &MockNotifier{}
	checker := NewSpreadChecker(0.005, mockNotifier, nil)

	ticker := &TickerInfo{
		BestBid: 0.001,
		BestAsk: 0.010, // spread = 0.009 > 0.005
	}

	// First notification: should be sent
	spread, err := testCheckSpread(checker, ticker, "BTC-24JUL26-64000-P", 1, 100)
	_ = spread // spread not used in this test
	if err == nil {
		t.Error("expected error for wide spread")
	}
	time.Sleep(50 * time.Millisecond) // Wait for async notification
	if len(mockNotifier.GetSentMessages()) != 1 {
		t.Errorf("expected 1 notification, got %d", len(mockNotifier.GetSentMessages()))
	}

	// Reset mock to capture next notifications
	mockNotifier.ClearMessages()

	// Second notification within 10 minutes: should be skipped
	spread, err = testCheckSpread(checker, ticker, "BTC-24JUL26-64000-P", 1, 100)
	_ = spread
	if err == nil {
		t.Error("expected error for wide spread")
	}
	time.Sleep(50 * time.Millisecond)
	if len(mockNotifier.GetSentMessages()) != 0 {
		t.Errorf("expected 0 notification (rate limited), got %d", len(mockNotifier.GetSentMessages()))
	}

	// Third notification for different symbol: should be sent (independent tracking)
	mockNotifier.ClearMessages()
	spread, err = testCheckSpread(checker, ticker, "ETH-25SEP26-2000-C", 1, 200)
	_ = spread
	if err == nil {
		t.Error("expected error for wide spread")
	}
	time.Sleep(50 * time.Millisecond)
	if len(mockNotifier.GetSentMessages()) != 1 {
		t.Errorf("expected 1 notification for different symbol, got %d", len(mockNotifier.GetSentMessages()))
	}
}

// TestSpreadChecker_ROILookup verifies that ROI from roiLookup is attached to the notification.
func TestSpreadChecker_ROILookup(t *testing.T) {
	mockNotifier := &MockNotifier{}
	roiLookup := func(userStrategyID uint64) float64 {
		if userStrategyID == 100 {
			return 0.1234 // 12.34%
		}
		return 0
	}
	checker := NewSpreadChecker(0.005, mockNotifier, roiLookup)

	ticker := &TickerInfo{BestBid: 0.001, BestAsk: 0.010} // spread 0.009 > 0.005

	_, err := testCheckSpread(checker, ticker, "BTC-24JUL26-64000-P", 1, 100)
	if err == nil {
		t.Fatal("expected error for wide spread")
	}
	time.Sleep(50 * time.Millisecond)

	msgs := mockNotifier.GetSentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(msgs))
	}
	if abs(msgs[0].ROI-0.1234) > 1e-9 {
		t.Errorf("expected ROI 0.1234, got %f", msgs[0].ROI)
	}
}

// TestSpreadChecker_NilROILookup verifies that nil roiLookup leaves ROI as 0 (no panic).
func TestSpreadChecker_NilROILookup(t *testing.T) {
	mockNotifier := &MockNotifier{}
	checker := NewSpreadChecker(0.005, mockNotifier, nil)

	ticker := &TickerInfo{BestBid: 0.001, BestAsk: 0.010}

	_, err := testCheckSpread(checker, ticker, "BTC-24JUL26-64000-P", 1, 100)
	if err == nil {
		t.Fatal("expected error for wide spread")
	}
	time.Sleep(50 * time.Millisecond)

	msgs := mockNotifier.GetSentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(msgs))
	}
	if msgs[0].ROI != 0 {
		t.Errorf("expected ROI 0 when lookup nil, got %f", msgs[0].ROI)
	}
}
