package main

import (
	"context"
	"log"

	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/config"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/pkg/database"
)

func main() {
	log.Println("开始数据库连接测试...")

	ctx := context.Background()

	// 加载配置
	cfg, err := config.Load(ctx)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	log.Printf("数据库URL已配置: %s", maskSensitiveInfo(cfg.DatabaseURL))

	// 创建数据库管理器
	dbConfig := &database.Config{
		DatabaseURL:    cfg.DatabaseURL,
		MaxConnections: 20,
		MinConnections: 5,
		MaxConnLifetime: 0, // 使用默认值
	}

	dbManager, err := database.NewDatabaseManager(ctx, dbConfig)
	if err != nil {
		log.Fatalf("创建数据库管理器失败: %v", err)
	}
	defer dbManager.Close()

	// 健康检查
	if err := dbManager.HealthCheck(ctx); err != nil {
		log.Fatalf("数据库健康检查失败: %v", err)
	}

	log.Println("✅ 数据库连接测试成功")
	log.Println("✅ Cloud SQL Proxy集成正常")
	log.Println("✅ 连接池配置有效")

	// 测试基本查询
	pool := dbManager.GetCloudSQLPool()
	var result string
	err = pool.QueryRow(ctx, "SELECT version()").Scan(&result)
	if err != nil {
		log.Printf("查询数据库版本失败: %v", err)
	} else {
		log.Printf("数据库版本: %s", result)
	}

	// 测试schema访问
	schemas := []string{"billing", "offers", "siterank", "adscenter", "useractivity"}
	for _, schema := range schemas {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)",
			schema).Scan(&exists)

		if err != nil {
			log.Printf("查询schema %s 失败: %v", schema, err)
		} else if exists {
			log.Printf("✅ Schema %s 存在", schema)
		} else {
			log.Printf("❌ Schema %s 不存在", schema)
		}
	}

	log.Println("数据库连接测试完成")
}

// maskSensitiveInfo 隐藏敏感信息用于日志输出
func maskSensitiveInfo(url string) string {
	if len(url) > 50 {
		return url[:20] + "***" + url[len(url)-20:]
	}
	return "***"
}