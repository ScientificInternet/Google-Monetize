package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ScientificInternet/Google-Monetize/pkg/database"
)

// PGXAdapter Billing服务的PGX兼容适配器
// 解决原有混合适配器的类型兼容性问题
type PGXAdapter struct {
	adapter database.DatabaseAdapter
	service string
}

// NewPGXAdapter 创建Billing服务的PGX兼容适配器
func NewPGXAdapter() (*PGXAdapter, error) {
	// 🔧 修复：使用FinalAdapter替代旧的GetPGXCompatibleAdapterForService
	adapter, err := database.GetFinalAdapterForService("billing")
	if err != nil {
		return nil, fmt.Errorf("failed to create PGX compatible adapter for billing service: %w", err)
	}

	return &PGXAdapter{
		adapter: adapter,
		service: "billing",
	}, nil
}

// === 兼容sql.*接口的方法 ===

// Query 执行查询并返回多行结果
func (a *PGXAdapter) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return a.adapter.Query(ctx, query, args...)
}

// QueryRow 执行查询并返回单行结果
func (a *PGXAdapter) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return a.adapter.QueryRow(ctx, query, args...)
}

// Exec 执行语句并返回结果
func (a *PGXAdapter) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return a.adapter.Exec(ctx, query, args...)
}

// BeginTx 开始事务
func (a *PGXAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return a.adapter.BeginTx(ctx, opts)
}

// === 连接管理方法 ===

// Ping 测试数据库连接
func (a *PGXAdapter) Ping(ctx context.Context) error {
	return a.adapter.Ping(ctx)
}

// Close 关闭数据库连接
func (a *PGXAdapter) Close() error {
	return a.adapter.Close()
}

// === 配置和状态方法 ===

// GetMode 获取适配器模式
func (a *PGXAdapter) GetMode() database.AdapterMode {
	return a.adapter.GetMode()
}

// GetServiceName 获取服务名称
func (a *PGXAdapter) GetServiceName() string {
	return a.adapter.GetServiceName()
}

// IsHealthy 检查适配器是否健康
func (a *PGXAdapter) IsHealthy(ctx context.Context) bool {
	return a.adapter.IsHealthy(ctx)
}

// === 性能优化方法 ===

// GetCloudSQLPool 获取Cloud SQL连接池（用于性能优化场景）
func (a *PGXAdapter) GetCloudSQLPool() *sql.DB {
	// 注意：这里返回nil因为PGX适配器已经包装了pgxpool
	// 如果需要直接访问pgxpool，可以使用类型断言
	if pgxAdapter, ok := a.adapter.(*database.PGXCompatibleAdapter); ok {
		return &PGXPoolWrapper{pool: pgxAdapter.GetCloudSQLPool()}
	}
	return nil
}

// GetSupabaseDB 获取Supabase数据库连接（用于特殊场景）
func (a *PGXAdapter) GetSupabaseDB() *sql.DB {
	if pgxAdapter, ok := a.adapter.(*database.PGXCompatibleAdapter); ok {
		return pgxAdapter.GetSupabaseDB()
	}
	return nil
}

// === 向后兼容方法 ===

// 为了与现有代码兼容，提供一些便捷方法

// GetDB 获取数据库连��（向后兼容）
// 已弃用：推荐直接使用Query/QueryRow/Exec方法
func (a *PGXAdapter) GetDB(writeOperation bool) *sql.DB {
	fmt.Printf("WARNING: GetDB() is deprecated in billing service. Use Query/QueryRow/Exec methods instead.")
	return nil
}

// GetCloudSQLPoolPGX 获取Cloud SQL连接池（PGX专用）
func (a *PGXAdapter) GetCloudSQLPoolPGX() interface{} {
	if pgxAdapter, ok := a.adapter.(*database.PGXCompatibleAdapter); ok {
		return pgxAdapter.GetCloudSQLPool()
	}
	return nil
}

// === 特殊包装器 ===

// PGXPoolWrapper 为需要*sql.DB的代码提供兼容性
type PGXPoolWrapper struct {
	pool interface{} // 使用interface{}避免循环依赖
}

// Begin 开始事务（PGXPool专用）
func (w *PGXPoolWrapper) Begin() (interface{}, error) {
	if pgxAdapter, ok := w.pool.(*database.PGXCompatibleAdapter); ok {
		return pgxAdapter.BeginTx(context.Background(), nil)
	}
	return nil, fmt.Errorf("pool is not a PGXCompatibleAdapter")
}

// PGXPoolWrapper 为需要*sql.DB的代码提供兼容性
type PGXPoolWrapper struct {
	pool *database.PGXCompatibleAdapter
}

func NewPGXPoolWrapper(pool *database.PGXCompatibleAdapter) *PGXPoolWrapper {
	return &PGXPoolWrapper{pool: pool}
}

// Exec 执行语句
func (w *PGXPoolWrapper) Exec(ctx context.Context, query string, args ...interface{}) (interface{}, error) {
	return w.pool.Exec(ctx, query, args...)
}

// Query 执行查询
func (w *PGXPoolWrapper) Query(ctx context.Context, query string, args ...interface{}) (interface{}, error) {
	return w.pool.Query(ctx, query, args...)
}

// QueryRow 执行单行查询
func (w *PGXPoolWrapper) QueryRow(ctx context.Context, query string, args ...interface{}) interface{} {
	return w.pool.QueryRow(ctx, query, args...)
}

// Close 关闭连接
func (w *PGXPoolWrapper) Close() error {
	return w.pool.Close()
}

// Ping 测试连接
func (w *PGXPoolWrapper) Ping(ctx context.Context) error {
	return w.pool.Ping(ctx)
}

// === 迁移辅助函数 ===

// MigrateFromLegacyAdapter 从旧适配器迁移到新适配器
func (a *PGXAdapter) MigrateFromLegacyAdapter(legacyAdapter interface{}) {
	fmt.Printf("Migrating billing service from legacy adapter to PGX compatible adapter")
	// 这里可以添加数据迁移或配置转换逻辑
}