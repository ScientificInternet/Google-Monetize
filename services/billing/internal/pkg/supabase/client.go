//go:build integration
// +build integration

package supabase

import (
	"context"
	"fmt"
	"log"

	"github.com/supabase-community/supabase-go"
	"github.com/supabase-community/gotrue-go/types"
)

// SupabaseClient Supabase客户端包装器
type SupabaseClient struct {
	client *supabase.Client
	logger *log.Logger
}

// Config Supabase配置
type Config struct {
	URL      string
	ServiceKey string
}

// NewSupabaseClient 创建Supabase客户端
func NewSupabaseClient(cfg *Config) (*SupabaseClient, error) {
	if cfg.URL == "" || cfg.ServiceKey == "" {
		return nil, fmt.Errorf("Supabase URL and Service Key are required")
	}

	client, err := supabase.NewClient(cfg.URL, cfg.ServiceKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Supabase client: %w", err)
	}

	return &SupabaseClient{
		client: client,
		logger: log.Default(),
	}, nil
}

// GetUser 获取用户信息
func (sc *SupabaseClient) GetUser(userID string) (*types.User, error) {
	user, err := sc.client.Auth.GetUser(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user from Supabase: %w", err)
	}
	return user, nil
}

// GetUserByEmail 通过邮箱获取用户
func (sc *SupabaseClient) GetUserByEmail(email string) (*types.User, error) {
	resp, err := sc.client.Auth.Admin.GetUserByEmail(email)
	if err != nil {
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	if len(resp.Users) == 0 {
		return nil, fmt.Errorf("user not found")
	}

	return &resp.Users[0], nil
}

// ValidateToken 验证JWT token
func (sc *SupabaseClient) ValidateToken(token string) (*types.User, error) {
	// 设置临时token进行验证
	sc.client.Auth.SetToken(token)

	user, err := sc.client.Auth.GetUser(token)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	return user, nil
}

// HealthCheck 健康检查
func (sc *SupabaseClient) HealthCheck(ctx context.Context) error {
	// 通过尝试获取一个不存在的用户来测试连接
	_, err := sc.client.Auth.GetUser("health-check-test")
	if err == nil {
		return fmt.Errorf("unexpected success during health check")
	}

	// 检查是否是预期的错误（用户不存在）
	if _, ok := err.(*types.ErrorResponse); ok {
		sc.logger.Println("Supabase health check passed")
		return nil
	}

	return fmt.Errorf("Supabase health check failed: %w", err)
}

// GetClient 获取原始Supabase客户端
func (sc *SupabaseClient) GetClient() *supabase.Client {
	return sc.client
}