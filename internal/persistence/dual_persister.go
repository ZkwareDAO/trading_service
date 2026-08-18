package persistence

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"trading-service/internal/order"
)

// DualPersister manages dual-file append-only persistence.
type DualPersister struct {
	dataDir string
	mu      sync.Mutex
}

// NewDualPersister creates a new persister for the given data directory.
func NewDualPersister(dataDir string) *DualPersister {
	return &DualPersister{dataDir: dataDir}
}

// AppendRow appends a single row to a CSV file.
// Writes header if the file is new or empty.
func (p *DualPersister) AppendRow(tableName string, record interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	filePath := filepath.Join(p.dataDir, tableName)

	// Check if file needs header
	needsHeader := false
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if stat.Size() == 0 {
		needsHeader = true
	}

	writer := csv.NewWriter(f)
	if needsHeader {
		headers := GetCSVHeaders(record)
		if err := writer.Write(headers); err != nil {
			return err
		}
	}

	row, err := EncodeToCSVRow(record)
	if err != nil {
		return err
	}

	if err := writer.Write(row); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}

// ReadAllCSV reads all data rows from a CSV file (skips header).
func (p *DualPersister) ReadAllCSV(tableName string) ([]map[string]string, error) {
	filePath := filepath.Join(p.dataDir, tableName)

	f, err := os.Open(filePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, nil
	}

	headers := records[0]
	data := make([]map[string]string, 0, len(records)-1)

	for i := 1; i < len(records); i++ {
		row := make(map[string]string)
		for j, h := range headers {
			if j < len(records[i]) {
				row[h] = records[i][j]
			}
		}
		data = append(data, row)
	}

	return data, nil
}

// WriteAllCSV writes a full CSV file with headers (used for compact).
func (p *DualPersister) WriteAllCSV(tableName string, rows []interface{}) error {
	filePath := filepath.Join(p.dataDir, tableName)
	return p.writeAllCSVPath(filePath, rows, tableName)
}

// Compact writes the latest state to a temp file and atomically replaces the target.
func (p *DualPersister) Compact(tableName string, latestRows []interface{}) error {
	tmpDir := filepath.Join(p.dataDir, ".compact")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return err
	}

	tmpPath := filepath.Join(tmpDir, tableName+".tmp")
	finalPath := filepath.Join(p.dataDir, tableName)

	// Pass tableName to writeAllCSVPath for header generation
	if err := p.writeAllCSVPath(tmpPath, latestRows, tableName); err != nil {
		return err
	}

	return os.Rename(tmpPath, finalPath)
}

// writeAllCSVPath writes a CSV to an explicit path.
func (p *DualPersister) writeAllCSVPath(filePath string, rows []interface{}, tableName string) error {
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	writer := csv.NewWriter(f)

	// Always write headers, even for empty tables
	if len(rows) > 0 {
		headers := GetCSVHeaders(rows[0])
		if err := writer.Write(headers); err != nil {
			return err
		}
	} else {
		// Use prototype for empty tables to ensure header is written
		var headers []string
		switch tableName {
		case "users.csv":
			headers = GetCSVHeaders(&order.User{})
		case "strategies.csv":
			headers = GetCSVHeaders(&order.Strategy{})
		case "strategy_assets.csv":
			headers = GetCSVHeaders(&order.StrategyAsset{})
		case "user_strategies.csv":
			headers = GetCSVHeaders(&order.UserStrategy{})
		case "user_orders.csv":
			headers = GetCSVHeaders(&order.UserOrder{})
		case "leverage_configs.csv":
			headers = GetCSVHeaders(&order.LeverageConfig{})
		case "exchange_symbol_filters.csv":
			headers = GetCSVHeaders(&order.ExchangeSymbolFilter{})
		case "uprunning_orders.csv":
			headers = GetCSVHeaders(&order.UprunningOrder{})
		case "user_order_positions.csv":
			headers = GetCSVHeaders(&order.UserOrderPosition{})
		case "user_positions.csv":
			headers = GetCSVHeaders(&order.UserPosition{})
		}
		if len(headers) > 0 {
			if err := writer.Write(headers); err != nil {
				return err
			}
		}
	}

	for _, row := range rows {
		csvRow, err := EncodeToCSVRow(row)
		if err != nil {
			continue
		}
		if err := writer.Write(csvRow); err != nil {
			return err
		}
	}

	writer.Flush()
	return writer.Error()
}

// parseTime safely parses an RFC3339 time string.
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}

// parseUserFromRecord creates a User from a CSV record map.
func parseUserFromRecord(rec map[string]string) (*order.User, error) {
	id, _ := strconv.ParseUint(rec["id"], 10, 64)
	createdAt, _ := parseTime(rec["created_at"])
	updatedAt, _ := parseTime(rec["updated_at"])

	return &order.User{
		ID:          id,
		Name:        rec["name"],
		Exchange:    rec["exchange"],
		APIKey:      rec["api_key"],
		APISecret:   rec["api_secret"],
		APIPassword: rec["api_password"],
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

// parseStrategyFromRecord creates a Strategy from a CSV record map.
func parseStrategyFromRecord(rec map[string]string) (*order.Strategy, error) {
	id, _ := strconv.ParseUint(rec["id"], 10, 64)
	createdAt, _ := parseTime(rec["created_at"])
	updatedAt, _ := parseTime(rec["updated_at"])

	return &order.Strategy{
		ID:           id,
		Name:         rec["name"],
		StrategyType: rec["strategy_type"],
		ModelName:    rec["model_name"],
		Description:  rec["description"],
		Params:       rec["params"],
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}, nil
}

// parseStrategyAssetFromRecord creates a StrategyAsset from a CSV record map.
func parseStrategyAssetFromRecord(rec map[string]string) (*order.StrategyAsset, error) {
	id, _ := strconv.ParseUint(rec["id"], 10, 64)
	strategyID, _ := strconv.ParseUint(rec["strategy_id"], 10, 64)
	posType, _ := strconv.Atoi(rec["pos_type"])
	sort, _ := strconv.Atoi(rec["sort"])
	createdAt, _ := parseTime(rec["created_at"])
	updatedAt, _ := parseTime(rec["updated_at"])

	return &order.StrategyAsset{
		ID:         id,
		Name:       rec["name"],
		Asset:      rec["asset"],
		StrategyID: strategyID,
		PosType:    order.PosType(posType),
		Sort:       sort,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}, nil
}

// parseUserStrategyFromRecord creates a UserStrategy from a CSV record map.
func parseUserStrategyFromRecord(rec map[string]string) (*order.UserStrategy, error) {
	id, _ := strconv.ParseUint(rec["id"], 10, 64)
	userID, _ := strconv.ParseUint(rec["user_id"], 10, 64)
	strategyID, _ := strconv.ParseUint(rec["strategy_id"], 10, 64)
	cash, _ := strconv.ParseFloat(rec["cash"], 64)
	parts, _ := strconv.Atoi(rec["parts"])
	status, _ := strconv.Atoi(rec["status"])
	ordersNum, _ := strconv.Atoi(rec["orders_num"])
	validBefore, _ := parseTime(rec["valid_before"])
	createdAt, _ := parseTime(rec["created_at"])
	updatedAt, _ := parseTime(rec["updated_at"])

	return &order.UserStrategy{
		ID:               id,
		UserID:           userID,
		Name:             rec["name"],
		Exchange:         rec["exchange"],
		ValidBefore:      validBefore,
		Cash:             cash,
		Parts:            parts,
		Status:           status,
		StrategyID:       strategyID,
		RiskStrategyType: rec["risk_strategy_type"],
		OrdersNum:        ordersNum,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}, nil
}

// parseUserOrderFromRecord creates a UserOrder from a CSV record map.
func parseUserOrderFromRecord(rec map[string]string) (*order.UserOrder, error) {
	id, _ := strconv.ParseUint(rec["id"], 10, 64)
	userID, _ := strconv.ParseUint(rec["user_id"], 10, 64)
	strategyID, _ := strconv.ParseUint(rec["user_strategy_id"], 10, 64)
	posType, _ := strconv.Atoi(rec["pos_type"])
	cash, _ := strconv.ParseFloat(rec["cash"], 64)
	triggerPrice, _ := strconv.ParseFloat(rec["trigger_price"], 64)
	slippage, _ := strconv.ParseFloat(rec["slippage"], 64)
	quantity, _ := strconv.ParseFloat(rec["quantity"], 64)
	side, _ := strconv.Atoi(rec["side"])
	orderType, _ := strconv.Atoi(rec["order_type"])
	status, _ := strconv.Atoi(rec["status"])
	validBefore, _ := parseTime(rec["valid_before"])
	finishedAt, _ := parseTime(rec["finished_at"])
	createdAt, _ := parseTime(rec["created_at"])
	updatedAt, _ := parseTime(rec["updated_at"])

	o := &order.UserOrder{
		ID:             id,
		UserID:         userID,
		UserStrategyID: strategyID,
		PosType:        order.PosType(posType),
		Exchange:       rec["exchange"],
		ValidBefore:    validBefore,
		BaseAsset:      rec["base_asset"],
		QuoteAsset:     rec["quote_asset"],
		Quantity:       quantity,
		Cash:           cash,
		TriggerPrice:   triggerPrice,
		Slippage:       slippage,
		Side:           order.Side(side),
		OrderType:      orderType,
		Status:         status,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
	if finishedAtStr := rec["finished_at"]; finishedAtStr != "" {
		t := finishedAt
		o.FinishedAt = &t
	}
	return o, nil
}

// parseLeverageConfigFromRecord creates a LeverageConfig from a CSV record map.
func parseLeverageConfigFromRecord(rec map[string]string) (*order.LeverageConfig, error) {
	id, _ := strconv.ParseUint(rec["id"], 10, 64)
	userID, _ := strconv.ParseUint(rec["user_id"], 10, 64)
	leverage, _ := strconv.Atoi(rec["leverage"])
	status, _ := strconv.Atoi(rec["status"])
	posType, _ := strconv.Atoi(rec["pos_type"])
	createdAt, _ := parseTime(rec["created_at"])
	updatedAt, _ := parseTime(rec["updated_at"])

	return &order.LeverageConfig{
		ID:        id,
		UserID:    userID,
		Asset:     rec["asset"],
		Quote:     rec["quote"],
		Leverage:  leverage,
		Exchange:  rec["exchange"],
		Status:    status,
		PosType:   order.PosType(posType),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func parseUserPositionFromRecord(rec map[string]string) (*order.UserPosition, error) {
	id, _ := strconv.ParseUint(rec["id"], 10, 64)
	userID, _ := strconv.ParseUint(rec["user_id"], 10, 64)
	usID, _ := strconv.ParseUint(rec["user_strategy_id"], 10, 64)
	posType, _ := strconv.Atoi(rec["pos_type"])
	curPrice, _ := strconv.ParseFloat(rec["current_price"], 64)
	qty, _ := strconv.ParseFloat(rec["quantity"], 64)
	latestMarketCap, _ := strconv.ParseFloat(rec["latest_market_capitalization"], 64)
	roi, _ := strconv.ParseFloat(rec["roi"], 64)
	pnl, _ := strconv.ParseFloat(rec["pnl"], 64)
	winRate, _ := strconv.ParseFloat(rec["win_rate"], 64)
	maximumDrawdown, _ := strconv.ParseFloat(rec["maximum_drawdown"], 64)
	totalMargin, _ := strconv.ParseFloat(rec["total_margin"], 64)
	maxProfitPercentage, _ := strconv.ParseFloat(rec["max_profit_percentage"], 64)
	maxLossPercentage, _ := strconv.ParseFloat(rec["max_loss_percentage"], 64)
	openTrades, _ := strconv.Atoi(rec["open_trades"])
	closedTrades, _ := strconv.Atoi(rec["closed_trades"])
	profitTrades, _ := strconv.Atoi(rec["profit_trades"])
	lossTrades, _ := strconv.Atoi(rec["loss_trades"])
	deleted, _ := strconv.Atoi(rec["deleted"])
	closeTime, _ := parseTime(rec["close_time"])
	createdAt, _ := parseTime(rec["created_at"])
	updatedAt, _ := parseTime(rec["updated_at"])
	rcID, _ := strconv.ParseUint(rec["risk_control_strategy_id"], 10, 64)
	var closeTimePtr *time.Time
	if rec["close_time"] != "" {
		closeTimePtr = &closeTime
	}
	return &order.UserPosition{
		ID: id, UserID: userID, UserStrategyID: usID, Exchange: rec["exchange"], PosType: order.PosType(posType),
		CurrentPrice: curPrice, Quantity: qty, LatestMarketCapitalization: latestMarketCap,
		ROI: roi, PnL: pnl, WinRate: winRate, MaximumDrawdown: maximumDrawdown,
		TotalMargin: totalMargin, MaxProfitPercentage: maxProfitPercentage, MaxLossPercentage: maxLossPercentage,
		OpenTrades: openTrades, ClosedTrades: closedTrades, ProfitTrades: profitTrades, LossTrades: lossTrades,
		Deleted: deleted, CloseTime: closeTimePtr, CreatedAt: createdAt, UpdatedAt: updatedAt, RiskCtrlStratID: rcID,
	}, nil
}

// parseExchangeSymbolFilterFromRecord creates an ExchangeSymbolFilter from a CSV record map.
func parseExchangeSymbolFilterFromRecord(rec map[string]string) (*order.ExchangeSymbolFilter, error) {
	id, _ := strconv.ParseUint(rec["id"], 10, 64)
	posType, _ := strconv.Atoi(rec["pos_type"])
	minPrice, _ := strconv.ParseFloat(rec["min_price"], 64)
	maxPrice, _ := strconv.ParseFloat(rec["max_price"], 64)
	tickSize, _ := strconv.ParseFloat(rec["tick_size"], 64)
	minQty, _ := strconv.ParseFloat(rec["min_qty"], 64)
	maxQty, _ := strconv.ParseFloat(rec["max_qty"], 64)
	stepSize, _ := strconv.ParseFloat(rec["step_size"], 64)
	minNotional, _ := strconv.ParseFloat(rec["min_notional"], 64)

	return &order.ExchangeSymbolFilter{
		ID:          uint(id),
		Exchange:    rec["exchange"],
		PosType:     order.PosType(posType),
		Symbol:      rec["symbol"],
		FilterType:  rec["filter_type"],
		MinPrice:    minPrice,
		MaxPrice:    maxPrice,
		TickSize:    tickSize,
		MinQty:      minQty,
		MaxQty:      maxQty,
		StepSize:    stepSize,
		MinNotional: minNotional,
	}, nil
}

// parseUprunningOrderFromRecord creates a UprunningOrder from a CSV record map.
func parseUprunningOrderFromRecord(rec map[string]string) (*order.UprunningOrder, error) {
	id, _ := strconv.ParseUint(rec["id"], 10, 64)
	userID, _ := strconv.ParseUint(rec["user_id"], 10, 64)
	relationID, _ := strconv.ParseUint(rec["relation_id"], 10, 64)
	riskCtrlStratID, _ := strconv.ParseUint(rec["risk_control_strategy_id"], 10, 64)
	userOrderPositionID, _ := strconv.ParseUint(rec["user_order_position_id"], 10, 64)
	userPositionID, _ := strconv.ParseUint(rec["user_position_id"], 10, 64)
	posType, _ := strconv.Atoi(rec["pos_type"])
	exchangeOrderID, _ := strconv.ParseUint(rec["exchange_order_id"], 10, 64)
	exchangeOrderPrice, _ := strconv.ParseFloat(rec["exchange_order_price"], 64)
	exchangeOrderQuantity, _ := strconv.ParseFloat(rec["exchange_order_quantity"], 64)
	side, _ := strconv.Atoi(rec["side"])
	exchangeUpdateTime, _ := parseTime(rec["exchange_update_time"])
	createdAt, _ := parseTime(rec["created_at"])
	updatedAt, _ := parseTime(rec["updated_at"])
	var updateTimePtr *time.Time
	if rec["exchange_update_time"] != "" {
		updateTimePtr = &exchangeUpdateTime
	}
	return &order.UprunningOrder{
		ID: id, UserID: userID, RelationID: relationID, RelationType: rec["relation_type"],
		RiskCtrlStratID: riskCtrlStratID, UserOrderPositionID: userOrderPositionID, UserPositionID: userPositionID,
		Exchange: rec["exchange"], Symbol: rec["symbol"], PosType: order.PosType(posType),
		ExchangeOrderID: exchangeOrderID, ExchangeOrderStatus: rec["exchange_order_status"],
		ExchangeOrderPrice: exchangeOrderPrice, ExchangeOrderQty: exchangeOrderQuantity,
		ExchangeUpdateTime: updateTimePtr, Side: order.Side(side),
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

// parseUserOrderPositionFromRecord creates a UserOrderPosition from a CSV record map.
func parseUserOrderPositionFromRecord(rec map[string]string) (*order.UserOrderPosition, error) {
	id, _ := strconv.ParseUint(rec["id"], 10, 64)
	userID, _ := strconv.ParseUint(rec["user_id"], 10, 64)
	uoID, _ := strconv.ParseUint(rec["uprunning_order_id"], 10, 64)
	uoOrderID, _ := strconv.ParseUint(rec["user_order_id"], 10, 64)
	usID, _ := strconv.ParseUint(rec["user_strategy_id"], 10, 64)
	rcID, _ := strconv.ParseUint(rec["risk_control_strategy_id"], 10, 64)
	curPrice, _ := strconv.ParseFloat(rec["current_price"], 64)
	qty, _ := strconv.ParseFloat(rec["quantity"], 64)
	posVal, _ := strconv.ParseFloat(rec["pos_value"], 64)
	lev, _ := strconv.Atoi(rec["leverage"])
	deleted, _ := strconv.Atoi(rec["deleted"])
	initMargin, _ := strconv.ParseFloat(rec["init_margin"], 64)
	posPrice, _ := strconv.ParseFloat(rec["pos_price"], 64)
	pnl, _ := strconv.ParseFloat(rec["pnl_value"], 64)
	side, _ := strconv.Atoi(rec["side"])
	posType, _ := strconv.Atoi(rec["pos_type"])
	ct, _ := parseTime(rec["close_time"])
	createdAt, _ := parseTime(rec["created_at"])
	updatedAt, _ := parseTime(rec["updated_at"])
	var ctPtr *time.Time
	if rec["close_time"] != "" {
		ctPtr = &ct
	}
	return &order.UserOrderPosition{
		ID: id, UserID: userID, UprunningOrderID: uoID, UserOrderID: uoOrderID,
		UserStrategyID: usID, RiskCtrlStratID: rcID, Exchange: rec["exchange"],
		PosType: order.PosType(posType), Asset: rec["asset"], CurrentPrice: curPrice,
		Quantity: qty, PosValue: posVal, Leverage: lev, Deleted: deleted,
		InitMargin: initMargin, PosPrice: posPrice, PnLValue: pnl, Side: order.Side(side),
		CloseTime: ctPtr, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}
