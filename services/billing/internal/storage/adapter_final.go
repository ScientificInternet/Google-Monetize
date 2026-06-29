package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ScientificInternet/Google-Monetize/pkg/database"
)

// FinalAdapterService Billing服务的最终适配器服务
// 完全基于DATABASE_ARCHITECTURE_CURRENT.md的最终架构状态
type FinalAdapterService struct {
	adapter database.DatabaseAdapter
	service string
}

// NewFinalAdapterService 创建Billing服务的最终适配器服务
func NewFinalAdapterService() (*FinalAdapterService, error) {
	// 使用最终适配器，不再使用任何Supabase数据库连接
	adapter, err := database.GetFinalAdapterForService("billing")
	if err != nil {
		return nil, fmt.Errorf("failed to create final adapter for billing service: %w", err)
	}

	return &FinalAdapterService{
		adapter:  adapter,
		service: "billing",
	}, nil
}

// === 实现DatabaseAdapter接口 ===

// Query 执行查询并返回多行结果（基于最终适配器）
func (s *FinalAdapterService) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return s.adapter.Query(ctx, query, args...)
}

// QueryRow 执行查询并返回单行结果
func (s *FinalAdapterService) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return s.adapter.QueryRow(ctx, query, args...)
}

// Exec 执行语句并返回结果
func (s *FinalAdapterService) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return s.adapter.Exec(ctx, query, args...)
}

// BeginTx 开始事务
func (s *FinalAdapterService) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return s.adapter.BeginTx(ctx, opts)
}

// === 连接管理方法 ===

// Ping 测试数据库连接
func (s *FinalAdapterService) Ping(ctx context.Context) error {
	return s.adapter.Ping(ctx)
}

// Close 关闭数据库连接
func (s *FinalAdapterService) Close() error {
	return s.adapter.Close()
}

// === 配置和状态方法 ===

// GetMode 获取适配器模式
func (s *FinalAdapterService) GetMode() database.AdapterMode {
	return s.adapter.GetMode()
}

// GetServiceName 获取服务名称
func (s *FinalAdapterService) GetServiceName() string {
	return s.adapter.GetServiceName()
}

// IsHealthy 检查适配器是否健康
func (s *FinalAdapterService) IsHealthy(ctx context.Context) bool {
	return s.adapter.IsHealthy(ctx)
}

// === 性能优化方法 ===

// GetCloudSQLPool ��取Cloud SQL连接池（用于性能优化场景）
func (s *FinalAdapterService) GetCloudSQLPool() interface{} {
	// 最终适配器提供高性能的pgxpool访问
	if finalAdapter, ok := s.adapter.(*database.FinalAdapter); ok {
		return finalAdapter.GetCloudSQLPool()
	}
	return nil
}

// BeginTxReadOnly 开始只读事务（适用于报告查询等场景）
func (s *FinalAdapterService) BeginTxReadOnly(ctx context.Context) (*sql.Tx, error) {
	if finalAdapter, ok := s.adapter.(*database.FinalAdapter); ok {
		return finalAdapter.BeginTxReadOnly(ctx)
	}
	return nil, fmt.Errorf("adapter does not support read-only transactions")
}

// === 向后兼容方法 ===

// GetDB 获取数据库连接（向后兼容，已弃用）
func (s *FinalAdapterService) GetDB() *sql.DB {
	fmt.Printf("WARNING: GetDB() is deprecated in billing final adapter. Use Query/QueryRow/Exec methods instead.")
	return nil
}

// === 便捷方法 ===

// ExecuteTransaction 执行事务操作（简化常见事务模式）
func (s *FinalAdapterService) ExecuteTransaction(ctx context.Context, operations []func(*sql.Tx) error) error {
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			fmt.Printf("Warning: failed to rollback transaction: %v", err)
		}
	}()

	// 执行所有操作
	for _, op := range operations {
		if err := op(tx); err != nil {
			return err
		}
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ExecuteBatch 执行批量操作（优化性能）
func (s *FinalAdapterService) ExecuteBatch(ctx context.Context, queries []string, args [][]interface{}) ([]sql.Result, error) {
	results := make([]sql.Result, len(queries))

	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin batch transaction: %w", err)
	}
	defer tx.Rollback()

	// 执行批量操作
	for i, query := range queries {
		var queryArgs interface{} = args[i]
		if queryArgs == nil {
			queryArgs = []interface{}{}
		}

		result, err := tx.ExecContext(ctx, query, queryArgs.([]interface{})...)
		if err != nil {
			return nil, fmt.Errorf("failed to execute batch query %d: %w", i, err)
		}
		results[i] = result
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit batch transaction: %w", err)
	}

	return results, nil
}

// === 健康检查和监控 ===

// GetConnectionStats 获取连接池统计信息（用于监控）
func (s *FinalAdapterService) GetConnectionStats() map[string]interface{} {
	if finalAdapter, ok := s.adapter.(*database.FinalAdapter); ok {
		// 模拟获取连接池统计（pgxpool实际提供了统计方法）
		return map[string]interface{}{
			"adapter_type": "final",
			"connection_pool": "available",
			"database_mode": s.adapter.GetMode().String(),
			"service_name": s.service,
			"status": "healthy",
		}
	}

	return map[string]interface{}{
		"adapter_type": "unknown",
		"status": "using_legacy_adapter",
	}
}