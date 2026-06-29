//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/config"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/pkg/supabase"
)

// HybridDatabaseManager 混合数据库管理器
// 整合Cloud SQL (业务数据) + Supabase (认证数据)
type HybridDatabaseManager struct {
	cloudSQLPool *pgxpool.Pool
	supabase     *supabase.SupabaseClient
	logger      *log.Logger
}

// UserInfo 用户信息整合
type UserInfo struct {
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	CreatedAt    string `json:"created_at"`
	Balance      int64  `json:"balance"`
	subscriptions string `json:"subscription"`
	LastLogin    string `json:"last_login,omitempty"`
}

// NewHybridDatabaseManager 创建混合数据库管理器
func NewHybridDatabaseManager(ctx context.Context, cfg *config.Config) (*HybridDatabaseManager, error) {
	// 创建Cloud SQL连接池
	dbConfig := &database.Config{
		DatabaseURL:    cfg.DatabaseURL,
		MaxConnections: 20,
		MinConnections: 5,
		MaxConnLifetime: 0, // 使用默认值
	}

	dbManager, err := database.NewDatabaseManager(ctx, dbConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create database manager: %w", err)
	}

	// 创建Supabase客户端
	supabaseConfig := &supabase.Config{
		URL:         cfg.SupabaseURL,
		ServiceKey:  cfg.SupabaseServiceKey,
	}

	supabaseClient, err := supabase.NewSupabaseClient(supabaseConfig)
	if err != nil {
		dbManager.Close()
		return nil, fmt.Errorf("failed to create Supabase client: %w", err)
	}

	return &HybridDatabaseManager{
		cloudSQLPool: dbManager.GetCloudSQLPool(),
		supabase:     supabaseClient,
		logger:      log.Default(),
	}, nil
}

// GetUserInfo 获取整合的用户信息
func (hdm *HybridDatabaseManager) GetUserInfo(ctx context.Context, userID string) (*UserInfo, error) {
	// 1. 从Supabase获取用户认证信息
	supabaseUser, err := hdm.supabase.GetUser(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user from Supabase: %w", err)
	}

	// 2. 从Cloud SQL获取业务数据
	var balance int64
	var subscription string
	var lastLogin *string

	err = hdm.cloudSQLPool.QueryRow(ctx,
		`SELECT balance, subscription_tier, last_login
		 FROM billing.users
		 WHERE user_id = $1`,
		userID).Scan(&balance, &subscription, &lastLogin)
	if err != nil {
		hdm.logger.Printf("Warning: failed to get business data from Cloud SQL: %v", err)
		// 设置默认值
		balance = 0
		subscription = "starter"
		lastLogin = nil
	}

	userInfo := &UserInfo{
		UserID:       supabaseUser.ID,
		Email:        supabaseUser.Email,
		CreatedAt:    supabaseUser.CreatedAt.Format("2006-01-02T15:04:05Z"),
		Balance:      balance,
		subscriptions: subscription,
	}

	if lastLogin != nil {
		userInfo.LastLogin = *lastLogin
	}

	return userInfo, nil
}

// UpdateUserBalance 更新用户余额
func (hdm *HybridDatabaseManager) UpdateUserBalance(ctx context.Context, userID string, amount int64, description string) error {
	tx, err := hdm.cloudSQLPool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. 更新余额
	_, err = tx.Exec(ctx,
		`UPDATE billing.users
		 SET balance = balance + $1, updated_at = NOW()
		 WHERE user_id = $2`,
		amount, userID)
	if err != nil {
		return fmt.Errorf("failed to update balance: %w", err)
	}

	// 2. 记录交易
	_, err = tx.Exec(ctx,
		`INSERT INTO billing.token_transactions
		 (user_id, amount, description, created_at)
		 VALUES ($1, $2, $3, NOW())`,
		userID, amount, description)
	if err != nil {
		return fmt.Errorf("failed to record transaction: %w", err)
	}

	// 3. 提交事务
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	hdm.logger.Printf("Updated balance for user %s: %d (%s)", userID, amount, description)
	return nil
}

// ValidateUser 验证用户（从Supabase获取，检查Cloud SQL中的业务状态）
func (hdm *HybridDatabaseManager) ValidateUser(ctx context.Context, userID string) (*UserInfo, error) {
	// 1. 验证Supabase用户存在
	supabaseUser, err := hdm.supabase.GetUser(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found in Supabase: %w", err)
	}

	// 2. 检查用户是否在Cloud SQL中有记录
	var exists bool
	err = hdm.cloudSQLPool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM billing.users WHERE user_id = $1)",
		userID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}

	// 3. 如果用户不存在于Cloud SQL，创建记录
	if !exists {
		err = hdm.createUserRecord(ctx, userID, supabaseUser.Email)
		if err != nil {
			return nil, fmt.Errorf("failed to create user record: %w", err)
		}
		hdm.logger.Printf("Created Cloud SQL record for user %s", userID)
	}

	// 4. 返回用户信息
	return hdm.GetUserInfo(ctx, userID)
}

// createUserRecord 在Cloud SQL中创建用户记录
func (hdm *HybridDatabaseManager) createUserRecord(ctx context.Context, userID, email string) error {
	_, err := hdm.cloudSQLPool.Exec(ctx,
		`INSERT INTO billing.users
		 (user_id, email, balance, subscription_tier, created_at, updated_at)
		 VALUES ($1, $2, 0, 'starter', NOW(), NOW())
		 ON CONFLICT (user_id) DO NOTHING`,
		userID, email)
	return err
}

// HealthCheck 健康检查
func (hdm *HybridDatabaseManager) HealthCheck(ctx context.Context) error {
	// 1. 检查Cloud SQL连接
	if err := hdm.cloudSQLPool.Ping(ctx); err != nil {
		return fmt.Errorf("Cloud SQL health check failed: %w", err)
	}

	// 2. 检查Supabase连接
	if err := hdm.supabase.HealthCheck(ctx); err != nil {
		return fmt.Errorf("Supabase health check failed: %w", err)
	}

	hdm.logger.Println("Hybrid database health check passed")
	return nil
}

// Close 关闭所有连接
func (hdm *HybridDatabaseManager) Close() {
	if hdm.cloudSQLPool != nil {
		hdm.cloudSQLPool.Close()
		hdm.logger.Println("Cloud SQL connection pool closed")
	}
}

// GetStats 获取数据库统计信息
func (hdm *HybridDatabaseManager) GetStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Cloud SQL统计
	var totalUsers, totalBalance int64
	err := hdm.cloudSQLPool.QueryRow(ctx,
		"SELECT COUNT(*), COALESCE(SUM(balance), 0) FROM billing.users").Scan(&totalUsers, &totalBalance)
	if err != nil {
		hdm.logger.Printf("Warning: failed to get Cloud SQL stats: %v", err)
		stats["cloud_sql_users"] = 0
		stats["total_balance"] = 0
	} else {
		stats["cloud_sql_users"] = totalUsers
		stats["total_balance"] = totalBalance
	}

	// 连接池统计
	stats["pool_max_conns"] = hdm.cloudSQLPool.Stat().MaxConns()
	stats["pool_total_conns"] = hdm.cloudSQLPool.Stat().TotalConns()
	stats["pool_idle_conns"] = hdm.cloudSQLPool.Stat().IdleConns()

	return stats, nil
}