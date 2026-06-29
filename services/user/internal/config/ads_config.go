package config

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/oauth2/google"
	"cloud.google.com/go/secretmanager/apiv1"
)

// AdsConfig holds Google Ads OAuth configuration
type AdsConfig struct {
	OAuthClientID     string
	OAuthClientSecret string
}

// LoadAdsCreds loads Google Ads credentials from Secret Manager or environment variables
func LoadAdsCreds(ctx context.Context) (*AdsConfig, error) {
	// Try to load from Secret Manager first
	if projectID := os.Getenv("GOOGLE_CLOUD_PROJECT"); projectID != "" {
		if config, err := loadFromSecretManager(ctx, projectID); err == nil {
			log.Printf("Loaded Google Ads credentials from Secret Manager")
			return config, nil
		} else {
			log.Printf("Warning: Failed to load from Secret Manager: %v", err)
		}
	}

	// Fallback to environment variables
	log.Println("Loading Google Ads credentials from environment variables")
	return &AdsConfig{
		OAuthClientID:     getEnvOrDefault("GOOGLE_ADS_CLIENT_ID", ""),
		OAuthClientSecret: getEnvOrDefault("GOOGLE_ADS_CLIENT_SECRET", ""),
	}, nil
}

// loadFromSecretManager loads credentials from Google Cloud Secret Manager
func loadFromSecretManager(ctx context.Context, projectID string) (*AdsConfig, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	config := &AdsConfig{}

	// Load client ID
	if err := loadSecret(ctx, client, projectID, "GOOGLE_ADS_CLIENT_ID", &config.OAuthClientID); err != nil {
		return nil, err
	}

	// Load client secret
	if err := loadSecret(ctx, client, projectID, "GOOGLE_ADS_CLIENT_SECRET", &config.OAuthClientSecret); err != nil {
		return nil, err
	}

	return config, nil
}

// loadSecret loads a specific secret from Secret Manager
func loadSecret(ctx context.Context, client *secretmanager.Client, projectID, secretName string, target *string) error {
	name := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretName)

	req := &secretmanager.AccessSecretVersionRequest{
		Name: name,
	}

	result, err := client.AccessSecretVersion(ctx, req)
	if err != nil {
		return err
	}

	if result.Payload == nil || result.Payload.Data == nil {
		return fmt.Errorf("secret payload is empty")
	}

	*target = string(result.Payload.Data)
	return nil
}

// getEnvOrDefault gets environment variable or returns default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}