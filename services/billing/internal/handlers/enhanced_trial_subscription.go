package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/pkg/errors"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/domain"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/events"
)

// EnhancedTrialSubscriptionHandler 增强的试用订阅处理器
// 提供更好的幂等性、错误处理和数据完整性
type EnhancedTrialSubscriptionHandler struct {
	adapter database.DatabaseAdapter
	pub     *events.Publisher
}

// InitializeUserBusinessDataRequest 完整的用户业务数据初始化请求
type InitializeUserBusinessDataRequest struct {
	UserID    string `json:"userId"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl,omitempty"`
	Days      int    `json:"days,omitempty"`
	Source    string `json:"source,omitempty"`
}

// InitializeUserBusinessDataResponse 用户业务数据初始化响应
type InitializeUserBusinessDataResponse struct {
	Success        bool      `json:"success"`
	AccountID      string    `json:"accountId,omitempty"`
	SubscriptionID string    `json:"subscriptionId,omitempty"`
	TokenBalance   int       `json:"tokenBalance,omitempty"`
	UserID         string    `json:"userId,omitempty"`
	Message        string    `json:"message,omitempty"`
	Error          string    `json:"error,omitempty"`
	Warnings       []string  `json:"warnings,omitempty"`
}

// UserIntegrityCheckResult 用户数据完整性检查结果
type UserIntegrityCheckResult struct {
	UserExists      bool     `json:"userExists"`
	AccountExists   bool     `json:"accountExists"`
	SubscriptionExists bool     `json:"subscriptionExists"`
	TokenBalanceExists bool     `json:"tokenBalanceExists"`
	MissingLayers  []string `json:"missingLayers,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
}

// NewEnhancedTrialSubscriptionHandler 创建增强的试用订阅处理器
func NewEnhancedTrialSubscriptionHandler(adapter database.DatabaseAdapter, pub *events.Publisher) *EnhancedTrialSubscriptionHandler {
	return &EnhancedTrialSubscriptionHandler{
		adapter: adapter,
		pub:     pub,
	}
}

// InitializeUserBusinessData 初始化用户业务数据
// 支持幂等操作、完整错误处理和数据完整性验证
func (h *EnhancedTrialSubscriptionHandler) InitializeUserBusinessData(c *gin.Context) {
	ctx := c.Request().Context()

	var req InitializeUserBusinessDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.Write(c, http.StatusBadRequest, "INVALID_JSON", "Invalid request body", map[string]string{"error": err.Error()})
		return
	}

	// 验证请求
	if err := h.validateInitializeRequest(&req); err != nil {
		errors.Write(c, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}

	log.Printf("🔄 开始用户业务数据初始化: userID=%s, email=%s, source=%s", req.UserID, req.Email, req.Source)

	// 执行完整的用户业务数据初始化
	resp, err := h.initializeCompleteUserData(ctx, &req)
	if err != nil {
		log.Printf("❌ 用户业务数据初始化失败: userID=%s, error=%v", req.UserID, err)
		errors.Write(c, http.StatusInternalServerError, "INITIALIZATION_FAILED", "用户业务数据初始化失败", map[string]string{"error": err.Error()})
		return
	}

	// 记录操作日志
	h.logInitializationResult(req.UserID, resp)

	// 返回响应
	c.JSON(http.StatusOK, resp)
}

// initializeCompleteUserData 执行完整的用户业务数据初始化
func (h *EnhancedTrialSubscriptionHandler) initializeCompleteUserData(ctx context.Context, req *InitializeUserBusinessDataRequest) (*InitializeUserBusinessDataResponse, error) {
	// 1. 检查当前数据完整性
	integrity, err := h.checkUserIntegrity(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("检查用户数据完整性失败: %w", err)
	}

	log.Printf("🔍 用户数据完整性检查结果: exists=%v, missing=%v",
		integrity.UserExists || integrity.AccountExists || integrity.SubscriptionExists,
		integrity.MissingLayers)

	// 2. 如果数据已经完整，返回现有信息
	if len(integrity.MissingLayers) == 0 {
		return h.buildExistingDataResponse(ctx, req.UserID)
	}

	// 3. 执行事务性初始化
	subscriptionID, accountID, err := h.executeTransactionalInitialization(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("事务性初始化失败: %w", err)
	}

	// 4. 返回成功响应
	return &InitializeUserBusinessDataResponse{
		Success:        true,
		AccountID:      accountID,
		SubscriptionID: subscriptionID,
		TokenBalance:   100, // 试用期初始代币
		UserID:         req.UserID,
		Message:        "用户业务数据初始化成功",
	}, nil
}

// executeTransactionalInitialization 在事务中执行完整的用户业务数据初始化
func (h *EnhancedTrialSubscriptionHandler) executeTransactionalInitialization(ctx context.Context, req *InitializeUserBusinessDataRequest) (string, string, error) {
	var subscriptionID, accountID string
	var err error

	// 在事务中执行所有操作
	transactionError := h.adapter.ExecuteInTransaction(ctx, []func(*sql.Tx) error{
		// Layer 2: 创建或更新业务用户主数据
		func(tx *sql.Tx) error {
			accountID, err = h.createOrUpdateUserLayer(tx, req)
			if err != nil {
				return fmt.Errorf("创建/更新用户数据失败: %w", err)
			}
			return nil
		},

		// Layer 3: 创建或更新计费账户数据
		func(tx *sql.Tx) error {
			return h.createOrUpdateAccountLayer(tx, req, accountID)
		},

		// Layer 3: 创建或更新试用订阅
		func(tx *sql.Tx) error {
			subscriptionID, err = h.createOrUpdateSubscriptionLayer(tx, req, accountID)
			if err != nil {
				return fmt.Errorf("创建/更新订阅失败: %w", err)
			}
			return nil
		},

		// Layer 3: 初始化或更新代币系统
		func(tx *sql.Tx) error {
			return h.initializeOrUpdateTokenSystem(tx, req, accountID)
		},

		// Layer 3: 记录初始交易
		func(tx *sql.Tx) error {
			return h.recordInitialTransaction(tx, req, accountID)
		},
	})

	if transactionError != nil {
		return "", "", transactionError
	}

	log.Printf("✅ 用户业务数据事务初始化成功: userID=%s, accountID=%s, subscriptionID=%s", req.UserID, accountID, subscriptionID)
	return subscriptionID, accountID, nil
}

// createOrUpdateUserLayer 创建或更新Layer 2: 业务用户主数据
func (h *EnhancedTrialSubscriptionHandler) createOrUpdateUserLayer(tx *sql.Tx, req *InitializeUserBusinessDataRequest) (string, error) {
	accountID := req.UserID // 使用用户ID作为账户ID

	query := `
		INSERT INTO user.users (
			id, email, name, avatar_url, status,
			language, timezone, preferences,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'active', 'zh', 'UTC', '{}', NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			email = EXCLUDED.email,
			name = EXCLUDED.name,
			avatar_url = EXCLUDED.avatar_url,
			updated_at = NOW()
		RETURNING id
	`

	var returnedID string
	err := tx.QueryRow(query, req.UserID, req.Email, req.Name, req.AvatarURL).Scan(&returnedID)
	if err != nil {
		return "", fmt.Errorf("执行用户数据操作失败: %w", err)
	}

	log.Printf("✅ Layer 2: 用户数据操作完成: userID=%s, returnedID=%s", req.UserID, returnedID)
	return returnedID, nil
}

// createOrUpdateAccountLayer 创建或更新Layer 3: 计费账户数据
func (h *EnhancedTrialSubscriptionHandler) createOrUpdateAccountLayer(tx *sql.Tx, req *InitializeUserBusinessDataRequest, accountID string) error {
	query := `
		INSERT INTO billing.accounts (
			id, user_id, account_type, status,
			balance_cents, billing_currency,
			trial_ends_at, created_at, updated_at
		) VALUES ($1, $2, 'standard', 'trial', 0, 'CNY', NOW() + INTERVAL '7 days', NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			account_type = 'standard',
			status = 'trial',
			trial_ends_at = NOW() + INTERVAL '7 days',
			updated_at = NOW()
	`

	_, err := tx.Exec(query, accountID, req.UserID)
	if err != nil {
		return fmt.Errorf("执行账户数据操作失败: %w", err)
	}

	log.Printf("✅ Layer 3: 账户数据操作完成: userID=%s", req.UserID)
	return nil
}

// createOrUpdateSubscriptionLayer 创建或更新Layer 3: 订阅数据
func (h *EnhancedTrialSubscriptionHandler) createOrUpdateSubscriptionLayer(tx *sql.Tx, req *InitializeUserBusinessDataRequest, accountID string) (string, error) {
	subscriptionID := generateSubscriptionID(req.UserID, "free")
	trialDays := 7
	if req.Days > 0 {
		trialDays = req.Days
	}

	query := `
		INSERT INTO billing.subscriptions (
			id, user_id, plan_name, status,
			current_period_start, current_period_end,
			trial_start, trial_end, trial_source,
			created_at, updated_at
		) VALUES ($1, $2, 'free', 'trial',
			NOW(), NOW() + INTERVAL '7 days',
			NOW(), NOW() + INTERVAL '%d days', $3,
			NOW(), NOW()
		)
		ON CONFLICT (user_id, plan_name) DO UPDATE SET
			status = 'trial',
			current_period_start = NOW(),
			current_period_end = NOW() + INTERVAL '7 days',
			trial_start = NOW(),
			trial_end = NOW() + INTERVAL '%d days',
			trial_source = $3,
			updated_at = NOW()
		RETURNING id
	`

	var returnedID string
	err := tx.QueryRow(query, subscriptionID, req.UserID, req.Source, trialDays).Scan(&returnedID)
	if err != nil {
		return "", fmt.Errorf("执行订阅数据操作失败: %w", err)
	}

	log.Printf("✅ Layer 3: 订阅数据操作完成: userID=%s, subscriptionID=%s", req.UserID, returnedID)
	return returnedID, nil
}

// initializeOrUpdateTokenSystem 初始化或更新代币系统
func (h *EnhancedTrialSubscriptionHandler) initializeOrUpdateTokenSystem(tx *sql.Tx, req *InitializeUserBusinessDataRequest, accountID string) error {
	// 初始化代币余额
	balanceQuery := `
		INSERT INTO billing.token_balances (
			id, user_id, token_type, balance, reserved,
			available, total_earned, total_spent,
			created_at, updated_at
		) VALUES ($1, $2, 'search', 100, 0, 100, 100, 0, NOW(), NOW())
		ON CONFLICT (user_id, token_type) DO UPDATE SET
			balance = balance + 100,
			reserved = reserved,
			available = available + 100,
			total_earned = total_earned + 100,
			last_updated = NOW(),
			updated_at = NOW()
	`

	balanceID := generateTokenBalanceID(req.UserID, "search")
	_, err := tx.Exec(balanceQuery, balanceID, req.UserID)
	if err != nil {
		return fmt.Errorf("执行代币余额操作失败: %w", err)
	}

	log.Printf("✅ Layer 3: 代币系统初始化完成: userID=%s", req.UserID)
	return nil
}

// recordInitialTransaction 记录初始交易
func (h *EnhancedTrialSubscriptionHandler) recordInitialTransaction(tx *sql.Tx, req *InitializeUserBusinessDataRequest, accountID string) error {
	transactionID := generateTransactionID()

	query := `
		INSERT INTO billing.token_transactions (
			id, user_id, token_type, amount,
			balance_before, balance_after,
			transaction_type, source, description,
			created_at
		) VALUES ($1, $2, 'search', 100, 0, 100,
			'bonus', $3, '试用期初始代币', NOW())
	`

	_, err := tx.Exec(query, transactionID, req.UserID, req.Source)
	if err != nil {
		return fmt.Errorf("记录初始交易失败: %w", err)
	}

	log.Printf("✅ Layer 3: 初始交易记录完成: userID=%s, transactionID=%s", req.UserID, transactionID)
	return nil
}

// checkUserIntegrity 检查用户数据完整性
func (h *EnhancedTrialSubscriptionHandler) checkUserIntegrity(ctx context.Context, userID string) (*UserIntegrityCheckResult, error) {
	result := &UserIntegrityCheckResult{}

	// 检查用户是否存在
	userExists, err := h.checkUserExists(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("检查用户存在失败: %w", err)
	}
	result.UserExists = userExists

	// 检查账户是否存在
	accountExists, err := h.checkAccountExists(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("检查账户存在失败: %w", err)
	}
	result.AccountExists = accountExists

	// 检查订阅是否存在
	subscriptionExists, err := h.checkSubscriptionExists(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("检查订阅存在失败: %w", err)
	}
	result.SubscriptionExists = subscriptionExists

	// 检查代币余额是否存在
	tokenBalanceExists, err := h.checkTokenBalanceExists(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("检查代币余额存在失败: %w", err)
	}
	result.TokenBalanceExists = tokenBalanceExists

	// 确定缺失的层
	if !result.UserExists {
		result.MissingLayers = append(result.MissingLayers, "user_layer")
	}
	if !result.AccountExists {
		result.MissingLayers = append(result.MissingLayers, "account_layer")
	}
	if !result.SubscriptionExists {
		result.MissingLayers = append(result.MissingLayers, "subscription_layer")
	}
	if !result.TokenBalanceExists {
		result.MissingLayers = append(result.MissingLayers, "token_balance_layer")
	}

	return result, nil
}

// 以下是各种检查方法的实现
func (h *EnhancedTrialSubscriptionHandler) checkUserExists(ctx context.Context, userID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM user.users WHERE id = $1)`
	var exists bool
	err := h.adapter.QueryRowContext(ctx, query, userID).Scan(&exists)
	return exists, err
}

func (h *EnhancedTrialSubscriptionHandler) checkAccountExists(ctx context.Context, userID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM billing.accounts WHERE user_id = $1)`
	var exists bool
	err := h.adapter.QueryRowContext(ctx, query, userID).Scan(&exists)
	return exists, err
}

func (h *EnhancedTrialSubscriptionHandler) checkSubscriptionExists(ctx context.Context, userID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM billing.subscriptions WHERE user_id = $1)`
	var exists bool
	err := h.adapter.QueryRowContext(ctx, query, userID).Scan(&exists)
	return exists, err
}

func (h *EnhancedTrialSubscriptionHandler) checkTokenBalanceExists(ctx context.Context, userID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM billing.token_balances WHERE user_id = $1 AND token_type = 'search')`
	var exists bool
	err := h.adapter.QueryRowContext(ctx, query, userID).Scan(&exists)
	return exists, err
}

// buildExistingDataResponse 构建现有数据响应
func (h *EnhancedTrialSubscriptionHandler) buildExistingDataResponse(ctx context.Context, userID string) (*InitializeUserBusinessDataResponse, error) {
	// 查询现有数据
	query := `
		SELECT
			a.id as account_id,
			s.id as subscription_id,
			b.balance as token_balance
		FROM billing.accounts a
		LEFT JOIN billing.subscriptions s ON a.user_id = s.user_id
		LEFT JOIN billing.token_balances b ON a.user_id = b.user_id AND b.token_type = 'search'
		WHERE a.user_id = $1
		LIMIT 1
	`

	var response InitializeUserBusinessDataResponse
	err := h.adapter.QueryRowContext(ctx, query, userID).Scan(
		&response.AccountID,
		&response.SubscriptionID,
		&response.TokenBalance,
	)

	if err != nil {
		return nil, fmt.Errorf("查询现有数据失败: %w", err)
	}

	response.Success = true
	response.UserID = userID
	response.Message = "用户业务数据已存在"

	return &response, nil
}

// validateInitializeRequest 验证初始化请求
func (h *EnhancedTrialSubscriptionHandler) validateInitializeRequest(req *InitializeUserBusinessDataRequest) error {
	if req.UserID == "" {
		return fmt.Errorf("用户ID不能为空")
	}

	if req.Email == "" {
		return fmt.Errorf("邮箱不能为空")
	}

	if req.Name == "" {
		return fmt.Errorf("姓名不能为空")
	}

	// 验证邮箱格式
	if !isValidEmail(req.Email) {
		return fmt.Errorf("邮箱格��无效")
	}

	// 验证天数
	if req.Days < 0 || req.Days > 365 {
		return fmt.Errorf("试用天数必须在1-365之间")
	}

	// 验证来源
	validSources := []string{"self_register", "google_oauth", "referral_invite", "referral_inviter"}
	isValidSource := false
	for _, source := range validSources {
		if req.Source == source {
			isValidSource = true
			break
		}
	}
	if !isValidSource {
		return fmt.Errorf("来源必须为: %s", validSources)
	}

	return nil
}

// isValidEmail 验证邮箱格式
func isValidEmail(email string) bool {
	// 简单的邮箱格式验证
	return len(email) > 3 && len(email) < 100 &&
		   (email[strings.Index(email, "@"):] != -1) &&
		   (email[strings.LastIndex(email, "."):] != -1)
}

// logInitializationResult 记录初始化结果日志
func (h *EnhancedTrialSubscriptionHandler) logInitializationResult(userID string, resp *InitializeUserBusinessDataResponse) {
	if resp.Success {
		log.Printf("✅ 用户业务数据初始化成功: userID=%s, accountID=%s, subscriptionID=%s, tokenBalance=%d",
			userID, resp.AccountID, resp.SubscriptionID, resp.TokenBalance)

		// 发布事件
		if h.pub != nil {
			event := map[string]interface{}{
				"eventId":    generateEventID(),
				"eventType":  "userBusinessDataInitialized",
				"occurredAt": time.Now().Format(time.RFC3339),
				"userId":     userID,
				"accountId":  resp.AccountID,
				"subscriptionId": resp.SubscriptionID,
				"tokenBalance": resp.TokenBalance,
				"message":    resp.Message,
			}

			h.pub.Publish(context.Background(), "user.business_data.initialized", event)
		}
	} else {
		log.Printf("❌ 用户业务数据初始化失败: userID=%s, error=%s", userID, resp.Error)
	}
}

// ID生成器函数
func generateSubscriptionID(userID, planName string) string {
	return fmt.Sprintf("sub_%s_%s_%d", userID, planName, time.Now().Unix())
}

func generateTokenBalanceID(userID, tokenType string) string {
	return fmt.Sprintf("bal_%s_%s_%d", userID, tokenType, time.Now().Unix())
}

func generateTransactionID() string {
	return fmt.Sprintf("tx_%s_%d", time.Now().UnixNano(), time.Now().Unix())
}

func generateEventID() string {
	return fmt.Sprintf("evt_%s_%d", time.Now().UnixNano(), time.Now().Unix())
}