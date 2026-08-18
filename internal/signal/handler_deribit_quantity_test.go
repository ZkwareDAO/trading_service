package signal

import (
	"testing"
	"time"

	"trading-service/internal/order"
	"trading-service/internal/persistence"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeribitQuantityValidation verifies that Deribit signals require quantity field.
func TestDeribitQuantityValidation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	gs, err := persistence.NewGlobalState(dir)
	require.NoError(t, err)
	defer gs.Shutdown()

	repo := persistence.NewStateRepository(gs)
	repo.SetSyncInterval(24 * 3600 * time.Second)

	h := NewHandlerWithDataDirAndTestnetConfig(repo, dir, false, false, true, nil, nil)

	user := &order.User{
		ID:          1,
		Name:        "deribit_test_user",
		Exchange:    "deribit",
		APIKey:      "test_api_key",
		APISecret:   "test_api_secret",
		APIPassword: "test_api_password",
	}
	repo.CreateUser(user)

	strategy := &order.Strategy{
		ID:           1,
		Name:         "test_strategy",
			StrategyType: "test",
		Description:  "test",
		Params:       "{}",
	}
	repo.CreateStrategy(strategy)

	userStrategy := &order.UserStrategy{
		ID:         1,
		UserID:     user.ID,
		StrategyID: strategy.ID,
		Cash:       1000.0,
		Status:     1,
	}
	repo.CreateUserStrategy(userStrategy)

	tests := []struct {
		name    string
		msg     *StrategySignal
		wantErr string
	}{
		{
			name: "deribit with quantity succeeds",
			msg: &StrategySignal{
				UserID: user.ID,
				Symbol: "BTC-13JUL26-64000-P",
				Strategy: StrategyConfig{
					Name:        "test_strategy",
					ValidBefore: CustomTime{Time: time.Now().Add(1 * time.Hour)},
				},
				Signal: SignalOrderConfig{
					Exchange:  "deribit",
					Quantity:  5.0,
					OrderType: orderTypeLimit,
					Side:      int(order.SideLong),
					Action:    ActionBuy,
				},
				PosType:      order.PosTypeOptions,
			StrategyType: "test",
			},
			wantErr: "",
		},
		{
			name: "deribit without quantity fails",
			msg: &StrategySignal{
				UserID: user.ID,
				Symbol: "BTC-13JUL26-64000-P",
				Strategy: StrategyConfig{
					Name:        "test_strategy",
					ValidBefore: CustomTime{Time: time.Now().Add(1 * time.Hour)},
				},
				Signal: SignalOrderConfig{
					Exchange:  "deribit",
					Cash:      100.0,
					OrderType: orderTypeLimit,
					Side:      int(order.SideLong),
					Action:    ActionBuy,
				},
				PosType:      order.PosTypeOptions,
			StrategyType: "test",
			},
			wantErr: "quantity is required for deribit options",
		},
		{
			name: "deribit with quantity=0 fails",
			msg: &StrategySignal{
				UserID: user.ID,
				Symbol: "BTC-13JUL26-64000-P",
				Strategy: StrategyConfig{
					Name:        "test_strategy",
					ValidBefore: CustomTime{Time: time.Now().Add(1 * time.Hour)},
				},
				Signal: SignalOrderConfig{
					Exchange:  "deribit",
					Quantity:  0.0,
					OrderType: orderTypeLimit,
					Side:      int(order.SideLong),
					Action:    ActionBuy,
				},
				PosType:      order.PosTypeOptions,
			StrategyType: "test",
			},
			wantErr: "quantity is required for deribit options",
		},
		{
			name: "deribit with market order succeeds",
			msg: &StrategySignal{
				UserID: user.ID,
				Symbol: "BTC-13JUL26-64000-P",
				Strategy: StrategyConfig{
					Name:        "test_strategy",
					ValidBefore: CustomTime{Time: time.Now().Add(1 * time.Hour)},
				},
				Signal: SignalOrderConfig{
					Exchange:  "deribit",
					Quantity:  5.0,
					OrderType: orderTypeMarket,
					Side:      int(order.SideLong),
					Action:    ActionBuy,
				},
				PosType:      order.PosTypeOptions,
			StrategyType: "test",
			},
			wantErr: "", // Market orders now supported for Deribit
		},
		{
			name: "non-deribit with cash succeeds",
			msg: &StrategySignal{
				UserID: user.ID,
				Symbol: "BTCUSDT",
				Strategy: StrategyConfig{
					Name:        "test_strategy",
					ValidBefore: CustomTime{Time: time.Now().Add(1 * time.Hour)},
				},
				Signal: SignalOrderConfig{
					Exchange:  "binance",
					Cash:      100.0,
					OrderType: orderTypeLimit,
					Side:      int(order.SideLong),
					Action:    ActionBuy,
				},
				PosType:      order.PosTypeOptions,
			StrategyType: "test",
			},
			wantErr: "",
		},
		{
			name: "non-deribit without cash and quantity fails",
			msg: &StrategySignal{
				UserID: user.ID,
				Symbol: "BTCUSDT",
				Strategy: StrategyConfig{
					Name:        "test_strategy",
					ValidBefore: CustomTime{Time: time.Now().Add(1 * time.Hour)},
				},
				Signal: SignalOrderConfig{
					Exchange:  "binance",
					OrderType: orderTypeLimit,
					Side:      int(order.SideLong),
					Action:    ActionBuy,
				},
				PosType:      order.PosTypeOptions,
			StrategyType: "test",
			},
			wantErr: "cash and quantity cannot both be zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := h.validateStrategySignal(tt.msg)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
