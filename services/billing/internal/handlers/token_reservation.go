// Package handlers - Token reservation endpoints for service integration
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// TokenReservation represents a reserved token transaction
type TokenReservation struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Amount    int       `json:"amount"`
	Reason    string    `json:"reason"`
	Status    string    `json:"status"` // pending, consumed, released, expired
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"created_at"`
}

// ReserveTokensRequest for reserving tokens
type ReserveTokensRequest struct {
	Amount  int    `json:"amount"`
	Service string `json:"service"`
	Action  string `json:"action"`
	Reason  string `json:"reason"`
}

// ReserveTokens handles POST /api/v1/users/{user_id}/tokens/reserve
func (h *Handler) ReserveTokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := r.PathValue("user_id")

	if userID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "user_id is required"})
		return
	}

	var req ReserveTokensRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid request body"})
		return
	}

	if req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Amount must be positive"})
		return
	}

	// Check user balance
	balance, err := h.getUserBalance(ctx, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to check balance"})
		return
	}

	if balance.Available < req.Amount {
		writeJSON(w, http.StatusPaymentRequired, ErrorResponse{
			Error: "Insufficient tokens",
			Details: map[string]interface{}{
				"required":  req.Amount,
				"available": balance.Available,
			},
		})
		return
	}

	// Create reservation
	reservationID := uuid.New().String()
	expiresAt := time.Now().Add(5 * time.Minute) // 5 minute expiry

	// Insert reservation record
	query := `
		INSERT INTO "token_transactions" (
			id, "user_id", type, amount, balance, description,
			service, "actionType", metadata, "created_at"
		) VALUES ($1, $2, 'reserve', $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, "created_at"
	`

	metadata := map[string]interface{}{
		"reason":    req.Reason,
		"expiresAt": expiresAt,
		"status":    "pending",
		"service":   req.Service,
		"action":    req.Action,
	}
	metadataJSON, _ := json.Marshal(metadata)

	var createdID string
	var created_at time.Time
	err = h.DB.QueryRow(ctx, query,
		reservationID,
		userID,
		-req.Amount, // Negative for reserve
		balance.Total-req.Amount,
		fmt.Sprintf("Token reservation: %s.%s - %s", req.Service, req.Action, req.Reason),
		req.Service,
		req.Action,
		metadataJSON,
		time.Now(),
	).Scan(&createdID, &created_at)

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to create reservation"})
		return
	}

	// Return reservation
	reservation := map[string]interface{}{
		"reservationId": reservationID,
		"user_id":        userID,
		"amount":        req.Amount,
		"expiresAt":     expiresAt,
		"created_at":     created_at,
	}

	writeJSON(w, http.StatusCreated, reservation)
}

// ConsumeReservation handles POST /api/v1/tokens/reservations/{reservationId}/consume
func (h *Handler) ConsumeReservation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	reservationID := r.PathValue("reservationId")

	if reservationID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "reservationId is required"})
		return
	}

	// Find reservation
	reservation, err := h.getReservation(ctx, reservationID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "Reservation not found"})
		return
	}

	// Check if expired
	if time.Now().After(reservation.ExpiresAt) {
		writeJSON(w, http.StatusGone, ErrorResponse{Error: "Reservation expired"})
		return
	}

	// Check if already consumed or released
	if reservation.Status != "pending" {
		writeJSON(w, http.StatusConflict, ErrorResponse{
			Error:   "Reservation already processed",
			Details: map[string]interface{}{"status": reservation.Status},
		})
		return
	}

	// Update reservation to consumed
	query := `
		UPDATE "token_transactions"
		SET metadata = jsonb_set(metadata, '{status}', '"consumed"'::jsonb),
		    description = description || ' (consumed)'
		WHERE id = $1
		RETURNING "user_id", amount, balance, "created_at"
	`

	var userID string
	var amount int
	var balance int
	var created_at time.Time

	err = h.DB.QueryRow(ctx, query, reservationID).Scan(&userID, &amount, &balance, &created_at)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to consume reservation"})
		return
	}

	// Return result
	result := map[string]interface{}{
		"transactionId": reservationID,
		"user_id":        userID,
		"amount":        -amount, // Convert back to positive
		"balance":       balance,
		"consumedAt":    time.Now(),
	}

	writeJSON(w, http.StatusOK, result)
}

// ReleaseReservation handles POST /api/v1/tokens/reservations/{reservationId}/release
func (h *Handler) ReleaseReservation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	reservationID := r.PathValue("reservationId")

	if reservationID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "reservationId is required"})
		return
	}

	// Find reservation
	reservation, err := h.getReservation(ctx, reservationID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "Reservation not found"})
		return
	}

	// Check if already consumed or released
	if reservation.Status != "pending" {
		writeJSON(w, http.StatusConflict, ErrorResponse{
			Error:   "Reservation already processed",
			Details: map[string]interface{}{"status": reservation.Status},
		})
		return
	}

	// Create a reverse transaction to release tokens
	releaseID := uuid.New().String()

	query := `
		INSERT INTO "token_transactions" (
			id, "user_id", type, amount, balance, description,
			metadata, "created_at"
		)
		SELECT
			$1,
			"user_id",
			'release',
			-amount, -- Reverse the amount
			balance - amount, -- Restore balance
			'Token reservation released: ' || description,
			jsonb_set(metadata, '{status}', '"released"'::jsonb),
			NOW()
		FROM "token_transactions"
		WHERE id = $2
		RETURNING "user_id", amount
	`

	var userID string
	var amount int

	err = h.DB.QueryRow(ctx, query, releaseID, reservationID).Scan(&userID, &amount)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to release reservation"})
		return
	}

	// Mark original reservation as released
	updateQuery := `
		UPDATE "token_transactions"
		SET metadata = jsonb_set(metadata, '{status}', '"released"'::jsonb)
		WHERE id = $1
	`
	_, err = h.DB.Exec(ctx, updateQuery, reservationID)
	if err != nil {
		// Log but don't fail
		_ = err
	}

	// Return result
	result := map[string]interface{}{
		"reservationId": reservationID,
		"releaseId":     releaseID,
		"user_id":        userID,
		"amount":        amount,
		"releasedAt":    time.Now(),
	}

	writeJSON(w, http.StatusOK, result)
}

// ConsumeTokensDirect handles POST /api/v1/users/{user_id}/tokens/consume
// Direct consumption without reservation (for simple operations)
func (h *Handler) ConsumeTokensDirect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := r.PathValue("user_id")

	if userID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "user_id is required"})
		return
	}

	var req struct {
		Amount  int    `json:"amount"`
		Service string `json:"service"`
		Action  string `json:"action"`
		Reason  string `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid request body"})
		return
	}

	if req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Amount must be positive"})
		return
	}

	// Check balance
	balance, err := h.getUserBalance(ctx, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to check balance"})
		return
	}

	if balance.Available < req.Amount {
		writeJSON(w, http.StatusPaymentRequired, ErrorResponse{
			Error: "Insufficient tokens",
			Details: map[string]interface{}{
				"required":  req.Amount,
				"available": balance.Available,
			},
		})
		return
	}

	// Create consumption transaction
	transactionID := uuid.New().String()

	query := `
		INSERT INTO "token_transactions" (
			id, "user_id", type, amount, balance, description,
			service, "actionType", metadata, "created_at"
		) VALUES ($1, $2, 'consume', $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, "created_at"
	`

	metadata := map[string]interface{}{
		"service": req.Service,
		"action":  req.Action,
		"reason":  req.Reason,
	}
	metadataJSON, _ := json.Marshal(metadata)

	var createdID string
	var created_at time.Time

	err = h.DB.QueryRow(ctx, query,
		transactionID,
		userID,
		-req.Amount,
		balance.Total-req.Amount,
		fmt.Sprintf("Token consumed: %s.%s - %s", req.Service, req.Action, req.Reason),
		req.Service,
		req.Action,
		metadataJSON,
		time.Now(),
	).Scan(&createdID, &created_at)

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to consume tokens"})
		return
	}

	// Return result
	result := map[string]interface{}{
		"transactionId": transactionID,
		"user_id":        userID,
		"amount":        req.Amount,
		"balance":       balance.Total - req.Amount,
		"consumedAt":    created_at,
	}

	writeJSON(w, http.StatusOK, result)
}

// getReservation fetches a reservation by ID
func (h *Handler) getReservation(ctx context.Context, reservationID string) (*TokenReservation, error) {
	query := `
		SELECT
			id, "user_id", amount, description, metadata, "created_at"
		FROM "token_transactions"
		WHERE id = $1 AND type = 'reserve'
	`

	var reservation TokenReservation
	var amount int
	var description string
	var metadataJSON []byte

	err := h.DB.QueryRow(ctx, query, reservationID).Scan(
		&reservation.ID,
		&reservation.UserID,
		&amount,
		&description,
		&metadataJSON,
		&reservation.CreatedAt,
	)

	if err != nil {
		return nil, err
	}

	reservation.Amount = -amount // Convert to positive

	// Parse metadata
	var metadata map[string]interface{}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &metadata)
	}

	if reason, ok := metadata["reason"].(string); ok {
		reservation.Reason = reason
	}
	if status, ok := metadata["status"].(string); ok {
		reservation.Status = status
	}
	if expiresAtStr, ok := metadata["expiresAt"].(string); ok {
		expiresAt, _ := time.Parse(time.RFC3339, expiresAtStr)
		reservation.ExpiresAt = expiresAt
	}

	return &reservation, nil
}

// getUserBalance fetches user's current balance
func (h *Handler) getUserBalance(ctx context.Context, userID string) (*user_tokensBalance, error) {
	query := `
		SELECT
			COALESCE(SUM(amount) FILTER (WHERE type IN ('topup', 'purchase', 'grant')), 0) as total_topup,
			COALESCE(SUM(ABS(amount)) FILTER (WHERE type IN ('consume', 'reserve')), 0) as total_consumed,
			COALESCE(SUM(ABS(amount)) FILTER (WHERE type = 'reserve' AND metadata->>'status' = 'pending'), 0) as reserved
		FROM "token_transactions"
		WHERE "user_id" = $1
	`

	var totalTopup, totalConsumed, reserved int64
	err := h.DB.QueryRow(ctx, query, userID).Scan(&totalTopup, &totalConsumed, &reserved)
	if err != nil {
		return nil, err
	}

	total := int(totalTopup - totalConsumed)
	available := int(totalTopup - totalConsumed - reserved)

	return &user_tokensBalance{
		UserID:    userID,
		Available: available,
		Reserved:  int(reserved),
		Total:     total,
	}, nil
}

// user_tokensBalance represents user's token balance
type user_tokensBalance struct {
	UserID    string `json:"user_id"`
	Available int    `json:"available"`
	Reserved  int    `json:"reserved"`
	Total     int    `json:"total"`
}
