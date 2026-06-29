package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/supabase/supabase-go"
	"github.com/ScientificInternet/Google-Monetize/pkg/database"
)

// MigrationStats tracks the migration process
type MigrationStats struct {
	TotalUsers      int    `json:"total_users"`
	MigratedUsers   int    `json:"migrated_users"`
	SkippedUsers   int    `json:"skipped_users"`
	ErrorCount     int    `json:"error_count"`
	Status          string `json:"status"`
	Timestamp      string `json:"timestamp"`
	SupabaseURL    string `json:"supabase_url"`
	ServiceRoleKey  string `json:"service_role_key_used"`
}

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Error loading environment variables: %v", err)
	}

	supabaseURL := os.Getenv("SUPABASE_URL")
	if supabaseURL == "" {
		log.Fatalf("SUPABASE_URL environment variable is required")
	}

	supabaseServiceRoleKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	if supabaseServiceRoleKey == "" {
		log.Fatalf("SUPABASE_SERVICE_ROLE_KEY environment variable is required")
	}

	// Database connection
	// We'll use the database adapter to connect to Cloud SQL
	adapter, err := database.GetAdapterForService("user-migration")
	if err != nil {
		log.Fatalf("Failed to create database adapter: %v", err)
	}
	defer adapter.Close()

	ctx := context.Background()

	log.Println("🚀 Starting user data migration from Supabase to Cloud SQL")
	log.Printf("Supabase URL: %s", supabaseURL)
	log.Printf("Service Role Key: %s", supabaseServiceRoleKey)

	// Initialize Supabase client
	supabaseClient, err := supabase.NewClient(supabaseURL, supabaseServiceRoleKey, nil)
	if err != nil {
		log.Fatalf("Failed to create Supabase client: %v", err)
	}

	// Get migration stats start time
	startTime := time.Now()

	// 1. Get all users from Supabase auth.users
	log.Println("📥 Step 1: Fetching users from Supabase auth.users...")

	// Type definition for Supabase auth user
	type SupabaseUser struct {
		ID           string     `json:"id"`
		Email        string     `json:"email"`
		Phone        string     `json:"phone"`
		EmailVerified *string `json:"email_confirmed_at"`
		PhoneVerified *string `json:"phone_confirmed_at"`
		CreatedAt     *string `json:"created_at"`
		UpdatedAt     *string `json:"updated_at"`
		LastSignInAt *string `json:"last_sign_in_at"`
		UserMetadata  any     `json:"user_metadata"`
		AppMetadata  any     `json:"app_metadata"`
	}

	var users []SupabaseUser
	{
		// Use pagination for large datasets
		pageSize := 1000
		page := 0
		hasMore := true

		for hasMore {
			var pageUsers []SupabaseUser
			usersResp, err := supabaseClient.Auth.From("users").
				Select("*").
				Order("created_at", "asc").
				Limit(pageSize).
				Offset(page * pageSize).
				Execute(ctx)

			if err != nil {
				log.Printf("Error fetching users from Supabase: %v", err)
				ErrorCount++
				continue
			}

			if len(usersResp.Error) > 0 {
				log.Printf("Supabase API error: %v", usersResp.Error[0])
				ErrorCount++
			}

			if len(usersResp.Data) == 0 {
				hasMore = false
				break
			}

			users = append(users, pageUsers...)
			page++
		}
	}

	log.Printf("✅ Fetched %d users from Supabase", len(users))

	// 2. Filter out already existing users
	log.Println("📥 Step 2: Filtering out existing users...")

	// Build list of existing user IDs
	existingUsers := make(map[string]bool)
	getExistingUsersSQL := `SELECT id FROM user.users WHERE deleted_at IS NULL`

	rows, err := adapter.Query(ctx, getExistingUsersSQL)
	if err != nil {
		log.Printf("Error fetching existing users from Cloud SQL: %v", err)
		ErrorCount++
	} else {
		defer rows.Close()
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				log.Printf("Error scanning existing user ID: %v", err)
				ErrorCount++
				continue
			}
			existingUsers[userID] = true
		}
	}

	log.Printf("✅ Found %d existing users in Cloud SQL", len(existingUsers))

	// 3. Migrate users to Cloud SQL
	log.Println("📥 Step 3: Migrating new users to Cloud SQL...")

	migratedCount := 0
	skippedCount := 0

	// Prepare batch insert statement
	insertSQL := `
		INSERT INTO user.users (
			id, email, display_name, photo_url, status,
			email_verified, phone_verified, created_at, updated_at,
			language, timezone, preferences, metadata
		) VALUES ($1, $2, $3, $4, 'active', $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (id) DO NOTHING
	`

	// Use transaction for batch insert
	tx, err := adapter.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("Error beginning transaction: %v", err)
		ErrorCount++
		return
	}
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic during migration: %v", r)
			ErrorCount++
		}

		// Rollback on error
		if err != nil {
			tx.Rollback()
			log.Printf("Transaction rolled back due to error: %v", err)
		} else {
			tx.Commit()
		}
	}()

	// Process users in batches
	for i := 0; i < len(users); i += 100 {
		end := i + 100
		if end >= len(users) {
			end = len(users)
		}

		batch := users[i:end]

		// Process each user in batch
		for _, user := range batch {
			// Skip if already exists
			if existingUsers[user.ID] {
				skippedCount++
				continue
			}

			// Prepare metadata JSONB
			metadataJSON, err := json.Marshal(user.UserMetadata)
			if err != nil {
				log.Printf("Error marshaling user metadata for user %s: %v", user.ID, err)
				ErrorCount++
				continue
			}

			preferencesJSON, err := json.Marshal(map[string]interface{}{})
			if err != nil {
				log.Printf("Error marshaling user preferences for user %s: %v", user.ID, err)
				ErrorCount++
				continue
			}

			// Handle NULL values
			var emailVerified, phoneVerified *bool
			var emailConfirmedAt, phoneConfirmedAt, lastSignInAt *string
			if user.EmailVerified != nil {
				emailVerified = new(bool, *user.EmailVerified)
				emailConfirmedAt = new(string, *user.EmailConfirmedAt)
			}
			if user.PhoneVerified != nil {
				phoneVerified = new(bool, *user.PhoneVerified)
				phoneConfirmedAt = new(string, *user.PhoneConfirmedAt)
			}
			if user.LastSignInAt != nil {
				lastSignInAt = new(string, *user.LastSignInAt)
			}

			// Convert timestamp to UTC format
			createdAt := time.Now().UTC().Format(time.RFC3339)
			updatedAt := createdAt

			// Execute insert
			_, err := tx.ExecContext(ctx, insertSQL,
				user.ID, user.Email, user.DisplayName(), user.PhotoURL(),
				true, *emailVerified, *phoneVerified,
				createdAt, updatedAt,
				"en", "UTC",
				metadataJSON, preferencesJSON,
			)
			if err != nil {
				log.Printf("Error inserting user %s: %v", user.ID, err)
				ErrorCount++
				continue
			}

			migratedCount++
		}

		log.Printf("✅ Successfully migrated %d new users to Cloud SQL", migratedCount)
		log.Printf("📊 Skipped %d already existing users", skippedCount)
	}

	// 4. Generate migration report
	log.Println("📋 Step 4: Generating migration report...")

	endTime := time.Now()
	duration := endTime.Sub(startTime).String()

	stats := MigrationStats{
		TotalUsers:      len(users),
		MigratedUsers:   migratedCount,
		SkippedUsers:   skippedCount,
		ErrorCount:     ErrorCount,
		Status:          "completed_with_errors",
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		SupabaseURL:    supabaseURL,
		ServiceRoleKey: supabaseServiceRoleKey,
	}

	// Print stats in JSON format
	statsJSON, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		log.Printf("Error marshaling migration stats: %v", err)
	} else {
		fmt.Println("📊 Migration Statistics:")
		fmt.Println(string(statsJSON))
	}

	// 5. Save migration stats to database (optional)
	saveStatsSQL := `
		INSERT INTO schema_migrations (version, description, executed_at, success, details)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (version) DO UPDATE SET
			description = EXCLUDED.description,
			executed_at = EXCLUDED.executed_at,
			details = EXCLUDED.details
	`

	detailsJSON, _ := json.Marshal(map[string]interface{}{
		"total_supabase_users": stats.TotalUsers,
		"migrated_count":       stats.MigratedUsers,
		"skipped_count":       stats.SkippedUsers,
		"error_count":        stats.ErrorCount,
		"duration_seconds":    duration,
	})

	_, err = adapter.ExecContext(ctx, saveStatsSQL,
		"20250122_001_migrate_users", "User migration from Supabase", stats.Timestamp, true, detailsJSON)
	if err != nil {
		log.Printf("Error saving migration stats: %v", err)
	} else {
		log.Println("✅ Migration report saved to database")
	}

	log.Printf("🎉 Migration completed in %s", duration)
	log.Printf("📈 Total users: %d, Migrated: %d, Skipped: %d, Errors: %d",
		stats.TotalUsers, stats.MigratedUsers, stats.SkippedCount, stats.ErrorCount)

	if stats.ErrorCount > 0 {
		log.Printf("⚠️  Migration completed with %d errors. Check logs above.", stats.ErrorCount)
		os.Exit(1)
	}

	log.Println("🎉 User migration from Supabase to Cloud SQL completed successfully!")
}