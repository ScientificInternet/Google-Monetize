package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/ScientificInternet/Google-Monetize/pkg/errors"
	"github.com/ScientificInternet/Google-Monetize/pkg/middleware"
)

// AdminTopUpRequest represents the request body for admin token top-up
type AdminTopUpRequest struct {
	UserID string `json:"user_id"`
	Amount int64  `json:"amount"`
	Reason string `json:"reason"`
}

// AdminTopUpResponse represents the response for admin token top-up
type AdminTopUpResponse struct {
	Success    bool   `json:"success"`
	UserID     string `json:"user_id"`
	NewBalance int64  `json:"newBalance"`
	Amount     int64  `json:"amount"`
	Message    string `json:"message"`
}

// AdminTopUpTokens handles admin token top-up operations
// POST /api/v1/billing/tokens/topup
// Body: {"user_id": "user123", "amount": 1000, "reason": "Admin top-up"}
//
// TODO(architecture): 此API已从Console迁移至Billing服务
// 原因：Token充值直接修改核心计费数据，应由Billing服务完全掌控
// 迁移说明：合并了Console服务中的3处重复实现
func (h *Handler) AdminTopUpTokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req AdminTopUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body", map[string]string{"error": err.Error()})
		return
	}

	// Validation
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "user_id is required", nil)
		return
	}

	if req.Amount <= 0 {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "amount must be positive", nil)
		return
	}

	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "reason is required", nil)
		return
	}

	// Get admin user ID from context (for audit trail)
	adminUserID, _ := r.Context().Value(middleware.UserIDKey).(string)
	if adminUserID == "" {
		adminUserID = "unknown"
	}

	// Start transaction
	tx, err := h.DB.Begin(ctx)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "TRANSACTION_ERROR", "Failed to start transaction", map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback(ctx)

	// Ensure user_tokens row exists
	_, err = tx.Exec(ctx, `
		INSERT INTO "user_tokens" ("user_id", balance, "updated_at")
		VALUES ($1, 0, NOW())
		ON CONFLICT ("user_id") DO NOTHING
	`, req.UserID)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INSERT_ERROR", "Failed to ensure user token record", map[string]string{"error": err.Error()})
		return
	}

	// Update balance and return new balance
	var newBalance int64
	err = tx.QueryRow(ctx, `
		UPDATE "user_tokens"
		SET balance = balance + $1,
		    "updated_at" = NOW()
		WHERE "user_id" = $2
		RETURNING balance
	`, req.Amount, req.UserID).Scan(&newBalance)

	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "UPDATE_ERROR", "Failed to update token balance", map[string]string{"error": err.Error()})
		return
	}

	// Record transaction in token_transactions table
	auditReason := fmt.Sprintf("Admin top-up: %s (by %s)", req.Reason, adminUserID)
	_, err = tx.Exec(ctx, `
		INSERT INTO "token_transactions" ("user_id", type, amount, description, "created_at")
		VALUES ($1, 'admin_topup', $2, $3, NOW())
	`, req.UserID, req.Amount, auditReason)

	if err != nil {
		// Log but don't fail the transaction if transaction logging fails
		log.Printf("[billing] failed to record token transaction: %v", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "COMMIT_ERROR", "Failed to commit transaction", map[string]string{"error": err.Error()})
		return
	}

	// Return success response
	response := AdminTopUpResponse{
		Success:    true,
		UserID:     req.UserID,
		NewBalance: newBalance,
		Amount:     req.Amount,
		Message:    fmt.Sprintf("Successfully added %d tokens", req.Amount),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
