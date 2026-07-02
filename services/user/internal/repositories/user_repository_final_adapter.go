package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/models"
)

// FinalAdapterUserRepository 使用FinalAdapter的用户仓储实现
type FinalAdapterUserRepository struct {
	adapter database.DatabaseAdapter
	service string
}

// GetAdapterMode 获取适配器模式
func (r *FinalAdapterUserRepository) GetAdapterMode() string {
	return "final"
}

// NewFinalAdapterUserRepository 创建使用FinalAdapter的用户仓储
func NewFinalAdapterUserRepository() (*FinalAdapterUserRepository, error) {
	adapter, err := database.GetFinalAdapterForService("user")
	if err != nil {
		return nil, fmt.Errorf("failed to create final database adapter for user service: %w", err)
	}
	return &FinalAdapterUserRepository{adapter: adapter, service: "user"}, nil
}

// Close 关闭数据库连接
func (r *FinalAdapterUserRepository) Close() error {
	if closer, ok := r.adapter.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

const userColumns = `id, email, display_name, photo_url, role, onboarded, is_active, organization_id, created_at, updated_at`

// scanUser 从 *sql.Row 或 *sql.Rows 扫描一个 User
func scanUser(row interface{ Scan(...interface{}) error }, user *models.User) error {
	return row.Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.PhotoURL,
		&user.Role, &user.Onboarded, &user.IsActive, &user.OrganizationID,
		&user.CreatedAt, &user.UpdatedAt,
	)
}

// GetUserByID 根据ID获取用户
func (r *FinalAdapterUserRepository) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	query := `SELECT ` + userColumns + ` FROM billing.users WHERE id = $1`
	var user models.User
	if err := scanUser(r.adapter.QueryRow(ctx, query, userID), &user); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found: %s", userID)
		}
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}
	return &user, nil
}

// GetUserByEmail 根据邮箱获取用户
func (r *FinalAdapterUserRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `SELECT ` + userColumns + ` FROM billing.users WHERE email = $1`
	var user models.User
	if err := scanUser(r.adapter.QueryRow(ctx, query, email), &user); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found: %s", email)
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}
	return &user, nil
}

// CreateUser 创建用户
func (r *FinalAdapterUserRepository) CreateUser(ctx context.Context, user *models.User) error {
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
	now := time.Now()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now
	_, err := r.adapter.Exec(ctx, query,
		user.ID, user.Email, user.DisplayName, user.PhotoURL,
		user.Role, user.Onboarded, user.IsActive, user.OrganizationID,
		user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

// UpdateUser 更新用户
func (r *FinalAdapterUserRepository) UpdateUser(ctx context.Context, user *models.User) error {
	query := `
		UPDATE billing.users
		SET email = $2, display_name = $3, photo_url = $4, role = $5, onboarded = $6, is_active = $7, organization_id = $8, updated_at = $9
		WHERE id = $1
	`
	user.UpdatedAt = time.Now()
	_, err := r.adapter.Exec(ctx, query,
		user.ID, user.Email, user.DisplayName, user.PhotoURL,
		user.Role, user.Onboarded, user.IsActive, user.OrganizationID,
		user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

// DeleteUser 删除用户
func (r *FinalAdapterUserRepository) DeleteUser(ctx context.Context, userID string) error {
	query := `DELETE FROM billing.users WHERE id = $1`
	if _, err := r.adapter.Exec(ctx, query, userID); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}

// ListUsers 获取用户列表
func (r *FinalAdapterUserRepository) ListUsers(ctx context.Context, limit, offset int) ([]*models.User, error) {
	query := `SELECT ` + userColumns + ` FROM billing.users ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.adapter.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()
	var users []*models.User
	for rows.Next() {
		var user models.User
		if err := scanUser(rows, &user); err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}
	return users, nil
}
