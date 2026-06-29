package handlers

import (
	"github.com/ScientificInternet/Google-Monetize/pkg/apierrors"

	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	apperrors "github.com/ScientificInternet/Google-Monetize/pkg/errors"
	"github.com/ScientificInternet/Google-Monetize/pkg/middleware"
	"github.com/ScientificInternet/Google-Monetize/pkg/telemetry"
)

const telemetryServiceName = "billing"

// Handler wires billing HTTP endpoints to the backing database layer.
type Handler struct {
	DB *pgxpool.Pool
}

// NewHandler returns a Handler with required dependencies.
func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{DB: db}
}

// RegisterRoutes attaches all HTTP handlers to the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	telemetry.RegisterDefaultMetrics(telemetryServiceName)

	mux.Handle("/health", telemetry.Middleware(telemetryServiceName, http.HandlerFunc(h.healthz)))
	mux.Handle("/healthz", telemetry.Middleware(telemetryServiceName, http.HandlerFunc(h.healthz)))
	mux.Handle("/readyz", telemetry.Middleware(telemetryServiceName, http.HandlerFunc(h.readyz)))

	// Authenticated user endpoints.
	mux.Handle(
		"/api/v1/billing/subscriptions/me",
		authMiddleware(withTelemetry(http.HandlerFunc(h.getsubscriptions))),
	)
	mux.Handle(
		"/api/v1/billing/tokens/me",
		authMiddleware(withTelemetry(http.HandlerFunc(h.getTokenBalance))),
	)
	mux.Handle(
		"/api/v1/billing/tokens/transactions",
		authMiddleware(withTelemetry(http.HandlerFunc(h.gettoken_transactionss))),
	)

	// Token reservation endpoints (service-to-service).
	mux.Handle("/api/v1/users/{user_id}/tokens/reserve", withTelemetry(http.HandlerFunc(h.ReserveTokens)))
	mux.Handle("/api/v1/users/{user_id}/tokens/consume", withTelemetry(http.HandlerFunc(h.ConsumeTokensDirect)))
	mux.Handle("/api/v1/users/{user_id}/tokens/balance", withTelemetry(http.HandlerFunc(h.Getuser_tokensBalance)))
	mux.Handle("/api/v1/tokens/reservations/{reservationId}/consume", withTelemetry(http.HandlerFunc(h.ConsumeReservation)))
	mux.Handle("/api/v1/tokens/reservations/{reservationId}/release", withTelemetry(http.HandlerFunc(h.ReleaseReservation)))

	// Token rules management (admin only) - 核心计费规则管理
	mux.Handle(
		"/api/v1/billing/tokens/rules",
		authMiddleware(middleware.AdminOnly(withTelemetry(http.HandlerFunc(h.tokenRulesHandler)))),
	)
	mux.Handle(
		"/api/v1/billing/tokens/rules/",
		authMiddleware(middleware.AdminOnly(withTelemetry(http.HandlerFunc(h.tokenRulesTree)))),
	)

	// Admin token operations (admin only) - 管理员Token操作
	mux.Handle(
		"/api/v1/billing/tokens/topup",
		authMiddleware(middleware.AdminOnly(withTelemetry(http.HandlerFunc(h.AdminTopUpTokens)))),
	)

	// Admin subscription operations (admin only) - 管理员订阅操作
	mux.Handle(
		"/api/v1/billing/subscriptions/{user_id}",
		authMiddleware(middleware.AdminOnly(withTelemetry(http.HandlerFunc(h.AdminUpdatesubscriptions)))),
	)
}

func withTelemetry(handler http.Handler) http.Handler {
	return telemetry.Middleware(telemetryServiceName, handler)
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if h.DB != nil {
		if err := h.DB.Ping(ctx); err != nil {
			respondWithJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "error": err.Error()})
			return
		}
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// subscriptions is a trimmed view returned to the frontend.
type subscriptions struct {
	ID               string    `json:"id"`
	PlanName         string    `json:"plan_name"`
	Status           string    `json:"status"`
	CurrentPeriodEnd time.Time `json:"currentPeriodEnd"`
}

// TokenBalance mirrors a subset of the user_tokens table.
type TokenBalance struct {
	Balance   int64     `json:"balance"`
	UpdatedAt time.Time `json:"updated_at"`
}

// token_transactions models the latest token movements for UI consumption.
type token_transactions struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Amount      int       `json:"amount"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

func (h *Handler) getsubscriptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok || strings.TrimSpace(userID) == "" {
		apiErr := apierrors.Unauthorized("Unauthorized")
		apiErr.WriteJSON(w, r)
		return
	}

	var sub subscriptions
	err := h.DB.QueryRow(r.Context(), `
		SELECT id, "plan_name", status, "currentPeriodEnd"
		FROM "subscriptions"
		WHERE "user_id" = $1
	`, userID).Scan(&sub.ID, &sub.PlanName, &sub.Status, &sub.CurrentPeriodEnd)
	if err != nil {
		apiErr := apierrors.NotFound("Not found", "")
		apiErr.WriteJSON(w, r)
		return
	}

	// Convert backend format to frontend-expected format
	response := map[string]interface{}{
		"tier":     sub.PlanName,           // Map plan_name -> tier
		"isActive": sub.Status == "active", // Map status -> isActive

		// Additional fields for frontend compatibility
		"isElite":                sub.PlanName == "elite" || sub.PlanName == "Enterprise",
		"canUseAI":               sub.PlanName != "trial" && sub.PlanName != "starter",
		"monthlyTokenAllocation": h.getMonthlyTokenAllocation(sub.PlanName),
		"currentTokenBalance":    0, // Will be updated by separate token balance call
		"subscriptionEndDate":    sub.CurrentPeriodEnd.Format("2006-01-02T15:04:05Z"),
		"trialEndDate":           nil,
		"isOnTrial":              sub.Status == "trial",
		"daysRemaining":          h.calculateDaysRemaining(sub.CurrentPeriodEnd),
	}

	respondWithJSON(w, http.StatusOK, response)
}

func (h *Handler) getTokenBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok || strings.TrimSpace(userID) == "" {
		apiErr := apierrors.Unauthorized("Unauthorized")
		apiErr.WriteJSON(w, r)
		return
	}

	var balance TokenBalance
	err := h.DB.QueryRow(r.Context(), `
		SELECT balance, "updated_at"
		FROM "user_tokens"
		WHERE "user_id" = $1
	`, userID).Scan(&balance.Balance, &balance.UpdatedAt)
	if err != nil {
		apiErr := apierrors.NotFound("Not found", "")
		apiErr.WriteJSON(w, r)
		return
	}

	// Convert backend format to frontend-expected format
	response := map[string]interface{}{
		"currentBalance": balance.Balance,                        // Map balance -> currentBalance
		"totalConsumed":  0,                                      // Will be calculated if needed
		"totalGranted":   h.getMonthlyTokenAllocation("starter"), // Default allocation
		"lastUpdated":    balance.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}

	respondWithJSON(w, http.StatusOK, response)
}

func (h *Handler) gettoken_transactionss(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok || strings.TrimSpace(userID) == "" {
		apiErr := apierrors.Unauthorized("Unauthorized")
		apiErr.WriteJSON(w, r)
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT id, type, amount, description, "created_at"
		FROM "token_transactions"
		WHERE "user_id" = $1
		ORDER BY "created_at" DESC
		LIMIT 50
	`, userID)
	if err != nil {
		log.Printf("query token transactions: %v", err)
		apiErr := apierrors.InternalError("Internal server error")
		apiErr.WriteJSON(w, r)
		return
	}
	defer rows.Close()

	var transactions []token_transactions
	for rows.Next() {
		var t token_transactions
		if scanErr := rows.Scan(&t.ID, &t.Type, &t.Amount, &t.Description, &t.CreatedAt); scanErr != nil {
			log.Printf("scan token transaction: %v", scanErr)
			apiErr := apierrors.InternalError("Internal server error")
			apiErr.WriteJSON(w, r)
			return
		}
		transactions = append(transactions, t)
	}

	respondWithJSON(w, http.StatusOK, transactions)
}

// Getuser_tokensBalance handles GET /api/v1/users/{user_id}/tokens/balance.
func (h *Handler) Getuser_tokensBalance(w http.ResponseWriter, r *http.Request) {
	userID := parseUserIDFromPath(r)
	if userID == "" {
		apperrors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "user_id is required", nil)
		return
	}

	balance, err := h.getUserBalance(r.Context(), userID)
	if err != nil {
		apperrors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "failed to get balance", map[string]string{"error": err.Error()})
		return
	}

	respondWithJSON(w, http.StatusOK, balance)
}

type ErrorResponse struct {
	Error   string                 `json:"error"`
	Details map[string]interface{} `json:"details,omitempty"`
}

func respondWithJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	respondWithJSON(w, status, payload)
}

func parseUserIDFromPath(r *http.Request) string {
	if val := r.PathValue("user_id"); val != "" {
		return val
	}
	// Fallback for older clients hitting deprecated patterns.
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i, seg := range segments {
		if seg == "users" && i+1 < len(segments) {
			return segments[i+1]
		}
	}
	return ""
}

// Helper function to get monthly token allocation based on plan
func (h *Handler) getMonthlyTokenAllocation(plan_name string) int {
	switch plan_name {
	case "trial", "starter":
		return 100
	case "professional", "pro":
		return 500
	case "elite", "enterprise":
		return 2000
	default:
		return 100
	}
}

// Helper function to calculate days remaining
func (h *Handler) calculateDaysRemaining(endDate time.Time) int {
	if endDate.IsZero() {
		return 0
	}
	duration := time.Until(endDate)
	if duration < 0 {
		return 0
	}
	return int(duration.Hours() / 24)
}
