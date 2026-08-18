package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateRule(t *testing.T) {
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

		// Decode request
		var req CreateRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		// Validate required fields
		if req.UserStrategyID == 0 {
			http.Error(w, "user_strategy_id required", http.StatusBadRequest)
			return
		}
		if req.ConditionName == "" {
			http.Error(w, "condition_name required", http.StatusBadRequest)
			return
		}
		if req.Action == "" {
			http.Error(w, "action required", http.StatusBadRequest)
			return
		}

		// Simulate PMS creating rule with ID 123
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(CreateRuleResponse{
			Success: true,
			RuleID:  123,
		})
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)

	tests := []struct {
		name    string
		req     CreateRuleRequest
		wantID  int
		wantErr bool
	}{
		{
			name: "creates rule successfully",
			req: CreateRuleRequest{
				UserStrategyID: 100,
				ConditionName:  "always",
				Operator:       "==",
				Value:          "true",
				Sort:           1,
				Action:         "reduce",
				Params: map[string]interface{}{
					"order_type":   1,
					"quantity_pct": 1.0,
				},
			},
			wantID:  123,
			wantErr: false,
		},
		{
			name: "fails when user_strategy_id is zero",
			req: CreateRuleRequest{
				UserStrategyID: 0,
				ConditionName:  "always",
				Action:         "reduce",
			},
			wantID:  0,
			wantErr: true,
		},
		{
			name: "fails when condition_name is empty",
			req: CreateRuleRequest{
				UserStrategyID: 100,
				ConditionName:  "",
				Action:         "reduce",
			},
			wantID:  0,
			wantErr: true,
		},
		{
			name: "fails when action is empty",
			req: CreateRuleRequest{
				UserStrategyID: 100,
				ConditionName:  "always",
				Action:         "",
			},
			wantID:  0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			resp, err := client.CreateRule(ctx, tt.req)

			if tt.wantErr {
				if err == nil {
					t.Errorf("CreateRule() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("CreateRule() unexpected error: %v", err)
				return
			}

			if resp.RuleID != tt.wantID {
				t.Errorf("CreateRule() RuleID = %d, want %d", resp.RuleID, tt.wantID)
			}

			if !resp.Success {
				t.Errorf("CreateRule() Success = false, want true")
			}
		})
	}
}

func TestCreateRule_Timeout(t *testing.T) {
	// Setup slow server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay beyond client timeout
		time.Sleep(15 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)
	// Client has 10s timeout by default

	ctx := context.Background()
	_, err := client.CreateRule(ctx, CreateRuleRequest{
		UserStrategyID: 100,
		ConditionName:  "always",
		Action:         "reduce",
	})

	if err == nil {
		t.Error("CreateRule() expected timeout error, got nil")
	}
}

func TestCreateRule_ServerError(t *testing.T) {
	// Setup server returning 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewOrderServiceClient(server.URL)

	ctx := context.Background()
	_, err := client.CreateRule(ctx, CreateRuleRequest{
		UserStrategyID: 100,
		ConditionName:  "always",
		Action:         "reduce",
	})

	if err == nil {
		t.Error("CreateRule() expected error for 500 status, got nil")
	}
}