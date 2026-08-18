package signal

import (
	"fmt"
	"strings"
	"time"

	"trading-service/internal/order"
)

// CustomTime accepts the timestamp formats used by upstream strategy signals.
type CustomTime struct {
	time.Time
}

func (ct *CustomTime) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" || s == `""` || s == "" {
		return nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}

	t, err := parseSignalTime(s)
	if err != nil {
		return err
	}
	ct.Time = t
	return nil
}

func parseSignalTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05-07:00",
		time.RFC3339,
		time.RFC3339Nano,
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// Action is the upstream signal operation type.
type Action string

const (
	ActionBuy          Action = "buy"
	ActionSell         Action = "sell"
	ActionBuyClose     Action = "buy_close"
	ActionSellClose    Action = "sell_close"
	ActionReverseLong  Action = "reverse_long"
	ActionReverseShort Action = "reverse_short"
)

func (a Action) IsOpenAction() bool {
	return a == ActionBuy || a == ActionSell
}

func (a Action) IsCloseAction() bool {
	return a == ActionBuyClose || a == ActionSellClose
}

func (a Action) IsReverseAction() bool {
	return a == ActionReverseLong || a == ActionReverseShort
}

func (a Action) GetOpenSide() (order.Side, bool) {
	switch a {
	case ActionBuy, ActionReverseLong:
		return order.SideLong, true
	case ActionSell, ActionReverseShort:
		return order.SideShort, true
	default:
		return 0, false
	}
}

func (a Action) GetCloseSide() (order.Side, bool) {
	switch a {
	case ActionBuyClose, ActionReverseLong:
		return order.SideShort, true
	case ActionSellClose, ActionReverseShort:
		return order.SideLong, true
	default:
		return 0, false
	}
}

// StrategySignal is the nested payload emitted by the strategy signal service.
type StrategySignal struct {
	SignalID         string            `json:"SignalID"`
	SignalTimestamp  CustomTime        `json:"SignalTimestamp"`
	Symbol           string            `json:"symbol"`
	UserID           uint64            `json:"user_id"`
	PosType          order.PosType     `json:"pos_type"`
	StrategyType     string            `json:"strategy_type"`
	RiskStrategyType string            `json:"risk_strategy_type"`
	Strategy         StrategyConfig    `json:"strategy"`
	Signal           SignalOrderConfig `json:"signal"`
}

type StrategyConfig struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Interval    string                 `json:"internal"`
	Description string                 `json:"description"`
	Params      map[string]interface{} `json:"params"`
	ValidBefore CustomTime             `json:"valid_before"`
	Cash        float64                `json:"cash"`
	Parts       int                    `json:"parts"`
	Leverage    int                    `json:"leverage"`
}

type SignalOrderConfig struct {
	Side         int        `json:"side"`
	Action       Action     `json:"action"`
	Exchange     string     `json:"exchange"`
	ValidBefore  CustomTime `json:"valid_before"`
	Quantity     float64    `json:"quantity"`
	Cash         float64    `json:"cash"`
	TriggerPrice float64    `json:"trigger_price"`
	Slippage     float64    `json:"slippage"`
	OrderType    int        `json:"order_type"`
}

func (s *StrategySignal) NormalizeSymbol() {
	s.Symbol = normalizeBaseAsset(s.Symbol)
}

func (sc StrategyConfig) ExtractStrategyName(symbol, exchange string) string {
	// 根据交易所适配symbol后缀
	adaptedSymbol := strings.ToUpper(strings.TrimSpace(symbol))
	base := normalizeBaseAsset(symbol)

	switch exchange {
	case "binance":
		adaptedSymbol = base + "USDT"
	case "hyperliquid":
		adaptedSymbol = base + "USDC"
	}

	// Agent订单特殊处理：Name="POSITIVE_1D_1", Interval="1D", Version="1"
	// 最终策略名称应该是：POSITIVE_1D_1_BTCUSDT（不重复拼接Interval和Version）
	name := strings.ToUpper(strings.TrimSpace(sc.Name))
	if sc.Interval != "" && sc.Version != "" {
		// 检查Name是否已经包含Interval和Version的格式
		// 例如：Name="POSITIVE_1D_1", Interval="1D", Version="1"
		// 此时Name格式为 {base}_{interval}_{version}，不应重复拼接
		expectedSuffix := fmt.Sprintf("_%s_%s", strings.ToUpper(sc.Interval), strings.ToUpper(sc.Version))
		if strings.HasSuffix(name, expectedSuffix) || strings.Contains(name, expectedSuffix+"_") {
			// Name已包含Interval和Version，只拼接symbol
			result := fmt.Sprintf("%s_%s", name, adaptedSymbol)
			// log.Printf("ExtractStrategyName (agent): name=%s, interval=%s, version=%s, symbol=%s, exchange=%s, adaptedSymbol=%s, result=%s",
			// 	name, sc.Interval, sc.Version, symbol, exchange, adaptedSymbol, result)
			return result
		}
		// 否则完整拼接（原有逻辑）
		return fmt.Sprintf("%s_%s_%s_%s", name, strings.ToUpper(sc.Interval), strings.ToUpper(sc.Version), adaptedSymbol)
	}

	// 没有Interval和Version时，直接拼接symbol
	return fmt.Sprintf("%s_%s", name, adaptedSymbol)
}

func normalizeBaseAsset(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	for _, suffix := range []string{"USDT", "USDC"} {
		if strings.HasSuffix(s, suffix) {
			return strings.TrimSuffix(s, suffix)
		}
	}
	return s
}

// defaultQuoteForExchange returns the default quote currency for an exchange.
func defaultQuoteForExchange(exchange string) string {
	if exchange == "hyperliquid" {
		return "USDC"
	}
	return "USDT"
}

// toExchangeSymbol converts a base asset to exchange symbol (USDT-based).
// Deprecated: use toExchangeSymbolWithExchange for exchange-aware symbols.
func toExchangeSymbol(baseAsset string) string {
	base := normalizeBaseAsset(baseAsset)
	if base == "" {
		return ""
	}
	return base + "USDT"
}

// toExchangeSymbolWithExchange converts a base asset to exchange symbol using the exchange's default quote.
func toExchangeSymbolWithExchange(baseAsset, exchange string) string {
	base := normalizeBaseAsset(baseAsset)
	if base == "" {
		return ""
	}
	return base + defaultQuoteForExchange(exchange)
}
