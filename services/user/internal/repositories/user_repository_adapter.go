package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/config"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/models"
)

// AdapterUserRepository 使用统一数据库适配器的用户仓储实现
type AdapterUserRepository struct {
	adapter *database.UnifiedDatabaseAdapter
}

// GetAdapterMode 获取适配器模式
func (r *AdapterUserRepository) GetAdapterMode() string {
	return "unified"
}

// NewAdapterUserRepository 创建使用统一适配器的用户仓储
func NewAdapterUserRepository(gcpConfig config.DatabaseConfig, supabaseConfig config.SupabaseConfig) (*AdapterUserRepository, error) {
	// 构建适配器配置
	adapterConfig := database.AdapterConfig{
		SupabaseURL:       supabaseConfig.URL,
		SupabaseServiceKey: supabaseConfig.APIKey,
		CloudSQLURL:       gcpConfig.URL,
		ConnectionMode:    "unified", // 使用统一模式
		QueryTimeout:      30 * time.Second, // 30秒
		ConnectTimeout:    10 * time.Second, // 10秒
		MaxRetries:        3,
		RetryDelay:        1 * time.Second,   // 1秒
	}

	// 创建统一数据库适配器
	adapter, err := database.NewUnifiedDatabaseAdapter(adapterConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create unified database adapter: %w", err)
	}

	return &AdapterUserRepository{
		adapter: adapter,
	}, nil
}

// Close 关闭数据库连接
func (r *AdapterUserRepository) Close() error {
	return r.adapter.Close()
}

// GetUser 从Supabase数据库获取用户（用户域统一到Supabase）
func (r *AdapterUserRepository) GetUser(userID string) (*models.User, error) {
	var user models.User
	query := `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM public.user_profiles
		WHERE user_id = $1
	`

	err := r.adapter.QueryRowContext(context.Background(), database.DatabaseTypeSupabase, query, userID).Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.PhotoURL, &user.Role,
		&user.Onboarded, &user.IsActive, &user.OrganizationID, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user from Supabase: %w", err)
	}

	return &user, nil
}

// GetUserFromGCP 从GCP数据库获取用户（保留兼容性，但重定向到Supabase）
func (r *AdapterUserRepository) GetUserFromGCP(userID string) (*models.User, error) {
	return r.GetUser(userID)
}

// GetUserFromSupabase 从Supabase数据库获取用户（保留兼容性）
func (r *AdapterUserRepository) GetUserFromSupabase(userID string) (*models.User, error) {
	return r.GetUser(userID)
}

// CreateUser 在Supabase数据库创建用户（用户域统一到Supabase）
func (r *AdapterUserRepository) CreateUser(user *models.User) error {
	query := `
		INSERT INTO public.user_profiles (user_id, email, display_name, photo_url, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := r.adapter.ExecContext(context.Background(), database.DatabaseTypeSupabase, query,
		user.ID, user.Email, user.DisplayName, user.PhotoURL, user.CreatedAt, user.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create user in Supabase: %w", err)
	}

	return nil
}

// CreateUserInGCP 在GCP数据库创建用户（保留兼容性，但重定向到Supabase）
func (r *AdapterUserRepository) CreateUserInGCP(user *models.User) error {
	return r.CreateUser(user)
}

// CreateUserInSupabase 在Supabase数据库创建用户（保留兼容性）
func (r *AdapterUserRepository) CreateUserInSupabase(user *models.User) error {
	return r.CreateUser(user)
}

// UpdateUser 更新Supabase数据库中的用户（用户域统一到Supabase）
func (r *AdapterUserRepository) UpdateUser(user *models.User) error {
	query := `
		UPDATE public.user_profiles
		SET email = $2, display_name = $3, photo_url = $4, updated_at = $5
		WHERE user_id = $1
	`

	_, err := r.adapter.ExecContext(context.Background(), database.DatabaseTypeSupabase, query,
		user.ID, user.Email, user.DisplayName, user.PhotoURL, user.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update user in Supabase: %w", err)
	}

	return nil
}

// UpdateUserInGCP 更新GCP数据库中的用户（保留兼容性，但重定向到Supabase）
func (r *AdapterUserRepository) UpdateUserInGCP(user *models.User) error {
	return r.UpdateUser(user)
}

// UpdateUserInSupabase 更新Supabase数据库中的用户（保留兼容性）
func (r *AdapterUserRepository) UpdateUserInSupabase(user *models.User) error {
	return r.UpdateUser(user)
}

// DeleteUser 从Supabase数据库删除用户（用户域统一到Supabase）
func (r *AdapterUserRepository) DeleteUser(userID string) error {
	query := "DELETE FROM public.user_profiles WHERE user_id = $1"
	_, err := r.adapter.ExecContext(context.Background(), database.DatabaseTypeSupabase, query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user from Supabase: %w", err)
	}
	return nil
}

// DeleteUserFromGCP 从GCP数据库删除用户（保留兼容性，但重定向到Supabase）
func (r *AdapterUserRepository) DeleteUserFromGCP(userID string) error {
	return r.DeleteUser(userID)
}

// DeleteUserFromSupabase 从Supabase数据库删除用户（保留兼容性）
func (r *AdapterUserRepository) DeleteUserFromSupabase(userID string) error {
	return r.DeleteUser(userID)
}

// GetUsersWithPagination 分页获取用户
func (r *AdapterUserRepository) GetUsersWithPagination(offset, limit int) ([]*models.User, error) {
	var users []*models.User
	query := `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.adapter.QueryContext(context.Background(), database.DatabaseTypeCloudSQL, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID, &user.Email, &user.DisplayName, &user.PhotoURL, &user.Role,
			&user.Onboarded, &user.IsActive, &user.OrganizationID, &user.CreatedAt, &user.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}

	return users, nil
}

// SearchUsers 搜索用户
func (r *AdapterUserRepository) SearchUsers(query string, offset, limit int) ([]*models.User, error) {
	var users []*models.User
	searchQuery := "%" + query + "%"
	sqlQuery := `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		WHERE email ILIKE $1 OR display_name ILIKE $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.adapter.QueryContext(context.Background(), database.DatabaseTypeCloudSQL, sqlQuery, searchQuery, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to search users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID, &user.Email, &user.DisplayName, &user.PhotoURL, &user.Role,
			&user.Onboarded, &user.IsActive, &user.OrganizationID, &user.CreatedAt, &user.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}

	return users, nil
}

// GetUserByEmail 通过邮箱获取用户
func (r *AdapterUserRepository) GetUserByEmail(email string) (*models.User, error) {
	var user models.User
	query := `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		WHERE email = $1
	`

	err := r.adapter.QueryRowContext(context.Background(), database.DatabaseTypeCloudSQL, query, email).Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.PhotoURL, &user.Role,
		&user.Onboarded, &user.IsActive, &user.OrganizationID, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	return &user, nil
}

// CountUsers 统计用户总数
func (r *AdapterUserRepository) CountUsers() (int, error) {
	var count int
	query := "SELECT COUNT(*) FROM billing.users"
	err := r.adapter.QueryRowContext(context.Background(), database.DatabaseTypeCloudSQL, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}
	return count, nil
}

// CountActiveUsers 统计活跃用户数
func (r *AdapterUserRepository) CountActiveUsers() (int, error) {
	var count int
	query := "SELECT COUNT(*) FROM billing.users WHERE is_active = true"
	err := r.adapter.QueryRowContext(context.Background(), database.DatabaseTypeCloudSQL, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count active users: %w", err)
	}
	return count, nil
}

// GetUsersByRole 按角色获取用户
func (r *AdapterUserRepository) GetUsersByRole(role string, offset, limit int) ([]*models.User, error) {
	var users []*models.User
	query := `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		WHERE role = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.adapter.QueryContext(context.Background(), database.DatabaseTypeCloudSQL, query, role, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get users by role: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID, &user.Email, &user.DisplayName, &user.PhotoURL, &user.Role,
			&user.Onboarded, &user.IsActive, &user.OrganizationID, &user.CreatedAt, &user.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}

	return users, nil
}

// GetUsersByOrganization 按组织获取用户
func (r *AdapterUserRepository) GetUsersByOrganization(orgID string, offset, limit int) ([]*models.User, error) {
	var users []*models.User
	query := `
		SELECT id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at
		FROM billing.users
		WHERE organization_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.adapter.QueryContext(context.Background(), database.DatabaseTypeCloudSQL, query, orgID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get users by organization: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID, &user.Email, &user.DisplayName, &user.PhotoURL, &user.Role,
			&user.Onboarded, &user.IsActive, &user.OrganizationID, &user.CreatedAt, &user.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}

	return users, nil
}

// BatchCreateUsers 批量创建用户（简化实现）
func (r *AdapterUserRepository) BatchCreateUsers(users []*models.User) error {
	// 使用事务创建多个用户
	// 由于适配器简化了连接管理，这里逐个创建
	for _, user := range users {
		if err := r.CreateUserInGCP(user); err != nil {
			return fmt.Errorf("failed to create user %s: %w", user.ID, err)
		}
	}
	return nil
}