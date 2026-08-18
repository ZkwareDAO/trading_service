package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Channel identifies the notification destination.
type Channel string

const (
	ChannelOpen  Channel = "open"
	ChannelClose Channel = "close"
	ChannelTest  Channel = "test"
)

// Notifier sends trading event notifications to different channels.
type Notifier interface {
	SendOpenOrder(msg *OpenOrderMessage) error
	SendCloseOrder(msg *CloseOrderMessage) error
	SendTest(msg *TestMessage) error
	SendManualCloseNotification(msg *ManualCloseMessage) error
	SendDeribitPositionNotification(msg *DeribitPositionMessage) error
}

// WebhookRouter routes notifications to different webhook URLs by channel.
type WebhookRouter struct {
	webhooks map[Channel]string
	client   *http.Client
	enabled  bool
}

// NewWebhookRouter creates a router with URLs for each channel.
// Empty URL means that channel is disabled.
func NewWebhookRouter(openURL, closeURL, testURL string) *WebhookRouter {
	enabled := openURL != "" || closeURL != "" || testURL != ""
	return &WebhookRouter{
		webhooks: map[Channel]string{
			ChannelOpen:  openURL,
			ChannelClose: closeURL,
			ChannelTest:  testURL,
		},
		client:  &http.Client{},
		enabled: enabled,
	}
}

// SendOpenOrder sends an open order notification.
func (r *WebhookRouter) SendOpenOrder(msg *OpenOrderMessage) error {
	if !r.enabled {
		return nil
	}
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": buildOpenOrderMarkdown(msg),
		},
	}
	return r.post(ChannelOpen, payload)
}

// SendCloseOrder sends a close order notification.
func (r *WebhookRouter) SendCloseOrder(msg *CloseOrderMessage) error {
	if !r.enabled {
		return nil
	}
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": buildCloseOrderMarkdown(msg),
		},
	}
	return r.post(ChannelClose, payload)
}

// SendTest sends a test notification.
func (r *WebhookRouter) SendTest(msg *TestMessage) error {
	if !r.enabled {
		return nil
	}
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": buildTestMarkdown(msg),
		},
	}
	return r.post(ChannelTest, payload)
}

// SendManualCloseNotification sends a manual close notification.
// Used when Deribit bid/ask spread is too wide for automatic closing.
func (r *WebhookRouter) SendManualCloseNotification(msg *ManualCloseMessage) error {
	if !r.enabled {
		return nil
	}
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": buildManualCloseMarkdown(msg),
		},
	}
	return r.post(ChannelClose, payload)
}

// SendDeribitPositionNotification sends a Deribit-specific position notification.
// Uses ChannelTest (test_url) as the webhook destination.
func (r *WebhookRouter) SendDeribitPositionNotification(msg *DeribitPositionMessage) error {
	if !r.enabled {
		return nil
	}
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": buildDeribitPositionMarkdown(msg),
		},
	}
	return r.post(ChannelTest, payload)
}

func (r *WebhookRouter) post(ch Channel, payload map[string]interface{}) error {
	url, ok := r.webhooks[ch]
	if !ok || url == "" {
		return nil // channel not configured, skip silently
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create notification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read notification response: %w", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("notification webhook returned %d: %s", resp.StatusCode, string(respBody))
	}
	var weComResp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &weComResp); err == nil && weComResp.ErrCode != 0 {
			return fmt.Errorf("notification webhook errcode=%d errmsg=%s", weComResp.ErrCode, weComResp.ErrMsg)
		}
	}
	return nil
}
