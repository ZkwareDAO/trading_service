package api

import (
	"encoding/json"
	"log"
	"net/http"

	"trading-service/internal/order"
)

// CreateUserRequest represents the request body for creating a user
type CreateUserRequest struct {
	Name        string `json:"name"`
	Exchange    string `json:"exchange"`
	APIKey      string `json:"api_key"`
	APISecret   string `json:"api_secret"`
	APIPassword string `json:"api_password"` // optional
}

// createUserHandler handles POST /api/v1/users
func (s *Server) createUserHandler(w http.ResponseWriter, r *http.Request) {
	// Log request
	log.Printf("[/api/v1/users] Received request: method=%s, remote=%s, user-agent=%s",
		r.Method, r.RemoteAddr, r.Header.Get("User-Agent"))

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, 1001, "invalid JSON")
		return
	}

	// Validate required fields
	if req.Name == "" {
		writeAPIError(w, 1001, "name is required")
		return
	}
	if req.Exchange == "" {
		writeAPIError(w, 1001, "exchange is required")
		return
	}
	if req.APIKey == "" {
		writeAPIError(w, 1001, "api_key is required")
		return
	}
	if req.APISecret == "" {
		writeAPIError(w, 1001, "api_secret is required")
		return
	}

	// Check for duplicate user
	existingUsers := s.repo.ListUsers()
	for _, u := range existingUsers {
		if u.Name == req.Name && u.Exchange == req.Exchange {
			writeAPIError(w, 1001, "user already exists")
			return
		}
	}

	// Create user object (CreateUser handles timestamps internally)
	user := &order.User{
		Name:        req.Name,
		Exchange:    req.Exchange,
		APIKey:      req.APIKey,
		APISecret:   req.APISecret,
		APIPassword: req.APIPassword,
	}

	// Save user (updates memory + CSV)
	userID := s.repo.CreateUser(user)

	// TODO: RPC sync to PMS (will be implemented in next phase)

	// Return success response (safe fields only)
	writeAPISuccess(w, map[string]interface{}{
		"id":         userID,
		"name":       user.Name,
		"exchange":   user.Exchange,
		"created_at": user.CreatedAt,
		"updated_at": user.UpdatedAt,
	})
}
