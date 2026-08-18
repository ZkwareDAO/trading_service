package reporter

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HTTP client with timeout for all HTTP requests
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// Side translation map (package-level, avoid recreation in loop)
var sideTranslations = map[string]string{
	"LONG":  "多单",
	"SHORT": "空单",
}

// User represents a user from users.csv
type User struct {
	ID       uint64
	Name     string
	Exchange string
	APIKey   string
	Secret   string
	Password string
}

// PositionData represents position data for CSV storage
type PositionData struct {
	Time       time.Time
	UserName   string
	Symbol     string
	Side       string
	Quantity   float64
	EntryPrice float64
	MarkPrice  float64
	PnL        float64
	Leverage   int
	ROI        float64
}

// ReadUsers reads user data from CSV file
func ReadUsers(filePath string) ([]User, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open users file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV file has no data rows")
	}

	var users []User
	// Skip header row
	for i, record := range records[1:] {
		if len(record) < 7 {
			return nil, fmt.Errorf("row %d has insufficient columns", i+2)
		}

		id, err := strconv.ParseUint(record[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid id in row %d: %w", i+2, err)
		}

		user := User{
			ID:       id,
			Name:     record[1],
			Exchange: record[2],
			APIKey:   record[3],
			Secret:   record[4],
			Password: record[5],
		}
		users = append(users, user)
	}

	return users, nil
}

// SavePositionsToCSV saves position data to CSV file (append mode)
// File format: exchange_positions_YYYYMMDD.csv (one file per day)
func SavePositionsToCSV(dateDir string, positions []PositionData) error {
	// Create directory if not exists
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Determine file name based on current date
	today := time.Now().Format("20060102")
	filePath := filepath.Join(dateDir, fmt.Sprintf("exchange_positions_%s.csv", today))

	// Check if file exists (for header writing)
	_, statErr := os.Stat(filePath)

	// Open file in append mode
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open CSV file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)

	// Write header if file is new
	if os.IsNotExist(statErr) {
		header := []string{"time", "user_name", "symbol", "side", "quantity", "entry_price", "mark_price", "pnl", "leverage", "roi"}
		if err := writer.Write(header); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}
	}

	// Write position data
	for _, pos := range positions {
		record := []string{
			pos.Time.Format(time.RFC3339),
			pos.UserName,
			pos.Symbol,
			pos.Side,
			fmt.Sprintf("%.8f", pos.Quantity),
			fmt.Sprintf("%.8f", pos.EntryPrice),
			fmt.Sprintf("%.8f", pos.MarkPrice),
			fmt.Sprintf("%.8f", pos.PnL),
			fmt.Sprintf("%d", pos.Leverage),
			fmt.Sprintf("%.2f", pos.ROI),
		}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("failed to write record: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("CSV write error: %w", err)
	}

	return nil
}

// colorProfit formats a value with green (info) color for profit.
func colorProfit(format string, args ...interface{}) string {
	text := fmt.Sprintf(format, args...)
	return fmt.Sprintf(`<font color="info">%s</font>`, text)
}

// colorLoss formats a value with red (warning) color for loss.
func colorLoss(format string, args ...interface{}) string {
	text := fmt.Sprintf(format, args...)
	return fmt.Sprintf(`<font color="warning">%s</font>`, text)
}

// colorPnL formats a PnL value with appropriate color.
func colorPnL(value float64, format string) string {
	if value >= 0 {
		return colorProfit(format, value)
	}
	return colorLoss(format, value)
}

// colorROI formats an ROI percentage with appropriate color and +/- prefix.
func colorROI(value float64) string {
	if value >= 0 {
		return colorProfit("+%.2f%%", value)
	}
	return colorLoss("%.2f%%", value)
}

// colorSide formats the side text with appropriate color.
func colorSide(sideText, side string) string {
	if side == "LONG" {
		return colorProfit("%s", sideText)
	}
	if side == "SHORT" {
		return colorLoss("%s", sideText)
	}
	return sideText
}

// FormatWeChatMessage formats position data for WeChat notification
func FormatWeChatMessage(positions []PositionData, userName, exchange string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("## 仓位播报 (%s - %s)\n\n", userName, exchange))

	for _, pos := range positions {
		sideText := sideTranslations[pos.Side]
		if sideText == "" {
			sideText = pos.Side
		}

		builder.WriteString(fmt.Sprintf("**%s** (%s)\n", pos.Symbol, userName))
		builder.WriteString(fmt.Sprintf("方向: %s\n", colorSide(sideText, pos.Side)))
		builder.WriteString(fmt.Sprintf("市场价: %.6f\n", pos.MarkPrice))
		builder.WriteString(fmt.Sprintf("成本价: %.6f\n", pos.EntryPrice))
		builder.WriteString(fmt.Sprintf("代币数量: %.4f\n", pos.Quantity))
		if pos.Leverage > 0 {
			builder.WriteString(fmt.Sprintf("杠杆倍数: %d\n", pos.Leverage))
		}
		builder.WriteString(fmt.Sprintf("盈亏金额: %s\n", colorPnL(pos.PnL, "%.6f")))
		builder.WriteString(fmt.Sprintf("盈亏百分比: %s\n\n", colorROI(pos.ROI)))
	}

	return builder.String()
}

// weChatPayload represents the WeChat webhook request body.
type weChatPayload struct {
	MsgType  string `json:"msgtype"`
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

// SendWeChatNotification sends notification via WeChat webhook
func SendWeChatNotification(webhookURL, message string) error {
	start := time.Now()
	log.Printf("[WeChat] Sending notification to webhook...")

	var payload weChatPayload
	payload.MsgType = "markdown"
	payload.Markdown.Content = message

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[WeChat] ERROR Failed to marshal JSON: %v", err)
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	resp, err := httpClient.Post(webhookURL, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		log.Printf("[WeChat] ERROR HTTP POST failed: %v", err)
		return fmt.Errorf("HTTP POST failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[WeChat] ERROR Failed to read response: %v", err)
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[WeChat] ERROR Webhook returned status %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[WeChat] SUCCESS Notification sent in %v", time.Since(start))
	return nil
}

// PositionInfo represents position data from API
type PositionInfo struct {
	Symbol        string  `json:"Symbol"`
	PositionSide  string  `json:"PositionSide"`
	Quantity      float64 `json:"Quantity"`
	EntryPrice    float64 `json:"EntryPrice"`
	MarkPrice     float64 `json:"MarkPrice"`
	UnrealizedPnl float64 `json:"UnrealizedPnl"`
	Leverage      int     `json:"Leverage"`
}

// FetchExchangePositions fetches positions from exchange API
func FetchExchangePositions(apiURL, userName, exchangeName string) ([]PositionData, error) {
	start := time.Now()
	log.Printf("[API] Fetching positions for %s/%s...", userName, exchangeName)

	// POST request with JSON body
	reqBody := struct {
		UserName string `json:"user_name"`
		Exchange string `json:"exchange"`
	}{
		UserName:  userName,
		Exchange:  exchangeName,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := httpClient.Post(apiURL, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		log.Printf("[API] ERROR HTTP POST failed for %s/%s: %v", userName, exchangeName, err)
		return nil, fmt.Errorf("HTTP POST failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[API] ERROR API returned status %d for %s/%s", resp.StatusCode, userName, exchangeName)
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[API] ERROR Failed to read response for %s/%s: %v", userName, exchangeName, err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Positions []PositionInfo `json:"positions"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[API] ERROR Failed to parse JSON for %s/%s: %v", userName, exchangeName, err)
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if result.Code != 0 {
		log.Printf("[API] ERROR API error for %s/%s: %s", userName, exchangeName, result.Message)
		return nil, fmt.Errorf("API error: %s", result.Message)
	}

	// Convert to PositionData
	var positions []PositionData
	for _, pos := range result.Data.Positions {
		roi := calculateROI(pos.EntryPrice, pos.MarkPrice, pos.Leverage, pos.PositionSide)

		positions = append(positions, PositionData{
			Time:       time.Now(),
			UserName:   userName,
			Symbol:     pos.Symbol,
			Side:       pos.PositionSide,
			Quantity:   pos.Quantity,
			EntryPrice: pos.EntryPrice,
			MarkPrice:  pos.MarkPrice,
			PnL:        pos.UnrealizedPnl,
			Leverage:   pos.Leverage,
			ROI:        roi,
		})
	}

	log.Printf("[API] SUCCESS Fetched %d positions for %s/%s in %v", len(positions), userName, exchangeName, time.Since(start))
	return positions, nil
}

// calculateROI calculates ROI based on price difference
// LONG:  (markPrice - entryPrice) / entryPrice * leverage * 100
// SHORT: (entryPrice - markPrice) / entryPrice * leverage * 100
// When leverage is 0 (e.g. options), treat as 1x leverage.
func calculateROI(entryPrice, markPrice float64, leverage int, side string) float64 {
	if entryPrice == 0 {
		return 0
	}

	effectiveLeverage := leverage
	if effectiveLeverage == 0 {
		effectiveLeverage = 1
	}

	var roi float64
	if side == "SHORT" {
		roi = (entryPrice - markPrice) / entryPrice
	} else {
		roi = (markPrice - entryPrice) / entryPrice
	}

	return roi * float64(effectiveLeverage) * 100
}
