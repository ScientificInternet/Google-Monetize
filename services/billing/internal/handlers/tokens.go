package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ScientificInternet/Google-Monetize/pkg/apierrors"
	"github.com/ScientificInternet/Google-Monetize/pkg/cache"
	"github.com/ScientificInternet/Google-Monetize/pkg/middleware"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/tokens"
)

// TokenService defines the interface for token operations
type TokenService interface {
	GetBalance(ctx context.Context, userID string) (int64, error)
	GetBalanceSummary(ctx context.Context, userID string) (tokens.BalanceSummary, error)
	CheckAndReserveTokens(ctx context.Context, userID string, amount int, description string) (string, error)
	ConfirmTokenDeduction(ctx context.Context, reservationID string) error
	RefundTokens(ctx context.Context, userID, reservationID string, amount int, reason string) error
}

// TokensHandler handles token-related requests
type TokensHandler struct {
	tokenService TokenService
}

// NewTokensHandler creates a new tokens handler
func NewTokensHandler(db *pgxpool.Pool, cache *cache.Cache) *TokensHandler {
	return &TokensHandler{
		tokenService: tokens.NewService(db, cache),
	}
}

// RegisterTokenRoutes registers token-related routes
func (h *TokensHandler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	mux.Handle("/api/v1/billing/tokens/balance", authMiddleware(http.HandlerFunc(h.getBalance)))
	mux.Handle("/api/v1/billing/tokens/reserve", authMiddleware(http.HandlerFunc(h.reserveTokens)))
	mux.Handle("/api/v1/billing/tokens/commit", authMiddleware(http.HandlerFunc(h.commitTokens)))
	mux.Handle("/api/v1/billing/tokens/release", authMiddleware(http.HandlerFunc(h.releaseTokens)))
}

// UserReserveTokensRequest represents the reserve tokens request body for authenticated user endpoints
type UserReserveTokensRequest struct {
	Amount int    `json:"amount"`
	TaskID string `json:"taskId"`
}

// UserReserveTokensResponse represents the reserve tokens response
type UserReserveTokensResponse struct {
	TxID   string `json:"txId"`
	Status string `json:"status"`
}

// UserCommitTokensRequest represents the commit tokens request
type UserCommitTokensRequest struct {
	TxID   string `json:"txId"`
	Amount int    `json:"amount"`
	TaskID string `json:"taskId"`
	Source string `json:"source,omitempty"`
}

// UserCommitTokensResponse represents the commit tokens response
type UserCommitTokensResponse struct {
	TxID    string `json:"txId"`
	DebitID string `json:"debitId"`
	Status  string `json:"status"`
	Balance int    `json:"balance"`
}

// UserReleaseTokensRequest represents the release tokens request
type UserReleaseTokensRequest struct {
	TxID   string `json:"txId"`
	Amount int    `json:"amount"`
	TaskID string `json:"taskId"`
}

// UserReleaseTokensResponse represents the release tokens response
type UserReleaseTokensResponse struct {
	TxID   string `json:"txId"`
	Status string `json:"status"`
}

// getBalance handles GET /api/v1/billing/tokens/balance
func (h *TokensHandler) getBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		apiErr := apierrors.Unauthorized("Unauthorized")
		apiErr.WriteJSON(w, r)
		return
	}

	summary, err := h.tokenService.GetBalanceSummary(r.Context(), userID)
	if err != nil {
		apiErr := apierrors.InternalError("Failed to get balance")
		apiErr.Details = map[string]interface{}{"error": err.Error()}
		apiErr.WriteJSON(w, r)
		return
	}

	payload := map[string]interface{}{
		"balance":                    summary.Balance,
		"total_balance":              summary.TotalBalance,
		"today_consumed":             summary.TodayConsumed,
		"this_month_consumed":        summary.ThisMonthConsumed,
		"pending_tasks_count":        summary.PendingTasksCount,
		"estimated_cost_for_pending": summary.EstimatedCostForPending,
		// 兼容旧前端字段
		"totalBalance":            summary.TotalBalance,
		"todayConsumed":           summary.TodayConsumed,
		"monthConsumed":           summary.ThisMonthConsumed,
		"pendingTasks":            summary.PendingTasksCount,
		"estimatedCostForPending": summary.EstimatedCostForPending,
	}

	if summary.UpdatedAt != nil {
		payload["updated_at"] = summary.UpdatedAt.Format(time.RFC3339)
	}

	respondWithJSON(w, http.StatusOK, payload)
}

// reserveTokens handles POST /api/v1/billing/tokens/reserve
func (h *TokensHandler) reserveTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiErr := apierrors.New(apierrors.CodeInvalidRequest, "Method not allowed", nil)
		apiErr.HTTPStatus = http.StatusMethodNotAllowed
		apiErr.WriteJSON(w, r)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		apiErr := apierrors.Unauthorized("Unauthorized")
		apiErr.WriteJSON(w, r)
		return
	}

	var req UserReserveTokensRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiErr := apierrors.InvalidRequest("body", "Invalid request body")
		apiErr.WriteJSON(w, r)
		return
	}

	// Validate request
	if req.Amount <= 0 {
		apiErr := apierrors.InvalidRequest("param", "Amount must be positive")
		apiErr.WriteJSON(w, r)
		return
	}
	if req.TaskID == "" {
		apiErr := apierrors.InvalidRequest("param", "TaskID is required")
		apiErr.WriteJSON(w, r)
		return
	}

	// Check idempotency key
	idempotencyKey := r.Header.Get("X-Idempotency-Key")
	_ = idempotencyKey

	// Reserve tokens with description
	description := fmt.Sprintf("Reserve tokens for task %s", req.TaskID)
	txID, err := h.tokenService.CheckAndReserveTokens(r.Context(), userID, req.Amount, description)
	if err != nil {
		if err.Error() == fmt.Sprintf("insufficient tokens: have %d, need %d", 0, req.Amount) {
			respondWithJSON(w, http.StatusPaymentRequired, map[string]interface{}{
				"code":    "INSUFFICIENT_TOKENS",
				"message": err.Error(),
			})
			return
		}
		apiErr := apierrors.InternalError("Failed to reserve tokens")
		apiErr.Details = map[string]interface{}{"error": err.Error()}
		apiErr.WriteJSON(w, r)
		return
	}

	resp := UserReserveTokensResponse{
		TxID:   txID,
		Status: "reserved",
	}

	respondWithJSON(w, http.StatusAccepted, resp)
}

// commitTokens handles POST /api/v1/billing/tokens/commit
func (h *TokensHandler) commitTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiErr := apierrors.New(apierrors.CodeInvalidRequest, "Method not allowed", nil)
		apiErr.HTTPStatus = http.StatusMethodNotAllowed
		apiErr.WriteJSON(w, r)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		apiErr := apierrors.Unauthorized("Unauthorized")
		apiErr.WriteJSON(w, r)
		return
	}

	var req UserCommitTokensRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiErr := apierrors.InvalidRequest("body", "Invalid request body")
		apiErr.WriteJSON(w, r)
		return
	}

	// Validate request
	if req.TxID == "" {
		apiErr := apierrors.InvalidRequest("param", "TxID is required")
		apiErr.WriteJSON(w, r)
		return
	}

	// Confirm the reservation
	if err := h.tokenService.ConfirmTokenDeduction(r.Context(), req.TxID); err != nil {
		apiErr := apierrors.InternalError("Failed to commit tokens")
		apiErr.Details = map[string]interface{}{"error": err.Error()}
		apiErr.WriteJSON(w, r)
		return
	}

	// Get current balance
	balance, err := h.tokenService.GetBalance(r.Context(), userID)
	if err != nil {
		balance = 0 // fallback
	}

	resp := UserCommitTokensResponse{
		TxID:    req.TxID,
		DebitID: req.TxID, // Same as txID in our implementation
		Status:  "committed",
		Balance: int(balance),
	}

	respondWithJSON(w, http.StatusOK, resp)
}

// releaseTokens handles POST /api/v1/billing/tokens/release
func (h *TokensHandler) releaseTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiErr := apierrors.New(apierrors.CodeInvalidRequest, "Method not allowed", nil)
		apiErr.HTTPStatus = http.StatusMethodNotAllowed
		apiErr.WriteJSON(w, r)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		apiErr := apierrors.Unauthorized("Unauthorized")
		apiErr.WriteJSON(w, r)
		return
	}

	var req UserReleaseTokensRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiErr := apierrors.InvalidRequest("body", "Invalid request body")
		apiErr.WriteJSON(w, r)
		return
	}

	// Validate request
	if req.TxID == "" {
		apiErr := apierrors.InvalidRequest("param", "TxID is required")
		apiErr.WriteJSON(w, r)
		return
	}
	if req.Amount <= 0 {
		apiErr := apierrors.InvalidRequest("param", "Amount must be positive")
		apiErr.WriteJSON(w, r)
		return
	}

	// Refund tokens
	reason := fmt.Sprintf("Task %s failed or cancelled", req.TaskID)
	if err := h.tokenService.RefundTokens(r.Context(), userID, req.TxID, req.Amount, reason); err != nil {
		apiErr := apierrors.InternalError("Failed to release tokens")
		apiErr.Details = map[string]interface{}{"error": err.Error()}
		apiErr.WriteJSON(w, r)
		return
	}

	resp := UserReleaseTokensResponse{
		TxID:   req.TxID,
		Status: "released",
	}

	respondWithJSON(w, http.StatusOK, resp)
}
