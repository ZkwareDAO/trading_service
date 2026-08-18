package persistence

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"trading-service/internal/order"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "globalstate-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	os.MkdirAll(filepath.Join(dir, ".compact"), 0755)
	return dir
}

func writeCSV(t *testing.T, dir, name string, rows []interface{}) {
	t.Helper()
	p := NewDualPersister(dir)
	if err := p.WriteAllCSV(name, rows); err != nil {
		t.Fatal(err)
	}
}

func TestReloadUprunningOrders_PreservesNewerInMemoryUpdate(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	oldTime := time.Date(2026, 6, 25, 3, 15, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)

	staleCSVOrder := &order.UprunningOrder{
		ID:                  1,
		UserID:              1,
		RelationID:          10,
		RelationType:        order.RelationTypeUserOrders,
		Exchange:            "binance",
		Symbol:              "NEARUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     100,
		ExchangeOrderStatus: "NEW",
		Side:                order.SideLong,
		CreatedAt:           oldTime,
		UpdatedAt:           oldTime,
	}
	writeCSV(t, dir, "uprunning_orders.csv", []interface{}{staleCSVOrder})

	gs.UprunningOrders[1] = &order.UprunningOrder{
		ID:                  1,
		UserID:              1,
		RelationID:          10,
		RelationType:        order.RelationTypeUserOrders,
		Exchange:            "binance",
		Symbol:              "NEARUSDT",
		PosType:             order.PosTypeFutures,
		ExchangeOrderID:     100,
		ExchangeOrderStatus: "FILLED",
		Side:                order.SideLong,
		CreatedAt:           oldTime,
		UpdatedAt:           newTime,
	}

	if err := repo.ReloadUprunningOrders(); err != nil {
		t.Fatalf("ReloadUprunningOrders: %v", err)
	}

	loaded, err := repo.GetUprunningOrderByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ExchangeOrderStatus != "FILLED" {
		t.Fatalf("expected newer in-memory FILLED to win, got %s", loaded.ExchangeOrderStatus)
	}
}

func TestGlobalState_NewGlobalState(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState: %v", err)
	}
	if gs == nil {
		t.Fatal("expected non-nil GlobalState")
	}
	if gs.Version != 0 {
		t.Errorf("expected initial version 0, got %d", gs.Version)
	}
	if gs.Users == nil {
		t.Error("expected Users map to be initialized")
	}
	if gs.Strategies == nil {
		t.Error("expected Strategies map to be initialized")
	}
	if gs.UserStrategies == nil {
		t.Error("expected UserStrategies map to be initialized")
	}
	if gs.UserOrders == nil {
		t.Error("expected UserOrders map to be initialized")
	}
	if gs.LeverageConfigs == nil {
		t.Error("expected LeverageConfigs map to be initialized")
	}
}

func TestGlobalState_CompactAll_DropsZeroIDRows(t *testing.T) {
	dir := setupTestDir(t)
	p := NewDualPersister(dir)
	if err := p.AppendRow("user_orders.csv", &order.UserOrder{}); err != nil {
		t.Fatal(err)
	}
	if err := p.AppendRow("leverage_configs.csv", &order.LeverageConfig{}); err != nil {
		t.Fatal(err)
	}

	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()

	if len(gs.UserOrders) != 0 {
		t.Fatalf("expected zero-id user order to be ignored, got %d", len(gs.UserOrders))
	}
	if len(gs.LeverageConfigs) != 0 {
		t.Fatalf("expected zero-id leverage config to be ignored, got %d", len(gs.LeverageConfigs))
	}

	// Clear the zero-ID rows from CSV before compact
	// (since they were ignored during load, memory is empty but CSV has data)
	if err := p.Compact("user_orders.csv", []interface{}{}); err != nil {
		t.Fatal(err)
	}
	if err := p.Compact("leverage_configs.csv", []interface{}{}); err != nil {
		t.Fatal(err)
	}

	// Now verify compacted files have only headers
	userOrderRows, err := p.ReadAllCSV("user_orders.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(userOrderRows) != 0 {
		t.Fatalf("expected compacted user_orders.csv to have no data rows, got %d", len(userOrderRows))
	}
	leverageRows, err := p.ReadAllCSV("leverage_configs.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(leverageRows) != 0 {
		t.Fatalf("expected compacted leverage_configs.csv to have no data rows, got %d", len(leverageRows))
	}
}

func TestUpsertLeverageConfig_SkipsUnchangedConfig(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewStateRepository(gs)

	firstID := repo.UpsertLeverageConfig(&order.LeverageConfig{
		UserID: 1, Asset: "NEAR", Quote: "USDT", Leverage: 5,
		Exchange: "binance", Status: 1, PosType: order.PosTypeFutures,
	})
	secondID := repo.UpsertLeverageConfig(&order.LeverageConfig{
		UserID: 1, Asset: "NEAR", Quote: "USDT", Leverage: 5,
		Exchange: "binance", Status: 1, PosType: order.PosTypeFutures,
	})
	gs.Shutdown()

	if secondID != firstID {
		t.Fatalf("expected unchanged upsert to return same ID %d, got %d", firstID, secondID)
	}
	rows, err := NewDualPersister(dir).ReadAllCSV("leverage_configs.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected unchanged leverage config to be written once, got %d rows", len(rows))
	}
}

func TestGlobalState_LoadExistingCSV(t *testing.T) {
	dir := setupTestDir(t)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	users := []*order.User{
		{ID: 1, Name: "user1", Exchange: "binance", CreatedAt: now, UpdatedAt: now},
		{ID: 2, Name: "user2", Exchange: "binance", CreatedAt: now, UpdatedAt: now},
	}
	writeCSV(t, dir, "users.csv", toInterfaceSliceGlobal(users))

	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState: %v", err)
	}

	if len(gs.Users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(gs.Users))
	}
	if gs.Users[1] == nil || gs.Users[1].Name != "user1" {
		t.Errorf("expected user1 at ID 1")
	}
	if gs.Users[2] == nil || gs.Users[2].Name != "user2" {
		t.Errorf("expected user2 at ID 2")
	}
}

func TestGlobalState_LoadWithDuplicates(t *testing.T) {
	dir := setupTestDir(t)

	// Write CSV with duplicate IDs (simulating append-only rows)
	f, err := os.Create(filepath.Join(dir, "users.csv"))
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("id,name,exchange,api_key,api_secret,api_password,created_at,updated_at\n")
	f.WriteString("1,old_user,binance,,,,2024-01-01T00:00:00Z,2024-01-01T00:00:00Z\n")
	f.WriteString("1,new_user,binance,,,,2024-01-01T00:00:00Z,2024-01-02T00:00:00Z\n")
	f.Close()

	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatalf("NewGlobalState: %v", err)
	}

	if len(gs.Users) != 1 {
		t.Fatalf("expected 1 user after dedup, got %d", len(gs.Users))
	}
	if gs.Users[1].Name != "new_user" {
		t.Errorf("expected 'new_user', got '%s'", gs.Users[1].Name)
	}
}

func TestStateRepository_CreateAndGetUser(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	user := &order.User{
		Name: "test_user", Exchange: "binance",
		CreatedAt: now, UpdatedAt: now,
	}

	id := repo.CreateUser(user)
	if id == 0 {
		t.Error("expected non-zero ID")
	}

	retrieved, err := repo.GetUserByID(id)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if retrieved.Name != "test_user" {
		t.Errorf("expected 'test_user', got '%s'", retrieved.Name)
	}
	if gs.Version != 1 {
		t.Errorf("expected version 1, got %d", gs.Version)
	}
}

func TestStateRepository_GetUserNotFound(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	_, err := repo.GetUserByID(999)
	if err == nil {
		t.Error("expected error for non-existent user")
	}
}

func TestStateRepository_CreateAndGetStrategy(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	s := &order.Strategy{
		Name: "ICT_1H", StrategyType: "FutureCTAV2Strategy",
		CreatedAt: now, UpdatedAt: now,
	}

	id := repo.CreateStrategy(s)
	if id == 0 {
		t.Error("expected non-zero ID")
	}

	retrieved, err := repo.GetStrategyByID(id)
	if err != nil {
		t.Fatalf("GetStrategyByID: %v", err)
	}
	if retrieved.Name != "ICT_1H" {
		t.Errorf("expected 'ICT_1H', got '%s'", retrieved.Name)
	}
}

func TestStateRepository_CreateAndListStrategyAsset(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	asset := &order.StrategyAsset{
		Name:       "OBVATRV2_4H_2_BTCUSDT",
		Asset:      "BTC",
		StrategyID: 1,
		PosType:    order.PosTypeFutures,
		Sort:       1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	id := repo.CreateStrategyAsset(asset)
	if id == 0 {
		t.Fatal("expected non-zero strategy_asset ID")
	}

	assets := repo.ListStrategyAssetsByStrategy(1)
	if len(assets) != 1 {
		t.Fatalf("expected 1 strategy_asset, got %d", len(assets))
	}
	if assets[0].Asset != "BTC" || assets[0].Name != "OBVATRV2_4H_2_BTCUSDT" {
		t.Fatalf("unexpected strategy_asset: %+v", assets[0])
	}
}

func TestStateRepository_GetStrategyAssetByNameAssetStrategy(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	repo.CreateStrategyAsset(&order.StrategyAsset{
		Name:       "OBVATRV2_4H_2_BTCUSDT",
		Asset:      "BTC",
		StrategyID: 7,
		PosType:    order.PosTypeFutures,
		Sort:       1,
	})

	asset, err := repo.GetStrategyAssetByNameAssetStrategy("OBVATRV2_4H_2_BTCUSDT", "BTC", 7)
	if err != nil {
		t.Fatalf("GetStrategyAssetByNameAssetStrategy: %v", err)
	}
	if asset.PosType != order.PosTypeFutures {
		t.Fatalf("expected futures pos type, got %d", asset.PosType)
	}
}

func TestStateRepository_UpdateUserStrategy(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	us := &order.UserStrategy{
		UserID: 1, Name: "ICT_1H", Exchange: "binance",
		Cash: 1000, Parts: 5, Status: 1, OrdersNum: 0,
		CreatedAt: now, UpdatedAt: now,
	}

	id := repo.CreateUserStrategy(us)
	if id == 0 {
		t.Error("expected non-zero ID from create")
	}

	updated := &order.UserStrategy{
		ID: id, UserID: 1, Name: "ICT_1H", Exchange: "binance",
		Cash: 1000, Parts: 5, Status: 1, OrdersNum: 1,
		CreatedAt: now, UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	repo.UpdateUserStrategy(updated)

	retrieved, err := repo.GetUserStrategyByID(id)
	if err != nil {
		t.Fatalf("GetUserStrategyByID: %v", err)
	}
	if retrieved.OrdersNum != 1 {
		t.Errorf("expected OrdersNum 1, got %d", retrieved.OrdersNum)
	}
	if retrieved.ID != id {
		t.Errorf("expected ID %d, got %d", id, retrieved.ID)
	}

	if len(gs.UserStrategies) != 1 {
		t.Errorf("expected 1 record after update, got %d", len(gs.UserStrategies))
	}
}

func TestStateRepository_UpdateUserStrategy_NotFound(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := &order.UserStrategy{
		ID: 999, UserID: 1, Name: "nonexistent",
		Cash: 500, Status: 1, CreatedAt: now, UpdatedAt: now,
	}

	err := repo.UpdateUserStrategy(updated)
	if err == nil {
		t.Error("expected error when updating non-existent user strategy")
	}
}

func TestStateRepository_CreateAndGetUserStrategy(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	validBefore := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	us := &order.UserStrategy{
		UserID: 1, Name: "ICT_1H", Exchange: "binance",
		ValidBefore: validBefore, Cash: 1000, Parts: 5, Status: 1,
		RiskStrategyType: "cta_intraday",
		CreatedAt:        now, UpdatedAt: now,
	}

	id := repo.CreateUserStrategy(us)
	if id == 0 {
		t.Error("expected non-zero ID")
	}

	retrieved, err := repo.GetUserStrategyByID(id)
	if err != nil {
		t.Fatalf("GetUserStrategyByID: %v", err)
	}
	if retrieved.Cash != 1000 {
		t.Errorf("expected Cash 1000, got %f", retrieved.Cash)
	}
	if retrieved.RiskStrategyType != "cta_intraday" {
		t.Errorf("expected 'cta_intraday', got '%s'", retrieved.RiskStrategyType)
	}
}

func TestStateRepository_CreateAndGetUserOrder(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	o := &order.UserOrder{
		UserID: 1, UserStrategyID: 1, PosType: order.PosTypeFutures,
		Exchange: "binance", BaseAsset: "BTC", QuoteAsset: "USDT",
		Cash: 100, TriggerPrice: 50000, Slippage: 0.01,
		Side: order.SideLong, OrderType: 0, Status: 1,
		CreatedAt: now, UpdatedAt: now,
	}

	id := repo.CreateUserOrder(o)
	if id == 0 {
		t.Error("expected non-zero ID")
	}

	retrieved, err := repo.GetUserOrderByID(id)
	if err != nil {
		t.Fatalf("GetUserOrderByID: %v", err)
	}
	if retrieved.TriggerPrice != 50000 {
		t.Errorf("expected TriggerPrice 50000, got %f", retrieved.TriggerPrice)
	}
}

func TestStateRepository_CreateAndGetLeverageConfig(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	lc := &order.LeverageConfig{
		UserID: 1, Asset: "BTC", Quote: "USDT",
		Leverage: 10, Exchange: "binance", Status: 1, PosType: order.PosTypeFutures,
		CreatedAt: now, UpdatedAt: now,
	}

	id := repo.CreateLeverageConfig(lc)
	if id == 0 {
		t.Error("expected non-zero ID")
	}

	retrieved, err := repo.GetLeverageConfigByID(id)
	if err != nil {
		t.Fatalf("GetLeverageConfigByID: %v", err)
	}
	if retrieved.Leverage != 10 {
		t.Errorf("expected Leverage 10, got %d", retrieved.Leverage)
	}
}

func TestStateRepository_IDGeneration(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	userID := repo.CreateUser(&order.User{Name: "u1", CreatedAt: now, UpdatedAt: now})
	stratID := repo.CreateStrategy(&order.Strategy{Name: "s1", CreatedAt: now, UpdatedAt: now})
	orderID := repo.CreateUserOrder(&order.UserOrder{
		UserID: userID, CreatedAt: now, UpdatedAt: now,
	})

	// With per-table counters, all IDs start from 1 for their respective tables
	// This is the correct behavior - IDs are unique within their table
	if userID != 1 {
		t.Errorf("User ID should be 1, got %d", userID)
	}
	if stratID != 1 {
		t.Errorf("Strategy ID should be 1, got %d", stratID)
	}
	if orderID != 1 {
		t.Errorf("Order ID should be 1, got %d", orderID)
	}
}

func TestStateRepository_LoadedIDCounter(t *testing.T) {
	dir := setupTestDir(t)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	users := []*order.User{
		{ID: 100, Name: "user100", Exchange: "binance", CreatedAt: now, UpdatedAt: now},
	}
	writeCSV(t, dir, "users.csv", toInterfaceSliceGlobal(users))

	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	newID := repo.CreateUser(&order.User{Name: "new_user", CreatedAt: now, UpdatedAt: now})
	if newID <= 100 {
		t.Errorf("expected ID > 100, got %d", newID)
	}
}

func TestStateRepository_ListAll(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	repo.CreateUser(&order.User{Name: "u1", CreatedAt: now, UpdatedAt: now})
	repo.CreateUser(&order.User{Name: "u2", CreatedAt: now, UpdatedAt: now})

	allUsers := repo.ListUsers()
	if len(allUsers) != 2 {
		t.Errorf("expected 2 users, got %d", len(allUsers))
	}
}

func TestStateRepository_ListStrategiesByType(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	repo.CreateStrategy(&order.Strategy{Name: "s1", StrategyType: "FutureCTAV2Strategy", CreatedAt: now, UpdatedAt: now})
	repo.CreateStrategy(&order.Strategy{Name: "s2", StrategyType: "SpotStrategy", CreatedAt: now, UpdatedAt: now})

	ctaStrats := repo.ListStrategiesByType("FutureCTAV2Strategy")
	if len(ctaStrats) != 1 {
		t.Errorf("expected 1 CTA strategy, got %d", len(ctaStrats))
	}
	if ctaStrats[0].Name != "s1" {
		t.Errorf("expected strategy 's1', got '%s'", ctaStrats[0].Name)
	}
}

func TestStateRepository_UserStrategiesByUser(t *testing.T) {
	dir := setupTestDir(t)
	gs, _ := NewGlobalState(dir)
	repo := NewStateRepository(gs)

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	repo.CreateUserStrategy(&order.UserStrategy{UserID: 1, Name: "us1", Status: 1, CreatedAt: now, UpdatedAt: now})
	repo.CreateUserStrategy(&order.UserStrategy{UserID: 1, Name: "us2", Status: 1, CreatedAt: now, UpdatedAt: now})
	repo.CreateUserStrategy(&order.UserStrategy{UserID: 2, Name: "us3", Status: 1, CreatedAt: now, UpdatedAt: now})

	user1Strats := repo.ListUserStrategiesByUser(1)
	if len(user1Strats) != 2 {
		t.Errorf("expected 2 strategies for user 1, got %d", len(user1Strats))
	}
}

func TestStateRepository_UpdateUserOrderPositionPrices_UpdatesActivePosition(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	pos := &order.UserOrderPosition{
		ID: 1, UserID: 1, UserOrderID: 10, Exchange: "binance",
		PosType: order.PosTypeFutures, Asset: "NEARUSDT",
		PosPrice: 5.0, Quantity: 1000, Side: order.SideLong,
		Leverage: 5, InitMargin: 1000,
		CurrentPrice: 4.8, PosValue: 4800, PnLValue: -200,
		Deleted: 0, CreatedAt: now, UpdatedAt: now,
	}
	repo.CreateUserOrderPosition(pos)

	prices := map[string]map[string]float64{
		"binance": {"NEARUSDT": 5.5},
	}
	count := repo.UpdateUserOrderPositionPrices(prices)
	if count != 1 {
		t.Fatalf("expected 1 position updated, got %d", count)
	}

	updated, err := repo.GetUserOrderPositionByID(1)
	if err != nil {
		t.Fatal(err)
	}

	// Dynamic fields should be updated
	if updated.CurrentPrice != 5.5 {
		t.Errorf("expected CurrentPrice=5.5, got %f", updated.CurrentPrice)
	}
	if updated.PosValue != 5500 {
		t.Errorf("expected PosValue=5500, got %f", updated.PosValue)
	}
	if updated.PnLValue != 500 {
		t.Errorf("expected PnLValue=500, got %f", updated.PnLValue)
	}

	// Static fields must NOT change
	if updated.PosPrice != 5.0 {
		t.Errorf("PosPrice should not change, got %f", updated.PosPrice)
	}
	if updated.Quantity != 1000 {
		t.Errorf("Quantity should not change, got %f", updated.Quantity)
	}
	if updated.InitMargin != 1000 {
		t.Errorf("InitMargin should not change, got %f", updated.InitMargin)
	}
	if updated.Leverage != 5 {
		t.Errorf("Leverage should not change, got %d", updated.Leverage)
	}
	if updated.Side != order.SideLong {
		t.Errorf("Side should not change, got %d", updated.Side)
	}
}

func TestStateRepository_UpdateUserOrderPositionPrices_SkipsDeletedPositions(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		ID: 1, Asset: "NEARUSDT", PosPrice: 5.0, Quantity: 1000,
		Side: order.SideLong, CurrentPrice: 4.0, PosValue: 4000,
		PnLValue: -1000, Deleted: 1, CreatedAt: now, UpdatedAt: now,
	})

	prices := map[string]map[string]float64{
		"": {"NEARUSDT": 5.5},
	}
	count := repo.UpdateUserOrderPositionPrices(prices)
	if count != 0 {
		t.Errorf("expected 0 (deleted), got %d", count)
	}
	updated, _ := repo.GetUserOrderPositionByID(1)
	if updated.CurrentPrice != 4.0 {
		t.Errorf("deleted position should not be updated, CurrentPrice=%f", updated.CurrentPrice)
	}
}

func TestStateRepository_UpdateUserOrderPositionPrices_SkipsMissingAssets(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		ID: 1, Asset: "BTCUSDT", PosPrice: 50000, Quantity: 0.1,
		Side: order.SideLong, CurrentPrice: 49000, PosValue: 4900,
		PnLValue: -100, Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})

	prices := map[string]map[string]float64{"": {"NEARUSDT": 5.5}}
	count := repo.UpdateUserOrderPositionPrices(prices)
	if count != 0 {
		t.Errorf("expected 0 (no matching price), got %d", count)
	}
}

func TestStateRepository_UpdateUserOrderPositionPrices_SkipsZeroPrice(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		ID: 1, Asset: "NEARUSDT", PosPrice: 5.0, Quantity: 1000,
		Side: order.SideLong, CurrentPrice: 4.8, PosValue: 4800,
		PnLValue: -200, Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})

	prices := map[string]map[string]float64{"": {"NEARUSDT": 0}}
	count := repo.UpdateUserOrderPositionPrices(prices)
	if count != 0 {
		t.Errorf("expected 0 (zero price), got %d", count)
	}
}

func TestStateRepository_UpdateUserOrderPositionPrices_ShortPosition(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		ID: 1, Asset: "NEARUSDT", PosPrice: 5.0, Quantity: 1000,
		Side: order.SideShort, InitMargin: 1000,
		CurrentPrice: 5.2, PosValue: 5200, PnLValue: -200,
		Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})

	// Price drops → short profit
	prices := map[string]map[string]float64{"": {"NEARUSDT": 4.5}}
	count := repo.UpdateUserOrderPositionPrices(prices)
	if count != 1 {
		t.Fatalf("expected 1, got %d", count)
	}

	updated, err := repo.GetUserOrderPositionByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CurrentPrice != 4.5 {
		t.Errorf("expected CurrentPrice=4.5, got %f", updated.CurrentPrice)
	}
	if updated.PosValue != 4500 {
		t.Errorf("expected PosValue=4500, got %f", updated.PosValue)
	}
	// Short PnL = (pos_price - price) * quantity = (5.0 - 4.5) * 1000 = 500
	if updated.PnLValue != 500 {
		t.Errorf("expected PnLValue=500, got %f", updated.PnLValue)
	}
}

func TestStateRepository_UpdateUserOrderPositionPrices_MultiplePositions(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		ID: 1, Asset: "NEARUSDT", PosPrice: 5.0, Quantity: 1000,
		Side: order.SideLong, CurrentPrice: 4.8, PosValue: 4800,
		PnLValue: -200, Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		ID: 2, Asset: "BTCUSDT", PosPrice: 50000, Quantity: 0.1,
		Side: order.SideLong, CurrentPrice: 49000, PosValue: 4900,
		PnLValue: -100, Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		ID: 3, Asset: "ETHUSDT", PosPrice: 2000, Quantity: 1,
		Side: order.SideLong, CurrentPrice: 1900, PosValue: 1900,
		PnLValue: -100, Deleted: 1, CreatedAt: now, UpdatedAt: now,
	})

	prices := map[string]map[string]float64{
		"": {
			"NEARUSDT": 5.5,
			"BTCUSDT":  55000,
			"ETHUSDT":  2500, // closed → should not update
		},
	}
	count := repo.UpdateUserOrderPositionPrices(prices)
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}

	near, _ := repo.GetUserOrderPositionByID(1)
	if near.CurrentPrice != 5.5 || near.PnLValue != 500 {
		t.Errorf("NEAR: CurrentPrice=%f PnL=%f", near.CurrentPrice, near.PnLValue)
	}

	btc, _ := repo.GetUserOrderPositionByID(2)
	if btc.CurrentPrice != 55000 || btc.PnLValue != 500 {
		t.Errorf("BTC: CurrentPrice=%f PnL=%f", btc.CurrentPrice, btc.PnLValue)
	}

	eth, _ := repo.GetUserOrderPositionByID(3)
	if eth.CurrentPrice != 1900 {
		t.Errorf("ETH closed should not change, CurrentPrice=%f", eth.CurrentPrice)
	}
}

func TestStateRepository_UpdateUserOrderPositionPrices_NoCSVAppend(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	repo.CreateUserOrderPosition(&order.UserOrderPosition{
		ID: 1, Asset: "NEARUSDT", PosPrice: 5.0, Quantity: 1000,
		Side: order.SideLong, CurrentPrice: 4.8, PosValue: 4800,
		PnLValue: -200, Deleted: 0, CreatedAt: now, UpdatedAt: now,
	})

	p := repo.Persister()
	rowsBefore, err := p.ReadAllCSV("user_order_positions.csv")
	if err != nil {
		t.Fatal(err)
	}

	prices := map[string]map[string]float64{"": {"NEARUSDT": 5.5}}
	repo.UpdateUserOrderPositionPrices(prices)

	rowsAfter, err := p.ReadAllCSV("user_order_positions.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rowsAfter) != len(rowsBefore) {
		t.Errorf("expected no CSV append, before=%d after=%d", len(rowsBefore), len(rowsAfter))
	}
}

// toInterfaceSliceGlobal converts a slice of pointers to []interface{}.
func toInterfaceSliceGlobal[T any](slice []*T) []interface{} {
	out := make([]interface{}, len(slice))
	for i, v := range slice {
		out[i] = v
	}
	return out
}

// ===== Tests for position query enhancement =====

func TestFindUserStrategyIDsByName_MultipleUsers(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	user1ID := repo.CreateUser(&order.User{Name: "alice", Exchange: "binance"})
	user2ID := repo.CreateUser(&order.User{Name: "bob", Exchange: "hyperliquid"})

	stratID := repo.CreateStrategy(&order.Strategy{Name: "DOLPHIN_USDT", StrategyType: "cta_intraday"})
	us1ID := repo.CreateUserStrategy(&order.UserStrategy{UserID: user1ID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})
	us2ID := repo.CreateUserStrategy(&order.UserStrategy{UserID: user2ID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})

	ids := repo.FindUserStrategyIDsByName("DOLPHIN_USDT")
	if len(ids) != 2 {
		t.Fatalf("Expected 2 IDs, got %d: %v", len(ids), ids)
	}

	found1, found2 := false, false
	for _, id := range ids {
		if id == us1ID {
			found1 = true
		}
		if id == us2ID {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Errorf("Expected IDs %d and %d, got %v", us1ID, us2ID, ids)
	}
}

func TestFindUserStrategyIDsByUserAndName_MultipleRecords(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	userID := repo.CreateUser(&order.User{Name: "alice", Exchange: "binance"})
	otherUserID := repo.CreateUser(&order.User{Name: "bob", Exchange: "hyperliquid"})

	stratID := repo.CreateStrategy(&order.Strategy{Name: "DOLPHIN_USDT", StrategyType: "cta_intraday"})

	// Same user, same strategy name → two user_strategy records
	usID1 := repo.CreateUserStrategy(&order.UserStrategy{UserID: userID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})
	usID2 := repo.CreateUserStrategy(&order.UserStrategy{UserID: userID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})
	// Different user, same name → should NOT be included
	repo.CreateUserStrategy(&order.UserStrategy{UserID: otherUserID, StrategyID: stratID, Name: "DOLPHIN_USDT", Status: 1})

	ids := repo.FindUserStrategyIDsByUserAndName(userID, "DOLPHIN_USDT")
	if len(ids) != 2 {
		t.Fatalf("Expected 2 IDs for same user, got %d: %v", len(ids), ids)
	}

	found1, found2 := false, false
	for _, id := range ids {
		if id == usID1 {
			found1 = true
		}
		if id == usID2 {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Errorf("Expected IDs %d and %d, got %v", usID1, usID2, ids)
	}
}

func TestFindUserStrategyIDsByUserAndName_NotFound(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	ids := repo.FindUserStrategyIDsByUserAndName(999, "NONEXISTENT")
	if len(ids) != 0 {
		t.Errorf("Expected empty slice for non-existent user+name, got %v", ids)
	}
}

func TestFindUserStrategyIDsByName_NotFound(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	ids := repo.FindUserStrategyIDsByName("NONEXISTENT")
	if len(ids) != 0 {
		t.Errorf("Expected empty slice for non-existent name, got %v", ids)
	}
}

func TestListUserOrderPositionsByFilter_UserStrategyIDs(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})
	now := time.Now()

	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 100, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "BTC", Deleted: 0, CreatedAt: now})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 200, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "ETH", Deleted: 0, CreatedAt: now})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 300, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "SOL", Deleted: 0, CreatedAt: now})

	result := repo.ListUserOrderPositionsByFilter(UserOrderPositionFilter{UserStrategyIDs: []uint64{100, 300}})
	if len(result) != 2 {
		t.Errorf("Expected 2 positions for IDs [100,300], got %d", len(result))
	}
	for _, pos := range result {
		if pos.UserStrategyID != 100 && pos.UserStrategyID != 300 {
			t.Errorf("Unexpected UserStrategyID %d", pos.UserStrategyID)
		}
	}
}

func TestListUserOrderPositionsByFilter_CreatedAtRange(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})

	t1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 1, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "A", Deleted: 0, CreatedAt: t1})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 2, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "B", Deleted: 0, CreatedAt: t2})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 3, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "C", Deleted: 0, CreatedAt: t3})

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC)
	result := repo.ListUserOrderPositionsByFilter(UserOrderPositionFilter{CreatedAtFrom: &from, CreatedAtTo: &to})
	if len(result) != 2 {
		t.Errorf("Expected 2 positions in July, got %d", len(result))
	}

	result2 := repo.ListUserOrderPositionsByFilter(UserOrderPositionFilter{CreatedAtFrom: &t2})
	if len(result2) != 2 {
		t.Errorf("Expected 2 positions from July 15, got %d", len(result2))
	}

	result3 := repo.ListUserOrderPositionsByFilter(UserOrderPositionFilter{CreatedAtTo: &t2})
	if len(result3) != 2 {
		t.Errorf("Expected 2 positions up to July 15, got %d", len(result3))
	}
}

func TestListUserPositionsByFilter_UserStrategyIDs(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})
	now := time.Now()

	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 100, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 1, Deleted: 0, CreatedAt: now})
	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 200, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 2, Deleted: 0, CreatedAt: now})

	result := repo.ListUserPositionsByFilter(UserPositionFilter{UserStrategyIDs: []uint64{100}})
	if len(result) != 1 {
		t.Errorf("Expected 1 position for ID 100, got %d", len(result))
	}
}

func TestListUserPositionsByFilter_CreatedAtRange(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})

	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 1, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 1, Deleted: 0, CreatedAt: t1})
	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 2, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 2, Deleted: 0, CreatedAt: t2})

	from := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	result := repo.ListUserPositionsByFilter(UserPositionFilter{CreatedAtFrom: &from})
	if len(result) != 1 {
		t.Errorf("Expected 1 position after June 15, got %d", len(result))
	}
}

func TestListUserOrderPositionsByFilter_CloseTimeRange(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})

	c1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	c2 := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	c3 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	// 3 条已平仓（含 close_time），1 条活跃（close_time 为 nil）
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 1, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "A", Deleted: 1, CloseTime: &c1, CreatedAt: c1})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 2, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "B", Deleted: 1, CloseTime: &c2, CreatedAt: c2})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 3, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "C", Deleted: 1, CloseTime: &c3, CreatedAt: c3})
	repo.CreateUserOrderPosition(&order.UserOrderPosition{UserID: userID, UserStrategyID: 4, Exchange: "binance", PosType: order.PosTypeFutures, Asset: "D", Deleted: 0, CreatedAt: c2})

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC)
	// 双边区间：只命中 7 月平仓的两条，活跃仓位被排除
	result := repo.ListUserOrderPositionsByFilter(UserOrderPositionFilter{CloseTimeFrom: &from, CloseTimeTo: &to})
	if len(result) != 2 {
		t.Errorf("Expected 2 closed positions in July, got %d", len(result))
	}

	// 仅起始：含 c2、c3 两条已平仓，活跃仓位排除
	result2 := repo.ListUserOrderPositionsByFilter(UserOrderPositionFilter{CloseTimeFrom: &c2})
	if len(result2) != 2 {
		t.Errorf("Expected 2 closed positions from July 15, got %d", len(result2))
	}

	// 仅截止：含 c1、c2 两条已平仓，活跃仓位排除
	result3 := repo.ListUserOrderPositionsByFilter(UserOrderPositionFilter{CloseTimeTo: &c2})
	if len(result3) != 2 {
		t.Errorf("Expected 2 closed positions up to July 15, got %d", len(result3))
	}

	// 不传任何 close 参数：活跃仓位仍被保留（共 4 条）
	result4 := repo.ListUserOrderPositionsByFilter(UserOrderPositionFilter{UserID: userID})
	if len(result4) != 4 {
		t.Errorf("Expected 4 positions without close filter (incl active), got %d", len(result4))
	}
}

func TestListUserPositionsByFilter_CloseTimeRange(t *testing.T) {
	dir := setupTestDir(t)
	gs, err := NewGlobalState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Shutdown()
	repo := NewStateRepository(gs)

	userID := repo.CreateUser(&order.User{Name: "test", Exchange: "binance"})

	c1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c2 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 1, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 1, Deleted: 1, CloseTime: &c1, CreatedAt: c1})
	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 2, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 2, Deleted: 1, CloseTime: &c2, CreatedAt: c2})
	repo.CreateUserPosition(&order.UserPosition{UserID: userID, UserStrategyID: 3, Exchange: "binance", PosType: order.PosTypeFutures, Quantity: 3, Deleted: 0, CreatedAt: c2})

	// 传入 close_from：活跃仓位（CloseTime==nil）必须被排除，只返回 7 月平仓的那条
	from := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	result := repo.ListUserPositionsByFilter(UserPositionFilter{CloseTimeFrom: &from})
	if len(result) != 1 {
		t.Errorf("Expected 1 closed position after June 15 (active excluded), got %d", len(result))
	}

	// 不传 close 参数：全部保留（含活跃），共 3 条
	result2 := repo.ListUserPositionsByFilter(UserPositionFilter{UserID: userID})
	if len(result2) != 3 {
		t.Errorf("Expected 3 positions without close filter (incl active), got %d", len(result2))
	}
}
