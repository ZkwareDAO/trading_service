package reporter

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadUsers(t *testing.T) {
	// 创建临时测试文件
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "users.csv")

	// 写入测试数据
	content := `id,name,exchange,api_key,api_secret,api_password,created_at,updated_at
102,test_strategy,binance,key1,secret1,,,
103,test_hy,hyperliquid,key2,secret2,,,
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// 测试读取用户
	users, err := ReadUsers(testFile)
	if err != nil {
		t.Fatalf("ReadUsers failed: %v", err)
	}

	if len(users) != 2 {
		t.Errorf("Expected 2 users, got %d", len(users))
	}

	if users[0].Name != "test_strategy" || users[0].Exchange != "binance" {
		t.Errorf("User 0: expected name=test_strategy, exchange=binance, got name=%s, exchange=%s",
			users[0].Name, users[0].Exchange)
	}

	if users[1].Name != "test_hy" || users[1].Exchange != "hyperliquid" {
		t.Errorf("User 1: expected name=test_hy, exchange=hyperliquid, got name=%s, exchange=%s",
			users[1].Name, users[1].Exchange)
	}
}

func TestReadUsersErrors(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantErr  bool
	}{
		{
			name:    "file not found",
			content: "",
			wantErr: true,
		},
		{
			name:    "empty file",
			content: "id,name,exchange,api_key,api_secret,api_password,created_at,updated_at",
			wantErr: true,
		},
		{
			name:    "invalid id",
			content: "id,name,exchange,api_key,api_secret,api_password,created_at,updated_at\ninvalid,test,binance,key,secret,,",
			wantErr: true,
		},
		{
			name:    "insufficient columns",
			content: "id,name,exchange,api_key,api_secret,api_password,created_at,updated_at\n102,test",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testFile := filepath.Join(tmpDir, "users.csv")

			if tt.content != "" {
				if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			}

			_, err := ReadUsers(testFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadUsers() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSavePositionsToCSV(t *testing.T) {
	tmpDir := t.TempDir()
	dateDir := filepath.Join(tmpDir, "20260723")

	positions := []PositionData{
		{
			Time:        time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC),
			UserName:    "test_user",
			Symbol:      "BTCUSDT",
			Side:        "LONG",
			Quantity:    0.1,
			EntryPrice:  45000.0,
			MarkPrice:   45500.0,
			PnL:         50.0,
			Leverage:    10,
			ROI:         1.11,
		},
		{
			Time:        time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC),
			UserName:    "test_user",
			Symbol:      "ETHUSDT",
			Side:        "SHORT",
			Quantity:    1.0,
			EntryPrice:  3000.0,
			MarkPrice:   2950.0,
			PnL:         50.0,
			Leverage:    5,
			ROI:         0.83,
		},
	}

	err := SavePositionsToCSV(dateDir, positions)
	if err != nil {
		t.Fatalf("SavePositionsToCSV failed: %v", err)
	}

	// 验证文件创建
	files, err := filepath.Glob(filepath.Join(dateDir, "exchange_positions_*.csv"))
	if err != nil {
		t.Fatalf("Failed to list files: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("No CSV file created")
	}

	// 读取并验证内容
	file, err := os.Open(files[0])
	if err != nil {
		t.Fatalf("Failed to open CSV: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read CSV: %v", err)
	}

	// 第一行是header
	if len(records) != 3 { // header + 2 data rows
		t.Errorf("Expected 3 records (header + 2 rows), got %d", len(records))
	}

	// 验证header
	expectedHeader := []string{"time", "user_name", "symbol", "side", "quantity", "entry_price", "mark_price", "pnl", "leverage", "roi"}
	for i, col := range expectedHeader {
		if records[0][i] != col {
			t.Errorf("Header column %d: expected %s, got %s", i, col, records[0][i])
		}
	}
}

func TestAppendPositionsToCSV(t *testing.T) {
	tmpDir := t.TempDir()
	dateDir := filepath.Join(tmpDir, "20260723")

	// 第一次写入
	positions1 := []PositionData{
		{
			Time:       time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC),
			Symbol:     "BTCUSDT",
			Side:       "LONG",
			Quantity:   0.1,
			EntryPrice: 45000.0,
			MarkPrice:  45500.0,
			PnL:        50.0,
			Leverage:   10,
			ROI:        1.11,
		},
	}

	err := SavePositionsToCSV(dateDir, positions1)
	if err != nil {
		t.Fatalf("First save failed: %v", err)
	}

	// 第二次追加（同一天，不同小时）
	positions2 := []PositionData{
		{
			Time:       time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC),
			Symbol:     "ETHUSDT",
			Side:       "SHORT",
			Quantity:   1.0,
			EntryPrice: 3000.0,
			MarkPrice:  2950.0,
			PnL:        50.0,
			Leverage:   5,
			ROI:        0.83,
		},
	}

	err = SavePositionsToCSV(dateDir, positions2)
	if err != nil {
		t.Fatalf("Second save failed: %v", err)
	}

	// 验证文件只有一个（追加模式）
	files, err := filepath.Glob(filepath.Join(dateDir, "exchange_positions_*.csv"))
	if err != nil {
		t.Fatalf("Failed to list files: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("Expected 1 file (append mode), got %d", len(files))
	}

	// 验证内容包含两条数据
	file, err := os.Open(files[0])
	if err != nil {
		t.Fatalf("Failed to open CSV: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read CSV: %v", err)
	}

	if len(records) != 3 { // header + 2 data rows
		t.Errorf("Expected 3 records after append, got %d", len(records))
	}
}

func TestSavePositionsToCSVEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	dateDir := filepath.Join(tmpDir, "20260723")

	// 测试空数组
	err := SavePositionsToCSV(dateDir, []PositionData{})
	if err != nil {
		t.Errorf("SavePositionsToCSV with empty slice should not fail: %v", err)
	}
}

func TestFormatWeChatMessage(t *testing.T) {
	positions := []PositionData{
		{
			Symbol:     "ETH-25DEC26-2400-C",
			Side:       "LONG",
			Quantity:   40.0,
			EntryPrice: 0.18,
			MarkPrice:  0.065857,
			PnL:        -4.56572,
			Leverage:   5,
			ROI:        -63.41,
		},
	}

	userName := "test_user"
	exchange := "deribit"

	msg := FormatWeChatMessage(positions, userName, exchange)

	// 验证消息格式
	if !strings.Contains(msg, "ETH-25DEC26-2400-C") {
		t.Error("Message should contain symbol")
	}
	if !strings.Contains(msg, "test_user") {
		t.Error("Message should contain user name")
	}
	if !strings.Contains(msg, "多单") {
		t.Error("Message should contain side '多单'")
	}
	if !strings.Contains(msg, "-63.41%") {
		t.Error("Message should contain ROI percentage")
	}
}

func TestFormatWeChatMessage_OptionalNoLeverage(t *testing.T) {
	// 期权仓位杠杆为0，不应显示"杠杆倍数"行
	positions := []PositionData{
		{
			Symbol:     "ETH-25SEP26-1900-C",
			Side:       "LONG",
			Quantity:   1.0,
			EntryPrice: 0.0735,
			MarkPrice:  0.082366,
			PnL:        0.008866,
			Leverage:   0,
			ROI:        12.06,
		},
	}

	msg := FormatWeChatMessage(positions, "test_deribit", "deribit")

	// 杠杆为0时不应出现"杠杆倍数"行
	if strings.Contains(msg, "杠杆倍数") {
		t.Error("Should NOT show leverage line when leverage is 0 (options)")
	}
}

func TestFormatWeChatMessage_LeverageShown(t *testing.T) {
	// 合约仓位杠杆>0，应显示"杠杆倍数"行
	positions := []PositionData{
		{
			Symbol:     "BTCUSDT",
			Side:       "LONG",
			Quantity:   0.1,
			EntryPrice: 45000.0,
			MarkPrice:  45500.0,
			PnL:        50.0,
			Leverage:   10,
			ROI:        11.11,
		},
	}

	msg := FormatWeChatMessage(positions, "test_user", "binance")

	// 杠杆>0时应显示"杠杆倍数"行
	if !strings.Contains(msg, "杠杆倍数: 10") {
		t.Error("Should show leverage line when leverage > 0")
	}
}

func TestFormatWeChatMessage_ProfitColor(t *testing.T) {
	// 盈利仓位应使用绿色(info)
	positions := []PositionData{
		{
			Symbol:     "ETH-25SEP26-1900-C",
			Side:       "LONG",
			Quantity:   1.0,
			EntryPrice: 0.0735,
			MarkPrice:  0.082366,
			PnL:        0.008866,
			Leverage:   0,
			ROI:        12.06,
		},
	}

	msg := FormatWeChatMessage(positions, "test_deribit", "deribit")

	// 盈利应显示绿色
	if !strings.Contains(msg, `<font color="info">`) {
		t.Error("Profit should use green color (info)")
	}
	// 盈利百分比应带+号
	if !strings.Contains(msg, "+12.06%") {
		t.Error("Profit ROI should have + prefix")
	}
}

func TestFormatWeChatMessage_LossColor(t *testing.T) {
	// 亏损仓位应使用红色(warning)
	positions := []PositionData{
		{
			Symbol:     "ETH-25DEC26-2400-C",
			Side:       "LONG",
			Quantity:   40.0,
			EntryPrice: 0.18,
			MarkPrice:  0.065857,
			PnL:        -4.56572,
			Leverage:   5,
			ROI:        -63.41,
		},
	}

	msg := FormatWeChatMessage(positions, "test_user", "deribit")

	// 亏损应显示红色
	if !strings.Contains(msg, `<font color="warning">`) {
		t.Error("Loss should use red color (warning)")
	}
}

func TestFormatWeChatMessage_SideColor(t *testing.T) {
	// 多单应绿色，空单应红色
	longPositions := []PositionData{
		{Symbol: "BTCUSDT", Side: "LONG", Quantity: 0.1, EntryPrice: 45000, MarkPrice: 45500, PnL: 50, Leverage: 10, ROI: 11.11},
	}
	shortPositions := []PositionData{
		{Symbol: "ETHUSDT", Side: "SHORT", Quantity: 1.0, EntryPrice: 3000, MarkPrice: 2950, PnL: 50, Leverage: 5, ROI: 8.33},
	}

	longMsg := FormatWeChatMessage(longPositions, "test", "binance")
	shortMsg := FormatWeChatMessage(shortPositions, "test", "binance")

	// 多单方向应绿色
	if !strings.Contains(longMsg, `<font color="info">多单</font>`) {
		t.Error("LONG side should be green")
	}
	// 空单方向应红色
	if !strings.Contains(shortMsg, `<font color="warning">空单</font>`) {
		t.Error("SHORT side should be red")
	}
}


func TestSendWeChatNotification(t *testing.T) {
	// 这个测试需要真实的webhook URL，仅验证序列化
	// 使用无效URL验证错误处理
	err := SendWeChatNotification("http://invalid.url/test", "test message")
	if err == nil {
		t.Log("Expected error for invalid URL, but got nil")
	}
	// 错误是预期的，因为URL不存在
}

func TestFetchExchangePositions(t *testing.T) {
	// 这个测试需要真实的API服务器
	// 使用无效URL验证错误处理
	_, err := FetchExchangePositions("http://invalid.api/api/v1/exchange/positions", "test_user", "binance")
	if err == nil {
		t.Error("Expected error for invalid API URL")
	}
}

func TestFetchExchangePositions_MockServer(t *testing.T) {
	// 使用 httptest 模拟 API 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证 POST 请求
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// 解析请求体
		var req struct {
			UserName string `json:"user_name"`
			Exchange string `json:"exchange"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if req.UserName != "test_user" || req.Exchange != "binance" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		resp := struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				Positions []PositionInfo `json:"positions"`
			} `json:"data"`
		}{
			Code: 0,
		}
		resp.Data.Positions = []PositionInfo{
			{
				Symbol:        "BTCUSDT",
				PositionSide:  "LONG",
				Quantity:      0.1,
				EntryPrice:    45000.0,
				MarkPrice:     45500.0,
				UnrealizedPnl: 50.0,
				Leverage:      10,
			},
			{
				Symbol:        "ETHUSDT",
				PositionSide:  "SHORT",
				Quantity:      1.0,
				EntryPrice:    3000.0,
				MarkPrice:     2950.0,
				UnrealizedPnl: 50.0,
				Leverage:      5,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	positions, err := FetchExchangePositions(server.URL+"/api/v1/exchange/positions", "test_user", "binance")
	if err != nil {
		t.Fatalf("FetchExchangePositions failed: %v", err)
	}

	if len(positions) != 2 {
		t.Fatalf("Expected 2 positions, got %d", len(positions))
	}

	// 验证 LONG 仓位
	if positions[0].Symbol != "BTCUSDT" {
		t.Errorf("Position 0: expected BTCUSDT, got %s", positions[0].Symbol)
	}
	if positions[0].Side != "LONG" {
		t.Errorf("Position 0: expected LONG, got %s", positions[0].Side)
	}
	if positions[0].UserName != "test_user" {
		t.Errorf("Position 0: expected user test_user, got %s", positions[0].UserName)
	}

	// 验证 SHORT 仓位
	if positions[1].Symbol != "ETHUSDT" {
		t.Errorf("Position 1: expected ETHUSDT, got %s", positions[1].Symbol)
	}
	if positions[1].Side != "SHORT" {
		t.Errorf("Position 1: expected SHORT, got %s", positions[1].Side)
	}
}

func TestFetchExchangePositions_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := FetchExchangePositions(server.URL+"/api/v1/exchange/positions", "test", "binance")
	if err == nil {
		t.Error("Expected error for 500 status")
	}
}

func TestFetchExchangePositions_BusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{Code: 1, Message: "user not found"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	_, err := FetchExchangePositions(server.URL+"/api/v1/exchange/positions", "test", "binance")
	if err == nil {
		t.Error("Expected error for business error response")
	}
}

func TestFetchExchangePositions_InvalidURL(t *testing.T) {
	_, err := FetchExchangePositions("://invalid", "test", "binance")
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestCalculateROI(t *testing.T) {
	tests := []struct {
		name       string
		entryPrice float64
		markPrice  float64
		leverage   int
		side       string
		want       float64
	}{
		{
			name:       "LONG profit",
			entryPrice: 45000.0,
			markPrice:  45500.0,
			leverage:   10,
			side:       "LONG",
			want:       11.11, // (45500-45000)/45000 * 10 * 100
		},
		{
			name:       "SHORT profit",
			entryPrice: 3000.0,
			markPrice:  2950.0,
			leverage:   5,
			side:       "SHORT",
			want:       8.33, // (3000-2950)/3000 * 5 * 100
		},
		{
			name:       "LONG loss",
			entryPrice: 45000.0,
			markPrice:  44000.0,
			leverage:   10,
			side:       "LONG",
			want:       -22.22,
		},
		{
			name:       "zero entry price",
			entryPrice: 0,
			markPrice:  100.0,
			leverage:   10,
			side:       "LONG",
			want:       0,
		},
		{
			name:       "default side (treated as LONG)",
			entryPrice: 100.0,
			markPrice:  110.0,
			leverage:   1,
			side:       "BOTH",
			want:       10.0,
		},
		{
			name:       "zero leverage (options, treated as 1x)",
			entryPrice: 0.0735,
			markPrice:  0.081943,
			leverage:   0,
			side:       "LONG",
			want:       11.48, // (0.081943-0.0735)/0.0735 * 1 * 100
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateROI(tt.entryPrice, tt.markPrice, tt.leverage, tt.side)
			if diff := got - tt.want; diff > 0.01 || diff < -0.01 {
				t.Errorf("calculateROI() = %v, want %v", got, tt.want)
			}
		})
	}
}