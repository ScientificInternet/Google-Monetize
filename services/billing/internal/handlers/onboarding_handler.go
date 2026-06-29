package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OnboardingHandler 负责新用户初始化流程协调
type OnboardingHandler struct {
	db              *pgxpool.Pool
	offerServiceURL string
	userActivityURL string
	httpClient      *http.Client
}

// NewOnboardingHandler 创建新的OnboardingHandler
func NewOnboardingHandler(db *pgxpool.Pool, offerServiceURL, userActivityURL string) *OnboardingHandler {
	return &OnboardingHandler{
		db:              db,
		offerServiceURL: offerServiceURL,
		userActivityURL: userActivityURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// OnboardingRequest 初始化请求
type OnboardingRequest struct {
	UserID  string   `json:"user_id"`
	Email   string   `json:"email,omitempty"`
	Modules []string `json:"modules,omitempty"`
}

// OnboardingResponse 初始化响应
type OnboardingResponse struct {
	Success            bool              `json:"success"`
	UserID             string            `json:"user_id"`
	InitializedModules map[string]bool   `json:"initializedModules"`
	Errors             map[string]string `json:"errors,omitempty"`
}

// InitializeNewUser 初始化新用户的所有数据和功能
// 此方法被Trial创建成功后调用
func (h *OnboardingHandler) InitializeNewUser(ctx context.Context, userID, email string) error {
	startTime := time.Now()
	log.Printf("[Onboarding] Starting initialization for user=%s email=%s", userID, email)

	type ModuleResult struct {
		name     string
		duration time.Duration
		success  bool
		error    string
	}

	var results []ModuleResult
	var initErrors []string

	// 1. 初始化Demo Offers (8个示例Offer)
	moduleStart := time.Now()
	if err := h.initializeDemoOffers(ctx, userID); err != nil {
		results = append(results, ModuleResult{"demo_offers", time.Since(moduleStart), false, err.Error()})
		log.Printf("[Onboarding] ❌ Failed to initialize demo offers for user=%s duration=%dms error=%v",
			userID, time.Since(moduleStart).Milliseconds(), err)
		initErrors = append(initErrors, fmt.Sprintf("offers: %v", err))
	} else {
		results = append(results, ModuleResult{"demo_offers", time.Since(moduleStart), true, ""})
		log.Printf("[Onboarding] ✓ Demo offers initialized for user=%s duration=%dms",
			userID, time.Since(moduleStart).Milliseconds())
	}

	// 2. 发送欢迎通知
	moduleStart = time.Now()
	if err := h.sendWelcomeNotification(ctx, userID); err != nil {
		results = append(results, ModuleResult{"welcome_notification", time.Since(moduleStart), false, err.Error()})
		log.Printf("[Onboarding] ❌ Failed to send welcome notification for user=%s duration=%dms error=%v",
			userID, time.Since(moduleStart).Milliseconds(), err)
		initErrors = append(initErrors, fmt.Sprintf("notification: %v", err))
	} else {
		results = append(results, ModuleResult{"welcome_notification", time.Since(moduleStart), true, ""})
		log.Printf("[Onboarding] ✓ Welcome notification sent for user=%s duration=%dms",
			userID, time.Since(moduleStart).Milliseconds())
	}

	// 3. 初始化签到数据
	moduleStart = time.Now()
	if err := h.initializeCheckin(ctx, userID); err != nil {
		results = append(results, ModuleResult{"checkin", time.Since(moduleStart), false, err.Error()})
		log.Printf("[Onboarding] ❌ Failed to initialize checkin for user=%s duration=%dms error=%v",
			userID, time.Since(moduleStart).Milliseconds(), err)
		initErrors = append(initErrors, fmt.Sprintf("checkin: %v", err))
	} else {
		results = append(results, ModuleResult{"checkin", time.Since(moduleStart), true, ""})
		log.Printf("[Onboarding] ✓ Checkin initialized for user=%s duration=%dms",
			userID, time.Since(moduleStart).Milliseconds())
	}

	// 4. 初始化邀请数据结构
	moduleStart = time.Now()
	if err := h.initializeReferral(ctx, userID); err != nil {
		results = append(results, ModuleResult{"referral", time.Since(moduleStart), false, err.Error()})
		log.Printf("[Onboarding] ❌ Failed to initialize referral for user=%s duration=%dms error=%v",
			userID, time.Since(moduleStart).Milliseconds(), err)
		initErrors = append(initErrors, fmt.Sprintf("referral: %v", err))
	} else {
		results = append(results, ModuleResult{"referral", time.Since(moduleStart), true, ""})
		log.Printf("[Onboarding] ✓ Referral initialized for user=%s duration=%dms",
			userID, time.Since(moduleStart).Milliseconds())
	}

	// 计算统计信息
	totalDuration := time.Since(startTime)
	successCount := 0
	for _, r := range results {
		if r.success {
			successCount++
		}
	}
	successRate := float64(successCount) / float64(len(results)) * 100

	if len(initErrors) > 0 {
		log.Printf("[Onboarding] ⚠️  Completed with errors for user=%s total_duration=%dms success_rate=%.1f%% succeeded=%d/%d errors=%v",
			userID, totalDuration.Milliseconds(), successRate, successCount, len(results), initErrors)
		// 不返回error,因为部分初始化失败不应阻止用户登录
		return nil
	}

	log.Printf("[Onboarding] ✅ Successfully initialized all modules for user=%s total_duration=%dms success_rate=%.1f%% modules=%d",
		userID, totalDuration.Milliseconds(), successRate, len(results))
	return nil
}

// initializeDemoOffers 调用Offer服务创建Demo数据
func (h *OnboardingHandler) initializeDemoOffers(ctx context.Context, userID string) error {
	url := fmt.Sprintf("%s/api/v1/offers/demo/initialize", h.offerServiceURL)

	reqBody := map[string]interface{}{
		"modules": []string{"offers"},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID) // 内部服务调用使用的用户标识

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// sendWelcomeNotification 发送欢迎通知
func (h *OnboardingHandler) sendWelcomeNotification(ctx context.Context, userID string) error {
	// 直接在数据库插入欢迎通知 (使用现有表结构: message字段而非content)
	query := `
		INSERT INTO user_notifications (
			user_id, type, title, message, created_at
		) VALUES (
			$1, 'welcome',
			'Welcome to Google-Monetize!',
			'Thank you for joining Google-Monetize! You have received 1000 free tokens to get started. Try creating your first offer to see AI-powered optimization in action.',
			NOW()
		)
	`

	_, err := h.db.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("insert welcome notification: %w", err)
	}

	return nil
}

// initializeCheckin 初始化签到系统
func (h *OnboardingHandler) initializeCheckin(ctx context.Context, userID string) error {
	// 初始化签到统计表 (使用现有表结构)
	query := `
		INSERT INTO user_checkin_stats (
			user_id, total_checkins, total_tokens_earned, this_month_checkins, last_checkin_date, updated_at
		) VALUES (
			$1::uuid, 0, 0, 0, NULL, NOW()
		)
		ON CONFLICT (user_id) DO NOTHING
	`

	_, err := h.db.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("initialize checkin stats: %w", err)
	}

	return nil
}

// initializeReferral 初始化邀请数据结构
func (h *OnboardingHandler) initializeReferral(ctx context.Context, userID string) error {
	// 为新用户生成邀请码 (使用现有表结构: referrer_user_id, status)
	query := `
		INSERT INTO referrals (
			referrer_user_id, referral_code, status, created_at
		) VALUES (
			$1::uuid,
			substring(md5(random()::text || $1) from 1 for 8),  -- 生成8位随机邀请码
			'pending',
			NOW()
		)
		ON CONFLICT (referral_code) DO NOTHING
	`

	_, err := h.db.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("initialize referral: %w", err)
	}

	return nil
}

// OnboardingStatusResponse onboarding状态响应
type OnboardingStatusResponse struct {
	Completed               bool   `json:"completed"`
	DemoOffersCreated       int    `json:"demoOffersCreated"`
	WelcomeNotificationSent bool   `json:"welcomeNotificationSent"`
	CheckinInitialized      bool   `json:"checkinInitialized"`
	ReferralCodeGenerated   string `json:"referralCodeGenerated,omitempty"`
	subscriptionsActive      bool   `json:"subscriptionActive"`
	TokenBalance            int    `json:"tokenBalance"`
}

// GetOnboardingStatus handles GET /api/v1/user/onboarding-status
func (h *OnboardingHandler) GetOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from query parameter or context
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	status, err := h.getOnboardingStatus(ctx, userID)
	if err != nil {
		log.Printf("Failed to get onboarding status for user %s: %v", userID, err)
		http.Error(w, fmt.Sprintf("Failed to get onboarding status: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// getOnboardingStatus retrieves onboarding completion status
func (h *OnboardingHandler) getOnboardingStatus(ctx context.Context, userID string) (*OnboardingStatusResponse, error) {
	status := &OnboardingStatusResponse{}

	// Check demo offers count
	var demoCount int
	err := h.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM offers WHERE user_id = $1 AND is_demo = true
	`, userID).Scan(&demoCount)
	if err != nil {
		log.Printf("Warning: Failed to count demo offers for user %s: %v", userID, err)
	}
	status.DemoOffersCreated = demoCount

	// Check welcome notification
	var hasWelcome bool
	err = h.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM user_notifications WHERE user_id = $1 AND type = 'welcome')
	`, userID).Scan(&hasWelcome)
	if err != nil {
		log.Printf("Warning: Failed to check welcome notification for user %s: %v", userID, err)
	}
	status.WelcomeNotificationSent = hasWelcome

	// Check checkin initialization
	var hasCheckin bool
	err = h.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM user_checkin_stats WHERE user_id = $1)
	`, userID).Scan(&hasCheckin)
	if err != nil {
		log.Printf("Warning: Failed to check checkin for user %s: %v", userID, err)
	}
	status.CheckinInitialized = hasCheckin

	// Check referral code
	var referralCode string
	err = h.db.QueryRow(ctx, `
		SELECT referral_code FROM referrals WHERE referrer_user_id = $1 LIMIT 1
	`, userID).Scan(&referralCode)
	if err == nil {
		status.ReferralCodeGenerated = referralCode
	}

	// Check subscription
	var hassubscriptions bool
	err = h.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM "subscriptions" WHERE "user_id" = $1 AND status = 'active')
	`, userID).Scan(&hassubscriptions)
	if err != nil {
		log.Printf("Warning: Failed to check subscription for user %s: %v", userID, err)
	}
	status.subscriptionsActive = hassubscriptions

	// Check token balance
	var tokenBalance int
	err = h.db.QueryRow(ctx, `
		SELECT balance FROM "user_tokens" WHERE "user_id" = $1
	`, userID).Scan(&tokenBalance)
	if err != nil {
		log.Printf("Warning: Failed to get token balance for user %s: %v", userID, err)
	}
	status.TokenBalance = tokenBalance

	// Determine if onboarding is completed
	status.Completed = demoCount >= 8 && hasWelcome && hasCheckin && referralCode != "" && hassubscriptions

	return status, nil
}

// HandleOnboardingAPI HTTP API endpoint for manual onboarding trigger
func (h *OnboardingHandler) HandleOnboardingAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req OnboardingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	// 执行初始化
	err := h.InitializeNewUser(r.Context(), req.UserID, req.Email)

	resp := OnboardingResponse{
		Success:            err == nil,
		UserID:             req.UserID,
		InitializedModules: make(map[string]bool),
	}

	if err != nil {
		resp.Errors = map[string]string{
			"general": err.Error(),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RetryOnboarding handles POST /api/v1/user/onboarding-retry
func (h *OnboardingHandler) RetryOnboarding(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		UserID string `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	log.Printf("[Onboarding] Manual retry requested for user=%s", req.UserID)

	// Execute onboarding initialization
	err := h.InitializeNewUser(ctx, req.UserID, "")

	if err != nil {
		log.Printf("[Onboarding] Manual retry failed for user=%s: %v", req.UserID, err)
		http.Error(w, fmt.Sprintf("Failed to retry onboarding: %v", err), http.StatusInternalServerError)
		return
	}

	// Get updated status
	status, err := h.getOnboardingStatus(ctx, req.UserID)
	if err != nil {
		log.Printf("Warning: Failed to get status after retry for user %s: %v", req.UserID, err)
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Onboarding retry completed",
		"status":  status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
