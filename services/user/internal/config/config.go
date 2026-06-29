package config

import (
	"os"
	"time"
)

type Config struct {
	Environment string         `json:"environment"`
	Database    DatabaseConfig `json:"database"`
	Supabase    SupabaseConfig `json:"supabase"`
	Redis       RedisConfig    `json:"redis"`
	Sync        SyncConfig     `json:"sync"`
}

type DatabaseConfig struct {
	URL             string        `json:"url"`
	MaxConnections  int           `json:"max_connections"`
	MaxIdleTime     time.Duration `json:"max_idle_time"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`
}

type SupabaseConfig struct {
	URL    string `json:"url"`
	APIKey string `json:"api_key"`
	DBURL  string `json:"db_url"`
}

type RedisConfig struct {
	Address  string        `json:"address"`
	Password string        `json:"password"`
	DB       int           `json:"db"`
	PoolSize int           `json:"pool_size"`
	Timeout  time.Duration `json:"timeout"`
}

type SyncConfig struct {
	Interval       time.Duration `json:"interval"`
	BatchSize      int           `json:"batch_size"`
	MaxRetries     int           `json:"max_retries"`
	RetryDelay     time.Duration `json:"retry_delay"`
	ConflictPolicy string        `json:"conflict_policy"` // "latest", "supabase", "gcp"
}

func Load() *Config {
	cfg := &Config{
		Environment: getEnvOrDefault("ENVIRONMENT", "development"),
		Database: DatabaseConfig{
			URL:             getEnvOrDefault("DATABASE_URL", "postgres://user:password@localhost:5432/gm"),
			MaxConnections:  25,
			MaxIdleTime:     5 * time.Minute,
			ConnMaxLifetime: 2 * time.Hour,
		},
		Supabase: SupabaseConfig{
			URL:    getEnvOrDefault("SUPABASE_URL", "https://jzzvizacfyipzdyiqfzb.supabase.co"),
			APIKey: getEnvOrDefault("SUPABASE_API_KEY", ""),
			DBURL:  getEnvOrDefault("SUPABASE_DB_URL", "postgresql://postgres:*HF#9dFnzV5DBA.@db.jzzvizacfyipzdyiqfzb.supabase.co:5432/postgres"),
		},
		Redis: RedisConfig{
			Address:  getEnvOrDefault("REDIS_ADDRESS", "localhost:6379"),
			Password: getEnvOrDefault("REDIS_PASSWORD", ""),
			DB:       0,
			PoolSize: 10,
			Timeout:  3 * time.Second,
		},
		Sync: SyncConfig{
			Interval:       30 * time.Second,
			BatchSize:      100,
			MaxRetries:     3,
			RetryDelay:     5 * time.Second,
			ConflictPolicy: "latest",
		},
	}

	return cfg
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
