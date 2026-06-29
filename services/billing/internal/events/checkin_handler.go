package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// CheckinCompletedPayload defines the structure for checkin completed event
type CheckinCompletedPayload struct {
	EventID    string                 `json:"eventId"`
	EventType  string                 `json:"eventType"`
	OccurredAt string                 `json:"occurredAt"`
	UserID     string                 `json:"user_id"`
	Data       CheckinCompletedData   `json:"data"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// CheckinCompletedData contains the checkin event data
type CheckinCompletedData struct {
	CheckinID   string `json:"checkinId"`
	UserID      string `json:"user_id"`
	CheckinDate string `json:"checkinDate"`
	Streak      int    `json:"streak"`
	TokenReward int    `json:"tokenReward"`
	IsFirstTime bool   `json:"isFirstTime,omitempty"`
}

// CheckinEventHandler handles checkin-related events
type CheckinEventHandler struct {
	db *sql.DB
}

// NewCheckinEventHandler creates a new checkin event handler
func NewCheckinEventHandler(db *sql.DB) *CheckinEventHandler {
	return &CheckinEventHandler{
		db: db,
	}
}

// HandleCheckinCompleted processes the user.checkin.completed event
// This is called by the Pub/Sub subscriber when a checkin event is received
func (h *CheckinEventHandler) HandleCheckinCompleted(ctx context.Context, payload []byte) error {
	var event CheckinCompletedPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("failed to unmarshal CheckinCompleted payload: %w", err)
	}

	log.Printf("Processing CheckinCompleted event for userID: %s, checkinID: %s, streak: %d, reward: %d",
		event.Data.UserID, event.Data.CheckinID, event.Data.Streak, event.Data.TokenReward)

	// Validate event data
	if event.Data.UserID == "" {
		return fmt.Errorf("userID is required in checkin event")
	}

	if event.Data.TokenReward <= 0 {
		log.Printf("No token reward for checkin %s. Skipping.", event.Data.CheckinID)
		return nil // Not an error, just no action needed
	}

	// Check for duplicate processing (idempotency)
	isDuplicate, err := h.isEventProcessed(ctx, event.EventID)
	if err != nil {
		return fmt.Errorf("failed to check event processing status: %w", err)
	}
	if isDuplicate {
		log.Printf("Event %s already processed. Skipping.", event.EventID)
		return nil
	}

	// Credit tokens using the existing CreditCheckinTokens function
	meta := map[string]interface{}{
		"checkinId":   event.Data.CheckinID,
		"checkinDate": event.Data.CheckinDate,
		"streak":      event.Data.Streak,
		"isFirstTime": event.Data.IsFirstTime,
		"eventId":     event.EventID,
	}

	description := fmt.Sprintf("Daily checkin reward (streak: %d)", event.Data.Streak)
	if event.Data.IsFirstTime {
		description = "First time checkin bonus"
	}

	err = CreditCheckinTokens(ctx, h.db, event.Data.UserID, event.Data.TokenReward, description, meta)
	if err != nil {
		return fmt.Errorf("failed to credit checkin tokens: %w", err)
	}

	// Mark event as processed
	err = h.markEventProcessed(ctx, event.EventID, event.Data.UserID, "checkin_completed")
	if err != nil {
		log.Printf("Warning: Failed to mark event as processed: %v", err)
		// Don't fail the operation if marking fails
	}

	log.Printf("Successfully processed checkin event for userID: %s. Awarded %d tokens.", event.Data.UserID, event.Data.TokenReward)

	return nil
}

// isEventProcessed checks if an event has already been processed (for idempotency)
func (h *CheckinEventHandler) isEventProcessed(ctx context.Context, eventID string) (bool, error) {
	query := `
		SELECT COUNT(*) 
		FROM processed_events 
		WHERE event_id = $1
	`

	var count int
	err := h.db.QueryRowContext(ctx, query, eventID).Scan(&count)
	if err != nil {
		// If table doesn't exist, assume not processed
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}

	return count > 0, nil
}

// markEventProcessed marks an event as processed
func (h *CheckinEventHandler) markEventProcessed(ctx context.Context, eventID, userID, eventType string) error {
	query := `
		INSERT INTO processed_events (event_id, user_id, event_type, processed_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (event_id) DO NOTHING
	`

	_, err := h.db.ExecContext(ctx, query, eventID, userID, eventType, time.Now())
	return err
}

// HandleCheckinCompletedWithRetry wraps HandleCheckinCompleted with retry logic
func (h *CheckinEventHandler) HandleCheckinCompletedWithRetry(ctx context.Context, payload []byte, maxRetries int) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := h.HandleCheckinCompleted(ctx, payload)
		if err == nil {
			return nil
		}

		lastErr = err
		log.Printf("Attempt %d/%d failed for checkin event: %v", attempt, maxRetries, err)

		if attempt < maxRetries {
			// Exponential backoff: 1s, 2s, 4s
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("Retrying in %v...", backoff)
			time.Sleep(backoff)
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}
