package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	stdErrors "errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"fmt"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
	pcache "github.com/ScientificInternet/Google-Monetize/pkg/cache"
	cfgpkg "github.com/ScientificInternet/Google-Monetize/pkg/config"
	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/pkg/errorreporting"
	"github.com/ScientificInternet/Google-Monetize/pkg/errors"
	ev "github.com/ScientificInternet/Google-Monetize/pkg/events"
	"github.com/ScientificInternet/Google-Monetize/pkg/middleware"
	"github.com/ScientificInternet/Google-Monetize/pkg/telemetry"
	config "github.com/ScientificInternet/Google-Monetize/services/billing/internal/config"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/handlers"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/notifications"
	api "github.com/ScientificInternet/Google-Monetize/services/billing/internal/oapi"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/schedulers"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/tokens"
	"strconv"

	bevents "github.com/ScientificInternet/Google-Monetize/services/billing/internal/events"
)

func main() {
	ctx := context.Background()

	// Setup distributed tracing (no-op if TRACES_ENABLED != 1)
	shutdownTracing := telemetry.SetupTracing("billing")
	defer func() { _ = shutdownTracing(context.Background()) }()

	// Setup error reporting (no-op if ERROR_REPORTING_ENABLED != 1)
	closeErrorReporting := errorreporting.Setup(ctx, "billing")
	defer closeErrorReporting()

	cfg, err := config.Load(ctx)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Minimal mode: skip DB and serve health endpoints only (for bring-up or emergency)
	if strings.EqualFold(strings.TrimSpace(os.Getenv("BILLING_MINIMAL")), "1") {
		r := chi.NewRouter()
		telemetry.RegisterDefaultMetrics("billing")
		r.Use(telemetry.ChiMiddleware("billing"))
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
		r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
		log.Printf("Billing (minimal) listening on :%s", cfg.Port)
		if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
			log.Fatalf("server: %v", err)
		}
		return
	}

	if !strings.EqualFold(strings.TrimSpace(os.Getenv("BILLING_SKIP_MIGRATIONS")), "1") {
		log.Println("Running database migrations...")
		if err := runMigrations(cfg.DatabaseURL); err != nil {
			log.Fatalf("Failed to run database migrations: %v", err)
		}
	} else {
		log.Println("BILLING_SKIP_MIGRATIONS=1 -> skipping DB migrations at startup")
	}

	// Initialize FinalAdapter for unified database access
	adapter, err := database.GetFinalAdapterForService("billing")
	if err != nil {
		log.Fatalf("Unable to create final database adapter: %v", err)
	}
	defer adapter.Close()

	// Get pgxpool for compatibility with existing handlers
	dbpool, ok := adapter.GetCloudSQLPool().(*pgxpool.Pool)
	if !ok {
		log.Fatalf("Expected pgxpool.Pool from adapter")
	}

	// Omit Pub/Sub subscriber in minimal deployment; projections run via CFs in prod.
	log.Println("Billing: skipping in-process Pub/Sub subscriber (use CFs in prod)")


	// Initialize cache (Redis or in-memory fallback)
	cache := pcache.NewFromEnv()
	if cache.Ready() {
		log.Println("Redis cache initialized successfully")
	} else {
		log.Println("Using in-memory cache fallback (REDIS_URL not configured)")
	}

	// Initialize three-layer user data validation middleware
	validationConfig := middleware.DefaultValidationConfig()
	validationConfig.StrictMode = true // billing service requires strict validation
	validationConfig.CriticalPaths = append(validationConfig.CriticalPaths,
		"/api/v1/billing/subscriptions",
		"/api/v1/billing/tokens/consume",
		"/api/v1/billing/tokens/credit/purchased",
		"/api/v1/billing/tokens/credit/subscription",
		"/api/v1/billing/tokens/credit/checkin",
	)

	threeLayerValidator := middleware.NewThreeLayerUserValidator(adapter, validationConfig)
	threeLayerMiddleware := threeLayerValidator.Middleware(validationConfig)

	// Initialize three-layer integration for auto-healing
	integrationConfig := middleware.IntegrationConfig{
		ServiceName:      "billing-service",
		ValidationConfig:  validationConfig,
			AutoHealMissing: true,
		HealTimeout:     10 * time.Second,
		EnableMetrics:    true,
	}
	threeLayerIntegration, err := middleware.NewThreeLayerIntegration("billing-service", integrationConfig)
	if err != nil {
		log.Fatalf("Failed to create three-layer integration: %v", err)
	}
	defer threeLayerIntegration.Close()

	// unified auth via pkg/middleware.AuthMiddleware
	// Pass adapter directly to handler for unified database access
	apiHandler := NewHandler(adapter, cache)
	r := chi.NewRouter()
	r.Use(middleware.RequestID())
	// P3-6: Enable gzip compression (level 5 balance between speed and compression)
	r.Use(chimiddleware.Compress(5))
	telemetry.RegisterDefaultMetrics("billing")
	// Middlewares must be registered before any routes on the mux
	r.Use(telemetry.ChiMiddleware("billing"))
	r.Use(middleware.LoggingMiddleware("billing"))
	r.Use(middleware.SecurityHeaders())
	// Routes
	r.Handle("/metrics", telemetry.MetricsHandler())
	r.Get("/health", apiHandler.healthz)
	r.Get("/healthz", apiHandler.healthz)
	// readiness: check DB and Redis (if configured)
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		cctx, cancel := context.WithTimeout(r.Context(), 800*time.Millisecond)
		defer cancel()
		if err := dbpool.Ping(cctx); err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "NOT_READY", "db not ready", map[string]string{"db": err.Error()})
			return
		}
		if strings.TrimSpace(os.Getenv("REDIS_URL")) != "" {
			rc := pcache.NewFromEnv()
			if rc == nil || !rc.Ready() {
				errors.Write(w, r, http.StatusInternalServerError, "NOT_READY", "redis client not ready", nil)
				return
			}
			pc, cc := context.WithTimeout(cctx, 300*time.Millisecond)
			defer cc()
			if err := rc.Redis().Ping(pc).Err(); err != nil {
				errors.Write(w, r, http.StatusInternalServerError, "NOT_READY", "redis ping failed", map[string]string{"redis": err.Error()})
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	// Atomic billing endpoints will be bound via OpenAPI chi server
	var pub *ev.Publisher
	if p, err := ev.NewPublisher(ctx); err == nil {
		pub = p
		defer p.Close()
	}

	// Get service URLs from environment for onboarding
	offerServiceURL := os.Getenv("OFFER_SERVICE_URL")
	if offerServiceURL == "" {
		offerServiceURL = "http://offer:8080" // Default internal service URL
	}
	userActivityURL := os.Getenv("USERACTIVITY_SERVICE_URL")
	if userActivityURL == "" {
		userActivityURL = "http://useractivity:8080"
	}
	consoleServiceURL := os.Getenv("CONSOLE_SERVICE_URL")
	if consoleServiceURL == "" {
		consoleServiceURL = "http://console:8080" // Default internal service URL
	}

	// Initialize onboarding handler for new user initialization
	onboardingHandler := handlers.NewOnboardingHandler(dbpool, offerServiceURL, userActivityURL)

	// Initialize token service
	tokenService := tokens.NewService(dbpool, cache)

	// Initialize notification client for in-app notifications
	notificationClient := notifications.NewNotificationClient(consoleServiceURL)

	// Initialize pending subscription handler
	pendingSubHandler := handlers.NewPendingsubscriptionsHandler(dbpool, sqldb, pub, tokenService, notificationClient)

	// Initialize trial subscription handler
	trialHandler := handlers.NewTrialsubscriptionsHandler(dbpool, sqldb, pub, onboardingHandler, pendingSubHandler)

	// Initialize scheduler for background tasks
	scheduler := schedulers.NewsubscriptionsScheduler(dbpool, pendingSubHandler, notificationClient)
	go scheduler.RunScheduler(ctx)

	// Initialize permission handler
	permissionHandler := handlers.NewPermissionHandler(dbpool, cache, pub)

	// Initialize token cost handler
	tokenCostHandler := handlers.NewTokenCostHandler(dbpool, cache, pub)

	// Initialize subscription config handler
	configHandler := handlers.NewsubscriptionsConfigHandler(dbpool, cache, pub)


	// Custom non-OAS endpoints first (so they aren't shadowed), all behind enhanced auth
	r.Group(func(rch chi.Router) {
		rch.Use(middleware.AuthMiddleware)
		rch.Get("/api/v1/billing/config", apiHandler.getBillingConfig)
		rch.Get("/api/v1/billing/tokens/transactions/{id}", apiHandler.gettoken_transactionsByID)
		rch.Get("/api/v1/billing/usage/report", apiHandler.getUsageReport)

		// Trial subscription endpoints
		rch.Post("/api/v1/billing/subscriptions/trial", trialHandler.CreateTrial)
		rch.Get("/api/v1/billing/subscriptions/trial/{user_id}", trialHandler.GetTrialHistory)

		// Onboarding endpoints
		rch.Get("/api/v1/user/onboarding-status", onboardingHandler.GetOnboardingStatus)
		rch.Post("/api/v1/user/onboarding-retry", onboardingHandler.RetryOnboarding)

		// Permission endpoints
		rch.Post("/api/v1/billing/permissions/check", permissionHandler.CheckPermission)
		rch.Get("/api/v1/billing/config/permissions", permissionHandler.GetPermissions)
		rch.Put("/api/v1/billing/config/permissions/{feature}", permissionHandler.UpdatePermission)

		// Token cost endpoints
		rch.Post("/api/v1/billing/tokens/cost", tokenCostHandler.GetTokenCost)
		rch.Get("/api/v1/billing/config/token-costs", tokenCostHandler.GetTokenCosts)
		rch.Put("/api/v1/billing/config/token-costs/{action}", tokenCostHandler.UpdateTokenCost)

		// subscriptions config endpoints
		rch.Get("/api/v1/billing/config/all", configHandler.GetAllConfig)
		rch.Get("/api/v1/billing/config/pricing", configHandler.GetPricing)
		rch.Put("/api/v1/billing/config/pricing/{plan}", configHandler.UpdatePricing)
		rch.Get("/api/v1/billing/config/history", configHandler.GetConfigHistory)

		// User-facing token endpoints（基于 Supabase 用户身份）
		rch.Get("/api/v1/tokens/balance", apiHandler.getTokenBalance)
		rch.Get("/api/v1/tokens/transactions", apiHandler.gettoken_transactionss)
		rch.Get("/api/v1/tokens/usage", apiHandler.getTokenUsageSummaryCurrentUser)

		// Legacy 兼容路径（迁移完成后下线）
		rch.Get("/api/v1/billing/tokens/me", apiHandler.getTokenBalance)
		rch.Handle("/api/v1/billing/tokens/transactions", http.HandlerFunc(apiHandler.gettoken_transactionss))

		// Token usage summary with service breakdown (for Console service)
		adminGroup := rch.With(middleware.AdminOnly)
		adminGroup.Get("/api/v1/tokens/{user_id}/usage", apiHandler.getTokenUsageSummary)
		adminGroup.Get("/api/v1/tokens/{user_id}/balance", apiHandler.getTokenBalanceAdmin)
		adminGroup.Get("/api/v1/tokens/{user_id}/transactions", apiHandler.gettoken_transactionssAdmin)

		// Credit endpoints (pool-aware). Idempotent via X-Idempotency-Key
		rch.Post("/api/v1/billing/tokens/credit/purchased", apiHandler.creditPurchased)
		rch.Post("/api/v1/billing/tokens/credit/subscription", apiHandler.creditsubscriptions)
		rch.Post("/api/v1/billing/tokens/credit/checkin", apiHandler.creditCheckin)
		// Consistency check (read-only): compare user_tokens vs user_tokensPool vs CreditLots
		rch.Get("/api/v1/billing/tokens/consistency", apiHandler.getTokenConsistency)
	})

	// Admin-only endpoints for consistency plan & repair
	r.Group(func(adm chi.Router) {
		adm.Use(middleware.AuthMiddleware)
		adm.Use(middleware.AdminOnly)
		adm.Get("/api/v1/billing/tokens/consistency/plan", apiHandler.getTokenConsistencyPlan)
		adm.Post("/api/v1/billing/tokens/consistency/repair", apiHandler.postTokenConsistencyRepair)
	})

	// Internal endpoints (for scheduled tasks)
	r.Group(func(internal chi.Router) {
		// TODO: Add service token validation
		internal.Post("/internal/v1/trials/expire", trialHandler.ExpireTrials)
	})

	// Bind OpenAPI chi server under /api/v1/billing
	apiHandler.Pub = pub
	oas := &oasImpl{h: apiHandler, pub: pub, trial: trialHandler}
	oapiHandler := api.HandlerWithOptions(oas, api.ChiServerOptions{
		BaseURL: "/api/v1/billing",
		Middlewares: []api.MiddlewareFunc{
			func(next http.Handler) http.Handler { return middleware.IdempotencyMiddleware(next) },
			func(next http.Handler) http.Handler { return middleware.AuthMiddleware(next) },
		},
	})
	r.Mount("/api/v1/billing", oapiHandler)

	log.Printf("Billing service v1.1.1 HTTP server listening on port %s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

// runMigrations remains the same...
// HTTP Handler and DTOs remain the same...

// The rest of the file (runMigrations, Handlers, DTOs, etc.) is unchanged.
// For brevity, it's omitted here but should be considered part of the file.
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
		log.Printf("WARN: DB not reachable during startup (skip migrations): %v", err)
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
			log.Printf("No migrations directory found (%s); skipping DB migrations.", migrationsDir)
			return tx.Commit()
		}
		return err
	}
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".sql") {
			log.Printf("Applying migration: %s", file.Name())
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

type Handler struct {
	Adapter *database.FinalAdapter
	DB      *pgxpool.Pool
	Pub     *ev.Publisher
	Cache   *pcache.Cache
}

func NewHandler(adapter *database.FinalAdapter, cache *pcache.Cache) *Handler {
	// Extract pgxpool for backward compatibility
	dbpool, ok := adapter.GetCloudSQLPool().(*pgxpool.Pool)
	if !ok {
		log.Fatalf("Expected pgxpool.Pool from adapter")
	}

	// For backward compatibility with existing credit functions that need *sql.DB
	// We'll create a bridge to use the same underlying connection
	var sqldb *sql.DB
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		if db, err := sql.Open("postgres", dbURL); err == nil {
			if err := db.Ping(); err == nil {
				sqldb = db
			} else {
				log.Printf("WARN: sql.DB ping failed, credit endpoints will be disabled: %v", err)
			}
		} else {
			log.Printf("WARN: sql.Open failed, credit endpoints will be disabled: %v", err)
		}
	}

	return &Handler{Adapter: adapter, DB: dbpool, SQLDB: sqldb, Cache: cache}
}
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/health", h.healthz)
	mux.Handle("/api/v1/billing/subscriptions/me", authMiddleware(http.HandlerFunc(h.getsubscriptions)))
	mux.Handle("/api/v1/billing/tokens/me", authMiddleware(http.HandlerFunc(h.getTokenBalance)))
	mux.Handle("/api/v1/billing/tokens/transactions", authMiddleware(http.HandlerFunc(h.gettoken_transactionss)))
	mux.Handle("/api/v1/tokens/balance", authMiddleware(http.HandlerFunc(h.getTokenBalance)))
	mux.Handle("/api/v1/tokens/transactions", authMiddleware(http.HandlerFunc(h.gettoken_transactionss)))
	mux.Handle("/api/v1/tokens/usage", authMiddleware(http.HandlerFunc(h.getTokenUsageSummaryCurrentUser)))
}
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

type subscriptions struct {
	ID               string    `json:"id"`
	PlanName         string    `json:"plan_name"`
	Status           string    `json:"status"`
	CurrentPeriodEnd time.Time `json:"currentPeriodEnd"`
}
type TokenBalance struct {
	Balance   int64     `json:"balance"`
	UpdatedAt time.Time `json:"updated_at"`
}
type adminTokenBalanceResponse struct {
	UserID    string    `json:"user_id"`
	Available int64     `json:"available"`
	Reserved  int64     `json:"reserved"`
	Total     int64     `json:"total"`
	UpdatedAt time.Time `json:"updated_at"`
}
type token_transactions struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Amount      int       `json:"amount"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type tokenTransactionsAdminResponse struct {
	Items      []token_transactions `json:"items"`
	TotalCount int                `json:"totalCount"`
	Limit      int                `json:"limit"`
	Offset     int                `json:"offset"`
}

type tokenTransactionQueryParams struct {
	Type      string
	Limit     int
	Offset    int
	StartDate *time.Time
	EndDate   *time.Time
}

type tokenUsageSummaryResponse struct {
	UserID        string         `json:"user_id"`
	TotalConsumed int            `json:"totalConsumed"`
	TotalTopUp    int            `json:"totalTopUp"`
	ByService     map[string]int `json:"byService"`
	StartDate     string         `json:"startDate"`
	EndDate       string         `json:"endDate"`
}

// getUsageReport returns aggregated usage for the current user within given days
// GET /api/v1/billing/usage/report?days=N
func (h *Handler) getUsageReport(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}
	days := 30
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	// Redis/Valkey short cache (30-60s)
	if c := pcache.NewFromEnv(); c != nil {
		key := fmt.Sprintf("billing:usage:%s:%d", uid, days)
		if raw, ok := c.Get(r.Context(), key); ok && strings.TrimSpace(raw) != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(raw))
			return
		}
		// Compute, marshal, then cache at the end
		// We'll marshal into buf and set later.
		// To keep structure, we keep original logic then marshal 'resp' and Set.
		// Implementation continues below.
		_ = key // placeholder to keep scope visible; actual Set will happen later
	}
	// optional breakdown dimension
	by := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("by")))
	wantModule := by == "module"
	// dailyUsage: tokensUsed per day (Allocation-first, fallback to token_transactions)
	rows, err := h.DB.Query(r.Context(), `
        WITH d AS (
          SELECT generate_series(date_trunc('day', NOW()) - ($1::int - 1) * interval '1 day', date_trunc('day', NOW()), interval '1 day') AS day
        ), alloc AS (
          SELECT date_trunc('day', td."created_at") AS day, SUM(alloc.amount) AS used
          FROM "TokenDebitAllocation" alloc
          JOIN "TokenDebit" td ON td.id = alloc.debit_id
          WHERE td."user_id"=$2 AND td."created_at" >= NOW() - ($1::int) * interval '1 day'
          GROUP BY 1
        )
        SELECT d.day::date, COALESCE(alloc.used,0)
        FROM d LEFT JOIN alloc ON alloc.day=d.day
        ORDER BY 1
    `, days, uid)
	if err != nil {
		// fallback to token_transactions
		rows, err = h.DB.Query(r.Context(), `
            WITH d AS (
              SELECT generate_series(date_trunc('day', NOW()) - ($1::int - 1) * interval '1 day', date_trunc('day', NOW()), interval '1 day') AS day
            ), tx AS (
              SELECT date_trunc('day', "created_at") AS day, SUM(ABS(amount)) AS used
              FROM "token_transactions"
              WHERE "user_id"=$2 AND lower(type)='debited' AND "created_at" >= NOW() - ($1::int) * interval '1 day'
              GROUP BY 1
            )
            SELECT d.day::date, COALESCE(tx.used,0)
            FROM d LEFT JOIN tx ON tx.day=d.day
            ORDER BY 1
        `, days, uid)
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "query failed", map[string]string{"error": err.Error()})
			return
		}
	}
	type du struct {
		Date       string `json:"date"`
		TokensUsed int64  `json:"tokensUsed"`
		ApiCalls   int    `json:"apiCalls"`
	}
	var daily []du
	for rows.Next() {
		var d time.Time
		var used int64
		if err := rows.Scan(&d, &used); err == nil {
			daily = append(daily, du{Date: d.Format("2006-01-02"), TokensUsed: used, ApiCalls: 0})
		}
	}
	rows.Close()
	// tokenBreakdownDaily by source category (Allocation-first, fallback to token_transactions)
	rows2, err := h.DB.Query(r.Context(), `
        WITH d AS (
          SELECT generate_series(date_trunc('day', NOW()) - ($1::int - 1) * interval '1 day', date_trunc('day', NOW()), interval '1 day') AS day
        ), src AS (
          SELECT date_trunc('day', td."created_at") AS day,
                 lower(tcl.source) AS category,
                 SUM(alloc.amount) AS used
          FROM "TokenDebitAllocation" alloc
          JOIN "TokenDebit" td ON td.id = alloc.debit_id
          JOIN "TokenCreditLot" tcl ON tcl.id = alloc.credit_lot_id
          WHERE td."user_id"=$2 AND td."created_at" >= NOW() - ($1::int) * interval '1 day'
          GROUP BY 1,2
        )
        SELECT d.day::date,
               COALESCE(SUM(CASE WHEN category='subscription' THEN used END),0) AS subscription,
               COALESCE(SUM(CASE WHEN category='activity' THEN used END),0)     AS activity,
               COALESCE(SUM(CASE WHEN category='purchased' THEN used END),0)    AS purchased
        FROM d LEFT JOIN src ON src.day=d.day
        GROUP BY 1
        ORDER BY 1
    `, days, uid)
	if err != nil {
		rows2, err = h.DB.Query(r.Context(), `
            WITH d AS (
              SELECT generate_series(date_trunc('day', NOW()) - ($1::int - 1) * interval '1 day', date_trunc('day', NOW()), interval '1 day') AS day
            ), tx AS (
              SELECT date_trunc('day', "created_at") AS day,
                     CASE 
                       WHEN lower(source) IN ('subscription','sub','recurring','stripe-subscription') THEN 'subscription'
                       WHEN lower(source) IN ('activity','reward','promo','bonus','invitation','checkin') THEN 'activity'
                       WHEN lower(source) IN ('purchase','purchased','oneoff','topup','store') THEN 'purchased'
                       ELSE 'purchased'
                     END AS category,
                     SUM(ABS(amount)) AS used
              FROM "token_transactions"
              WHERE "user_id"=$2 AND lower(type)='debited' AND "created_at" >= NOW() - ($1::int) * interval '1 day'
              GROUP BY 1,2
            )
            SELECT d.day::date,
                   COALESCE(SUM(CASE WHEN category='subscription' THEN used END),0) AS subscription,
                   COALESCE(SUM(CASE WHEN category='activity' THEN used END),0)     AS activity,
                   COALESCE(SUM(CASE WHEN category='purchased' THEN used END),0)    AS purchased
            FROM d LEFT JOIN tx ON tx.day=d.day
            GROUP BY 1
            ORDER BY 1
        `, days, uid)
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "query failed", map[string]string{"error": err.Error()})
			return
		}
	}
	type bd struct {
		Date         string `json:"date"`
		subscriptions int64  `json:"subscription"`
		Activity     int64  `json:"activity"`
		Purchased    int64  `json:"purchased"`
	}
	var breakdown []bd
	var totalUsed int64
	for rows2.Next() {
		var d time.Time
		var s, a, p int64
		if err := rows2.Scan(&d, &s, &a, &p); err == nil {
			breakdown = append(breakdown, bd{Date: d.Format("2006-01-02"), subscriptions: s, Activity: a, Purchased: p})
			totalUsed += s + a + p
		}
	}
	rows2.Close()
	// tokenDistribution (window usage split)
	var distSub, distAct, distPur int64
	for _, x := range breakdown {
		distSub += x.subscriptions
		distAct += x.Activity
		distPur += x.Purchased
	}
	// rateLimitStatus: derive from current balance
	var balance int64
	var updated time.Time
	_ = h.DB.QueryRow(r.Context(), `SELECT balance, "updated_at" FROM "user_tokens" WHERE "user_id"=$1`, uid).Scan(&balance, &updated)
	limit := balance + totalUsed
	if limit < 1000 {
		limit = 1000
	}
	percentage := 0
	if limit > 0 {
		percentage = int((totalUsed * 100) / limit)
	}
	// build response
	resp := map[string]any{
		"dailyUsage":          daily,
		"tokenBreakdownDaily": breakdown,
		"monthlyStats": map[string]any{
			"totalTokensUsed": totalUsed,
			"totalApiCalls":   0,
			"avgDailyUsage": func() int64 {
				if days > 0 {
					return totalUsed / int64(days)
				}
				return 0
			}(),
			"peakUsageDay": func() string {
				var max int64 = -1
				var d string
				for _, x := range daily {
					if x.TokensUsed > max {
						max = x.TokensUsed
						d = x.Date
					}
				}
				return d
			}(),
			"growthRate": 0,
		},
		"tokenDistribution": map[string]any{"subscription": distSub, "activity": distAct, "purchased": distPur},
		"rateLimitStatus":   map[string]any{"currentUsage": totalUsed, "limit": limit, "resetTime": time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour).Format(time.RFC3339), "percentage": percentage},
	}
	// module-level breakdown (Allocation-first)
	if wantModule {
		rowsM, err := h.DB.Query(r.Context(), `
            WITH d AS (
              SELECT generate_series(date_trunc('day', NOW()) - ($1::int - 1) * interval '1 day', date_trunc('day', NOW()), interval '1 day') AS day
            ), modu AS (
              SELECT date_trunc('day', td."created_at") AS day,
                     COALESCE(NULLIF(TRIM((td.meta->>'module')), ''), 'unknown') AS module,
                     SUM(alloc.amount) AS used
              FROM "TokenDebitAllocation" alloc
              JOIN "TokenDebit" td ON td.id = alloc.debit_id
              WHERE td."user_id"=$2 AND td."created_at" >= NOW() - ($1::int) * interval '1 day'
              GROUP BY 1,2
            )
            SELECT d.day::date, module, COALESCE(SUM(used),0)
            FROM d LEFT JOIN modu ON modu.day=d.day
            GROUP BY 1,2
            ORDER BY 1,2
        `, days, uid)
		if err == nil {
			type md struct {
				Date   string `json:"date"`
				Module string `json:"module"`
				Used   int64  `json:"used"`
			}
			mods := []md{}
			for rowsM.Next() {
				var d time.Time
				var m string
				var u int64
				if err := rowsM.Scan(&d, &m, &u); err == nil {
					mods = append(mods, md{Date: d.Format("2006-01-02"), Module: m, Used: u})
				}
			}
			rowsM.Close()
			resp["moduleBreakdownDaily"] = mods
		}
	}
	// Marshal once to reuse for cache and response
	b, _ := json.Marshal(resp)
	// Best-effort set cache 60s
	if c := pcache.NewFromEnv(); c != nil {
		key := fmt.Sprintf("billing:usage:%s:%d", uid, days)
		c.Set(r.Context(), key, string(b), 60*time.Second)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// getTokenUsageSummary 返回指定用户的 Token 使用摘要（管理员接口）。
func (h *Handler) getTokenUsageSummary(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if uid == "" {
		errors.Write(w, r, http.StatusBadRequest, "MISSING_USER_ID", "user_id is required", nil)
		return
	}

	h.handleTokenUsageSummary(w, r, uid)
}

// getTokenUsageSummaryCurrentUser 返回当前用户的 Token 使用摘要（基于 Supabase Session）。
func (h *Handler) getTokenUsageSummaryCurrentUser(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	uid = strings.TrimSpace(uid)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}

	h.handleTokenUsageSummary(w, r, uid)
}

func (h *Handler) handleTokenUsageSummary(w http.ResponseWriter, r *http.Request, uid string) {
	startDateStr := strings.TrimSpace(r.URL.Query().Get("startDate"))
	endDateStr := strings.TrimSpace(r.URL.Query().Get("endDate"))

	if startDateStr == "" || endDateStr == "" {
		errors.Write(w, r, http.StatusBadRequest, "MISSING_DATES", "startDate and endDate are required", nil)
		return
	}

	startDate, err := time.Parse(time.RFC3339, startDateStr)
	if err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_START_DATE", "invalid startDate format (expected RFC3339)", map[string]string{"error": err.Error()})
		return
	}

	endDate, err := time.Parse(time.RFC3339, endDateStr)
	if err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_END_DATE", "invalid endDate format (expected RFC3339)", map[string]string{"error": err.Error()})
		return
	}

	if startDate.After(endDate) {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_DATE_RANGE", "startDate must be before endDate", nil)
		return
	}

	summary, err := h.computeTokenUsageSummary(r.Context(), uid, startDate, endDate)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "QUERY_FAILED", "failed to query usage summary", map[string]string{"error": err.Error()})
		return
	}

	respondWithJSON(w, http.StatusOK, summary)

	adminID, adminEmail, actorSource := actorFromRequest(r)
	if adminID != "" && strings.TrimSpace(uid) != "" && !strings.EqualFold(adminID, uid) {
		go h.logAdminImpersonationEvent(r.Context(), adminID, adminEmail, uid, actorSource+"::billing.tokens.usage")
	}
}

func (h *Handler) computeTokenUsageSummary(ctx context.Context, uid string, startDate, endDate time.Time) (*tokenUsageSummaryResponse, error) {
	summary := &tokenUsageSummaryResponse{
		UserID:        uid,
		TotalConsumed: 0,
		TotalTopUp:    0,
		ByService:     make(map[string]int),
		StartDate:     startDate.Format(time.RFC3339),
		EndDate:       endDate.Format(time.RFC3339),
	}

	if err := h.DB.QueryRow(ctx, `
        SELECT COALESCE(SUM(ABS(amount)), 0)
        FROM "token_transactions"
        WHERE "user_id" = $1
          AND type IN ('consume', 'reserve')
          AND "created_at" BETWEEN $2 AND $3
    `, uid, startDate, endDate).Scan(&summary.TotalConsumed); err != nil {
		return nil, err
	}

	if err := h.DB.QueryRow(ctx, `
        SELECT COALESCE(SUM(amount), 0)
        FROM "token_transactions"
        WHERE "user_id" = $1
          AND type = 'topup'
          AND "created_at" BETWEEN $2 AND $3
    `, uid, startDate, endDate).Scan(&summary.TotalTopUp); err != nil {
		return nil, err
	}

	rows, err := h.DB.Query(ctx, `
        SELECT
            COALESCE(service, 'unknown') as service,
            SUM(ABS(amount)) as total_consumed
        FROM "token_transactions"
        WHERE "user_id" = $1
          AND type IN ('consume', 'reserve')
          AND "created_at" BETWEEN $2 AND $3
        GROUP BY service
        ORDER BY total_consumed DESC
    `, uid, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var service string
		var amount int
		if err := rows.Scan(&service, &amount); err != nil {
			log.Printf("Failed to scan token usage row: %v", err)
			continue
		}
		summary.ByService[service] = amount
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return summary, nil
}

func (h *Handler) logAdminImpersonationEvent(ctx context.Context, adminID, adminEmail, targetUserID, source string) {
	if h.DB == nil {
		return
	}
	adminID = strings.TrimSpace(adminID)
	targetUserID = strings.TrimSpace(targetUserID)
	if adminID == "" || targetUserID == "" {
		return
	}

	cctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var targetEmail sql.NullString
	if err := h.DB.QueryRow(cctx, `SELECT email FROM user_profiles WHERE user_id = $1 LIMIT 1`, targetUserID).Scan(&targetEmail); err != nil {
		targetEmail.Valid = false
	}

	_, err := h.DB.Exec(cctx, `
        INSERT INTO admin_impersonation_events (admin_id, admin_email, target_user_id, target_email, redirect_to, issued_at)
        VALUES ($1, NULLIF($2, ''), $3, NULLIF($4, ''), $5, NOW())
    `, adminID, strings.TrimSpace(adminEmail), targetUserID, targetEmail.String, source)
	if err != nil {
		log.Printf("billing: failed to log admin impersonation event: %v", err)
	}
}

func actorFromRequest(r *http.Request) (id, email, source string) {
	if uid, ok := r.Context().Value(middleware.UserIDKey).(string); ok {
		uid = strings.TrimSpace(uid)
		if uid != "" {
			email, _ := middleware.GetUserEmailFromContext(r.Context())
			return uid, strings.TrimSpace(email), "supabase-admin"
		}
	}

	serviceToken := strings.TrimSpace(r.Header.Get("X-Service-Token"))
	if serviceToken != "" {
		expected := strings.TrimSpace(os.Getenv("INTERNAL_SERVICE_TOKEN"))
		if expected != "" && subtle.ConstantTimeCompare([]byte(serviceToken), []byte(expected)) == 1 {
			return "service-token", "", "service-token"
		}
	}

	return "", "", ""
}

func (h *Handler) getsubscriptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}
	var sub subscriptions
	err := h.DB.QueryRow(r.Context(), `SELECT id, "plan_name", status, "currentPeriodEnd" FROM "subscriptions" WHERE "user_id" = $1`, userID).Scan(&sub.ID, &sub.PlanName, &sub.Status, &sub.CurrentPeriodEnd)
	if err != nil {
		errors.Write(w, r, http.StatusNotFound, "NOT_FOUND", "Not found", nil)
		return
	}
	respondWithJSON(w, http.StatusOK, sub)
	_ = writeBillingUI(r.Context(), userID, map[string]any{"subscription": sub})
}
func (h *Handler) getTokenBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}

	// Try cache first (1-minute TTL)
	cacheKey := fmt.Sprintf("billing:balance:%s", userID)
	if h.Cache != nil && h.Cache.Ready() {
		if cached, ok := h.Cache.Get(r.Context(), cacheKey); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			w.Write([]byte(cached))
			return
		}
	}

	balance, err := h.queryTokenBalance(r.Context(), userID)
	if err != nil {
		if stdErrors.Is(err, pgx.ErrNoRows) {
			errors.Write(w, r, http.StatusNotFound, "NOT_FOUND", "Not found", nil)
		} else {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Query failed", map[string]string{"error": err.Error()})
		}
		return
	}

	// Write to cache (1 minute TTL)
	if h.Cache != nil && h.Cache.Ready() {
		if data, err := json.Marshal(balance); err == nil {
			h.Cache.Set(r.Context(), cacheKey, string(data), 1*time.Minute)
		}
	}

	w.Header().Set("X-Cache", "MISS")
	respondWithJSON(w, http.StatusOK, balance)
	_ = writeBillingUI(r.Context(), userID, map[string]any{"tokens": balance})
}

func (h *Handler) getTokenBalanceAdmin(w http.ResponseWriter, r *http.Request) {
	targetUserID := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if targetUserID == "" {
		errors.Write(w, r, http.StatusBadRequest, "MISSING_USER_ID", "user_id is required", nil)
		return
	}

	balance, err := h.queryTokenBalance(r.Context(), targetUserID)
	if err != nil {
		if stdErrors.Is(err, pgx.ErrNoRows) {
			errors.Write(w, r, http.StatusNotFound, "NOT_FOUND", "User token balance not found", nil)
		} else {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "Query failed", map[string]string{"error": err.Error()})
		}
		return
	}

	resp := adminTokenBalanceResponse{
		UserID:    targetUserID,
		Available: balance.Balance,
		Reserved:  0,
		Total:     balance.Balance,
		UpdatedAt: balance.UpdatedAt,
	}

	respondWithJSON(w, http.StatusOK, resp)

	adminID, adminEmail, actorSource := actorFromRequest(r)
	if adminID != "" && !strings.EqualFold(adminID, targetUserID) {
		go h.logAdminImpersonationEvent(r.Context(), adminID, adminEmail, targetUserID, actorSource+"::billing.tokens.balance")
	}
}

func (h *Handler) queryTokenBalance(ctx context.Context, userID string) (TokenBalance, error) {
	var balance TokenBalance
	err := h.DB.QueryRow(ctx, `SELECT balance, "updated_at" FROM "user_tokens" WHERE "user_id" = $1`, userID).Scan(&balance.Balance, &balance.UpdatedAt)
	if err != nil {
		return TokenBalance{}, err
	}
	return balance, nil
}

func parsetoken_transactionsQueryParams(r *http.Request) (tokenTransactionQueryParams, error) {
	params := tokenTransactionQueryParams{
		Limit:  50,
		Offset: 0,
	}

	q := r.URL.Query()

	if v := strings.TrimSpace(q.Get("type")); v != "" {
		params.Type = v
	}

	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return params, fmt.Errorf("invalid limit")
		}
		if n > 200 {
			n = 200
		}
		params.Limit = n
	}

	if v := strings.TrimSpace(q.Get("offset")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return params, fmt.Errorf("invalid offset")
		}
		params.Offset = n
	}

	if v := strings.TrimSpace(q.Get("startDate")); v != "" {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return params, fmt.Errorf("invalid startDate format (expected RFC3339)")
		}
		params.StartDate = &ts
	}

	if v := strings.TrimSpace(q.Get("endDate")); v != "" {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return params, fmt.Errorf("invalid endDate format (expected RFC3339)")
		}
		params.EndDate = &ts
	}

	if params.StartDate != nil && params.EndDate != nil && params.StartDate.After(*params.EndDate) {
		return params, fmt.Errorf("startDate must be before endDate")
	}

	return params, nil
}

func (h *Handler) querytoken_transactionss(ctx context.Context, userID string, params tokenTransactionQueryParams) ([]token_transactions, int, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}

	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	whereParts := []string{`"user_id" = $1`}
	args := []any{userID}
	idx := 2

	if params.Type != "" {
		whereParts = append(whereParts, fmt.Sprintf("type = $%d", idx))
		args = append(args, params.Type)
		idx++
	}
	if params.StartDate != nil {
		whereParts = append(whereParts, fmt.Sprintf(`"created_at" >= $%d`, idx))
		args = append(args, *params.StartDate)
		idx++
	}
	if params.EndDate != nil {
		whereParts = append(whereParts, fmt.Sprintf(`"created_at" <= $%d`, idx))
		args = append(args, *params.EndDate)
		idx++
	}

	whereClause := strings.Join(whereParts, " AND ")

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM "token_transactions" WHERE %s`, whereClause)
	countArgs := append([]any(nil), args...)
	var total int
	if err := h.DB.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectArgs := append([]any(nil), args...)
	selectArgs = append(selectArgs, limit, offset)
	selectQuery := fmt.Sprintf(`
        SELECT id, type, amount, description, "created_at"
        FROM "token_transactions"
        WHERE %s
        ORDER BY "created_at" DESC
        LIMIT $%d OFFSET $%d
    `, whereClause, idx, idx+1)

	rows, err := h.DB.Query(ctx, selectQuery, selectArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]token_transactions, 0, limit)
	for rows.Next() {
		var t token_transactions
		if err := rows.Scan(&t.ID, &t.Type, &t.Amount, &t.Description, &t.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, t)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}
func (h *Handler) gettoken_transactionss(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}
	items, _, err := h.querytoken_transactionss(r.Context(), uid, tokenTransactionQueryParams{Limit: 50})
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "query failed", map[string]string{"error": err.Error()})
		return
	}
	respondWithJSON(w, http.StatusOK, items)
}

func (h *Handler) gettoken_transactionssAdmin(w http.ResponseWriter, r *http.Request) {
	targetUserID := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if targetUserID == "" {
		errors.Write(w, r, http.StatusBadRequest, "MISSING_USER_ID", "user_id is required", nil)
		return
	}

	params, err := parsetoken_transactionsQueryParams(r)
	if err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}

	items, total, err := h.querytoken_transactionss(r.Context(), targetUserID, params)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "query failed", map[string]string{"error": err.Error()})
		return
	}

	resp := tokenTransactionsAdminResponse{
		Items:      items,
		TotalCount: total,
		Limit:      params.Limit,
		Offset:     params.Offset,
	}

	respondWithJSON(w, http.StatusOK, resp)

	adminID, adminEmail, actorSource := actorFromRequest(r)
	if adminID != "" && !strings.EqualFold(adminID, targetUserID) {
		go h.logAdminImpersonationEvent(r.Context(), adminID, adminEmail, targetUserID, actorSource+"::billing.tokens.transactions")
	}
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

func writeBillingUI(ctx context.Context, userID string, patch map[string]any) error {
	if strings.TrimSpace(os.Getenv("FIRESTORE_ENABLED")) != "1" {
		return nil
	}
	pid := strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	if pid == "" {
		pid = strings.TrimSpace(os.Getenv("PROJECT_ID"))
	}
	if pid == "" || userID == "" {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	cli, err := firestore.NewClient(cctx, pid)
	if err != nil {
		return err
	}
	defer cli.Close()
	patch["updated_at"] = time.Now().UTC()
	_, err = cli.Collection("users/"+userID+"/billing").Doc("summary").Set(cctx, patch, firestore.MergeAll)
	return err
}

// --- Atomic Billing minimal endpoints ---
// reserveTokens publishes TokenReserved and writes a token_transactions row (type=reserved). No balance mutation here.
func (h *Handler) reserveTokens(pub *ev.Publisher) func(http.ResponseWriter, *http.Request) {
	type reqT struct {
		Amount int    `json:"amount"`
		TaskID string `json:"taskId"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			return
		}
		uid, _ := r.Context().Value(middleware.UserIDKey).(string)
		var req reqT
		_ = json.NewDecoder(r.Body).Decode(&req)
		if uid == "" || req.Amount <= 0 {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid", nil)
			return
		}
		// Idempotency
		idem := strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
		id := ""
		if idem != "" {
			if ex, ok := h.lookupIdem(r.Context(), idem, uid, "billing.reserve"); ok {
				id = ex
			}
		}
		if id == "" {
			id = newID()
		}
		// snapshot current balance for audit fields (no mutation)
		var before int64
		_ = h.DB.QueryRow(r.Context(), `SELECT balance FROM "user_tokens" WHERE "user_id"=$1`, uid).Scan(&before)
		meta := map[string]any{"taskId": req.TaskID, "action": "reserve"}
		_, _ = h.DB.Exec(r.Context(), `INSERT INTO "token_transactions"(id, "user_id", type, amount, "balanceBefore", "balanceAfter", source, description, metadata) VALUES ($1,$2,'reserved',$3,$4,$4,'billing','reserve',to_jsonb($5::json))`, id, uid, req.Amount, before, mustJSON(meta))
		if idem != "" {
			_ = h.upsertIdem(r.Context(), idem, uid, "billing.reserve", id, 24*time.Hour)
		}
		if pub != nil {
			_ = pub.Publish(r.Context(), ev.EventTokenReserved, map[string]any{"txId": id, "user_id": uid, "amount": req.Amount, "taskId": req.TaskID, "time": time.Now().UTC().Format(time.RFC3339)}, ev.WithSource("billing"), ev.WithSubject(id))
		}
		respondWithJSON(w, http.StatusAccepted, map[string]any{"txId": id, "status": "reserved"})
	}
}

func (h *Handler) commitTokens(pub *ev.Publisher) func(http.ResponseWriter, *http.Request) {
	type reqT struct {
		TxID   string `json:"txId"`
		Amount int    `json:"amount"`
		TaskID string `json:"taskId"`
		Source string `json:"source,omitempty"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			return
		}
		uid, _ := r.Context().Value(middleware.UserIDKey).(string)
		var req reqT
		_ = json.NewDecoder(r.Body).Decode(&req)
		if uid == "" || req.Amount <= 0 {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid", nil)
			return
		}
		// Idempotency
		idem := strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
		if idem != "" {
			if ex, ok := h.lookupIdem(r.Context(), idem, uid, "billing.commit"); ok {
				respondWithJSON(w, http.StatusOK, map[string]any{"txId": ex, "status": "committed"})
				return
			}
		}
		id := req.TxID
		if id == "" {
			id = newID()
		}
		// atomic debit
		tx, err := h.DB.Begin(r.Context())
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "begin tx failed", map[string]string{"error": err.Error()})
			return
		}
		defer tx.Rollback(r.Context())
		var before int64
		// lock row if exists
		_ = tx.QueryRow(r.Context(), `SELECT balance FROM "user_tokens" WHERE "user_id"=$1 FOR UPDATE`, uid).Scan(&before)
		if before < int64(req.Amount) {
			errors.Write(w, r, http.StatusConflict, "INSUFFICIENT_TOKENS", "insufficient token balance", map[string]any{"balance": before, "attempt": req.Amount})
			return
		}
		after := before - int64(req.Amount)
		// upsert balance
		if before == 0 {
			// ensure row exists to allow update
			_, _ = tx.Exec(r.Context(), `INSERT INTO "user_tokens"("user_id", balance, "updated_at") VALUES ($1, $2, NOW()) ON CONFLICT ("user_id") DO NOTHING`, uid, before)
		}
		if _, err := tx.Exec(r.Context(), `UPDATE "user_tokens" SET balance=$1, "updated_at"=NOW() WHERE "user_id"=$2`, after, uid); err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "update balance failed", map[string]string{"error": err.Error()})
			return
		}
		// Pools deduction (best-effort, within same tx)
		var poolSub, poolAct, poolPur int64
		// lock row if exists
		if err := tx.QueryRow(r.Context(), `SELECT subscription, activity, purchased FROM "user_tokensPool" WHERE "user_id"=$1 FOR UPDATE`, uid).Scan(&poolSub, &poolAct, &poolPur); err != nil {
			// create row on demand
			_, _ = tx.Exec(r.Context(), `INSERT INTO "user_tokensPool"("user_id", subscription, activity, purchased, "updated_at") VALUES ($1,0,0,0,NOW()) ON CONFLICT ("user_id") DO NOTHING`, uid)
			_ = tx.QueryRow(r.Context(), `SELECT subscription, activity, purchased FROM "user_tokensPool" WHERE "user_id"=$1 FOR UPDATE`, uid).Scan(&poolSub, &poolAct, &poolPur)
		}
		// Snapshot original pools for usage calculation
		origSub, origAct, origPur := poolSub, poolAct, poolPur
		// Global priority: subscription -> activity -> purchased
		remaining := int64(req.Amount)
		deduct := func(avail *int64) {
			if remaining <= 0 {
				return
			}
			if *avail <= 0 {
				return
			}
			if *avail >= remaining {
				*avail -= remaining
				remaining = 0
				return
			}
			remaining -= *avail
			*avail = 0
		}
		deduct(&poolSub)
		deduct(&poolAct)
		deduct(&poolPur)
		// Update pools (do not allow negatives)
		if _, err := tx.Exec(r.Context(), `UPDATE "user_tokensPool" SET subscription=$1, activity=$2, purchased=$3, "updated_at"=NOW() WHERE "user_id"=$4`, poolSub, poolAct, poolPur, uid); err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "update pools failed", map[string]string{"error": err.Error()})
			return
		}
		// Record debits per pool to reflect actual breakdown
		usedSub := origSub - poolSub
		usedAct := origAct - poolAct
		usedPur := origPur - poolPur
		metaBase := map[string]any{"taskId": req.TaskID, "action": "commit"}
		if strings.TrimSpace(req.Source) != "" {
			metaBase["module"] = strings.TrimSpace(req.Source)
		}
		prev := before
		insertTx := func(amount int64, pool string, txid string) error {
			if amount <= 0 {
				return nil
			}
			m := make(map[string]any, len(metaBase)+1)
			for k, v := range metaBase {
				m[k] = v
			}
			m["pool"] = pool
			if _, err := tx.Exec(r.Context(), `INSERT INTO "token_transactions"(id, "user_id", type, amount, "balanceBefore", "balanceAfter", source, description, metadata) VALUES ($1,$2,'debited',$3,$4,$5,$6,'commit',to_jsonb($7::json))`, txid, uid, amount, prev, prev-amount, pool, mustJSON(m)); err != nil {
				return err
			}
			prev = prev - amount
			return nil
		}
		// Use provided id for the first non-zero part to keep response stable
		ids := []struct {
			amt  int64
			pool string
		}{{usedSub, "subscription"}, {usedAct, "activity"}, {usedPur, "purchased"}}
		firstUsed := true
		for _, it := range ids {
			if it.amt <= 0 {
				continue
			}
			thisID := id
			if !firstUsed {
				thisID = newID()
			}
			if err := insertTx(it.amt, it.pool, thisID); err != nil {
				errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "insert tx failed", map[string]string{"error": err.Error()})
				return
			}
			firstUsed = false
		}

		// Record debit + allocations across credit lots (Lot + Allocation model)
		// Create one TokenDebit representing this commit
		meta := map[string]any{"taskId": req.TaskID, "action": "commit"}
		if strings.TrimSpace(req.Source) != "" {
			meta["module"] = strings.TrimSpace(req.Source)
		}
		bmeta, _ := json.Marshal(meta)
		if _, err := tx.Exec(r.Context(), `INSERT INTO "TokenDebit"(id, "user_id", amount, meta) VALUES ($1,$2,$3,$4)`, id, uid, req.Amount, string(bmeta)); err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "insert debit failed", map[string]string{"error": err.Error()})
			return
		}
		// Allocate from credit lots by global priority (subscription -> activity -> purchased), FIFO by expiresAt asc, created_at asc
		remainingAlloc := int64(req.Amount)
		// Lock eligible lots for update to avoid race
		rows, err := tx.Query(r.Context(), `
            SELECT id, source, remaining
            FROM "TokenCreditLot"
            WHERE "user_id"=$1 AND remaining>0
            ORDER BY CASE source WHEN 'subscription' THEN 1 WHEN 'activity' THEN 2 WHEN 'purchased' THEN 3 ELSE 4 END,
                     COALESCE("expiresAt", TIMESTAMPTZ '9999-12-31') ASC,
                     "created_at" ASC
            FOR UPDATE
        `, uid)
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "query lots failed", map[string]string{"error": err.Error()})
			return
		}
		type lotRow struct {
			id        int64
			source    string
			remaining int64
		}
		lots := make([]lotRow, 0)
		for rows.Next() {
			var lr lotRow
			if err := rows.Scan(&lr.id, &lr.source, &lr.remaining); err != nil {
				rows.Close()
				errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "scan lots failed", map[string]string{"error": err.Error()})
				return
			}
			lots = append(lots, lr)
		}
		rows.Close()
		for i := range lots {
			if remainingAlloc <= 0 {
				break
			}
			if lots[i].remaining <= 0 {
				continue
			}
			use := lots[i].remaining
			if use > remainingAlloc {
				use = remainingAlloc
			}
			// Update lot remaining
			if _, err := tx.Exec(r.Context(), `UPDATE "TokenCreditLot" SET remaining=remaining-$1 WHERE id=$2`, use, lots[i].id); err != nil {
				errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "update lot failed", map[string]string{"error": err.Error()})
				return
			}
			// Insert allocation
			if _, err := tx.Exec(r.Context(), `INSERT INTO "TokenDebitAllocation"(debit_id, credit_lot_id, amount) VALUES ($1,$2,$3)`, id, lots[i].id, use); err != nil {
				errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "insert allocation failed", map[string]string{"error": err.Error()})
				return
			}
			remainingAlloc -= use
		}
		if remainingAlloc > 0 {
			// This should not happen if balance check passed; log and continue
			log.Printf("WARN: debit allocation remaining > 0 (uid=%s, remaining=%d)", uid, remainingAlloc)
		}
		if err := tx.Commit(r.Context()); err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "commit failed", map[string]string{"error": err.Error()})
			return
		}
		if idem != "" {
			_ = h.upsertIdem(r.Context(), idem, uid, "billing.commit", id, 24*time.Hour)
		}
		if pub != nil {
			_ = pub.Publish(r.Context(), ev.EventTokenDebited, map[string]any{"txId": id, "user_id": uid, "amount": req.Amount, "taskId": req.TaskID, "time": time.Now().UTC().Format(time.RFC3339)}, ev.WithSource("billing"), ev.WithSubject(id))
		}
		// Invalidate cache after successful balance change
		if h.Cache != nil && h.Cache.Ready() {
			cacheKey := fmt.Sprintf("billing:balance:%s", uid)
			h.Cache.Del(r.Context(), cacheKey)
		}
		respondWithJSON(w, http.StatusOK, map[string]any{"txId": id, "debitId": id, "status": "committed", "balance": after})
	}
}

func (h *Handler) releaseTokens(pub *ev.Publisher) func(http.ResponseWriter, *http.Request) {
	type reqT struct {
		TxID   string `json:"txId"`
		Amount int    `json:"amount"`
		TaskID string `json:"taskId"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			return
		}
		uid, _ := r.Context().Value(middleware.UserIDKey).(string)
		var req reqT
		_ = json.NewDecoder(r.Body).Decode(&req)
		if uid == "" || req.Amount <= 0 {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid", nil)
			return
		}
		// Idempotency
		idem := strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
		if idem != "" {
			if ex, ok := h.lookupIdem(r.Context(), idem, uid, "billing.release"); ok {
				respondWithJSON(w, http.StatusOK, map[string]any{"txId": ex, "status": "released"})
				return
			}
		}
		id := req.TxID
		if id == "" {
			id = newID()
		}
		// record reversal (no balance mutation since reserve didn't deduct)
		var before int64
		_ = h.DB.QueryRow(r.Context(), `SELECT balance FROM "user_tokens" WHERE "user_id"=$1`, uid).Scan(&before)
		meta := map[string]any{"taskId": req.TaskID, "action": "release"}
		_, _ = h.DB.Exec(r.Context(), `INSERT INTO "token_transactions"(id, "user_id", type, amount, "balanceBefore", "balanceAfter", source, description, metadata) VALUES ($1,$2,'reverted',$3,$4,$4,'billing','release',to_jsonb($5::json))`, id, uid, req.Amount, before, mustJSON(meta))
		if idem != "" {
			_ = h.upsertIdem(r.Context(), idem, uid, "billing.release", id, 24*time.Hour)
		}
		if pub != nil {
			_ = pub.Publish(r.Context(), ev.EventTokenReverted, map[string]any{"txId": id, "user_id": uid, "amount": req.Amount, "taskId": req.TaskID, "time": time.Now().UTC().Format(time.RFC3339)}, ev.WithSource("billing"), ev.WithSubject(id))
		}
		respondWithJSON(w, http.StatusOK, map[string]any{"txId": id, "status": "released"})
	}
}

func newID() string {
	return strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
}

// Idempotency helpers
func (h *Handler) lookupIdem(ctx context.Context, key, userID, scope string) (string, bool) {
	var id string
	err := h.DB.QueryRow(ctx, `SELECT target_id FROM idempotency_keys WHERE key=$1 AND user_id=$2 AND scope=$3 AND expires_at>NOW()`, key, userID, scope).Scan(&id)
	if err != nil {
		return "", false
	}
	return id, id != ""
}
func (h *Handler) upsertIdem(ctx context.Context, key, userID, scope, targetID string, ttl time.Duration) error {
	_, err := h.DB.Exec(ctx, `
        INSERT INTO idempotency_keys(key, user_id, scope, target_id, created_at, expires_at)
        VALUES ($1,$2,$3,$4,NOW(), NOW()+$5::interval)
        ON CONFLICT (key) DO UPDATE SET user_id=EXCLUDED.user_id, scope=EXCLUDED.scope, target_id=EXCLUDED.target_id, expires_at=EXCLUDED.expires_at
    `, key, userID, scope, targetID, fmt.Sprintf("%d hours", int(ttl.Hours())))
	return err
}

// --- OpenAPI adapter ---
type oasImpl struct {
	h     *Handler
	pub   *ev.Publisher
	trial *handlers.TrialsubscriptionsHandler
}

func (o *oasImpl) Getsubscriptions(w http.ResponseWriter, r *http.Request) { o.h.getsubscriptions(w, r) }
func (o *oasImpl) GetTokenBalance(w http.ResponseWriter, r *http.Request) { o.h.getTokenBalance(w, r) }
func (o *oasImpl) Listtoken_transactionss(w http.ResponseWriter, r *http.Request) {
	o.h.gettoken_transactionss(w, r)
}
func (o *oasImpl) ReserveTokens(w http.ResponseWriter, r *http.Request) {
	o.h.reserveTokens(o.pub)(w, r)
}
func (o *oasImpl) CommitTokens(w http.ResponseWriter, r *http.Request) { o.h.commitTokens(o.pub)(w, r) }
func (o *oasImpl) ReleaseTokens(w http.ResponseWriter, r *http.Request) {
	o.h.releaseTokens(o.pub)(w, r)
}
func (o *oasImpl) CreditPurchasedTokens(w http.ResponseWriter, r *http.Request) {
	o.h.creditPurchased(w, r)
}
func (o *oasImpl) CreditsubscriptionsTokens(w http.ResponseWriter, r *http.Request) {
	o.h.creditsubscriptions(w, r)
}
func (o *oasImpl) GetTokenConsistency(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement token consistency check
	respondWithJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "GetTokenConsistency not implemented yet",
	})
}
func (o *oasImpl) GetTokenConsistencyPlan(w http.ResponseWriter, r *http.Request, params api.GetTokenConsistencyPlanParams) {
	// TODO: Implement consistency repair plan computation
	respondWithJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "GetTokenConsistencyPlan not implemented yet",
	})
}
func (o *oasImpl) PostTokenConsistencyRepair(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement consistency repair
	respondWithJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "PostTokenConsistencyRepair not implemented yet",
	})
}

// subscriptions endpoints
func (o *oasImpl) GetsubscriptionsMe(w http.ResponseWriter, r *http.Request) {
	o.h.getsubscriptions(w, r)
}

// Token usage endpoint
func (o *oasImpl) GetTokenUsage(w http.ResponseWriter, r *http.Request, params api.GetTokenUsageParams) {
	o.h.getTokenUsageSummaryCurrentUser(w, r)
}

// Trial subscription endpoints
func (o *oasImpl) StartTrialsubscriptions(w http.ResponseWriter, r *http.Request) {
	o.trial.CreateTrial(w, r)
}

func (o *oasImpl) GetTrialStatus(w http.ResponseWriter, r *http.Request) {
	// Get current user's trial status
	o.trial.GetTrialHistory(w, r)
}

func (o *oasImpl) ConvertTrialTosubscriptions(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement trial to paid subscription conversion
	respondWithJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "ConvertTrialTosubscriptions not implemented yet",
	})
}

// Admin and config endpoints
func (o *oasImpl) GetAllPermissions(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement admin permissions endpoint
	respondWithJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "GetAllPermissions not implemented yet",
	})
}

func (o *oasImpl) GetAllTokenCosts(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement admin token costs endpoint
	respondWithJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "GetAllTokenCosts not implemented yet",
	})
}

func (o *oasImpl) GetAllsubscriptionsConfig(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement admin subscription config endpoint
	respondWithJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "GetAllsubscriptionsConfig not implemented yet",
	})
}

func (o *oasImpl) GetConfigHistory(w http.ResponseWriter, r *http.Request, params api.GetConfigHistoryParams) {
	// TODO: Implement config history endpoint
	respondWithJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "GetConfigHistory not implemented yet",
	})
}

func (o *oasImpl) GetPricingConfig(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement pricing config endpoint
	respondWithJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "GetPricingConfig not implemented yet",
	})
}

func (o *oasImpl) UpdatePricingConfig(w http.ResponseWriter, r *http.Request, plan string) {
	// TODO: Implement pricing config update endpoint
	respondWithJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "UpdatePricingConfig not implemented yet",
	})
}

// --- Non-OAS handlers ---
// getBillingConfig returns central pricing/limits config. Source: Secret Manager (projects/<id>/secrets/billing-pricing),
// or env BILLING_PRICING_JSON as fallback. Response is JSON passthrough with sensible defaults.
func (h *Handler) getBillingConfig(w http.ResponseWriter, r *http.Request) {
	type cfgOut struct {
		Pricing   map[string]int `json:"pricing"`
		Limits    map[string]int `json:"limits,omitempty"`
		UpdatedAt string         `json:"updated_at"`
		Source    string         `json:"source"`
	}
	// Default pricing
	out := cfgOut{
		Pricing:   map[string]int{"siterank.analyze": 1, "batchopen.task": 1, "adscenter.preflight": 1},
		Limits:    map[string]int{"daily.maxTasks": 1000},
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Source:    "default",
	}
	// Try Secret Manager JSON (cache TTL 5m)
	// Priority 1: explicit env mapping BILLING_PRICING_SECRET (full resource or shorthand)
	if js := strings.TrimSpace(os.Getenv("BILLING_PRICING_SECRET")); js != "" {
		if val, err := cfgpkg.SecretCached(r.Context(), js, 5*time.Minute); err == nil && strings.TrimSpace(val) != "" {
			if m := parsePricingJSON(val); m != nil {
				out.Pricing = m
				out.Source = "secret"
			}
		}
	}
	// Priority 2: secret inferred by STACK, e.g., billing-pricing-preview → fallback billing-pricing
	if out.Source == "default" {
		if val, err := cfgpkg.SecretForStack(r.Context(), "billing-pricing"); err == nil && strings.TrimSpace(val) != "" {
			if m := parsePricingJSON(val); m != nil {
				out.Pricing = m
				out.Source = "secret(stack)"
			}
		}
	}
	// Fallback to env JSON
	if out.Source == "default" {
		if val := strings.TrimSpace(os.Getenv("BILLING_PRICING_JSON")); val != "" {
			if m := parsePricingJSON(val); m != nil {
				out.Pricing = m
				out.Source = "env"
			}
		}
	}
	respondWithJSON(w, http.StatusOK, out)
}

// gettoken_transactionsByID returns a transaction that belongs to the current user.
func (h *Handler) gettoken_transactionsByID(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "id required", nil)
		return
	}
	var (
		tType         string
		amount        int
		before, after int64
		source, desc  string
		created       time.Time
		metadataJSON  *string
	)
	err := h.DB.QueryRow(r.Context(), `SELECT type, amount, "balanceBefore", "balanceAfter", source, description, "created_at", metadata::text FROM "token_transactions" WHERE id=$1 AND "user_id"=$2`, id, uid).
		Scan(&tType, &amount, &before, &after, &source, &desc, &created, &metadataJSON)
	if err != nil {
		if err == pgx.ErrNoRows {
			errors.Write(w, r, http.StatusNotFound, "NOT_FOUND", "transaction not found", nil)
			return
		}
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "query failed", map[string]string{"error": err.Error()})
		return
	}
	var metadata map[string]any
	if metadataJSON != nil && *metadataJSON != "" {
		_ = json.Unmarshal([]byte(*metadataJSON), &metadata)
	}
	respondWithJSON(w, http.StatusOK, map[string]any{
		"id": id, "type": tType, "amount": amount, "balanceBefore": before, "balanceAfter": after, "source": source, "description": desc, "created_at": created, "metadata": metadata,
	})
}

// helpers
func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// removed legacy getSecret helper; using pkg/config.SecretCached instead
func parsePricingJSON(val string) map[string]int {
	var m map[string]int
	if err := json.Unmarshal([]byte(val), &m); err != nil {
		return nil
	}
	return m
}

// --- Credit endpoints ---
// POST /api/v1/billing/tokens/credit/purchased { amount:int, description?:string, metadata?:object }
func (h *Handler) creditPurchased(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}
	var body struct {
		Amount      int            `json:"amount"`
		Description string         `json:"description"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid body", nil)
		return
	}
	if body.Amount <= 0 {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "amount must be > 0", nil)
		return
	}
	// Idempotency
	idem := strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
	if idem != "" {
		if ex, ok := h.lookupIdem(r.Context(), idem, uid, "billing.credit.purchased"); ok {
			respondWithJSON(w, http.StatusOK, map[string]any{"txId": ex, "status": "credited"})
			return
		}
	}
	if h.SQLDB == nil {
		errors.Write(w, r, http.StatusServiceUnavailable, "NOT_CONFIGURED", "credit backend not configured", nil)
		return
	}
	id := newID()
	if err := bevents.CreditPurchasedTokens(r.Context(), h.SQLDB, uid, body.Amount, coalesce(body.Description, "Token purchase"), body.Metadata); err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "credit failed", map[string]string{"error": err.Error()})
		return
	}
	if idem != "" {
		_ = h.upsertIdem(r.Context(), idem, uid, "billing.credit.purchased", id, 24*time.Hour)
	}
	respondWithJSON(w, http.StatusOK, map[string]any{"txId": id, "status": "credited", "amount": body.Amount})
}

// POST /api/v1/billing/tokens/credit/subscription { amount:int, description?:string, metadata?:object }
func (h *Handler) creditsubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}
	var body struct {
		Amount      int            `json:"amount"`
		Description string         `json:"description"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid body", nil)
		return
	}
	if body.Amount <= 0 {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "amount must be > 0", nil)
		return
	}
	idem := strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
	if idem != "" {
		if ex, ok := h.lookupIdem(r.Context(), idem, uid, "billing.credit.subscription"); ok {
			respondWithJSON(w, http.StatusOK, map[string]any{"txId": ex, "status": "credited"})
			return
		}
	}
	if h.SQLDB == nil {
		errors.Write(w, r, http.StatusServiceUnavailable, "NOT_CONFIGURED", "credit backend not configured", nil)
		return
	}
	id := newID()
	if err := bevents.CreditsubscriptionsTokens(r.Context(), h.SQLDB, uid, body.Amount, coalesce(body.Description, "subscriptions credit"), body.Metadata); err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "credit failed", map[string]string{"error": err.Error()})
		return
	}
	if idem != "" {
		_ = h.upsertIdem(r.Context(), idem, uid, "billing.credit.subscription", id, 24*time.Hour)
	}
	respondWithJSON(w, http.StatusOK, map[string]any{"txId": id, "status": "credited", "amount": body.Amount})
}

// POST /api/v1/billing/tokens/credit/checkin { amount:int, description?:string, metadata?:object }
// Internal endpoint called by useractivity service to credit checkin rewards
func (h *Handler) creditCheckin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}
	var body struct {
		Amount      int            `json:"amount"`
		Description string         `json:"description"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid body", nil)
		return
	}
	if body.Amount <= 0 {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "amount must be > 0", nil)
		return
	}
	// Idempotency using date-based key
	checkinDate := time.Now().Format("2006-01-02")
	idemKey := fmt.Sprintf("checkin:%s:%s", uid, checkinDate)
	if ex, ok := h.lookupIdem(r.Context(), idemKey, uid, "billing.credit.checkin"); ok {
		respondWithJSON(w, http.StatusOK, map[string]any{"txId": ex, "status": "credited"})
		return
	}
	if h.SQLDB == nil {
		errors.Write(w, r, http.StatusServiceUnavailable, "NOT_CONFIGURED", "credit backend not configured", nil)
		return
	}
	id := newID()
	if err := bevents.CreditCheckinTokens(r.Context(), h.SQLDB, uid, body.Amount, coalesce(body.Description, "Daily check-in reward"), body.Metadata); err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "credit failed", map[string]string{"error": err.Error()})
		return
	}
	_ = h.upsertIdem(r.Context(), idemKey, uid, "billing.credit.checkin", id, 24*time.Hour)
	respondWithJSON(w, http.StatusOK, map[string]any{"txId": id, "status": "credited", "amount": body.Amount})
}

func coalesce[T comparable](v T, def T) T {
	var z T
	if v == z {
		return def
	}
	return v
}

// GET /api/v1/billing/tokens/consistency
// Returns computed balances across user_tokens, user_tokensPool, and TokenCreditLot remaining breakdown for the current user
func (h *Handler) getTokenConsistency(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}
	// Aggregate current balances
	var utBalance int64
	_ = h.DB.QueryRow(r.Context(), `SELECT balance FROM "user_tokens" WHERE "user_id"=$1`, uid).Scan(&utBalance)
	var poolSub, poolAct, poolPur int64
	_ = h.DB.QueryRow(r.Context(), `SELECT subscription, activity, purchased FROM "user_tokensPool" WHERE "user_id"=$1`, uid).Scan(&poolSub, &poolAct, &poolPur)
	// Remaining lots by source
	type sumRow struct {
		s string
		v int64
	}
	rows, _ := h.DB.Query(r.Context(), `SELECT source, COALESCE(SUM(remaining),0) FROM "TokenCreditLot" WHERE "user_id"=$1 GROUP BY source`, uid)
	defer func() {
		if rows != nil {
			rows.Close()
		}
	}()
	var lotSub, lotAct, lotPur int64
	for rows != nil && rows.Next() {
		var sr sumRow
		if err := rows.Scan(&sr.s, &sr.v); err == nil {
			switch strings.ToLower(sr.s) {
			case "subscription":
				lotSub = sr.v
			case "activity":
				lotAct = sr.v
			case "purchased":
				lotPur = sr.v
			}
		}
	}
	// Compose response
	resp := map[string]any{
		"userToken":           map[string]any{"balance": utBalance},
		"userTokenPool":       map[string]any{"subscription": poolSub, "activity": poolAct, "purchased": poolPur, "total": poolSub + poolAct + poolPur},
		"creditLotsRemaining": map[string]any{"subscription": lotSub, "activity": lotAct, "purchased": lotPur, "total": lotSub + lotAct + lotPur},
		"diff": map[string]any{
			"userToken_vs_pool_total": utBalance - (poolSub + poolAct + poolPur),
			"pool_vs_lot_total":       (poolSub + poolAct + poolPur) - (lotSub + lotAct + lotPur),
		},
	}
	respondWithJSON(w, http.StatusOK, resp)
}

// Admin: GET /api/v1/billing/tokens/consistency/plan?user_id=...
// Computes desired pool and user balance from TokenCreditLot remaining.
func (h *Handler) getTokenConsistencyPlan(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if target == "" {
		uid, _ := r.Context().Value(middleware.UserIDKey).(string)
		target = uid
	}
	if target == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "user_id required", nil)
		return
	}
	// current values
	var utBalance int64
	_ = h.DB.QueryRow(r.Context(), `SELECT balance FROM "user_tokens" WHERE "user_id"=$1`, target).Scan(&utBalance)
	var poolSub, poolAct, poolPur int64
	_ = h.DB.QueryRow(r.Context(), `SELECT subscription, activity, purchased FROM "user_tokensPool" WHERE "user_id"=$1`, target).Scan(&poolSub, &poolAct, &poolPur)
	// desired from lots
	var lotSub, lotAct, lotPur int64
	rows, _ := h.DB.Query(r.Context(), `SELECT source, COALESCE(SUM(remaining),0) FROM "TokenCreditLot" WHERE "user_id"=$1 GROUP BY source`, target)
	if rows != nil {
		for rows.Next() {
			var s string
			var v int64
			if err := rows.Scan(&s, &v); err == nil {
				switch strings.ToLower(s) {
				case "subscription":
					lotSub = v
				case "activity":
					lotAct = v
				case "purchased":
					lotPur = v
				}
			}
		}
		rows.Close()
	}
	desiredTotal := lotSub + lotAct + lotPur
	actions := make([]map[string]any, 0)
	if utBalance != desiredTotal {
		actions = append(actions, map[string]any{
			"type": "update_user_token",
			"from": utBalance,
			"to":   desiredTotal,
		})
	}
	if poolSub != lotSub || poolAct != lotAct || poolPur != lotPur {
		actions = append(actions, map[string]any{
			"type": "update_user_token_pool",
			"from": map[string]any{"subscription": poolSub, "activity": poolAct, "purchased": poolPur},
			"to":   map[string]any{"subscription": lotSub, "activity": lotAct, "purchased": lotPur},
		})
	}
	resp := map[string]any{
		"user_id": target,
		"current": map[string]any{
			"userToken": utBalance,
			"pool":      map[string]any{"subscription": poolSub, "activity": poolAct, "purchased": poolPur},
		},
		"desired": map[string]any{
			"userToken": desiredTotal,
			"pool":      map[string]any{"subscription": lotSub, "activity": lotAct, "purchased": lotPur},
		},
		"actions": actions,
	}
	respondWithJSON(w, http.StatusOK, resp)
}

// Admin: POST /api/v1/billing/tokens/consistency/repair { user_id, confirm?: bool, dryRun?: bool }
// Applies the plan by updating user_tokens and user_tokensPool to match TokenCreditLot remaining.
func (h *Handler) postTokenConsistencyRepair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID  string `json:"user_id"`
		Confirm bool   `json:"confirm"`
		DryRun  bool   `json:"dryRun"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid body", nil)
		return
	}
	target := strings.TrimSpace(body.UserID)
	if target == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "user_id required", nil)
		return
	}
	// Environment protection
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("BILLING_REPAIR_ENABLED")), "1") && body.Confirm && !body.DryRun {
		errors.Write(w, r, http.StatusForbidden, "FORBIDDEN", "repair disabled by env (set BILLING_REPAIR_ENABLED=1)", nil)
		return
	}
	// Operator (admin) uid
	operatorUID, _ := r.Context().Value(middleware.UserIDKey).(string)
	// compute desired from lots
	var lotSub, lotAct, lotPur int64
	rows, err := h.DB.Query(r.Context(), `SELECT source, COALESCE(SUM(remaining),0) FROM "TokenCreditLot" WHERE "user_id"=$1 GROUP BY source`, target)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "query lots failed", map[string]string{"error": err.Error()})
		return
	}
	for rows.Next() {
		var s string
		var v int64
		if err := rows.Scan(&s, &v); err == nil {
			switch strings.ToLower(s) {
			case "subscription":
				lotSub = v
			case "activity":
				lotAct = v
			case "purchased":
				lotPur = v
			}
		}
	}
	rows.Close()
	desiredTotal := lotSub + lotAct + lotPur
	if body.DryRun || !body.Confirm {
		respondWithJSON(w, http.StatusOK, map[string]any{"status": "dry_run", "user_id": target, "desired": map[string]any{"userToken": desiredTotal, "pool": map[string]any{"subscription": lotSub, "activity": lotAct, "purchased": lotPur}}})
		return
	}
	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "begin tx failed", map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback(r.Context())
	// Snapshot before
	var beforeBal int64
	var beforeSub, beforeAct, beforePur int64
	_ = tx.QueryRow(r.Context(), `SELECT balance FROM "user_tokens" WHERE "user_id"=$1 FOR UPDATE`, target).Scan(&beforeBal)
	_ = tx.QueryRow(r.Context(), `SELECT subscription, activity, purchased FROM "user_tokensPool" WHERE "user_id"=$1 FOR UPDATE`, target).Scan(&beforeSub, &beforeAct, &beforePur)
	// upsert pool
	if _, err := tx.Exec(r.Context(), `INSERT INTO "user_tokensPool"("user_id", subscription, activity, purchased, "updated_at") VALUES ($1,$2,$3,$4,NOW()) ON CONFLICT ("user_id") DO UPDATE SET subscription=$2, activity=$3, purchased=$4, "updated_at"=NOW()`, target, lotSub, lotAct, lotPur); err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "update pool failed", map[string]string{"error": err.Error()})
		return
	}
	// upsert user token balance
	if _, err := tx.Exec(r.Context(), `INSERT INTO "user_tokens"("user_id", balance, "updated_at") VALUES ($1,$2,NOW()) ON CONFLICT ("user_id") DO UPDATE SET balance=$2, "updated_at"=NOW()`, target, desiredTotal); err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "update user token failed", map[string]string{"error": err.Error()})
		return
	}
	// Audit record
	beforePool := map[string]any{"subscription": beforeSub, "activity": beforeAct, "purchased": beforePur}
	afterPool := map[string]any{"subscription": lotSub, "activity": lotAct, "purchased": lotPur}
	bpb, _ := json.Marshal(beforePool)
	apb, _ := json.Marshal(afterPool)
	meta := map[string]any{"reason": "consistency_repair"}
	metab, _ := json.Marshal(meta)
	if _, err := tx.Exec(r.Context(), `INSERT INTO "TokenRepairAudit"("user_id","operatorUserId",before_balance,after_balance,before_pool,after_pool,meta) VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb)`, target, operatorUID, beforeBal, desiredTotal, string(bpb), string(apb), string(metab)); err != nil {
		log.Printf("WARN: insert TokenRepairAudit failed: %v", err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "commit failed", map[string]string{"error": err.Error()})
		return
	}
	if h.Pub != nil {
		_ = h.Pub.Publish(r.Context(), ev.EventTokenConsistencyRepaired, map[string]any{
			"user_id":         target,
			"operatorUserId": operatorUID,
			"before":         map[string]any{"userToken": beforeBal, "pool": beforePool},
			"after":          map[string]any{"userToken": desiredTotal, "pool": afterPool},
			"userToken":      desiredTotal,
			"pool":           afterPool,
			"time":           time.Now().UTC().Format(time.RFC3339),
		}, ev.WithSource("billing"), ev.WithSubject(target))
	}
	respondWithJSON(w, http.StatusOK, map[string]any{"status": "repaired", "user_id": target, "userToken": desiredTotal, "pool": map[string]any{"subscription": lotSub, "activity": lotAct, "purchased": lotPur}})
}
