package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ScientificInternet/Google-Monetize/pkg/errors"
)

// AdminUpdatesubscriptionsRequest represents the request body for admin subscription update
type AdminUpdatesubscriptionsRequest struct {
	PlanName string `json:"plan_name"`
	Status   string `json:"status"`
}

// AdminUpdatesubscriptionsResponse represents the response for admin subscription update
type AdminUpdatesubscriptionsResponse struct {
	Success  bool   `json:"success"`
	UserID   string `json:"user_id"`
	PlanName string `json:"plan_name"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

// AdminUpdatesubscriptions handles admin subscription management operations
// PUT /api/v1/billing/subscriptions/{user_id}
// Body: {"plan_name": "pro", "status": "active"}
//
// TODO(architecture): 此API已从Console迁移至Billing服务
// 原因：订阅状态管理直接修改核心计费数据，应由Billing服务完全掌控
func (h *Handler) AdminUpdatesubscriptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user_id from path
	userID := r.PathValue("user_id")
	if userID == "" {
		// Fallback for older path parsing
		path := r.URL.Path
		const prefix = "/api/v1/billing/subscriptions/"
		if len(path) > len(prefix) {
			userID = strings.TrimPrefix(path, prefix)
			userID = strings.Split(userID, "/")[0]
		}
	}

	userID = strings.TrimSpace(userID)
	if userID == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "user_id is required", nil)
		return
	}

	var req AdminUpdatesubscriptionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body", map[string]string{"error": err.Error()})
		return
	}

	// Validate and normalize plan_name
	req.PlanName = strings.ToLower(strings.TrimSpace(req.PlanName))
	if req.PlanName == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "plan_name is required", nil)
		return
	}

	validPlans := map[string]bool{
		"free": true,
		"pro":  true,
		"max":  true,
	}
	if !validPlans[req.PlanName] {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "plan_name must be one of: free, pro, max", nil)
		return
	}

	// Validate and normalize status
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.Status == "" {
		req.Status = "active" // Default to active
	}

	validStatuses := map[string]bool{
		"active":   true,
		"inactive": true,
		"canceled": true,
		"trialing": true,
	}
	if !validStatuses[req.Status] {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "status must be one of: active, inactive, canceled, trialing", nil)
		return
	}

	// Upsert subscription
	// If status is 'active', extend currentPeriodEnd by 30 days from now
	_, err := h.DB.Exec(ctx, `
		INSERT INTO "subscriptions" (id, "user_id", "plan_name", status, "currentPeriodEnd")
		VALUES ('sub_' || $1::text, $1, $2, $3, NOW() + interval '30 days')
		ON CONFLICT ("user_id") DO UPDATE
		SET "plan_name" = EXCLUDED."plan_name",
		    status = EXCLUDED.status,
		    "currentPeriodEnd" = CASE
		        WHEN EXCLUDED.status = 'active' THEN NOW() + interval '30 days'
		        ELSE "subscriptions"."currentPeriodEnd"
		    END
	`, userID, req.PlanName, req.Status)

	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "UPDATE_ERROR", "Failed to update subscription", map[string]string{"error": err.Error()})
		return
	}

	// Return success response
	response := AdminUpdatesubscriptionsResponse{
		Success:  true,
		UserID:   userID,
		PlanName: req.PlanName,
		Status:   req.Status,
		Message:  fmt.Sprintf("Successfully updated subscription to %s (%s)", req.PlanName, req.Status),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
