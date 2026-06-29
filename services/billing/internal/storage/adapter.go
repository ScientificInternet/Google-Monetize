package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ScientificInternet/Google-Monetize/pkg/database"
)

// Adapter 数据库适配器，支持CloudSQL和Supabase两种模式
type Adapter struct {
	adapter database.DatabaseAdapter
	service string
}

// NewAdapter 创建数据库适配器
func NewAdapter() (*Adapter, error) {
	// 🔧 修复：使用FinalAdapter替代旧的GetAdapterForService
	adapter, err := database.GetFinalAdapterForService("billing")
	if err != nil {
		return nil, fmt.Errorf("failed to create database adapter for billing service: %w", err)
	}

	return &Adapter{
		adapter: adapter,
		service: "billing",
	}, nil
}

// NewAdapterWithMode 使用指定模式创建数据库适配器（向后兼容）
func NewAdapterWithMode(mode database.AdapterMode) (*Adapter, error) {
	config := database.Config{
		ServiceName:    "billing",
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		Mode:           mode,
		MaxConnections: 20,
	}

	adapter, err := database.NewUniversalAdapter(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create database adapter: %w", err)
	}

	return &Adapter{
		adapter: adapter,
		service: "billing",
	}, nil
}

// GetDB 获取数据库连接
func (a *Adapter) GetDB(writeOperation bool) *sql.DB {
	switch a.adapter.GetMode() {
	case database.CloudSQLMode:
		if pool := a.adapter.GetCloudSQLPool(); pool != nil {
			// Convert pgxpool to sql.DB interface for backward compatibility
			// Note: This is a compatibility layer. New code should use GetCloudSQLPool directly
			return nil // pgxpool doesn't directly convert to *sql.DB
		}
	case database.SupabaseMode:
		return a.adapter.GetSupabaseDB()
	}
	return nil
}

// GetCloudSQLPool 获取Cloud SQL连接池
func (a *Adapter) GetCloudSQLPool() *pgxpool.Pool {
	return a.adapter.GetCloudSQLPool()
}

// GetSupabaseDB 获取Supabase数据库连接
func (a *Adapter) GetSupabaseDB() *sql.DB {
	return a.adapter.GetSupabaseDB()
}

// Close 关闭所有数据库连接
func (a *Adapter) Close() error {
	return a.adapter.Close()
}

// GetMode 获取当前连接模式
func (a *Adapter) GetMode() database.AdapterMode {
	return a.adapter.GetMode()
}

// Ping 测试数据库连接
func (a *Adapter) Ping(ctx context.Context) error {
	return a.adapter.Ping(ctx)
}

// IsHealthy 检查适配器是否健康
func (a *Adapter) IsHealthy(ctx context.Context) bool {
	return a.adapter.IsHealthy(ctx)
}