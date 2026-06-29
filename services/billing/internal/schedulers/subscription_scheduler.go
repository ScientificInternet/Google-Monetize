package schedulers

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/handlers"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/notifications"
)

// subscriptionsScheduler handles subscription-related scheduled tasks
type subscriptionsScheduler struct {
	db                 *pgxpool.Pool
	pendingSubHandler  *handlers.PendingsubscriptionsHandler
	notificationClient *notifications.NotificationClient
}

// NewsubscriptionsScheduler creates a new subscription scheduler
func NewsubscriptionsScheduler(db *pgxpool.Pool, pendingSubHandler *handlers.PendingsubscriptionsHandler, notificationClient *notifications.NotificationClient) *subscriptionsScheduler {
	return &subscriptionsScheduler{
		db:                 db,
		pendingSubHandler:  pendingSubHandler,
		notificationClient: notificationClient,
	}
}

// ProcessPendingActivations activates pending subscriptions when blocking subscription has expired
// This should run hourly
func (s *subscriptionsScheduler) ProcessPendingActivations(ctx context.Context) error {
	log.Println("[Scheduler] Processing pending subscription activations...")

	// Query pending subscriptions where:
	// 1. Status is 'pending'
	// 2. Not expired yet
	// 3. Blocking subscription has expired or is no longer active
	query := `
		SELECT ps.id, ps.user_id, ps.blocking_subscription_id
		FROM "Pendingsubscriptions" ps
		LEFT JOIN "subscriptions" bs ON ps.blocking_subscription_id = bs.id
		WHERE ps.status = 'pending'
		  AND ps.expires_at > NOW()
		  AND (
		      ps.blocking_subscription_id IS NULL
		      OR bs.id IS NULL
		      OR bs."trialEndDate" < NOW()
		      OR bs.status IN ('expired', 'canceled')
		  )
		ORDER BY ps.created_at ASC
	`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query pending subscriptions: %w", err)
	}
	defer rows.Close()

	var activatedCount int
	var failedCount int

	for rows.Next() {
		var pendingID string
		var userID string
		var blockingSubID *string

		if err := rows.Scan(&pendingID, &userID, &blockingSubID); err != nil {
			log.Printf("[Scheduler] Error scanning pending subscription: %v", err)
			failedCount++
			continue
		}

		// Activate the pending subscription
		if s.pendingSubHandler != nil {
			if err := s.pendingSubHandler.ActivatePendingsubscriptions(ctx, pendingID); err != nil {
				log.Printf("[Scheduler] Failed to activate pending subscription %s for user %s: %v", pendingID, userID, err)
				failedCount++
				continue
			}

			log.Printf("[Scheduler] Activated pending subscription %s for user %s", pendingID, userID)
			activatedCount++

			// Send activation notification
			if s.notificationClient != nil {
				// Note: We'd need to fetch plan details and token quota from the pending subscription
				// For now, we'll skip the notification here since we don't have those details in the query
				// The notification will be sent from the ActivatePendingsubscriptions method instead
			}
		}
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating pending subscriptions: %w", err)
	}

	log.Printf("[Scheduler] Completed pending activation processing: activated=%d, failed=%d", activatedCount, failedCount)
	return nil
}

// ExpirePendingsubscriptionss marks pending subscriptions as expired after 180 days
// This should run daily
func (s *subscriptionsScheduler) ExpirePendingsubscriptionss(ctx context.Context) error {
	log.Println("[Scheduler] Expiring old pending subscriptions...")

	query := `
		UPDATE "Pendingsubscriptions"
		SET status = 'expired'
		WHERE status = 'pending'
		  AND expires_at < NOW()
	`

	result, err := s.db.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to expire pending subscriptions: %w", err)
	}

	rowsAffected := result.RowsAffected()
	log.Printf("[Scheduler] Expired %d pending subscriptions", rowsAffected)

	return nil
}

// CleanExpiredTokens removes expired tokens from user balances
// This should run daily at 2am
func (s *subscriptionsScheduler) CleanExpiredTokens(ctx context.Context) error {
	log.Println("[Scheduler] Cleaning expired tokens...")

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Find all expired tokens that still have positive amounts
	query := `
		SELECT id, "user_id", amount, source, expires_at, description
		FROM "token_transactions"
		WHERE expires_at IS NOT NULL
		  AND expires_at < NOW()
		  AND amount > 0
		  AND type = 'grant'
	`

	rows, err := tx.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query expired tokens: %w", err)
	}
	defer rows.Close()

	type expiredToken struct {
		ID          string
		UserID      string
		Amount      int
		Source      string
		ExpiresAt   time.Time
		Description string
	}

	var expiredTokens []expiredToken
	for rows.Next() {
		var token expiredToken
		if err := rows.Scan(&token.ID, &token.UserID, &token.Amount, &token.Source, &token.ExpiresAt, &token.Description); err != nil {
			log.Printf("[Scheduler] Error scanning expired token: %v", err)
			continue
		}
		expiredTokens = append(expiredTokens, token)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating expired tokens: %w", err)
	}

	// 2. For each expired token, zero out the amount and deduct from balance
	var totalExpired int64
	for _, token := range expiredTokens {
		// Zero out the grant transaction
		_, err = tx.Exec(ctx, `
			UPDATE "token_transactions"
			SET amount = 0
			WHERE id = $1
		`, token.ID)
		if err != nil {
			log.Printf("[Scheduler] Failed to zero out expired token %s: %v", token.ID, err)
			continue
		}

		// Deduct from user balance
		_, err = tx.Exec(ctx, `
			UPDATE "user_tokens"
			SET balance = balance - $1,
			    "updated_at" = NOW()
			WHERE "user_id" = $2
		`, token.Amount, token.UserID)
		if err != nil {
			log.Printf("[Scheduler] Failed to deduct expired tokens from balance for user %s: %v", token.UserID, err)
			continue
		}

		// Create expiry transaction record
		_, err = tx.Exec(ctx, `
			INSERT INTO "token_transactions" (
				id, "user_id", type, amount, description,
				source, "created_at", metadata
			) VALUES (gen_random_uuid(), $1, 'deduct', $2, $3, $4, NOW(), $5)
		`, token.UserID, -token.Amount,
			fmt.Sprintf("Token expiry: %s", token.Description),
			token.Source,
			fmt.Sprintf(`{"expired_transaction_id": "%s", "expired_at": "%s"}`, token.ID, token.ExpiresAt.Format(time.RFC3339)))

		if err != nil {
			log.Printf("[Scheduler] Failed to create expiry transaction for user %s: %v", token.UserID, err)
			continue
		}

		log.Printf("[Scheduler] Expired %d tokens (source=%s) for user %s", token.Amount, token.Source, token.UserID)
		totalExpired += int64(token.Amount)

		// Send expiry notification
		if s.notificationClient != nil {
			if err := s.notificationClient.SendTokenExpiredNotification(ctx, token.UserID, token.Amount); err != nil {
				log.Printf("[Scheduler] Failed to send token expired notification to user %s: %v", token.UserID, err)
			}
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit token expiry transaction: %w", err)
	}

	log.Printf("[Scheduler] Completed token expiry cleanup: %d tokens from %d transactions", totalExpired, len(expiredTokens))
	return nil
}

// NotifyExpiringTokens sends notifications for tokens expiring soon (within 3 days)
// This should run daily at 9am
func (s *subscriptionsScheduler) NotifyExpiringTokens(ctx context.Context) error {
	log.Println("[Scheduler] Checking for tokens expiring soon...")

	// Find tokens expiring in the next 3 days
	query := `
		SELECT DISTINCT "user_id", SUM(amount) as total_expiring
		FROM "token_transactions"
		WHERE expires_at IS NOT NULL
		  AND expires_at > NOW()
		  AND expires_at < NOW() + INTERVAL '3 days'
		  AND amount > 0
		  AND type = 'grant'
		GROUP BY "user_id"
		HAVING SUM(amount) > 0
	`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query expiring tokens: %w", err)
	}
	defer rows.Close()

	var notificationCount int
	var failedNotifications int
	for rows.Next() {
		var userID string
		var totalExpiring int

		if err := rows.Scan(&userID, &totalExpiring); err != nil {
			log.Printf("[Scheduler] Error scanning expiring token: %v", err)
			continue
		}

		// Get the earliest expiry date for this user
		var expiresAt time.Time
		expiryQuery := `
			SELECT MIN(expires_at)
			FROM "token_transactions"
			WHERE "user_id" = $1
			  AND expires_at IS NOT NULL
			  AND expires_at > NOW()
			  AND expires_at < NOW() + INTERVAL '3 days'
			  AND amount > 0
			  AND type = 'grant'
		`
		if err := s.db.QueryRow(ctx, expiryQuery, userID).Scan(&expiresAt); err != nil {
			log.Printf("[Scheduler] Failed to get expiry date for user %s: %v", userID, err)
			failedNotifications++
			continue
		}

		// Send in-app notification
		if s.notificationClient != nil {
			if err := s.notificationClient.SendTokenExpiringSoonNotification(ctx, userID, totalExpiring, expiresAt); err != nil {
				log.Printf("[Scheduler] Failed to send expiring token notification to user %s: %v", userID, err)
				failedNotifications++
				continue
			}
		}

		log.Printf("[Scheduler] Sent expiring token notification to user %s (%d tokens expiring)", userID, totalExpiring)
		notificationCount++
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating expiring tokens: %w", err)
	}

	log.Printf("[Scheduler] Completed expiring token notifications: sent=%d, failed=%d", notificationCount, failedNotifications)
	return nil
}

// RunScheduler starts the scheduler with appropriate intervals
func (s *subscriptionsScheduler) RunScheduler(ctx context.Context) {
	log.Println("[Scheduler] Starting subscription scheduler...")

	// Run pending activation every hour
	activationTicker := time.NewTicker(1 * time.Hour)
	defer activationTicker.Stop()

	// Run token expiry cleanup daily at 2am
	expiryCleanupTicker := time.NewTicker(24 * time.Hour)
	defer expiryCleanupTicker.Stop()

	// Run expiry notification daily at 9am
	notificationTicker := time.NewTicker(24 * time.Hour)
	defer notificationTicker.Stop()

	// Run pending expiry daily
	pendingExpiryTicker := time.NewTicker(24 * time.Hour)
	defer pendingExpiryTicker.Stop()

	// Run initial tasks immediately
	go func() {
		if err := s.ProcessPendingActivations(ctx); err != nil {
			log.Printf("[Scheduler] Error in initial pending activation: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Scheduler] Stopping scheduler...")
			return

		case <-activationTicker.C:
			if err := s.ProcessPendingActivations(ctx); err != nil {
				log.Printf("[Scheduler] Error processing pending activations: %v", err)
			}

		case <-expiryCleanupTicker.C:
			// Check if it's around 2am
			now := time.Now()
			if now.Hour() == 2 {
				if err := s.CleanExpiredTokens(ctx); err != nil {
					log.Printf("[Scheduler] Error cleaning expired tokens: %v", err)
				}
			}

		case <-notificationTicker.C:
			// Check if it's around 9am
			now := time.Now()
			if now.Hour() == 9 {
				if err := s.NotifyExpiringTokens(ctx); err != nil {
					log.Printf("[Scheduler] Error notifying expiring tokens: %v", err)
				}
			}

		case <-pendingExpiryTicker.C:
			if err := s.ExpirePendingsubscriptionss(ctx); err != nil {
				log.Printf("[Scheduler] Error expiring pending subscriptions: %v", err)
			}
		}
	}
}
