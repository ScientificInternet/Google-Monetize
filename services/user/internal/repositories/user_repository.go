package repositories

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/config"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/models"
)

type UserRepository struct {
	gcpDB      *sqlx.DB
	supabaseDB *sqlx.DB
}

func NewUserRepository(gcpConfig config.DatabaseConfig, supabaseConfig config.SupabaseConfig) (*UserRepository, error) {
	// Connect to GCP Cloud SQL
	gcpDB, err := sqlx.Connect("postgres", gcpConfig.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to GCP database: %w", err)
	}

	gcpDB.SetMaxOpenConns(gcpConfig.MaxConnections)
	gcpDB.SetConnMaxIdleTime(gcpConfig.MaxIdleTime)
	gcpDB.SetConnMaxLifetime(gcpConfig.ConnMaxLifetime)

	// Connect to Supabase
	supabaseDB, err := sqlx.Connect("postgres", supabaseConfig.DBURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Supabase: %w", err)
	}

	supabaseDB.SetMaxOpenConns(10)
	supabaseDB.SetConnMaxIdleTime(5 * time.Minute)

	return &UserRepository{
		gcpDB:      gcpDB,
		supabaseDB: supabaseDB,
	}, nil
}

// GetUserFromGCP retrieves user from GCP database
func (r *UserRepository) GetUserFromGCP(userID string) (*models.User, error) {
	var user models.User
	err := r.gcpDB.Get(&user, `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		WHERE id = $1
	`, userID)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found in GCP database")
		}
		return nil, fmt.Errorf("failed to get user from GCP: %w", err)
	}

	return &user, nil
}

// GetUserFromSupabase retrieves user from Supabase database
func (r *UserRepository) GetUserFromSupabase(userID string) (*models.User, error) {
	var user models.User
	err := r.supabaseDB.Get(&user, `
		SELECT id, display_name, photo_url, onboarded, created_at, updated_at
		FROM public.users
		WHERE id = $1
	`, userID)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found in Supabase database")
		}
		return nil, fmt.Errorf("failed to get user from Supabase: %w", err)
	}

	return &user, nil
}

// CreateUserInGCP creates user in GCP database
func (r *UserRepository) CreateUserInGCP(user *models.User) error {
	query := `
		INSERT INTO billing.users (id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			email = EXCLUDED.email,
			display_name = EXCLUDED.display_name,
			photo_url = EXCLUDED.photo_url,
			role = EXCLUDED.role,
			onboarded = EXCLUDED.onboarded,
			is_active = EXCLUDED.is_active,
			organization_id = EXCLUDED.organization_id,
			updated_at = EXCLUDED.updated_at
	`

	_, err := r.gcpDB.Exec(query,
		user.ID, user.Email, user.DisplayName, user.PhotoURL,
		user.Role, user.Onboarded, user.IsActive, user.OrganizationID,
		user.CreatedAt, user.UpdatedAt)

	return err
}

// CreateUserInSupabase creates user in Supabase database
func (r *UserRepository) CreateUserInSupabase(user *models.User) error {
	query := `
		INSERT INTO public.users (id, display_name, photo_url, onboarded, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			photo_url = EXCLUDED.photo_url,
			onboarded = EXCLUDED.onboarded,
			updated_at = EXCLUDED.updated_at
	`

	_, err := r.supabaseDB.Exec(query,
		user.ID, user.DisplayName, user.PhotoURL,
		user.Onboarded, user.CreatedAt, user.UpdatedAt)

	return err
}

// UpdateUserInGCP updates user in GCP database
func (r *UserRepository) UpdateUserInGCP(user *models.User) error {
	query := `
		UPDATE billing.users
		SET display_name = $2, photo_url = $3, role = $4, onboarded = $5,
			is_active = $6, organization_id = $7, updated_at = $8
		WHERE id = $1
	`

	_, err := r.gcpDB.Exec(query,
		user.ID, user.DisplayName, user.PhotoURL, user.Role,
		user.Onboarded, user.IsActive, user.OrganizationID, time.Now())

	return err
}

// UpdateUserInSupabase updates user in Supabase database
func (r *UserRepository) UpdateUserInSupabase(user *models.User) error {
	query := `
		UPDATE public.users
		SET display_name = $2, photo_url = $3, onboarded = $4, updated_at = $5
		WHERE id = $1
	`

	_, err := r.supabaseDB.Exec(query,
		user.ID, user.DisplayName, user.PhotoURL,
		user.Onboarded, time.Now())

	return err
}

// DeleteUserFromGCP deletes user from GCP database
func (r *UserRepository) DeleteUserFromGCP(userID string) error {
	_, err := r.gcpDB.Exec("DELETE FROM billing.users WHERE id = $1", userID)
	return err
}

// DeleteUserFromSupabase deletes user from Supabase database
func (r *UserRepository) DeleteUserFromSupabase(userID string) error {
	_, err := r.supabaseDB.Exec("DELETE FROM public.users WHERE id = $1", userID)
	return err
}

// GetUsersWithPagination retrieves users with pagination
func (r *UserRepository) GetUsersWithPagination(offset, limit int) ([]*models.User, error) {
	var users []*models.User
	err := r.gcpDB.Select(&users, `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)

	if err != nil {
		return nil, fmt.Errorf("failed to get users: %w", err)
	}

	return users, nil
}

// SearchUsers searches users by query
func (r *UserRepository) SearchUsers(query string, offset, limit int) ([]*models.User, error) {
	var users []*models.User
	searchQuery := "%" + query + "%"

	err := r.gcpDB.Select(&users, `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		WHERE email ILIKE $1 OR display_name ILIKE $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, searchQuery, limit, offset)

	if err != nil {
		return nil, fmt.Errorf("failed to search users: %w", err)
	}

	return users, nil
}

// GetUserByEmail retrieves user by email
func (r *UserRepository) GetUserByEmail(email string) (*models.User, error) {
	var user models.User
	err := r.gcpDB.Get(&user, `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		WHERE email = $1
	`, email)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	return &user, nil
}

// CountUsers returns total number of users
func (r *UserRepository) CountUsers() (int, error) {
	var count int
	err := r.gcpDB.Get(&count, "SELECT COUNT(*) FROM billing.users")
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}
	return count, nil
}

// CountActiveUsers returns number of active users
func (r *UserRepository) CountActiveUsers() (int, error) {
	var count int
	err := r.gcpDB.Get(&count, "SELECT COUNT(*) FROM billing.users WHERE is_active = true")
	if err != nil {
		return 0, fmt.Errorf("failed to count active users: %w", err)
	}
	return count, nil
}

// GetUsersByRole retrieves users by role
func (r *UserRepository) GetUsersByRole(role string, offset, limit int) ([]*models.User, error) {
	var users []*models.User
	err := r.gcpDB.Select(&users, `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		WHERE role = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, role, limit, offset)

	if err != nil {
		return nil, fmt.Errorf("failed to get users by role: %w", err)
	}

	return users, nil
}

// GetUsersByOrganization retrieves users by organization
func (r *UserRepository) GetUsersByOrganization(orgID string, offset, limit int) ([]*models.User, error) {
	var users []*models.User
	err := r.gcpDB.Select(&users, `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		WHERE organization_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, orgID, limit, offset)

	if err != nil {
		return nil, fmt.Errorf("failed to get users by organization: %w", err)
	}

	return users, nil
}

// BatchCreateUsers creates multiple users in both databases
func (r *UserRepository) BatchCreateUsers(users []*models.User) error {
	// Start transaction for GCP
	tx, err := r.gcpDB.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert into GCP
	for _, user := range users {
		query := `
			INSERT INTO billing.users (id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (id) DO NOTHING
		`

		_, err = tx.Exec(query,
			user.ID, user.Email, user.DisplayName, user.PhotoURL,
			user.Role, user.Onboarded, user.IsActive, user.OrganizationID,
			user.CreatedAt, user.UpdatedAt)

		if err != nil {
			return fmt.Errorf("failed to insert user %s into GCP: %w", user.ID, err)
		}
	}

	// Commit GCP transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit GCP transaction: %w", err)
	}

	// Insert into Supabase
	for _, user := range users {
		query := `
			INSERT INTO public.users (id, display_name, photo_url, onboarded, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (id) DO NOTHING
		`

		_, err = r.supabaseDB.Exec(query,
			user.ID, user.DisplayName, user.PhotoURL,
			user.Onboarded, user.CreatedAt, user.UpdatedAt)

		if err != nil {
			// Log error but don't fail the entire batch
			fmt.Printf("Warning: failed to insert user %s into Supabase: %v\n", user.ID, err)
		}
	}

	return nil
}

// StoreAdsRefreshToken stores encrypted Google Ads refresh token for user
func (r *UserRepository) StoreAdsRefreshToken(userID, loginCustomerID, encryptedToken string) error {
	query := `
		INSERT INTO billing.user_ads_tokens (user_id, login_customer_id, refresh_token, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			login_customer_id = EXCLUDED.login_customer_id,
			refresh_token = EXCLUDED.refresh_token,
			updated_at = NOW()
	`

	_, err := r.gcpDB.Exec(query, userID, loginCustomerID, encryptedToken)
	if err != nil {
		return fmt.Errorf("failed to store ads refresh token: %w", err)
	}

	return nil
}

// RevokeAdsRefreshToken revokes/deletes Google Ads refresh token for user
func (r *UserRepository) RevokeAdsRefreshToken(userID string) error {
	query := `DELETE FROM billing.user_ads_tokens WHERE user_id = $1`

	_, err := r.gcpDB.Exec(query, userID)
	if err != nil {
		return fmt.Errorf("failed to revoke ads refresh token: %w", err)
	}

	return nil
}

// GetUserAdsTokens retrieves stored Ads tokens for user
func (r *UserRepository) GetUserAdsTokens(userID string) (map[string]interface{}, error) {
	var tokens []struct {
		LoginCustomerID string `db:"login_customer_id"`
		RefreshToken   string `db:"refresh_token"`
		CreatedAt      time.Time `db:"created_at"`
		UpdatedAt      time.Time `db:"updated_at"`
	}

	err := r.gcpDB.Select(&tokens, `
		SELECT login_customer_id, refresh_token, created_at, updated_at
		FROM billing.user_ads_tokens
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)

	if err != nil {
		return nil, fmt.Errorf("failed to get ads tokens: %w", err)
	}

	if len(tokens) == 0 {
		return map[string]interface{}{}, nil
	}

	// Return latest token as simple map for now
	return map[string]interface{}{
		"login_customer_id": tokens[0].LoginCustomerID,
		"refresh_token":    tokens[0].RefreshToken,
		"created_at":      tokens[0].CreatedAt,
		"updated_at":      tokens[0].UpdatedAt,
	}, nil
}

// Close closes database connections
func (r *UserRepository) Close() error {
	if err := r.gcpDB.Close(); err != nil {
		return fmt.Errorf("failed to close GCP database: %w", err)
	}

	if err := r.supabaseDB.Close(); err != nil {
		return fmt.Errorf("failed to close Supabase database: %w", err)
	}

	return nil
}
