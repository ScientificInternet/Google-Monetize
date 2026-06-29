//go:build integration
// +build integration

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/config"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/pkg/integration"
)

func main() {
	log.Println("开始混合数据库架构测试...")
	log.Println("架构: Cloud SQL (业务数据) + Supabase (认证数据)")

	ctx := context.Background()

	// 加载配置
	cfg, err := config.Load(ctx)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	log.Printf("Cloud SQL: %s", maskSensitiveInfo(cfg.DatabaseURL))
	log.Printf("Supabase URL: %s", cfg.SupabaseURL)

	// 创建混合数据库管理器
	hdm, err := integration.NewHybridDatabaseManager(ctx, cfg)
	if err != nil {
		log.Fatalf("创建混合数据库管理器失败: %v", err)
	}
	defer hdm.Close()

	// 健康检查
	if err := hdm.HealthCheck(ctx); err != nil {
		log.Fatalf("健康检查失败: %v", err)
	}

	log.Println("✅ 混合数据库架构连接成功")

	// 获取数据库统计
	stats, err := hdm.GetStats(ctx)
	if err != nil {
		log.Printf("获取统计信息失败: %v", err)
	} else {
		log.Println("📊 数据库统计:")
		for key, value := range stats {
			log.Printf("  %s: %v", key, value)
		}
	}

	// 测试用户验证（如果提供了测试用户ID）
	testUserID := os.Getenv("TEST_USER_ID")
	if testUserID != "" {
		log.Printf("测试用户验证: %s", testUserID)

		userInfo, err := hdm.ValidateUser(ctx, testUserID)
		if err != nil {
			log.Printf("用户验证失败: %v", err)
		} else {
			log.Printf("✅ 用户验证成功:")
			log.Printf("  用户ID: %s", userInfo.UserID)
			log.Printf("  邮箱: %s", userInfo.Email)
			log.Printf("  余额: %d", userInfo.Balance)
			log.Printf("  订阅: %s", userInfo.subscriptions)
			log.Printf("  创建时间: %s", userInfo.CreatedAt)
		}
	} else {
		log.Println("💡 提示: 设置 TEST_USER_ID 环境变量来测试用户验证")
	}

	// 性能测试
	log.Println("🚀 开始性能测试...")
	start := time.Now()

	// 测试并发查询
	concurrency := 10
	done := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < 5; j++ {
				stats, err := hdm.GetStats(ctx)
				if err != nil {
					log.Printf("Worker %d: 获取统计失败: %v", id, err)
					return
				}

				// 模拟一些处理时间
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < concurrency; i++ {
		<-done
	}

	duration := time.Since(start)
	log.Printf("✅ 性能测试完成: %v (并发度: %d, 查询数: %d)",
		duration, concurrency, concurrency*5)

	// 显示连接池状态
	finalStats, _ := hdm.GetStats(ctx)
	if poolMaxConns, ok := finalStats["pool_max_conns"]; ok {
		if poolTotalConns, ok := finalStats["pool_total_conns"]; ok {
			if poolIdleConns, ok := finalStats["pool_idle_conns"]; ok {
				log.Printf("📈 连接池状态:")
				log.Printf("  最大连接数: %v", poolMaxConns)
				log.Printf("  总连接数: %v", poolTotalConns)
				log.Printf("  空闲连接数: %v", poolIdleConns)
			}
		}
	}

	log.Println("🎉 混合数据库架构测试完成!")
	log.Println("🔗 Cloud SQL (业务数据) + Supabase (认证数据) 集成成功")
}

// maskSensitiveInfo 隐藏敏感信息用于日志输出
func maskSensitiveInfo(url string) string {
	if len(url) > 50 {
		return url[:20] + "***" + url[len(url)-20:]
	}
	return "***"
}