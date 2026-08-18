package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"trading-service/internal/exchange"
	"trading-service/internal/exchange/binance"
	exchangews "trading-service/internal/exchange/ws"
	"trading-service/internal/notification"
	"trading-service/internal/order"
	"trading-service/internal/persistence"
	"trading-service/internal/rpc"

	"github.com/adshao/go-binance/v2/futures"
)

type listenKeyService interface {
	Start(ctx context.Context) (string, error)
	Keepalive(ctx context.Context, listenKey string) error
}

type orderMonitorStarter interface {
	StartFuturesUserDataWS(listenKeyProvider func() (string, error))
	Stop()
}

type BinanceUserOrderRuntime struct {
	listenKeys    listenKeyService
	monitor       orderMonitorStarter
	stopOnce      sync.Once
	stopKeepalive chan struct{}
}

func NewBinanceUserOrderRuntimeForTest(listenKeys listenKeyService, monitor orderMonitorStarter) *BinanceUserOrderRuntime {
	return &BinanceUserOrderRuntime{listenKeys: listenKeys, monitor: monitor}
}

func (r *BinanceUserOrderRuntime) Start(ctx context.Context) error {
	listenKey, err := r.listenKeys.Start(ctx)
	if err != nil {
		return err
	}

	r.monitor.StartFuturesUserDataWS(func() (string, error) {
		return listenKey, nil
	})

	r.stopKeepalive = make(chan struct{})
	go func() {
		ticker := time.NewTicker(25 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-r.stopKeepalive:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.listenKeys.Keepalive(ctx, listenKey); err != nil {
					log.Printf("binance listenKey keepalive failed: %v", err)
				}
			}
		}
	}()
	return nil
}

func (r *BinanceUserOrderRuntime) Stop() {
	r.stopOnce.Do(func() {
		if r.stopKeepalive != nil {
			close(r.stopKeepalive)
		}
		if r.monitor != nil {
			r.monitor.Stop()
		}
	})
}

type binanceListenKeyService struct {
	client *futures.Client
}

func (s *binanceListenKeyService) Start(ctx context.Context) (string, error) {
	return s.client.NewStartUserStreamService().Do(ctx)
}

func (s *binanceListenKeyService) Keepalive(ctx context.Context, listenKey string) error {
	return s.client.NewKeepaliveUserStreamService().ListenKey(listenKey).Do(ctx)
}

type BinanceUserOrderRuntimeFactory struct {
	repo            *persistence.StateRepository
	orderServiceURL string
	testnet         bool
	ruleUpdater     ruleStatusUpdater
	notifier        notification.Notifier
}

func NewBinanceUserOrderRuntimeFactory(repo *persistence.StateRepository, orderServiceURL string, testnet bool) *BinanceUserOrderRuntimeFactory {
	return &BinanceUserOrderRuntimeFactory{repo: repo, orderServiceURL: orderServiceURL, testnet: testnet}
}

func NewBinanceUserOrderRuntimeFactoryWithRuleUpdater(repo *persistence.StateRepository, orderServiceURL string, testnet bool, updater ruleStatusUpdater, notifier notification.Notifier) *BinanceUserOrderRuntimeFactory {
	return &BinanceUserOrderRuntimeFactory{repo: repo, orderServiceURL: orderServiceURL, testnet: testnet, ruleUpdater: updater, notifier: notifier}
}

func (f *BinanceUserOrderRuntimeFactory) NewUserOrderRuntime(user *order.User) (UserOrderRuntime, error) {
	if user == nil || user.Exchange != "binance" {
		return nil, fmt.Errorf("not a binance user")
	}
	client := futures.NewClient(user.APIKey, user.APISecret)
	if f.testnet {
		client.BaseURL = "https://testnet.binancefuture.com"
		futures.UseTestnet = true
	}
	listenKeys := &binanceListenKeyService{client: client}
	if f.repo == nil {
		return &BinanceUserOrderRuntime{listenKeys: listenKeys, monitor: noopOrderMonitor{}}, nil
	}

	ex := binance.NewBinanceFutures(user.APIKey, user.APISecret, f.testnet)
	exec := exchange.NewOrderExecutor(f.repo, ex)
	if f.orderServiceURL != "" {
		exec.SetRPCClient(rpc.NewOrderServiceClient(f.orderServiceURL))
	}
	if f.ruleUpdater != nil {
		exec.SetRuleStatusUpdater(f.ruleUpdater)
	}
	if f.notifier != nil {
		exec.SetNotifier(f.notifier)
	}
	monitor := exchangews.NewOrderMonitor(exec, f.repo)
	return &BinanceUserOrderRuntime{listenKeys: listenKeys, monitor: monitor}, nil
}

type noopOrderMonitor struct{}

func (noopOrderMonitor) StartFuturesUserDataWS(listenKeyProvider func() (string, error)) {}
func (noopOrderMonitor) Stop()                                                           {}
