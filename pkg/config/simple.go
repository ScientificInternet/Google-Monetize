package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// ServiceType 服务类型
type ServiceType string

const (
	ServiceTypeAPI     ServiceType = "api"
	ServiceTypeWorker  ServiceType = "worker"
	ServiceTypeWeb     ServiceType = "web"
)

// SimpleConfig 简化配置结构
type SimpleConfig struct {
	// 基本信息
	ServiceName    string `env:"SERVICE_NAME"`
	Environment    string `env:"ENV"`
	Version        string `env:"SERVICE_VERSION"`

	// 服务端口
	Port           string `env:"PORT"`
	HealthPort     string `env:"HEALTH_PORT"`

	// 数据库 (支持Secret Manager)
	DatabaseURL         string `env:"DATABASE_URL"`
	DatabaseURLSecret    string `env:"DATABASE_URL_SECRET_NAME"` // Secret Manager密钥名称

	// GCP
	GCPProjectID       string `env:"GOOGLE_CLOUD_PROJECT"`
	GCPRegion          string `env:"GOOGLE_CLOUD_REGION"`
	ServiceAccountKey   string `env:"GOOGLE_APPLICATION_CREDENTIALS"` // 服务账号密钥路径

	// JWT (支持Secret Manager)
	JWTIssuer         string `env:"JWT_ISSUER"`
	JWTAudience       string `env:"JWT_AUDIENCE"`
	JWTSecret         string `env:"JWT_SECRET_NAME"` // Secret Manager密钥名称

	// 可选配置
	EnableCORS         bool `env:"ENABLE_CORS" envDefault:"true"`
	EnableMetrics      bool `env:"ENABLE_METRICS" envDefault:"true"`
	EnableTracing      bool `env:"TRACES_ENABLED" envDefault:"false"`
}

// Load 加载简化配置
func Load(ctx context.Context, serviceType ServiceType) (*SimpleConfig, error) {
	// 开发环境加载.env文件
	if os.Getenv("ENV") == "development" {
		_ = godotenv.Load()
	}

	config := &SimpleConfig{
		ServiceName:      os.Getenv("SERVICE_NAME"),
		Version:         os.Getenv("SERVICE_VERSION"),
		Environment:      getEnvironment(),
		BuildTime:        time.Now(),

		Port:            getPort(serviceType),
		HealthPort:       os.Getenv("HEALTH_PORT"),

		// 数据库配置 - 支持Secret Manager
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		DatabaseURLSecret:    os.Getenv("DATABASE_URL_SECRET_NAME"),

		// GCP配置
		GCPProjectID:       os.Getenv("GOOGLE_CLOUD_PROJECT"),
		GCPRegion:          os.Getenv("GOOGLE_CLOUD_REGION"),
		ServiceAccountKey:   os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),

		// JWT配置 - 支持Secret Manager
		JWTIssuer:         os.Getenv("JWT_ISSUER"),
		JWTAudience:       os.Getenv("JWT_AUDIENCE"),
		JWTSecret:         os.Getenv("JWT_SECRET_NAME"),

		EnableCORS:         getBoolEnv("ENABLE_CORS", true),
		EnableMetrics:      getBoolEnv("ENABLE_METRICS", true),
		EnableTracing:      getBoolEnv("TRACES_ENABLED", false),
	}

	// 验证必需配置
	if err := validate(config); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	log.Printf("✅ 配置加载成功: %s", config.ServiceName)
	return config, nil
}

// validate 验证配置
func validate(config *SimpleConfig) error {
	// 如果使用Secret Manager，则验证密钥名称而非直接值
	if config.DatabaseURL == "" && config.DatabaseURLSecret == "" {
		return fmt.Errorf("DATABASE_URL or DATABASE_URL_SECRET_NAME is required")
	}

	if config.JWTSecret == "" && config.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET or JWT_SECRET_NAME is required")
	}

	if config.GCPProjectID == "" {
		return fmt.Errorf("GOOGLE_CLOUD_PROJECT is required")
	}

	return nil
}

// 辅助函数
func getEnvironment() string {
	env := os.Getenv("ENV")
	switch env {
	case "development", "dev":
		return "development"
	case "staging", "stage":
		return "staging"
	case "production", "prod":
		return "production"
	default:
		return "production" // 默认生产环境
	}
}

func getPort(serviceType ServiceType) string {
	if port := os.Getenv("PORT"); port != "" {
		return port
	}

	// 默认端口
	switch serviceType {
	case ServiceTypeAPI:
		return "8080"
	case ServiceTypeWorker:
		return "8081"
	case ServiceTypeWeb:
		return "8082"
	default:
		return "8080"
	}
}

func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := fmt.Sscanf(value, "%t", &defaultValue); err == nil && parsed == 1 {
			return true
		}
	}
	return defaultValue
}