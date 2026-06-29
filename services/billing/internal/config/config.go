package config

import (
	"context"
	"fmt"
	"log"
	"os"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/joho/godotenv"
	"github.com/ScientificInternet/Google-Monetize/pkg/dburl"
)

// Config holds the application configuration.
type Config struct {
	DatabaseURL          string
	Port                 string
	ProjectID            string
	PubSubTopicID        string
	PubSubsubscriptionsID string
}

// Load reads configuration from environment variables.
func Load(ctx context.Context) (*Config, error) {
	if os.Getenv("ENV") == "development" {
		_ = godotenv.Load()
	}
	env := os.Getenv("ENV")
	if env == "" {
		env = "production"
	}

	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		log.Println("WARN: GOOGLE_CLOUD_PROJECT environment variable not set.")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Prefer Secret Manager when DATABASE_URL_SECRET_NAME is provided, else fallback to env
	var databaseURL string
	if sec := os.Getenv("DATABASE_URL_SECRET_NAME"); sec != "" {
		if s, err := accessSecretVersion(ctx, sec); err == nil {
			databaseURL = s
		} else {
			log.Printf("WARN: access DATABASE_URL via Secret Manager failed: %v; falling back to env DATABASE_URL", err)
			databaseURL = os.Getenv("DATABASE_URL")
		}
	} else {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set (or DATABASE_URL_SECRET_NAME missing)")
	}

	// Apply DB_NAME override if specified (for logical database isolation)
	databaseURL = dburl.RewriteIfNeeded(databaseURL)

	pubSubTopicID := os.Getenv("PUBSUB_TOPIC_ID")
	pubSubsubscriptionsID := os.Getenv("PUBSUB_SUBSCRIPTION_ID")
	stack := os.Getenv("STACK")
	if env == "development" {
		if pubSubTopicID == "" {
			pubSubTopicID = "domain-events-dev"
		}
		if pubSubsubscriptionsID == "" {
			pubSubsubscriptionsID = "billing-sub-dev"
		}
	} else {
		if pubSubTopicID == "" && stack != "" {
			pubSubTopicID = "domain-events-" + stack
		}
		if pubSubsubscriptionsID == "" && stack != "" {
			pubSubsubscriptionsID = "billing-sub-" + stack
		}
		// In minimal deployment we skip in-process subscriber; do not hard-require Pub/Sub config.
		if pubSubTopicID == "" {
			log.Println("WARN: PUBSUB_TOPIC_ID not set; skipping in-process subscriber")
		}
		if pubSubsubscriptionsID == "" {
			log.Println("WARN: PUBSUB_SUBSCRIPTION_ID not set; skipping in-process subscriber")
		}
	}

	return &Config{
		DatabaseURL:          databaseURL,
		Port:                 port,
		ProjectID:            projectID,
		PubSubTopicID:        pubSubTopicID,
		PubSubsubscriptionsID: pubSubsubscriptionsID,
	}, nil
}

// accessSecretVersion accesses a secret version from Google Cloud Secret Manager.
func accessSecretVersion(ctx context.Context, name string) (string, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return "", err
	}
	defer client.Close()
	res, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: name})
	if err != nil {
		return "", err
	}
	return string(res.Payload.Data), nil
}
