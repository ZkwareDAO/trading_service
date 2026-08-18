package order

import "time"

// PosType represents the position type.
type PosType int

const (
	PosTypeSpot    PosType = 1 // Spot
	PosTypeFutures PosType = 2 // Futures
	PosTypeOptions PosType = 3 // Options
)

const (
	RelationTypeUserOrders          = "user_orders"
	RelationTypeRiskControlStrategy = "risk_control_strategy"
)

// Side represents order/position direction.
type Side int

const (
	SideLong  Side = 0 // Long
	SideShort Side = 1 // Short
)

// ============================================
// User (users.csv)
// ============================================

// User represents a trader account.
type User struct {
	ID          uint64    `csv:"id"`
	Name        string    `csv:"name"`
	Exchange    string    `csv:"exchange"`
	APIKey      string    `csv:"api_key"`
	APISecret   string    `csv:"api_secret"`
	APIPassword string    `csv:"api_password"`
	CreatedAt   time.Time `csv:"created_at"`
	UpdatedAt   time.Time `csv:"updated_at"`
}

// ============================================
// Strategy (strategies.csv)
// ============================================

// Strategy represents a trading strategy definition.
type Strategy struct {
	ID           uint64    `csv:"id"`
	Name         string    `csv:"name"`
	StrategyType string    `csv:"strategy_type"`
	ModelName    string    `csv:"model_name"`
	Description  string    `csv:"description"`
	Params       string    `csv:"params"`
	CreatedAt    time.Time `csv:"created_at"`
	UpdatedAt    time.Time `csv:"updated_at"`
}

// ============================================
// StrategyAsset (strategy_assets.csv)
// ============================================

// StrategyAsset links a strategy to a tradable asset.
type StrategyAsset struct {
	ID         uint64    `csv:"id"`
	Name       string    `csv:"name"`
	Asset      string    `csv:"asset"`
	StrategyID uint64    `csv:"strategy_id"`
	PosType    PosType   `csv:"pos_type"`
	Sort       int       `csv:"sort"`
	CreatedAt  time.Time `csv:"created_at"`
	UpdatedAt  time.Time `csv:"updated_at"`
}

// ============================================
// UserStrategy (user_strategies.csv)
// ============================================

// RiskStrategyType constants.
const (
	RiskStrategyTypeTraditional = "traditional"
	RiskStrategyTypeCtaIntraday = "cta_intraday"
	RiskStrategyTypeSignalClose = "signal_close"
)

// UserStrategy represents a user's strategy assignment.
type UserStrategy struct {
	ID               uint64    `csv:"id"`
	UserID           uint64    `csv:"user_id"`
	Name             string    `csv:"name"`
	Exchange         string    `csv:"exchange"`
	ValidBefore      time.Time `csv:"valid_before"`
	Cash             float64   `csv:"cash"`
	Parts            int       `csv:"parts"`
	Status           int       `csv:"status"`
	StrategyID       uint64    `csv:"strategy_id"`
	RiskStrategyType string    `csv:"risk_strategy_type"`
	OrdersNum        int       `csv:"orders_num"`
	CreatedAt        time.Time `csv:"created_at"`
	UpdatedAt        time.Time `csv:"updated_at"`
}

// ============================================
// UserOrder (user_orders.csv)
// ============================================

// UserOrder represents a user's order request.
type UserOrder struct {
	ID             uint64     `csv:"id"`
	UserID         uint64     `csv:"user_id"`
	UserStrategyID uint64     `csv:"user_strategy_id"`
	PosType        PosType    `csv:"pos_type"`
	Exchange       string     `csv:"exchange"`
	ValidBefore    time.Time  `csv:"valid_before"`
	BaseAsset      string     `csv:"base_asset"`
	QuoteAsset     string     `csv:"quote_asset"`
	Quantity       float64    `csv:"quantity"`
	Cash           float64    `csv:"cash"`
	TriggerPrice   float64    `csv:"trigger_price"`
	Slippage       float64    `csv:"slippage"`
	Side           Side       `csv:"side"`
	OrderType      int        `csv:"order_type"`
	Status         int        `csv:"status"`
	FinishedAt     *time.Time `csv:"finished_at"`
	CreatedAt      time.Time  `csv:"created_at"`
	UpdatedAt      time.Time  `csv:"updated_at"`
}

// ============================================
// LeverageConfig (leverage_configs.csv)
// ============================================

// LeverageConfig represents user leverage configuration.
type LeverageConfig struct {
	ID        uint64    `csv:"id"`
	UserID    uint64    `csv:"user_id"`
	Asset     string    `csv:"asset"`
	Quote     string    `csv:"quote"`
	Leverage  int       `csv:"leverage"`
	Exchange  string    `csv:"exchange"`
	Status    int       `csv:"status"`
	PosType   PosType   `csv:"pos_type"`
	CreatedAt time.Time `csv:"created_at"`
	UpdatedAt time.Time `csv:"updated_at"`
}

// ============================================
// RiskControlStrategy (risk_control_strategy.csv)
// ============================================

// RiskControlStrategy represents risk management rules.
type RiskControlStrategy struct {
	ID             uint64     `csv:"id"`
	UserID         uint64     `csv:"user_id"`
	Name           string     `csv:"name"`
	Symbol         string     `csv:"symbol"`
	RiskType       string     `csv:"risk_type"`
	OrderType      int        `csv:"order_type"`
	Threshold      float64    `csv:"threshold"`
	Price          float64    `csv:"price"`
	DynamicFallPct float64    `csv:"dynamic_fall_percent"`
	QuantityPct    float64    `csv:"quantity_percent"`
	Quantity       float64    `csv:"quantity"`
	Status         int        `csv:"status"`
	Sort           int        `csv:"sort"`
	PosType        PosType    `csv:"pos_type"`
	Side           Side       `csv:"side"`
	UserStrategyID uint64     `csv:"user_strategy_id"`
	ExecuteTime    *time.Time `csv:"execute_time"`
	CreatedAt      time.Time  `csv:"created_at"`
	UpdatedAt      time.Time  `csv:"updated_at"`
}

// ============================================
// UprunningOrder (uprunning_orders.csv) - owned by Exchange Service
// ============================================

// UprunningOrder represents a running order on the exchange.
type UprunningOrder struct {
	ID                  uint64     `csv:"id"`
	UserID              uint64     `csv:"user_id"`
	RelationID          uint64     `csv:"relation_id"`
	RelationType        string     `csv:"relation_type"`
	RiskCtrlStratID     uint64     `csv:"risk_control_strategy_id"`
	UserOrderPositionID uint64     `csv:"user_order_position_id"`
	UserPositionID      uint64     `csv:"user_position_id"`
	Exchange            string     `csv:"exchange"`
	Symbol              string     `csv:"symbol"`
	PosType             PosType    `csv:"pos_type"`
	ExchangeOrderID     uint64     `csv:"exchange_order_id"`
	ExchangeOrderStatus string     `csv:"exchange_order_status"`
	ExchangeOrderPrice  float64    `csv:"exchange_order_price"`
	ExchangeOrderQty    float64    `csv:"exchange_order_quantity"`
	ExchangeUpdateTime  *time.Time `csv:"exchange_update_time"`
	Side                Side       `csv:"side"`
	CreatedAt           time.Time  `csv:"created_at"`
	UpdatedAt           time.Time  `csv:"updated_at"`
}

// ============================================
// UserOrderPosition (user_order_positions.csv) - owned by Exchange Service
// ============================================

// UserOrderPosition represents a position at the order level.
type UserOrderPosition struct {
	ID               uint64     `csv:"id" json:"id"`
	UserID           uint64     `csv:"user_id" json:"user_id"`
	UprunningOrderID uint64     `csv:"uprunning_order_id" json:"uprunning_order_id"`
	UserOrderID      uint64     `csv:"user_order_id" json:"user_order_id"`
	UserStrategyID   uint64     `csv:"user_strategy_id" json:"user_strategy_id"`
	RiskCtrlStratID  uint64     `csv:"risk_control_strategy_id" json:"risk_control_strategy_id"`
	Exchange         string     `csv:"exchange" json:"exchange"`
	PosType          PosType    `csv:"pos_type" json:"pos_type"`
	Asset            string     `csv:"asset" json:"asset"`
	CurrentPrice     float64    `csv:"current_price" json:"current_price"`
	Quantity         float64    `csv:"quantity" json:"quantity"`
	PosValue         float64    `csv:"pos_value" json:"pos_value"`
	Leverage         int        `csv:"leverage" json:"leverage"`
	Deleted          int        `csv:"deleted" json:"deleted"`
	InitMargin       float64    `csv:"init_margin" json:"init_margin"`
	PosPrice         float64    `csv:"pos_price" json:"pos_price"`
	PnLValue         float64    `csv:"pnl_value" json:"pnl_value"`
	Side             Side       `csv:"side" json:"side"`
	CloseTime        *time.Time `csv:"close_time" json:"close_time"`
	CreatedAt        time.Time  `csv:"created_at" json:"created_at"`
	UpdatedAt        time.Time  `csv:"updated_at" json:"updated_at"`
}

// ============================================
// UserPosition (user_positions.csv) - aggregate position
// ============================================

// UserPosition represents an aggregate position at strategy level.
type UserPosition struct {
	ID                         uint64     `csv:"id" json:"id"`
	UserID                     uint64     `csv:"user_id" json:"user_id"`
	UserStrategyID             uint64     `csv:"user_strategy_id" json:"user_strategy_id"`
	Exchange                   string     `csv:"exchange" json:"exchange"`
	PosType                    PosType    `csv:"pos_type" json:"pos_type"`
	CurrentPrice               float64    `csv:"current_price" json:"current_price"`
	Quantity                   float64    `csv:"quantity" json:"quantity"`
	LatestMarketCapitalization float64    `csv:"latest_market_capitalization" json:"latest_market_capitalization"`
	ROI                        float64    `csv:"roi" json:"roi"`
	PnL                        float64    `csv:"pnl" json:"pnl"`
	WinRate                    float64    `csv:"win_rate" json:"win_rate"`
	MaximumDrawdown            float64    `csv:"maximum_drawdown" json:"maximum_drawdown"`
	TotalMargin                float64    `csv:"total_margin" json:"total_margin"`
	MaxProfitPercentage        float64    `csv:"max_profit_percentage" json:"max_profit_percentage"`
	MaxLossPercentage          float64    `csv:"max_loss_percentage" json:"max_loss_percentage"`
	OpenTrades                 int        `csv:"open_trades" json:"open_trades"`
	ClosedTrades               int        `csv:"closed_trades" json:"closed_trades"`
	ProfitTrades               int        `csv:"profit_trades" json:"profit_trades"`
	LossTrades                 int        `csv:"loss_trades" json:"loss_trades"`
	Deleted                    int        `csv:"deleted" json:"deleted"`
	CloseTime                  *time.Time `csv:"close_time" json:"close_time"`
	CreatedAt                  time.Time  `csv:"created_at" json:"created_at"`
	UpdatedAt                  time.Time  `csv:"updated_at" json:"updated_at"`
	RiskCtrlStratID            uint64     `csv:"risk_control_strategy_id" json:"risk_control_strategy_id"`
}

// ============================================
// ExchangeSymbolFilter (exchange_symbol_filters.csv)
// ============================================

// ExchangeSymbolFilter represents exchange trading rules.
type ExchangeSymbolFilter struct {
	ID          uint    `csv:"id"`
	Exchange    string  `csv:"exchange"`
	PosType     PosType `csv:"pos_type"`
	Symbol      string  `csv:"symbol"`
	FilterType  string  `csv:"filter_type"`
	MinPrice    float64 `csv:"min_price"`
	MaxPrice    float64 `csv:"max_price"`
	TickSize    float64 `csv:"tick_size"`
	MinQty      float64 `csv:"min_qty"`
	MaxQty      float64 `csv:"max_qty"`
	StepSize    float64 `csv:"step_size"`
	MinNotional float64 `csv:"min_notional"`
}
