package testutil

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDB represents a test database connection
type TestDB struct {
	Pool *pgxpool.Pool
	t    *testing.T
}

// NewTestDB creates a new test database connection
func NewTestDB(t *testing.T) *TestDB {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		// Default test database URL
		dbURL = "postgresql://postgres:postgres@localhost:5432/billing_test?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Ping to verify connection
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("Failed to ping test database: %v", err)
	}

	return &TestDB{
		Pool: pool,
		t:    t,
	}
}

// Close closes the test database connection
func (tdb *TestDB) Close() {
	tdb.Pool.Close()
}

// Cleanup cleans up test data from the database
func (tdb *TestDB) Cleanup() {
	ctx := context.Background()

	// Clean up test tables in reverse order of dependencies
	tables := []string{
		"token_transactions",
		"user_tokens",
		"subscriptions",
		"User",
	}

	for _, table := range tables {
		query := fmt.Sprintf(`DELETE FROM "%s" WHERE "user_id" LIKE 'test-%%' OR id LIKE 'test-%%'`, table)
		if _, err := tdb.Pool.Exec(ctx, query); err != nil {
			tdb.t.Logf("Warning: Failed to clean up table %s: %v", table, err)
		}
	}
}

// SetupTestTables creates test tables if they don't exist
func (tdb *TestDB) SetupTestTables() {
	ctx := context.Background()

	// Create User table
	userTable := `
	CREATE TABLE IF NOT EXISTS "User" (
		id VARCHAR(255) PRIMARY KEY,
		email VARCHAR(255) UNIQUE NOT NULL,
		"created_at" TIMESTAMP NOT NULL DEFAULT NOW(),
		"updated_at" TIMESTAMP NOT NULL DEFAULT NOW()
	);
	`
	if _, err := tdb.Pool.Exec(ctx, userTable); err != nil {
		tdb.t.Fatalf("Failed to create User table: %v", err)
	}

	// Create user_tokens table
	userTokenTable := `
	CREATE TABLE IF NOT EXISTS "user_tokens" (
		"user_id" VARCHAR(255) PRIMARY KEY,
		balance BIGINT NOT NULL DEFAULT 0,
		"created_at" TIMESTAMP NOT NULL DEFAULT NOW(),
		"updated_at" TIMESTAMP NOT NULL DEFAULT NOW(),
		FOREIGN KEY ("user_id") REFERENCES "User"(id) ON DELETE CASCADE
	);
	`
	if _, err := tdb.Pool.Exec(ctx, userTokenTable); err != nil {
		tdb.t.Fatalf("Failed to create user_tokens table: %v", err)
	}

	// Create token_transactions table
	tokenTransactionTable := `
	CREATE TABLE IF NOT EXISTS "token_transactions" (
		id VARCHAR(255) PRIMARY KEY,
		"user_id" VARCHAR(255) NOT NULL,
		type VARCHAR(50) NOT NULL,
		amount BIGINT NOT NULL,
		description TEXT,
		metadata JSONB,
		"created_at" TIMESTAMP NOT NULL DEFAULT NOW(),
		FOREIGN KEY ("user_id") REFERENCES "User"(id) ON DELETE CASCADE
	);
	`
	if _, err := tdb.Pool.Exec(ctx, tokenTransactionTable); err != nil {
		tdb.t.Fatalf("Failed to create token_transactions table: %v", err)
	}

	// Create subscriptions table
	subscriptionTable := `
	CREATE TABLE IF NOT EXISTS "subscriptions" (
		id VARCHAR(255) PRIMARY KEY,
		"user_id" VARCHAR(255) NOT NULL,
		"planId" VARCHAR(255) NOT NULL,
		status VARCHAR(50) NOT NULL,
		"created_at" TIMESTAMP NOT NULL DEFAULT NOW(),
		"updated_at" TIMESTAMP NOT NULL DEFAULT NOW(),
		FOREIGN KEY ("user_id") REFERENCES "User"(id) ON DELETE CASCADE
	);
	`
	if _, err := tdb.Pool.Exec(ctx, subscriptionTable); err != nil {
		tdb.t.Fatalf("Failed to create subscriptions table: %v", err)
	}
}

// TeardownTestTables drops test tables
func (tdb *TestDB) TeardownTestTables() {
	ctx := context.Background()

	tables := []string{
		"token_transactions",
		"subscriptions",
		"user_tokens",
		"User",
	}

	for _, table := range tables {
		query := fmt.Sprintf(`DROP TABLE IF EXISTS "%s" CASCADE`, table)
		if _, err := tdb.Pool.Exec(ctx, query); err != nil {
			tdb.t.Logf("Warning: Failed to drop table %s: %v", table, err)
		}
	}
}

// InsertTestUser inserts a test user
func (tdb *TestDB) InsertTestUser(ctx context.Context, userID, email string) error {
	query := `
		INSERT INTO "User" (id, email, "created_at", "updated_at")
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`
	_, err := tdb.Pool.Exec(ctx, query, userID, email)
	return err
}

// InsertTestuser_tokens inserts a test user token
func (tdb *TestDB) InsertTestuser_tokens(ctx context.Context, token *Testuser_tokens) error {
	// Ensure user exists
	if err := tdb.InsertTestUser(ctx, token.UserID, token.UserID+"@test.com"); err != nil {
		return err
	}

	query := `
		INSERT INTO "user_tokens" ("user_id", balance, "created_at", "updated_at")
		VALUES ($1, $2, $3, $4)
		ON CONFLICT ("user_id") DO UPDATE
		SET balance = $2, "updated_at" = $4
	`
	_, err := tdb.Pool.Exec(ctx, query,
		token.UserID,
		token.Balance,
		token.CreatedAt,
		token.UpdatedAt,
	)
	return err
}

// GetTestuser_tokens retrieves a test user token
func (tdb *TestDB) GetTestuser_tokens(ctx context.Context, userID string) (*Testuser_tokens, error) {
	query := `
		SELECT "user_id", balance, "created_at", "updated_at"
		FROM "user_tokens"
		WHERE "user_id" = $1
	`

	token := &Testuser_tokens{}
	err := tdb.Pool.QueryRow(ctx, query, userID).Scan(
		&token.UserID,
		&token.Balance,
		&token.CreatedAt,
		&token.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return token, nil
}

// InsertTestTransaction inserts a test transaction
func (tdb *TestDB) InsertTestTransaction(ctx context.Context, tx *Testtoken_transactions) error {
	// Ensure user exists
	if err := tdb.InsertTestUser(ctx, tx.UserID, tx.UserID+"@test.com"); err != nil {
		return err
	}

	query := `
		INSERT INTO "token_transactions" (id, "user_id", type, amount, description, metadata, "created_at")
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	metadata := map[string]interface{}{"status": tx.Status}
	_, err := tdb.Pool.Exec(ctx, query,
		tx.ID,
		tx.UserID,
		tx.Type,
		tx.Amount,
		tx.Description,
		metadata,
		tx.CreatedAt,
	)
	return err
}

// GetTestTransaction retrieves a test transaction
func (tdb *TestDB) GetTestTransaction(ctx context.Context, txID string) (*Testtoken_transactions, error) {
	query := `
		SELECT id, "user_id", type, amount, description, "created_at"
		FROM "token_transactions"
		WHERE id = $1
	`

	tx := &Testtoken_transactions{}
	err := tdb.Pool.QueryRow(ctx, query, txID).Scan(
		&tx.ID,
		&tx.UserID,
		&tx.Type,
		&tx.Amount,
		&tx.Description,
		&tx.CreatedAt,
	)

	if err != nil {
		return nil, err
	}

	return tx, nil
}
