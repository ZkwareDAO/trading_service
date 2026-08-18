package notification

import (
	"strings"
	"testing"
)

func TestBuildManualCloseMarkdown_ROIDisplay(t *testing.T) {
	tests := []struct {
		name      string
		roi       float64
		wantShown bool
	}{
		{"roi nonzero shown", 0.1234, true},
		{"roi negative shown", -0.25, true},
		{"roi zero hidden", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &ManualCloseMessage{
				Symbol:   "BTC-28AUG26-67000-C",
				BidPrice: 0.0515,
				AskPrice: 0.0595,
				Spread:   0.008,
				Message:  "买一卖一差距悬殊（0.008000），需要手动操作",
				ROI:      tt.roi,
			}
			md := buildManualCloseMarkdown(msg)
			contains := strings.Contains(md, "当前roi")
			if contains != tt.wantShown {
				t.Errorf("roi=%v: contains 当前roi=%v, want %v\nmarkdown:\n%s", tt.roi, contains, tt.wantShown, md)
			}
		})
	}
}

func TestBuildManualCloseMarkdown_ROIValue(t *testing.T) {
	msg := &ManualCloseMessage{
		Symbol:  "ETH-14AUG26-1900-P",
		Spread:  0.008,
		Message: "x",
		ROI:     0.156, // 15.6%
	}
	md := buildManualCloseMarkdown(msg)
	// ROI stored as decimal (0.156 = 15.6%), displayed as percentage
	if !strings.Contains(md, "15.60") {
		t.Errorf("expected 15.60%% in markdown, got:\n%s", md)
	}
}
