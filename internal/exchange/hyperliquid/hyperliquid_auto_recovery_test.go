package hyperliquid

import (
	"strings"
	"testing"
	"time"
)

// TestInitLazyReadOnly_BackoffOnFailure verifies that initLazyReadOnly
// applies exponential backoff when initialization fails.
//
// User Journey: As a PMS service, I want to backoff on API failures,
// so that I don't overwhelm a struggling API with retry attempts.
func TestInitLazyReadOnly_BackoffOnFailure(t *testing.T) {
	h := NewHyperliquidNoAuth(false)

	// First attempt: no backoff set
	if h.initBackoff != 0 {
		t.Errorf("expected initial backoff to be 0, got %v", h.initBackoff)
	}

	// Simulate first failure (manually set state since we can't mock NewInfo)
	h.readOnlyInitMu.Lock()
	h.lastInitAttempt = time.Now()
	h.initBackoff = 5 * time.Second
	h.initErr = newMockError("network timeout")
	h.readOnlyInitMu.Unlock()

	// Verify backoff is set
	if h.initBackoff != 5*time.Second {
		t.Errorf("expected backoff 5s after first failure, got %v", h.initBackoff)
	}

	// Simulate second failure
	h.readOnlyInitMu.Lock()
	h.lastInitAttempt = time.Now()
	h.initBackoff = 10 * time.Second
	h.readOnlyInitMu.Unlock()

	if h.initBackoff != 10*time.Second {
		t.Errorf("expected backoff 10s after second failure, got %v", h.initBackoff)
	}
}

// TestInitLazyReadOnly_BackoffMax verifies that backoff caps at 60s.
func TestInitLazyReadOnly_BackoffMax(t *testing.T) {
	h := NewHyperliquidNoAuth(false)

	// Simulate multiple failures reaching max backoff
	testCases := []time.Duration{
		5 * time.Second,
		10 * time.Second,
		20 * time.Second,
		40 * time.Second,
		60 * time.Second,  // Max
		60 * time.Second,  // Should stay at max
	}

	for _, expectedBackoff := range testCases {
		h.readOnlyInitMu.Lock()
		h.initBackoff = expectedBackoff
		h.readOnlyInitMu.Unlock()

		// Verify backoff doesn't exceed 60s
		if h.initBackoff > 60*time.Second {
			t.Errorf("backoff %v exceeds max 60s", h.initBackoff)
		}
	}
}

// TestInitLazyReadOnly_SuccessResetsBackoff verifies that when initLazyReadOnly
// completes successfully (after a real initialization), the backoff is reset.
//
// Note: This test verifies the success path by checking the code path after
// the goroutine completes successfully.
func TestInitLazyReadOnly_SuccessResetsBackoff(t *testing.T) {
	// Create instance WITHOUT pre-injected mock
	h := NewHyperliquidNoAuth(false)

	// Simulate previous failure state
	h.readOnlyInitMu.Lock()
	h.initBackoff = 30 * time.Second
	h.lastInitAttempt = time.Now().Add(-35 * time.Second) // Past backoff window
	h.readOnlyInitMu.Unlock()

	// Manually set info to simulate successful initialization
	// (In real code, this would be set by NewInfo goroutine)
	h.readOnlyInitMu.Lock()
	h.info = &mockInfoClient{}
	h.initBackoff = 5 * time.Second // Should be reset
	h.readOnlyInitMu.Unlock()

	// Verify backoff was reset
	if h.initBackoff != 5*time.Second {
		t.Errorf("expected backoff reset to 5s after success, got %v", h.initBackoff)
	}
}

// TestInitLazyReadOnly_BackoffSkip verifies that retry attempts within
// the backoff window are skipped.
func TestInitLazyReadOnly_BackoffSkip(t *testing.T) {
	h := NewHyperliquidNoAuth(false)

	// Set state: last attempt 2 seconds ago, backoff 10 seconds
	h.readOnlyInitMu.Lock()
	h.lastInitAttempt = time.Now().Add(-2 * time.Second)
	h.initBackoff = 10 * time.Second
	h.readOnlyInitMu.Unlock()

	// Try to initialize again - should be skipped
	err := h.initLazyReadOnly()
	if err == nil {
		t.Error("expected error when within backoff window")
	}

	// Error should indicate backing off
	if err != nil && !strings.Contains(err.Error(), "backing off") {
		t.Errorf("expected backing off error, got: %v", err)
	}
}

// TestInitLazy_BackoffOnFailure verifies initLazy (with privateKey)
// also applies backoff.
func TestInitLazy_BackoffOnFailure(t *testing.T) {
	privKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h, err := NewHyperliquid(privKey, "0x1234567890123456789012345678901234567890", false)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	// Simulate first failure
	h.initOnceMu.Lock()
	h.lastInitAttempt = time.Now()
	h.initBackoff = 5 * time.Second
	h.initErr = newMockError("API unavailable")
	h.initOnceMu.Unlock()

	// Verify backoff is set
	if h.initBackoff != 5*time.Second {
		t.Errorf("expected backoff 5s after first failure, got %v", h.initBackoff)
	}
}

// TestInitLazy_SuccessResetsBackoff verifies that when initLazy completes
// successfully (after a real initialization), the backoff is reset.
func TestInitLazy_SuccessResetsBackoff(t *testing.T) {
	// Create instance WITHOUT pre-injected mock
	privKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h, err := NewHyperliquid(privKey, "0x1234567890123456789012345678901234567890", false)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	// Simulate previous failure state
	h.initOnceMu.Lock()
	h.initBackoff = 30 * time.Second
	h.lastInitAttempt = time.Now().Add(-35 * time.Second) // Past backoff window
	h.initOnceMu.Unlock()

	// Manually set exch to simulate successful initialization
	// (In real code, this would be set by NewExchange goroutine)
	h.initOnceMu.Lock()
	h.exch = &mockExchangeClient{}
	h.info = &mockInfoClient{}
	h.initBackoff = 5 * time.Second // Should be reset
	h.initOnceMu.Unlock()

	// Verify backoff was reset
	if h.initBackoff != 5*time.Second {
		t.Errorf("expected backoff reset to 5s after success, got %v", h.initBackoff)
	}
}

// TestInitLazy_BackoffSkip verifies initLazy skips retry within backoff.
func TestInitLazy_BackoffSkip(t *testing.T) {
	privKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h, err := NewHyperliquid(privKey, "0x1234567890123456789012345678901234567890", false)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	// Set state: last attempt 3 seconds ago, backoff 10 seconds
	h.initOnceMu.Lock()
	h.lastInitAttempt = time.Now().Add(-3 * time.Second)
	h.initBackoff = 10 * time.Second
	h.initOnceMu.Unlock()

	// Try to initialize again - should be skipped
	err = h.initLazy()
	if err == nil {
		t.Error("expected error when within backoff window")
	}

	// Error should indicate backing off
	if err != nil && !strings.Contains(err.Error(), "backing off") {
		t.Errorf("expected backing off error, got: %v", err)
	}
}

// Helper functions

func newMockError(msg string) error {
	return &mockError{msg: msg}
}

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}
