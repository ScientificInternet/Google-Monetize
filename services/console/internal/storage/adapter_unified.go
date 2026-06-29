package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UnifiedAdapter 统一数据库适配器，支持多种模式
type UnifiedAdapter struct {
	// 使用统一数据库适配器
	uniAdapter database.DatabaseAdapter

	// 保留pgxpool以兼容现有代码
	pgxpool   *pgxpool.Pool
	service   string
}

// NewUnifiedAdapter 创建Console服务的统一数据库适配器
func NewUnifiedAdapter(ctx context.Context, service string, databaseURL string) (*UnifiedAdapter, error) {
	// 🔧 修复：使用FinalAdapter替代旧的GetAdapterForService
	uniAdapter, err := database.GetFinalAdapterForService(service)
	if err != nil {
		return nil, fmt.Errorf("failed to create universal adapter: %w", err)
	}

	adapter := &UnifiedAdapter{
		uniAdapter: uniAdapter,
		service:     service,
	}

	// 如果是直接模式，初始化pgxpool以兼容现有代码
	if uniAdapter.GetMode() == database.DirectMode || uniAdapter.GetMode() == database.HybridMode {
		pgxpool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			uniAdapter.Close()
			return nil, fmt.Errorf("failed to create pgxpool connection: %w", err)
		}
		adapter.pgxpool = pgxpool

		log.Printf("Console unified adapter initialized in %s mode", uniAdapter.GetMode())
	} else {
		log.Printf("Console unified adapter initialized in DBAdmin mode")
	}

	// 测试连接
	if err := uniAdapter.Ping(ctx); err != nil {
		adapter.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return adapter, nil
}

// GetMode 获取适配器模式
func (a *UnifiedAdapter) GetMode() database.AdapterMode {
	return a.uniAdapter.GetMode()
}

// GetServiceName 获取服务名称
func (a *UnifiedAdapter) GetServiceName() string {
	return a.uniAdapter.GetServiceName()
}

// GetPgxPool 获取pgxpool连接（兼容性方法）
func (a *UnifiedAdapter) GetPgxPool() *pgxpool.Pool {
	if a.pgxpool != nil {
		return a.pgxpool
	}
	return nil
}

// GetDB 获取sql.DB连接（兼容性方法）
func (a *UnifiedAdapter) GetDB() *sql.DB {
	// 在DBAdmin模式下，此方法返回nil，服务需要使用统一接口
	if a.uniAdapter.GetMode() == database.DirectMode || a.uniAdapter.GetMode() == database.HybridMode {
		log.Printf("WARNING: GetDB() is deprecated in console service. Use Query/QueryRow/Exec methods instead.")
		// TODO: 从uniAdapter获取底层sql.DB连接
		return nil
	}
	return nil
}

// Query 执行查询（统一接口）
func (a *UnifiedAdapter) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return a.uniAdapter.Query(ctx, query, args...)
}

// QueryRow 执行查询并返回单行（统一接口）
func (a *UnifiedAdapter) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return a.uniAdapter.QueryRow(ctx, query, args...)
}

// Exec 执行语句（统一接口）
func (a *UnifiedAdapter) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return a.uniAdapter.Exec(ctx, query, args...)
}

// Ping 检查连接
func (a *UnifiedAdapter) Ping(ctx context.Context) error {
	return a.uniAdapter.Ping(ctx)
}

// Close 关闭适配器
func (a *UnifiedAdapter) Close() error {
	if a.pgxpool != nil {
		a.pgxpool.Close()
	}
	return a.uniAdapter.Close()
}

// IsHealthy 检查适配器是否健康
func (a *UnifiedAdapter) IsHealthy(ctx context.Context) bool {
	return a.uniAdapter.IsHealthy(ctx)
}

// BeginTx 开始事务
func (a *UnifiedAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return a.uniAdapter.BeginTx(ctx, opts)
}

// 兼容性别名，用于逐步迁移
type Adapter = UnifiedAdapter

// NewAdapter 兼容性构造函数
func NewAdapter(ctx context.Context, service string, databaseURL string) (*UnifiedAdapter, error) {
	// 检查是否应该使用新的统一适配器
	useUnified := os.Getenv("USE_UNIFIED_ADAPTER")
	if useUnified == "true" || useUnified == "1" {
		return NewUnifiedAdapter(ctx, service, databaseURL)
	}

	// 否则使用原有的适配器创建逻辑
	return createLegacyAdapter(ctx, service, databaseURL)
}

// createLegacyAdapter 创建传统适配器（向后兼容）
func createLegacyAdapter(ctx context.Context, service string, databaseURL string) (*UnifiedAdapter, error) {
	// 这里实现原有的适配器逻辑
	// 为了简化，我们直接使用统一适配器但设置为DirectMode

	config := database.Config{
		ServiceName: service,
		DatabaseURL: databaseURL,
		Mode:        database.DirectMode,
		Timeout:     30 * time.Second,
		MaxConnections: 20,
	}

	uniAdapter, err := database.NewUniversalAdapter(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create legacy adapter: %w", err)
	}

	adapter := &UnifiedAdapter{
		uniAdapter: uniAdapter,
		service:     service,
	}

	// 初始化pgxpool
	pgxpool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		uniAdapter.Close()
		return nil, fmt.Errorf("failed to create pgxpool connection: %w", err)
	}
	adapter.pgxpool = pgxpool

	log.Printf("Console legacy adapter initialized in DirectMode")

	return adapter, nil
}