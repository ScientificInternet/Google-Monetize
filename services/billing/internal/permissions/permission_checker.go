package permissions

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ============================================================================
// 权限检查器 - 用于业务服务检查用户权限和Token消耗规则
// ============================================================================

// PermissionChecker 权限检查器
type PermissionChecker struct {
	db          *sql.DB
	redisClient *redis.Client
}

// PlanPermissions 套餐权限配置
type PlanPermissions struct {
	Tier        string
	Permissions map[string]interface{}
	TokenCosts  map[string]interface{}
}

func NewPermissionChecker(db *sql.DB, redisClient *redis.Client) *PermissionChecker {
	return &PermissionChecker{
		db:          db,
		redisClient: redisClient,
	}
}

// ============================================================================
// 权限检查函数
// ============================================================================

// CheckFeaturePermission 检查用户是否有权限使用某个功能
// 用法示例：
//
//	allowed, err := checker.CheckFeaturePermission(ctx, userTier, "offer_evaluation_ai")
func (pc *PermissionChecker) CheckFeaturePermission(ctx context.Context, userTier string, featureKey string) (bool, error) {
	permissions, err := pc.GetPlanPermissions(ctx, userTier)
	if err != nil {
		return false, err
	}

	// 读取权限值
	value, exists := permissions.Permissions[featureKey]
	if !exists {
		return false, fmt.Errorf("feature key not found: %s", featureKey)
	}

	// 布尔类型权限
	if boolValue, ok := value.(bool); ok {
		return boolValue, nil
	}

	// 整数配额（大于0表示有权限）
	if floatValue, ok := value.(float64); ok {
		return floatValue > 0, nil
	}

	return false, fmt.Errorf("invalid permission value type for key: %s", featureKey)
}

// GetFeatureQuota 获取功能的配额限制
// 用法示例：
//
//	quota, err := checker.GetFeatureQuota(ctx, userTier, "offer_evaluation_concurrency")
//	返回：10 （表示可以并发10个评估）
func (pc *PermissionChecker) GetFeatureQuota(ctx context.Context, userTier string, featureKey string) (int, error) {
	permissions, err := pc.GetPlanPermissions(ctx, userTier)
	if err != nil {
		return 0, err
	}

	value, exists := permissions.Permissions[featureKey]
	if !exists {
		return 0, fmt.Errorf("feature key not found: %s", featureKey)
	}

	// 转换为整数
	if floatValue, ok := value.(float64); ok {
		return int(floatValue), nil
	}

	return 0, fmt.Errorf("feature value is not a number: %s", featureKey)
}

// GetFeatureStringList 获取功能的字符串列表配置
// 用法示例：
//
//	countries, err := checker.GetFeatureStringList(ctx, userTier, "autoclick_proxy_countries")
//	返回：["US", "GB", "CA"]
func (pc *PermissionChecker) GetFeatureStringList(ctx context.Context, userTier string, featureKey string) ([]string, error) {
	permissions, err := pc.GetPlanPermissions(ctx, userTier)
	if err != nil {
		return nil, err
	}

	value, exists := permissions.Permissions[featureKey]
	if !exists {
		return nil, fmt.Errorf("feature key not found: %s", featureKey)
	}

	// 转换为字符串数组
	if arrayValue, ok := value.([]interface{}); ok {
		result := make([]string, len(arrayValue))
		for i, v := range arrayValue {
			if strValue, ok := v.(string); ok {
				result[i] = strValue
			}
		}
		return result, nil
	}

	return nil, fmt.Errorf("feature value is not a string list: %s", featureKey)
}

// ============================================================================
// Token消耗规则查询
// ============================================================================

// GetTokenCost 获取操作的Token消耗量
// 用法示例：
//
//	cost, err := checker.GetTokenCost(ctx, userTier, "offer_evaluation_basic")
//	返回：1 （表示普通评估消耗1个token）
func (pc *PermissionChecker) GetTokenCost(ctx context.Context, userTier string, operationKey string) (int, error) {
	permissions, err := pc.GetPlanPermissions(ctx, userTier)
	if err != nil {
		return 0, err
	}

	value, exists := permissions.TokenCosts[operationKey]
	if !exists {
		return 0, fmt.Errorf("operation key not found in token_costs: %s", operationKey)
	}

	// 转换为整数
	if floatValue, ok := value.(float64); ok {
		return int(floatValue), nil
	}

	return 0, fmt.Errorf("token cost is not a number: %s", operationKey)
}

// ============================================================================
// 内部辅助函数 - 获取套餐权限配置（带缓存）
// ============================================================================

// GetPlanPermissions 获取套餐的权限配置（优先从Redis缓存读取）
func (pc *PermissionChecker) GetPlanPermissions(ctx context.Context, tier string) (*PlanPermissions, error) {
	cacheKey := fmt.Sprintf("subscription:config:%s", tier)

	// 1. 尝试从Redis读取
	if cached, err := pc.redisClient.Get(ctx, cacheKey).Result(); err == nil {
		var permissions PlanPermissions
		if json.Unmarshal([]byte(cached), &permissions) == nil {
			return &permissions, nil
		}
	}

	// 2. 从数据库读取
	permissions, err := pc.fetchPermissionsFromDB(ctx, tier)
	if err != nil {
		return nil, err
	}

	// 3. 写入Redis缓存（5分钟TTL）
	if permissionsJSON, err := json.Marshal(permissions); err == nil {
		pc.redisClient.Set(ctx, cacheKey, permissionsJSON, 5*time.Minute)
	}

	return permissions, nil
}

// fetchPermissionsFromDB 从数据库获取权限配置
func (pc *PermissionChecker) fetchPermissionsFromDB(ctx context.Context, tier string) (*PlanPermissions, error) {
	query := `
		SELECT tier, permissions, token_costs
		FROM subscription_plan_configs
		WHERE tier = $1 AND is_active = true
	`

	var permissions PlanPermissions
	var permissionsJSON, tokenCostsJSON []byte

	err := pc.db.QueryRowContext(ctx, query, tier).Scan(
		&permissions.Tier, &permissionsJSON, &tokenCostsJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("plan not found for tier: %s", tier)
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	// 解析JSONB
	json.Unmarshal(permissionsJSON, &permissions.Permissions)
	json.Unmarshal(tokenCostsJSON, &permissions.TokenCosts)

	return &permissions, nil
}

// ============================================================================
// 便捷函数 - 常用权限检查
// ============================================================================

// CanUseAIEvaluation 是否可以使用AI评估
func (pc *PermissionChecker) CanUseAIEvaluation(ctx context.Context, userTier string) (bool, error) {
	return pc.CheckFeaturePermission(ctx, userTier, "offer_evaluation_ai")
}

// CanUseLinkReplacement 是否可以换链接
func (pc *PermissionChecker) CanUseLinkReplacement(ctx context.Context, userTier string) (bool, error) {
	return pc.CheckFeaturePermission(ctx, userTier, "offer_link_replacement")
}

// GetEvaluationConcurrency 获取评估并发数
func (pc *PermissionChecker) GetEvaluationConcurrency(ctx context.Context, userTier string) (int, error) {
	return pc.GetFeatureQuota(ctx, userTier, "offer_evaluation_concurrency")
}

// GetProxyCountries 获取允许使用的代理IP国家列表
func (pc *PermissionChecker) GetProxyCountries(ctx context.Context, userTier string) ([]string, error) {
	countries, err := pc.GetFeatureStringList(ctx, userTier, "autoclick_proxy_countries")
	if err != nil {
		return nil, err
	}

	// 特殊处理：["*"] 表示所有国家
	if len(countries) == 1 && countries[0] == "*" {
		return []string{"*"}, nil // 业务层判断为"所有国家"
	}

	return countries, nil
}
