//go:build migration || trial_migration
// +build migration trial_migration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	// Check if this is a trial migration
	if os.Getenv("TRIAL_MIGRATION") == "true" {
		log.Printf("Running trial subscriptions migration...")
		if err := runTrialMigration(ctx, databaseURL); err != nil {
			log.Fatalf("Trial migration failed: %v", err)
		}
		log.Printf("✅ Trial migration completed successfully")
		return
	}

	migrationsDir := strings.TrimSpace(os.Getenv("MIGRATIONS_DIR"))
	if migrationsDir == "" {
		migrationsDir = "internal/migrations"
	}

	log.Printf("Billing DB Migrator starting...")
	log.Printf("Migrations directory: %s", migrationsDir)

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Wait for database readiness (max 60s)
	retries := 12
	for i := 0; i < retries; i++ {
		if err := db.PingContext(ctx); err == nil {
			log.Printf("Database connection established")
			break
		}
		if i == retries-1 {
			log.Fatalf("Database not ready after %d attempts", retries)
		}
		log.Printf("Waiting for database... (attempt %d/%d)", i+1, retries)
		time.Sleep(5 * time.Second)
	}

	// Run migrations in transaction
	if err := runMigrations(ctx, db, migrationsDir); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	log.Printf("✅ All migrations completed successfully")
}

func runMigrations(ctx context.Context, db *sql.DB, dir string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			log.Printf("WARN: failed to rollback: %v", err)
		}
	}()

	// Create migration tracking table
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	// Get applied migrations
	applied := make(map[string]bool)
	rows, err := tx.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return fmt.Errorf("scan version: %w", err)
		}
		applied[version] = true
	}
	rows.Close()

	// Read migration files
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("No migrations directory found at %s, skipping", dir)
			return tx.Commit()
		}
		return fmt.Errorf("read migrations directory: %w", err)
	}

	// Filter and sort .sql files (exclude hidden files)
	var migrations []string
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || !strings.HasSuffix(name, ".sql") || strings.HasPrefix(name, ".") {
			continue
		}
		migrations = append(migrations, name)
	}
	sort.Strings(migrations)

	// Apply migrations
	for _, filename := range migrations {
		if applied[filename] {
			log.Printf("⏭️  Skipping already applied: %s", filename)
			continue
		}

		log.Printf("📝 Applying migration: %s", filename)
		content, err := os.ReadFile(filepath.Join(dir, filename))
		if err != nil {
			return fmt.Errorf("read migration file %s: %w", filename, err)
		}

		// Split by semicolon and execute each statement
		statements := strings.Split(string(content), ";")
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("execute migration %s: %w", filename, err)
			}
		}

		// Record migration
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO schema_migrations (version) VALUES ($1)
		`, filename); err != nil {
			return fmt.Errorf("record migration %s: %w", filename, err)
		}

		log.Printf("✅ Applied: %s", filename)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// runTrialMigration executes trial subscriptions migration
func runTrialMigration(ctx context.Context, databaseURL string) error {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize migration
	migrator := &TrialsubscriptionssMigrator{db: db}

	// Execute migration
	return migrator.Migrate(ctx)
}

// TrialsubscriptionssMigrator handles migration of trial subscriptions data
type TrialsubscriptionssMigrator struct {
	db *sql.DB
}

// MigrationReport contains the results of the migration
type MigrationReport struct {
	TotalRecords     int       `json:"total_records"`
	SuccessCount     int       `json:"success_count"`
	FailureCount     int       `json:"failure_count"`
	SkippedCount     int       `json:"skipped_count"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	Duration         string    `json:"duration"`
	SourceTable      string    `json:"source_table"`
	DestinationTable string    `json:"destination_table"`
}

// Migrate executes the trial subscriptions migration
func (m *TrialsubscriptionssMigrator) Migrate(ctx context.Context) error {
	report := &MigrationReport{
		StartTime:        time.Now(),
		SourceTable:      "trial_subscriptions",
		DestinationTable: "subscriptions",
	}

	log.Println("Starting trial subscriptions migration...")

	// Check if source table exists
	var sourceExists bool
	err := m.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'trial_subscriptions'
		)
	`).Scan(&sourceExists)
	if err != nil {
		return fmt.Errorf("failed to check source table: %w", err)
	}

	if !sourceExists {
		log.Println("Warning: trial_subscriptions table does not exist. Migration not needed.")
		return nil
	}

	// Check if destination table exists
	var destExists bool
	err = m.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'subscriptions'
		)
	`).Scan(&destExists)
	if err != nil {
		return fmt.Errorf("failed to check destination table: %w", err)
	}

	if !destExists {
		return fmt.Errorf("subscriptions table does not exist. Please run billing service migrations first.")
	}

	// Create backup table
	backupTable := fmt.Sprintf("trial_subscriptions_backup_%s", time.Now().Format("20060102_150405"))
	_, err = m.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s AS SELECT * FROM trial_subscriptions
	`, backupTable))
	if err != nil {
		return fmt.Errorf("failed to create backup table: %w", err)
	}
	log.Printf("✓ Backup created: %s", backupTable)

	// Get count of records to migrate
	var totalRecords int
	err = m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM trial_subscriptions").Scan(&totalRecords)
	if err != nil {
		return fmt.Errorf("failed to count source records: %w", err)
	}
	report.TotalRecords = totalRecords
	log.Printf("Total records to migrate: %d", totalRecords)

	if totalRecords == 0 {
		log.Println("No records to migrate")
		return nil
	}

	// Perform migration
	err = m.migrateData(ctx, report)
	if err != nil {
		return fmt.Errorf("data migration failed: %w", err)
	}

	report.EndTime = time.Now()
	report.Duration = report.EndTime.Sub(report.StartTime).String()

	// Print report
	m.printReport(report)

	return nil
}

// migrateData performs the actual data migration
func (m *TrialsubscriptionssMigrator) migrateData(ctx context.Context, report *MigrationReport) error {
	// Query source data
	rows, err := m.db.QueryContext(ctx, `
		SELECT
			id,
			"user_id",
			"planTier",
			"startDate",
			"endDate",
			"isActive",
			COALESCE(source, 'self_register') as source,
			"created_at"
		FROM trial_subscriptions
		ORDER BY "created_at" ASC
	`)
	if err != nil {
		return fmt.Errorf("failed to query source table: %w", err)
	}
	defer rows.Close()

	// Process each record
	for rows.Next() {
		var id, userID, planTier, source string
		var startDate, endDate, created_at time.Time
		var isActive bool

		err := rows.Scan(
			&id,
			&userID,
			&planTier,
			&startDate,
			&endDate,
			&isActive,
			&source,
			&created_at,
		)
		if err != nil {
			log.Printf("Warning: Failed to scan row: %v", err)
			report.FailureCount++
			continue
		}

		// Check if record already exists in destination
		var exists bool
		err = m.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM subscriptions
				WHERE id = $1
			)
		`, id).Scan(&exists)
		if err != nil {
			log.Printf("Warning: Failed to check existing record for ID %s: %v", id, err)
			report.FailureCount++
			continue
		}

		if exists {
			log.Printf("  Skipping ID %s (already exists)", id)
			report.SkippedCount++
			continue
		}

		// Convert data format
		plan := m.convertPlanTier(planTier)
		status := m.determineStatus(isActive, endDate)
		trialSource := m.convertTrialSource(source)

		// Insert into destination table
		_, err = m.db.ExecContext(ctx, `
			INSERT INTO subscriptions (
				id,
				"user_id",
				plan,
				status,
				"trialStartDate",
				"trialEndDate",
				"trialSource",
				"created_at",
				"updated_at"
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		`,
			id,
			userID,
			plan,
			status,
			startDate,
			endDate,
			trialSource,
			created_at,
		)

		if err != nil {
			log.Printf("  ✗ Failed to migrate ID %s: %v", id, err)
			report.FailureCount++
		} else {
			log.Printf("  ✓ Migrated ID %s (user: %s, plan: %s, status: %s)", id, userID, plan, status)
			report.SuccessCount++
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	return nil
}

// convertPlanTier converts old plan tier format to new format
func (m *TrialsubscriptionssMigrator) convertPlanTier(oldTier string) string {
	switch oldTier {
	case "pro", "professional":
		return "professional"
	case "elite", "max":
		return "elite"
	case "starter", "free":
		return "starter"
	default:
		return "professional" // Default to professional for trial subscriptions
	}
}

// determineStatus determines the subscription status
func (m *TrialsubscriptionssMigrator) determineStatus(isActive bool, endDate time.Time) string {
	if !isActive {
		return "expired"
	}
	if time.Now().After(endDate) {
		return "expired"
	}
	return "trial"
}

// convertTrialSource converts trial source format
func (m *TrialsubscriptionssMigrator) convertTrialSource(source string) string {
	switch source {
	case "self_register", "registration":
		return "self_register"
	case "referral_inviter", "inviter":
		return "referral_inviter"
	case "referral_invitee", "invitee":
		return "referral_invitee"
	default:
		return "self_register"
	}
}

// printReport prints the migration report
func (m *TrialsubscriptionssMigrator) printReport(report *MigrationReport) {
	log.Println("=== Migration Report ===")
	log.Printf("Source Table: %s", report.SourceTable)
	log.Printf("Destination Table: %s", report.DestinationTable)
	log.Printf("Start Time: %s", report.StartTime.Format(time.RFC3339))
	log.Printf("End Time: %s", report.EndTime.Format(time.RFC3339))
	log.Printf("Duration: %s", report.Duration)
	log.Println()
	log.Printf("Total Records: %d", report.TotalRecords)
	log.Printf("Successfully Migrated: %d", report.SuccessCount)
	log.Printf("Skipped (Already Exists): %d", report.SkippedCount)
	log.Printf("Failed: %d", report.FailureCount)

	successRate := float64(report.SuccessCount) / float64(report.TotalRecords) * 100
	log.Printf("Success Rate: %.2f%%", successRate)
}
