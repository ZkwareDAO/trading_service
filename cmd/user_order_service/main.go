package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"trading-service/internal/api"
	"trading-service/internal/config"
	"trading-service/internal/notification"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"
	tradesignal "trading-service/internal/signal"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Loading state from %s...", cfg.Storage.DataDir)
	gs, err := persistence.NewGlobalState(cfg.Storage.DataDir)
	if err != nil {
		log.Fatalf("Failed to load global state: %v", err)
	}
	log.Printf("State loaded: version=%d, users=%d, strategies=%d, orders=%d",
		gs.Version, len(gs.Users), len(gs.Strategies), len(gs.UserOrders))

	repo := persistence.NewStateRepository(gs)

	// Set position sync interval from config
	// This controls how often UOS syncs user_order_positions from CSV to pick up PMS updates
	if cfg.PositionSyncInterval > 0 {
		repo.SetSyncInterval(cfg.PositionSyncInterval)
		log.Printf("Position sync interval: %v", cfg.PositionSyncInterval)
	}

	// Testnet configuration: config.yaml `exchange:` section, with the *_TESTNET env
	// vars taking precedence (resolved inside config.LoadConfig). Unset means mainnet.
	testnetBinance := cfg.Exchange.BinanceTestnet
	testnetHyperliquid := cfg.Exchange.HyperliquidTestnet
	testnetDeribit := cfg.Exchange.DeribitTestnet
	log.Printf("Exchange networks: binance=%s, hyperliquid=%s, deribit=%s",
		networkName(testnetBinance), networkName(testnetHyperliquid), networkName(testnetDeribit))

	positionMonitorURL := os.Getenv("POSITION_MONITOR_URL")
	if positionMonitorURL == "" {
		positionMonitorURL = "http://localhost:8080"
	}

	// Create notifier from config
	var notifier notification.Notifier
	if cfg.Notification.Enabled {
		notifier = notification.NewWebhookRouter(cfg.Notification.OpenURL, cfg.Notification.CloseURL, cfg.Notification.TestURL)
		log.Printf("Notification enabled: open=%s close=%s test=%s",
			maskKey(cfg.Notification.OpenURL),
			maskKey(cfg.Notification.CloseURL),
			maskKey(cfg.Notification.TestURL))
	}

	sigHandler := tradesignal.NewHandlerWithDataDirAndTestnetConfig(repo, cfg.Storage.DataDir, testnetBinance, testnetHyperliquid, testnetDeribit, rpc.NewOrderServiceClient(positionMonitorURL), notifier)

	// Start symbol filter sync (runs in background, skips mock)
	log.Printf("Filter sync interval: %v", cfg.FilterSyncInterval)
// TODO: redesign filter sync without factory
// 	go exchange.StartFilterSync(context.Background(), exFactory, repo, cfg.FilterSyncInterval)

	mux := http.NewServeMux()
	registerHTTPHandlers(mux, repo, sigHandler)

	// Only listen on localhost (127.0.0.1) to prevent external access
	// External traffic should go through nginx reverse proxy
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Server.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("User order service starting on %s (mode=%s, internal-only)", addr, cfg.Server.Mode)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down user order service...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	// Wait for pending async writes before compact
	log.Println("Waiting for pending writes...")
	gs.Shutdown()

	// Reload from CSV to pick up any updates made by position_monitor_service
	// (e.g., WS handler/scanner updating order status to FILLED)
	// This is critical: without reload, CompactAll would overwrite CSV with stale memory state
	log.Println("Reloading state from CSV before compact...")
	if err := gs.Reload(); err != nil {
		log.Printf("Reload error: %v", err)
		log.Printf("CRITICAL: Skipping compact to prevent data loss!")
		log.Println("User order service stopped.")
		return
	}

	// Final compact on shutdown
	log.Println("Compacting CSV files...")
	if err := gs.CompactAll(); err != nil {
		log.Printf("Compact error: %v", err)
	}

	log.Println("User order service stopped.")
}

func registerHTTPHandlers(mux *http.ServeMux, repo *persistence.StateRepository, sigHandler *tradesignal.Handler) {
	// Register all API handlers (health, state, users, signals, orders)
	api.RegisterHandlers(mux, repo, sigHandler)

	// Register RPC handlers called by position_monitor_service.
	rpcServer := rpc.NewServer(repo)
	mux.Handle("/rpc/", rpcServer.Handle())
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

// networkName renders a testnet flag for the startup log. Orders are placed with real
// funds on MAINNET, so the active network is stated explicitly rather than implied.
func networkName(testnet bool) string {
	if testnet {
		return "testnet"
	}
	return "MAINNET"
}
