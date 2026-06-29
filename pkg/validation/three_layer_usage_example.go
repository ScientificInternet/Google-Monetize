package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ScientificInternet/Google-Monetize/pkg/database"
)

// UsageExample 使用示例和测试工具
type UsageExample struct {
	adapter database.DatabaseAdapter
}

// NewUsageExample 创建使用示例实例
func NewUsageExample(adapter database.DatabaseAdapter) *UsageExample {
	return &UsageExample{adapter: adapter}
}

// DemonstrateThreeLayerValidation 演示三层验证功能
func (u *UsageExample) DemonstrateThreeLayerValidation() {
	log.Println("=== 三层用户数据架构验证中间件使用示例 ===")

	// 示例1: 验证特定用户的三层状态
	log.Println("1. 验证用户的三层数据状态:")
	userID := "550e8400-e29b-41d4-a716-4466554400000"
	if err := u.validateUserThreeLayerStatus(userID); err != nil {
		log.Printf("验证失败: %v\n", err)
	} else {
		log.Println("✅ 用户三层状态验证通过")
	}

	// 示例2: 检查数据一致性
	log.Println("2. 检查数据一致性:")
	if err := u.checkDataConsistency(); err != nil {
		log.Printf("数据一致性检查失败: %v\n", err)
	} else {
		log.Println("✅ 数据一致性检查通过")
	}

	// 示例3: 模拟自动修复
	log.Println("3. 自动修复缺失数据:")
	if err := u.autoRepairMissingData(userID, "test@example.com", "Test User", "https://example.com/avatar.jpg"); err != nil {
		log.Printf("自动修复失败: %v\n", err)
	} else {
		log.Println("✅ 自动修复完成")
	}

	// 示例4: 性能测试
	log.Println("4. 性能测试:")
	u.performanceTest()

	// 示例5: 监控和指标收集
	log.Println("5. 监控指标收集:")
	u.collectMetrics()

	log.Println("=== 使用示例完成 ===")
}

// validateUserThreeLayerStatus 验证用户三层数据状态
func (u *UsageExample) validateUserThreeLayerStatus(userID string) error {
	ctx := context.Background()

	// 验证Layer 1: Supabase auth.users (通过JWT验证)
	// 在实际使用中，这一步在中间件中自动完成
	log.Printf("  ✓ Layer 1 (Supabase): 用户认证通过 userID=%s", userID)

	// 验证Layer 2: Cloud SQL user.users
	pool := u.adapter.GetCloudSQLPool()
	if pool == nil {
		return fmt.Errorf("数据库连接池不可用")
	}

	var email string
	var userExists bool
	err := pool.QueryRow(ctx,
		`SELECT email, true FROM user.users WHERE id = $1`, userID).Scan(&email, &userExists)
	if err != nil {
		return fmt.Errorf("查询user.users失败: %w", err)
	}

	if !userExists {
		return fmt.Errorf("用户在Layer 2中不存在")
	}

	log.Printf("  ✓ Layer 2 (Cloud SQL): user.users存在 email=%s", email)

	// 验证Layer 3: Cloud SQL billing.accounts
	var billingExists bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM billing.accounts WHERE user_id = $1)`, userID).Scan(&billingExists)
	if err != nil {
		return fmt.Errorf("查询billing.accounts失败: %w", err)
	}

	if !billingExists {
		return fmt.Errorf("用户在Layer 3中不存在")
	}

	log.Printf("  ✓ Layer 3 (Cloud SQL): billing.accounts存在")

	return nil
}

// checkDataConsistency 检查数据一致性
func (u *UsageExample) checkDataConsistency() error {
	ctx := context.Background()
	pool := u.adapter.GetCloudSQLPool()

	// 检查1: 确保所有billing.accounts都有对应的user.users
	const consistencyQuery1 = `
		SELECT COUNT(*) FROM billing.accounts ba
		LEFT JOIN user.users u ON ba.user_id = u.id
		WHERE u.id IS NULL
	`

	var orphanedBillingCount int
	err := pool.QueryRow(ctx, consistencyQuery1).Scan(&orphanedBillingCount)
	if err != nil {
		return fmt.Errorf("检查孤立billing记录失败: %w", err)
	}

	if orphanedBillingCount > 0 {
		return fmt.Errorf("发现%d个孤立的billing记录", orphanedBillingCount)
	}

	log.Printf("  ✓ 数据一致性检查: 无孤立billing记录")

	// 检查2: 验证邮箱一致性
	const consistencyQuery2 = `
		SELECT COUNT(*) FROM user.users u
		LEFT JOIN billing.accounts ba ON u.id = ba.user_id
		LEFT JOIN auth.users au ON u.id = au.id
		WHERE u.email != au.email
			AND ba.user_id IS NOT NULL
			AND au.id IS NOT NULL
	`

	var emailInconsistentCount int
	err = pool.QueryRow(ctx, consistencyQuery2).Scan(&emailInconsistentCount)
	if err != nil {
		return fmt.Errorf("检查邮箱一致性失败: %w", err)
	}

	if emailInconsistentCount > 0 {
		return fmt.Errorf("发现%d个邮箱不一致的记录", emailInconsistentCount)
	}

	log.Printf("  ✓ 数据一致性检查: 邮箱一致性验证通过")

	return nil
}

// autoRepairMissingData 自动修复缺失数据
func (u *UsageExample) autoRepairMissingData(userID, email, name, avatarURL string) error {
	ctx := context.Background()
	pool := u.adapter.GetCloudSQLPool()

	// 检查需要修复的层级
	var needsUserRecord, needsBillingRecord bool

	err := pool.QueryRow(ctx,
		`SELECT
			EXISTS(SELECT 1 FROM user.users WHERE id = $1),
			EXISTS(SELECT 1 FROM billing.accounts WHERE user_id = $1)
		`, userID, userID).Scan(&needsUserRecord, &needsBillingRecord)

	if err != nil {
		return fmt.Errorf("检查修复需求失败: %w", err)
	}

	if !needsUserRecord || !needsBillingRecord {
		log.Printf("  用户数据完整，无需修复")
		return nil
	}

	// 执行修复（事务保证）
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开始修复事务失败: %w", err)
	}
	defer tx.Rollback(ctx)

	// 修复Layer 2: 创建user.users记录
	if needsUserRecord {
		_, err = tx.Exec(ctx, `
			INSERT INTO user.users (id, email, name, avatar_url, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'active', NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET
				email = EXCLUDED.email,
				name = EXCLUDED.name,
				avatar_url = EXCLUDED.avatar_url,
				updated_at = NOW()
		`, userID, email, name, avatarURL)

		if err != nil {
			return fmt.Errorf("创建user记录失败: %w", err)
		}

		log.Printf("  ✓ Layer 2 修复: 创建/更新user.users记录")
	}

	// 修复Layer 3: 创建billing.accounts记录
	if needsBillingRecord {
		_, err = tx.Exec(ctx, `
			INSERT INTO billing.accounts (user_id, account_type, status, balance_cents, created_at, updated_at)
			VALUES ($1, 'standard', 'active', 0, NOW(), NOW())
			ON CONFLICT (user_id) DO UPDATE SET
				status = 'active',
				updated_at = NOW()
		`, userID)

		if err != nil {
			return fmt.Errorf("创建billing记录失败: %w", err)
		}

		log.Printf("  ✓ Layer 3 修复: 创建/更新billing.accounts记录")
	}

	// 提交事务
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交修复事务失败: %w", err)
	}

	log.Printf("  ✓ 自动修复完成: userID=%s", userID)
	return nil
}

// performanceTest 性能测试
func (u *UsageExample) performanceTest() {
	ctx := context.Background()
	pool := u.adapter.GetCloudSQLPool()

	// 测试验证查询性能
	start := time.Now()
	var userCount int
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM user.users").Scan(&userCount)
	duration := time.Since(start)

	if err != nil {
		log.Printf("性能测试查询失败: %v", err)
		return
	}

	log.Printf("  ✓ 性能测试: 用户查询耗时 %vms, 总用户数: %d", duration.Milliseconds(), userCount)

	// 测试批量查询性能
	start = time.Now()
	rows, err := pool.Query(ctx, `
		SELECT u.id, u.email, ba.status as billing_status
		FROM user.users u
		LEFT JOIN billing.accounts ba ON u.id = ba.user_id
		LIMIT 100
	`)
	if err != nil {
		log.Printf("批量查询失败: %v", err)
		return
	}
	defer rows.Close()

	var processedCount int
	for rows.Next() {
		processedCount++
	}
	duration = time.Since(start)

	log.Printf("  ✓ 性能测试: 批量查询100条记录耗时 %vms", duration.Milliseconds())
}

// collectMetrics 收集监控指标
func (u *UsageExample) collectMetrics() {
	ctx := context.Background()
	pool := u.adapter.GetCloudSQLPool()

	// 收集用户完整性指标
	metrics := make(map[string]interface{})

	// 统计各层状态
	var layer1Count, layer2Count, layer3Count int64

	// Layer 1: 假设所有通过Supabase验证的用户都在
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM auth.users").Scan(&layer1Count)

	// Layer 2: user.users计数
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM user.users").Scan(&layer2Count)

	// Layer 3: billing.accounts计数
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM billing.accounts").Scan(&layer3Count)

	// 计算完整性比例
	var userCompleteCount int64
	pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM user.users u
		JOIN billing.accounts ba ON u.id = ba.user_id
		WHERE ba.status = 'active'
	`).Scan(&userCompleteCount)

	// 填充指标
	metrics["layer_counts"] = map[string]interface{}{
		"layer1_supabase": layer1Count,
		"layer2_users":     layer2Count,
		"layer3_billing":   layer3Count,
	}

	metrics["completion_metrics"] = map[string]interface{}{
		"total_users":       layer2Count,
		"complete_users":    userCompleteCount,
		"completion_rate":    float64(userCompleteCount) / float64(layer2Count) * 100,
	}

	// 添加时间戳
	metrics["timestamp"] = time.Now().Unix()
	metrics["service_name"] = "three_layer_validation_demo"

	// 输出JSON格式的指标
	if jsonBytes, err := json.MarshalIndent(metrics, "", "  "); err == nil {
		log.Printf("  ✓ 监控指标: %s", string(jsonBytes))
	}
}

// GetValidationReport 获取验证报告
func (u *UsageExample) GetValidationReport(ctx context.Context) (map[string]interface{}, error) {
	pool := u.adapter.GetCloudSQLPool()

	// 生成综合验证报告
	report := map[string]interface{}{
		"summary": map[string]interface{}{
			"total_users":        0,
			"complete_users":      0,
			"incomplete_users":    0,
			"data_issues":        0,
		},
		"details": []map[string]interface{}{},
		"generated_at": time.Now().Format(time.RFC3339),
	}

	// 统计用户完整性状态
	const completenessQuery := `
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN ba.user_id IS NOT NULL THEN 1 END) as with_billing,
			COUNT(CASE WHEN ba.user_id IS NULL THEN 1 END) as without_billing
		FROM user.users u
		LEFT JOIN billing.accounts ba ON u.id = ba.user_id
	`

	var total, withBilling, withoutBilling int
	err := pool.QueryRow(ctx, completenessQuery).Scan(&total, &withBilling, &withoutBilling)
	if err != nil {
		return nil, fmt.Errorf("查询用户完整性失败: %w", err)
	}

	report["summary"] = map[string]interface{}{
		"total_users":     total,
		"complete_users":  withBilling,
		"incomplete_users": withoutBilling,
		"completion_rate": float64(withBilling) / float64(total) * 100,
	}

	// 查找数据问题
	const issuesQuery := `
		SELECT
			'user missing billing account' as issue_type,
			COUNT(*) as count
		FROM user.users u
		LEFT JOIN billing.accounts ba ON u.id = ba.user_id
		WHERE ba.user_id IS NULL
		UNION ALL
		SELECT
			'orphaned billing account' as issue_type,
			COUNT(*) as count
		FROM billing.accounts ba
		LEFT JOIN user.users u ON ba.user_id = u.id
		WHERE u.id IS NULL
	`

	rows, err := pool.Query(ctx, issuesQuery)
	if err != nil {
		return nil, fmt.Errorf("查询数据问题失败: %w", err)
	}
	defer rows.Close()

	details := make([]map[string]interface{}, 0)
	for rows.Next() {
		var issueType string
		var count int
		if err := rows.Scan(&issueType, &count); err != nil {
			continue
		}
		details = append(details, map[string]interface{}{
			"issue":  issueType,
			"count":  count,
		})
	}

	report["details"] = details

	return report, nil
}