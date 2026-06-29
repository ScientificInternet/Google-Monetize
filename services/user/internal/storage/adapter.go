package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/config"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/models"
)

// DualDatabaseAdapter 支持双数据库（Cloud SQL + Supabase）的适配器
type DualDatabaseAdapter struct {
	cloudSQLAdapter  *SingleDatabaseAdapter
	supabaseAdapter  *SingleDatabaseAdapter
}

// SingleDatabaseAdapter 单数据库适配器
type SingleDatabaseAdapter struct {
	mode         database.AdapterMode
	name         string // 数据库名称，用于日志
	cloudSQLPool *pgxpool.Pool
	supabaseDB   *sql.DB
	sqlxDB       *sqlx.DB // 向后兼容
}

// NewDualDatabaseAdapter 创建双数据库适配器
func NewDualDatabaseAdapter(gcpConfig config.DatabaseConfig, supabaseConfig config.SupabaseConfig) (*DualDatabaseAdapter, error) {
	// 创建Cloud SQL数据库适配器
	cloudSQLAdapter, err := createSingleAdapter(gcpConfig.URL, database.CloudSQLMode, "CloudSQL")
	if err != nil {
		return nil, fmt.Errorf("failed to create CloudSQL adapter: %w", err)
	}

	// 创建Supabase数据库适配器
	supabaseAdapter, err := createSingleAdapter(supabaseConfig.DBURL, database.SupabaseMode, "Supabase")
	if err != nil {
		cloudSQLAdapter.Close()
		return nil, fmt.Errorf("failed to create Supabase adapter: %w", err)
	}

	return &DualDatabaseAdapter{
		cloudSQLAdapter: cloudSQLAdapter,
		supabaseAdapter: supabaseAdapter,
	}, nil
}

// createSingleAdapter 创建单数据库适配器
func createSingleAdapter(databaseURL string, mode database.AdapterMode, name string) (*SingleDatabaseAdapter, error) {
	adapter := &SingleDatabaseAdapter{
		mode: mode,
		name: name,
	}

	// 根据模式初始化连接
	switch mode {
	case database.CloudSQLMode:
		// 使用新的CloudSQL模式
		config := database.Config{
			ServiceName:    "user",
			DatabaseURL:    databaseURL,
			Mode:           database.CloudSQLMode,
			MaxConnections: 20,
		}

		universalAdapter, err := database.NewUniversalAdapter(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create CloudSQL adapter: %w", err)
		}

		adapter.cloudSQLPool = universalAdapter.GetCloudSQLPool()

	case database.SupabaseMode:
		// 使用新的Supabase模式
		config := database.Config{
			ServiceName:    "user",
			DatabaseURL:    databaseURL,
			Mode:           database.SupabaseMode,
			MaxConnections: 10,
		}

		universalAdapter, err := database.NewUniversalAdapter(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create Supabase adapter: %w", err)
		}

		adapter.supabaseDB = universalAdapter.GetSupabaseDB()

	default:
		return nil, fmt.Errorf("unsupported adapter mode: %s", mode)
	}

	return adapter, nil
}

// GCP 返回Cloud SQL数据库适配器（向后兼容）
func (a *DualDatabaseAdapter) GCP() *SingleDatabaseAdapter {
	return a.cloudSQLAdapter
}

// CloudSQL 返回Cloud SQL数据库适配器
func (a *DualDatabaseAdapter) CloudSQL() *SingleDatabaseAdapter {
	return a.cloudSQLAdapter
}

// Supabase 返回Supabase数据库适配器
func (a *DualDatabaseAdapter) Supabase() *SingleDatabaseAdapter {
	return a.supabaseAdapter
}

// Close 关闭所有数据库连接
func (a *DualDatabaseAdapter) Close() error {
	var firstErr error

	if err := a.cloudSQLAdapter.Close(); err != nil && firstErr == nil {
		firstErr = err
	}

	if err := a.supabaseAdapter.Close(); err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

// GetMode 获取当前模式
func (a *DualDatabaseAdapter) GetMode() database.AdapterMode {
	return a.cloudSQLAdapter.mode
}

// Ping 测试所有数据库连接
func (a *DualDatabaseAdapter) Ping(ctx context.Context) error {
	if err := a.cloudSQLAdapter.Ping(ctx); err != nil {
		return fmt.Errorf("CloudSQL database ping failed: %w", err)
	}

	if err := a.supabaseAdapter.Ping(ctx); err != nil {
		return fmt.Errorf("Supabase database ping failed: %w", err)
	}

	return nil
}

// SingleDatabaseAdapter methods

// Close 关闭单数据库连接
func (a *SingleDatabaseAdapter) Close() error {
	if a.cloudSQLPool != nil {
		a.cloudSQLPool.Close()
	}
	if a.supabaseDB != nil {
		return a.supabaseDB.Close()
	}
	if a.sqlxDB != nil {
		return a.sqlxDB.Close()
	}
	return nil
}

// Ping 测试单数据库连接
func (a *SingleDatabaseAdapter) Ping(ctx context.Context) error {
	if a.cloudSQLPool != nil {
		return a.cloudSQLPool.Ping(ctx)
	}
	if a.supabaseDB != nil {
		return a.supabaseDB.PingContext(ctx)
	}
	if a.sqlxDB != nil {
		return a.sqlxDB.PingContext(ctx)
	}
	return fmt.Errorf("database connection not initialized")
}

// GetDB 获取sqlx.DB连接（向后兼容）
func (a *SingleDatabaseAdapter) GetDB() *sqlx.DB {
	if a.sqlxDB == nil {
		// 尝试从现有连接创建sqlx.DB
		if a.supabaseDB != nil {
			a.sqlxDB = sqlx.NewDb(a.supabaseDB, "postgres")
		}
	}
	return a.sqlxDB
}

// GetCloudSQLPool 获取Cloud SQL连接池
func (a *SingleDatabaseAdapter) GetCloudSQLPool() *pgxpool.Pool {
	return a.cloudSQLPool
}

// GetSupabaseDB 获取Supabase数据库连接
func (a *SingleDatabaseAdapter) GetSupabaseDB() *sql.DB {
	return a.supabaseDB
}

// GetMode 获取当前模式
func (a *SingleDatabaseAdapter) GetMode() database.AdapterMode {
	return a.mode
}

// User operations using the adapter

// GetUserFromGCP 从Cloud SQL数据库获取用户
func (a *DualDatabaseAdapter) GetUserFromGCP(userID string) (*models.User, error) {
	if a.cloudSQLAdapter.cloudSQLPool != nil {
		var user models.User
		err := a.cloudSQLAdapter.cloudSQLPool.QueryRow(context.Background(), `
			SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
			FROM billing.users
			WHERE id = $1
		`, userID).Scan(
			&user.ID, &user.Email, &user.DisplayName, &user.PhotoURL,
			&user.Role, &user.Onboarded, &user.IsActive, &user.OrganizationID,
			&user.CreatedAt, &user.UpdatedAt,
		)

		if err != nil {
			if err.Error() == "no rows in result set" {
				return nil, fmt.Errorf("user not found in CloudSQL database")
			}
			return nil, fmt.Errorf("failed to get user from CloudSQL: %w", err)
		}

		return &user, nil
	}

	// Fallback to sqlx for backward compatibility
	db := a.cloudSQLAdapter.GetDB()
	if db == nil {
		return nil, fmt.Errorf("CloudSQL database connection not available")
	}

	var user models.User
	err := db.Get(&user, `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		WHERE id = $1
	`, userID)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found in CloudSQL database")
		}
		return nil, fmt.Errorf("failed to get user from CloudSQL: %w", err)
	}

	return &user, nil
}

// GetUserFromSupabase 从Supabase数据库获取用户
func (a *DualDatabaseAdapter) GetUserFromSupabase(userID string) (*models.User, error) {
	db := a.supabaseAdapter.GetDB()
	if db == nil {
		return nil, fmt.Errorf("Supabase database connection not available")
	}

	var user models.User
	err := db.Get(&user, `
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

// CreateUserInGCP 在Cloud SQL数据库创建用户
func (a *DualDatabaseAdapter) CreateUserInGCP(user *models.User) error {
	if a.cloudSQLAdapter.cloudSQLPool != nil {
		_, err := a.cloudSQLAdapter.cloudSQLPool.Exec(context.Background(), `
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
		`,
			user.ID, user.Email, user.DisplayName, user.PhotoURL,
			user.Role, user.Onboarded, user.IsActive, user.OrganizationID,
			user.CreatedAt, user.UpdatedAt)

		return err
	}

	// Fallback to sqlx for backward compatibility
	db := a.cloudSQLAdapter.GetDB()
	if db == nil {
		return fmt.Errorf("CloudSQL database connection not available")
	}

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

	_, err := db.Exec(query,
		user.ID, user.Email, user.DisplayName, user.PhotoURL,
		user.Role, user.Onboarded, user.IsActive, user.OrganizationID,
		user.CreatedAt, user.UpdatedAt)

	return err
}

// CreateUserInSupabase 在Supabase数据库创建用户
func (a *DualDatabaseAdapter) CreateUserInSupabase(user *models.User) error {
	db := a.supabaseAdapter.GetDB()
	if db == nil {
		return fmt.Errorf("Supabase database connection not available")
	}

	query := `
		INSERT INTO public.users (id, display_name, photo_url, onboarded, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			photo_url = EXCLUDED.photo_url,
			onboarded = EXCLUDED.onboarded,
			updated_at = EXCLUDED.updated_at
	`

	_, err := db.Exec(query,
		user.ID, user.DisplayName, user.PhotoURL,
		user.Onboarded, user.CreatedAt, user.UpdatedAt)

	return err
}

// UpdateUserInGCP 更新Cloud SQL数据库中的用户
func (a *DualDatabaseAdapter) UpdateUserInGCP(user *models.User) error {
	if a.cloudSQLAdapter.cloudSQLPool != nil {
		_, err := a.cloudSQLAdapter.cloudSQLPool.Exec(context.Background(), `
			UPDATE billing.users
			SET display_name = $2, photo_url = $3, role = $4, onboarded = $5,
				is_active = $6, organization_id = $7, updated_at = $8
			WHERE id = $1
		`,
			user.ID, user.DisplayName, user.PhotoURL, user.Role,
			user.Onboarded, user.IsActive, user.OrganizationID, time.Now())

		return err
	}

	// Fallback to sqlx for backward compatibility
	db := a.cloudSQLAdapter.GetDB()
	if db == nil {
		return fmt.Errorf("CloudSQL database connection not available")
	}

	query := `
		UPDATE billing.users
		SET display_name = $2, photo_url = $3, role = $4, onboarded = $5,
			is_active = $6, organization_id = $7, updated_at = $8
		WHERE id = $1
	`

	_, err := db.Exec(query,
		user.ID, user.DisplayName, user.PhotoURL, user.Role,
		user.Onboarded, user.IsActive, user.OrganizationID, time.Now())

	return err
}

// UpdateUserInSupabase 更新Supabase数据库中的用户
func (a *DualDatabaseAdapter) UpdateUserInSupabase(user *models.User) error {
	db := a.supabaseAdapter.GetDB()
	if db == nil {
		return fmt.Errorf("Supabase database connection not available")
	}

	query := `
		UPDATE public.users
		SET display_name = $2, photo_url = $3, onboarded = $4, updated_at = $5
		WHERE id = $1
	`

	_, err := db.Exec(query,
		user.ID, user.DisplayName, user.PhotoURL,
		user.Onboarded, time.Now())

	return err
}

// BatchCreateUsers 批量创建用户
func (a *DualDatabaseAdapter) BatchCreateUsers(users []*models.User) error {
	if a.cloudSQLAdapter.cloudSQLPool != nil {
		// Use pgxpool transaction
		tx, err := a.cloudSQLAdapter.cloudSQLPool.Begin(context.Background())
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback(context.Background())

		// Insert into Cloud SQL
		for _, user := range users {
			query := `
				INSERT INTO billing.users (id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				ON CONFLICT (id) DO NOTHING
			`

			_, err = tx.Exec(context.Background(), query,
				user.ID, user.Email, user.DisplayName, user.PhotoURL,
				user.Role, user.Onboarded, user.IsActive, user.OrganizationID,
				user.CreatedAt, user.UpdatedAt)

			if err != nil {
				return fmt.Errorf("failed to insert user %s into CloudSQL: %w", user.ID, err)
			}
		}

		// Commit Cloud SQL transaction
		if err := tx.Commit(context.Background()); err != nil {
			return fmt.Errorf("failed to commit CloudSQL transaction: %w", err)
		}
	} else {
		// Fallback to sqlx for backward compatibility
		db := a.cloudSQLAdapter.GetDB()
		if db == nil {
			return fmt.Errorf("CloudSQL database connection not available")
		}

		tx, err := db.Beginx()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback()

		// Insert into Cloud SQL
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
				return fmt.Errorf("failed to insert user %s into CloudSQL: %w", user.ID, err)
			}
		}

		// Commit Cloud SQL transaction
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit CloudSQL transaction: %w", err)
		}
	}

	// Insert into Supabase
	db := a.supabaseAdapter.GetDB()
	if db != nil {
		for _, user := range users {
			query := `
				INSERT INTO public.users (id, display_name, photo_url, onboarded, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (id) DO NOTHING
			`

			_, err := db.Exec(query,
				user.ID, user.DisplayName, user.PhotoURL,
				user.Onboarded, user.CreatedAt, user.UpdatedAt)

			if err != nil {
				// Log error but don't fail the entire batch
				fmt.Printf("Warning: failed to insert user %s into Supabase: %v\n", user.ID, err)
			}
		}
	}

	return nil
}