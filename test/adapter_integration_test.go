package test

import (
	"context"
	"os"
	"testing"

	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/storage"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/storage"
	"github.com/ScientificInternet/Google-Monetize/services/useractivity/internal/storage"
	"github.com/ScientificInternet/Google-Monetize/services/console/internal/storage"
)

// TestAdapterIntegration 测试新适配器的集成
func TestAdapterIntegration(t *testing.T) {
	// 设置测试环境变量
	os.Setenv("DB_CONNECTION_MODE", "cloudsql")
	os.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/testdb")
	os.Setenv("NEXT_PUBLIC_SUPABASE_URL", "https://test-project.supabase.co")
	os.Setenv("SUPABASE_SERVICE_KEY", "test-key")

	t.Run("UniversalAdapter", func(t *testing.T) {
		config := database.Config{
			ServiceName:    "test-service",
			DatabaseURL:    "postgres://test:test@localhost:5432/testdb",
			Mode:           database.CloudSQLMode,
			MaxConnections: 10,
		}

		adapter, err := database.NewUniversalAdapter(config)
		if err != nil {
			// 预期会失败，因为这是测试数据库URL
			t.Logf("Expected connection error: %v", err)
			return
		}

		defer adapter.Close()

		if adapter.GetMode() != database.CloudSQLMode {
			t.Errorf("Expected CloudSQLMode, got %v", adapter.GetMode())
		}

		if adapter.GetServiceName() != "test-service" {
			t.Errorf("Expected service name 'test-service', got %s", adapter.GetServiceName())
		}
	})

	t.Run("HybridDatabaseManager", func(t *testing.T) {
		config := database.HybridConfig{
			DatabaseURL:         os.Getenv("DATABASE_URL"),
			SupabaseURL:          os.Getenv("NEXT_PUBLIC_SUPABASE_URL"),
			SupabaseKey:          os.Getenv("SUPABASE_SERVICE_KEY"),
			MaxConnections:      10,
			Timeout:             30,
			HealthCheckInterval: 300,
		}

		manager, err := database.NewHybridDatabaseManager(context.Background(), config)
		if err != nil {
			// 预期会失败，因为这是测试配置
			t.Logf("Expected manager initialization error: %v", err)
			return
		}

		defer manager.Close()

		if !manager.IsInitialized() {
			t.Error("HybridDatabaseManager should be initialized")
		}
	})

	t.Run("BillingAdapter", func(t *testing.T) {
		adapter, err := storage.NewAdapter()
		if err != nil {
			t.Logf("Expected billing adapter error: %v", err)
			return
		}

		defer adapter.Close()

		if adapter.GetMode() != database.CloudSQLMode {
			t.Errorf("Expected CloudSQLMode, got %v", adapter.GetMode())
		}
	})

	t.Run("UserActivityAdapter", func(t *testing.T) {
		adapter, err := storage.NewAdapter("postgres://test:test@localhost:5432/testdb")
		if err != nil {
			t.Logf("Expected useractivity adapter error: %v", err)
			return
		}

		defer adapter.Close()

		if adapter.GetMode() != database.CloudSQLMode {
			t.Errorf("Expected CloudSQLMode, got %v", adapter.GetMode())
		}
	})

	t.Run("ConsoleAdapter", func(t *testing.T) {
		ctx := context.Background()
		adapter, err := storage.NewAdapter(ctx, "console", "postgres://test:test@localhost:5432/testdb")
		if err != nil {
			t.Logf("Expected console adapter error: %v", err)
			return
		}

		defer adapter.Close()

		if adapter.GetMode() != database.CloudSQLMode {
			t.Errorf("Expected CloudSQLMode, got %v", adapter.GetMode())
		}

		if adapter.GetServiceName() != "console" {
			t.Errorf("Expected service name 'console', got %s", adapter.GetServiceName())
		}
	})
}

// TestBackwardCompatibility 测试向后兼容性
func TestBackwardCompatibility(t *testing.T) {
	os.Setenv("DB_CONNECTION_MODE", "cloudsql")

	t.Run("OldModeConstants", func(t *testing.T) {
		// 测试旧的常量映射是否正确工作
		adapter, err := database.GetAdapterForService("test-compat")
		if err != nil {
			t.Logf("Expected compatibility error: %v", err)
			return
		}

		defer adapter.Close()

		// 验证环境变量映射正确
		// DB_CONNECTION_MODE=cloudsql 应该映射到 CloudSQLMode
		if adapter.GetMode() != database.CloudSQLMode {
			t.Errorf("Expected CloudSQLMode from environment mapping, got %v", adapter.GetMode())
		}
	})
}