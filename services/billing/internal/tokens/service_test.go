package tokens

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ScientificInternet/Google-Monetize/pkg/cache"
)

// TestService_CheckAndReserveTokens tests the token reservation functionality
func TestService_CheckAndReserveTokens(t *testing.T) {
	// Skip if no test database available
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	testCache := &cache.Cache{}
	service := NewService(db, testCache)

	t.Run("successful reservation with sufficient balance", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 1000)

		// Act
		reservationID, err := service.CheckAndReserveTokens(ctx, userID, 100, "Test reservation")

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, reservationID)

		// Verify balance was deducted
		balance, err := service.GetBalance(ctx, userID)
		require.NoError(t, err)
		assert.Equal(t, int64(900), balance)
	})

	t.Run("fail with insufficient balance", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 50)

		// Act
		reservationID, err := service.CheckAndReserveTokens(ctx, userID, 100, "Test reservation")

		// Assert
		require.Error(t, err)
		assert.Empty(t, reservationID)
		assert.Contains(t, err.Error(), "insufficient tokens")

		// Verify balance unchanged
		balance, err := service.GetBalance(ctx, userID)
		require.NoError(t, err)
		assert.Equal(t, int64(50), balance)
	})

	t.Run("fail with non-existent user", func(t *testing.T) {
		// Act
		reservationID, err := service.CheckAndReserveTokens(ctx, "non-existent-user", 100, "Test")

		// Assert
		require.Error(t, err)
		assert.Empty(t, reservationID)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("exact balance reservation", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 100)

		// Act
		reservationID, err := service.CheckAndReserveTokens(ctx, userID, 100, "Exact balance")

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, reservationID)

		// Verify balance is zero
		balance, err := service.GetBalance(ctx, userID)
		require.NoError(t, err)
		assert.Equal(t, int64(0), balance)
	})
}

// TestService_ConfirmTokenDeduction tests confirming a reserved deduction
func TestService_ConfirmTokenDeduction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	testCache := &cache.Cache{}
	service := NewService(db, testCache)

	t.Run("successful confirmation", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 1000)
		reservationID, err := service.CheckAndReserveTokens(ctx, userID, 100, "Test")
		require.NoError(t, err)

		// Act
		err = service.ConfirmTokenDeduction(ctx, reservationID)

		// Assert
		require.NoError(t, err)

		// Verify transaction status updated
		tx := getTransaction(t, db, reservationID)
		assert.Contains(t, tx.Metadata, "status")
		assert.Equal(t, "confirmed", tx.Metadata["status"])
	})

	t.Run("confirm non-existent reservation", func(t *testing.T) {
		// Act
		err := service.ConfirmTokenDeduction(ctx, "non-existent-id")

		// Assert - should not error, just no-op
		require.NoError(t, err)
	})
}

// TestService_RefundTokens tests the token refund functionality
func TestService_RefundTokens(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	testCache := &cache.Cache{}
	service := NewService(db, testCache)

	t.Run("successful refund", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 1000)
		reservationID, err := service.CheckAndReserveTokens(ctx, userID, 100, "Test")
		require.NoError(t, err)

		// Verify balance after reservation
		balance, _ := service.GetBalance(ctx, userID)
		assert.Equal(t, int64(900), balance)

		// Act
		err = service.RefundTokens(ctx, userID, reservationID, 100, "Task failed")

		// Assert
		require.NoError(t, err)

		// Verify balance restored
		balance, err = service.GetBalance(ctx, userID)
		require.NoError(t, err)
		assert.Equal(t, int64(1000), balance)

		// Verify refund transaction created
		history, err := service.GetTransactionHistory(ctx, userID, 10)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(history), 2)

		// Find refund transaction
		var refundTx *Transaction
		for i := range history {
			if history[i].Type == TransactionTypeRefund {
				refundTx = &history[i]
				break
			}
		}
		require.NotNil(t, refundTx)
		assert.Equal(t, 100, refundTx.Amount)
		assert.Contains(t, refundTx.Metadata, "reason")
	})

	t.Run("partial refund", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 1000)
		reservationID, err := service.CheckAndReserveTokens(ctx, userID, 100, "Test")
		require.NoError(t, err)

		// Act - refund only 50 tokens
		err = service.RefundTokens(ctx, userID, reservationID, 50, "Partial completion")

		// Assert
		require.NoError(t, err)

		// Verify balance
		balance, err := service.GetBalance(ctx, userID)
		require.NoError(t, err)
		assert.Equal(t, int64(950), balance) // 1000 - 100 + 50
	})
}

// TestService_GetBalance tests balance retrieval
func TestService_GetBalance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	testCache := &cache.Cache{}
	service := NewService(db, testCache)

	t.Run("get existing balance", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 1500)

		// Act
		balance, err := service.GetBalance(ctx, userID)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, int64(1500), balance)
	})

	t.Run("get non-existent user balance", func(t *testing.T) {
		// Act
		balance, err := service.GetBalance(ctx, "non-existent-user")

		// Assert
		require.Error(t, err)
		assert.Equal(t, int64(0), balance)
		assert.Contains(t, err.Error(), "not found")
	})
}

// TestService_GetTransactionHistory tests transaction history retrieval
func TestService_GetTransactionHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	testCache := &cache.Cache{}
	service := NewService(db, testCache)

	t.Run("get transaction history", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 1000)

		// Create multiple transactions
		_, err := service.CheckAndReserveTokens(ctx, userID, 100, "Transaction 1")
		require.NoError(t, err)
		_, err = service.CheckAndReserveTokens(ctx, userID, 50, "Transaction 2")
		require.NoError(t, err)

		// Act
		history, err := service.GetTransactionHistory(ctx, userID, 10)

		// Assert
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(history), 2)

		// Verify transactions are ordered by creation time (newest first)
		if len(history) >= 2 {
			assert.True(t, history[0].CreatedAt.After(history[1].CreatedAt) ||
				history[0].CreatedAt.Equal(history[1].CreatedAt))
		}
	})

	t.Run("limit transaction history", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 1000)

		// Create 5 transactions
		for i := 0; i < 5; i++ {
			_, err := service.CheckAndReserveTokens(ctx, userID, 10, "Transaction")
			require.NoError(t, err)
		}

		// Act - limit to 3
		history, err := service.GetTransactionHistory(ctx, userID, 3)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, 3, len(history))
	})

	t.Run("empty history for new user", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 1000)

		// Act
		history, err := service.GetTransactionHistory(ctx, userID, 10)

		// Assert
		require.NoError(t, err)
		assert.Empty(t, history)
	})
}

// TestService_GetBalanceSummary tests balance summary retrieval
func TestService_GetBalanceSummary(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	testCache := &cache.Cache{}
	service := NewService(db, testCache)

	t.Run("get balance summary", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 1000)

		// Create some transactions
		_, err := service.CheckAndReserveTokens(ctx, userID, 100, "Today's transaction")
		require.NoError(t, err)

		// Act
		summary, err := service.GetBalanceSummary(ctx, userID)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, int64(900), summary.Balance)
		assert.Equal(t, int64(900), summary.TotalBalance)
		assert.Equal(t, int64(100), summary.TodayConsumed)
		assert.NotNil(t, summary.UpdatedAt)
	})

	t.Run("summary for non-existent user", func(t *testing.T) {
		// Act
		summary, err := service.GetBalanceSummary(ctx, "non-existent-user")

		// Assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.Equal(t, int64(0), summary.Balance)
	})
}

// TestService_ChecksubscriptionsLevel tests subscription level checking
func TestService_ChecksubscriptionsLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	testCache := &cache.Cache{}
	service := NewService(db, testCache)

	t.Run("check subscription level - sufficient", func(t *testing.T) {
		// Arrange
		userID := createTestUserWithsubscriptions(t, db, "elite")

		// Act
		hasAccess, plan_name, err := service.ChecksubscriptionsLevel(ctx, userID, "basic")

		// Assert
		require.NoError(t, err)
		assert.True(t, hasAccess)
		assert.Equal(t, "elite", plan_name)
	})

	t.Run("check subscription level - insufficient", func(t *testing.T) {
		// Arrange
		userID := createTestUserWithsubscriptions(t, db, "basic")

		// Act
		hasAccess, plan_name, err := service.ChecksubscriptionsLevel(ctx, userID, "elite")

		// Assert
		require.NoError(t, err)
		assert.False(t, hasAccess)
		assert.Equal(t, "basic", plan_name)
	})

	t.Run("check subscription level - no subscription", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 1000)

		// Act
		hasAccess, plan_name, err := service.ChecksubscriptionsLevel(ctx, userID, "basic")

		// Assert
		require.Error(t, err)
		assert.False(t, hasAccess)
		assert.Empty(t, plan_name)
		assert.Contains(t, err.Error(), "no active subscription")
	})
}

// TestService_ConcurrentReservations tests concurrent token reservations
func TestService_ConcurrentReservations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	testCache := &cache.Cache{}
	service := NewService(db, testCache)

	t.Run("concurrent reservations maintain consistency", func(t *testing.T) {
		// Arrange
		userID := createTestUser(t, db, 1000)

		// Act - try to reserve 100 tokens 15 times concurrently
		// Only 10 should succeed
		results := make(chan error, 15)
		for i := 0; i < 15; i++ {
			go func() {
				_, err := service.CheckAndReserveTokens(ctx, userID, 100, "Concurrent test")
				results <- err
			}()
		}

		// Collect results
		var successCount, failCount int
		for i := 0; i < 15; i++ {
			err := <-results
			if err == nil {
				successCount++
			} else {
				failCount++
			}
		}

		// Assert
		assert.Equal(t, 10, successCount, "Expected exactly 10 successful reservations")
		assert.Equal(t, 5, failCount, "Expected 5 failed reservations")

		// Verify final balance
		balance, err := service.GetBalance(ctx, userID)
		require.NoError(t, err)
		assert.Equal(t, int64(0), balance, "Balance should be zero after all successful reservations")
	})
}

// Helper functions

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	// Use test database URL from environment or default
	dbURL := getTestDatabaseURL()

	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Skipf("Skipping test: cannot parse database URL: %v", err)
	}

	db, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to database: %v", err)
	}

	// Verify connection
	err = db.Ping(context.Background())
	if err != nil {
		db.Close()
		t.Skipf("Skipping test: database not available: %v", err)
	}

	return db
}

func cleanupTestDB(t *testing.T, db *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()

	// Clean up test data
	_, _ = db.Exec(ctx, `DELETE FROM "token_transactions" WHERE "user_id" LIKE 'test-%'`)
	_, _ = db.Exec(ctx, `DELETE FROM "subscriptions" WHERE "user_id" LIKE 'test-%'`)
	_, _ = db.Exec(ctx, `DELETE FROM "user_tokens" WHERE "user_id" LIKE 'test-%'`)
	_, _ = db.Exec(ctx, `DELETE FROM "User" WHERE id LIKE 'test-%'`)

	db.Close()
}

func createTestUser(t *testing.T, db *pgxpool.Pool, initialBalance int64) string {
	t.Helper()

	ctx := context.Background()
	userID := "test-" + time.Now().Format("20060102150405") + "-" + randomString(8)

	// Create user
	_, err := db.Exec(ctx, `
		INSERT INTO "User" (id, email, name, "created_at", "updated_at")
		VALUES ($1, $2, $3, NOW(), NOW())
	`, userID, userID+"@test.com", "Test User")
	require.NoError(t, err)

	// Create user token
	_, err = db.Exec(ctx, `
		INSERT INTO "user_tokens" ("user_id", balance, "created_at", "updated_at")
		VALUES ($1, $2, NOW(), NOW())
	`, userID, initialBalance)
	require.NoError(t, err)

	return userID
}

func createTestUserWithsubscriptions(t *testing.T, db *pgxpool.Pool, plan_name string) string {
	t.Helper()

	userID := createTestUser(t, db, 1000)

	ctx := context.Background()
	_, err := db.Exec(ctx, `
		INSERT INTO "subscriptions" (
			id, "user_id", "plan_name", status,
			"currentPeriodStart", "currentPeriodEnd",
			"created_at", "updated_at"
		) VALUES ($1, $2, $3, $4, NOW(), NOW() + INTERVAL '30 days', NOW(), NOW())
	`, "sub-"+userID, userID, plan_name, "active")
	require.NoError(t, err)

	return userID
}

func getTransaction(t *testing.T, db *pgxpool.Pool, txID string) Transaction {
	t.Helper()

	ctx := context.Background()
	var tx Transaction
	err := db.QueryRow(ctx, `
		SELECT id, type, amount, description, "created_at", metadata
		FROM "token_transactions"
		WHERE id = $1
	`, txID).Scan(&tx.ID, &tx.Type, &tx.Amount, &tx.Description, &tx.CreatedAt, &tx.Metadata)
	require.NoError(t, err)

	return tx
}

func getTestDatabaseURL() string {
	// Try environment variable first
	if url := getEnv("TEST_DATABASE_URL"); url != "" {
		return url
	}

	// Default test database
	return "postgresql://postgres:postgres@localhost:5432/billing_test?sslmode=disable"
}

func getEnv(key string) string {
	// This would normally use os.Getenv, but for testing we can provide a default
	return ""
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}
