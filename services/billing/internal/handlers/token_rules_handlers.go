package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ScientificInternet/Google-Monetize/pkg/errors"
)

// TokenRule represents a token consumption rule
// TODO(architecture): Token规则管理已从Console迁移至Billing服务
// 原因：Token规则定义计费成本，属于核心计费逻辑，应由Billing服务完全掌控
type TokenRule struct {
	ID          string    `json:"id"`
	ServiceName string    `json:"serviceName"`
	ActionType  string    `json:"actionType"`
	CostPerUnit int       `json:"costPerUnit"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// tokenRulesHandler routes /api/v1/billing/tokens/rules requests
func (h *Handler) tokenRulesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getTokenRules(w, r)
	case http.MethodPost:
		h.createTokenRule(w, r)
	default:
		errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
	}
}

// getTokenRules returns all token consumption rules
// GET /api/v1/billing/tokens/rules
func (h *Handler) getTokenRules(w http.ResponseWriter, r *http.Request) {
	// Query parameters
	enabledOnly := r.URL.Query().Get("enabledOnly") == "true"
	service := r.URL.Query().Get("service")

	query := `
		SELECT id, service_name, action_type, cost_per_unit, description, enabled, created_at, updated_at
		FROM token_consumption_rules
		WHERE 1=1
	`
	args := []interface{}{}
	argIdx := 1

	if enabledOnly {
		query += ` AND enabled = $` + string(rune('0'+argIdx))
		args = append(args, true)
		argIdx++
	}

	if service != "" {
		query += ` AND service_name = $` + string(rune('0'+argIdx))
		args = append(args, service)
		argIdx++
	}

	query += ` ORDER BY service_name, action_type`

	rows, err := h.DB.Query(r.Context(), query, args...)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "QUERY_ERROR", "Failed to query token rules", map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	var rules []TokenRule
	for rows.Next() {
		var rule TokenRule
		err := rows.Scan(
			&rule.ID,
			&rule.ServiceName,
			&rule.ActionType,
			&rule.CostPerUnit,
			&rule.Description,
			&rule.Enabled,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		)
		if err != nil {
			continue
		}
		rules = append(rules, rule)
	}

	if rules == nil {
		rules = []TokenRule{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"items": rules,
		"total": len(rules),
	})
}

// createTokenRule creates a new token consumption rule
// POST /api/v1/billing/tokens/rules
func (h *Handler) createTokenRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServiceName string `json:"serviceName"`
		ActionType  string `json:"actionType"`
		CostPerUnit int    `json:"costPerUnit"`
		Description string `json:"description"`
		Enabled     *bool  `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body", map[string]string{"error": err.Error()})
		return
	}

	// Validation
	if req.ServiceName == "" {
		errors.Write(w, r, http.StatusBadRequest, "MISSING_SERVICE_NAME", "serviceName is required", nil)
		return
	}
	if req.ActionType == "" {
		errors.Write(w, r, http.StatusBadRequest, "MISSING_ACTION_TYPE", "actionType is required", nil)
		return
	}
	if req.CostPerUnit <= 0 {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_COST", "costPerUnit must be positive", nil)
		return
	}

	// Normalize names
	req.ServiceName = strings.ToLower(req.ServiceName)
	req.ActionType = strings.ToLower(req.ActionType)

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	var rule TokenRule
	err := h.DB.QueryRow(r.Context(), `
		INSERT INTO token_consumption_rules (service_name, action_type, cost_per_unit, description, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, service_name, action_type, cost_per_unit, description, enabled, created_at, updated_at
	`, req.ServiceName, req.ActionType, req.CostPerUnit, req.Description, enabled).Scan(
		&rule.ID,
		&rule.ServiceName,
		&rule.ActionType,
		&rule.CostPerUnit,
		&rule.Description,
		&rule.Enabled,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)

	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			errors.Write(w, r, http.StatusConflict, "RULE_EXISTS", "Rule already exists for this service and action", map[string]string{"error": err.Error()})
			return
		}
		errors.Write(w, r, http.StatusInternalServerError, "INSERT_ERROR", "Failed to create token rule", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(rule)
}

// tokenRulesTree handles sub-routes for /api/v1/billing/tokens/rules/{id}
func (h *Handler) tokenRulesTree(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/billing/tokens/rules/")
	id := strings.Split(path, "/")[0]

	if id == "" {
		errors.Write(w, r, http.StatusBadRequest, "MISSING_ID", "Rule ID is required", nil)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getTokenRuleByID(w, r, id)
	case http.MethodPut:
		h.updateTokenRule(w, r, id)
	case http.MethodDelete:
		h.deleteTokenRule(w, r, id)
	default:
		errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
	}
}

// getTokenRuleByID returns a single token rule by ID
// GET /api/v1/billing/tokens/rules/{id}
func (h *Handler) getTokenRuleByID(w http.ResponseWriter, r *http.Request, id string) {
	var rule TokenRule
	err := h.DB.QueryRow(r.Context(), `
		SELECT id, service_name, action_type, cost_per_unit, description, enabled, created_at, updated_at
		FROM token_consumption_rules
		WHERE id = $1
	`, id).Scan(
		&rule.ID,
		&rule.ServiceName,
		&rule.ActionType,
		&rule.CostPerUnit,
		&rule.Description,
		&rule.Enabled,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			errors.Write(w, r, http.StatusNotFound, "NOT_FOUND", "Token rule not found", nil)
			return
		}
		errors.Write(w, r, http.StatusInternalServerError, "QUERY_ERROR", "Failed to query token rule", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rule)
}

// updateTokenRule updates an existing token rule
// PUT /api/v1/billing/tokens/rules/{id}
func (h *Handler) updateTokenRule(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		CostPerUnit *int    `json:"costPerUnit"`
		Description *string `json:"description"`
		Enabled     *bool   `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body", map[string]string{"error": err.Error()})
		return
	}

	// Build dynamic UPDATE query
	updates := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.CostPerUnit != nil {
		if *req.CostPerUnit <= 0 {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_COST", "costPerUnit must be positive", nil)
			return
		}
		updates = append(updates, "cost_per_unit = $"+string(rune('0'+argIdx)))
		args = append(args, *req.CostPerUnit)
		argIdx++
	}

	if req.Description != nil {
		updates = append(updates, "description = $"+string(rune('0'+argIdx)))
		args = append(args, *req.Description)
		argIdx++
	}

	if req.Enabled != nil {
		updates = append(updates, "enabled = $"+string(rune('0'+argIdx)))
		args = append(args, *req.Enabled)
		argIdx++
	}

	if len(updates) == 0 {
		errors.Write(w, r, http.StatusBadRequest, "NO_UPDATES", "No fields to update", nil)
		return
	}

	args = append(args, id)
	query := `
		UPDATE token_consumption_rules
		SET ` + strings.Join(updates, ", ") + `
		WHERE id = $` + string(rune('0'+argIdx)) + `
		RETURNING id, service_name, action_type, cost_per_unit, description, enabled, created_at, updated_at
	`

	var rule TokenRule
	err := h.DB.QueryRow(r.Context(), query, args...).Scan(
		&rule.ID,
		&rule.ServiceName,
		&rule.ActionType,
		&rule.CostPerUnit,
		&rule.Description,
		&rule.Enabled,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			errors.Write(w, r, http.StatusNotFound, "NOT_FOUND", "Token rule not found", nil)
			return
		}
		errors.Write(w, r, http.StatusInternalServerError, "UPDATE_ERROR", "Failed to update token rule", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rule)
}

// deleteTokenRule soft-deletes a token rule by setting enabled=false
// DELETE /api/v1/billing/tokens/rules/{id}
func (h *Handler) deleteTokenRule(w http.ResponseWriter, r *http.Request, id string) {
	// Soft delete: set enabled = false
	result, err := h.DB.Exec(r.Context(), `
		UPDATE token_consumption_rules
		SET enabled = FALSE
		WHERE id = $1
	`, id)

	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "DELETE_ERROR", "Failed to delete token rule", map[string]string{"error": err.Error()})
		return
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		errors.Write(w, r, http.StatusNotFound, "NOT_FOUND", "Token rule not found", nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
