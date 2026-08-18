package signal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"trading-service/internal/rpc"
)

func TestCloseRuleWriter_RPC_CreateRule(t *testing.T) {
	// Setup test server simulating PMS's RPC endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc/v1/rules/create" {
			http.NotFound(w, r)
			return
		}

		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Decode and validate request
		var req rpc.CreateRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		// Check it's a close rule (condition_name=always, action=reduce)
		if req.ConditionName != "always" {
			http.Error(w, "expected condition_name=always for close rule", http.StatusBadRequest)
			return
		}
		if req.Action != "reduce" {
			http.Error(w, "expected action=reduce for close rule", http.StatusBadRequest)
			return
		}
		if req.Operator != "==" {
			http.Error(w, "expected operator=== for close rule", http.StatusBadRequest)
			return
		}
		if req.Value != "true" {
			http.Error(w, "expected value=true for close rule", http.StatusBadRequest)
			return
		}
		if req.Sort != 1 {
			http.Error(w, "expected sort=1 for close rule", http.StatusBadRequest)
			return
		}

		// Check params
		if req.Params == nil {
			http.Error(w, "params required for close rule", http.StatusBadRequest)
			return
		}
		orderType, ok := req.Params["order_type"]
		if !ok {
			http.Error(w, "order_type param required", http.StatusBadRequest)
			return
		}
		if orderType != 1.0 { // JSON unmarshals numbers as float64
			http.Error(w, "expected order_type=1, got %v", http.StatusBadRequest)
			return
		}
		quantityPct, ok := req.Params["quantity_pct"]
		if !ok {
			http.Error(w, "quantity_pct param required", http.StatusBadRequest)
			return
		}
		if quantityPct != 1.0 {
			http.Error(w, "expected quantity_pct=1.0, got %v", http.StatusBadRequest)
			return
		}

		// Return success with rule ID 999
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(rpc.CreateRuleResponse{
			Success: true,
			RuleID:  999,
		})
	}))
	defer server.Close()

	rpcClient := rpc.NewOrderServiceClient(server.URL)
	writer := NewCloseRuleWriterWithRPC(rpcClient)

	tests := []struct {
		name         string
		req          CloseRuleRequest
		wantRuleID   int
		wantErr      bool
		errContains  string
	}{
		{
			name: "creates close rule successfully",
			req: CloseRuleRequest{
				UserStrategyID: 123,
				QuantityPct:    1.0,
				OrderType:      1,
			},
			wantRuleID: 999,
			wantErr:    false,
		},
		{
			name: "creates close rule with default quantity_pct",
			req: CloseRuleRequest{
				UserStrategyID: 456,
				QuantityPct:    0, // should default to 1.0
				OrderType:      1,
			},
			wantRuleID: 999,
			wantErr:    false,
		},
		{
			name: "creates close rule with default order_type",
			req: CloseRuleRequest{
				UserStrategyID: 789,
				QuantityPct:    1.0,
				OrderType:      0, // should default to 1 (market)
			},
			wantRuleID: 999,
			wantErr:    false,
		},
		{
			name:        "fails when user_strategy_id is zero",
			req:         CloseRuleRequest{
				UserStrategyID: 0,
			},
			wantErr:     true,
			errContains: "user_strategy_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ruleID, err := writer.AppendImmediateCloseRule(ctx, tt.req)

			if tt.wantErr {
				if err == nil {
					t.Errorf("AppendImmediateCloseRule() expected error, got nil")
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("AppendImmediateCloseRule() error = %v, want contains %s", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("AppendImmediateCloseRule() unexpected error: %v", err)
				return
			}

			if ruleID != tt.wantRuleID {
				t.Errorf("AppendImmediateCloseRule() ruleID = %d, want %d", ruleID, tt.wantRuleID)
			}
		})
	}
}

func TestCloseRuleWriter_RPC_ServerError(t *testing.T) {
	// Setup server returning 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	rpcClient := rpc.NewOrderServiceClient(server.URL)
	writer := NewCloseRuleWriterWithRPC(rpcClient)

	ctx := context.Background()
	_, err := writer.AppendImmediateCloseRule(ctx, CloseRuleRequest{
		UserStrategyID: 100,
	})

	if err == nil {
		t.Error("AppendImmediateCloseRule() expected error for server error, got nil")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && s[:len(substr)] == substr)
}