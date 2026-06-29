package main

import (
	"context"
	"database/sql"
	"encoding/json"
	stdlog "log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/pkg/errors"
	"github.com/ScientificInternet/Google-Monetize/pkg/events"
	"github.com/ScientificInternet/Google-Monetize/pkg/logger"
	"github.com/ScientificInternet/Google-Monetize/pkg/middleware"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/handlers"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/notifications"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/schedulers"
		"github.com/ScientificInternet/Google-Monetize/services/billing/internal/tokens"
)

var (
	adapter database.DatabaseAdapter
	ctx     = context.Background()
	log     = logger.Get()
	pub     *events.Publisher
)

// token_transactions represents the structure for a single transaction record.
type token_transactions struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Amount      int       `json:"amount"`
	Source      string    `json:"source"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// subscriptions represents the structure for the user's current plan.
type subscriptions struct {
	ID               string     `json:"id"`
	PlanName         string     `json:"plan_name"`
	Status           string     `json:"status"`
	TrialEndsAt      *time.Time `json:"trialEndsAt,omitempty"`
	CurrentPeriodEnd time.Time  `json:"currentPeriodEnd"`
}

func main() {
	log.Info().Msg("Billing Service starting...")

	// 🔧 使用FinalAdapter统一数据库访问
	// 适配器会自动选择最佳的实现方式
	adapter, err := database.GetFinalAdapterForService("billing-service")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize database adapter")
	}
	defer adapter.Close()

	// Test database connection
	if err := adapter.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to ping database")
	}
	log.Info().Msg("Successfully connected to database with FinalAdapter!")

	// Get database connection for compatibility
	db = adapter.GetDB()

	// Check if we should run migrations
	if strings.EqualFold(strings.TrimSpace(os.Getenv("BILLING_SKIP_MIGRATIONS")), "1") {
		log.Info().Msg("BILLING_SKIP_MIGRATIONS=1 -> skipping DB migrations at startup")
	} else {
		// 使用FinalAdapter的迁移功能
		log.Info().Msg("Database migrations should be handled via CI/CD pipeline")
	}

	// FinalAdapter manages connection pools automatically

	// Initialize event publisher
	pub, err = events.NewPublisher(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize event publisher")
	}

	// Get service URLs from environment
	offerServiceURL := os.Getenv("OFFER_SERVICE_URL")
	if offerServiceURL == "" {
		offerServiceURL = "http://offer:8080" // Default internal service URL
	}
	userActivityURL := os.Getenv("USERACTIVITY_SERVICE_URL")
	if userActivityURL == "" {
		userActivityURL = "http://useractivity:8080" // Default internal service URL
	}

	// Initialize onboarding handler for new user initialization
	onboardingHandler := handlers.NewOnboardingHandler(pgxPool, offerServiceURL, userActivityURL)

	// Get console service URL for notifications
	consoleServiceURL := os.Getenv("CONSOLE_SERVICE_URL")
	if consoleServiceURL == "" {
		consoleServiceURL = "http://console:8080" // Default internal service URL
	}

	// Initialize token service
	tokenService := tokens.NewService(pgxPool, nil) // cache can be nil for now

	// Initialize notification client for in-app notifications
	notificationClient := notifications.NewNotificationClient(consoleServiceURL)

	// Initialize pending subscription handler
	pendingSubHandler := handlers.NewPendingsubscriptionsHandler(adapter, pub, tokenService, notificationClient)

	// Initialize handlers
	trialHandler := handlers.NewTrialsubscriptionsHandler(adapter, pub, onboardingHandler, pendingSubHandler)
	permissionHandler := handlers.NewPermissionHandler(adapter, nil, pub)
	tokenCostHandler := handlers.NewTokenCostHandler(adapter, nil, pub)
	configHandler := handlers.NewsubscriptionsConfigHandler(adapter, nil, pub)

	// --- HTTP Server Setup ---
	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("/health", healthCheckHandler)
	mux.HandleFunc("/healthz", healthCheckHandler)
	mux.HandleFunc("/readyz", healthCheckHandler)

	// Public marketing endpoints (no auth required)
	mux.HandleFunc("/public/marketing/summary", getMarketingSummaryHandler)

	// Define protected routes that require authentication.
	protectedRoutes := http.NewServeMux()

	// Legacy endpoints
	protectedRoutes.HandleFunc("/tokens/balance", getTokenBalanceHandler)
	protectedRoutes.HandleFunc("/tokens/transactions", gettoken_transactionssHandler)
	protectedRoutes.HandleFunc("/subscription", getsubscriptionsHandler)
	protectedRoutes.HandleFunc("/tokens/reserve", reserveTokensHandler)
	protectedRoutes.HandleFunc("/tokens/commit", commitTokensHandler)
	protectedRoutes.HandleFunc("/tokens/release", releaseTokensHandler)

	// New subscription endpoints
	protectedRoutes.HandleFunc("/billing/subscriptions/trial", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			trialHandler.CreateTrial(w, r)
		}
	})
	protectedRoutes.HandleFunc("/billing/subscriptions/trial/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			trialHandler.GetTrialHistory(w, r)
		}
	})

	// New permission endpoints
	protectedRoutes.HandleFunc("/billing/permissions/check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			permissionHandler.CheckPermission(w, r)
		}
	})
	// Legacy permissions endpoint for frontend compatibility
	protectedRoutes.HandleFunc("/permissions/check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			permissionHandler.CheckPermission(w, r)
		}
	})
	protectedRoutes.HandleFunc("/billing/config/permissions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			permissionHandler.GetPermissions(w, r)
		} else if r.Method == http.MethodPut {
			permissionHandler.UpdatePermission(w, r)
		}
	})

	// New token cost endpoints
	protectedRoutes.HandleFunc("/billing/tokens/cost", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			tokenCostHandler.GetTokenCost(w, r)
		}
	})
	protectedRoutes.HandleFunc("/billing/config/token-costs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			tokenCostHandler.GetTokenCosts(w, r)
		} else if r.Method == http.MethodPut {
			tokenCostHandler.UpdateTokenCost(w, r)
		}
	})

	// New config endpoints
	protectedRoutes.HandleFunc("/billing/config/all", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			configHandler.GetAllConfig(w, r)
		}
	})
	protectedRoutes.HandleFunc("/billing/config/pricing", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			configHandler.GetPricing(w, r)
		}
	})
	protectedRoutes.HandleFunc("/billing/config/pricing/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			configHandler.UpdatePricing(w, r)
		}
	})
	protectedRoutes.HandleFunc("/billing/config/history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			configHandler.GetConfigHistory(w, r)
		}
	})

	// Internal endpoint for trial expiration
	mux.HandleFunc("/internal/v1/trials/expire", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			trialHandler.ExpireTrials(w, r)
		}
	})

	// Internal endpoint for executing subscription config migration
	mux.HandleFunc("/internal/v1/migrate-subscription-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			executesubscriptionsConfigMigration(w, r)
		}
	})

	// Apply auth middleware to protected routes.
	mux.Handle("/api/", http.StripPrefix("/api", middleware.AuthMiddleware(protectedRoutes)))

	// Initialize and start scheduler for background tasks
	scheduler := schedulers.NewsubscriptionsScheduler(pgxPool, pendingSubHandler, notificationClient)
	go scheduler.RunScheduler(ctx)
	log.Info().Msg("subscriptions scheduler started for background tasks")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Info().Str("port", port).Str("version", "v1.2.0").Msg("Billing service starting...")
	root := middleware.RequestID()(mux)
	if err := http.ListenAndServe(":"+port, root); err != nil {
		log.Fatal().Err(err).Msg("Failed to start server")
	}
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	if err := db.Ping(); err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "NOT_READY", "dependencies not ready", map[string]string{"db": err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// getMarketingSummaryHandler handles GET /public/marketing/summary
func getMarketingSummaryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Handle preflight requests
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Get current statistics
	var totalUsers, totalOffers, totalEvaluations int64

	// Get total users count
	err := db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM "User"
	`).Scan(&totalUsers)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query total users")
		totalUsers = 0
	}

	// Get total offers count
	err = db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM offers
	`).Scan(&totalOffers)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query total offers")
		totalOffers = 0
	}

	// Get total evaluations count
	err = db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM offer_evaluations
	`).Scan(&totalEvaluations)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query total evaluations")
		totalEvaluations = 0
	}

	// Get plan distribution
	type PlanStats struct {
		Plan  string `json:"plan"`
		Count int64  `json:"count"`
	}

	rows, err := db.QueryContext(r.Context(), `
		SELECT
			COALESCE(subscription_tier, 'unknown') as plan,
			COUNT(*) as count
		FROM users
		GROUP BY COALESCE(subscription_tier, 'unknown')
		ORDER BY count DESC
	`)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query plan distribution")
		rows = nil
	}

	var planStats []PlanStats
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var stat PlanStats
			if err := rows.Scan(&stat.Plan, &stat.Count); err == nil {
				planStats = append(planStats, stat)
			}
		}
	}

	// Marketing summary response
	summary := map[string]interface{}{
		"totalUsers":       totalUsers,
		"totalOffers":      totalOffers,
		"totalEvaluations": totalEvaluations,
		"planDistribution": planStats,
		"updated_at":        time.Now().UTC().Format(time.RFC3339),
		"version":          "v1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(summary)
}

func getTokenBalanceHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok || userID == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "User ID not found in context", nil)
		return
	}

	var balance int64
	err := db.QueryRowContext(r.Context(), `SELECT balance FROM "user_tokens" WHERE "user_id" = $1`, userID).Scan(&balance)
	if err != nil {
		if err == sql.ErrNoRows {
			balance = 0
		} else {
			log.Error().Err(err).Str("userID", userID).Msg("Failed to query token balance")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	// Get today's token consumption (deduct only, absolute value)
	var todayConsumed int64
	err = db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(ABS(amount)), 0)
		FROM "token_transactions"
		WHERE "user_id" = $1
		  AND type = 'deduct'
		  AND DATE("created_at") = CURRENT_DATE
	`, userID).Scan(&todayConsumed)
	if err != nil {
		log.Error().Err(err).Str("userID", userID).Msg("Failed to query today's consumption")
		todayConsumed = 0
	}

	// Get this month's token consumption
	var monthConsumed int64
	err = db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(ABS(amount)), 0)
		FROM "token_transactions"
		WHERE "user_id" = $1
		  AND type = 'deduct'
		  AND DATE_TRUNC('month', "created_at") = DATE_TRUNC('month', CURRENT_DATE)
	`, userID).Scan(&monthConsumed)
	if err != nil {
		log.Error().Err(err).Str("userID", userID).Msg("Failed to query month's consumption")
		monthConsumed = 0
	}

	// Get pending tasks count
	var pendingTasks int
	err = db.QueryRowContext(r.Context(), `
		SELECT COUNT(*)
		FROM tasks
		WHERE user_id = $1
		  AND status IN ('pending', 'processing')
	`, userID).Scan(&pendingTasks)
	if err != nil {
		log.Error().Err(err).Str("userID", userID).Msg("Failed to query pending tasks")
		pendingTasks = 0
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"balance":       balance,
		"todayConsumed": todayConsumed,
		"monthConsumed": monthConsumed,
		"pendingTasks":  pendingTasks,
	})
}

func gettoken_transactionssHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok || userID == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "User ID not found in context", nil)
		return
	}

	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "20"
	}
	offset := r.URL.Query().Get("offset")
	if offset == "" {
		offset = "0"
	}

	query := `
		SELECT id, type, amount, source, description, "created_at"
		FROM "token_transactions"
		WHERE "user_id" = $1
		ORDER BY "created_at" DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := db.QueryContext(r.Context(), query, userID, limit, offset)
	if err != nil {
		log.Error().Err(err).Str("userID", userID).Msg("Failed to query token transactions")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Internal server error", nil)
		return
	}
	defer rows.Close()

	var transactions []token_transactions
	for rows.Next() {
		var t token_transactions
		if err := rows.Scan(&t.ID, &t.Type, &t.Amount, &t.Source, &t.Description, &t.CreatedAt); err != nil {
			log.Error().Err(err).Str("userID", userID).Msg("Failed to scan transaction row")
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Internal server error", nil)
			return
		}
		transactions = append(transactions, t)
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Str("userID", userID).Msg("Error during transaction rows iteration")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Internal server error", nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(transactions)
}

func getsubscriptionsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized: User ID not found in context", http.StatusUnauthorized)
		return
	}

	var sub subscriptions
	query := `
		SELECT id, "plan_name", status, "trialEndsAt", "currentPeriodEnd"
		FROM "subscriptions"
		WHERE "user_id" = $1
	`
	err := db.QueryRowContext(r.Context(), query, userID).Scan(
		&sub.ID, &sub.PlanName, &sub.Status, &sub.TrialEndsAt, &sub.CurrentPeriodEnd,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			errors.Write(w, r, http.StatusNotFound, "NOT_FOUND", "subscriptions not found", nil)
			return
		}
		log.Error().Err(err).Str("userID", userID).Msg("Failed to query subscription")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Internal server error", nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sub)
}

// reserveTokensHandler handles POST /tokens/reserve
func reserveTokensHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok || userID == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "User ID not found in context", nil)
		return
	}

	var req struct {
		Amount int    `json:"amount"`
		TaskID string `json:"taskId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body", nil)
		return
	}

	if req.Amount <= 0 {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_AMOUNT", "Amount must be positive", nil)
		return
	}

	// Begin transaction
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to begin transaction")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Internal server error", nil)
		return
	}
	defer tx.Rollback()

	// Check and lock user balance
	var balance int64
	err = tx.QueryRowContext(r.Context(), `
		SELECT balance FROM "user_tokens" WHERE "user_id" = $1 FOR UPDATE
	`, userID).Scan(&balance)

	if err == sql.ErrNoRows {
		errors.Write(w, r, http.StatusNotFound, "USER_TOKEN_NOT_FOUND", "User token record not found", nil)
		return
	}
	if err != nil {
		log.Error().Err(err).Msg("Failed to query balance")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Internal server error", nil)
		return
	}

	// Check sufficient balance
	if balance < int64(req.Amount) {
		errors.Write(w, r, http.StatusPaymentRequired, "INSUFFICIENT_TOKENS",
			"Insufficient token balance", map[string]interface{}{
				"required": req.Amount,
				"balance":  balance,
			})
		return
	}

	// Deduct tokens
	_, err = tx.ExecContext(r.Context(), `
		UPDATE "user_tokens"
		SET balance = balance - $1, "updated_at" = NOW()
		WHERE "user_id" = $2
	`, req.Amount, userID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to deduct tokens")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to deduct tokens", nil)
		return
	}

	// Create transaction record
	txID := "tx_" + time.Now().Format("20060102150405") + "_" + userID[:8]
	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO "token_transactions" (id, "user_id", type, amount, source, description, "created_at")
		VALUES ($1, $2, 'deduct', $3, 'evaluation', $4, NOW())
	`, txID, userID, -req.Amount, "Reserve for task "+req.TaskID)

	if err != nil {
		log.Error().Err(err).Msg("Failed to create transaction")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to create transaction", nil)
		return
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		log.Error().Err(err).Msg("Failed to commit transaction")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to commit transaction", nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"txId":   txID,
		"status": "reserved",
	})
}

// commitTokensHandler handles POST /tokens/commit
func commitTokensHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok || userID == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "User ID not found in context", nil)
		return
	}

	var req struct {
		TxID string `json:"txId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body", nil)
		return
	}

	// Get current balance
	var balance int64
	err := db.QueryRowContext(r.Context(), `SELECT balance FROM "user_tokens" WHERE "user_id" = $1`, userID).Scan(&balance)
	if err != nil {
		balance = 0
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"txId":    req.TxID,
		"debitId": req.TxID,
		"status":  "committed",
		"balance": balance,
	})
}

// releaseTokensHandler handles POST /tokens/release (refund)
func releaseTokensHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok || userID == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "User ID not found in context", nil)
		return
	}

	var req struct {
		TxID   string `json:"txId"`
		Amount int    `json:"amount"`
		TaskID string `json:"taskId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body", nil)
		return
	}

	// Begin transaction
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to begin transaction")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Internal server error", nil)
		return
	}
	defer tx.Rollback()

	// Refund tokens
	_, err = tx.ExecContext(r.Context(), `
		UPDATE "user_tokens"
		SET balance = balance + $1, "updated_at" = NOW()
		WHERE "user_id" = $2
	`, req.Amount, userID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to refund tokens")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to refund tokens", nil)
		return
	}

	// Create refund transaction
	refundID := "refund_" + time.Now().Format("20060102150405") + "_" + userID[:8]
	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO "token_transactions" (id, "user_id", type, amount, source, description, "created_at")
		VALUES ($1, $2, 'refund', $3, 'evaluation', $4, NOW())
	`, refundID, userID, req.Amount, "Refund for task "+req.TaskID+" (original: "+req.TxID+")")

	if err != nil {
		log.Error().Err(err).Msg("Failed to create refund transaction")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to create refund transaction", nil)
		return
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		log.Error().Err(err).Msg("Failed to commit refund")
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Failed to commit refund", nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"txId":   refundID,
		"status": "released",
	})
}

// executesubscriptionsConfigMigration executes the subscription config tables migration
func executesubscriptionsConfigMigration(w http.ResponseWriter, r *http.Request) {
	// Read migration SQL from embedded file or use hardcoded SQL
	migrationSQL := `
	-- 创建subscription_permissions表
	CREATE TABLE IF NOT EXISTS subscription_permissions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		feature VARCHAR(255) NOT NULL UNIQUE,
		feature_name VARCHAR(255) NOT NULL,
		category VARCHAR(50) NOT NULL,
		starter_value JSONB NOT NULL,
		professional_value JSONB NOT NULL,
		elite_value JSONB NOT NULL,
		display_only BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_by UUID REFERENCES "User"(id)
	);

	-- 创建索引
	CREATE INDEX IF NOT EXISTS idx_permissions_category ON subscription_permissions(category);
	CREATE INDEX IF NOT EXISTS idx_permissions_feature ON subscription_permissions(feature);

	-- 创建subscription_token_costs表
	CREATE TABLE IF NOT EXISTS subscription_token_costs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		action VARCHAR(255) NOT NULL UNIQUE,
		action_name VARCHAR(255) NOT NULL,
		category VARCHAR(50) NOT NULL,
		starter_cost JSONB NOT NULL,
		professional_cost JSONB NOT NULL,
		elite_cost JSONB NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_by UUID REFERENCES "User"(id)
	);

	-- 创建索引
	CREATE INDEX IF NOT EXISTS idx_token_costs_category ON subscription_token_costs(category);
	CREATE INDEX IF NOT EXISTS idx_token_costs_action ON subscription_token_costs(action);

	-- 创建subscription_pricing表
	CREATE TABLE IF NOT EXISTS subscription_pricing (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		plan VARCHAR(50) NOT NULL UNIQUE,
		display_name VARCHAR(255) NOT NULL,
		description TEXT,
		badge VARCHAR(100),
		recommended BOOLEAN DEFAULT FALSE,
		token_quota INTEGER NOT NULL,
		monthly_amount INTEGER NOT NULL,
		monthly_stripe_price_id VARCHAR(255) NOT NULL,
		yearly_amount INTEGER NOT NULL,
		yearly_stripe_price_id VARCHAR(255) NOT NULL,
		yearly_discount INTEGER,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_by UUID REFERENCES "User"(id)
	);

	-- 创建索引
	CREATE INDEX IF NOT EXISTS idx_pricing_plan ON subscription_pricing(plan);
	CREATE INDEX IF NOT EXISTS idx_pricing_recommended ON subscription_pricing(recommended);

	-- 创建subscription_config_history表
	CREATE TABLE IF NOT EXISTS subscription_config_history (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		config_type VARCHAR(50) NOT NULL,
		config_id UUID NOT NULL,
		action VARCHAR(20) NOT NULL,
		old_value JSONB,
		new_value JSONB NOT NULL,
		changed_by UUID REFERENCES "User"(id),
		changed_at TIMESTAMPTZ DEFAULT NOW()
	);

	-- 创建索引
	CREATE INDEX IF NOT EXISTS idx_config_history_type ON subscription_config_history(config_type, changed_at DESC);
	CREATE INDEX IF NOT EXISTS idx_config_history_config_id ON subscription_config_history(config_id, changed_at DESC);
	CREATE INDEX IF NOT EXISTS idx_config_history_changed_by ON subscription_config_history(changed_by, changed_at DESC);

	-- 插入初始数据（忽略冲突）
	INSERT INTO subscription_permissions (feature, feature_name, category, starter_value, professional_value, elite_value) VALUES
	('dashboard.overview', 'permissions.features.dashboard.overview', 'dashboard', true, true, true),
	('dashboard.analytics', 'permissions.features.dashboard.analytics', 'dashboard', false, true, true),
	('dashboard.export', 'permissions.features.dashboard.export', 'dashboard', false, true, true),
	('offer.create', 'permissions.features.offer.create', 'offer', true, true, true),
	('offer.evaluate', 'permissions.features.offer.evaluate', 'offer', 5, 50, 500),
	('offer.evaluate.ai', 'permissions.features.offer.evaluate.ai', 'offer', 0, 50, 500),
	('offer.batch.evaluate', 'permissions.features.offer.batch.evaluate', 'offer', 0, 10, 100),
	('batchopen.create', 'permissions.features.batchopen.create', 'batchopen', 0, 10, 100),
	('batchopen.schedule', 'permissions.features.batchopen.schedule', 'batchopen', false, true, true),
	('adscenter.connect', 'permissions.features.adscenter.connect', 'adscenter', 1, 5, 50),
	('adscenter.manage', 'permissions.features.adscenter.manage', 'adscenter', false, true, true),
	('adscenter.bulk_operations', 'permissions.features.adscenter.bulk_operations', 'adscenter', false, false, true)
	ON CONFLICT (feature) DO NOTHING;

	INSERT INTO subscription_token_costs (action, action_name, category, starter_cost, professional_cost, elite_cost) VALUES
	('offer.evaluate', 'token_costs.actions.offer.evaluate', 'offer', 1, 1, 1),
	('offer.evaluate.ai', 'token_costs.actions.offer.evaluate.ai', 'offer', 'unsupported', 3, 3),
	('offer.batch.evaluate', 'token_costs.actions.offer.batch.evaluate', 'offer', 'unsupported', 1, 1),
	('batchopen.create', 'token_costs.actions.batchopen.create', 'batchopen', 'unsupported', 5, 5),
	('batchopen.execute', 'token_costs.actions.batchopen.execute', 'batchopen', 'unsupported', 1, 1),
	('adscenter.sync', 'token_costs.actions.adscenter.sync', 'adscenter', 1, 1, 1),
	('adscenter.bulk_sync', 'token_costs.actions.adscenter.bulk_sync', 'adscenter', 'unsupported', 5, 5)
	ON CONFLICT (action) DO NOTHING;

	INSERT INTO subscription_pricing (plan, display_name, description, badge, recommended, token_quota, monthly_amount, monthly_stripe_price_id, yearly_amount, yearly_stripe_price_id, yearly_discount) VALUES
	('starter', 'pricing.plans.starter.name', 'pricing.plans.starter.description', 'pricing.plans.starter.badge', false, 100, 0, 'price_starter_monthly', 0, 'price_starter_yearly', 0),
	('professional', 'pricing.plans.professional.name', 'pricing.plans.professional.description', 'pricing.plans.professional.badge', true, 1000, 2980, 'price_professional_monthly', 29800, 'price_professional_yearly', 17),
	('elite', 'pricing.plans.elite.name', 'pricing.plans.elite.description', 'pricing.plans.elite.badge', false, 10000, 29980, 'price_elite_monthly', 299800, 'price_elite_yearly', 17)
	ON CONFLICT (plan) DO NOTHING;
	`

	// Execute migration using pgxpool
	_, err := pgxPool.Exec(r.Context(), migrationSQL)
	if err != nil {
		log.Error().Err(err).Msg("Failed to execute subscription config migration")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	log.Info().Msg("subscriptions config migration executed successfully")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "subscriptions config migration completed successfully",
	})
}

// runMigrations runs all SQL migration files from internal/migrations directory
func runMigrations(databaseURL string) error {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	// Bound database readiness to avoid blocking Cloud Run startup
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		stdlog.Printf("WARN: DB not reachable during startup (skip migrations): %v", err)
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	migrationsDir := "internal/migrations"
	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			stdlog.Printf("No migrations directory found (%s); skipping DB migrations.", migrationsDir)
			return tx.Commit()
		}
		return err
	}

	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".sql") {
			stdlog.Printf("Applying migration: %s", file.Name())
			content, err := os.ReadFile(filepath.Join(migrationsDir, file.Name()))
			if err != nil {
				return err
			}
			statements := strings.Split(string(content), ";")
			for _, stmt := range statements {
				if strings.TrimSpace(stmt) != "" {
					if _, err := tx.Exec(stmt); err != nil {
						return err
					}
				}
			}
		}
	}

	return tx.Commit()
}
