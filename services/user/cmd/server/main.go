package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/ScientificInternet/Google-Monetize/pkg/logger"
	"github.com/ScientificInternet/Google-Monetize/pkg/telemetry"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/config"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/handlers"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/middleware"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/repositories"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/services"
)

var (
	zlog = logger.Get()
)

func main() {
	log.Println("User Service starting...")

	// Setup OpenTelemetry tracing
	shutdown := telemetry.SetupTracing("user-service")
	defer func() { _ = shutdown(context.Background()) }()

	// Load configuration
	cfg := config.Load()
	log.Printf("Configuration loaded - Environment: %s, Supabase URL: %s",
		cfg.Environment, cfg.Supabase.URL)

	// Initialize database connections using adapter
	var userRepo repositories.UserRepositoryInterface

	// Check if we should use the adapter-based repository
	useAdapter := os.Getenv("USE_DB_ADAPTER") != "false" // default to true

	if useAdapter {
		// Use FinalAdapter for standard database access
		finalAdapterRepo, err := repositories.NewFinalAdapterUserRepository()
		if err != nil {
			log.Fatalf("Failed to create final adapter user repository: %v", err)
		}
		userRepo = finalAdapterRepo
		log.Printf("Using FinalAdapter for user repository (mode: %v)", finalAdapterRepo.GetAdapterMode())
	} else {
		// Fallback to original repository
		legacyRepo, err := repositories.NewUserRepository(cfg.Database, cfg.Supabase)
		if err != nil {
			log.Fatalf("Failed to create legacy user repository: %v", err)
		}
		userRepo = legacyRepo
		log.Println("Using legacy database connection for user repository")
	}
	defer userRepo.Close()

	// Initialize Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer rdb.Close()

	// Test Redis connection
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("Warning: Failed to connect to Redis: %v", err)
	}

	// Initialize services
	userService := services.NewUserService(userRepo, rdb)
	syncService := services.NewSyncService(userRepo, nil, nil)

	// Initialize handlers
	userHandler := handlers.NewUserHandler(userService, syncService)
	oauthHandler := handlers.NewOAuthHandler(userService)

	// Setup Gin router
	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(middleware.CORS())
	router.Use(middleware.Tracing())

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"service":   "user-service",
			"timestamp": time.Now().UTC(),
		})
	})

	// API routes
	api := router.Group("/api/v1")
	{
		// User profile routes
		api.GET("/users/:userId/profile", userHandler.GetUserProfile)
		api.GET("/users/:userId", userHandler.GetUserCompleteInfo)

		// Sync routes
		api.POST("/sync/user/:userId", userHandler.SyncUser)

		// OAuth routes
		oauthGroup := api.Group("/users/auth/oauth")
		{
			oauthGroup.GET("/url", oauthHandler.HandleOAuthURL)
			oauthGroup.GET("/callback", oauthHandler.HandleOAuthCallback)
			oauthGroup.POST("/revoke", oauthHandler.HandleOAuthRevoke)
			oauthGroup.GET("/tokens", oauthHandler.HandleOAuthTokens)
		}
	}

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	go func() {
		log.Printf("Starting user service on port %s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down user service...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("User service stopped")
}
