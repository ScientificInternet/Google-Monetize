package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/pkg/errors"
	ev "github.com/ScientificInternet/Google-Monetize/pkg/events"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/domain"
	bevents "github.com/ScientificInternet/Google-Monetize/services/billing/internal/events"
)

// TrialsubscriptionsHandler handles trial subscription operations
type TrialsubscriptionsHandler struct {
	adapter           database.DatabaseAdapter
	pub               *ev.Publisher
	onboardingHandler *OnboardingHandler          // 新用户初始化协调器
	pendingSubHandler *PendingsubscriptionsHandler // 待激活套餐处理器
}

// NewTrialsubscriptionsHandler creates a new trial subscription handler
func NewTrialsubscriptionsHandler(adapter database.DatabaseAdapter, pub *ev.Publisher, onboarding *OnboardingHandler, pendingSub *PendingsubscriptionsHandler) *TrialsubscriptionsHandler {
	return &TrialsubscriptionsHandler{
		adapter:           adapter,
		pub:               pub,
		onboardingHandler: onboarding,
		pendingSubHandler: pendingSub,
	}
}

// CreateTrialRequest represents a request to create a trial subscription
type CreateTrialRequest struct {
	UserID string `json:"user_id"`
	Days   int    `json:"days"`
	Source string `json:"source"`
}

// CreateTrialResponse represents the response after creating a trial subscription
type CreateTrialResponse struct {
	subscriptionsID string    `json:"subscriptionId"`
	UserID         string    `json:"user_id"`
	Plan           string    `json:"plan"`
	Status         string    `json:"status"`
	TrialStartDate time.Time `json:"trialStartDate"`
	TrialEndDate   time.Time `json:"trialEndDate"`
	TokensGranted  int       `json:"tokensGranted"`
}

// TrialHistoryResponse represents trial subscription history
type TrialHistoryResponse struct {
	Items []TrialHistoryItem `json:"items"`
}

// TrialHistoryItem represents a single trial subscription record
type TrialHistoryItem struct {
	subscriptionsID string    `json:"subscriptionId"`
	Plan           string    `json:"plan"`
	Status         string    `json:"status"`
	TrialStartDate time.Time `json:"trialStartDate"`
	TrialEndDate   time.Time `json:"trialEndDate"`
	Source         string    `json:"source"`
}

// CreateTrial handles POST /api/v1/billing/subscriptions/trial
func (h *TrialsubscriptionsHandler) CreateTrial(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateTrialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body", map[string]string{"error": err.Error()})
		return
	}

	// Validate request
	if err := h.validateCreateTrialRequest(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}

	// Route based on trial source
	switch req.Source {
	case "referral_inviter":
		// Inviter: Always allow, extend or create trial
		log.Printf("[Trial] Processing referral inviter reward for user=%s", req.UserID)
		resp, err := h.extendOrCreateTrial(ctx, &req)
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to extend/create trial", map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)

	case "referral_invitee":
		// Invitee: Check if already received invitee reward
		log.Printf("[Trial] Processing referral invitee reward for user=%s", req.UserID)
		hasInviteeReward, err := h.hasTrialHistoryBySource(ctx, req.UserID, "referral_invitee")
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to check trial history", map[string]string{"error": err.Error()})
			return
		}
		if hasInviteeReward {
			log.Printf("[Trial] User %s already has invitee reward, rejecting", req.UserID)
			errors.Write(w, r, http.StatusConflict, "SUB_002", "用户已获得过被邀请奖励", map[string]string{"user_id": req.UserID})
			return
		}

		// Create invitee trial
		resp, err := h.createTrial(ctx, &req)
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to create trial", map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)

	case "self_register":
		// Self-register: Check if has any trial history (original logic)
		log.Printf("[Trial] Processing self-register trial for user=%s", req.UserID)
		hasTrialHistory, err := h.hasTrialHistory(ctx, req.UserID)
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to check trial history", map[string]string{"error": err.Error()})
			return
		}
		if hasTrialHistory {
			errors.Write(w, r, http.StatusConflict, "SUB_001", "用户已有试用订阅记录", map[string]string{"user_id": req.UserID})
			return
		}

		resp, err := h.createTrial(ctx, &req)
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to create trial", map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)

	default:
		errors.Write(w, r, http.StatusBadRequest, "INVALID_SOURCE", "Unknown trial source", map[string]string{"source": req.Source})
	}
}

// GetTrialHistory handles GET /api/v1/billing/subscriptions/trial/{user_id}
func (h *TrialsubscriptionsHandler) GetTrialHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "user_id is required", nil)
		return
	}

	resp, err := h.getTrialHistory(ctx, userID)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to get trial history", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// ExpireTrials handles POST /internal/v1/trials/expire (for scheduled tasks)
func (h *TrialsubscriptionsHandler) ExpireTrials(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// This is an internal endpoint, should be protected by service token
	// TODO: Add service token validation

	err := h.expireTrials(ctx)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to expire trials", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// createTrial creates a trial subscription for a user
func (h *TrialsubscriptionsHandler) createTrial(ctx context.Context, req *CreateTrialRequest) (*CreateTrialResponse, error) {
	// Create subscription domain object
	subscriptionID := uuid.New().String()
	sub := domain.NewTrialsubscriptionsWithSource(subscriptionID, req.UserID, req.Source, req.Days)

	// ✅ 完整的三层数据创建（事务保证）
	err := h.adapter.ExecuteInTransaction(ctx, []func(*sql.Tx) error{
		// Layer 2: 创建业务用户数据
		func(tx *sql.Tx) error {
			return h.createUserLayer(tx, req, subscriptionID)
		},
		// Layer 3: 创建计费账户数据
		func(tx *sql.Tx) error {
			return h.createBillingLayer(tx, req, sub)
		},
		// Layer 3: 初始化代币系统
		func(tx *sql.Tx) error {
			return h.initializeTokenSystem(tx, req, subscriptionID)
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create trial with three-layer architecture: %w", err)
	}

	// ✅ 三层架构完成后的代币已经在事务中处理，无需额外授权
	tokensGranted := 1000

	// Publish event
	if h.pub != nil {
		event := map[string]interface{}{
			"eventId":    uuid.New().String(),
			"eventType":  "subscriptionsTrialCreated",
			"occurredAt": time.Now().Format(time.RFC3339),
			"user_id":     req.UserID,
			"data": map[string]interface{}{
				"subscriptionId": subscriptionID,
				"plan":           sub.PlanName,
				"trialDays":      req.Days,
				"trialEndDate":   sub.TrialEndDate.Format(time.RFC3339),
				"source":         req.Source,
				"tokensGranted":  tokensGranted,
				"architecture":   "three-layer-transactional",
			},
		}

		err = h.pub.Publish(ctx, "subscription.trial.created", event)
		if err != nil {
			// Log warning but don't fail the trial creation
			fmt.Printf("Warning: Failed to publish subscriptionsTrialCreated event: %v\n", err)
		}
	}

	// 🎯 异步初始化其他服务，实现最终一致性
	go h.initializeAsyncServices(req.UserID)

	return &CreateTrialResponse{
		subscriptionsID: subscriptionID,
		UserID:         req.UserID,
		Plan:           sub.PlanName,
		Status:         sub.Status,
		TrialStartDate: *sub.TrialStartDate,
		TrialEndDate:   *sub.TrialEndDate,
		TokensGranted:  tokensGranted,
	}, nil
}

// extendOrCreateTrial extends an existing trial or creates a new one
// This is used for referral inviters who can accumulate trial rewards
func (h *TrialsubscriptionsHandler) extendOrCreateTrial(ctx context.Context, req *CreateTrialRequest) (*CreateTrialResponse, error) {
	// Maximum trial days limit to prevent abuse
	const MaxTrialDays = 365

	// Get tier for the new trial (always Professional = tier 2 for referral rewards)
	newTrialPlanID := "professional"
	newTrialTier := domain.GetTierForPlan(newTrialPlanID)

	// 1. Query for user's current active subscription (including trials)
	var currentSubID string
	var currentPlanID string
	var currentTier *int
	var currentEndDate time.Time
	var currentStartDate time.Time

	query := `
		SELECT id, COALESCE("planId", 'professional'), tier, "trialStartDate", "trialEndDate"
		FROM "subscriptions"
		WHERE "user_id" = $1
		  AND (status = 'active' OR status = 'trialing')
		  AND "trialEndDate" > NOW()
		ORDER BY "trialEndDate" DESC
		LIMIT 1
	`

	pool := h.adapter.GetCloudSQLPool()
	err := pool.QueryRow(ctx, query, req.UserID).Scan(&currentSubID, &currentPlanID, &currentTier, &currentStartDate, &currentEndDate)

	// 2a. If user has active subscription, check tier conflict
	if err == nil {
		// Get current subscription tier
		var activeTier int
		if currentTier != nil {
			activeTier = *currentTier
		} else {
			activeTier = domain.GetTierForPlan(currentPlanID)
		}

		// If new trial is lower tier than active subscription, create pending subscription
		if domain.IsLowerTier(newTrialTier, activeTier) {
			log.Printf("[Trial] New trial (tier=%d) is lower than active subscription (tier=%d), creating pending subscription for user=%s",
				newTrialTier, activeTier, req.UserID)

			if h.pendingSubHandler != nil {
				tokenQuota := 1000 // Professional plan tokens
				pending, err := h.pendingSubHandler.CreatePendingsubscriptions(
					ctx,
					req.UserID,
					newTrialPlanID,
					newTrialTier,
					req.Source,
					currentSubID,
					tokenQuota,
				)
				if err != nil {
					return nil, fmt.Errorf("failed to create pending subscription: %w", err)
				}

				// Return response indicating subscription is pending
				return &CreateTrialResponse{
					subscriptionsID: pending.ID,
					UserID:         req.UserID,
					Plan:           "Professional (Pending)",
					Status:         "pending",
					TrialStartDate: pending.CreatedAt,
					TrialEndDate:   pending.ExpiresAt,
					TokensGranted:  0, // No tokens granted yet
				}, nil
			}

			return nil, fmt.Errorf("pending subscription handler not available")
		}

		// If same tier or higher tier, proceed with extension/upgrade
		// Calculate new end date
		newEndDate := currentEndDate.Add(time.Duration(req.Days) * 24 * time.Hour)

		// Cap at maximum trial days
		maxEndDate := time.Now().Add(MaxTrialDays * 24 * time.Hour)
		if newEndDate.After(maxEndDate) {
			newEndDate = maxEndDate
			log.Printf("[Trial] Capped trial extension at %d days for user=%s", MaxTrialDays, req.UserID)
		}

		// Update trial end date
		updateQuery := `
			UPDATE "subscriptions"
			SET "trialEndDate" = $1,
			    "updated_at" = NOW()
			WHERE id = $2
			RETURNING id, "trialStartDate", "trialEndDate"
		`

		var response CreateTrialResponse
		err = pool.QueryRow(ctx, updateQuery, newEndDate, currentSubID).Scan(
			&response.subscriptionsID,
			&response.TrialStartDate,
			&response.TrialEndDate,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to extend trial: %w", err)
		}

		// ✅ 使用三层架构事务模式处理代币扩展
		tokensGranted := 1000
		err = h.adapter.ExecuteInTransaction(ctx, []func(*sql.Tx) error{
			func(tx *sql.Tx) error {
				return h.grantAdditionalTokens(tx, req.UserID, tokensGranted, "trial_extension", map[string]any{
					"subscriptionId": currentSubID,
					"source":         req.Source,
					"trialDays":      req.Days,
					"extensionType":  "referral_reward",
				})
			},
		})
		if err != nil {
			// Log warning but don't fail the extension
			fmt.Printf("Warning: Failed to grant tokens for trial extension %s: %v\n", currentSubID, err)
		}

		// Publish trial extended event
		if h.pub != nil {
			event := map[string]interface{}{
				"eventId":    uuid.New().String(),
				"eventType":  "subscriptionsTrialExtended",
				"occurredAt": time.Now().Format(time.RFC3339),
				"user_id":     req.UserID,
				"data": map[string]interface{}{
					"subscriptionId": currentSubID,
					"extensionDays":  req.Days,
					"newEndDate":     newEndDate.Format(time.RFC3339),
					"source":         req.Source,
					"tokensGranted":  tokensGranted,
				},
			}

			err = h.pub.Publish(ctx, "subscription.trial.extended", event)
			if err != nil {
				fmt.Printf("Warning: Failed to publish subscriptionsTrialExtended event: %v\n", err)
			}
		}

		log.Printf("[Trial] Extended trial for user=%s by %d days, new end date=%s, tokens=%d",
			req.UserID, req.Days, newEndDate.Format(time.RFC3339), tokensGranted)

		response.UserID = req.UserID
		response.Plan = "Professional"
		response.Status = "active"
		response.TokensGranted = tokensGranted

		return &response, nil
	}

	// 2b. If no active trial (or trial expired), create new trial
	log.Printf("[Trial] No active trial found for user=%s, creating new trial", req.UserID)
	return h.createTrial(ctx, req)
}

// getTrialHistory returns the trial subscription history for a user
func (h *TrialsubscriptionsHandler) getTrialHistory(ctx context.Context, userID string) (*TrialHistoryResponse, error) {
	query := `
		SELECT id, "plan_name", status, "trialStartDate", "trialEndDate", "trialSource"
		FROM "subscriptions"
		WHERE "user_id" = $1 AND "trialStartDate" IS NOT NULL
		ORDER BY "created_at" DESC
	`

	pool := h.adapter.GetCloudSQLPool()
	rows, err := pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query trial history: %w", err)
	}
	defer rows.Close()

	var items []TrialHistoryItem
	for rows.Next() {
		var item TrialHistoryItem
		var trialStartDate, trialEndDate *time.Time
		var trialSource *string

		err := rows.Scan(
			&item.subscriptionsID,
			&item.Plan,
			&item.Status,
			&trialStartDate,
			&trialEndDate,
			&trialSource,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan trial history row: %w", err)
		}

		if trialStartDate != nil {
			item.TrialStartDate = *trialStartDate
		}
		if trialEndDate != nil {
			item.TrialEndDate = *trialEndDate
		}
		if trialSource != nil {
			item.Source = *trialSource
		}

		items = append(items, item)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate trial history rows: %w", err)
	}

	return &TrialHistoryResponse{
		Items: items,
	}, nil
}

// expireTrials processes expired trial subscriptions
func (h *TrialsubscriptionsHandler) expireTrials(ctx context.Context) error {
	fmt.Println("Running trial expiration check...")

	// Find all expired trial subscriptions
	query := `
		SELECT DISTINCT "user_id", id
		FROM "subscriptions"
		WHERE status = 'trialing' AND "trialEndDate" < NOW()
		LIMIT 1000
	`

	pool := h.adapter.GetCloudSQLPool()
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query expired trials: %w", err)
	}
	defer rows.Close()

	type expiredSub struct {
		UserID         string
		subscriptionsID string
	}

	var expiredsubscriptionss []expiredSub
	for rows.Next() {
		var userID, subscriptionID string
		if err := rows.Scan(&userID, &subscriptionID); err != nil {
			fmt.Printf("Scan error: %v\n", err)
			continue
		}
		expiredsubscriptionss = append(expiredsubscriptionss, expiredSub{
			UserID:         userID,
			subscriptionsID: subscriptionID,
		})
	}

	if len(expiredsubscriptionss) == 0 {
		fmt.Println("No trials expired")
		return nil
	}

	fmt.Printf("Found %d expired trial subscriptions\n", len(expiredsubscriptionss))

	// Update expired trials to expired status
	updateQuery := `
		UPDATE "subscriptions"
		SET status = 'expired', "updated_at" = NOW()
		WHERE status = 'trialing' AND "trialEndDate" < NOW()
	`

	result, err := pool.Exec(ctx, updateQuery)
	if err != nil {
		return fmt.Errorf("failed to update expired trials: %w", err)
	}

	rowsAffected := result.RowsAffected()
	fmt.Printf("Marked %d trial subscriptions as expired\n", rowsAffected)

	// Create Starter subscriptions for expired trial users
	for _, expired := range expiredsubscriptionss {
		err := h.createStartersubscriptions(ctx, expired.UserID)
		if err != nil {
			fmt.Printf("Failed to create Starter subscription for user %s: %v\n", expired.UserID, err)
			continue
		}

		// Publish expiration event
		if h.pub != nil {
			event := map[string]interface{}{
				"eventId":    uuid.New().String(),
				"eventType":  "subscriptionsTrialExpired",
				"occurredAt": time.Now().Format(time.RFC3339),
				"user_id":     expired.UserID,
				"data": map[string]interface{}{
					"subscriptionId": expired.subscriptionsID,
					"oldPlan":        "Pro",
					"newPlan":        "Free",
					"trialEndDate":   time.Now().Format(time.RFC3339),
				},
			}

			err = h.pub.Publish(ctx, "subscription.trial.expired", event)
			if err != nil {
				fmt.Printf("Warning: Failed to publish subscriptionsTrialExpired event: %v\n", err)
			}
		}

		fmt.Printf("User %s trial expired, downgraded to Starter plan\n", expired.UserID)
	}

	return nil
}

// hasTrialHistory checks if user has any trial subscription history
func (h *TrialsubscriptionsHandler) hasTrialHistory(ctx context.Context, userID string) (bool, error) {
	query := `
		SELECT COUNT(*)
		FROM "subscriptions"
		WHERE "user_id" = $1 AND "trialStartDate" IS NOT NULL
	`

	var count int
	pool := h.adapter.GetCloudSQLPool()
	err := pool.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check trial history: %w", err)
	}

	return count > 0, nil
}

// hasTrialHistoryBySource checks if user has trial subscription history from a specific source
// This is used to prevent invitees from getting multiple referral rewards
func (h *TrialsubscriptionsHandler) hasTrialHistoryBySource(ctx context.Context, userID, source string) (bool, error) {
	query := `
		SELECT COUNT(*)
		FROM "subscriptions"
		WHERE "user_id" = $1
		  AND "trialStartDate" IS NOT NULL
		  AND "trialSource" = $2
	`

	var count int
	err = pool.QueryRow(ctx, query, userID, source).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check trial history by source: %w", err)
	}

	return count > 0, nil
}

// createStartersubscriptions creates a Starter subscription for a user
func (h *TrialsubscriptionsHandler) createStartersubscriptions(ctx context.Context, userID string) error {
	subscriptionID := uuid.New().String()
	now := time.Now()

	query := `
		INSERT INTO "subscriptions" (
			id, "user_id", "plan_name", status, "created_at", "updated_at", "currentPeriodEnd"
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := pool.Exec(ctx, query,
		subscriptionID,
		userID,
		"Free",
		domain.StatusActive,
		now,
		now,
		now.AddDate(100, 0, 0), // 100 years for free plan
	)

	if err != nil {
		return fmt.Errorf("failed to create Starter subscription: %w", err)
	}

	return nil
}

// validateCreateTrialRequest validates the create trial request
func (h *TrialsubscriptionsHandler) validateCreateTrialRequest(req *CreateTrialRequest) error {
	if req.UserID == "" {
		return fmt.Errorf("user_id is required")
	}

	if req.Days != 7 && req.Days != 14 {
		return fmt.Errorf("days must be 7 or 14")
	}

	validSources := []string{
		domain.TrialSourceSelfRegister,
		domain.TrialSourceReferralInviter,
		domain.TrialSourceReferralInvitee,
	}

	for _, validSource := range validSources {
		if req.Source == validSource {
			return nil
		}
	}

	return fmt.Errorf("source must be one of: %s", strings.Join(validSources, ", "))
}

// ============================================================================
// Three-Layer Architecture Implementation
// ============================================================================

// TrialSubscriptionRequest represents the complete trial subscription data
type TrialSubscriptionRequest struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Source    string `json:"source"`
	Days      int    `json:"days"`
}

// createUserLayer 创建 Layer 2: 业务用户主数据
func (h *TrialsubscriptionsHandler) createUserLayer(tx *sql.Tx, req *CreateTrialRequest, subscriptionID string) error {
	// 注意：这里需要从认证系统获取用户详细信息
	// 在实际实现中，应该通过认证系统API获取
	email := fmt.Sprintf("user-%s@example.com", req.UserID) // 临时邮箱
	name := fmt.Sprintf("User %s", req.UserID[:8])          // 临时名称
	avatarURL := ""                                           // 临时头像

	query := `
		INSERT INTO user.users (
			id, email, name, avatar_url, status,
			language, timezone, preferences, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'active', 'zh', 'UTC', '{}', NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			email = EXCLUDED.email,
			name = EXCLUDED.name,
			avatar_url = EXCLUDED.avatar_url,
			updated_at = NOW()
	`

	_, err := tx.Exec(query, req.UserID, email, name, avatarURL)
	if err != nil {
		return fmt.Errorf("failed to create user layer data: %w", err)
	}

	// 创建用户权限记录
	permissionQuery := `
		INSERT INTO user.user_permissions (
			id, user_id, is_admin, subscription_plan, can_use_ai, can_create_offers,
			can_manage_ads, can_access_analytics, max_offers_per_month, max_tokens_per_month,
			can_export_data, created_at, updated_at
		) VALUES ($1, $2, false, 'professional', true, true, true, true, 100, 10000, true, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			subscription_plan = EXCLUDED.subscription_plan,
			can_use_ai = EXCLUDED.can_use_ai,
			can_create_offers = EXCLUDED.can_create_offers,
			can_manage_ads = EXCLUDED.can_manage_ads,
			can_access_analytics = EXCLUDED.can_access_analytics,
			max_offers_per_month = EXCLUDED.max_offers_per_month,
			max_tokens_per_month = EXCLUDED.max_tokens_per_month,
			updated_at = NOW()
	`

	permissionID := uuid.New().String()
	_, err = tx.Exec(permissionQuery, permissionID, req.UserID)
	if err != nil {
		return fmt.Errorf("failed to create user permissions: %w", err)
	}

	return nil
}

// createBillingLayer 创建 Layer 3: 计费账户数据
func (h *TrialsubscriptionsHandler) createBillingLayer(tx *sql.Tx, req *CreateTrialRequest, sub *domain.Trialsubscriptions) error {
	// 插入订阅记录
	subscriptionQuery := `
		INSERT INTO "subscriptions" (
			id, "user_id", "plan_name", status, "trialStartDate", "trialEndDate", "trialSource",
			"currentPeriodEnd", "created_at", "updated_at"
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
	`

	_, err := tx.Exec(subscriptionQuery,
		sub.ID,
		sub.UserID,
		sub.PlanName,
		sub.Status,
		sub.TrialStartDate,
		sub.TrialEndDate,
		sub.TrialSource,
		sub.CurrentPeriodEnd,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// 创建用户账户记录
	accountQuery := `
		INSERT INTO billing.user_accounts (
			id, user_id, account_status, billing_currency,
			trial_ends_at, created_at, updated_at
		) VALUES ($1, $2, 'trial', 'CNY', $3, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			account_status = EXCLUDED.account_status,
			trial_ends_at = EXCLUDED.trial_ends_at,
			updated_at = NOW()
	`

	accountID := uuid.New().String()
	_, err = tx.Exec(accountQuery, accountID, req.UserID, sub.TrialEndDate)
	if err != nil {
		return fmt.Errorf("failed to create user account: %w", err)
	}

	return nil
}

// initializeTokenSystem 初始化代币系统
func (h *TrialsubscriptionsHandler) initializeTokenSystem(tx *sql.Tx, req *CreateTrialRequest, subscriptionID string) error {
	// 创建代币余额记录
	balanceQuery := `
		INSERT INTO billing.token_balances (
			id, user_id, balance, reserved, available,
			total_earned, total_spent, last_updated, created_at, updated_at
		) VALUES ($1, $2, 1000, 0, 1000, 1000, 0, NOW(), NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			balance = token_balances.balance + 1000,
			available = token_balances.available + 1000,
			total_earned = token_balances.total_earned + 1000,
			last_updated = NOW(),
			updated_at = NOW()
	`

	balanceID := uuid.New().String()
	_, err := tx.Exec(balanceQuery, balanceID, req.UserID)
	if err != nil {
		return fmt.Errorf("failed to initialize token balance: %w", err)
	}

	// 记录初始充值交易
	transactionQuery := `
		INSERT INTO billing.token_transactions (
			id, user_id, token_type, amount, balance_before, balance_after,
			transaction_type, source, description, created_at
		) VALUES ($1, $2, 'search', 1000, 0, 1000, 'bonus', 'trial_registration', '试用期初始代币', NOW())
	`

	transactionID := uuid.New().String()
	_, err = tx.Exec(transactionQuery, transactionID, req.UserID)
	if err != nil {
		return fmt.Errorf("failed to record initial token transaction: %w", err)
	}

	return nil
}

// initializeAsyncServices 异步初始化其他服务，实现最终一致性
func (h *TrialsubscriptionsHandler) initializeAsyncServices(userID string) {
	// 异步初始化其他服务，实现最终一致性
	services := []struct {
		name string
		initFunc func(string) error
	}{
		{"Offer Service", h.initializeDemoOffers},
		{"User Activity Service", h.sendWelcomeNotification},
		{"Checkin Service", h.initializeCheckin},
		{"Referral Service", h.initializeReferral},
	}

	for _, service := range services {
		go func(svcName string, svcInit func(string) error) {
			if err := svcInit(userID); err != nil {
				log.Printf("Failed to initialize %s for user %s: %v", svcName, userID, err)
			} else {
				log.Printf("Successfully initialized %s for user %s", svcName, userID)
			}
		}(service.name, service.initFunc)
	}
}

// Placeholder implementations for async services
func (h *TrialsubscriptionsHandler) initializeDemoOffers(userID string) error {
	// TODO: 调用 offer service API 创建 demo offers
	log.Printf("Initializing demo offers for user %s", userID)
	return nil
}

func (h *TrialsubscriptionsHandler) sendWelcomeNotification(userID string) error {
	// TODO: 调用 notification service API 发送欢迎通知
	log.Printf("Sending welcome notification to user %s", userID)
	return nil
}

func (h *TrialsubscriptionsHandler) initializeCheckin(userID string) error {
	// TODO: 调用 checkin service API 初始化签到系统
	log.Printf("Initializing checkin system for user %s", userID)
	return nil
}

func (h *TrialsubscriptionsHandler) initializeReferral(userID string) error {
	// TODO: 调用 referral service API 初始化邀请系统
	log.Printf("Initializing referral system for user %s", userID)
	return nil
}

// grantAdditionalTokens 在事务中授予额外代币
func (h *TrialsubscriptionsHandler) grantAdditionalTokens(tx *sql.Tx, userID string, amount int, source string, metadata map[string]any) error {
	// 更新代币余额
	updateQuery := `
		UPDATE billing.token_balances
		SET balance = balance + $1,
			available = available + $1,
			total_earned = total_earned + $1,
			last_updated = NOW(),
			updated_at = NOW()
		WHERE user_id = $2
	`

	_, err := tx.Exec(updateQuery, amount, userID)
	if err != nil {
		return fmt.Errorf("failed to update token balance: %w", err)
	}

	// 记录交易
	transactionID := uuid.New().String()
	description := fmt.Sprintf("Trial extension tokens (%d)", amount)

	transactionQuery := `
		INSERT INTO billing.token_transactions (
			id, user_id, token_type, amount, balance_before, balance_after,
			transaction_type, source, description, metadata, created_at
		) VALUES ($1, $2, 'search', $3,
			(SELECT balance FROM billing.token_balances WHERE user_id = $2) - $3,
			(SELECT balance FROM billing.token_balances WHERE user_id = $2),
			'bonus', $4, $5, $6, NOW())
	`

	metadataJSON, _ := json.Marshal(metadata)
	_, err = tx.Exec(transactionQuery, transactionID, userID, amount, source, description, metadataJSON)
	if err != nil {
		return fmt.Errorf("failed to record token transaction: %w", err)
	}

	return nil
}

// 三层架构数据流完善方法
// =====================

// createUserLayer 创建Layer 2业务用户数据
func (h *TrialsubscriptionsHandler) createUserLayer(tx *sql.Tx, req *CreateTrialRequest, subscriptionID string) error {
	// Layer 2: 创建业务用户主数据
	userQuery := `
		INSERT INTO user.users (
			id, email, name, avatar_url, status,
			language, timezone, preferences, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'active', 'zh', 'UTC', '{}', NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			email = EXCLUDED.email,
			name = EXCLUDED.name,
			avatar_url = EXCLUDED.avatar_url,
			updated_at = NOW()
	`

	// 从请求中提取用户信息（需要在调用时提供完整数据）
	userName := req.UserID // 如果有具体名称字段，从请求中提取
	userEmail := req.UserID + "@example.com" // 如果有邮箱字段，从请求中提取
	userAvatar := ""

	_, err := tx.Exec(userQuery, req.UserID, userEmail, userName, userAvatar)
	if err != nil {
		return fmt.Errorf("failed to create user layer data: %w", err)
	}

	log.Printf("✅ Layer 2: Created user data for user=%s", req.UserID)
	return nil
}

// createBillingLayer 创建Layer 3计费账户数据
func (h *TrialsubscriptionsHandler) createBillingLayer(tx *sql.Tx, req *CreateTrialRequest, sub *domain.Trialsubscriptions) error {
	// Layer 3: 创建计费账户
	accountQuery := `
		INSERT INTO billing.accounts (
			user_id, account_type, status, balance_cents, created_at, updated_at
		) VALUES ($1, 'standard', 'trial', 0, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			status = 'trial',
			updated_at = NOW()
	`

	if _, err := tx.Exec(accountQuery, req.UserID); err != nil {
		return fmt.Errorf("failed to create billing account: %w", err)
	}

	// Layer 3: 创建试用订阅
	subscriptionQuery := `
		INSERT INTO billing.subscriptions (
			user_id, plan_name, status, current_period_start,
			current_period_end, trial_end, created_at, updated_at
		) VALUES ($1, $2, 'trial', NOW(), NOW() + INTERVAL '%d days', NOW() + INTERVAL '%d days', NOW(), NOW())
		ON CONFLICT (user_id, plan_name) DO UPDATE SET
			status = 'trial',
			trial_end = NOW() + INTERVAL '%d days',
			updated_at = NOW()
	`

	if _, err := tx.Exec(subscriptionQuery, req.UserID, sub.PlanName, req.Days, req.Days, req.Days); err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	log.Printf("✅ Layer 3: Created billing account and subscription for user=%s", req.UserID)
	return nil
}

// initializeTokenSystem 初始化代币系统
func (h *TrialsubscriptionsHandler) initializeTokenSystem(tx *sql.Tx, req *CreateTrialRequest, subscriptionID string) error {
	// Layer 3: 初始化代币余额
	tokenQuery := `
		INSERT INTO billing.token_balances (
			user_id, token_type, balance, created_at, updated_at
		) VALUES ($1, 'search', 100, NOW(), NOW())
		ON CONFLICT (user_id, token_type) DO UPDATE SET
			balance = balance + 100,
			updated_at = NOW()
	`

	if _, err := tx.Exec(tokenQuery, req.UserID); err != nil {
		return fmt.Errorf("failed to initialize token balance: %w", err)
	}

	// Layer 3: 记录初始充值交易
	transactionQuery := `
		INSERT INTO billing.token_transactions (
			user_id, token_type, amount, balance_before, balance_after,
			transaction_type, source, description, created_at
		) VALUES (
			$1, 'search', 100, 0, 100, 'bonus', 'trial_registration',
			'试用期初始代币', NOW()
		)
	`

	if _, err := tx.Exec(transactionQuery, req.UserID); err != nil {
		return fmt.Errorf("failed to record token transaction: %w", err)
	}

	log.Printf("✅ Layer 3: Initialized token system for user=%s with 100 tokens", req.UserID)
	return nil
}
