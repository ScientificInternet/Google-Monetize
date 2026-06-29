//go:build ignore

package projectors

import (
	"context"
	"database/sql"
	"github.com/ScientificInternet/Google-Monetize/services/identity/internal/domain"
	"log"
	"time"
)

// subscriptionsProjector handles events to create and update billing read models.
type subscriptionsProjector struct {
	db *sql.DB
}

// NewsubscriptionsProjector creates a new subscriptionsProjector.
func NewsubscriptionsProjector(db *sql.DB) *subscriptionsProjector {
	return &subscriptionsProjector{db: db}
}

// HandleUserRegistered creates a default trial subscription and initial token balance for a new user.
func (p *subscriptionsProjector) HandleUserRegistered(ctx context.Context, event domain.UserRegisteredEvent) error {
	log.Printf("PROJECTOR: Handling UserRegisteredEvent for new user %s", event.UserID)

	// Use a transaction to ensure both subscription and tokens are created atomically.
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // Rollback on error

	// 1. Create a default "Free" plan subscription
	trialEndDate := time.Now().AddDate(100, 0, 0) // Long trial for the free plan
	_, err = tx.ExecContext(ctx, `
		INSERT INTO "subscriptions" (id, "user_id", "planId", "plan_name", status, "trialEndsAt", "currentPeriodEnd")
		VALUES (gen_random_uuid(), $1, 'free-plan', 'Free', 'trialing', $2, $2)
		ON CONFLICT ("user_id") DO NOTHING`,
		event.UserID, trialEndDate)
	if err != nil {
		log.Printf("ERROR: Failed to project subscription for user %s: %v", event.UserID, err)
		return err
	}

	// 2. Create the initial token balance (1,000 for Free plan)
	initialTokens := 1000
	_, err = tx.ExecContext(ctx, `
		INSERT INTO "user_tokens" ("user_id", balance, "updated_at")
		VALUES ($1, $2, NOW())
		ON CONFLICT ("user_id") DO NOTHING`,
		event.UserID, initialTokens)
	if err != nil {
		log.Printf("ERROR: Failed to project user token for user %s: %v", event.UserID, err)
		return err
	}

	log.Printf("Successfully created trial subscription and token balance for user %s", event.UserID)
	return tx.Commit()
}
