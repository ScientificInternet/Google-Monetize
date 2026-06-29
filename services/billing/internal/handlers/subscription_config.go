package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	pcache "github.com/ScientificInternet/Google-Monetize/pkg/cache"
	"github.com/ScientificInternet/Google-Monetize/pkg/errors"
	ev "github.com/ScientificInternet/Google-Monetize/pkg/events"
	"github.com/ScientificInternet/Google-Monetize/pkg/middleware"
)

// subscriptionsConfigHandler handles subscription configuration management
type subscriptionsConfigHandler struct {
	db    *pgxpool.Pool
	cache *pcache.Cache
	pub   *ev.Publisher
}

// NewsubscriptionsConfigHandler creates a new subscription config handler
func NewsubscriptionsConfigHandler(db *pgxpool.Pool, cache *pcache.Cache, pub *ev.Publisher) *subscriptionsConfigHandler {
	return &subscriptionsConfigHandler{
		db:    db,
		cache: cache,
		pub:   pub,
	}
}

// GetAllConfigResponse represents the response for getting all configuration
type GetAllConfigResponse struct {
	Permissions map[string]PlanPermissionValue `json:"permissions"`
	TokenCosts  map[string]PlanTokenCostValue  `json:"tokenCosts"`
	Pricing     map[string]PlanPricingValue    `json:"pricing"`
	Version     string                         `json:"version"`
	UpdatedAt   string                         `json:"updated_at"`
}

// PlanPricingValue represents pricing values for different plans
type PlanPricingValue struct {
	Starter      interface{} `json:"starter"`
	Professional interface{} `json:"professional"`
	Elite        interface{} `json:"elite"`
}

// GetPricingResponse represents the response for getting pricing
type GetPricingResponse struct {
	Pricing map[string]PlanPricingValue `json:"pricing"`
}

// UpdatePricingRequest represents a request to update pricing
type UpdatePricingRequest struct {
	MonthlyPrice interface{} `json:"monthlyPrice"`
	YearlyPrice  interface{} `json:"yearlyPrice"`
	TokenQuota   interface{} `json:"tokenQuota"`
}

// ConfigHistoryResponse represents configuration change history
type ConfigHistoryResponse struct {
	Items      []ConfigHistoryItem `json:"items"`
	TotalCount int                 `json:"totalCount"`
	Limit      int                 `json:"limit"`
	Offset     int                 `json:"offset"`
}

// ConfigHistoryItem represents a single configuration change record
type ConfigHistoryItem struct {
	ID             string                 `json:"id"`
	ConfigType     string                 `json:"configType"`
	ConfigID       string                 `json:"configId"`
	Action         string                 `json:"action"`
	OldValue       map[string]interface{} `json:"oldValue,omitempty"`
	NewValue       map[string]interface{} `json:"newValue"`
	ChangedBy      string                 `json:"changedBy"`
	ChangedAt      string                 `json:"changedAt"`
	ChangedByEmail string                 `json:"changedByEmail,omitempty"`
}

// GetAllConfig handles GET /api/v1/billing/config/all
func (h *subscriptionsConfigHandler) GetAllConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Try cache first
	cacheKey := "subscription:config:all"
	if h.cache != nil && h.cache.Ready() {
		if cached, ok := h.cache.Get(ctx, cacheKey); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(cached))
			return
		}
	}

	// Get all configurations
	permissions, err := h.getAllPermissions(ctx)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to get permissions", map[string]string{"error": err.Error()})
		return
	}

	tokenCosts, err := h.getAllTokenCosts(ctx)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to get token costs", map[string]string{"error": err.Error()})
		return
	}

	pricing, err := h.getAllPricing(ctx)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to get pricing", map[string]string{"error": err.Error()})
		return
	}

	resp := GetAllConfigResponse{
		Permissions: permissions,
		TokenCosts:  tokenCosts,
		Pricing:     pricing,
		Version:     fmt.Sprintf("v%d", time.Now().Unix()),
		UpdatedAt:   time.Now().Format(time.RFC3339),
	}

	// Cache the result
	if h.cache != nil && h.cache.Ready() {
		if jsonData, err := json.Marshal(resp); err == nil {
			h.cache.Set(ctx, cacheKey, string(jsonData), 5*time.Minute)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// GetPricing handles GET /api/v1/billing/config/pricing
func (h *subscriptionsConfigHandler) GetPricing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	pricing, err := h.getAllPricing(ctx)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to get pricing", map[string]string{"error": err.Error()})
		return
	}

	resp := GetPricingResponse{
		Pricing: pricing,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// UpdatePricing handles PUT /api/v1/billing/config/pricing/{plan}
func (h *subscriptionsConfigHandler) UpdatePricing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	plan := chi.URLParam(r, "plan")
	if plan == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "plan is required", nil)
		return
	}

	var req UpdatePricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body", map[string]string{"error": err.Error()})
		return
	}

	// Get current user for audit
	userID, _ := r.Context().Value(middleware.UserIDKey).(string)
	if userID == "" {
		userID = "system"
	}

	// Update pricing in database
	err := h.updatePricing(ctx, plan, req, userID)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "SUB_007", "Failed to update pricing", map[string]string{"error": err.Error()})
		return
	}

	// Invalidate cache
	if h.cache != nil && h.cache.Ready() {
		h.cache.Del(ctx, "subscription:config:all")
		h.cache.Del(ctx, "pricing:all")
	}

	// Publish config updated event
	if h.pub != nil {
		event := map[string]interface{}{
			"eventId":    uuid.New().String(),
			"eventType":  "ConfigUpdated",
			"occurredAt": time.Now().Format(time.RFC3339),
			"data": map[string]interface{}{
				"configType": "pricing",
				"plan":       plan,
				"newValue": map[string]interface{}{
					"monthlyPrice": req.MonthlyPrice,
					"yearlyPrice":  req.YearlyPrice,
					"tokenQuota":   req.TokenQuota,
				},
				"changedBy": userID,
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
		"plan":         plan,
		"monthlyPrice": req.MonthlyPrice,
		"yearlyPrice":  req.YearlyPrice,
		"tokenQuota":   req.TokenQuota,
		"updated_at":    time.Now().Format(time.RFC3339),
	})
}

// GetConfigHistory handles GET /api/v1/billing/config/history
func (h *subscriptionsConfigHandler) GetConfigHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	configType := strings.TrimSpace(r.URL.Query().Get("configType"))
	startDate := strings.TrimSpace(r.URL.Query().Get("startDate"))
	endDate := strings.TrimSpace(r.URL.Query().Get("endDate"))
	changedBy := strings.TrimSpace(r.URL.Query().Get("changedBy"))

	limit := 50
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	offset := 0
	if v := strings.TrimSpace(r.URL.Query().Get("offset")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	// Query history
	history, total, err := h.getConfigHistory(ctx, configType, startDate, endDate, changedBy, limit, offset)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to get config history", map[string]string{"error": err.Error()})
		return
	}

	resp := ConfigHistoryResponse{
		Items:      history,
		TotalCount: total,
		Limit:      limit,
		Offset:     offset,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// getAllPermissions gets all permissions configuration
func (h *subscriptionsConfigHandler) getAllPermissions(ctx context.Context) (map[string]PlanPermissionValue, error) {
	// Query from database
	query := `
		SELECT feature, starter_value, professional_value, elite_value
		FROM subscription_permissions
		ORDER BY category, feature
	`

	rows, err := h.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query permissions: %w", err)
	}
	defer rows.Close()

	result := make(map[string]PlanPermissionValue)
	for rows.Next() {
		var feature string
		var starterValue, professionalValue, eliteValue []byte

		if err := rows.Scan(&feature, &starterValue, &professionalValue, &eliteValue); err != nil {
			return nil, fmt.Errorf("failed to scan permission row: %w", err)
		}

		var pv PlanPermissionValue

		// Parse JSON values
		if len(starterValue) > 0 {
			if err := json.Unmarshal(starterValue, &pv.Starter); err != nil {
				fmt.Printf("Warning: Failed to parse starter value for %s: %v\n", feature, err)
			}
		}

		if len(professionalValue) > 0 {
			if err := json.Unmarshal(professionalValue, &pv.Professional); err != nil {
				fmt.Printf("Warning: Failed to parse professional value for %s: %v\n", feature, err)
			}
		}

		if len(eliteValue) > 0 {
			if err := json.Unmarshal(eliteValue, &pv.Elite); err != nil {
				fmt.Printf("Warning: Failed to parse elite value for %s: %v\n", feature, err)
			}
		}

		result[feature] = pv
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate permission rows: %w", err)
	}

	return result, nil
}

// getAllTokenCosts gets all token costs configuration
func (h *subscriptionsConfigHandler) getAllTokenCosts(ctx context.Context) (map[string]PlanTokenCostValue, error) {
	// Query from database
	query := `
		SELECT action, starter_cost, professional_cost, elite_cost
		FROM subscription_token_costs
		ORDER BY category, action
	`

	rows, err := h.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query token costs: %w", err)
	}
	defer rows.Close()

	result := make(map[string]PlanTokenCostValue)
	for rows.Next() {
		var action string
		var starterCost, professionalCost, eliteCost []byte

		if err := rows.Scan(&action, &starterCost, &professionalCost, &eliteCost); err != nil {
			return nil, fmt.Errorf("failed to scan token cost row: %w", err)
		}

		var tcv PlanTokenCostValue

		// Parse JSON values
		if len(starterCost) > 0 {
			if err := json.Unmarshal(starterCost, &tcv.Starter); err != nil {
				fmt.Printf("Warning: Failed to parse starter cost for %s: %v\n", action, err)
			}
		}

		if len(professionalCost) > 0 {
			if err := json.Unmarshal(professionalCost, &tcv.Professional); err != nil {
				fmt.Printf("Warning: Failed to parse professional cost for %s: %v\n", action, err)
			}
		}

		if len(eliteCost) > 0 {
			if err := json.Unmarshal(eliteCost, &tcv.Elite); err != nil {
				fmt.Printf("Warning: Failed to parse elite cost for %s: %v\n", action, err)
			}
		}

		result[action] = tcv
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate token cost rows: %w", err)
	}

	return result, nil
}

// getAllPricing gets all pricing configuration
func (h *subscriptionsConfigHandler) getAllPricing(ctx context.Context) (map[string]PlanPricingValue, error) {
	// Query from database
	query := `
		SELECT plan, token_quota, monthly_amount, yearly_amount
		FROM subscription_pricing
		ORDER BY plan
	`

	rows, err := h.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query pricing: %w", err)
	}
	defer rows.Close()

	result := map[string]PlanPricingValue{
		"monthlyPrice": {},
		"yearlyPrice":  {},
		"tokenQuota":   {},
	}

	for rows.Next() {
		var plan string
		var tokenQuota, monthlyAmount, yearlyAmount int

		if err := rows.Scan(&plan, &tokenQuota, &monthlyAmount, &yearlyAmount); err != nil {
			return nil, fmt.Errorf("failed to scan pricing row: %w", err)
		}

		// Convert to appropriate format (convert cents to yuan for display)
		monthlyPrice := float64(monthlyAmount) / 100
		yearlyPrice := float64(yearlyAmount) / 100

		switch plan {
		case "starter", "Starter", "Free":
			result["monthlyPrice"] = PlanPricingValue{
				Starter:      monthlyPrice,
				Professional: result["monthlyPrice"].Professional,
				Elite:        result["monthlyPrice"].Elite,
			}
			result["yearlyPrice"] = PlanPricingValue{
				Starter:      yearlyPrice,
				Professional: result["yearlyPrice"].Professional,
				Elite:        result["yearlyPrice"].Elite,
			}
			result["tokenQuota"] = PlanPricingValue{
				Starter:      tokenQuota,
				Professional: result["tokenQuota"].Professional,
				Elite:        result["tokenQuota"].Elite,
			}

		case "professional", "Professional", "Pro":
			result["monthlyPrice"] = PlanPricingValue{
				Starter:      result["monthlyPrice"].Starter,
				Professional: monthlyPrice,
				Elite:        result["monthlyPrice"].Elite,
			}
			result["yearlyPrice"] = PlanPricingValue{
				Starter:      result["yearlyPrice"].Starter,
				Professional: yearlyPrice,
				Elite:        result["yearlyPrice"].Elite,
			}
			result["tokenQuota"] = PlanPricingValue{
				Starter:      result["tokenQuota"].Starter,
				Professional: tokenQuota,
				Elite:        result["tokenQuota"].Elite,
			}

		case "elite", "Elite", "Max":
			result["monthlyPrice"] = PlanPricingValue{
				Starter:      result["monthlyPrice"].Starter,
				Professional: result["monthlyPrice"].Professional,
				Elite:        monthlyPrice,
			}
			result["yearlyPrice"] = PlanPricingValue{
				Starter:      result["yearlyPrice"].Starter,
				Professional: result["yearlyPrice"].Professional,
				Elite:        yearlyPrice,
			}
			result["tokenQuota"] = PlanPricingValue{
				Starter:      result["tokenQuota"].Starter,
				Professional: result["tokenQuota"].Professional,
				Elite:        tokenQuota,
			}
		}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate pricing rows: %w", err)
	}

	return result, nil
}

// updatePricing updates pricing configuration
func (h *subscriptionsConfigHandler) updatePricing(ctx context.Context, plan string, req UpdatePricingRequest, changedBy string) error {
	// Get current pricing record for history
	var currentID string
	var monthlyAmount, yearlyAmount, tokenQuota int

	query := `SELECT id, monthly_amount, yearly_amount, token_quota FROM subscription_pricing WHERE plan = $1`
	err := h.db.QueryRow(ctx, query, plan).Scan(&currentID, &monthlyAmount, &yearlyAmount, &tokenQuota)

	oldValue := map[string]interface{}{
		"monthlyAmount": monthlyAmount,
		"yearlyAmount":  yearlyAmount,
		"tokenQuota":    tokenQuota,
	}
	if err != nil {
		return fmt.Errorf("failed to get current pricing: %w", err)
	}

	// Update pricing record
	updateQuery := `
		UPDATE subscription_pricing
		SET monthly_amount = $1, yearly_amount = $2, token_quota = $3, updated_at = NOW(), updated_by = $4
		WHERE plan = $5
	`

	var newMonthlyAmount, newYearlyAmount, newTokenQuota int

	// Convert interface{} to appropriate types
	switch v := req.MonthlyPrice.(type) {
	case int:
		newMonthlyAmount = v
	case float64:
		newMonthlyAmount = int(v)
	case string:
		if val, err := strconv.Atoi(v); err == nil {
			newMonthlyAmount = val
		}
	}

	switch v := req.YearlyPrice.(type) {
	case int:
		newYearlyAmount = v
	case float64:
		newYearlyAmount = int(v)
	case string:
		if val, err := strconv.Atoi(v); err == nil {
			newYearlyAmount = val
		}
	}

	switch v := req.TokenQuota.(type) {
	case int:
		newTokenQuota = v
	case float64:
		newTokenQuota = int(v)
	case string:
		if val, err := strconv.Atoi(v); err == nil {
			newTokenQuota = val
		}
	}

	_, err = h.db.Exec(ctx, updateQuery, newMonthlyAmount, newYearlyAmount, newTokenQuota, changedBy, plan)
	if err != nil {
		return fmt.Errorf("failed to update pricing: %w", err)
	}

	// Insert into config history
	historyQuery := `
		INSERT INTO subscription_config_history (config_type, config_id, action, old_value, new_value, changed_by)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	newValue := map[string]interface{}{
		"monthlyAmount": newMonthlyAmount,
		"yearlyAmount":  newYearlyAmount,
		"tokenQuota":    newTokenQuota,
	}

	_, err = h.db.Exec(ctx, historyQuery, "pricing", currentID, "update", oldValue, newValue, changedBy)
	if err != nil {
		// Log warning but don't fail the update
		fmt.Printf("Warning: Failed to insert config history: %v\n", err)
	}

	return nil
}

// getConfigHistory gets configuration change history
func (h *subscriptionsConfigHandler) getConfigHistory(ctx context.Context, configType, startDate, endDate, changedBy string, limit, offset int) ([]ConfigHistoryItem, int, error) {
	// Build query conditions
	whereConditions := []string{"1=1"}
	args := []interface{}{}
	argIndex := 1

	if configType != "" {
		whereConditions = append(whereConditions, fmt.Sprintf("config_type = $%d", argIndex))
		args = append(args, configType)
		argIndex++
	}

	if changedBy != "" {
		whereConditions = append(whereConditions, fmt.Sprintf("changed_by = $%d", argIndex))
		args = append(args, changedBy)
		argIndex++
	}

	if startDate != "" {
		whereConditions = append(whereConditions, fmt.Sprintf("changed_at >= $%d", argIndex))
		args = append(args, startDate)
		argIndex++
	}

	if endDate != "" {
		whereConditions = append(whereConditions, fmt.Sprintf("changed_at <= $%d", argIndex))
		args = append(args, endDate)
		argIndex++
	}

	whereClause := strings.Join(whereConditions, " AND ")

	// Get total count
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM subscription_config_history
		WHERE %s
	`, whereClause)

	var totalCount int
	err := h.db.QueryRow(ctx, countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count config history: %w", err)
	}

	// Get history records with pagination
	query := fmt.Sprintf(`
		SELECT
			id, config_type, config_id, action, old_value, new_value, changed_by, changed_at
		FROM subscription_config_history
		WHERE %s
		ORDER BY changed_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, limit, offset)

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query config history: %w", err)
	}
	defer rows.Close()

	var items []ConfigHistoryItem
	for rows.Next() {
		var item ConfigHistoryItem
		var oldValueJSON, newValueJSON []byte

		err := rows.Scan(
			&item.ID,
			&item.ConfigType,
			&item.ConfigID,
			&item.Action,
			&oldValueJSON,
			&newValueJSON,
			&item.ChangedBy,
			&item.ChangedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan config history row: %w", err)
		}

		// Parse JSON values
		if len(oldValueJSON) > 0 {
			if err := json.Unmarshal(oldValueJSON, &item.OldValue); err != nil {
				fmt.Printf("Warning: Failed to parse old_value JSON: %v\n", err)
			}
		}

		if err := json.Unmarshal(newValueJSON, &item.NewValue); err != nil {
			return nil, 0, fmt.Errorf("failed to parse new_value JSON: %w", err)
		}

		items = append(items, item)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("failed to iterate config history rows: %w", err)
	}

	return items, totalCount, nil
}
