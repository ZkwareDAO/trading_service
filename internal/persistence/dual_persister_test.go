package persistence

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
	"time"

	"trading-service/internal/order"
)

func setupTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dual-persister-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestAppendAndReadCSV(t *testing.T) {
	dir := setupTempDir(t)
	p := NewDualPersister(dir)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	users := []*order.User{
		{ID: 1, Name: "user1", Exchange: "binance", CreatedAt: now, UpdatedAt: now},
		{ID: 2, Name: "user2", Exchange: "binance", CreatedAt: now, UpdatedAt: now},
	}

	if err := p.WriteAllCSV("users.csv", toInterfaceSlice(users)); err != nil {
		t.Fatalf("WriteAllCSV: %v", err)
	}

	records, err := p.ReadAllCSV("users.csv")
	if err != nil {
		t.Fatalf("ReadAllCSV: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0]["name"] != "user1" {
		t.Errorf("expected name 'user1', got '%s'", records[0]["name"])
	}
	if records[1]["name"] != "user2" {
		t.Errorf("expected name 'user2', got '%s'", records[1]["name"])
	}
}

func TestAppendRow(t *testing.T) {
	dir := setupTempDir(t)
	p := NewDualPersister(dir)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	user := &order.User{
		ID: 1, Name: "user1", Exchange: "binance",
		CreatedAt: now, UpdatedAt: now,
	}

	if err := p.AppendRow("users.csv", user); err != nil {
		t.Fatalf("AppendRow: %v", err)
	}

	file, err := os.Open(filepath.Join(dir, "users.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 rows (header + data), got %d", len(records))
	}
}

func TestReadAllCSVFileNotExist(t *testing.T) {
	dir := setupTempDir(t)
	p := NewDualPersister(dir)

	records, err := p.ReadAllCSV("nonexistent.csv")
	if err != nil {
		t.Fatalf("expected nil error for non-existent file, got: %v", err)
	}
	if records != nil {
		t.Errorf("expected nil records, got %v", records)
	}
}

func TestCompactReplacesFileAtomically(t *testing.T) {
	dir := setupTempDir(t)
	p := NewDualPersister(dir)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	latest := []*order.User{
		{ID: 1, Name: "user1_latest", Exchange: "binance", CreatedAt: now, UpdatedAt: now},
		{ID: 2, Name: "user2", Exchange: "binance", CreatedAt: now, UpdatedAt: now},
	}
	if err := p.Compact("users.csv", toInterfaceSlice(latest)); err != nil {
		t.Fatal(err)
	}

	records, err := p.ReadAllCSV("users.csv")
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 2 {
		t.Errorf("expected 2 records after compact, got %d", len(records))
	}

	user1Found := false
	for _, rec := range records {
		if rec["id"] == "1" && rec["name"] == "user1_latest" {
			user1Found = true
		}
	}
	if !user1Found {
		t.Error("expected compacted file to contain user1_latest")
	}
}

func TestReadCSVAndParseUser(t *testing.T) {
	dir := setupTempDir(t)
	p := NewDualPersister(dir)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	users := []*order.User{
		{ID: 1, Name: "user1", Exchange: "binance", CreatedAt: now, UpdatedAt: now},
	}
	if err := p.WriteAllCSV("users.csv", toInterfaceSlice(users)); err != nil {
		t.Fatal(err)
	}

	records, err := p.ReadAllCSV("users.csv")
	if err != nil {
		t.Fatal(err)
	}

	result, err := parseUserFromRecord(records[0])
	if err != nil {
		t.Fatal(err)
	}

	if result.Name != "user1" {
		t.Errorf("expected name 'user1', got '%s'", result.Name)
	}
	if result.Exchange != "binance" {
		t.Errorf("expected exchange 'binance', got '%s'", result.Exchange)
	}
	if result.ID != 1 {
		t.Errorf("expected id 1, got %d", result.ID)
	}
}

func TestReadCSVAndParseStrategy(t *testing.T) {
	dir := setupTempDir(t)
	p := NewDualPersister(dir)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	strategies := []*order.Strategy{
		{
			ID: 1, Name: "ICT_1H_V1_BTCUSDT", StrategyType: "FutureCTAV2Strategy",
			Params: `{"StopLossThreshold":-0.1}`,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	if err := p.WriteAllCSV("strategies.csv", toInterfaceSlice(strategies)); err != nil {
		t.Fatal(err)
	}

	records, err := p.ReadAllCSV("strategies.csv")
	if err != nil {
		t.Fatal(err)
	}

	result, err := parseStrategyFromRecord(records[0])
	if err != nil {
		t.Fatal(err)
	}

	if result.Name != "ICT_1H_V1_BTCUSDT" {
		t.Errorf("unexpected name: %s", result.Name)
	}
	if result.StrategyType != "FutureCTAV2Strategy" {
		t.Errorf("unexpected type: %s", result.StrategyType)
	}
}

func TestReadCSVAndParseUserStrategy(t *testing.T) {
	dir := setupTempDir(t)
	p := NewDualPersister(dir)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	validBefore := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	userStrats := []*order.UserStrategy{
		{
			ID: 1, UserID: 1, Name: "ICT_1H_V1_BTCUSDT", Exchange: "binance",
			ValidBefore: validBefore, Cash: 1000, Parts: 5, Status: 1,
			StrategyID: 1, RiskStrategyType: "traditional", OrdersNum: 0,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	if err := p.WriteAllCSV("user_strategies.csv", toInterfaceSlice(userStrats)); err != nil {
		t.Fatal(err)
	}

	records, err := p.ReadAllCSV("user_strategies.csv")
	if err != nil {
		t.Fatal(err)
	}

	result, err := parseUserStrategyFromRecord(records[0])
	if err != nil {
		t.Fatal(err)
	}

	if result.UserID != 1 {
		t.Errorf("expected UserID 1, got %d", result.UserID)
	}
	if result.Cash != 1000 {
		t.Errorf("expected Cash 1000, got %f", result.Cash)
	}
	if result.RiskStrategyType != "traditional" {
		t.Errorf("expected 'traditional', got '%s'", result.RiskStrategyType)
	}
}

func TestReadCSVAndParseUserOrder(t *testing.T) {
	dir := setupTempDir(t)
	p := NewDualPersister(dir)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	orders := []*order.UserOrder{
		{
			ID: 1, UserID: 1, UserStrategyID: 1, PosType: order.PosTypeFutures,
			Exchange: "binance", BaseAsset: "BTC", QuoteAsset: "USDT",
			Quantity: 0, Cash: 100, TriggerPrice: 50000, Slippage: 0.01,
			Side: order.SideLong, OrderType: 0, Status: 1,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	if err := p.WriteAllCSV("user_orders.csv", toInterfaceSlice(orders)); err != nil {
		t.Fatal(err)
	}

	records, err := p.ReadAllCSV("user_orders.csv")
	if err != nil {
		t.Fatal(err)
	}

	result, err := parseUserOrderFromRecord(records[0])
	if err != nil {
		t.Fatal(err)
	}

	if result.UserStrategyID != 1 {
		t.Errorf("expected UserStrategyID 1, got %d", result.UserStrategyID)
	}
	if result.PosType != order.PosTypeFutures {
		t.Errorf("expected PosTypeFutures, got %d", result.PosType)
	}
	if result.TriggerPrice != 50000 {
		t.Errorf("expected TriggerPrice 50000, got %f", result.TriggerPrice)
	}
}

func TestReadCSVAndParseLeverageConfig(t *testing.T) {
	dir := setupTempDir(t)
	p := NewDualPersister(dir)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	configs := []*order.LeverageConfig{
		{
			ID: 1, UserID: 1, Asset: "BTC", Quote: "USDT",
			Leverage: 10, Exchange: "binance", Status: 1, PosType: order.PosTypeFutures,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	if err := p.WriteAllCSV("leverage_configs.csv", toInterfaceSlice(configs)); err != nil {
		t.Fatal(err)
	}

	records, err := p.ReadAllCSV("leverage_configs.csv")
	if err != nil {
		t.Fatal(err)
	}

	result, err := parseLeverageConfigFromRecord(records[0])
	if err != nil {
		t.Fatal(err)
	}

	if result.Leverage != 10 {
		t.Errorf("expected Leverage 10, got %d", result.Leverage)
	}
	if result.Asset != "BTC" {
		t.Errorf("expected Asset 'BTC', got '%s'", result.Asset)
	}
}

// toInterfaceSlice converts a slice of pointers to []interface{}.
func toInterfaceSlice[T any](slice []*T) []interface{} {
	out := make([]interface{}, len(slice))
	for i, v := range slice {
		out[i] = v
	}
	return out
}
