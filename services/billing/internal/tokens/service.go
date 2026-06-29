package tokens

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ScientificInternet/Google-Monetize/pkg/cache"
	"github.com/ScientificInternet/Google-Monetize/pkg/metrics"
)

// Service handles token operations
type Service struct {
	db    *pgxpool.Pool
	cache *cache.Cache
}

// NewService creates a new token service
func NewService(db *pgxpool.Pool, cache *cache.Cache) *Service {
	return &Service{
		db:    db,
		cache: cache,
	}
}

// BalanceSummary 表示用户 Token 余额与消耗统计
type BalanceSummary struct {
	TotalBalance            int64      `json:"total_balance"`
	TodayConsumed           int64      `json:"today_consumed"`
	ThisMonthConsumed       int64      `json:"this_month_consumed"`
	PendingTasksCount       int64      `json:"pending_tasks_count"`
	EstimatedCostForPending int64      `json:"estimated_cost_for_pending"`
	Balance                 int64      `json:"balance"`
	UpdatedAt               *time.Time `json:"updated_at,omitempty"`
}

// TransactionType represents the type of token transaction
type TransactionType string

const (
	TransactionTypeDeduct TransactionType = "deduct"
	TransactionTypeRefund TransactionType = "refund"
	TransactionTypeGrant  TransactionType = "grant"
)

// CheckAndReserveTokens checks if user has enough tokens and reserves them
// Returns reservation ID if successful
func (s *Service) CheckAndReserveTokens(ctx context.Context, userID string, amount int, description string) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Check current balance
	var balance int64
	err = tx.QueryRow(ctx, `
		SELECT balance
		FROM "user_tokens"
		WHERE "user_id" = $1
		FOR UPDATE
	`, userID).Scan(&balance)

	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("user token record not found")
	}
	if err != nil {
		return "", fmt.Errorf("failed to query balance: %w", err)
	}

	// 2. Check if sufficient balance
	if balance < int64(amount) {
		return "", fmt.Errorf("insufficient tokens: have %d, need %d", balance, amount)
	}

	// 3. Deduct tokens
	_, err = tx.Exec(ctx, `
		UPDATE "user_tokens"
		SET balance = balance - $1,
		    "updated_at" = NOW()
		WHERE "user_id" = $2
	`, amount, userID)
	if err != nil {
		return "", fmt.Errorf("failed to deduct tokens: %w", err)
	}

	// 4. Create transaction record with reservation flag
	reservationID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO "token_transactions" (
			id, "user_id", type, amount, description,
			"created_at", metadata
		) VALUES ($1, $2, $3, $4, $5, NOW(), $6)
	`, reservationID, userID, TransactionTypeDeduct, -amount, description,
		map[string]interface{}{"status": "reserved"})

	if err != nil {
		return "", fmt.Errorf("failed to create transaction: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate balance cache
	s.invalidateBalanceCache(ctx, userID)

	// Record metrics
	m := metrics.GetGlobalBusinessMetrics()
	m.RecordTokenReservation(userID, float64(amount))

	return reservationID, nil
}

// ConfirmTokenDeduction confirms a reserved token deduction
func (s *Service) ConfirmTokenDeduction(ctx context.Context, reservationID string) error {
	// Get transaction details for metrics
	var userID string
	var amount int
	var description string
	err := s.db.QueryRow(ctx, `
		SELECT "user_id", amount, description
		FROM "token_transactions"
		WHERE id = $1
	`, reservationID).Scan(&userID, &amount, &description)

	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("failed to query transaction: %w", err)
	}

	// Update transaction status
	_, err = s.db.Exec(ctx, `
		UPDATE "token_transactions"
		SET metadata = metadata || '{"status": "confirmed"}'::jsonb
		WHERE id = $1
	`, reservationID)

	if err != nil {
		return fmt.Errorf("failed to confirm deduction: %w", err)
	}

	// Record consumption metrics (convert negative amount to positive)
	if userID != "" && amount < 0 {
		m := metrics.GetGlobalBusinessMetrics()
		// Extract operation from description (e.g., "offer_creation", "ad_campaign")
		operation := "unknown"
		if description != "" {
			operation = description
		}
		m.RecordTokenConsumption(userID, operation, float64(-amount))
	}

	return nil
}

// RefundTokens refunds tokens to user (e.g., on task failure)
func (s *Service) RefundTokens(ctx context.Context, userID, reservationID string, amount int, reason string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Add tokens back
	_, err = tx.Exec(ctx, `
		UPDATE "user_tokens"
		SET balance = balance + $1,
		    "updated_at" = NOW()
		WHERE "user_id" = $2
	`, amount, userID)
	if err != nil {
		return fmt.Errorf("failed to refund tokens: %w", err)
	}

	// 2. Create refund transaction
	refundID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO "token_transactions" (
			id, "user_id", type, amount, description,
			"created_at", metadata
		) VALUES ($1, $2, $3, $4, $5, NOW(), $6)
	`, refundID, userID, TransactionTypeRefund, amount, fmt.Sprintf("Refund: %s", reason),
		map[string]interface{}{"original_reservation": reservationID, "reason": reason})

	if err != nil {
		return fmt.Errorf("failed to create refund transaction: %w", err)
	}

	// 3. Mark original transaction as refunded
	_, err = tx.Exec(ctx, `
		UPDATE "token_transactions"
		SET metadata = metadata || '{"status": "refunded", "refund_id": $1}'::jsonb
		WHERE id = $2
	`, refundID, reservationID)

	if err != nil {
		return fmt.Errorf("failed to mark original transaction: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit refund: %w", err)
	}

	// Invalidate balance cache
	s.invalidateBalanceCache(ctx, userID)

	// Record refund metrics
	m := metrics.GetGlobalBusinessMetrics()
	m.AddCounter(metrics.MetricTokensRefunded, float64(amount), map[string]string{
		"user_id": userID,
		"reason":  reason,
	})

	return nil
}

// GetBalance returns user's current token balance
func (s *Service) GetBalance(ctx context.Context, userID string) (int64, error) {
	// Try cache first (60 second TTL)
	cacheKey := fmt.Sprintf("token:balance:%s", userID)
	if s.cache != nil && s.cache.Ready() {
		if cached, ok := s.cache.Get(ctx, cacheKey); ok && cached != "" {
			var balance int64
			if _, err := fmt.Sscanf(cached, "%d", &balance); err == nil {
				return balance, nil
			}
		}
	}

	// Cache miss - query database
	var balance int64
	err := s.db.QueryRow(ctx, `
        SELECT balance
        FROM "user_tokens"
        WHERE "user_id" = $1
	`, userID).Scan(&balance)

	if err == pgx.ErrNoRows {
		return 0, fmt.Errorf("user token record not found")
	}
	if err != nil {
		return 0, fmt.Errorf("failed to query balance: %w", err)
	}

	// Write to cache (60 second TTL)
	if s.cache != nil && s.cache.Ready() {
		s.cache.Set(ctx, cacheKey, fmt.Sprintf("%d", balance), 60*time.Second)
	}

	return balance, nil
}

// invalidateBalanceCache clears the cached balance and summary for a user
func (s *Service) invalidateBalanceCache(ctx context.Context, userID string) {
	if s.cache != nil && s.cache.Ready() {
		// Clear balance cache
		balanceKey := fmt.Sprintf("token:balance:%s", userID)
		s.cache.Del(ctx, balanceKey)

		// Clear summary cache
		summaryKey := fmt.Sprintf("token:summary:%s", userID)
		s.cache.Del(ctx, summaryKey)
	}
}

// GetTransactionHistory returns user's token transaction history
func (s *Service) GetTransactionHistory(ctx context.Context, userID string, limit int) ([]Transaction, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, type, amount, description, "created_at", metadata
		FROM "token_transactions"
		WHERE "user_id" = $1
		ORDER BY "created_at" DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	var transactions []Transaction
	for rows.Next() {
		var t Transaction
		var metadata map[string]interface{}
		err := rows.Scan(&t.ID, &t.Type, &t.Amount, &t.Description, &t.CreatedAt, &metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to scan transaction: %w", err)
		}
		t.Metadata = metadata
		transactions = append(transactions, t)
	}

	return transactions, nil
}

// GetBalanceSummary 返回 Token 余额及常用统计
func (s *Service) GetBalanceSummary(ctx context.Context, userID string) (BalanceSummary, error) {
	// Try cache first (30 second TTL for summary data)
	cacheKey := fmt.Sprintf("token:summary:%s", userID)
	if s.cache != nil && s.cache.Ready() {
		if cached, ok := s.cache.Get(ctx, cacheKey); ok && cached != "" {
			var summary BalanceSummary
			if err := json.Unmarshal([]byte(cached), &summary); err == nil {
				return summary, nil
			}
		}
	}

	// Cache miss - query database
	summary := BalanceSummary{}

	var (
		updated_at sql.NullTime
	)

	err := s.db.QueryRow(ctx, `
        SELECT balance, "updated_at"
        FROM "user_tokens"
        WHERE "user_id" = $1
    `, userID).Scan(&summary.TotalBalance, &updated_at)
	if err == pgx.ErrNoRows {
		return summary, fmt.Errorf("user token record not found")
	}
	if err != nil {
		return summary, fmt.Errorf("failed to query balance: %w", err)
	}

	summary.Balance = summary.TotalBalance
	if updated_at.Valid {
		ts := updated_at.Time.UTC()
		summary.UpdatedAt = &ts
	}

	if err := s.db.QueryRow(ctx, `
        SELECT COALESCE(SUM(ABS(amount)), 0)
        FROM "token_transactions"
        WHERE "user_id" = $1
          AND type = $2
          AND DATE("created_at") = CURRENT_DATE
    `, userID, TransactionTypeDeduct).Scan(&summary.TodayConsumed); err != nil {
		return summary, fmt.Errorf("failed to query today's consumption: %w", err)
	}

	if err := s.db.QueryRow(ctx, `
        SELECT COALESCE(SUM(ABS(amount)), 0)
        FROM "token_transactions"
        WHERE "user_id" = $1
          AND type = $2
          AND DATE_TRUNC('month', "created_at") = DATE_TRUNC('month', CURRENT_DATE)
    `, userID, TransactionTypeDeduct).Scan(&summary.ThisMonthConsumed); err != nil {
		return summary, fmt.Errorf("failed to query month consumption: %w", err)
	}

	pendingStatuses := []string{"queued", "running"}
	if err := s.db.QueryRow(ctx, `
        SELECT COUNT(*)
        FROM "BatchopenTask"
        WHERE "user_id" = $1
          AND status = ANY($2)
    `, userID, pgtype.FlatArray[string](pendingStatuses)).Scan(&summary.PendingTasksCount); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			summary.PendingTasksCount = 0
		} else if err != pgx.ErrNoRows {
			return summary, fmt.Errorf("failed to query pending tasks: %w", err)
		}
	}

	// 目前暂无精准估算，先使用0占位，保留字段方便后续升级
	summary.EstimatedCostForPending = 0

	// Write to cache (30 second TTL for summary with time-sensitive data)
	if s.cache != nil && s.cache.Ready() {
		if jsonData, err := json.Marshal(summary); err == nil {
			s.cache.Set(ctx, cacheKey, string(jsonData), 30*time.Second)
		}
	}

	return summary, nil
}

// Transaction represents a token transaction
type Transaction struct {
	ID          string                 `json:"id"`
	Type        TransactionType        `json:"type"`
	Amount      int                    `json:"amount"`
	Description string                 `json:"description"`
	CreatedAt   time.Time              `json:"created_at"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ChecksubscriptionsLevel checks if user has required subscription level
func (s *Service) ChecksubscriptionsLevel(ctx context.Context, userID string, requiredPlan string) (bool, string, error) {
	var plan_name string
	var status string

	err := s.db.QueryRow(ctx, `
		SELECT "plan_name", status
		FROM "subscriptions"
		WHERE "user_id" = $1
		  AND status = 'active'
		ORDER BY "created_at" DESC
		LIMIT 1
	`, userID).Scan(&plan_name, &status)

	if err == pgx.ErrNoRows {
		return false, "", fmt.Errorf("no active subscription found")
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to query subscription: %w", err)
	}

	// Plan hierarchy: free < basic < elite
	planLevels := map[string]int{
		"free":  1,
		"basic": 2,
		"elite": 3,
	}

	userLevel := planLevels[plan_name]
	requiredLevel := planLevels[requiredPlan]

	return userLevel >= requiredLevel, plan_name, nil
}

// TokenSource represents the source of tokens
type TokenSource string

const (
	TokenSourcesubscriptions TokenSource = "subscription"
	TokenSourceCheckin      TokenSource = "checkin"
	TokenSourceReferral     TokenSource = "referral"
	TokenSourceManual       TokenSource = "manual"
	TokenSourceAdmin        TokenSource = "admin"
)

// GrantTokensWithSource grants tokens to user with source tracking and optional expiry
func (s *Service) GrantTokensWithSource(ctx context.Context, userID string, amount int, source TokenSource, expiresAt *time.Time, subscriptionID *string, reason string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Add tokens to balance
	_, err = tx.Exec(ctx, `
		UPDATE "user_tokens"
		SET balance = balance + $1,
		    "updated_at" = NOW()
		WHERE "user_id" = $2
	`, amount, userID)
	if err != nil {
		return fmt.Errorf("failed to add tokens: %w", err)
	}

	// 2. Create transaction record with source and expiry
	txID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO "token_transactions" (
			id, "user_id", type, amount, description,
			source, expires_at, subscription_id,
			"created_at", metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), $9)
	`, txID, userID, TransactionTypeGrant, amount, reason,
		string(source), expiresAt, subscriptionID,
		map[string]interface{}{"source": source})

	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate balance cache
	s.invalidateBalanceCache(ctx, userID)

	return nil
}

// DeductionDetail represents details of a token deduction from a specific source
type DeductionDetail struct {
	TransactionID string
	Source        TokenSource
	Amount        int
	ExpiresAt     *time.Time
}

// DeductTokensWithPriority deducts tokens following priority order:
// subscription > checkin > referral
// Within same priority: expiring soon first, then FIFO
func (s *Service) DeductTokensWithPriority(ctx context.Context, userID string, totalAmount int, description string) ([]DeductionDetail, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Check current balance
	var balance int64
	err = tx.QueryRow(ctx, `
		SELECT balance
		FROM "user_tokens"
		WHERE "user_id" = $1
		FOR UPDATE
	`, userID).Scan(&balance)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("user token record not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query balance: %w", err)
	}

	// 2. Check if sufficient balance
	if balance < int64(totalAmount) {
		return nil, fmt.Errorf("insufficient tokens: have %d, need %d", balance, totalAmount)
	}

	// 3. Query available tokens in priority order
	rows, err := tx.Query(ctx, `
		SELECT id, source, amount, expires_at
		FROM "token_transactions"
		WHERE "user_id" = $1
		  AND amount > 0
		  AND type = 'grant'
		  AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY
		  CASE source
		    WHEN 'subscription' THEN 1
		    WHEN 'checkin' THEN 2
		    WHEN 'referral' THEN 3
		    ELSE 4
		  END,
		  expires_at ASC NULLS LAST,
		  "created_at" ASC
		FOR UPDATE
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query available tokens: %w", err)
	}
	defer rows.Close()

	// 4. Deduct from tokens in priority order
	remainingToDeduct := totalAmount
	var deductions []DeductionDetail

	for rows.Next() && remainingToDeduct > 0 {
		var txID string
		var source string
		var availableAmount int
		var expiresAt *time.Time

		if err := rows.Scan(&txID, &source, &availableAmount, &expiresAt); err != nil {
			return nil, fmt.Errorf("failed to scan token: %w", err)
		}

		// Calculate how much to deduct from this token source
		amountToDeduct := remainingToDeduct
		if availableAmount < amountToDeduct {
			amountToDeduct = availableAmount
		}

		// Update the grant transaction (reduce its amount)
		_, err = tx.Exec(ctx, `
			UPDATE "token_transactions"
			SET amount = amount - $1
			WHERE id = $2
		`, amountToDeduct, txID)
		if err != nil {
			return nil, fmt.Errorf("failed to deduct from token source: %w", err)
		}

		// Record the deduction
		deductions = append(deductions, DeductionDetail{
			TransactionID: txID,
			Source:        TokenSource(source),
			Amount:        amountToDeduct,
			ExpiresAt:     expiresAt,
		})

		remainingToDeduct -= amountToDeduct
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tokens: %w", err)
	}

	// 5. Create deduction transaction record
	deductID := uuid.New().String()
	deductionsJSON, _ := json.Marshal(deductions)
	_, err = tx.Exec(ctx, `
		INSERT INTO "token_transactions" (
			id, "user_id", type, amount, description,
			"created_at", metadata
		) VALUES ($1, $2, $3, $4, $5, NOW(), $6)
	`, deductID, userID, TransactionTypeDeduct, -totalAmount, description,
		map[string]interface{}{
			"deductions": string(deductionsJSON),
			"status":     "confirmed",
		})
	if err != nil {
		return nil, fmt.Errorf("failed to create deduction transaction: %w", err)
	}

	// 6. Update balance
	_, err = tx.Exec(ctx, `
		UPDATE "user_tokens"
		SET balance = balance - $1,
		    "updated_at" = NOW()
		WHERE "user_id" = $2
	`, totalAmount, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to update balance: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate balance cache
	s.invalidateBalanceCache(ctx, userID)

	return deductions, nil
}
