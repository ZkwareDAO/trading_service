package ws

import (
	"sync"
	"testing"

	"github.com/sonirico/go-hyperliquid"
)

// fakeWsAllMidsClient lets tests inject AllMids callbacks and trigger them.
type fakeWsAllMidsClient struct {
	mu           sync.Mutex
	callback     func(hyperliquid.AllMids, error)
	connectErr   error
	subscribeErr error
	closed       bool
}

func (f *fakeWsAllMidsClient) Connect() error { return f.connectErr }
func (f *fakeWsAllMidsClient) Close() error   { f.closed = true; return nil }

func (f *fakeWsAllMidsClient) SubscribeAllMids(fn func(hyperliquid.AllMids, error)) error {
	f.mu.Lock()
	f.callback = fn
	f.mu.Unlock()
	return f.subscribeErr
}

func (f *fakeWsAllMidsClient) fireAllMids(mids map[string]string) {
	f.mu.Lock()
	cb := f.callback
	f.mu.Unlock()
	if cb != nil {
		cb(hyperliquid.AllMids{Mids: mids}, nil)
	}
}

func (f *fakeWsAllMidsClient) fireError(err error) {
	f.mu.Lock()
	cb := f.callback
	f.mu.Unlock()
	if cb != nil {
		cb(hyperliquid.AllMids{}, err)
	}
}

func TestHyperliquidWsPriceManager_ConnectSubscribes(t *testing.T) {
	fake := &fakeWsAllMidsClient{}
	m := NewHyperliquidWsPriceManager(WithTestClientOption(fake))

	if err := m.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if fake.callback == nil {
		t.Fatal("expected SubscribeAllMids to be called")
	}
}

func TestHyperliquidWsPriceManager_ReceivesPriceUpdates(t *testing.T) {
	fake := &fakeWsAllMidsClient{}
	m := NewHyperliquidWsPriceManager(WithTestClientOption(fake))
	_ = m.Connect()

	fake.fireAllMids(map[string]string{
		"BTC":     "50000",
		"ETH":     "3000",
		"ETHUSDC": "2800",
	})

	snap := m.Manager.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected 3 prices, got %d: %+v", len(snap), snap)
	}
	if snap["BTC"] != 50000 {
		t.Errorf("expected BTC=50000, got %f", snap["BTC"])
	}
	if snap["ETH"] != 3000 {
		t.Errorf("expected ETH=3000, got %f", snap["ETH"])
	}
	if snap["ETHUSDC"] != 2800 {
		t.Errorf("expected ETHUSDC=2800, got %f", snap["ETHUSDC"])
	}
}

func TestHyperliquidWsPriceManager_OverwritesOldPrices(t *testing.T) {
	fake := &fakeWsAllMidsClient{}
	m := NewHyperliquidWsPriceManager(WithTestClientOption(fake))
	_ = m.Connect()

	fake.fireAllMids(map[string]string{"BTC": "50000"})
	snap := m.Manager.Snapshot()
	if snap["BTC"] != 50000 {
		t.Fatalf("expected BTC=50000, got %f", snap["BTC"])
	}

	fake.fireAllMids(map[string]string{"BTC": "55000"})
	snap = m.Manager.Snapshot()
	if snap["BTC"] != 55000 {
		t.Fatalf("expected BTC=55000 after update, got %f", snap["BTC"])
	}
}

func TestHyperliquidWsPriceManager_IgnoresInvalidPrices(t *testing.T) {
	fake := &fakeWsAllMidsClient{}
	m := NewHyperliquidWsPriceManager(WithTestClientOption(fake))
	_ = m.Connect()

	fake.fireAllMids(map[string]string{
		"BTC": "50000",
		"ETH": "not-a-number",
	})
	snap := m.Manager.Snapshot()
	if snap["BTC"] != 50000 {
		t.Errorf("expected BTC=50000, got %f", snap["BTC"])
	}
	if _, ok := snap["ETH"]; ok {
		t.Error("expected ETH to be ignored (invalid price)")
	}
}

func TestHyperliquidWsPriceManager_CloseClosesClient(t *testing.T) {
	fake := &fakeWsAllMidsClient{}
	m := NewHyperliquidWsPriceManager(WithTestClientOption(fake))
	_ = m.Connect()

	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !fake.closed {
		t.Error("expected underlying client to be closed")
	}
}

func TestHyperliquidWsPriceManager_ConnectError(t *testing.T) {
	fake := &fakeWsAllMidsClient{connectErr: errFakeConnect}
	m := NewHyperliquidWsPriceManager(WithTestClientOption(fake))

	if err := m.Connect(); err == nil {
		t.Fatal("expected connect error")
	}
}

func TestHyperliquidWsPriceManager_SubscribeError(t *testing.T) {
	fake := &fakeWsAllMidsClient{subscribeErr: errFakeSubscribe}
	m := NewHyperliquidWsPriceManager(WithTestClientOption(fake))

	if err := m.Connect(); err == nil {
		t.Fatal("expected subscribe error")
	}
}

func TestHyperliquidWsPriceManager_CallbackErrorLogged(t *testing.T) {
	fake := &fakeWsAllMidsClient{}
	m := NewHyperliquidWsPriceManager(WithTestClientOption(fake))
	_ = m.Connect()

	fake.fireAllMids(map[string]string{"BTC": "50000"})
	fake.fireError(errFakeCallback)

	snap := m.Manager.Snapshot()
	if snap["BTC"] != 50000 {
		t.Errorf("expected BTC to survive callback error, got %f", snap["BTC"])
	}
}

var errFakeConnect = errorf("connect failed")
var errFakeSubscribe = errorf("subscribe failed")
var errFakeCallback = errorf("callback error")

type errorf string

func (e errorf) Error() string { return string(e) }
