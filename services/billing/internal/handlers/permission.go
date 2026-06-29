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

// PermissionHandler handles permission checking and configuration
type PermissionHandler struct {
	db    *pgxpool.Pool
	cache *pcache.Cache
	pub   *ev.Publisher
}

// NewPermissionHandler creates a new permission handler
func NewPermissionHandler(db *pgxpool.Pool, cache *pcache.Cache, pub *ev.Publisher) *PermissionHandler {
	return &PermissionHandler{
		db:    db,
		cache: cache,
		pub:   pub,
	}
}

// CheckPermissionRequest represents a permission check request
type CheckPermissionRequest struct {
	UserID  string `json:"user_id"`
	Feature string `json:"feature"`
}

// CheckPermissionResponse represents a permission check response
type CheckPermissionResponse struct {
	Allowed      bool        `json:"allowed"`
	Value        interface{} `json:"value,omitempty"`
	Plan         string      `json:"plan,omitempty"`
	Reason       string      `json:"reason,omitempty"`
	RequiredPlan string      `json:"requiredPlan,omitempty"`
	ErrorCode    string      `json:"errorCode,omitempty"`
}

// GetPermissionsResponse represents the response for getting permissions
type GetPermissionsResponse struct {
	Permissions map[string]PlanPermissionValue `json:"permissions"`
	Plan        string                         `json:"plan"`
}

// PlanPermissionValue represents permission values for different plans
type PlanPermissionValue struct {
	Starter      interface{} `json:"starter"`
	Professional interface{} `json:"professional"`
	Elite        interface{} `json:"elite"`
}

// UpdatePermissionRequest represents a request to update permission
type UpdatePermissionRequest struct {
	Starter      interface{} `json:"starter"`
	Professional interface{} `json:"professional"`
	Elite        interface{} `json:"elite"`
}

// CheckPermission handles POST /api/v1/billing/permissions/check
func (h *PermissionHandler) CheckPermission(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CheckPermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body", map[string]string{"error": err.Error()})
		return
	}

	if req.UserID == "" || req.Feature == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "user_id and feature are required", nil)
		return
	}

	// Get user's subscription plan
	plan, err := h.getUserPlan(ctx, req.UserID)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to get user plan", map[string]string{"error": err.Error()})
		return
	}

	// Check permission
	resp, err := h.checkPermission(ctx, plan, req.Feature)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to check permission", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// GetPermissions handles GET /api/v1/billing/config/permissions
func (h *PermissionHandler) GetPermissions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	permissions, err := h.getAllPermissions(ctx)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to get permissions", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(permissions)
}

// UpdatePermission handles PUT /api/v1/billing/config/permissions/{feature}
func (h *PermissionHandler) UpdatePermission(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	feature := chi.URLParam(r, "feature")
	if feature == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "feature is required", nil)
		return
	}

	var req UpdatePermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body", map[string]string{"error": err.Error()})
		return
	}

	// Update permission in database
	err := h.updatePermission(ctx, feature, req)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "SUB_007", "Failed to update permission", map[string]string{"error": err.Error()})
		return
	}

	// Invalidate cache
	if h.cache != nil && h.cache.Ready() {
		h.cache.Del(ctx, "permissions:all")
		h.cache.Del(ctx, "permissions:plan:starter")
		h.cache.Del(ctx, "permissions:plan:professional")
		h.cache.Del(ctx, "permissions:plan:elite")
	}

	// Publish config updated event
	if h.pub != nil {
		event := map[string]interface{}{
			"eventId":    fmt.Sprintf("config-%d", time.Now().UnixNano()),
			"eventType":  "ConfigUpdated",
			"occurredAt": time.Now().Format(time.RFC3339),
			"data": map[string]interface{}{
				"configType": "permission",
				"feature":    feature,
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
		"feature":      feature,
		"starter":      req.Starter,
		"professional": req.Professional,
		"elite":        req.Elite,
		"updated_at":    time.Now().Format(time.RFC3339),
	})
}

// getUserPlan gets the user's subscription plan
func (h *PermissionHandler) getUserPlan(ctx context.Context, userID string) (string, error) {
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

// checkPermission checks if a plan has permission for a feature
func (h *PermissionHandler) checkPermission(ctx context.Context, plan, feature string) (*CheckPermissionResponse, error) {
	// Get permissions from cache or database
	cacheKey := fmt.Sprintf("permissions:plan:%s", plan)
	var permissions map[string]interface{}

	if h.cache != nil && h.cache.Ready() {
		if cached, ok := h.cache.Get(ctx, cacheKey); ok {
			if err := json.Unmarshal([]byte(cached), &permissions); err == nil {
				return h.evaluatePermission(plan, feature, permissions), nil
			}
		}
	}

	// Query from database (using subscription_plan_configs table)
	query := `
		SELECT permissions
		FROM subscription_plan_configs
		WHERE tier = $1 AND is_active = true
		LIMIT 1
	`

	var permissionsJSON []byte
	err := h.db.QueryRow(ctx, query, plan).Scan(&permissionsJSON)
	if err != nil {
		// Return default deny if plan not found
		return &CheckPermissionResponse{
			Allowed:      false,
			Reason:       "Plan configuration not found",
			ErrorCode:    "SUB_006",
			RequiredPlan: "Professional",
		}, nil
	}

	if err := json.Unmarshal(permissionsJSON, &permissions); err != nil {
		return nil, fmt.Errorf("failed to parse permissions: %w", err)
	}

	// Cache the result
	if h.cache != nil && h.cache.Ready() {
		if jsonData, err := json.Marshal(permissions); err == nil {
			h.cache.Set(ctx, cacheKey, string(jsonData), 5*time.Minute)
		}
	}

	return h.evaluatePermission(plan, feature, permissions), nil
}

// evaluatePermission evaluates if a permission is granted
func (h *PermissionHandler) evaluatePermission(plan, feature string, permissions map[string]interface{}) *CheckPermissionResponse {
	value, exists := permissions[feature]
	if !exists {
		// For features not in permission control, all users can use them
		return &CheckPermissionResponse{
			Allowed: true,
			Value:   nil,
			Plan:    plan,
			Reason:  "Feature not under permission control - available to all plans",
		}
	}

	// Boolean permission
	if boolValue, ok := value.(bool); ok {
		if boolValue {
			return &CheckPermissionResponse{
				Allowed: true,
				Value:   boolValue,
				Plan:    plan,
			}
		}
		return &CheckPermissionResponse{
			Allowed:      false,
			Reason:       "Current plan does not support this feature",
			ErrorCode:    "SUB_005",
			RequiredPlan: "Professional",
		}
	}

	// Numeric quota
	if numValue, ok := value.(float64); ok {
		if numValue > 0 {
			return &CheckPermissionResponse{
				Allowed: true,
				Value:   int(numValue),
				Plan:    plan,
			}
		}
		return &CheckPermissionResponse{
			Allowed:      false,
			Reason:       "Current plan does not support this feature",
			ErrorCode:    "SUB_005",
			RequiredPlan: "Professional",
		}
	}

	// Other types (string, array, etc.)
	return &CheckPermissionResponse{
		Allowed: true,
		Value:   value,
		Plan:    plan,
	}
}

// getAllPermissions gets all permissions configuration
func (h *PermissionHandler) getAllPermissions(ctx context.Context) (map[string]PlanPermissionValue, error) {
	// Try cache first
	cacheKey := "permissions:all"
	if h.cache != nil && h.cache.Ready() {
		if cached, ok := h.cache.Get(ctx, cacheKey); ok {
			var permissions map[string]PlanPermissionValue
			if err := json.Unmarshal([]byte(cached), &permissions); err == nil {
				return permissions, nil
			}
		}
	}

	// Query from database
	query := `
		SELECT tier, permissions
		FROM subscription_plan_configs
		WHERE is_active = true
		ORDER BY tier
	`

	rows, err := h.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query permissions: %w", err)
	}
	defer rows.Close()

	planPermissions := make(map[string]map[string]interface{})
	for rows.Next() {
		var tier string
		var permissionsJSON []byte

		if err := rows.Scan(&tier, &permissionsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		var permissions map[string]interface{}
		if err := json.Unmarshal(permissionsJSON, &permissions); err != nil {
			return nil, fmt.Errorf("failed to parse permissions: %w", err)
		}

		planPermissions[tier] = permissions
	}

	// Reorganize by feature
	result := make(map[string]PlanPermissionValue)
	for tier, permissions := range planPermissions {
		for feature, value := range permissions {
			if _, exists := result[feature]; !exists {
				result[feature] = PlanPermissionValue{}
			}

			pv := result[feature]
			switch tier {
			case "Free", "Starter":
				pv.Starter = value
			case "Pro", "Professional":
				pv.Professional = value
			case "Max", "Elite":
				pv.Elite = value
			}
			result[feature] = pv
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

// updatePermission updates a permission configuration
func (h *PermissionHandler) updatePermission(ctx context.Context, feature string, req UpdatePermissionRequest) error {
	// This is a simplified implementation
	// In production, you would update the subscription_plan_configs table
	// For now, we'll just log the update
	fmt.Printf("Updating permission %s: starter=%v, professional=%v, elite=%v\n",
		feature, req.Starter, req.Professional, req.Elite)

	// TODO: Implement actual database update
	// UPDATE subscription_plan_configs SET permissions = jsonb_set(permissions, '{feature}', value)

	return nil
}
