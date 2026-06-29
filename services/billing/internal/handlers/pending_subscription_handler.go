package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/pkg/errors"
	ev "github.com/ScientificInternet/Google-Monetize/pkg/events"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/domain"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/notifications"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/tokens"
)

// PendingsubscriptionsHandler handles pending subscription operations
type PendingsubscriptionsHandler struct {
	adapter            database.DatabaseAdapter
	pub                *ev.Publisher
	tokenService       *tokens.Service
	notificationClient *notifications.NotificationClient
}

// NewPendingsubscriptionsHandler creates a new pending subscription handler
func NewPendingsubscriptionsHandler(adapter database.DatabaseAdapter, pub *ev.Publisher, tokenService *tokens.Service, notificationClient *notifications.NotificationClient) *PendingsubscriptionsHandler {
	return &PendingsubscriptionsHandler{
		adapter:            adapter,
		pub:                pub,
		tokenService:       tokenService,
		notificationClient: notificationClient,
	}
}

// ListPendingsubscriptionssResponse represents the response for listing pending subscriptions
type ListPendingsubscriptionssResponse struct {
	Items []PendingsubscriptionsItem `json:"items"`
	Total int                       `json:"total"`
}

// PendingsubscriptionsItem represents a pending subscription
type PendingsubscriptionsItem struct {
	ID                     string     `json:"id"`
	PlanID                 string     `json:"planId"`
	PlanTier               int        `json:"planTier"`
	Status                 string     `json:"status"`
	Source                 string     `json:"source"`
	BlockingsubscriptionsID *string    `json:"blockingsubscriptionsId,omitempty"`
	TokenQuota             int        `json:"tokenQuota"`
	CreatedAt              time.Time  `json:"created_at"`
	ExpiresAt              time.Time  `json:"expiresAt"`
	ActivatedAt            *time.Time `json:"activatedAt,omitempty"`
}

// ListPendingsubscriptionss handles GET /api/v1/billing/subscriptions/pending
func (h *PendingsubscriptionsHandler) ListPendingsubscriptionss(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := chi.URLParam(r, "user_id")

	if userID == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "user_id is required", nil)
		return
	}

	resp, err := h.listPendingsubscriptionss(ctx, userID)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to list pending subscriptions", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// CancelPendingsubscriptions handles DELETE /api/v1/billing/subscriptions/pending/{id}
func (h *PendingsubscriptionsHandler) CancelPendingsubscriptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pendingID := chi.URLParam(r, "id")

	if pendingID == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "pending subscription id is required", nil)
		return
	}

	err := h.cancelPendingsubscriptions(ctx, pendingID)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to cancel pending subscription", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "canceled"})
}

// CreatePendingsubscriptions creates a new pending subscription when tier conflict occurs
func (h *PendingsubscriptionsHandler) CreatePendingsubscriptions(
	ctx context.Context,
	userID string,
	planID string,
	planTier int,
	source string,
	blockingsubscriptionsID string,
	tokenQuota int,
) (*domain.Pendingsubscriptions, error) {
	pendingID := uuid.New().String()
	pending := domain.NewPendingsubscriptions(
		pendingID,
		userID,
		planID,
		planTier,
		source,
		&blockingsubscriptionsID,
		tokenQuota,
	)

	query := `
		INSERT INTO "Pendingsubscriptions" (
			id, user_id, plan_id, plan_tier, status, source,
			blocking_subscription_id, token_quota,
			created_at, expires_at, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err := h.db.Exec(ctx, query,
		pending.ID,
		pending.UserID,
		pending.PlanID,
		pending.PlanTier,
		pending.Status,
		pending.Source,
		pending.BlockingsubscriptionsID,
		pending.TokenQuota,
		pending.CreatedAt,
		pending.ExpiresAt,
		"{}",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert pending subscription: %w", err)
	}

	// Publish event
	if h.pub != nil {
		event := map[string]interface{}{
			"eventId":    uuid.New().String(),
			"eventType":  "PendingsubscriptionsCreated",
			"occurredAt": time.Now().Format(time.RFC3339),
			"user_id":     userID,
			"data": map[string]interface{}{
				"pendingsubscriptionsId":  pending.ID,
				"planId":                 planID,
				"planTier":               planTier,
				"source":                 source,
				"blockingsubscriptionsId": blockingsubscriptionsID,
				"tokenQuota":             tokenQuota,
				"expiresAt":              pending.ExpiresAt.Format(time.RFC3339),
			},
		}

		if err := h.pub.Publish(ctx, "subscription.pending.created", event); err != nil {
			fmt.Printf("Warning: Failed to publish PendingsubscriptionsCreated event: %v\n", err)
		}
	}

	return pending, nil
}

// ActivatePendingsubscriptions activates a pending subscription
// This is called by the scheduler when the blocking subscription expires
func (h *PendingsubscriptionsHandler) ActivatePendingsubscriptions(ctx context.Context, pendingID string) error {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Get pending subscription details
	var pending domain.Pendingsubscriptions
	query := `
		SELECT id, user_id, plan_id, plan_tier, status, source,
		       blocking_subscription_id, token_quota, created_at, expires_at
		FROM "Pendingsubscriptions"
		WHERE id = $1 AND status = 'pending'
		FOR UPDATE
	`

	err = tx.QueryRow(ctx, query, pendingID).Scan(
		&pending.ID,
		&pending.UserID,
		&pending.PlanID,
		&pending.PlanTier,
		&pending.Status,
		&pending.Source,
		&pending.BlockingsubscriptionsID,
		&pending.TokenQuota,
		&pending.CreatedAt,
		&pending.ExpiresAt,
	)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("pending subscription not found or already processed")
	}
	if err != nil {
		return fmt.Errorf("failed to query pending subscription: %w", err)
	}

	// 2. Create actual subscription
	subscriptionID := uuid.New().String()
	now := time.Now()
	trialDays := domain.TrialDaysReferral // Default to 14 days
	trialEndDate := now.AddDate(0, 0, trialDays)
	tierValue := pending.PlanTier

	insertSubQuery := `
		INSERT INTO "subscriptions" (
			id, "user_id", "planId", "plan_name", status, tier,
			"trialStartDate", "trialEndDate", "trialSource",
			"currentPeriodEnd", "created_at", "updated_at"
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
	`

	_, err = tx.Exec(ctx, insertSubQuery,
		subscriptionID,
		pending.UserID,
		pending.PlanID,
		"Professional", // TODO: Get from plan config
		domain.StatusTrialing,
		tierValue,
		now,
		trialEndDate,
		pending.Source,
		trialEndDate,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// 3. Grant tokens with expiry
	if h.tokenService != nil {
		err = h.tokenService.GrantTokensWithSource(
			ctx,
			pending.UserID,
			pending.TokenQuota,
			tokens.TokenSourcesubscriptions,
			&trialEndDate, // Tokens expire when subscription expires
			&subscriptionID,
			fmt.Sprintf("Pending subscription activated: %s", pending.Source),
		)
		if err != nil {
			fmt.Printf("Warning: Failed to grant tokens for activated subscription: %v\n", err)
		}
	}

	// 4. Mark pending subscription as activated
	updateQuery := `
		UPDATE "Pendingsubscriptions"
		SET status = 'activated',
		    activated_at = NOW()
		WHERE id = $1
	`

	_, err = tx.Exec(ctx, updateQuery, pendingID)
	if err != nil {
		return fmt.Errorf("failed to update pending subscription status: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 5. Publish activation event
	if h.pub != nil {
		event := map[string]interface{}{
			"eventId":    uuid.New().String(),
			"eventType":  "PendingsubscriptionsActivated",
			"occurredAt": time.Now().Format(time.RFC3339),
			"user_id":     pending.UserID,
			"data": map[string]interface{}{
				"pendingsubscriptionsId": pending.ID,
				"subscriptionId":        subscriptionID,
				"planId":                pending.PlanID,
				"tokensGranted":         pending.TokenQuota,
			},
		}

		if err := h.pub.Publish(ctx, "subscription.pending.activated", event); err != nil {
			fmt.Printf("Warning: Failed to publish PendingsubscriptionsActivated event: %v\n", err)
		}
	}

	// 6. Send activation notification
	if h.notificationClient != nil {
		if err := h.notificationClient.SendPendingActivatedNotification(ctx, pending.UserID, pending.PlanID, pending.TokenQuota); err != nil {
			fmt.Printf("Warning: Failed to send pending activated notification: %v\n", err)
		}
	}

	return nil
}

// listPendingsubscriptionss returns user's pending subscriptions
func (h *PendingsubscriptionsHandler) listPendingsubscriptionss(ctx context.Context, userID string) (*ListPendingsubscriptionssResponse, error) {
	query := `
		SELECT id, plan_id, plan_tier, status, source,
		       blocking_subscription_id, token_quota,
		       created_at, expires_at, activated_at
		FROM "Pendingsubscriptions"
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := h.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending subscriptions: %w", err)
	}
	defer rows.Close()

	var items []PendingsubscriptionsItem
	for rows.Next() {
		var item PendingsubscriptionsItem
		err := rows.Scan(
			&item.ID,
			&item.PlanID,
			&item.PlanTier,
			&item.Status,
			&item.Source,
			&item.BlockingsubscriptionsID,
			&item.TokenQuota,
			&item.CreatedAt,
			&item.ExpiresAt,
			&item.ActivatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan pending subscription: %w", err)
		}
		items = append(items, item)
	}

	return &ListPendingsubscriptionssResponse{
		Items: items,
		Total: len(items),
	}, nil
}

// cancelPendingsubscriptions cancels a pending subscription
func (h *PendingsubscriptionsHandler) cancelPendingsubscriptions(ctx context.Context, pendingID string) error {
	query := `
		UPDATE "Pendingsubscriptions"
		SET status = 'canceled',
		    canceled_at = NOW()
		WHERE id = $1 AND status = 'pending'
	`

	result, err := h.db.Exec(ctx, query, pendingID)
	if err != nil {
		return fmt.Errorf("failed to cancel pending subscription: %w", err)
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("pending subscription not found or already processed")
	}

	return nil
}
