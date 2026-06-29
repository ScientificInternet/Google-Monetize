package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ScientificInternet/Google-Monetize/pkg/database"
)

// Adapter 数据库适配器，支持从直接数据库连接逐步迁移到db-admin
type Adapter struct {
	db       *sql.DB
	mode     database.AdapterMode
	service  string
}

// NewAdapter 创建数据库适配器
func NewAdapter(databaseURL string) (*Adapter, error) {
	// 检查环境变量设置的模式
	modeStr := os.Getenv("DB_CONNECTION_MODE")
	var mode database.AdapterMode
	switch modeStr {
	case "cloudsql", "direct", "hybrid", "dbadmin", "":
		mode = database.CloudSQLMode // Default to Cloud SQL mode for batchopen service
	case "supabase":
		mode = database.SupabaseMode
	default:
		fmt.Printf("Warning: Invalid DB_CONNECTION_MODE '%s', defaulting to CloudSQL mode for service '%s'\n", modeStr, "batchopen")
		mode = database.CloudSQLMode
	}

	adapter := &Adapter{
		mode:    mode,
		service: "batchopen",
	}

	// 根据模式初始化连接
	switch mode {
	case database.CloudSQLMode:
		// 初始化Cloud SQL数据库连接
		db, err := sql.Open("postgres", databaseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Cloud SQL database: %w", err)
		}
		adapter.db = db

	case database.SupabaseMode:
		// 初始化Supabase数据库连接
		db, err := sql.Open("postgres", databaseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Supabase database: %w", err)
		}
		adapter.db = db

	default:
		// 默认使用Cloud SQL模式
		fmt.Printf("Warning: Unknown mode %v, defaulting to CloudSQL mode for service '%s'\n", mode, adapter.service)
		db, err := sql.Open("postgres", databaseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to database: %w", err)
		}
		adapter.db = db
		adapter.mode = database.CloudSQLMode
	}

	return adapter, nil
}

// GetDB 获取数据库连接
func (a *Adapter) GetDB() *sql.DB {
	return a.db
}

// Close 关闭所有数据库连接
func (a *Adapter) Close() error {
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			return err
		}
	}
	return nil
}

// Ping 测试连接
func (a *Adapter) Ping(ctx context.Context) error {
	if a.db != nil {
		return a.db.PingContext(ctx)
	}
	return fmt.Errorf("database connection not initialized")
}

// GetMode 获取当前模式
func (a *Adapter) GetMode() database.AdapterMode {
	return a.mode
}

// CreateBatchopenTask 创建批量操作任务
func (a *Adapter) CreateBatchopenTask(ctx context.Context, taskID, userID, offerID string, simulationConfigJSON []byte) error {
	if a.db == nil {
		return fmt.Errorf("database connection not initialized")
	}

	query := `INSERT INTO "BatchopenTask" (id, "userId", "offerId", "simulationConfig", status, "createdAt") VALUES ($1, $2, $3, $4, 'queued', NOW())`
	_, err := a.db.ExecContext(ctx, query, taskID, userID, offerID, simulationConfigJSON)
	return err
}

// UpdateTaskStatus 更新任务状态
func (a *Adapter) UpdateTaskStatus(ctx context.Context, taskID, status string) error {
	if a.db == nil {
		return fmt.Errorf("database connection not initialized")
	}

	query := `UPDATE "BatchopenTask" SET status = $1, "updatedAt" = NOW() WHERE id = $2`
	_, err := a.db.ExecContext(ctx, query, status, taskID)
	return err
}

// GetTask 获取任务信息
func (a *Adapter) GetTask(ctx context.Context, taskID string) (*BatchopenTask, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database connection not initialized")
	}

	query := `SELECT id, "userId", "offerId", "simulationConfig", status, "createdAt", "updatedAt" FROM "BatchopenTask" WHERE id = $1`
	row := a.db.QueryRowContext(ctx, query, taskID)

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
func (a *Adapter) GetUserTasks(ctx context.Context, userID string, limit int) ([]BatchopenTask, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database connection not initialized")
	}

	query := `SELECT id, "userId", "offerId", "simulationConfig", status, "createdAt", "updatedAt"
	          FROM "BatchopenTask" WHERE "userId" = $1 ORDER BY "createdAt" DESC LIMIT $2`
	rows, err := a.db.QueryContext(ctx, query, userID, limit)
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

// BatchopenTask 批量操作任务结构
type BatchopenTask struct {
	ID               string                 `json:"id"`
	UserID           string                 `json:"userId"`
	OfferID          string                 `json:"offerId"`
	SimulationConfig map[string]interface{} `json:"simulationConfig"`
	Status           string                 `json:"status"`
	CreatedAt        sql.NullTime           `json:"createdAt"`
	UpdatedAt        sql.NullTime           `json:"updatedAt"`
}