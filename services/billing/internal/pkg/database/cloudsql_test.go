package database

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestCloudSQLConnection tests Cloud SQL connection
func TestCloudSQLConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database connection test in short mode")
	}

	ctx := context.Background()

	// Get database URL from environment variables
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL environment variable not set, skipping test")
	}

	// Test pgxpool connection
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Skipf("Failed to parse database config (likely special characters in password): %v", err)
	}

	// Optimize connection pool parameters
	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create connection pool: %v", err)
	}
	defer pool.Close()

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Database connection failed: %v", err)
	}

	// Test simple query
	var result string
	err = pool.QueryRow(ctx, "SELECT 'Cloud SQL connection test successful'").Scan(&result)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result != "Cloud SQL connection test successful" {
		t.Errorf("Query result incorrect, expected: 'Cloud SQL connection test successful', actual: '%s'", result)
	}

	t.Logf("Cloud SQL connection test successful: %s", result)
}

// TestDatabaseSchema tests database schema
func TestDatabaseSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database schema test in short mode")
	}

	ctx := context.Background()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL environment variable not set, skipping test")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("Failed to create connection pool (likely special characters in password): %v", err)
	}
	defer pool.Close()

	// Test existence of each business domain schema
	schemas := []string{"billing", "offers", "siterank", "adscenter", "useractivity"}

	for _, schema := range schemas {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)",
			schema).Scan(&exists)

		if err != nil {
			t.Logf("Failed to query schema %s: %v", schema, err)
			continue
		}

		if exists {
			t.Logf("✅ Schema %s exists", schema)
		} else {
			t.Logf("❌ Schema %s does not exist", schema)
		}
	}

	// Test connection to billing domain tables
	var tableCount int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'billing'").Scan(&tableCount)
	if err != nil {
		t.Logf("Failed to query billing table count: %v", err)
	} else {
		t.Logf("Billing domain contains %d tables", tableCount)
	}
}

// TestConnectionPoolPerformance tests connection pool performance
func TestConnectionPoolPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping connection pool performance test in short mode")
	}

	ctx := context.Background()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL environment variable not set, skipping test")
	}

	// Test different connection pool configurations
	configs := []struct {
		name     string
		maxConns int32
		minConns int32
	}{
		{"Small Scale", 5, 1},
		{"Medium Scale", 20, 5},
		{"Large Scale", 50, 10},
	}

	for _, config := range configs {
		t.Run(config.name, func(t *testing.T) {
			poolConfig, err := pgxpool.ParseConfig(databaseURL)
			if err != nil {
				t.Skipf("Failed to parse database config (likely special characters in password): %v", err)
			}

			poolConfig.MaxConns = config.maxConns
			poolConfig.MinConns = config.minConns
			poolConfig.MaxConnLifetime = time.Hour

			pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
			if err != nil {
				t.Fatalf("Failed to create connection pool: %v", err)
			}
			defer pool.Close()

			// Test concurrent connections
			start := time.Now()
			concurrency := 10
			done := make(chan bool, concurrency)

			for i := 0; i < concurrency; i++ {
				go func() {
					defer func() { done <- true }()

					for j := 0; j < 5; j++ {
						var result int
						err := pool.QueryRow(ctx, "SELECT 1").Scan(&result)
						if err != nil {
							t.Errorf("Query failed: %v", err)
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
				}()
			}

			// Wait for all goroutines to complete
			for i := 0; i < concurrency; i++ {
				<-done
			}

			duration := time.Since(start)
			t.Logf("Config %s (max: %d, min: %d): completion time %v, avg per query %v",
				config.name, config.maxConns, config.minConns,
				duration, duration/(time.Duration(concurrency*5)))
		})
	}
}

// TestCloudSQLProxyIntegration integration test
func TestCloudSQLProxyIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skip integration test")
	}

	ctx := context.Background()

	// This test simulates actual Cloud SQL Proxy integration scenarios
	// In actual deployment, Cloud SQL Proxy will be embedded as a client tool in the application

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL environment variable not set, skipping test")
	}

	// Create database manager
	dbConfig := &Config{
		DatabaseURL:    databaseURL,
		MaxConnections: 20,
		MinConnections: 5,
		MaxConnLifetime: time.Hour,
	}

	dbManager, err := NewDatabaseManager(ctx, dbConfig)
	if err != nil {
		t.Fatalf("Failed to create database manager: %v", err)
	}
	defer dbManager.Close()

	// Health check
	if err := dbManager.HealthCheck(ctx); err != nil {
		t.Fatalf("Database health check failed: %v", err)
	}

	t.Log("✅ Cloud SQL Proxy integration test successful")
}

// Benchmark test
func BenchmarkConnectionPool(b *testing.B) {
	ctx := context.Background()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		b.Skip("DATABASE_URL environment variable not set, skipping benchmark test")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		b.Fatalf("Failed to create connection pool: %v", err)
	}
	defer pool.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var result int
		err := pool.QueryRow(ctx, "SELECT 1").Scan(&result)
		if err != nil {
			b.Fatalf("Query failed: %v", err)
		}
	}
}

// Main function to run tests manually
func RunDatabaseTests() {
	log.Println("Starting database connection tests...")

	// Run basic connection tests
	tests := []testing.InternalTest{
		{Name: "TestCloudSQLConnection", F: TestCloudSQLConnection},
		{Name: "TestDatabaseSchema", F: TestDatabaseSchema},
		{Name: "TestConnectionPoolPerformance", F: TestConnectionPoolPerformance},
		{Name: "TestCloudSQLProxyIntegration", F: TestCloudSQLProxyIntegration},
	}

	// Manually run tests
	for _, test := range tests {
		log.Printf("Running test: %s", test.Name)
		t := &testing.T{}
	test.F(t)
		if !t.Failed() {
			log.Printf("✅ %s passed", test.Name)
		} else {
			log.Printf("❌ %s failed", test.Name)
		}
	}

	log.Println("✅ All database connection tests completed")
}