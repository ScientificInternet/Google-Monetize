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
	// 使用GetFinalAdapterForService获取标准适配器
	adapter, err := database.GetFinalAdapterForService("user")
	if err != nil {
		return nil, fmt.Errorf("failed to create final database adapter for user service: %w", err)
	}

	return &FinalAdapterUserRepository{
		adapter: adapter,
		service: "user",
	}, nil
}

// Close 关闭数据库连接
func (r *FinalAdapterUserRepository) Close() error {
	if closer, ok := r.adapter.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// GetUserByID 根据ID获取用户
func (r *FinalAdapterUserRepository) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	query := `
		SELECT id, email, name, avatar_url, status, created_at, updated_at
		FROM user.users
		WHERE id = $1
	`

	var user models.User
	err := r.adapter.QueryRowContext(ctx, query, userID).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.AvatarURL,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found: %s", userID)
		}
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}

	return &user, nil
}

// GetUserByEmail 根据邮箱获取用户
func (r *FinalAdapterUserRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
		SELECT id, email, name, avatar_url, status, created_at, updated_at
		FROM user.users
		WHERE email = $1
	`

	var user models.User
	err := r.adapter.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.AvatarURL,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
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
		INSERT INTO user.users (id, email, name, avatar_url, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			email = EXCLUDED.email,
			name = EXCLUDED.name,
			avatar_url = EXCLUDED.avatar_url,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`

	now := time.Now()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now

	_, err := r.adapter.ExecContext(ctx, query,
		user.ID,
		user.Email,
		user.Name,
		user.AvatarURL,
		user.Status,
		user.CreatedAt,
		user.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// UpdateUser 更新用户
func (r *FinalAdapterUserRepository) UpdateUser(ctx context.Context, user *models.User) error {
	query := `
		UPDATE user.users
		SET email = $2, name = $3, avatar_url = $4, status = $5, updated_at = $6
		WHERE id = $1
	`

	user.UpdatedAt = time.Now()

	_, err := r.adapter.ExecContext(ctx, query,
		user.ID,
		user.Email,
		user.Name,
		user.AvatarURL,
		user.Status,
		user.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// DeleteUser 删除用户
func (r *FinalAdapterUserRepository) DeleteUser(ctx context.Context, userID string) error {
	query := `DELETE FROM user.users WHERE id = $1`

	_, err := r.adapter.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	return nil
}

// ListUsers 获取用户列表
func (r *FinalAdapterUserRepository) ListUsers(ctx context.Context, limit, offset int) ([]*models.User, error) {
	query := `
		SELECT id, email, name, avatar_url, status, created_at, updated_at
		FROM user.users
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.adapter.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID,
			&user.Email,
			&user.Name,
			&user.AvatarURL,
			&user.Status,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}

	return users, nil
}