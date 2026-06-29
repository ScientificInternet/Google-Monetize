//go:build ignore

// github.com/ScientificInternet/Google-Monetize/services/billing/internal/events/subscriber.go
package events

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/pubsub"
)

// Subscriber listens for domain events from Google Cloud Pub/Sub.
type Subscriber struct {
	client *pubsub.Client
	db     *sql.DB
}

// NewSubscriber creates a new event subscriber.
func NewSubscriber(ctx context.Context, db *sql.DB) (*Subscriber, error) {
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		return nil, fmt.Errorf("GOOGLE_CLOUD_PROJECT environment variable must be set")
	}

	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}

	return &Subscriber{
		client: client,
		db:     db,
	}, nil
}

// StartListening begins listening for events on a specific subscription.
// This function will block, so it should be run in a goroutine.
func (s *Subscriber) StartListening(ctx context.Context) {
	subscriptionID := os.Getenv("PUBSUB_SUBSCRIPTION_ID")
	if subscriptionID == "" {
		log.Fatal("PUBSUB_SUBSCRIPTION_ID environment variable must be set")
	}

	sub := s.client.subscriptions(subscriptionID)
	log.Printf("Starting to listen for events on subscription: %s", subscriptionID)

	// Receive messages in a blocking call.
	err := sub.Receive(ctx, func(cctx context.Context, msg *pubsub.Message) {
		eventType := msg.Attributes["eventType"]
		log.Printf("Received event of type: %s", eventType)

		var err error
		switch eventType {
		case "OnboardingStepCompleted":
			err = HandleOnboardingStepCompleted(cctx, s.db, msg.Data)
		// Add other event handlers here
		// case "subscriptionsStarted":
		//     err = HandlesubscriptionsStarted(cctx, s.db, msg.Data)
		default:
			log.Printf("No handler for event type: %s", eventType)
			msg.Ack() // Acknowledge messages with no handler to avoid redelivery.
			return
		}

		if err != nil {
			log.Printf("Error processing event '%s': %v", eventType, err)
			// Nack the message to signal that it should be redelivered for another attempt.
			msg.Nack()
		} else {
			// Ack the message to prevent it from being sent again.
			log.Printf("Successfully processed event '%s'", eventType)
			msg.Ack()
		}
	})

	if err != nil {
		log.Fatalf("Pub/Sub Receive error: %v. This is a fatal error, and the service will shut down.", err)
	}
}
