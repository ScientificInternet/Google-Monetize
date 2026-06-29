package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pcache "github.com/ScientificInternet/Google-Monetize/pkg/cache"
	"github.com/ScientificInternet/Google-Monetize/pkg/errors"
	ev "github.com/ScientificInternet/Google-Monetize/pkg/events"
)

// TokenCostHandler handles token cost calculation and configuration
type TokenCostHandler struct {
	db    *pgxpool.Pool
	cache *pcache.Cache
	pub   *ev.Publisher
}

// NewTokenCostHandler creates a new token cost handler
func NewTokenCostHandler(db *pgxpool.Pool, cache *pcache.Cache, pub *ev.Publisher) *TokenCostHandler {
	return &TokenCostHandler{
		db:    db,
		cache: cache,
		pub:   pub,
	}
}

// GetTokenCostRequest represents a request to get token cost
type GetTokenCostRequest struct {
	UserID string `json:"user_id"`
	Action string `json:"action"`
}

// GetTokenCostResponse represents the response for token cost
type GetTokenCostResponse struct {
	Cost      int    `json:"cost"`
	Plan      string `json:"plan"`
	Supported bool   `json:"supported"`
	ErrorCode string `json:"errorCode,omitempty"`
}

// GetTokenCostsResponse represents the response for getting all token costs
type GetTokenCostsResponse struct {
	TokenCosts map[string]PlanTokenCostValue `json:"tokenCosts"`
}

// PlanTokenCostValue represents token cost values for different plans
type PlanTokenCostValue struct {
	Starter      interface{} `json:"starter"`
	Professional interface{} `json:"professional"`
	Elite        interface{} `json:"elite"`
}

// UpdateTokenCostRequest represents a request to update token cost
type UpdateTokenCostRequest struct {
	Starter      interface{} `json:"starter"`
	Professional interface{} `json:"professional"`
	Elite        interface{} `json:"elite"`
}

// GetTokenCost handles POST /api/v1/billing/tokens/cost
func (h *TokenCostHandler) GetTokenCost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req GetTokenCostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body", map[string]string{"error": err.Error()})
		return
	}

	if req.UserID == "" || req.Action == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "user_id and action are required", nil)
		return
	}

	// Get user's subscription plan
	plan, err := h.getUserPlan(ctx, req.UserID)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to get user plan", map[string]string{"error": err.Error()})
		return
	}

	// Get token cost
	resp, err := h.getTokenCost(ctx, plan, req.Action)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to get token cost", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// GetTokenCosts handles GET /api/v1/billing/config/token-costs
func (h *TokenCostHandler) GetTokenCosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tokenCosts, err := h.getAllTokenCosts(ctx)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to get token costs", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(tokenCosts)
}

// UpdateTokenCost handles PUT /api/v1/billing/config/token-costs/{action}
func (h *TokenCostHandler) UpdateTokenCost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	action := chi.URLParam(r, "action")
	if action == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "action is required", nil)
		return
	}

	var req UpdateTokenCostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body", map[string]string{"error": err.Error()})
		return
	}

	// Update token cost in database
	err := h.updateTokenCost(ctx, action, req)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "SUB_007", "Failed to update token cost", map[string]string{"error": err.Error()})
		return
	}

	// Invalidate cache
	if h.cache != nil && h.cache.Ready() {
		h.cache.Del(ctx, "token-costs:all")
		h.cache.Del(ctx, "token-costs:plan:starter")
		h.cache.Del(ctx, "token-costs:plan:professional")
		h.cache.Del(ctx, "token-costs:plan:elite")
	}

	// Publish config updated event
	if h.pub != nil {
		event := map[string]interface{}{
			"eventId":    fmt.Sprintf("config-%d", time.Now().UnixNano()),
			"eventType":  "ConfigUpdated",
			"occurredAt": time.Now().Format(time.RFC3339),
			"data": map[string]interface{}{
				"configType": "token_cost",
				"action":     action,
				"newValue": map[string]interface{}{
					"starter":      req.Starter,
					"professional": req.Professional,
					"elite":        req.Elite,
				},
			},
		}

		err = h.pub.Publish(ctx, "config.updated", event)
		if err != nil {
			fmt.Printf("Warning: Failed to publish ConfigUpdated event: %v\n", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"action":       action,
		"starter":      req.Starter,
		"professional": req.Professional,
		"elite":        req.Elite,
		"updated_at":    time.Now().Format(time.RFC3339),
	})
}

// getUserPlan gets the user's subscription plan
func (h *TokenCostHandler) getUserPlan(ctx context.Context, userID string) (string, error) {
	// Try cache first
	cacheKey := fmt.Sprintf("subscription:user:%s", userID)
	if h.cache != nil && h.cache.Ready() {
		if cached, ok := h.cache.Get(ctx, cacheKey); ok {
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(cached), &data); err == nil {
				if plan, ok := data["plan"].(string); ok {
					return plan, nil
				}
			}
		}
	}

	// Query from database
	var plan_name string
	query := `
		SELECT "plan_name"
		FROM "subscriptions"
		WHERE "user_id" = $1 AND status IN ('active', 'trialing')
		ORDER BY "created_at" DESC
		LIMIT 1
	`

	err := h.db.QueryRow(ctx, query, userID).Scan(&plan_name)
	if err != nil {
		// Default to Free plan if no subscription found
		return "Free", nil
	}

	// Cache the result
	if h.cache != nil && h.cache.Ready() {
		data := map[string]interface{}{
			"user_id": userID,
			"plan":   plan_name,
		}
		if jsonData, err := json.Marshal(data); err == nil {
			h.cache.Set(ctx, cacheKey, string(jsonData), 5*time.Minute)
		}
	}

	return plan_name, nil
}

// getTokenCost gets the token cost for an action
func (h *TokenCostHandler) getTokenCost(ctx context.Context, plan, action string) (*GetTokenCostResponse, error) {
	// Get token costs from cache or database
	cacheKey := fmt.Sprintf("token-costs:plan:%s", plan)
	var tokenCosts map[string]interface{}

	if h.cache != nil && h.cache.Ready() {
		if cached, ok := h.cache.Get(ctx, cacheKey); ok {
			if err := json.Unmarshal([]byte(cached), &tokenCosts); err == nil {
				return h.evaluateTokenCost(plan, action, tokenCosts), nil
			}
		}
	}

	// Query from database (using subscription_plan_configs table)
	query := `
		SELECT token_costs
		FROM subscription_plan_configs
		WHERE tier = $1 AND is_active = true
		LIMIT 1
	`

	var tokenCostsJSON []byte
	err := h.db.QueryRow(ctx, query, plan).Scan(&tokenCostsJSON)
	if err != nil {
		// Return unsupported if plan not found
		return &GetTokenCostResponse{
			Cost:      0,
			Plan:      plan,
			Supported: false,
			ErrorCode: "SUB_005",
		}, nil
	}

	if err := json.Unmarshal(tokenCostsJSON, &tokenCosts); err != nil {
		return nil, fmt.Errorf("failed to parse token costs: %w", err)
	}

	// Cache the result
	if h.cache != nil && h.cache.Ready() {
		if jsonData, err := json.Marshal(tokenCosts); err == nil {
			h.cache.Set(ctx, cacheKey, string(jsonData), 5*time.Minute)
		}
	}

	return h.evaluateTokenCost(plan, action, tokenCosts), nil
}

// evaluateTokenCost evaluates the token cost for an action
func (h *TokenCostHandler) evaluateTokenCost(plan, action string, tokenCosts map[string]interface{}) *GetTokenCostResponse {
	value, exists := tokenCosts[action]
	if !exists {
		return &GetTokenCostResponse{
			Cost:      0,
			Plan:      plan,
			Supported: false,
			ErrorCode: "SUB_005",
		}
	}

	// Check for "unsupported" string value
	if strValue, ok := value.(string); ok && strValue == "unsupported" {
		return &GetTokenCostResponse{
			Cost:      0,
			Plan:      plan,
			Supported: false,
			ErrorCode: "SUB_005",
		}
	}

	// Numeric cost
	if numValue, ok := value.(float64); ok {
		return &GetTokenCostResponse{
			Cost:      int(numValue),
			Plan:      plan,
			Supported: true,
		}
	}

	// Default to unsupported
	return &GetTokenCostResponse{
		Cost:      0,
		Plan:      plan,
		Supported: false,
		ErrorCode: "SUB_005",
	}
}

// getAllTokenCosts gets all token costs configuration
func (h *TokenCostHandler) getAllTokenCosts(ctx context.Context) (map[string]PlanTokenCostValue, error) {
	// Try cache first
	cacheKey := "token-costs:all"
	if h.cache != nil && h.cache.Ready() {
		if cached, ok := h.cache.Get(ctx, cacheKey); ok {
			var tokenCosts map[string]PlanTokenCostValue
			if err := json.Unmarshal([]byte(cached), &tokenCosts); err == nil {
				return tokenCosts, nil
			}
		}
	}

	// Query from database
	query := `
		SELECT tier, token_costs
		FROM subscription_plan_configs
		WHERE is_active = true
		ORDER BY tier
	`

	rows, err := h.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query token costs: %w", err)
	}
	defer rows.Close()

	planTokenCosts := make(map[string]map[string]interface{})
	for rows.Next() {
		var tier string
		var tokenCostsJSON []byte

		if err := rows.Scan(&tier, &tokenCostsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		var tokenCosts map[string]interface{}
		if err := json.Unmarshal(tokenCostsJSON, &tokenCosts); err != nil {
			return nil, fmt.Errorf("failed to parse token costs: %w", err)
		}

		planTokenCosts[tier] = tokenCosts
	}

	// Reorganize by action
	result := make(map[string]PlanTokenCostValue)
	for tier, tokenCosts := range planTokenCosts {
		for action, value := range tokenCosts {
			if _, exists := result[action]; !exists {
				result[action] = PlanTokenCostValue{}
			}

			tcv := result[action]
			switch tier {
			case "Free", "Starter":
				tcv.Starter = value
			case "Pro", "Professional":
				tcv.Professional = value
			case "Max", "Elite":
				tcv.Elite = value
			}
			result[action] = tcv
		}
	}

	// Cache the result
	if h.cache != nil && h.cache.Ready() {
		if jsonData, err := json.Marshal(result); err == nil {
			h.cache.Set(ctx, cacheKey, string(jsonData), 5*time.Minute)
		}
	}

	return result, nil
}

// updateTokenCost updates a token cost configuration
func (h *TokenCostHandler) updateTokenCost(ctx context.Context, action string, req UpdateTokenCostRequest) error {
	// This is a simplified implementation
	// In production, you would update the subscription_plan_configs table
	// For now, we'll just log the update
	fmt.Printf("Updating token cost %s: starter=%v, professional=%v, elite=%v\n",
		action, req.Starter, req.Professional, req.Elite)

	// TODO: Implement actual database update
	// UPDATE subscription_plan_configs SET token_costs = jsonb_set(token_costs, '{action}', value)

	return nil
}
