package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/pkg/dbadmin"
	"github.com/ScientificInternet/Google-Monetize/services/useractivity/internal/config"
	"github.com/ScientificInternet/Google-Monetize/services/useractivity/internal/models"
)

// UserService 展示如何使用数据库适配器重构后的用户服务
type UserService struct {
	db     database.AdapterInterface // 使用适配器接口而非直接sql.DB
	dbAdmin *dbadmin.Client         // db-admin客户端用于DDL操作
	config *config.Config
}

// NewUserService 创建新的用户服务实例
func NewUserService(cfg *config.Config) (*UserService, error) {
	// 创建数据库适配器
	adapter, err := database.NewAdapter("useractivity", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create database adapter: %w", err)
	}

	// 创建db-admin客户端（用于DDL和管理操作）
	dbAdminClient := dbadmin.NewClient(cfg.DBAdminURL, cfg.DBAdminToken)

	return &UserService{
		db:     adapter,
		dbAdmin: dbAdminClient,
		config: cfg,
	}, nil
}

// GetUserNotifications 获取用户通知列表 - 展示查询功能
func (s *UserService) GetUserNotifications(ctx context.Context, userID string, limit int) ([]*models.UserNotification, error) {
	// 使用适配器执行查询，自动选择最佳执行方式
	query := `
		SELECT id, user_id, type, title, message, created_at
		FROM user_notifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := s.db.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query user notifications: %w", err)
	}
	defer rows.Close()

	var notifications []*models.UserNotification
	for rows.Next() {
		notification := &models.UserNotification{}
		err := rows.Scan(
			&notification.ID,
			&notification.UserID,
			&notification.Type,
			&notification.Title,
			&notification.Message,
			&notification.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification: %w", err)
		}
		notifications = append(notifications, notification)
	}

	return notifications, nil
}

// CreateUserNotification 创建用户通知 - 展示写入功能
func (s *UserService) CreateUserNotification(ctx context.Context, notification *models.UserNotification) error {
	query := `
		INSERT INTO user_notifications (user_id, type, title, message, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`

	var notificationID int64
	err := s.db.QueryRow(ctx, query,
		notification.UserID,
		notification.Type,
		notification.Title,
		notification.Message,
		time.Now(),
	).Scan(&notificationID)

	if err != nil {
		return fmt.Errorf("failed to create user notification: %w", err)
	}

	notification.ID = notificationID
	return nil
}

// UpdateNotificationState 更新通知读取状态 - 展示更新功能
func (s *UserService) UpdateNotificationState(ctx context.Context, userID string, lastReadID int64) error {
	// 使用适配器的Exec方法执行更新
	query := `
		INSERT INTO user_notification_state (user_id, last_read_id, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id)
		DO UPDATE SET
			last_read_id = EXCLUDED.last_read_id,
			updated_at = EXCLUDED.updated_at
	`

	_, err := s.db.Exec(ctx, query, userID, lastReadID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update notification state: %w", err)
	}

	return nil
}

// CreateUserCheckin 创建用户签到记录 - 展示事务功能
func (s *UserService) CreateUserCheckin(ctx context.Context, userID string) (*models.Checkin, error) {
	// 注意：在实际应用中，复杂的业务逻辑可能需要事务支持
	// 适配器接口可以扩展支持事务

	// 首先检查今天是否已经签到
	today := time.Now().Format("2006-01-02")
	var existingCount int
	err := s.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM user_checkin_stats WHERE user_id = $1 AND checkin_date = $2",
		userID, today,
	).Scan(&existingCount)

	if err != nil {
		return nil, fmt.Errorf("failed to check existing checkin: %w", err)
	}

	if existingCount > 0 {
		return nil, fmt.Errorf("user already checked in today")
	}

	// 这里简化处理，实际应用中应该使用事务来保证数据一致性
	// 适配器可以扩展支持BeginTx, Commit, Rollback等方法

	return nil, fmt.Errorf("transaction support needed for this operation")
}

// EnsureTablesExist 通过db-admin确保表结构存在 - 展示DDL管理
func (s *UserService) EnsureTablesExist(ctx context.Context) error {
	// 使用db-admin客户端执行DDL，而不是内嵌DDL
	ddlStatements := []string{
		`CREATE TABLE IF NOT EXISTS user_notification_state (
			user_id TEXT PRIMARY KEY,
			last_read_id BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS user_checkin_stats (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			checkin_date DATE NOT NULL,
			tokens_earned INTEGER NOT NULL DEFAULT 0,
			streak_day INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(user_id, checkin_date)
		)`,
	}

	for _, ddl := range ddlStatements {
		// 先进行dry run验证
		result, err := s.dbAdmin.ExecuteDDL(ctx, "useractivity", ddl, true)
		if err != nil {
			return fmt.Errorf("DDL validation failed for statement: %s, error: %w", ddl, err)
		}

		if !result.Success {
			return fmt.Errorf("DDL validation returned failure for statement: %s", ddl)
		}

		// 执行实际的DDL
		result, err = s.dbAdmin.ExecuteDDL(ctx, "useractivity", ddl, false)
		if err != nil {
			return fmt.Errorf("failed to execute DDL: %s, error: %w", ddl, err)
		}

		if !result.Success {
			return fmt.Errorf("DDL execution returned failure for statement: %s", ddl)
		}

		log.Printf("Successfully executed DDL: %s", ddl)
	}

	return nil
}

// GetDatabaseStatus 获取数据库状态信息 - 展示监控功能
func (s *UserService) GetDatabaseStatus(ctx context.Context) (*database.StatusInfo, error) {
	// 使用适配器获取连接状态
	status, err := s.db.GetStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database status: %w", err)
	}

	// 使用db-admin客户端获取详细信息
	dbAdminStatus, err := s.dbAdmin.GetStatus(ctx, "useractivity")
	if err != nil {
		log.Printf("Warning: failed to get db-admin status: %v", err)
		// 继续使用适配器状态
	}

	// 合并状态信息
	return &database.StatusInfo{
		AdapterMode:      s.db.GetMode(),
		ConnectionStatus: status,
		DBAdminStatus:    dbAdminStatus,
		LastChecked:      time.Now(),
	}, nil
}

// Close 关闭服务，释放资源
func (s *UserService) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// demonstrateAdapterUsage 演示适配器使用模式
func demonstrateAdapterUsage() {
	// 配置示例
	cfg := &config.Config{
		DatabaseURL:  "postgresql://user:password@localhost:5432/useractivity",
		DBAdminURL:   "https://db-admin.example.com",
		DBAdminToken: "your-jwt-token-here",
	}

	// 创建用户服务
	userService, err := NewUserService(cfg)
	if err != nil {
		log.Fatalf("Failed to create user service: %v", err)
	}
	defer userService.Close()

	ctx := context.Background()

	// 1. 确保表结构存在（通过db-admin）
	if err := userService.EnsureTablesExist(ctx); err != nil {
		log.Printf("Warning: Failed to ensure tables exist: %v", err)
	}

	// 2. 获取数据库状态
	status, err := userService.GetDatabaseStatus(ctx)
	if err != nil {
		log.Printf("Warning: Failed to get database status: %v", err)
	} else {
		log.Printf("Database status: Mode=%v, Connected=%v",
			status.AdapterMode, status.ConnectionStatus.Connected)
	}

	// 3. 创建用户通知
	notification := &models.UserNotification{
		UserID:  "user123",
		Type:    "system",
		Title:   "欢迎使用",
		Message: "欢迎使用我们的服务！",
	}

	err = userService.CreateUserNotification(ctx, notification)
	if err != nil {
		log.Printf("Failed to create notification: %v", err)
	} else {
		log.Printf("Successfully created notification with ID: %d", notification.ID)
	}

	// 4. 获取用户通知列表
	notifications, err := userService.GetUserNotifications(ctx, "user123", 10)
	if err != nil {
		log.Printf("Failed to get notifications: %v", err)
	} else {
		log.Printf("Found %d notifications for user user123", len(notifications))
		for i, notif := range notifications {
			log.Printf("  %d. [%s] %s: %s", i+1, notif.Type, notif.Title, notif.Message)
		}
	}

	// 5. 更新通知状态
	err = userService.UpdateNotificationState(ctx, "user123", notification.ID)
	if err != nil {
		log.Printf("Failed to update notification state: %v", err)
	} else {
		log.Printf("Successfully updated notification state")
	}
}

// main 主函数
func main() {
	log.Println("=== UserActivity Service Adapter Integration Demo ===")
	log.Println("演示如何使用数据库适配器重构useractivity服务")

	demonstrateAdapterUsage()

	log.Println("=== Demo completed ===")
	log.Println("适配器优势:")
	log.Println("1. 🔄 渐进式迁移：支持从直连模式平滑过渡到API模式")
	log.Println("2. 🛡️ 安全降级：API不可用时自动回退到直连模式")
	log.Println("3. ⚡ 性能优化：智能选择查询执行方式")
	log.Println("4. 📊 监控集成：完整的查询日志和性能指标")
	log.Println("5. 🚀 零停机：服务重启时自动切换到最佳连接模式")
}