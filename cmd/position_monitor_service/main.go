package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"trading-service/internal/deribit_position_sync"
	"trading-service/internal/exchange/deribit"
	"trading-service/internal/notification"
	"trading-service/internal/persistence"
	"trading-service/internal/risk"
	"trading-service/internal/risk/aggregator"
	"trading-service/internal/risk/config"
	riskexec "trading-service/internal/risk/executor"
	"trading-service/internal/risk/pipeline"
	"trading-service/internal/rpc"
)

func main() {
	pmCfg, err := LoadPMConfig()
	if err != nil {
		log.Fatalf("Failed to load PM config: %v", err)
	}

	initialState := &risk.GlobalState{
		Version: 0,
		Snapshot: &risk.MarketSnapshot{
			Prices:  make(map[string]map[string]float64),
			Funding: make(map[string]float64),
		},
		Positions: []*risk.UserPosition{},
	}

	// Start HTTP API for dynamic rule registration
	dataDir := getDataDir()
	ruleStore, err := config.NewRuleStore(dataDir)
	if err != nil {
		log.Fatalf("Failed to create RuleStore: %v", err)
	}

	positionState, err := persistence.NewGlobalState(dataDir)
	if err != nil {
		log.Fatalf("Failed to load position query state: %v", err)
	}
	defer positionState.Shutdown()

	positionRepo := persistence.NewStateRepository(positionState)

	api := NewAPIHandler(ruleStore, positionRepo, pmCfg.Defaults.TimeStopHours)
	http.HandleFunc("/api/v1/rules", api.HandleRegisterRule)

	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"healthy"}`))
	})

	// Exchange resolver for exchange queries
	exchangeResolver := NewUserExchangeResolver(positionRepo, pmCfg.Exchange.BinanceTestnet, pmCfg.Exchange.HyperliquidTestnet, pmCfg.Exchange.DeribitTestnet)

	// Position API (user-order-positions, user-positions, close-all, close-partial, exchange/positions)
	positionAPI := NewPositionAPIHandler(positionRepo, ruleStore, exchangeResolver)
	http.Handle("/api/v1/user-order-positions", positionAPI)
	http.Handle("/api/v1/user-positions", positionAPI)
	http.Handle("/api/v1/positions/close-all", positionAPI)
	http.Handle("/api/v1/positions/close-partial", positionAPI)
	http.Handle("/api/v1/exchange/positions", positionAPI)

	// Build price runtimes before starting RPC handlers (need real-time prices)
	exchangeNames := make([]string, 0)
	for _, user := range positionRepo.ListUsers() {
		exchangeNames = append(exchangeNames, user.Exchange)
	}
	priceRuntimes := BuildPriceRuntimes(CollectNonMockExchanges(exchangeNames), pmCfg.Exchange.HyperliquidTestnet, positionRepo)

	// RPC handlers
	positionQuery := NewPositionQueryHandler(positionRepo, priceRuntimes)
	http.HandleFunc("/rpc/v1/user-order-positions/query", positionQuery.HandleQueryUserOrderPositions)
	http.HandleFunc("/rpc/v1/uprunning-order/create", positionQuery.HandleCreateUprunningOrder)

	// Enhanced market price handler with REST API fallback (fixes market order price issue)
	positionRPC := NewPositionRPCHandler(positionRepo, priceRuntimes, exchangeResolver)
	http.HandleFunc("/rpc/v1/market-price/get", positionRPC.HandleGetMarketPriceEnhanced)
	http.HandleFunc("/rpc/v1/rules/create", api.HandleRPCCreateRule)
	http.HandleFunc("/rpc/v1/rules/invalidate-for-strategy", api.HandleRPCInvalidateRulesForStrategy)
	http.HandleFunc("/rpc/v1/order-position-metadata/query", positionRPC.HandleQueryOrderPositionMetadata)

	port := os.Getenv("POSITION_MONITOR_PORT")
	if port == "" {
		port = "8080"
	}
	// Only listen on localhost (127.0.0.1) to prevent external access
	// External traffic should go through nginx reverse proxy
	addr := "127.0.0.1:" + port
	go func() {
		log.Printf("Position Monitor API listening on %s (internal-only)\n", addr)
		log.Fatal(http.ListenAndServe(addr, nil))
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start price runtimes
	if len(priceRuntimes) > 0 {
		if err := StartPriceRuntimes(ctx, priceRuntimes); err != nil {
			log.Printf("warn: start price runtimes failed: %v", err)
		} else {
			log.Printf("Started %d price runtime(s)", len(priceRuntimes))
		}
	}

	orderServiceURL := os.Getenv("POSITION_MONITOR_ORDER_SERVICE_URL")
	if orderServiceURL == "" {
		orderServiceURL = "http://localhost:8081"
	}
	var notifier notification.Notifier
	if pmCfg.Notification.Enabled {
		notifier = notification.NewWebhookRouter(pmCfg.Notification.OpenURL, pmCfg.Notification.CloseURL, pmCfg.Notification.TestURL)
		log.Printf("Position monitor notification enabled: open=%s close=%s test=%s",
			maskKey(pmCfg.Notification.OpenURL),
			maskKey(pmCfg.Notification.CloseURL),
			maskKey(pmCfg.Notification.TestURL))
	} else {
		log.Printf("Position monitor notification disabled")
	}

	userRuntimeFactory := NewCompositeUserOrderRuntimeFactory(map[string]UserOrderRuntimeFactory{
		"binance":     NewBinanceUserOrderRuntimeFactoryWithRuleUpdater(positionRepo, orderServiceURL, pmCfg.Exchange.BinanceTestnet, ruleStore, notifier),
		"hyperliquid": NewHyperliquidUserOrderRuntimeFactoryWithRuleUpdater(positionRepo, orderServiceURL, pmCfg.Exchange.HyperliquidTestnet, ruleStore, notifier),
		"deribit":     NewDeribitUserOrderRuntimeFactoryWithRuleUpdater(positionRepo, orderServiceURL, pmCfg.Exchange.DeribitTestnet, ruleStore, notifier),
	})
	userRuntimeManager := NewUserOrderRuntimeManager(userRuntimeFactory)
	StartUserOrderRuntimeReconcileLoop(ctx, userRuntimeManager, positionRepo, 30*time.Second)
	log.Printf("Started user order runtime reconcile loop, interval=%v", 30*time.Second)

	// Create Deribit spread checker
	roiLookup := func(userStrategyID uint64) float64 {
		for _, up := range positionRepo.ListActiveUserPositions() {
			if up.UserStrategyID == userStrategyID {
				return up.ROI
			}
		}
		return 0
	}
	spreadChecker := deribit.NewSpreadChecker(pmCfg.DeribitSpreadThreshold, notifier, roiLookup)

	riskApplier := NewRiskActionApplierWithRuleStore(positionRepo, ruleStore, exchangeResolver, notifier, spreadChecker)

	// Create RPC client for Scanner fallback notifications
	rpcClient := rpc.NewOrderServiceClient(orderServiceURL)

	// Order status scanner: REST API fallback for missed WS events
	scanner := NewOrderStatusScanner(positionRepo, exchangeResolver, ruleStore, 30*time.Second, rpcClient, notifier)
	scanner.Start(ctx)

	// Periodic sync loop: price → aggregate positions → pipeline → apply actions
	interval := pmCfg.Runtime.PriceSnapshotInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	StartSyncLoop(ctx, initialState, priceRuntimes, positionRepo, ruleStore, riskApplier, interval)
	log.Printf("Started sync loop, interval=%v", interval)

	// Start filter sync: sync exchange_symbol_filters from exchanges
	filterSyncInterval := pmCfg.FilterSyncInterval
	if filterSyncInterval <= 0 {
		filterSyncInterval = 240 * time.Hour // default 10 days
	}
	go StartFilterSync(ctx, positionRepo, orderServiceURL, filterSyncInterval, pmCfg.Exchange.BinanceTestnet, pmCfg.Exchange.HyperliquidTestnet)
	log.Printf("Started filter sync, interval=%v", filterSyncInterval)

	// Start Deribit position sync (one-time at startup)
	// Periodic sync is now triggered by WS monitor when order not found
	if pmCfg.DeribitPositionSync.Enabled {
		go func() {
			log.Printf("[DeribitSync] Starting initial sync at startup...")
			if err := deribit_position_sync.SyncDeribitPositions(rpcClient, positionRepo, pmCfg.Exchange.DeribitTestnet, notifier); err != nil {
				log.Printf("[DeribitSync] Initial sync error: %v", err)
			} else {
				log.Printf("[DeribitSync] Initial sync completed successfully")
			}
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Position monitor service shutting down...")

	// Wait for pending async writes
	log.Println("Waiting for pending writes...")
	positionState.Shutdown()

	// Reload from CSV to pick up any updates made by user_order_service
	log.Println("Reloading state from CSV before compact...")
	if err := positionState.Reload(); err != nil {
		log.Printf("Reload error: %v", err)
		log.Printf("CRITICAL: Skipping compact to prevent data loss!")
		log.Println("Position monitor service stopped.")
		return
	}

	// Compact CSV files to persist latest state
	log.Println("Compacting CSV files...")
	if err := positionState.CompactAll(); err != nil {
		log.Printf("Compact error: %v", err)
	}

	log.Println("Position monitor service stopped.")
}

func loadConfig() (*config.Config, error) {
	loader := config.NewConfigLoader(getDataDir())
	return loader.LoadAll()
}

func getDataDir() string {
	if d := os.Getenv("DATA_DIR"); d != "" {
		return d
	}
	return "data"
}

func maskKey(url string) string {
	if url == "" {
		return "(disabled)"
	}
	if idx := strings.LastIndex(url, "key="); idx > 0 {
		return url[:idx+4] + "***"
	}
	return "***"
}

// StartPriceRuntimes starts all price runtimes.
func StartPriceRuntimes(ctx context.Context, runtimes []PriceRuntime) error {
	for _, rt := range runtimes {
		if rt == nil {
			continue
		}
		if err := rt.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

// StartSyncLoop periodically syncs prices, aggregates positions, runs risk pipeline, and applies actions.
func StartSyncLoop(
	ctx context.Context,
	state *risk.GlobalState,
	runtimes []PriceRuntime,
	repo *persistence.StateRepository,
	ruleStore *config.RuleStore,
	applier *riskexec.RiskActionApplier,
	interval time.Duration,
) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		syncCycle(state, runtimes, repo, ruleStore, applier)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				syncCycle(state, runtimes, repo, ruleStore, applier)
			}
		}
	}()
}

// syncCycle: sync prices → ensure Deribit subscriptions → update order positions → aggregate → run pipeline → apply actions.
func syncCycle(
	state *risk.GlobalState,
	runtimes []PriceRuntime,
	repo *persistence.StateRepository,
	ruleStore *config.RuleStore,
	applier *riskexec.RiskActionApplier,
) {
	// 1. Sync price snapshots
	SyncPriceSnapshots(state, runtimes)

	// 1.5. Ensure Deribit subscriptions for new options (dynamic subscription)
	for _, runtime := range runtimes {
		if dr, ok := runtime.(*DeribitPriceRuntime); ok {
			dr.EnsureSubscribed()
		}
	}

	// 2. Update in-memory user_order_positions with latest prices (memory-only, per-exchange)
	if n := repo.UpdateUserOrderPositionPrices(state.Snapshot.Prices); n > 0 {
		log.Printf("Price update: updated %d user_order_position(s) with latest prices", n)
	}

	// 3. Aggregate positions with WS prices (reads updated in-memory state)
	aggResults := aggregator.AggregateFromPersistenceWithMetrics(repo, state.Snapshot.Prices)
	log.Printf("Position aggregation: found %d positions to evaluate", len(aggResults))

	// 4. Build risk.UserPosition list for pipeline
	state.Positions = nil // Clear before rebuilding to avoid accumulation
	for _, agg := range aggResults {
		state.Positions = append(state.Positions, agg.Position)
	}

	// 5. Sync user_positions to DB
	if err := aggregator.UserPositionSyncer(repo, repo, state.Snapshot.Prices); err != nil {
		log.Printf("warn: sync user positions: %v", err)
	} else if len(aggResults) > 0 {
		log.Printf("User positions synced: updated %d user_positions with latest metrics", len(aggResults))
	}

	// 6. Auto-generate default rules for strategies with positions but no rules
	gen := NewDefaultRuleGeneratorWithRepo(ruleStore, repo)
	gen.GenerateForMissingStrategies(aggResults)

	// 7. Run risk pipeline and apply actions
	riskCfg := ruleStore.SnapshotConfig()
	p := pipeline.NewRiskPipelineWithRepo(repo)
	results := p.Run(state, riskCfg)

	actionCount := 0
	for _, pr := range results {
		// Handle chain actions: activate linked rules
		for _, rule := range pr.Rules {
			if chainID, err := strconv.Atoi(rule.Action); err == nil {
				if chainRule, ok := ruleStore.GetRule(chainID); ok {
					if chainRule.Status == "inactive" {
						chainRule.Status = "active"
						_ = ruleStore.AddRules([]risk.Rule{*chainRule})
						log.Printf("chain action: activated rule %d (condition=%s)", chainID, chainRule.ConditionName)
					}
				}
			}
		}
		for _, action := range pr.Results {
			if action.ActionType == "reduce" {
				actionCount++
				log.Printf("Applying risk action: actionType=%s, userID=%d, strategyID=%d, symbol=%s, ruleID=%d",
					action.ActionType, action.UserID, action.UserStrategyID, action.Symbol, action.RuleID)
				if err := applier.ApplyReduce(action); err != nil {
					log.Printf("Risk action apply failed: %v", err)
				}
			}
		}
	}
	if actionCount > 0 {
		log.Printf("Risk cycle completed: applied %d reduce actions", actionCount)
	}
}
