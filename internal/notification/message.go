package notification

import (
	"fmt"
	"strings"
)

// OpenOrderMessage represents an open order notification.
type OpenOrderMessage struct {
	UserName     string
	EventName    string
	Symbol       string
	StrategyName string
	Side         string
	Price        float64
	Quantity     float64
}

// CloseOrderMessage represents a close order notification.
type CloseOrderMessage struct {
	UserName         string
	EventName        string
	Symbol           string
	StrategyName     string
	Side             string
	Price            float64
	Quantity         float64
	Profit           float64
	ProfitPercentage float64
}

// TestMessage represents a test notification.
type TestMessage struct {
	Title string
	Body  string
}

// ManualCloseMessage represents a manual close notification.
// Used when bid/ask spread is too wide for automatic closing.
type ManualCloseMessage struct {
	UserName     string
	EventName    string
	Symbol       string
	StrategyName string
	BidPrice     float64
	AskPrice     float64
	Spread       float64 // Absolute spread value
	Message      string
	ROI          float64 // current ROI from UserPosition, 0 if not found (hidden when 0)
}

// DeribitPositionMessage represents a Deribit-specific position notification.
// Sent for all Deribit WS order state changes, regardless of uprunning_order existence.
type DeribitPositionMessage struct {
	UserName    string  // user.Name for "DeribitPositions({userName})"
	Symbol      string  // instrument_name, e.g. "BTC-31JUL26-66000-P"
	Action      string  // e.g. "开卖仓挂单成功", "开买仓成功", "减买仓部分成交成功"
	Quantity    float64 // order amount or filled_amount
	UnfilledQty float64 // remaining unfilled quantity (0 if fully filled)
	Price       float64 // order price or average_price
	ROI         float64 // from UserPosition, 0 if not found
	MaxROI      float64 // from UserPosition.MaxProfitPercentage, 0 if not found
	IsRiskCtrl  bool    // true if triggered by risk control (止盈止损)
}

// DeribitPositionPrefix is the fixed prefix for Deribit position notifications.
const DeribitPositionPrefix = "DeribitPositions"

func buildDeribitPositionMarkdown(msg *DeribitPositionMessage) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("> **%s(%s)**\n", DeribitPositionPrefix, msg.UserName))
	sb.WriteString(fmt.Sprintf("代币：%s，%s，数量：%.4f", msg.Symbol, msg.Action, msg.Quantity))
	if msg.UnfilledQty > 0 {
		sb.WriteString(fmt.Sprintf("，未成交数量：%.4f", msg.UnfilledQty))
	}
	if msg.IsRiskCtrl {
		sb.WriteString(" (触发止盈止损)")
	}
	if msg.ROI != 0 || msg.MaxROI != 0 {
		sb.WriteString(fmt.Sprintf(" (ROI: %.2f%%, Max ROI: %.2f%%)", msg.ROI*100, msg.MaxROI*100))
	}
	sb.WriteString(fmt.Sprintf(" ，价格: %.4f", msg.Price))
	return sb.String()
}

func buildTestMarkdown(msg *TestMessage) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**> %s**\n", msg.Title))
	sb.WriteString(msg.Body)
	return sb.String()
}

func buildManualCloseMarkdown(msg *ManualCloseMessage) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**> %s**\n", notificationTitle("需要手动平仓", msg.UserName, msg.EventName)))
	sb.WriteString(fmt.Sprintf("- 合约代币：%s\n", msg.Symbol))
	sb.WriteString(fmt.Sprintf("- 策略名称：%s\n", msg.StrategyName))
	sb.WriteString(fmt.Sprintf("- 买一价：%.6f\n", msg.BidPrice))
	sb.WriteString(fmt.Sprintf("- 卖一价：%.6f\n", msg.AskPrice))
	sb.WriteString(fmt.Sprintf("- 价差：%.6f\n", msg.Spread))
	if msg.ROI != 0 {
		sb.WriteString(fmt.Sprintf("- 当前roi：%.2f%%\n", msg.ROI*100))
	}
	sb.WriteString(fmt.Sprintf("- 说明：%s\n", msg.Message))
	return sb.String()
}

// GetOpenOrderSide returns the side text for an open order action.
func GetOpenOrderSide(isBuy bool) string {
	if isBuy {
		return "开多挂单"
	}
	return "开空挂单"
}

// GetCloseOrderSide returns the side text for a close order action.
func GetCloseOrderSide(isCloseLong bool) string {
	if isCloseLong {
		return "平多挂单"
	}
	return "平空挂单"
}

func notificationTitle(defaultTitle, userName, eventName string) string {
	if userName != "" && eventName != "" {
		return fmt.Sprintf("%s(%s)", userName, eventName)
	}
	return defaultTitle
}

func buildOpenOrderMarkdown(msg *OpenOrderMessage) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**> %s**\n", notificationTitle("下单", msg.UserName, msg.EventName)))
	sb.WriteString(fmt.Sprintf("- 合约代币：%s\n", msg.Symbol))
	sb.WriteString(fmt.Sprintf("- 策略名称：%s\n", msg.StrategyName))
	sb.WriteString(fmt.Sprintf("- 方向：%s\n", msg.Side))
	sb.WriteString(fmt.Sprintf("- 下单价格：%.6f\n", msg.Price))
	sb.WriteString(fmt.Sprintf("- 数量：%.6f\n", msg.Quantity))
	return sb.String()
}

func buildCloseOrderMarkdown(msg *CloseOrderMessage) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**> %s**\n", notificationTitle("风控平仓", msg.UserName, msg.EventName)))
	sb.WriteString(fmt.Sprintf("- 合约代币：%s\n", msg.Symbol))
	sb.WriteString(fmt.Sprintf("- 策略名称：%s\n", msg.StrategyName))
	sb.WriteString(fmt.Sprintf("- 方向：%s\n", msg.Side))
	sb.WriteString(fmt.Sprintf("- 平仓价格：%.6f\n", msg.Price))
	sb.WriteString(fmt.Sprintf("- 数量：%.6f\n", msg.Quantity))
	sb.WriteString(fmt.Sprintf("- 盈利值：%.6f\n", msg.Profit))
	sb.WriteString(fmt.Sprintf("- 盈利百分比：%.4f%%\n", msg.ProfitPercentage*100))
	return sb.String()
}
