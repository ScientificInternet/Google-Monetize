//go:build ignore

package projectors

import (
	"context"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/domain"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OnboardingProjector handles events related to user onboarding and rewards.
type OnboardingProjector struct {
	db *pgxpool.Pool
}

// NewOnboardingProjector creates a new OnboardingProjector.
func NewOnboardingProjector(db *pgxpool.Pool) *OnboardingProjector {
	return &OnboardingProjector{db: db}
}

// HandleOfferCreated checks if it's the user's first offer and grants a token reward.
func (p *OnboardingProjector) HandleOfferCreated(ctx context.Context, event domain.OfferCreatedEvent) error {
	const stepID = "create-first-offer" // This ID should match the one in the OnboardingChecklist table.
	log.Printf("PROJECTOR: Handling OfferCreatedEvent for user %s to check for onboarding reward.", event.UserID)

	tx, err := p.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 1. Check if this step is already completed to ensure idempotency.
	var progressID string
	err = tx.QueryRow(ctx, `SELECT id FROM "UserChecklistProgress" WHERE "user_id" = $1 AND "stepId" = $2`, event.UserID, stepID).Scan(&progressID)
	if err == nil {
		// If a row is found, it means the step is already completed.
		log.Printf("PROJECTOR: Onboarding step '%s' already completed for user %s. No action taken.", stepID, event.UserID)
		return tx.Commit(ctx) // Commit to end the transaction.
	}
	// We expect pgx.ErrNoRows here, any other error is a problem.

	// 2. Get the reward amount for this step.
	var rewardTokens int
	err = tx.QueryRow(ctx, `SELECT "rewardTokens" FROM "OnboardingChecklist" WHERE id = $1`, stepID).Scan(&rewardTokens)
	if err != nil {
		log.Printf("ERROR: Could not find reward tokens for step '%s': %v", stepID, err)
		return err // Rollback
	}

	if rewardTokens <= 0 {
		log.Printf("PROJECTOR: No reward tokens for step '%s'. Marking as complete without reward.", stepID)
	} else {
		// 3. Update user's token balance.
		_, err = tx.Exec(ctx, `UPDATE "user_tokens" SET balance = balance + $1 WHERE "user_id" = $2`, rewardTokens, event.UserID)
		if err != nil {
			log.Printf("ERROR: Failed to update token balance for user %s: %v", event.UserID, err)
			return err // Rollback
		}

		// 4. Record the transaction.
		description := "Reward for creating your first offer"
		_, err = tx.Exec(ctx,
			`INSERT INTO "token_transactions" (id, "user_id", type, amount, source, description) VALUES (gen_random_uuid(), $1, 'REWARD', $2, $3, $4)`,
			event.UserID, rewardTokens, stepID, description)
		if err != nil {
			log.Printf("ERROR: Failed to record token transaction for user %s: %v", event.UserID, err)
			return err // Rollback
		}
		log.Printf("PROJECTOR: Awarded %d tokens to user %s.", rewardTokens, event.UserID)
	}

	// 5. Mark the step as completed.
	_, err = tx.Exec(ctx,
		`INSERT INTO "UserChecklistProgress" (id, "user_id", "stepId", "isCompleted", "completedAt") VALUES (gen_random_uuid(), $1, $2, TRUE, NOW())`,
		event.UserID, stepID)
	if err != nil {
		log.Printf("ERROR: Failed to mark onboarding step '%s' as complete for user %s: %v", stepID, err)
		return err // Rollback
	}

	return tx.Commit(ctx)
}
