package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/pkg/supabaseauth"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/models"
)

// UserSyncService 处理从Supabase到Cloud SQL的用户数据同步
type UserSyncService struct {
	dbAdapter    database.DatabaseAdapter
	supabaseClient supabaseauth.SupabaseClient
}

// NewUserSyncService 创建新的用户同步服务
func NewUserSyncService() (*UserSyncService, error) {
	// 获取数据库适配器
	adapter, err := database.GetFinalAdapterForService("user")
	if err != nil {
		return nil, fmt.Errorf("failed to create database adapter: %w", err)
	}

	// 创建Supabase客户端
	supabaseClient, err := supabaseauth.NewSupabaseClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create supabase client: %w", err)
	}

	return &UserSyncService{
		dbAdapter:    adapter,
		supabaseClient: supabaseClient,
	}, nil
}

// SyncUserFromSupabase 从Supabase同步用户信息到Cloud SQL
func (s *UserSyncService) SyncUserFromSupabase(ctx context.Context, userID string) error {
	// 1. 从Supabase获取最新的用户信息
	supabaseUser, err := s.supabaseClient.GetUser(userID)
	if err != nil {
		return fmt.Errorf("从Supabase获取用户信息失败: %w", err)
	}

	// 2. 提取用户元数据
	fullName := ""
	avatarURL := ""

	if supabaseUser.UserMetadata != nil {
		if name, ok := supabaseUser.UserMetadata["full_name"].(string); ok {
			fullName = name
		} else if name, ok := supabaseUser.UserMetadata["name"].(string); ok {
			fullName = name
		}

		if avatar, ok := supabaseUser.UserMetadata["avatar_url"].(string); ok {
			avatarURL = avatar
		} else if picture, ok := supabaseUser.UserMetadata["picture"].(string); ok {
			avatarURL = picture
		}
	}

	// 3. 更新Cloud SQL中的用户信息
	err = s.updateUserInCloudSQL(ctx, userID, supabaseUser.Email, fullName, avatarURL)
	if err != nil {
		return fmt.Errorf("同步用户信息到Cloud SQL失败: %w", err)
	}

	return nil
}

// updateUserInCloudSQL 更新Cloud SQL中的用户信息
func (s *UserSyncService) updateUserInCloudSQL(ctx context.Context, userID, email, fullName, avatarURL string) error {
	query := `
		UPDATE user.users
		SET email = $1,
		    name = $2,
		    avatar_url = $3,
		    updated_at = NOW()
		WHERE id = $4
	`

	_, err := s.dbAdapter.ExecPGX(
		ctx,
		query,
		email,
		fullName,
		avatarURL,
		userID,
	)

	if err != nil {
		return fmt.Errorf("更新用户信息失败: %w", err)
	}

	return nil
}

// GetCloudSQLUser 从Cloud SQL获取用户信息
func (s *UserSyncService) GetCloudSQLUser(ctx context.Context, userID string) (*models.User, error) {
	query := `
		SELECT id, email, name, avatar_url, status, created_at, updated_at
		FROM user.users
		WHERE id = $1
	`

	var user models.User
	err := s.dbAdapter.QueryRowPGX(ctx, query, userID).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.AvatarURL,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("user not found: %s", userID)
		}
		return nil, fmt.Errorf("查询用户信息失败: %w", err)
	}

	return &user, nil
}

// CreateCloudSQLUser 在Cloud SQL中创建用户记录
func (s *UserSyncService) CreateCloudSQLUser(ctx context.Context, userID, email, fullName, avatarURL string) error {
	query := `
		INSERT INTO user.users (id, email, name, avatar_url, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'active', NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			email = EXCLUDED.email,
			name = EXCLUDED.name,
			avatar_url = EXCLUDED.avatar_url,
			updated_at = NOW()
	`

	_, err := s.dbAdapter.ExecPGX(
		ctx,
		query,
		userID,
		email,
		fullName,
		avatarURL,
	)

	if err != nil {
		return fmt.Errorf("创建用户记录失败: %w", err)
	}

	return nil
}

// BatchSyncUsers 批量同步用户信息
func (s *UserSyncService) BatchSyncUsers(ctx context.Context, userIDs []string) (successCount, failCount int, errs []error) {
	for _, userID := range userIDs {
		err := s.SyncUserFromSupabase(ctx, userID)
		if err != nil {
			failCount++
			errs = append(errs, fmt.Errorf("sync user %s failed: %w", userID, err))
			continue
		}
		successCount++
	}

	return successCount, failCount, errs
}

// ValidateUserDataIntegrity 验证用户数据完整性
func (s *UserSyncService) ValidateUserDataIntegrity(ctx context.Context, userID string) (*UserDataIntegrityResult, error) {
	// 获取Supabase用户信息
	supabaseUser, err := s.supabaseClient.GetUser(userID)
	if err != nil {
		return nil, fmt.Errorf("从Supabase获取用户信息失败: %w", err)
	}

	// 获取Cloud SQL用户信息
	cloudSQLUser, err := s.GetCloudSQLUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("从Cloud SQL获取用户信息失败: %w", err)
	}

	result := &UserDataIntegrityResult{
		UserID:        userID,
		SupabaseUser: supabaseUser,
		CloudSQLUser:  cloudSQLUser,
		IsIntegrityOK: true,
		Issues:       []string{},
	}

	// 检查邮箱一致性
	if supabaseUser.Email != cloudSQLUser.Email {
		result.IsIntegrityOK = false
		result.Issues = append(result.Issues, fmt.Sprintf("邮箱不一致: Supabase=%s, CloudSQL=%s", supabaseUser.Email, cloudSQLUser.Email))
	}

	// 检查用户状态
	if cloudSQLUser.Status != "active" {
		result.IsIntegrityOK = false
		result.Issues = append(result.Issues, fmt.Sprintf("用户状态异常: %s", cloudSQLUser.Status))
	}

	return result, nil
}

// UserDataIntegrityResult 用户数据完整性检查结果
type UserDataIntegrityResult struct {
	UserID        string
	SupabaseUser *supabaseauth.User
	CloudSQLUser  *models.User
	IsIntegrityOK bool
	Issues       []string
}

// RepairUserDataIntegrity 修复用户数据完整性问题
func (s *UserSyncService) RepairUserDataIntegrity(ctx context.Context, result *UserDataIntegrityResult) error {
	if !result.IsIntegrityOK {
		// 从Supabase重新同步数据
		return s.SyncUserFromSupabase(ctx, result.UserID)
	}
	return nil
}

// Close 关闭服务连接
func (s *UserSyncService) Close() error {
	if s.dbAdapter != nil {
		return s.dbAdapter.Close()
	}
	return nil
}