package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ScientificInternet/Google-Monetize/pkg/database"
)

// FinalAdapter 使用FinalAdapter的数据库适配器
type FinalAdapter struct {
	adapter database.DatabaseAdapter
	service string
}

// NewFinalAdapter 创建使用FinalAdapter的数据库适配器
func NewFinalAdapter() (*FinalAdapter, error) {
	// 使用GetFinalAdapterForService获取标准适配器
	adapter, err := database.GetFinalAdapterForService("batchopen")
	if err != nil {
		return nil, fmt.Errorf("failed to create final database adapter for batchopen service: %w", err)
	}

	return &FinalAdapter{
		adapter: adapter,
		service: "batchopen",
	}, nil
}

// Close 关闭数据库连接
func (a *FinalAdapter) Close() error {
	if closer, ok := a.adapter.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// GetAdapterMode 获取适配器模式
func (a *FinalAdapter) GetAdapterMode() string {
	return "final"
}

// GetService 获取服务名称
func (a *FinalAdapter) GetService() string {
	return a.service
}

// ExecuteInTransaction 在事务中执行操作
func (a *FinalAdapter) ExecuteInTransaction(ctx context.Context, fn func(*sql.Tx) error) error {
	// Get the underlying adapter and execute transaction
	if txAdapter, ok := a.adapter.(interface{ BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) }); ok {
		tx, err := txAdapter.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
				panic(r)
			}
		}()

		if err := fn(tx); err != nil {
			tx.Rollback()
			return err
		}

		return tx.Commit()
	}

	return fmt.Errorf("underlying adapter does not support transactions")
}

// QueryContext 执行查询并返回多行结果
func (a *FinalAdapter) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return a.adapter.Query(ctx, query, args...)
}

// QueryRowContext 执行查询并返回单行结果
func (a *FinalAdapter) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return a.adapter.QueryRow(ctx, query, args...)
}

// ExecContext 执行非查询操作
func (a *FinalAdapter) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return a.adapter.Exec(ctx, query, args...)
}

// Ping 检查数据库连接
func (a *FinalAdapter) Ping(ctx context.Context) error {
	return a.adapter.Ping(ctx)
}

// GetStats 获取数据库统计信息
func (a *FinalAdapter) GetStats() *sql.DBStats {
	// Try to get stats from underlying adapter if it's a SQL database
	if dbGetter, ok := a.adapter.(interface{ GetSupabaseDB() *sql.DB }); ok {
		if db := dbGetter.GetSupabaseDB(); db != nil {
			stats := db.Stats()
			return &stats
		}
	}

	// Return empty stats if no SQL database available
	return &sql.DBStats{}
}

// SetMaxOpenConns 设置最大打开连接数
func (a *FinalAdapter) SetMaxOpenConns(n int) {
	if setter, ok := a.adapter.(interface{ SetMaxOpenConns(int) }); ok {
		setter.SetMaxOpenConns(n)
	}
}

// SetMaxIdleConns 设置最大空闲连接数
func (a *FinalAdapter) SetMaxIdleConns(n int) {
	if setter, ok := a.adapter.(interface{ SetMaxIdleConns(int) }); ok {
		setter.SetMaxIdleConns(n)
	}
}

// SetConnMaxLifetime 设置连接最大生存时间
func (a *FinalAdapter) SetConnMaxLifetime(d time.Duration) {
	if setter, ok := a.adapter.(interface{ SetConnMaxLifetime(time.Duration) }); ok {
		setter.SetConnMaxLifetime(d)
	}
}

// SetConnMaxIdleTime 设置连接最大空闲时间
func (a *FinalAdapter) SetConnMaxIdleTime(d time.Duration) {
	if setter, ok := a.adapter.(interface{ SetConnMaxIdleTime(time.Duration) }); ok {
		setter.SetConnMaxIdleTime(d)
	}
}

// CreateBatchopenTask 创建批量操作任务
func (a *FinalAdapter) CreateBatchopenTask(ctx context.Context, taskID, userID, offerID string, simulationConfigJSON []byte) error {
	query := `INSERT INTO "BatchopenTask" (id, "userId", "offerId", "simulationConfig", status, "createdAt") VALUES ($1, $2, $3, $4, 'queued', NOW())`
	_, err := a.ExecContext(ctx, query, taskID, userID, offerID, simulationConfigJSON)
	return err
}

// UpdateTaskStatus 更新任务状态
func (a *FinalAdapter) UpdateTaskStatus(ctx context.Context, taskID, status string) error {
	query := `UPDATE "BatchopenTask" SET status = $1, "updatedAt" = NOW() WHERE id = $2`
	_, err := a.ExecContext(ctx, query, status, taskID)
	return err
}

// GetTask 获取任务信息
func (a *FinalAdapter) GetTask(ctx context.Context, taskID string) (*BatchopenTask, error) {
	query := `SELECT id, "userId", "offerId", "simulationConfig", status, "createdAt", "updatedAt" FROM "BatchopenTask" WHERE id = $1`
	row := a.QueryRowContext(ctx, query, taskID)

	var task BatchopenTask
	var simulationConfigJSON []byte
	err := row.Scan(&task.ID, &task.UserID, &task.OfferID, &simulationConfigJSON, &task.Status, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return nil, err
	}

	// 解析simulationConfig JSON
	if len(simulationConfigJSON) > 0 {
		if err := json.Unmarshal(simulationConfigJSON, &task.SimulationConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal simulationConfig: %w", err)
		}
	}

	return &task, nil
}

// GetUserTasks 获取用户的任务列表
func (a *FinalAdapter) GetUserTasks(ctx context.Context, userID string, limit int) ([]BatchopenTask, error) {
	query := `SELECT id, "userId", "offerId", "simulationConfig", status, "createdAt", "updatedAt"
          FROM "BatchopenTask" WHERE "userId" = $1 ORDER BY "createdAt" DESC LIMIT $2`
	rows, err := a.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []BatchopenTask
	for rows.Next() {
		var task BatchopenTask
		var simulationConfigJSON []byte
		err := rows.Scan(&task.ID, &task.UserID, &task.OfferID, &simulationConfigJSON, &task.Status, &task.CreatedAt, &task.UpdatedAt)
		if err != nil {
			return nil, err
		}

		// 解析simulationConfig JSON
		if len(simulationConfigJSON) > 0 {
			if err := json.Unmarshal(simulationConfigJSON, &task.SimulationConfig); err != nil {
				return nil, fmt.Errorf("failed to unmarshal simulationConfig: %w", err)
			}
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}