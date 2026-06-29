// github.com/ScientificInternet/Google-Monetize/services/billing/internal/events/handler.go
package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log" // Using standard log for simplicity in this module
)

// OnboardingStepCompletedPayload defines the structure for its event.
type OnboardingStepCompletedPayload struct {
	UserID       string `json:"user_id"`
	StepID       string `json:"stepId"`
	RewardTokens int    `json:"rewardTokens"`
}

// HandleOnboardingStepCompleted processes the event to grant rewards for completing a step.
func HandleOnboardingStepCompleted(ctx context.Context, db *sql.DB, payload []byte) error {
	var data OnboardingStepCompletedPayload
	if err := json.Unmarshal(payload, &data); err != nil {
		return fmt.Errorf("failed to unmarshal OnboardingStepCompleted payload: %w", err)
	}

	log.Printf("Processing OnboardingStepCompleted event for userID: %s, step: %s", data.UserID, data.StepID)

	rewardTokens := data.RewardTokens
	if rewardTokens <= 0 {
		log.Printf("No reward for step %s. Skipping.", data.StepID)
		return nil // Not an error, just no action needed.
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Mark the step as completed for the user in the read model.
	// This makes the operation idempotent.
	_, err = tx.ExecContext(ctx, `
        INSERT INTO "UserChecklistProgress" (id, "user_id", "stepId", "isCompleted", "completedAt")
        VALUES (DEFAULT, $1, $2, TRUE, NOW())
        ON CONFLICT ("user_id", "stepId") DO NOTHING
    `, data.UserID, data.StepID)
	if err != nil {
		return fmt.Errorf("failed to mark onboarding step as completed: %w", err)
	}

	// 2. Get current token balance.
	var balanceBefore int64
	err = tx.QueryRowContext(ctx, `SELECT balance FROM "user_tokens" WHERE "user_id" = $1 FOR UPDATE`, data.UserID).Scan(&balanceBefore)
	if err != nil {
		if err == sql.ErrNoRows {
			balanceBefore = 0
		} else {
			return fmt.Errorf("failed to query user token balance: %w", err)
		}
	}

	// 3. Upsert the user_tokens balance.
	_, err = tx.ExecContext(ctx, `
        INSERT INTO "user_tokens" ("user_id", balance, "updated_at")
        VALUES ($1, $2, NOW())
        ON CONFLICT ("user_id") DO UPDATE SET
        balance = "user_tokens".balance + $2,
        "updated_at" = NOW()
    `, data.UserID, rewardTokens)
	if err != nil {
		return fmt.Errorf("failed to upsert user token balance: %w", err)
	}

	// 3.1 Upsert user_tokensPool for activity pool
	_, _ = tx.ExecContext(ctx, `INSERT INTO "user_tokensPool"("user_id", subscription, activity, purchased, "updated_at") VALUES ($1,0,$2,0,NOW())
        ON CONFLICT ("user_id") DO UPDATE SET activity = "user_tokensPool".activity + EXCLUDED.activity, "updated_at"=NOW()`, data.UserID, rewardTokens)

	// 4. Create a token_transactions record.
	_, err = tx.ExecContext(ctx, `
        INSERT INTO "token_transactions"
        (id, "user_id", type, amount, "balanceBefore", "balanceAfter", source, description, metadata, "created_at")
        VALUES (DEFAULT, $1, 'ACTIVITY', $2, $3, $4, 'activity', $5, $6, NOW())
    `, data.UserID, rewardTokens, balanceBefore, balanceBefore+int64(rewardTokens),
		fmt.Sprintf("Onboarding reward for step: %s", data.StepID),
		fmt.Sprintf(`{"stepId":"%s"}`, data.StepID),
	)
	if err != nil {
		return fmt.Errorf("failed to create token transaction for onboarding: %w", err)
	}

	// 5. Insert credit lot for activity (no expiry)
	_, _ = tx.ExecContext(ctx, `
        INSERT INTO "TokenCreditLot"("user_id", source, amount, remaining, "expiresAt", meta)
        VALUES ($1,'activity',$2,$2,NULL,$3)
    `, data.UserID, rewardTokens, fmt.Sprintf(`{"event":"OnboardingStepCompleted","stepId":"%s"}`, data.StepID))

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Successfully processed onboarding step for userID: %s. Awarded %d tokens.", data.UserID, rewardTokens)
	return nil
}

// isUniqueViolation checks if an error is a PostgreSQL unique violation error.
func isUniqueViolation(err error) bool {
	// This is a simplified check. In a real application, you'd use the driver-specific error code.
	// For pq, the code is '23505'.
	return err != nil && len(err.Error()) > 18 && err.Error()[12:17] == "23505"
}

// CreditsubscriptionsTokens credits subscription pool and aggregate balance.
func CreditsubscriptionsTokens(ctx context.Context, db *sql.DB, userID string, amount int, desc string, meta map[string]any) error {
	if amount <= 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before int64
	_ = tx.QueryRowContext(ctx, `SELECT balance FROM "user_tokens" WHERE "user_id"=$1 FOR UPDATE`, userID).Scan(&before)
	if _, err := tx.ExecContext(ctx, `INSERT INTO "user_tokens"("user_id", balance, "updated_at") VALUES ($1,$2,NOW()) ON CONFLICT ("user_id") DO UPDATE SET balance = "user_tokens".balance + EXCLUDED.balance, "updated_at"=NOW()`, userID, amount); err != nil {
		return err
	}
	// pool +subscription
	_, _ = tx.ExecContext(ctx, `INSERT INTO "user_tokensPool"("user_id", subscription, activity, purchased, "updated_at") VALUES ($1,$2,0,0,NOW()) ON CONFLICT ("user_id") DO UPDATE SET subscription = "user_tokensPool".subscription + EXCLUDED.subscription, "updated_at"=NOW()`, userID, amount)
	after := before + int64(amount)
	if meta == nil {
		meta = map[string]any{}
	}
	b, _ := json.Marshal(meta)
	if _, err := tx.ExecContext(ctx, `INSERT INTO "token_transactions"(id, "user_id", type, amount, "balanceBefore", "balanceAfter", source, description, metadata, "created_at") VALUES (DEFAULT,$1,'CREDIT',$2,$3,$4,'subscription',$5,$6,NOW())`, userID, amount, before, after, desc, string(b)); err != nil {
		return err
	}
	// credit lot with optional expiry
	var expires any = nil
	if v, ok := meta["expiresAt"]; ok {
		if s, ok2 := v.(string); ok2 && s != "" {
			expires = s
		}
	}
	_, _ = tx.ExecContext(ctx, `INSERT INTO "TokenCreditLot"("user_id", source, amount, remaining, "expiresAt", meta) VALUES ($1,'subscription',$2,$2,$3,$4)`, userID, amount, expires, string(b))
	return tx.Commit()
}

// CreditPurchasedTokens credits purchased pool and aggregate balance.
func CreditPurchasedTokens(ctx context.Context, db *sql.DB, userID string, amount int, desc string, meta map[string]any) error {
	if amount <= 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before int64
	_ = tx.QueryRowContext(ctx, `SELECT balance FROM "user_tokens" WHERE "user_id"=$1 FOR UPDATE`, userID).Scan(&before)
	if _, err := tx.ExecContext(ctx, `INSERT INTO "user_tokens"("user_id", balance, "updated_at") VALUES ($1,$2,NOW()) ON CONFLICT ("user_id") DO UPDATE SET balance = "user_tokens".balance + EXCLUDED.balance, "updated_at"=NOW()`, userID, amount); err != nil {
		return err
	}
	// pool +purchased
	_, _ = tx.ExecContext(ctx, `INSERT INTO "user_tokensPool"("user_id", subscription, activity, purchased, "updated_at") VALUES ($1,0,0,$2,NOW()) ON CONFLICT ("user_id") DO UPDATE SET purchased = "user_tokensPool".purchased + EXCLUDED.purchased, "updated_at"=NOW()`, userID, amount)
	after := before + int64(amount)
	if meta == nil {
		meta = map[string]any{}
	}
	b, _ := json.Marshal(meta)
	if _, err := tx.ExecContext(ctx, `INSERT INTO "token_transactions"(id, "user_id", type, amount, "balanceBefore", "balanceAfter", source, description, metadata, "created_at") VALUES (DEFAULT,$1,'CREDIT',$2,$3,$4,'purchased',$5,$6,NOW())`, userID, amount, before, after, desc, string(b)); err != nil {
		return err
	}
	// credit lot without expiry
	_, _ = tx.ExecContext(ctx, `INSERT INTO "TokenCreditLot"("user_id", source, amount, remaining, "expiresAt", meta) VALUES ($1,'purchased',$2,$2,NULL,$3)`, userID, amount, string(b))
	return tx.Commit()
}

// CreditCheckinTokens credits activity pool (checkin rewards) and aggregate balance.
func CreditCheckinTokens(ctx context.Context, db *sql.DB, userID string, amount int, desc string, meta map[string]any) error {
	if amount <= 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before int64
	_ = tx.QueryRowContext(ctx, `SELECT balance FROM "user_tokens" WHERE "user_id"=$1 FOR UPDATE`, userID).Scan(&before)
	if _, err := tx.ExecContext(ctx, `INSERT INTO "user_tokens"("user_id", balance, "updated_at") VALUES ($1,$2,NOW()) ON CONFLICT ("user_id") DO UPDATE SET balance = "user_tokens".balance + EXCLUDED.balance, "updated_at"=NOW()`, userID, amount); err != nil {
		return err
	}
	// pool +activity (checkin is an activity reward)
	_, _ = tx.ExecContext(ctx, `INSERT INTO "user_tokensPool"("user_id", subscription, activity, purchased, "updated_at") VALUES ($1,0,$2,0,NOW()) ON CONFLICT ("user_id") DO UPDATE SET activity = "user_tokensPool".activity + EXCLUDED.activity, "updated_at"=NOW()`, userID, amount)
	after := before + int64(amount)
	if meta == nil {
		meta = map[string]any{}
	}
	b, _ := json.Marshal(meta)
	if _, err := tx.ExecContext(ctx, `INSERT INTO "token_transactions"(id, "user_id", type, amount, "balanceBefore", "balanceAfter", source, description, metadata, "created_at") VALUES (DEFAULT,$1,'CREDIT',$2,$3,$4,'checkin',$5,$6,NOW())`, userID, amount, before, after, desc, string(b)); err != nil {
		return err
	}
	// Also insert into DailyCheckin table for backward compatibility
	if _, err := tx.ExecContext(ctx, `INSERT INTO "DailyCheckin"("user_id", "checkinDate", "reward", "streak", "created_at") VALUES ($1, CURRENT_DATE, $2, $3, NOW()) ON CONFLICT ("user_id", "checkinDate") DO NOTHING`, userID, amount, meta["streak"]); err != nil {
		// Log but don't fail if DailyCheckin insert fails
		log.Printf("Warning: Failed to insert DailyCheckin record: %v", err)
	}
	// credit lot without expiry
	_, _ = tx.ExecContext(ctx, `INSERT INTO "TokenCreditLot"("user_id", source, amount, remaining, "expiresAt", meta) VALUES ($1,'checkin',$2,$2,NULL,$3)`, userID, amount, string(b))
	return tx.Commit()
}
