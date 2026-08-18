package persistence

import (
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"trading-service/internal/order"
)

// GlobalState is the singleton in-memory state store.
type GlobalState struct {
	Version int64 // atomically incremented on each mutation

	// Entity maps (keyed by ID)
	Users                 map[uint64]*order.User
	Strategies            map[uint64]*order.Strategy
	StrategyAssets        map[uint64]*order.StrategyAsset
	UserStrategies        map[uint64]*order.UserStrategy
	UserOrders            map[uint64]*order.UserOrder
	LeverageConfigs       map[uint64]*order.LeverageConfig
	ExchangeSymbolFilters map[uint64]*order.ExchangeSymbolFilter
	UprunningOrders       map[uint64]*order.UprunningOrder
	UserOrderPositions    map[uint64]*order.UserOrderPosition
	UserPositions         map[uint64]*order.UserPosition

	// Per-table ID generators
	idCounters sync.Map // tableName(string) -> *uint64

	// Persistence engine
	persister *DualPersister

	// Write wait group for shutdown
	writeWg sync.WaitGroup

	// Write lock for the ID counter
	mu sync.Mutex

	// Read-write lock for all entity maps
	rw sync.RWMutex
}

// NewGlobalState creates a new state store, loading existing data from CSV.
func NewGlobalState(dataDir string) (*GlobalState, error) {
	gs := &GlobalState{
		Users:                 make(map[uint64]*order.User),
		Strategies:            make(map[uint64]*order.Strategy),
		StrategyAssets:        make(map[uint64]*order.StrategyAsset),
		UserStrategies:        make(map[uint64]*order.UserStrategy),
		UserOrders:            make(map[uint64]*order.UserOrder),
		LeverageConfigs:       make(map[uint64]*order.LeverageConfig),
		ExchangeSymbolFilters: make(map[uint64]*order.ExchangeSymbolFilter),
		UprunningOrders:       make(map[uint64]*order.UprunningOrder),
		UserOrderPositions:    make(map[uint64]*order.UserOrderPosition),
		UserPositions:         make(map[uint64]*order.UserPosition),
		persister:             NewDualPersister(dataDir),
	}

	if err := gs.loadAll(); err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	return gs, nil
}

// Persister returns the dual persister for direct CSV access (testing).
func (gs *GlobalState) Persister() *DualPersister {
	return gs.persister
}

// loadAll reads every entity CSV and deduplicates by ID.
func (gs *GlobalState) loadAll() error {
	if err := gs.loadUsers(); err != nil {
		return fmt.Errorf("users: %w", err)
	}
	if err := gs.loadStrategies(); err != nil {
		return fmt.Errorf("strategies: %w", err)
	}
	if err := gs.loadStrategyAssets(); err != nil {
		return fmt.Errorf("strategy_assets: %w", err)
	}
	if err := gs.loadUserStrategies(); err != nil {
		return fmt.Errorf("user_strategies: %w", err)
	}
	if err := gs.loadUserOrders(); err != nil {
		return fmt.Errorf("user_orders: %w", err)
	}
	if err := gs.loadLeverageConfigs(); err != nil {
		return fmt.Errorf("leverage_configs: %w", err)
	}
	if err := gs.loadExchangeSymbolFilters(); err != nil {
		return fmt.Errorf("exchange_symbol_filters: %w", err)
	}
	if err := gs.loadUprunningOrders(); err != nil {
		return fmt.Errorf("uprunning_orders: %w", err)
	}
	if err := gs.loadUserOrderPositions(); err != nil {
		return fmt.Errorf("user_order_positions: %w", err)
	}
	if err := gs.loadUserPositions(); err != nil {
		return fmt.Errorf("user_positions: %w", err)
	}
	return nil
}

// loadUsers reads users.csv, deduplicates by ID (latest updated_at wins).
func (gs *GlobalState) loadUsers() error {
	records, err := gs.persister.ReadAllCSV("users.csv")
	if err != nil {
		return err
	}

	latest := make(map[uint64]*order.User)
	for _, rec := range records {
		u, err := parseUserFromRecord(rec)
		if err != nil {
			log.Printf("warn: skipping invalid user record: %v", err)
			continue
		}
		existing := latest[u.ID]
		if existing == nil || u.UpdatedAt.After(existing.UpdatedAt) {
			latest[u.ID] = u
		}
		gs.updateIDCounter("users.csv", u.ID)
	}
	gs.Users = latest
	return nil
}

// loadStrategies reads strategies.csv.
func (gs *GlobalState) loadStrategies() error {
	records, err := gs.persister.ReadAllCSV("strategies.csv")
	if err != nil {
		return err
	}

	latest := make(map[uint64]*order.Strategy)
	for _, rec := range records {
		s, err := parseStrategyFromRecord(rec)
		if err != nil {
			log.Printf("warn: skipping invalid strategy record: %v", err)
			continue
		}
		existing := latest[s.ID]
		if existing == nil || s.UpdatedAt.After(existing.UpdatedAt) {
			latest[s.ID] = s
		}
		gs.updateIDCounter("strategies.csv", s.ID)
	}
	gs.Strategies = latest
	return nil
}

// loadStrategyAssets reads strategy_assets.csv.
func (gs *GlobalState) loadStrategyAssets() error {
	records, err := gs.persister.ReadAllCSV("strategy_assets.csv")
	if err != nil {
		return err
	}

	latest := make(map[uint64]*order.StrategyAsset)
	for _, rec := range records {
		asset, err := parseStrategyAssetFromRecord(rec)
		if err != nil {
			log.Printf("warn: skipping invalid strategy_asset record: %v", err)
			continue
		}
		existing := latest[asset.ID]
		if existing == nil || asset.UpdatedAt.After(existing.UpdatedAt) {
			latest[asset.ID] = asset
		}
		gs.updateIDCounter("strategy_assets.csv", asset.ID)
	}
	gs.StrategyAssets = latest
	return nil
}

// loadUserStrategies reads user_strategies.csv.
func (gs *GlobalState) loadUserStrategies() error {
	records, err := gs.persister.ReadAllCSV("user_strategies.csv")
	if err != nil {
		return err
	}

	latest := make(map[uint64]*order.UserStrategy)
	for _, rec := range records {
		us, err := parseUserStrategyFromRecord(rec)
		if err != nil {
			log.Printf("warn: skipping invalid user_strategy record: %v", err)
			continue
		}
		existing := latest[us.ID]
		if existing == nil || us.UpdatedAt.After(existing.UpdatedAt) {
			latest[us.ID] = us
		}
		gs.updateIDCounter("user_strategies.csv", us.ID)
	}
	gs.UserStrategies = latest
	return nil
}

// loadUserOrders reads user_orders.csv.
func (gs *GlobalState) loadUserOrders() error {
	records, err := gs.persister.ReadAllCSV("user_orders.csv")
	if err != nil {
		return err
	}

	latest := make(map[uint64]*order.UserOrder)
	for _, rec := range records {
		o, err := parseUserOrderFromRecord(rec)
		if err != nil {
			log.Printf("warn: skipping invalid user_order record: %v", err)
			continue
		}
		if o.ID == 0 {
			log.Printf("warn: skipping zero-id user_order record")
			continue
		}
		existing := latest[o.ID]
		// CSV takes precedence if UpdatedAt is newer, or if same UpdatedAt but terminal status
		shouldReplace := existing == nil ||
			o.UpdatedAt.After(existing.UpdatedAt) ||
			(o.UpdatedAt.Equal(existing.UpdatedAt) && isTerminalOrderStatusInt(o.Status) && !isTerminalOrderStatusInt(existing.Status))
		if shouldReplace {
			latest[o.ID] = o
		}
		gs.updateIDCounter("user_orders.csv", o.ID)
	}
	gs.UserOrders = latest
	return nil
}

// loadLeverageConfigs reads leverage_configs.csv.
func (gs *GlobalState) loadLeverageConfigs() error {
	records, err := gs.persister.ReadAllCSV("leverage_configs.csv")
	if err != nil {
		return err
	}

	latest := make(map[uint64]*order.LeverageConfig)
	for _, rec := range records {
		lc, err := parseLeverageConfigFromRecord(rec)
		if err != nil {
			log.Printf("warn: skipping invalid leverage_config record: %v", err)
			continue
		}
		if lc.ID == 0 {
			log.Printf("warn: skipping zero-id leverage_config record")
			continue
		}
		existing := latest[lc.ID]
		if existing == nil || lc.UpdatedAt.After(existing.UpdatedAt) {
			latest[lc.ID] = lc
		}
		gs.updateIDCounter("leverage_configs.csv", lc.ID)
	}
	gs.LeverageConfigs = latest
	return nil
}

// loadExchangeSymbolFilters reads exchange_symbol_filters.csv.
func (gs *GlobalState) loadExchangeSymbolFilters() error {
	records, err := gs.persister.ReadAllCSV("exchange_symbol_filters.csv")
	if err != nil {
		return err
	}

	latest := make(map[uint64]*order.ExchangeSymbolFilter)
	for _, rec := range records {
		filter, err := parseExchangeSymbolFilterFromRecord(rec)
		if err != nil {
			log.Printf("warn: skipping invalid exchange_symbol_filter record: %v", err)
			continue
		}
		latest[uint64(filter.ID)] = filter
		gs.updateIDCounter("exchange_symbol_filters.csv", uint64(filter.ID))
	}
	gs.ExchangeSymbolFilters = latest
	return nil
}

// loadUprunningOrders reads uprunning_orders.csv.
func (gs *GlobalState) loadUprunningOrders() error {
	return gs.reloadUprunningOrders()
}

// reloadUprunningOrders re-reads uprunning_orders.csv from disk, updating the in-memory map.
// This ensures cross-service visibility when user_order_service creates orders.
func (gs *GlobalState) reloadUprunningOrders() error {
	records, err := gs.persister.ReadAllCSV("uprunning_orders.csv")
	if err != nil {
		return err
	}

	gs.rw.Lock()
	defer gs.rw.Unlock()

	// Start with current in-memory state to preserve unflushed updates (e.g., WS FILLED)
	latest := make(map[uint64]*order.UprunningOrder)
	for id, uo := range gs.UprunningOrders {
		latest[id] = uo
	}

	for _, rec := range records {
		uo, err := parseUprunningOrderFromRecord(rec)
		if err != nil {
			log.Printf("warn: skipping invalid uprunning_order record: %v", err)
			continue
		}
		existing := latest[uo.ID]
		// CSV takes precedence if:
		// 1. Missing in memory
		// 2. CSV UpdatedAt is newer
		// 3. UpdatedAt equal but CSV has terminal status (FILLED/CANCELLED/failed)
		shouldReplace := existing == nil ||
			uo.UpdatedAt.After(existing.UpdatedAt) ||
			(uo.UpdatedAt.Equal(existing.UpdatedAt) && isTerminalOrderStatus(uo.ExchangeOrderStatus) && !isTerminalOrderStatus(existing.ExchangeOrderStatus))
		if shouldReplace {
			latest[uo.ID] = uo
		}
		gs.updateIDCounter("uprunning_orders.csv", uo.ID)
	}

	gs.UprunningOrders = latest
	return nil
}

// isTerminalOrderStatus returns true if the order status is terminal (no further changes).
func isTerminalOrderStatus(status string) bool {
	switch status {
	case "FILLED", "CANCELLED", "cancelled", "failed":
		return true
	default:
		return false
	}
}

// isDeleted returns true if the position/order is deleted (closed).
func isDeleted(deleted int) bool {
	return deleted == 1
}

// uint64InSlice checks if v is present in slice.
func uint64InSlice(v uint64, slice []uint64) bool {
	for _, s := range slice {
		if s == v {
			return true
		}
	}
	return false
}

// matchCloseTime reports whether a position's close_time satisfies the [from,to] range.
// from/to are inclusive; nil bounds are unbounded on that side. Returns false (excluded)
// when any bound is set but the position has no close_time (i.e. still active).
// When both bounds are nil the filter is inactive and the position is always matched.
func matchCloseTime(closeTime *time.Time, from, to *time.Time) bool {
	if from == nil && to == nil {
		return true
	}
	if closeTime == nil {
		return false
	}
	if from != nil && closeTime.Before(*from) {
		return false
	}
	if to != nil && closeTime.After(*to) {
		return false
	}
	return true
}

// isTerminalOrderStatusInt returns true if the user_order status is terminal (FILLED or FAILED).
func isTerminalOrderStatusInt(status int) bool {
	return status == 2 || status == 3 // 2=FILLED, 3=FAILED
}

// loadUserOrderPositions reads user_order_positions.csv.
func (gs *GlobalState) loadUserOrderPositions() error {
	records, err := gs.persister.ReadAllCSV("user_order_positions.csv")
	if err != nil {
		return err
	}

	latest := make(map[uint64]*order.UserOrderPosition)
	for _, rec := range records {
		pos, err := parseUserOrderPositionFromRecord(rec)
		if err != nil {
			log.Printf("warn: skipping invalid user_order_position record: %v", err)
			continue
		}
		existing := latest[pos.ID]
		// CSV takes precedence if UpdatedAt is newer, or if same UpdatedAt but deleted (closed)
		shouldReplace := existing == nil ||
			pos.UpdatedAt.After(existing.UpdatedAt) ||
			(pos.UpdatedAt.Equal(existing.UpdatedAt) && isDeleted(pos.Deleted) && !isDeleted(existing.Deleted))
		if shouldReplace {
			latest[pos.ID] = pos
		}
		gs.updateIDCounter("user_order_positions.csv", pos.ID)
	}
	gs.UserOrderPositions = latest
	return nil
}

func (gs *GlobalState) loadUserPositions() error {
	records, err := gs.persister.ReadAllCSV("user_positions.csv")
	if err != nil {
		return err
	}

	latest := make(map[uint64]*order.UserPosition)
	for _, rec := range records {
		pos, err := parseUserPositionFromRecord(rec)
		if err != nil {
			log.Printf("warn: skipping invalid user_position record: %v", err)
			continue
		}
		if pos.ID == 0 {
			log.Printf("warn: skipping zero-id user_position record")
			continue
		}
		existing := latest[pos.ID]
		// CSV takes precedence if UpdatedAt is newer, or if same UpdatedAt but deleted (closed)
		shouldReplace := existing == nil ||
			pos.UpdatedAt.After(existing.UpdatedAt) ||
			(pos.UpdatedAt.Equal(existing.UpdatedAt) && isDeleted(pos.Deleted) && !isDeleted(existing.Deleted))
		if shouldReplace {
			latest[pos.ID] = pos
		}
		gs.updateIDCounter("user_positions.csv", pos.ID)
	}
	gs.UserPositions = latest
	return nil
}

// nextID returns the next unique ID for a specific table (thread-safe).
func (gs *GlobalState) nextID(tableName string) uint64 {
	value, _ := gs.idCounters.LoadOrStore(tableName, new(uint64))
	counter := value.(*uint64)
	return atomic.AddUint64(counter, 1)
}

// updateIDCounter updates the ID counter for a table if the given ID is greater.
// This is used during loading to ensure the counter starts from the max existing ID.
func (gs *GlobalState) updateIDCounter(tableName string, id uint64) {
	value, _ := gs.idCounters.LoadOrStore(tableName, new(uint64))
	counter := value.(*uint64)
	for {
		current := atomic.LoadUint64(counter)
		if id <= current {
			return
		}
		if atomic.CompareAndSwapUint64(counter, current, id) {
			return
		}
	}
}

// tickVersion atomically increments the version counter.
func (gs *GlobalState) tickVersion() {
	atomic.AddInt64(&gs.Version, 1)
}

// ============================================
// StateRepository - CRUD operations
// ============================================

// StateRepository provides typed CRUD over GlobalState.
type StateRepository struct {
	gs           *GlobalState
	lastSyncTime time.Time     // last time user_order_positions was synced from CSV
	syncInterval time.Duration // minimum interval between syncs (default 5s)
	syncMu       sync.Mutex    // protects lastSyncTime during concurrent access
}

type UserOrderPositionFilter struct {
	UserStrategyID  uint64
	UserStrategyIDs []uint64    // 按多个 user_strategy_id 过滤
	UserID          uint64      // 按用户 ID 过滤
	UserIDs         []uint64    // 按多个用户 ID 过滤
	Side            *order.Side
	Active          *bool
	Asset           string
	Exchange        string     // 按交易所过滤
	PosType         *order.PosType
	CreatedAtFrom   *time.Time // 创建时间起始（含）
	CreatedAtTo     *time.Time // 创建时间截止（含）
	CloseTimeFrom   *time.Time // 平仓时间起始（含）；传入任一 close 参数时排除未平仓仓位
	CloseTimeTo     *time.Time // 平仓时间截止（含）
}

// NewStateRepository creates a new repository backed by the given state.
func NewStateRepository(gs *GlobalState) *StateRepository {
	return &StateRepository{
		gs:           gs,
		lastSyncTime: time.Now(), // Initialize to prevent immediate reload
	}
}

// Persister returns the dual persister for direct CSV access (testing/ws monitor).
func (r *StateRepository) Persister() *DualPersister {
	return r.gs.persister
}

// ----- Users -----

// GetUserByID retrieves a user by ID.
func (r *StateRepository) GetUserByID(id uint64) (*order.User, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	u, ok := r.gs.Users[id]
	if !ok {
		return nil, fmt.Errorf("user %d not found", id)
	}
	return u, nil
}

// CreateUser creates a new user and returns its ID.
func (r *StateRepository) CreateUser(u *order.User) uint64 {
	r.gs.mu.Lock()
	id := r.gs.nextID("users.csv")
	r.gs.mu.Unlock()

	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	u.ID = id
	u.UpdatedAt = time.Now()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = u.UpdatedAt
	}

	r.gs.Users[id] = u
	r.gs.tickVersion()

	// Snapshot for async write to avoid race with caller mutations
	snap := *u
	r.gs.writeWg.Add(1)
	go func(snapshot order.User) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("users.csv", &snapshot); err != nil {
			log.Printf("error appending user: %v", err)
		}
	}(snap)

	return id
}

// ListUsers returns all users.
func (r *StateRepository) ListUsers() []*order.User {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.User, 0, len(r.gs.Users))
	for _, u := range r.gs.Users {
		result = append(result, u)
	}
	return result
}

// FindUserIDsByName finds user IDs by name and optionally exchange.
// If exchange is empty, returns all user IDs matching the name.
// Returns error if no user found.
func (r *StateRepository) FindUserIDsByName(userName, exchange string) ([]uint64, error) {
	users := r.ListUsers()
	var userIDs []uint64
	for _, user := range users {
		if user.Name == userName {
			if exchange == "" || user.Exchange == exchange {
				userIDs = append(userIDs, user.ID)
			}
		}
	}
	if len(userIDs) == 0 {
		return nil, fmt.Errorf("user '%s' not found", userName)
	}
	return userIDs, nil
}

// FindUserIDByName finds a single user ID by name and exchange.
// Returns error if user not found or if multiple users match (when exchange is empty).
func (r *StateRepository) FindUserIDByName(userName, exchange string) (uint64, error) {
	userIDs, err := r.FindUserIDsByName(userName, exchange)
	if err != nil {
		return 0, err
	}
	if len(userIDs) > 1 && exchange == "" {
		return 0, fmt.Errorf("multiple users found with name '%s', please specify exchange", userName)
	}
	return userIDs[0], nil
}

// ListUserOrderPositionsByUserName returns order positions filtered by user name and optional exchange.
func (r *StateRepository) ListUserOrderPositionsByUserName(userName, exchange string) []*order.UserOrderPosition {
	userIDs, err := r.FindUserIDsByName(userName, exchange)
	if err != nil {
		return []*order.UserOrderPosition{}
	}
	return r.ListUserOrderPositionsByFilter(UserOrderPositionFilter{
		UserIDs:  userIDs,
		Exchange: exchange,
	})
}

// ----- Strategies -----

// GetStrategyByID retrieves a strategy by ID.
func (r *StateRepository) GetStrategyByID(id uint64) (*order.Strategy, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	s, ok := r.gs.Strategies[id]
	if !ok {
		return nil, fmt.Errorf("strategy %d not found", id)
	}
	return s, nil
}

// CreateStrategy creates a new strategy and returns its ID.
func (r *StateRepository) CreateStrategy(s *order.Strategy) uint64 {
	r.gs.mu.Lock()
	id := r.gs.nextID("strategies.csv")
	r.gs.mu.Unlock()

	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	s.ID = id
	s.UpdatedAt = time.Now()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = s.UpdatedAt
	}

	r.gs.Strategies[id] = s
	r.gs.tickVersion()

	snap := *s
	r.gs.writeWg.Add(1)
	go func(snapshot order.Strategy) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("strategies.csv", &snapshot); err != nil {
			log.Printf("error appending strategy: %v", err)
		}
	}(snap)

	return id
}

// ListStrategiesByType returns strategies matching the given type.
func (r *StateRepository) ListStrategiesByType(strategyType string) []*order.Strategy {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.Strategy, 0)
	for _, s := range r.gs.Strategies {
		if s.StrategyType == strategyType {
			result = append(result, s)
		}
	}
	return result
}

// ListStrategies returns all strategies.
func (r *StateRepository) ListStrategies() []*order.Strategy {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.Strategy, 0, len(r.gs.Strategies))
	for _, s := range r.gs.Strategies {
		result = append(result, s)
	}
	return result
}

// GetStrategyByName retrieves a strategy by its unique name.
func (r *StateRepository) GetStrategyByName(name string) (*order.Strategy, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	for _, s := range r.gs.Strategies {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("strategy %s not found", name)
}

// UpdateStrategy updates an existing strategy in-place.
func (r *StateRepository) UpdateStrategy(s *order.Strategy) error {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	if _, ok := r.gs.Strategies[s.ID]; !ok {
		return fmt.Errorf("strategy %d not found", s.ID)
	}

	s.UpdatedAt = time.Now()
	r.gs.Strategies[s.ID] = s
	r.gs.tickVersion()

	snap := *s
	r.gs.writeWg.Add(1)
	go func(snapshot order.Strategy) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("strategies.csv", &snapshot); err != nil {
			log.Printf("error appending strategy update: %v", err)
		}
	}(snap)

	return nil
}

// ----- StrategyAssets -----

// CreateStrategyAsset creates a strategy asset and returns its ID.
func (r *StateRepository) CreateStrategyAsset(asset *order.StrategyAsset) uint64 {
	r.gs.mu.Lock()
	id := r.gs.nextID("strategy_assets.csv")
	r.gs.mu.Unlock()

	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	asset.ID = id
	asset.UpdatedAt = time.Now()
	if asset.CreatedAt.IsZero() {
		asset.CreatedAt = asset.UpdatedAt
	}

	r.gs.StrategyAssets[id] = asset
	r.gs.tickVersion()

	snap := *asset
	r.gs.writeWg.Add(1)
	go func(snapshot order.StrategyAsset) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("strategy_assets.csv", &snapshot); err != nil {
			log.Printf("error appending strategy_asset: %v", err)
		}
	}(snap)

	return id
}

// GetStrategyAssetByNameAssetStrategy finds a strategy asset by natural key.
func (r *StateRepository) GetStrategyAssetByNameAssetStrategy(name, asset string, strategyID uint64) (*order.StrategyAsset, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	for _, strategyAsset := range r.gs.StrategyAssets {
		if strategyAsset.Name == name && strategyAsset.Asset == asset && strategyAsset.StrategyID == strategyID {
			return strategyAsset, nil
		}
	}
	return nil, fmt.Errorf("strategy_asset for name=%s asset=%s strategy=%d not found", name, asset, strategyID)
}

// ListStrategyAssetsByStrategy returns all assets for a strategy.
func (r *StateRepository) ListStrategyAssetsByStrategy(strategyID uint64) []*order.StrategyAsset {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.StrategyAsset, 0)
	for _, strategyAsset := range r.gs.StrategyAssets {
		if strategyAsset.StrategyID == strategyID {
			result = append(result, strategyAsset)
		}
	}
	return result
}

// ----- UserStrategies -----

// GetUserStrategyByID retrieves a user strategy by ID.
func (r *StateRepository) GetUserStrategyByID(id uint64) (*order.UserStrategy, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	us, ok := r.gs.UserStrategies[id]
	if !ok {
		return nil, fmt.Errorf("user_strategy %d not found", id)
	}
	return us, nil
}

// ListUserStrategies returns all user strategies.
func (r *StateRepository) ListUserStrategies() []*order.UserStrategy {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.UserStrategy, 0, len(r.gs.UserStrategies))
	for _, us := range r.gs.UserStrategies {
		result = append(result, us)
	}
	return result
}

// CreateUserStrategy creates a new user strategy and returns its ID.
func (r *StateRepository) CreateUserStrategy(us *order.UserStrategy) uint64 {
	r.gs.mu.Lock()
	id := r.gs.nextID("user_strategies.csv")
	r.gs.mu.Unlock()

	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	us.ID = id
	us.UpdatedAt = time.Now()
	if us.CreatedAt.IsZero() {
		us.CreatedAt = us.UpdatedAt
	}

	r.gs.UserStrategies[id] = us
	r.gs.tickVersion()

	snap := *us
	r.gs.writeWg.Add(1)
	go func(snapshot order.UserStrategy) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("user_strategies.csv", &snapshot); err != nil {
			log.Printf("error appending user_strategy: %v", err)
		}
	}(snap)

	return id
}

// UpdateUserStrategy updates an existing user strategy in-place (same ID).
func (r *StateRepository) UpdateUserStrategy(us *order.UserStrategy) error {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	if _, ok := r.gs.UserStrategies[us.ID]; !ok {
		return fmt.Errorf("user_strategy %d not found", us.ID)
	}

	us.UpdatedAt = time.Now()
	r.gs.UserStrategies[us.ID] = us
	r.gs.tickVersion()

	snap := *us
	r.gs.writeWg.Add(1)
	go func(snapshot order.UserStrategy) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("user_strategies.csv", &snapshot); err != nil {
			log.Printf("error appending user_strategy update: %v", err)
		}
	}(snap)

	return nil
}

// ListUserStrategiesByUser returns all strategies for a given user.
func (r *StateRepository) ListUserStrategiesByUser(userID uint64) []*order.UserStrategy {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.UserStrategy, 0)
	for _, us := range r.gs.UserStrategies {
		if us.UserID == userID {
			result = append(result, us)
		}
	}
	return result
}

// FindUserStrategyIDsByName returns all user_strategy IDs matching the given name (cross-user).
func (r *StateRepository) FindUserStrategyIDsByName(name string) []uint64 {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]uint64, 0)
	for _, us := range r.gs.UserStrategies {
		if us.Name == name {
			result = append(result, us.ID)
		}
	}
	return result
}

// FindUserStrategyIDsByUserAndName returns all user_strategy IDs matching the given userID and name.
func (r *StateRepository) FindUserStrategyIDsByUserAndName(userID uint64, name string) []uint64 {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]uint64, 0)
	for _, us := range r.gs.UserStrategies {
		if us.UserID == userID && us.Name == name {
			result = append(result, us.ID)
		}
	}
	return result
}

// GetUserStrategyByUserNameStrategy retrieves a user strategy by user, unique strategy name, and strategy ID.
func (r *StateRepository) GetUserStrategyByUserNameStrategy(userID uint64, name string, strategyID uint64) (*order.UserStrategy, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	for _, us := range r.gs.UserStrategies {
		if us.UserID == userID && us.Name == name && us.StrategyID == strategyID {
			return us, nil
		}
	}
	return nil, fmt.Errorf("user_strategy for user=%d name=%s strategy=%d not found", userID, name, strategyID)
}

// GetUserOrderByID retrieves a user order by ID.
func (r *StateRepository) GetUserOrderByID(id uint64) (*order.UserOrder, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	o, ok := r.gs.UserOrders[id]
	if !ok {
		return nil, fmt.Errorf("user_order %d not found", id)
	}
	return o, nil
}

// CreateUserOrder creates a new user order and returns its ID.
func (r *StateRepository) CreateUserOrder(o *order.UserOrder) uint64 {
	r.gs.mu.Lock()
	id := r.gs.nextID("user_orders.csv")
	r.gs.mu.Unlock()

	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	o.ID = id
	o.UpdatedAt = time.Now()
	if o.CreatedAt.IsZero() {
		o.CreatedAt = o.UpdatedAt
	}

	r.gs.UserOrders[id] = o
	r.gs.tickVersion()

	snap := *o
	r.gs.writeWg.Add(1)
	go func(snapshot order.UserOrder) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("user_orders.csv", &snapshot); err != nil {
			log.Printf("error appending user_order: %v", err)
		}
	}(snap)

	return id
}

// UpdateUserOrder updates an entire user order and persists to CSV.
func (r *StateRepository) UpdateUserOrder(o *order.UserOrder) error {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	if _, ok := r.gs.UserOrders[o.ID]; !ok {
		return fmt.Errorf("user_order %d not found", o.ID)
	}
	o.UpdatedAt = time.Now()
	r.gs.UserOrders[o.ID] = o
	r.gs.tickVersion()

	snap := *o
	r.gs.writeWg.Add(1)
	go func(snap order.UserOrder) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("user_orders.csv", &snap); err != nil {
			log.Printf("error appending user_order update: %v", err)
		}
	}(snap)
	return nil
}

// UpdateUserOrderStatus updates a user order's status and persists to CSV.
func (r *StateRepository) UpdateUserOrderStatus(id uint64, status int, finishedAt *time.Time, updatedAt time.Time) error {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	o, ok := r.gs.UserOrders[id]
	if !ok {
		return fmt.Errorf("user_order %d not found", id)
	}
	o.Status = status
	o.FinishedAt = finishedAt
	o.UpdatedAt = updatedAt
	r.gs.tickVersion()

	snap := *o
	r.gs.writeWg.Add(1)
	go func(snap order.UserOrder) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("user_orders.csv", &snap); err != nil {
			log.Printf("error appending user_order status update: %v", err)
		}
	}(snap)
	return nil
}

// CountUserOrdersByStrategyAndStatus counts user orders by strategy ID and status.
func (r *StateRepository) CountUserOrdersByStrategyAndStatus(strategyID uint64, status int) int {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	count := 0
	for _, o := range r.gs.UserOrders {
		if o.UserStrategyID == strategyID && o.Status == status {
			count++
		}
	}
	return count
}

// CountActivePositionsByStrategy counts active positions (deleted=0) by strategy ID.
func (r *StateRepository) CountActivePositionsByStrategy(strategyID uint64) int {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	count := 0
	for _, p := range r.gs.UserOrderPositions {
		if p.UserStrategyID == strategyID && p.Deleted == 0 {
			count++
		}
	}
	return count
}

// ----- LeverageConfigs -----

// GetLeverageConfigByID retrieves a leverage config by ID.
func (r *StateRepository) GetLeverageConfigByID(id uint64) (*order.LeverageConfig, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	lc, ok := r.gs.LeverageConfigs[id]
	if !ok {
		return nil, fmt.Errorf("leverage_config %d not found", id)
	}
	return lc, nil
}

// CreateLeverageConfig creates a new leverage config and returns its ID.
func (r *StateRepository) CreateLeverageConfig(lc *order.LeverageConfig) uint64 {
	r.gs.mu.Lock()
	id := r.gs.nextID("leverage_configs.csv")
	r.gs.mu.Unlock()

	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	lc.ID = id
	lc.UpdatedAt = time.Now()
	if lc.CreatedAt.IsZero() {
		lc.CreatedAt = lc.UpdatedAt
	}

	r.gs.LeverageConfigs[id] = lc
	r.gs.tickVersion()

	snap := *lc
	r.gs.writeWg.Add(1)
	go func(snapshot order.LeverageConfig) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("leverage_configs.csv", &snapshot); err != nil {
			log.Printf("error appending leverage_config: %v", err)
		}
	}(snap)

	return id
}

// FindLeverageConfig finds a leverage config by natural key.
func (r *StateRepository) FindLeverageConfig(userID uint64, asset string, posType order.PosType, exchange string) (*order.LeverageConfig, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	for _, lc := range r.gs.LeverageConfigs {
		if lc.UserID == userID && lc.Asset == asset && lc.PosType == posType && lc.Exchange == exchange {
			return lc, nil
		}
	}
	return nil, fmt.Errorf("leverage_config not found")
}

// UpsertLeverageConfig creates or updates a leverage config by natural key.
func (r *StateRepository) UpsertLeverageConfig(lc *order.LeverageConfig) uint64 {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()

	for _, existing := range r.gs.LeverageConfigs {
		if existing.UserID == lc.UserID && existing.Asset == lc.Asset && existing.PosType == lc.PosType && existing.Exchange == lc.Exchange {
			if existing.Quote == lc.Quote && existing.Leverage == lc.Leverage && existing.Status == lc.Status {
				return existing.ID
			}
			existing.Quote = lc.Quote
			existing.Leverage = lc.Leverage
			existing.Status = lc.Status
			existing.UpdatedAt = time.Now()
			r.gs.LeverageConfigs[existing.ID] = existing
			r.gs.tickVersion()

			snap := *existing
			r.gs.writeWg.Add(1)
			go func(snapshot order.LeverageConfig) {
				defer r.gs.writeWg.Done()
				if err := r.gs.persister.AppendRow("leverage_configs.csv", &snapshot); err != nil {
					log.Printf("error appending leverage_config update: %v", err)
				}
			}(snap)
			return existing.ID
		}
	}

	id := r.gs.nextID("leverage_configs.csv")
	lc.ID = id
	lc.UpdatedAt = time.Now()
	if lc.CreatedAt.IsZero() {
		lc.CreatedAt = lc.UpdatedAt
	}
	r.gs.LeverageConfigs[id] = lc
	r.gs.tickVersion()

	snap := *lc
	r.gs.writeWg.Add(1)
	go func(snapshot order.LeverageConfig) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("leverage_configs.csv", &snapshot); err != nil {
			log.Printf("error appending leverage_config: %v", err)
		}
	}(snap)
	return id
}

// ----- ExchangeSymbolFilters -----

// CreateExchangeSymbolFilter creates a new exchange symbol filter and returns its ID.
func (r *StateRepository) CreateExchangeSymbolFilter(filter *order.ExchangeSymbolFilter) uint64 {
	r.gs.mu.Lock()
	id := r.gs.nextID("exchange_symbol_filters.csv")
	r.gs.mu.Unlock()

	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	filter.ID = uint(id)
	r.gs.ExchangeSymbolFilters[id] = filter
	r.gs.tickVersion()

	snap := *filter
	r.gs.writeWg.Add(1)
	go func(snapshot order.ExchangeSymbolFilter) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("exchange_symbol_filters.csv", &snapshot); err != nil {
			log.Printf("error appending exchange_symbol_filter: %v", err)
		}
	}(snap)

	return id
}

// ReplaceExchangeSymbolFilters replaces all exchange symbol filters in memory and on disk.
func (r *StateRepository) ReplaceExchangeSymbolFilters(filters []*order.ExchangeSymbolFilter) error {
	replaced := make(map[uint64]*order.ExchangeSymbolFilter, len(filters))
	entities := make([]interface{}, 0, len(filters))
	for i, filter := range filters {
		if filter == nil {
			continue
		}
		id := uint64(i + 1)
		copyFilter := *filter
		copyFilter.ID = uint(id)
		replaced[id] = &copyFilter
		entities = append(entities, &copyFilter)
	}

	// Use atomic Compact instead of WriteAllCSV to prevent corruption on kill
	if err := r.gs.persister.Compact("exchange_symbol_filters.csv", entities); err != nil {
		return err
	}

	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	r.gs.ExchangeSymbolFilters = replaced
	r.gs.tickVersion()
	return nil
}

// ListExchangeSymbolFilters returns filters matching exchange, position type, and symbol.
func (r *StateRepository) ListExchangeSymbolFilters(exchange string, posType order.PosType, symbol string) []*order.ExchangeSymbolFilter {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.ExchangeSymbolFilter, 0)
	for _, filter := range r.gs.ExchangeSymbolFilters {
		if filter.Exchange == exchange && filter.PosType == posType && filter.Symbol == symbol {
			result = append(result, filter)
		}
	}
	return result
}

// ----- UprunningOrders (Exchange Service owned) -----

// GetUprunningOrderByID retrieves a running order by ID.
func (r *StateRepository) GetUprunningOrderByID(id uint64) (*order.UprunningOrder, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	uo, ok := r.gs.UprunningOrders[id]
	if !ok {
		return nil, fmt.Errorf("uprunning_order %d not found", id)
	}
	return uo, nil
}

// CreateUprunningOrder creates a new running order.
func (r *StateRepository) CreateUprunningOrder(uo *order.UprunningOrder) uint64 {
	r.gs.mu.Lock()
	id := r.gs.nextID("uprunning_orders.csv")
	r.gs.mu.Unlock()

	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	uo.ID = id
	uo.UpdatedAt = time.Now()
	if uo.CreatedAt.IsZero() {
		uo.CreatedAt = uo.UpdatedAt
	}

	r.gs.UprunningOrders[id] = uo
	r.gs.tickVersion()

	snap := *uo
	r.gs.writeWg.Add(1)
	go func(snapshot order.UprunningOrder) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("uprunning_orders.csv", &snapshot); err != nil {
			log.Printf("error appending uprunning_order: %v", err)
		}
	}(snap)

	return id
}

// UpdateUprunningOrderStatus updates an order status.
func (r *StateRepository) UpdateUprunningOrderStatus(id uint64, status string, updateTime *time.Time) error {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	uo, ok := r.gs.UprunningOrders[id]
	if !ok {
		return fmt.Errorf("uprunning_order %d not found", id)
	}
	if uo.ExchangeOrderStatus == status {
		return nil
	}
	uo.ExchangeOrderStatus = status
	if updateTime != nil {
		uo.ExchangeUpdateTime = updateTime
	}
	uo.UpdatedAt = time.Now()
	r.gs.tickVersion()

	snap := *uo
	r.gs.writeWg.Add(1)
	go func(snapshot order.UprunningOrder) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("uprunning_orders.csv", &snapshot); err != nil {
			log.Printf("error appending uprunning_order update: %v", err)
		}
	}(snap)

	return nil
}

// UpdateUprunningOrderFilled updates a filled order status and its executed fill fields.
func (r *StateRepository) UpdateUprunningOrderFilled(id uint64, avgPrice, executedQty float64, updateTime *time.Time) error {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	uo, ok := r.gs.UprunningOrders[id]
	if !ok {
		return fmt.Errorf("uprunning_order %d not found", id)
	}
	uo.ExchangeOrderStatus = "FILLED"
	if avgPrice > 0 {
		uo.ExchangeOrderPrice = avgPrice
	}
	uo.ExchangeOrderQty = executedQty
	if updateTime != nil {
		uo.ExchangeUpdateTime = updateTime
	}
	uo.UpdatedAt = time.Now()
	r.gs.tickVersion()

	snap := *uo
	r.gs.writeWg.Add(1)
	go func(snapshot order.UprunningOrder) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("uprunning_orders.csv", &snapshot); err != nil {
			log.Printf("error appending uprunning_order filled update: %v", err)
		}
	}(snap)

	return nil
}

// UpdateUprunningOrder updates a full uprunning order and persists to CSV.
func (r *StateRepository) UpdateUprunningOrder(uo *order.UprunningOrder) error {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	if _, ok := r.gs.UprunningOrders[uo.ID]; !ok {
		return fmt.Errorf("uprunning_order %d not found", uo.ID)
	}
	uo.UpdatedAt = time.Now()
	r.gs.UprunningOrders[uo.ID] = uo
	r.gs.tickVersion()

	snap := *uo
	r.gs.writeWg.Add(1)
	go func(snapshot order.UprunningOrder) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("uprunning_orders.csv", &snapshot); err != nil {
			log.Printf("error appending uprunning_order update: %v", err)
		}
	}(snap)

	return nil
}

// ListUprunningOrders returns all running orders.
func (r *StateRepository) ListUprunningOrders() []*order.UprunningOrder {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.UprunningOrder, 0, len(r.gs.UprunningOrders))
	for _, uo := range r.gs.UprunningOrders {
		result = append(result, uo)
	}
	return result
}

// FindUprunningOrderByExchangeID finds a running order by its exchange order ID.
// If not found in memory, reloads uprunning_orders.csv from disk to pick up
// orders created by user_order_service (which has a separate memory state).
func (r *StateRepository) FindUprunningOrderByExchangeID(exchangeOrderID uint64) (*order.UprunningOrder, error) {
	r.gs.rw.RLock()
	for _, uo := range r.gs.UprunningOrders {
		if uo.ExchangeOrderID == exchangeOrderID {
			r.gs.rw.RUnlock()
			return uo, nil
		}
	}
	r.gs.rw.RUnlock()

	// Not found in memory — reload uprunning_orders.csv from disk
	// to pick up orders created by user_order_service.
	if err := r.gs.reloadUprunningOrders(); err != nil {
		return nil, fmt.Errorf("reload uprunning_orders: %w", err)
	}

	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	for _, uo := range r.gs.UprunningOrders {
		if uo.ExchangeOrderID == exchangeOrderID {
			return uo, nil
		}
	}
	return nil, fmt.Errorf("uprunning_order with exchange_order_id=%d not found", exchangeOrderID)
}

// ListUprunningOrdersByExchangeStatus returns all uprunning_orders with the given exchange status.
func (r *StateRepository) ListUprunningOrdersByExchangeStatus(status string) []*order.UprunningOrder {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	var result []*order.UprunningOrder
	for _, uo := range r.gs.UprunningOrders {
		if uo.ExchangeOrderStatus == status {
			result = append(result, uo)
		}
	}
	return result
}

// ----- UserOrderPositions (Exchange Service owned) -----

// GetUserOrderPositionByID retrieves a position by ID.
func (r *StateRepository) GetUserOrderPositionByID(id uint64) (*order.UserOrderPosition, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	pos, ok := r.gs.UserOrderPositions[id]
	if !ok {
		return nil, fmt.Errorf("user_order_position %d not found", id)
	}
	return pos, nil
}

// CreateUserOrderPosition creates a new position.
func (r *StateRepository) CreateUserOrderPosition(pos *order.UserOrderPosition) uint64 {
	r.gs.mu.Lock()
	id := r.gs.nextID("user_order_positions.csv")
	r.gs.mu.Unlock()

	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	pos.ID = id
	pos.UpdatedAt = time.Now()
	if pos.CreatedAt.IsZero() {
		pos.CreatedAt = pos.UpdatedAt
	}

	r.gs.UserOrderPositions[id] = pos
	r.gs.tickVersion()

	snap := *pos
	r.gs.writeWg.Add(1)
	go func(snapshot order.UserOrderPosition) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("user_order_positions.csv", &snapshot); err != nil {
			log.Printf("error appending user_order_position: %v", err)
		}
	}(snap)

	return id
}

// CreateUserOrderPositionIfAbsent creates a position unless an active position already exists for the same uprunning_order.
func (r *StateRepository) CreateUserOrderPositionIfAbsent(pos *order.UserOrderPosition) (uint64, bool, error) {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	for _, existing := range r.gs.UserOrderPositions {
		if existing.UprunningOrderID == pos.UprunningOrderID && existing.Deleted == 0 {
			return existing.ID, false, nil
		}
	}

	r.gs.mu.Lock()
	id := r.gs.nextID("user_order_positions.csv")
	r.gs.mu.Unlock()
	pos.ID = id
	pos.UpdatedAt = time.Now()
	if pos.CreatedAt.IsZero() {
		pos.CreatedAt = pos.UpdatedAt
	}
	r.gs.UserOrderPositions[id] = pos
	r.gs.tickVersion()

	snap := *pos
	r.gs.writeWg.Add(1)
	go func(snapshot order.UserOrderPosition) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("user_order_positions.csv", &snapshot); err != nil {
			log.Printf("error appending user_order_position: %v", err)
		}
	}(snap)
	return id, true, nil
}

// ClosePosition marks a position as closed.
func (r *StateRepository) ClosePosition(id uint64, closeTime time.Time) error {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	pos, ok := r.gs.UserOrderPositions[id]
	if !ok {
		return fmt.Errorf("user_order_position %d not found", id)
	}
	pos.Deleted = 1
	pos.CloseTime = &closeTime
	pos.UpdatedAt = time.Now()
	r.gs.tickVersion()

	snap := *pos
	r.gs.writeWg.Add(1)
	go func(snapshot order.UserOrderPosition) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("user_order_positions.csv", &snapshot); err != nil {
			log.Printf("error appending position close: %v", err)
		}
	}(snap)

	return nil
}

// ListActivePositions returns all open positions (deleted=0).
// CloseAndCreateRemainingUserOrderPosition closes the original order position and appends a remaining active position when partially reduced.
func (r *StateRepository) CloseAndCreateRemainingUserOrderPosition(id uint64, closedQty float64, riskCtrlStratID uint64, closeTime time.Time) (uint64, error) {
	r.gs.rw.Lock()
	pos, ok := r.gs.UserOrderPositions[id]
	if !ok {
		r.gs.rw.Unlock()
		return 0, fmt.Errorf("user_order_position %d not found", id)
	}
	if closedQty < 0 {
		r.gs.rw.Unlock()
		return 0, fmt.Errorf("closed quantity must be non-negative, got %f", closedQty)
	}

	remainingQty := pos.Quantity - closedQty
	// Handle floating-point precision: e.g., 183.64 - 183.63 = 0.009999999...
	// Round to 8 decimal places (sufficient for crypto quantities) and correct small negatives
	if remainingQty < 0 && remainingQty > -1e-8 {
		remainingQty = 0
	}
	if remainingQty > 1e-12 {
		remainingQty = math.Round(remainingQty*1e8) / 1e8
	}
	if remainingQty < -1e-12 {
		r.gs.rw.Unlock()
		return 0, fmt.Errorf("closed quantity %.12f exceeds position quantity %.12f", closedQty, pos.Quantity)
	}
	pos.Deleted = 1
	pos.CloseTime = &closeTime
	pos.UpdatedAt = time.Now()
	r.gs.tickVersion()
	closedSnap := *pos

	var remainingSnap *order.UserOrderPosition
	var remainingID uint64
	if remainingQty > 1e-12 {
		r.gs.mu.Lock()
		remainingID = r.gs.nextID("user_order_positions.csv")
		r.gs.mu.Unlock()

		remaining := *pos
		remaining.ID = remainingID
		remaining.Quantity = remainingQty
		remaining.PosValue = remaining.PosPrice * remainingQty
		// Scale InitMargin proportionally to remaining quantity
		if pos.Quantity > 0 && pos.InitMargin != 0 {
			remaining.InitMargin = pos.InitMargin * (remainingQty / pos.Quantity)
		}
		remaining.Deleted = 0
		remaining.CloseTime = nil
		remaining.RiskCtrlStratID = riskCtrlStratID
		remaining.CreatedAt = time.Now()
		remaining.UpdatedAt = remaining.CreatedAt
		r.gs.UserOrderPositions[remainingID] = &remaining
		remainingSnap = &remaining
	}
	r.gs.rw.Unlock()

	r.gs.writeWg.Add(1)
	go func(snapshot order.UserOrderPosition) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("user_order_positions.csv", &snapshot); err != nil {
			log.Printf("error appending position close: %v", err)
		}
	}(closedSnap)
	if remainingSnap != nil {
		r.gs.writeWg.Add(1)
		go func(snapshot order.UserOrderPosition) {
			defer r.gs.writeWg.Done()
			if err := r.gs.persister.AppendRow("user_order_positions.csv", &snapshot); err != nil {
				log.Printf("error appending remaining position: %v", err)
			}
		}(*remainingSnap)
	}

	return remainingID, nil
}

func (r *StateRepository) CreateUserPosition(pos *order.UserPosition) uint64 {
	r.gs.mu.Lock()
	id := r.gs.nextID("user_positions.csv")
	r.gs.mu.Unlock()

	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	pos.ID = id
	pos.UpdatedAt = time.Now()
	if pos.CreatedAt.IsZero() {
		pos.CreatedAt = pos.UpdatedAt
	}
	r.gs.UserPositions[id] = pos
	r.gs.tickVersion()

	snap := *pos
	r.gs.writeWg.Add(1)
	go func(snapshot order.UserPosition) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("user_positions.csv", &snapshot); err != nil {
			log.Printf("error appending user_position: %v", err)
		}
	}(snap)
	return id
}

// UserPositionFilter defines filter criteria for user_positions.
type UserPositionFilter struct {
	UserStrategyID  uint64
	UserStrategyIDs []uint64    // 按多个 user_strategy_id 过滤
	UserID          uint64
	UserIDs         []uint64
	Exchange        string
	Deleted         *int       // 0=active, 1=closed
	PosType         *order.PosType
	CreatedAtFrom   *time.Time // 创建时间起始（含）
	CreatedAtTo     *time.Time // 创建时间截止（含）
	CloseTimeFrom   *time.Time // 平仓时间起始（含）；传入任一 close 参数时排除未平仓仓位
	CloseTimeTo     *time.Time // 平仓时间截止（含）
}

// ListUserPositionsByFilter returns user_positions matching the filter.
func (r *StateRepository) ListUserPositionsByFilter(filter UserPositionFilter) []*order.UserPosition {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.UserPosition, 0)
	for _, pos := range r.gs.UserPositions {
		// 过滤：user_strategy_id
		if filter.UserStrategyID != 0 && pos.UserStrategyID != filter.UserStrategyID {
			continue
		}
		// 过滤：user_id（单个）
		if filter.UserID != 0 && pos.UserID != filter.UserID {
			continue
		}
		// 过滤：user_ids（多个）
		if len(filter.UserIDs) > 0 && !uint64InSlice(pos.UserID, filter.UserIDs) {
			continue
		}
		// 过滤：exchange
		if filter.Exchange != "" && pos.Exchange != filter.Exchange {
			continue
		}
		// 过滤：deleted
		if filter.Deleted != nil && pos.Deleted != *filter.Deleted {
			continue
		}
		// 过滤：pos_type
		if filter.PosType != nil && pos.PosType != *filter.PosType {
			continue
		}
		// 过滤：user_strategy_ids（多个）
		if len(filter.UserStrategyIDs) > 0 && !uint64InSlice(pos.UserStrategyID, filter.UserStrategyIDs) {
			continue
		}
		// 过滤：created_at 范围
		if filter.CreatedAtFrom != nil && pos.CreatedAt.Before(*filter.CreatedAtFrom) {
			continue
		}
		if filter.CreatedAtTo != nil && pos.CreatedAt.After(*filter.CreatedAtTo) {
			continue
		}
		// 过滤：close_time 范围（传入任一 close 参数时排除未平仓仓位）
		if !matchCloseTime(pos.CloseTime, filter.CloseTimeFrom, filter.CloseTimeTo) {
			continue
		}
		result = append(result, pos)
	}
	return result
}

// ListUserPositionsByUserName returns user_positions filtered by user name and optional exchange.
func (r *StateRepository) ListUserPositionsByUserName(userName, exchange string) []*order.UserPosition {
	userIDs, err := r.FindUserIDsByName(userName, exchange)
	if err != nil {
		return []*order.UserPosition{}
	}
	return r.ListUserPositionsByFilter(UserPositionFilter{
		UserIDs:  userIDs,
		Exchange: exchange,
	})
}

func (r *StateRepository) GetUserPositionByID(id uint64) (*order.UserPosition, error) {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	pos, ok := r.gs.UserPositions[id]
	if !ok {
		return nil, fmt.Errorf("user_position %d not found", id)
	}
	return pos, nil
}

func (r *StateRepository) CloseAndCreateRemainingUserPosition(id uint64, closedQty float64, riskCtrlStratID uint64, closeTime time.Time) (uint64, error) {
	log.Printf("[CloseAndCreateRemainingUserPosition] called: id=%d, closedQty=%.4f, remainingQty=%.4f", id, closedQty, r.gs.UserPositions[id].Quantity-closedQty)

	r.gs.rw.Lock()
	pos, ok := r.gs.UserPositions[id]
	if !ok {
		r.gs.rw.Unlock()
		return 0, fmt.Errorf("user_position %d not found", id)
	}
	if closedQty < 0 {
		r.gs.rw.Unlock()
		return 0, fmt.Errorf("closed quantity must be non-negative, got %f", closedQty)
	}
	remainingQty := pos.Quantity - closedQty
	// Handle floating-point precision: e.g., 183.64 - 183.63 = 0.009999999...
	// Round to 8 decimal places (sufficient for crypto quantities) and correct small negatives
	if remainingQty < 0 && remainingQty > -1e-8 {
		remainingQty = 0
	}
	if remainingQty > 1e-12 {
		// Round to 8 decimal places to avoid floating-point artifacts
		remainingQty = math.Round(remainingQty*1e8) / 1e8
	}
	if remainingQty < -1e-12 {
		r.gs.rw.Unlock()
		return 0, fmt.Errorf("closed quantity %.12f exceeds user_position quantity %.12f", closedQty, pos.Quantity)
	}

	// If closing all (closedQty == 0 or remainingQty == 0), just mark as deleted
	if remainingQty <= 1e-12 {
		pos.Deleted = 1
		pos.CloseTime = &closeTime
		pos.UpdatedAt = time.Now()
		r.gs.tickVersion()
		closedSnap := *pos
		r.gs.rw.Unlock()

		r.gs.writeWg.Add(1)
		go func(snapshot order.UserPosition) {
			defer r.gs.writeWg.Done()
			if err := r.gs.persister.AppendRow("user_positions.csv", &snapshot); err != nil {
				log.Printf("error appending user_position close: %v", err)
			}
		}(closedSnap)
		return 0, nil
	}

	pos.Deleted = 1
	pos.CloseTime = &closeTime
	pos.UpdatedAt = time.Now()
	r.gs.tickVersion()
	closedSnap := *pos

	var remainingSnap *order.UserPosition
	var remainingID uint64
	if remainingQty > 1e-12 {
		log.Printf("[CloseAndCreateRemainingUserPosition] CREATING REMAINING: originalID=%d, remainingQty=%.4f, UserStrategyID=%d", id, remainingQty, pos.UserStrategyID)

		r.gs.mu.Lock()
		remainingID = r.gs.nextID("user_positions.csv")
		r.gs.mu.Unlock()

		remaining := *pos
		remaining.ID = remainingID
		remaining.Quantity = remainingQty
		if pos.Quantity > 0 {
			ratio := remainingQty / pos.Quantity
			remaining.LatestMarketCapitalization = pos.LatestMarketCapitalization * ratio
			remaining.TotalMargin = pos.TotalMargin * ratio
		}
		remaining.Deleted = 0
		remaining.CloseTime = nil
		remaining.RiskCtrlStratID = riskCtrlStratID
		remaining.CreatedAt = time.Now()
		remaining.UpdatedAt = remaining.CreatedAt
		r.gs.UserPositions[remainingID] = &remaining
		remainingSnap = &remaining
	}
	r.gs.rw.Unlock()

	r.gs.writeWg.Add(1)
	go func(snapshot order.UserPosition) {
		defer r.gs.writeWg.Done()
		if err := r.gs.persister.AppendRow("user_positions.csv", &snapshot); err != nil {
			log.Printf("error appending user_position close: %v", err)
		}
	}(closedSnap)
	if remainingSnap != nil {
		r.gs.writeWg.Add(1)
		go func(snapshot order.UserPosition) {
			defer r.gs.writeWg.Done()
			if err := r.gs.persister.AppendRow("user_positions.csv", &snapshot); err != nil {
				log.Printf("error appending remaining user_position: %v", err)
			}
		}(*remainingSnap)
	}
	return remainingID, nil
}

// UpdateUserOrderPositionPrices updates in-memory current_price, pos_value, and pnl_value
// for all active positions using the latest prices. This is memory-only — no CSV append.
// Returns the number of positions updated.
func (r *StateRepository) UpdateUserOrderPositionPrices(prices map[string]map[string]float64) int {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()

	now := time.Now()
	updated := 0

	for _, pos := range r.gs.UserOrderPositions {
		if pos.Deleted != 0 {
			continue
		}

		price, ok := findPrice(prices, pos.Asset, pos.Exchange)
		if !ok || price <= 0 {
			continue
		}

		pos.CurrentPrice = price
		pos.PosValue = price * pos.Quantity
		pos.PnLValue = order.CalculatePnL(pos.PosPrice, price, pos.Quantity, pos.Side)
		pos.UpdatedAt = now
		updated++
	}

	if updated > 0 {
		r.gs.tickVersion()
	}

	return updated
}

// findPrice looks up the best-matching price for a given asset and exchange.
func findPrice(prices map[string]map[string]float64, asset, exchange string) (float64, bool) {
	exchangePrices, ok := prices[exchange]
	if !ok {
		return 0, false
	}

	// Direct match — works for Binance (e.g., "NEARUSDT")
	if price, ok := exchangePrices[asset]; ok {
		return price, true
	}

	// For Hyperliquid positions: strip quote suffix to match coin-only keys (e.g., "NEARUSDC" → "NEAR")
	coin := stripQuoteSuffix(asset)
	if coin != asset {
		if price, ok := exchangePrices[coin]; ok {
			return price, true
		}
	}

	return 0, false
}

// stripQuoteSuffix removes USDT/USDC suffix from a symbol.
func stripQuoteSuffix(s string) string {
	switch {
	case strings.HasSuffix(s, "USDT"):
		return s[:len(s)-4]
	case strings.HasSuffix(s, "USDC"):
		return s[:len(s)-4]
	}
	return s
}

func (r *StateRepository) ListActiveUserPositions() []*order.UserPosition {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.UserPosition, 0)
	for _, pos := range r.gs.UserPositions {
		if pos.Deleted == 0 {
			result = append(result, pos)
		}
	}
	return result
}

func (r *StateRepository) ListActivePositions() []*order.UserOrderPosition {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.UserOrderPosition, 0)
	for _, pos := range r.gs.UserOrderPositions {
		if pos.Deleted == 0 {
			result = append(result, pos)
		}
	}
	return result
}

func (r *StateRepository) ListUserOrderPositionsByFilter(filter UserOrderPositionFilter) []*order.UserOrderPosition {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.UserOrderPosition, 0)
	for _, pos := range r.gs.UserOrderPositions {
		// 过滤：user_strategy_id
		if filter.UserStrategyID != 0 && pos.UserStrategyID != filter.UserStrategyID {
			continue
		}
		// 过滤：user_id（单个）
		if filter.UserID != 0 && pos.UserID != filter.UserID {
			continue
		}
		// 过滤：user_ids（多个）
		if len(filter.UserIDs) > 0 && !uint64InSlice(pos.UserID, filter.UserIDs) {
			continue
		}
		// 过滤：side
		if filter.Side != nil && pos.Side != *filter.Side {
			continue
		}
		// 过滤：active/deleted
		if filter.Active != nil {
			if *filter.Active && pos.Deleted != 0 {
				continue
			}
			if !*filter.Active && pos.Deleted == 0 {
				continue
			}
		}
		// 过滤：asset
		if filter.Asset != "" && pos.Asset != filter.Asset {
			continue
		}
		// 过滤：exchange
		if filter.Exchange != "" && pos.Exchange != filter.Exchange {
			continue
		}
		// 过滤：pos_type
		if filter.PosType != nil && pos.PosType != *filter.PosType {
			continue
		}
		// 过滤：user_strategy_ids（多个）
		if len(filter.UserStrategyIDs) > 0 && !uint64InSlice(pos.UserStrategyID, filter.UserStrategyIDs) {
			continue
		}
		// 过滤：created_at 范围
		if filter.CreatedAtFrom != nil && pos.CreatedAt.Before(*filter.CreatedAtFrom) {
			continue
		}
		if filter.CreatedAtTo != nil && pos.CreatedAt.After(*filter.CreatedAtTo) {
			continue
		}
		// 过滤：close_time 范围（传入任一 close 参数时排除未平仓仓位）
		if !matchCloseTime(pos.CloseTime, filter.CloseTimeFrom, filter.CloseTimeTo) {
			continue
		}
		result = append(result, pos)
	}
	return result
}

func (r *StateRepository) CountUserOrderPositionsByFilter(filter UserOrderPositionFilter) int {
	return len(r.ListUserOrderPositionsByFilter(filter))
}

// ListUserOrderPositions returns all positions.
func (gs *GlobalState) ListUserOrderPositions() []*order.UserOrderPosition {
	gs.rw.RLock()
	defer gs.rw.RUnlock()
	result := make([]*order.UserOrderPosition, 0, len(gs.UserOrderPositions))
	for _, pos := range gs.UserOrderPositions {
		result = append(result, pos)
	}
	return result
}

// mapToInterfaceSlice converts a map of *T values to []interface{}.
func mapToInterfaceSlice[T any](m map[uint64]*T) []interface{} {
	s := make([]interface{}, 0, len(m))
	for _, v := range m {
		s = append(s, v)
	}
	return s
}

// CompactAll writes the latest state for all tables atomically.
// IMPORTANT: Waits for all pending writes to complete before compacting
// to prevent data corruption from concurrent AppendRow operations.
func (gs *GlobalState) CompactAll() error {
	// Step 1: Wait for all pending writes to complete
	// This ensures that all goroutines spawned by Create* methods
	// have finished writing their data to CSV files.
	gs.writeWg.Wait()

	// Step 2: Acquire write lock to block new writes during compact
	// Using Lock() instead of RLock() ensures no new writes can start
	// while we're compacting the files.
	gs.rw.Lock()
	defer gs.rw.Unlock()

	// SAFETY CHECK: Validate memory state before compact
	if err := validateAllTablesBeforeCompact(gs.persister, gs); err != nil {
		log.Printf("SAFETY CHECK FAILED: %v - ABORTING COMPACT", err)
		return fmt.Errorf("safety check failed: %w", err)
	}
	if err := gs.persister.Compact("users.csv", mapToInterfaceSlice(gs.Users)); err != nil {
		return fmt.Errorf("compact users: %w", err)
	}
	if err := gs.persister.Compact("strategies.csv", mapToInterfaceSlice(gs.Strategies)); err != nil {
		return fmt.Errorf("compact strategies: %w", err)
	}
	if err := gs.persister.Compact("strategy_assets.csv", mapToInterfaceSlice(gs.StrategyAssets)); err != nil {
		return fmt.Errorf("compact strategy_assets: %w", err)
	}
	if err := gs.persister.Compact("user_strategies.csv", mapToInterfaceSlice(gs.UserStrategies)); err != nil {
		return fmt.Errorf("compact user_strategies: %w", err)
	}
	if err := gs.persister.Compact("user_orders.csv", mapToInterfaceSlice(gs.UserOrders)); err != nil {
		return fmt.Errorf("compact user_orders: %w", err)
	}
	if err := gs.persister.Compact("leverage_configs.csv", mapToInterfaceSlice(gs.LeverageConfigs)); err != nil {
		return fmt.Errorf("compact leverage_configs: %w", err)
	}
	if err := gs.persister.Compact("exchange_symbol_filters.csv", mapToInterfaceSlice(gs.ExchangeSymbolFilters)); err != nil {
		return fmt.Errorf("compact exchange_symbol_filters: %w", err)
	}
	if err := gs.persister.Compact("uprunning_orders.csv", mapToInterfaceSlice(gs.UprunningOrders)); err != nil {
		return fmt.Errorf("compact uprunning_orders: %w", err)
	}
	if err := gs.persister.Compact("user_order_positions.csv", mapToInterfaceSlice(gs.UserOrderPositions)); err != nil {
		return fmt.Errorf("compact user_order_positions: %w", err)
	}
	if err := gs.persister.Compact("user_positions.csv", mapToInterfaceSlice(gs.UserPositions)); err != nil {
		return fmt.Errorf("compact user_positions: %w", err)
	}
	return nil
}

// Shutdown waits for all pending writes to complete.
func (gs *GlobalState) Shutdown() {
	gs.writeWg.Wait()
}

// Reload re-reads all entity CSVs from disk, updating the in-memory state.
// This ensures the in-memory state reflects any changes made by other services
// (e.g., user_order_service creating new uprunning orders).
// NOTE: Does not hold rw lock globally — each load function acquires it internally.
func (gs *GlobalState) Reload() error {
	gs.rw.Lock()
	gs.Users = make(map[uint64]*order.User)
	gs.Strategies = make(map[uint64]*order.Strategy)
	gs.StrategyAssets = make(map[uint64]*order.StrategyAsset)
	gs.UserStrategies = make(map[uint64]*order.UserStrategy)
	gs.UserOrders = make(map[uint64]*order.UserOrder)
	gs.LeverageConfigs = make(map[uint64]*order.LeverageConfig)
	gs.ExchangeSymbolFilters = make(map[uint64]*order.ExchangeSymbolFilter)
	gs.UprunningOrders = make(map[uint64]*order.UprunningOrder)
	gs.UserOrderPositions = make(map[uint64]*order.UserOrderPosition)
	gs.UserPositions = make(map[uint64]*order.UserPosition)
	gs.rw.Unlock()

	return gs.loadAll()
}

// Reload re-reads all entity CSVs from disk via the underlying GlobalState.
func (r *StateRepository) Reload() error {
	return r.gs.Reload()
}

// ReloadUprunningOrders reloads only the uprunning_orders from disk.
// This is used by the order scanner to pick up WS updates without
// clearing the entire in-memory state.
func (r *StateRepository) ReloadUprunningOrders() error {
	return r.gs.reloadUprunningOrders()
}

// SetSyncInterval sets the minimum interval between position syncs.
func (r *StateRepository) SetSyncInterval(d time.Duration) {
	r.syncMu.Lock()
	r.syncInterval = d
	r.syncMu.Unlock()
}

// resetLastSyncTime resets the last sync time to force a reload on next call (for testing).
func (r *StateRepository) resetLastSyncTime() {
	r.syncMu.Lock()
	r.lastSyncTime = time.Time{}
	r.syncMu.Unlock()
}

// ReloadUserOrderPositionsIfNeeded reloads user_order_positions from CSV
// only if the sync interval has passed since the last reload.
// This prevents frequent CSV reads during dense signal bursts while
// ensuring the in-memory state stays reasonably up-to-date with PMS updates.
func (r *StateRepository) ReloadUserOrderPositionsIfNeeded() error {
	r.syncMu.Lock()

	// Check if interval has passed
	if r.syncInterval > 0 && time.Since(r.lastSyncTime) < r.syncInterval {
		r.syncMu.Unlock()
		return nil // Skip reload, use cached data
	}

	// Interval passed or first call - need to reload
	// Update time immediately to prevent concurrent reloads
	r.lastSyncTime = time.Now()
	r.syncMu.Unlock()

	// Perform reload (unlocked to allow reads during reload)
	err := r.gs.reloadUserOrderPositions()
	if err != nil {
		// Reset time on error to allow retry
		r.syncMu.Lock()
		r.lastSyncTime = time.Time{}
		r.syncMu.Unlock()
	}
	return err
}

// reloadUserOrderPositions reloads only user_order_positions from CSV.
func (gs *GlobalState) reloadUserOrderPositions() error {
	gs.rw.Lock()
	gs.UserOrderPositions = make(map[uint64]*order.UserOrderPosition)
	gs.rw.Unlock()
	return gs.loadUserOrderPositions()
}

// ReloadExchangeSymbolFilters re-reads exchange_symbol_filters.csv from disk.
// This is called via RPC when PMS updates the filters.
func (r *StateRepository) ReloadExchangeSymbolFilters() error {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	return r.gs.loadExchangeSymbolFilters()
}

// ReloadUserStrategies re-reads user_strategies.csv from disk.
// This allows PMS to pick up newly created strategies from UOS.
func (r *StateRepository) ReloadUserStrategies() error {
	r.gs.rw.Lock()
	defer r.gs.rw.Unlock()
	r.gs.UserStrategies = make(map[uint64]*order.UserStrategy)
	return r.gs.loadUserStrategies()
}

// ListAllExchangeSymbolFilters returns all exchange symbol filters.
func (r *StateRepository) ListAllExchangeSymbolFilters() []*order.ExchangeSymbolFilter {
	r.gs.rw.RLock()
	defer r.gs.rw.RUnlock()
	result := make([]*order.ExchangeSymbolFilter, 0, len(r.gs.ExchangeSymbolFilters))
	for _, filter := range r.gs.ExchangeSymbolFilters {
		result = append(result, filter)
	}
	return result
}
