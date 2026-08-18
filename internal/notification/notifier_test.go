package notification

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookRouter_RoutesToCorrectChannel(t *testing.T) {
	var openReceived, closeReceived map[string]interface{}
	openServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&openReceived)
		w.WriteHeader(200)
	}))
	defer openServer.Close()
	closeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&closeReceived)
		w.WriteHeader(200)
	}))
	defer closeServer.Close()

	router := NewWebhookRouter(openServer.URL, closeServer.URL, "")

	router.SendOpenOrder(&OpenOrderMessage{UserName: "follow_prod", EventName: "FutureOrder", Symbol: "ETHUSDT", Side: "开多挂单"})
	router.SendCloseOrder(&CloseOrderMessage{UserName: "machineLightGbm", EventName: "新风控下单", Symbol: "ETHUSDT", Side: "平空挂单", Profit: 5.0})

	openMd := openReceived["markdown"].(map[string]interface{})["content"].(string)
	if !strings.Contains(openMd, "follow_prod(FutureOrder)") {
		t.Fatalf("expected user/event title in open notification, got %s", openMd)
	}

	closeMd := closeReceived["markdown"].(map[string]interface{})["content"].(string)
	if !strings.Contains(closeMd, "machineLightGbm(新风控下单)") {
		t.Fatalf("expected user/event title in close notification, got %s", closeMd)
	}
}

func TestWebhookRouter_DisabledChannel_Skips(t *testing.T) {
	router := NewWebhookRouter("", "", "")
	if err := router.SendOpenOrder(&OpenOrderMessage{}); err != nil {
		t.Error("should not error when disabled")
	}
}

func TestWebhookRouter_ReturnsErrorWhenWeComErrcodeNonZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook"}`))
	}))
	defer server.Close()

	router := NewWebhookRouter(server.URL, "", "")
	err := router.SendOpenOrder(&OpenOrderMessage{Symbol: "ETHUSDT", Side: "开多挂单"})
	if err == nil {
		t.Fatal("expected error for non-zero WeCom errcode")
	}
	if !strings.Contains(err.Error(), "93000") || !strings.Contains(err.Error(), "invalid webhook") {
		t.Fatalf("expected errcode and errmsg in error, got %v", err)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func TestWebhookRouter_SendDeribitPositionNotification_UsesTestChannel(t *testing.T) {
	var testReceived map[string]interface{}
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&testReceived)
		w.WriteHeader(200)
	}))
	defer testServer.Close()

	router := NewWebhookRouter("", "", testServer.URL)

	msg := &DeribitPositionMessage{
		UserName: "monthopt",
		Symbol:   "BTC-31JUL26-66000-P",
		Action:   "开卖仓挂单成功",
		Quantity: 2.0,
		Price:    0.02,
	}
	if err := router.SendDeribitPositionNotification(msg); err != nil {
		t.Fatalf("SendDeribitPositionNotification failed: %v", err)
	}

	md := testReceived["markdown"].(map[string]interface{})["content"].(string)
	if !strings.Contains(md, "DeribitPositions(monthopt)") {
		t.Fatalf("expected DeribitPositions(monthopt) prefix, got %s", md)
	}
	if !strings.Contains(md, "BTC-31JUL26-66000-P") {
		t.Fatalf("expected symbol in notification, got %s", md)
	}
	if !strings.Contains(md, "开卖仓挂单成功") {
		t.Fatalf("expected action in notification, got %s", md)
	}
}

func TestBuildDeribitPositionMarkdown_FullFormat(t *testing.T) {
	tests := []struct {
		name     string
		msg      DeribitPositionMessage
		contains []string
		missing  []string
	}{
		{
			name: "open order success",
			msg: DeribitPositionMessage{
				UserName: "monthopt", Symbol: "BTC-31JUL26-66000-P",
				Action: "开卖仓挂单成功", Quantity: 2.0, Price: 0.02,
			},
			contains: []string{"DeribitPositions(monthopt)", "BTC-31JUL26-66000-P", "开卖仓挂单成功", "数量：2.0000", "价格: 0.0200"},
			missing:  []string{"未成交", "ROI", "止盈止损"},
		},
		{
			name: "filled with partial fill",
			msg: DeribitPositionMessage{
				UserName: "monthopt", Symbol: "ETH-26JUN26-5000-C",
				Action: "减买仓部分成交成功", Quantity: 59.0, UnfilledQty: 21.0, Price: 0.0013,
			},
			contains: []string{"减买仓部分成交成功", "未成交数量：21.0000"},
			missing:  []string{"ROI", "止盈止损"},
		},
		{
			name: "risk control with ROI",
			msg: DeribitPositionMessage{
				UserName: "monthopt", Symbol: "BTC-31JUL26-63000-P",
				Action: "触发止盈止损，减卖仓成功", Quantity: 2.0, Price: 0.0075,
				ROI: 0.582, MaxROI: 0.6277, IsRiskCtrl: true,
			},
			contains: []string{"触发止盈止损", "ROI: 58.20%", "Max ROI: 62.77%"},
		},
		{
			name: "no ROI when zero",
			msg: DeribitPositionMessage{
				UserName: "monthopt", Symbol: "BTC-25DEC26-85000-C",
				Action: "开买仓成功", Quantity: 4.0, Price: 0.022,
			},
			missing: []string{"ROI"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildDeribitPositionMarkdown(&tt.msg)
			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("expected %q in result, got: %s", s, result)
				}
			}
			for _, s := range tt.missing {
				if strings.Contains(result, s) {
					t.Errorf("did not expect %q in result, got: %s", s, result)
				}
			}
		})
	}
}
