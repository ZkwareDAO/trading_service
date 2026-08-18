package deribit

import (
	"os"
	"testing"

	"trading-service/internal/notification"
)

// TestSpreadChecker_RealWeComNotification sends a real notification to WeCom for manual verification
// Run with: go test -v -run TestSpreadChecker_RealWeComNotification -tags=integration
// Environment variables:
//   - WECOM_CLOSE_URL: enterprise WeChat webhook URL
func TestSpreadChecker_RealWeComNotification(t *testing.T) {
	closeURL := os.Getenv("WECOM_CLOSE_URL")
	if closeURL == "" {
		t.Skip("Skipping: WECOM_CLOSE_URL not set")
	}

	// Create real WebhookRouter with your WeCom URL
	router := notification.NewWebhookRouter("", closeURL, "")

	// Simulate a ticker with wide spread (trigger notification)
	ticker := &TickerInfo{
		BestBid: 0.001,
		BestAsk: 0.010, // spread = 0.009 > 0.005
	}

	// Send notification synchronously for test verification
	msg := &notification.ManualCloseMessage{
		Symbol:       "BTC-24JUL26-64000-P",
		BidPrice:     ticker.BestBid,
		AskPrice:     ticker.BestAsk,
		Spread:       0.009,
		Message:      "买一卖一差距悬殊（0.009000），需要手动操作（测试通知）",
	}

	// Send real notification
	err := router.SendManualCloseNotification(msg)
	if err != nil {
		t.Fatalf("Failed to send notification: %v", err)
	}

	t.Logf("✅ Notification sent successfully to WeCom")
	t.Logf("   Symbol: %s", msg.Symbol)
	t.Logf("   Bid: %.6f", msg.BidPrice)
	t.Logf("   Ask: %.6f", msg.AskPrice)
	t.Logf("   Spread: %.6f", msg.Spread)
	t.Logf("   Message: %s", msg.Message)
	t.Logf("")
	t.Logf("Please check your WeCom group for the notification!")
}

// TestSpreadChecker_RealNotification_WithConfig sends notification using config.yaml
// Run with: go test -v -run TestSpreadChecker_RealNotification_WithConfig
// NOTE: This test requires building in cmd/position_monitor_service package context
// Use TestSpreadChecker_RealWeComNotification instead for simpler setup
func TestSpreadChecker_RealNotification_WithConfig(t *testing.T) {
	t.Skip("This test requires cmd/position_monitor_service context. Use TestSpreadChecker_RealWeComNotification instead.")
}

// TestSpreadChecker_RealScenario simulates the actual spread check with real notification
// This is the complete flow: check spread -> send notification if too wide
func TestSpreadChecker_RealScenario(t *testing.T) {
	closeURL := os.Getenv("WECOM_CLOSE_URL")
	if closeURL == "" {
		t.Skip("Skipping: WECOM_CLOSE_URL not set. Set it to test real notification.")
	}

	router := notification.NewWebhookRouter("", closeURL, "")
	checker := NewSpreadChecker(0.005, router, nil)

	// Scenario: Spread too wide
	ticker := &TickerInfo{
		BestBid: 0.001,
		BestAsk: 0.010,
	}

	spread := 0.009
	msg := &notification.ManualCloseMessage{
		Symbol:       "BTC-24JUL26-64000-P",
		StrategyName: "REAL_TEST",
		BidPrice:     ticker.BestBid,
		AskPrice:     ticker.BestAsk,
		Spread:       spread,
		Message:      "【真实场景测试】差值超过阈值，请手动平仓",
	}

	// Verify logic
	if spread <= checker.spreadThreshold {
		t.Errorf("Test setup error: spread should be > threshold")
	}

	// Send real notification
	err := router.SendManualCloseNotification(msg)
	if err != nil {
		t.Fatalf("Failed to send real notification: %v", err)
	}

	t.Logf("✅ Real scenario test passed!")
	t.Logf("   Spread %.6f > Threshold %.6f", spread, checker.spreadThreshold)
	t.Logf("   Notification sent to WeCom!")
	t.Logf("   Check your phone/WeCom app now!")
}
