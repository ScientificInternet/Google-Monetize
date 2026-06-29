package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DatabaseManager 简化版数据库管理器
type DatabaseManager struct {
	cloudSQLPool *pgxpool.Pool
	logger       *log.Logger
}

// Config 数据库配置
type Config struct {
	DatabaseURL    string
	MaxConnections int
	MinConnections int
	MaxConnLifetime time.Duration
}

// NewDatabaseManager 创建数据库管理器实例
func NewDatabaseManager(ctx context.Context, cfg *Config) (*DatabaseManager, error) {
	// 创建 Cloud SQL 连接池
	pool, err := createCloudSQLPool(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloud SQL pool: %w", err)
	}

	return &DatabaseManager{
		cloudSQLPool: pool,
		logger:       log.Default(),
	}, nil
}

// createCloudSQLPool 创建 Cloud SQL pgxpool 连接池
func createCloudSQLPool(ctx context.Context, cfg *Config) (*pgxpool.Pool, error) {
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = 20
	}
	if cfg.MinConnections == 0 {
		cfg.MinConnections = 5
	}
	if cfg.MaxConnLifetime == 0 {
		cfg.MaxConnLifetime = time.Hour
	}

	// 解析连接字符串并配置连接池
	config, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// 优化连接池参数
	config.MaxConns = int32(cfg.MaxConnections)
	config.MinConns = int32(cfg.MinConnections)
	config.MaxConnLifetime = cfg.MaxConnLifetime
	config.HealthCheckPeriod = 30 * time.Second

	return pgxpool.NewWithConfig(ctx, config)
}

// GetCloudSQLPool 获取 Cloud SQL 连接池
func (dm *DatabaseManager) GetCloudSQLPool() *pgxpool.Pool {
	return dm.cloudSQLPool
}

// Close 关闭所有连接
func (dm *DatabaseManager) Close() {
	if dm.cloudSQLPool != nil {
		dm.cloudSQLPool.Close()
		dm.logger.Println("Cloud SQL connection pool closed")
	}
}

// HealthCheck 健康检查
func (dm *DatabaseManager) HealthCheck(ctx context.Context) error {
	// 检查 Cloud SQL 连接
	if err := dm.cloudSQLPool.Ping(ctx); err != nil {
		return fmt.Errorf("Cloud SQL health check failed: %w", err)
	}

	dm.logger.Println("Database health check passed")
	return nil
}