package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// ============================================================================
// Data Structures
// ============================================================================

// PlanConfig 套餐配置结构
type PlanConfig struct {
	ID                string                 `json:"id"`
	Tier              string                 `json:"tier"`
	DisplayNameEn     string                 `json:"display_name_en"`
	DisplayNameZh     string                 `json:"display_name_zh"`
	Permissions       map[string]interface{} `json:"permissions"`
	TokenCosts        map[string]interface{} `json:"token_costs"`
	MonthlyTokens     int                    `json:"monthly_tokens"`
	Pricing           map[string]interface{} `json:"pricing"`
	MarketingFeatures json.RawMessage        `json:"marketing_features"`
	DisplayOrder      int                    `json:"display_order"`
	IsActive          bool                   `json:"is_active"`
	Version           int                    `json:"version"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

// UpdatePlanRequest 更新套餐配置请求
type UpdatePlanRequest struct {
	DisplayNameEn     *string                `json:"display_name_en"`
	DisplayNameZh     *string                `json:"display_name_zh"`
	Permissions       map[string]interface{} `json:"permissions"`
	TokenCosts        map[string]interface{} `json:"token_costs"`
	MonthlyTokens     *int                   `json:"monthly_tokens"`
	Pricing           map[string]interface{} `json:"pricing"`
	MarketingFeatures json.RawMessage        `json:"marketing_features"`
	DisplayOrder      *int                   `json:"display_order"`
	IsActive          *bool                  `json:"is_active"`
	ChangeSummary     string                 `json:"change_summary"` // 变更摘要
}

// ============================================================================
// Handler
// ============================================================================

type subscriptionsPlanHandler struct {
	db           *sql.DB
	redisClient  *redis.Client
	pubsubClient *pubsub.Client
	topicName    string
}

func NewsubscriptionsPlanHandler(db *sql.DB, redisClient *redis.Client, pubsubClient *pubsub.Client) *subscriptionsPlanHandler {
	return &subscriptionsPlanHandler{
		db:           db,
		redisClient:  redisClient,
		pubsubClient: pubsubClient,
		topicName:    "subscription-config-updated",
	}
}

// ============================================================================
// GET /api/v1/billing/plans - 获取所有活跃套餐配置
// ============================================================================

func (h *subscriptionsPlanHandler) GetAllPlans(c *gin.Context) {
	ctx := c.Request.Context()
	cacheKey := "subscription:plans:all"

	// 1. 尝试从Redis读取
	if cached, err := h.redisClient.Get(ctx, cacheKey).Result(); err == nil {
		var plans []PlanConfig
		if json.Unmarshal([]byte(cached), &plans) == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"data":    plans,
				"source":  "cache",
			})
			return
		}
	}

	// 2. 从数据库读取
	plans, err := h.fetchAllPlansFromDB(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "failed to fetch plans from database",
		})
		return
	}

	// 3. 写入Redis缓存（5分钟TTL）
	if plansJSON, err := json.Marshal(plans); err == nil {
		h.redisClient.Set(ctx, cacheKey, plansJSON, 5*time.Minute)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    plans,
		"source":  "database",
	})
}

// fetchAllPlansFromDB 从数据库获取所有活跃套餐
func (h *subscriptionsPlanHandler) fetchAllPlansFromDB(ctx context.Context) ([]PlanConfig, error) {
	query := `
		SELECT id, tier, display_name_en, display_name_zh,
		       permissions, token_costs, monthly_tokens, pricing,
		       marketing_features, display_order, is_active, version,
		       created_at, updated_at
		FROM subscription_plan_configs
		WHERE is_active = true
		ORDER BY display_order
	`

	rows, err := h.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []PlanConfig
	for rows.Next() {
		var plan PlanConfig
		var permissionsJSON, tokenCostsJSON, pricingJSON, marketingFeaturesJSON []byte

		err := rows.Scan(
			&plan.ID, &plan.Tier, &plan.DisplayNameEn, &plan.DisplayNameZh,
			&permissionsJSON, &tokenCostsJSON, &plan.MonthlyTokens, &pricingJSON,
			&marketingFeaturesJSON, &plan.DisplayOrder, &plan.IsActive, &plan.Version,
			&plan.CreatedAt, &plan.UpdatedAt,
		)
		if err != nil {
			continue
		}

		// 解析JSONB字段
		json.Unmarshal(permissionsJSON, &plan.Permissions)
		json.Unmarshal(tokenCostsJSON, &plan.TokenCosts)
		json.Unmarshal(pricingJSON, &plan.Pricing)
		plan.MarketingFeatures = marketingFeaturesJSON

		plans = append(plans, plan)
	}

	return plans, nil
}

// ============================================================================
// GET /api/v1/billing/plans/:tier - 获取特定套餐配置
// ============================================================================

func (h *subscriptionsPlanHandler) GetPlanByTier(c *gin.Context) {
	ctx := c.Request.Context()
	tier := c.Param("tier")

	// 验证tier参数
	if tier != "starter" && tier != "pro" && tier != "elite" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "invalid tier, must be one of: starter, pro, elite",
		})
		return
	}

	cacheKey := fmt.Sprintf("subscription:config:%s", tier)

	// 1. Redis缓存
	if cached, err := h.redisClient.Get(ctx, cacheKey).Result(); err == nil {
		var plan PlanConfig
		if json.Unmarshal([]byte(cached), &plan) == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"data":    plan,
				"source":  "cache",
			})
			return
		}
	}

	// 2. 数据库查询
	plan, err := h.fetchPlanFromDB(ctx, tier)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "plan not found",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "database error",
		})
		return
	}

	// 3. 写入Redis缓存
	if planJSON, err := json.Marshal(plan); err == nil {
		h.redisClient.Set(ctx, cacheKey, planJSON, 5*time.Minute)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    plan,
		"source":  "database",
	})
}

// fetchPlanFromDB 从数据库获取单个套餐
func (h *subscriptionsPlanHandler) fetchPlanFromDB(ctx context.Context, tier string) (*PlanConfig, error) {
	query := `
		SELECT id, tier, display_name_en, display_name_zh,
		       permissions, token_costs, monthly_tokens, pricing,
		       marketing_features, display_order, is_active, version,
		       created_at, updated_at
		FROM subscription_plan_configs
		WHERE tier = $1 AND is_active = true
	`

	var plan PlanConfig
	var permissionsJSON, tokenCostsJSON, pricingJSON, marketingFeaturesJSON []byte

	err := h.db.QueryRowContext(ctx, query, tier).Scan(
		&plan.ID, &plan.Tier, &plan.DisplayNameEn, &plan.DisplayNameZh,
		&permissionsJSON, &tokenCostsJSON, &plan.MonthlyTokens, &pricingJSON,
		&marketingFeaturesJSON, &plan.DisplayOrder, &plan.IsActive, &plan.Version,
		&plan.CreatedAt, &plan.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	// 解析JSONB字段
	json.Unmarshal(permissionsJSON, &plan.Permissions)
	json.Unmarshal(tokenCostsJSON, &plan.TokenCosts)
	json.Unmarshal(pricingJSON, &plan.Pricing)
	plan.MarketingFeatures = marketingFeaturesJSON

	return &plan, nil
}

// ============================================================================
// PUT /api/v1/billing/plans/:tier - 更新套餐配置（管理员专用）
// ============================================================================

func (h *subscriptionsPlanHandler) UpdatePlan(c *gin.Context) {
	ctx := c.Request.Context()
	tier := c.Param("tier")

	// 1. 权限检查（仅管理员）
	// TODO: 实现JWT验证和角色检查
	// user := c.MustGet("user").(*User)
	// if !user.IsAdmin {
	//     c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
	//     return
	// }

	// 2. 解析请求
	var req UpdatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// 3. 获取旧配置（用于历史记录）
	oldPlan, err := h.fetchPlanFromDB(ctx, tier)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "plan not found",
		})
		return
	}

	// 4. 开启事务更新
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "failed to begin transaction",
		})
		return
	}
	defer tx.Rollback()

	// 5. 更新配置表
	newVersion, err := h.updatePlanConfig(ctx, tx, tier, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("failed to update plan: %v", err),
		})
		return
	}

	// 6. 插入变更历史
	err = h.insertConfigHistory(ctx, tx, oldPlan, tier, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("failed to insert history: %v", err),
		})
		return
	}

	// 7. 提交事务
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "failed to commit transaction",
		})
		return
	}

	// 8. 删除Redis缓存
	h.invalidateCache(ctx, tier)

	// 9. 发布Pub/Sub事件通知其他服务
	h.publishConfigUpdateEvent(ctx, tier, newVersion)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "configuration updated and cache invalidated",
		"data": gin.H{
			"tier":    tier,
			"version": newVersion,
		},
	})
}

// updatePlanConfig 更新套餐配置
func (h *subscriptionsPlanHandler) updatePlanConfig(ctx context.Context, tx *sql.Tx, tier string, req *UpdatePlanRequest) (int, error) {
	// 构建动态UPDATE语句
	query := `
		UPDATE subscription_plan_configs
		SET display_name_en = COALESCE($1, display_name_en),
		    display_name_zh = COALESCE($2, display_name_zh),
		    permissions = COALESCE($3, permissions),
		    token_costs = COALESCE($4, token_costs),
		    monthly_tokens = COALESCE($5, monthly_tokens),
		    pricing = COALESCE($6, pricing),
		    marketing_features = COALESCE($7, marketing_features),
		    display_order = COALESCE($8, display_order),
		    is_active = COALESCE($9, is_active),
		    version = version + 1,
		    updated_at = NOW()
		WHERE tier = $10
		RETURNING version
	`

	// 将map转为JSONB
	permissionsJSON, _ := json.Marshal(req.Permissions)
	tokenCostsJSON, _ := json.Marshal(req.TokenCosts)
	pricingJSON, _ := json.Marshal(req.Pricing)

	var newVersion int
	err := tx.QueryRowContext(ctx, query,
		req.DisplayNameEn, req.DisplayNameZh,
		permissionsJSON, tokenCostsJSON,
		req.MonthlyTokens, pricingJSON, req.MarketingFeatures,
		req.DisplayOrder, req.IsActive,
		tier,
	).Scan(&newVersion)

	return newVersion, err
}

// insertConfigHistory 插入配置变更历史
func (h *subscriptionsPlanHandler) insertConfigHistory(ctx context.Context, tx *sql.Tx, oldPlan *PlanConfig, tier string, req *UpdatePlanRequest) error {
	oldConfigJSON, _ := json.Marshal(oldPlan)
	newConfigJSON, _ := json.Marshal(req)

	query := `
		INSERT INTO subscription_config_history
		(config_id, tier, old_config, new_config, change_summary, changed_by, change_type)
		VALUES ($1, $2, $3, $4, $5, $6, 'update')
	`

	// TODO: 获取当前管理员user_id
	adminUserID := "00000000-0000-0000-0000-000000000000" // 临时值

	_, err := tx.ExecContext(ctx, query,
		oldPlan.ID, tier, oldConfigJSON, newConfigJSON,
		req.ChangeSummary, adminUserID,
	)

	return err
}

// invalidateCache 失效缓存
func (h *subscriptionsPlanHandler) invalidateCache(ctx context.Context, tier string) {
	cacheKeys := []string{
		fmt.Sprintf("subscription:config:%s", tier),
		"subscription:plans:all",
	}
	h.redisClient.Del(ctx, cacheKeys...)
}

// publishConfigUpdateEvent 发布配置更新事件到Pub/Sub
func (h *subscriptionsPlanHandler) publishConfigUpdateEvent(ctx context.Context, tier string, version int) {
	topic := h.pubsubClient.Topic(h.topicName)
	defer topic.Stop()

	msg := map[string]interface{}{
		"event":     "config_updated",
		"tier":      tier,
		"version":   version,
		"timestamp": time.Now().Unix(),
	}

	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return
	}

	topic.Publish(ctx, &pubsub.Message{
		Data: msgJSON,
	})
}
