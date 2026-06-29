//go:build ignore

package events

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ScientificInternet/Google-Monetize/services/identity/internal/domain"
	"log"
	"sync"
	"time"

	"cloud.google.com/go/pubsub"
)

// DomainEvent represents a generic domain event.
type DomainEvent interface {
	EventType() string
}

// Subscriber defines the function signature for an event handler.
type Subscriber func(ctx context.Context, event DomainEvent) error

// PubSubSubscriber listens for messages on a Pub/Sub subscription and dispatches them.
type PubSubSubscriber struct {
	client         *pubsub.Client
	subscription   *pubsub.subscriptions
	subscriptionID string
	eventHandlers  map[string]Subscriber
	mu             sync.RWMutex
}

// NewPubSubSubscriber creates a new PubSubSubscriber.
func NewPubSubSubscriber(ctx context.Context, projectID, topicName, subscriptionID string) (*PubSubSubscriber, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}

	sub := client.subscriptions(subscriptionID)
	exists, err := sub.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check if subscription exists: %w", err)
	}
	if !exists {
		log.Printf("subscriptions %s does not exist, creating it...", subscriptionID)
		topic := client.Topic(topicName)
		sub, err = client.Createsubscriptions(ctx, subscriptionID, pubsub.subscriptionsConfig{
			Topic:       topic,
			AckDeadline: 20 * time.Second,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create subscription %s: %w", subscriptionID, err)
		}
	}

	return &PubSubSubscriber{
		client:         client,
		subscription:   sub,
		subscriptionID: subscriptionID,
		eventHandlers:  make(map[string]Subscriber),
	}, nil
}

// On registers a handler for a specific event type.
func (s *PubSubSubscriber) On(eventType string, handler Subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventHandlers[eventType] = handler
}

// Start begins listening for messages in a background goroutine.
func (s *PubSubSubscriber) Start(ctx context.Context) {
	log.Printf("Starting Pub/Sub subscriber for %s...", s.subscriptionID)
	go func() {
		err := s.subscription.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
			eventType := msg.Attributes["eventType"]
			if eventType == "" {
				log.Printf("WARN: Received message without eventType attribute. Acking.")
				msg.Ack()
				return
			}

			s.mu.RLock()
			handler, ok := s.eventHandlers[eventType]
			s.mu.RUnlock()

			if !ok {
				log.Printf("WARN: No handler registered for eventType '%s'. Acking.", eventType)
				msg.Ack()
				return
			}

			// A real implementation would use a factory or reflection based on eventType
			// to unmarshal into the correct struct.
			var event domain.UserRegisteredEvent
			if err := json.Unmarshal(msg.Data, &event); err != nil {
				log.Printf("ERROR: Failed to unmarshal message data for eventType '%s': %v. Nacking.", eventType, err)
				msg.Nack()
				return
			}

			if err := handler(ctx, event); err != nil {
				log.Printf("ERROR: Handler for eventType '%s' failed: %v. Nacking.", eventType, err)
				msg.Nack()
				return
			}

			msg.Ack()
		})
		if err != nil {
			log.Fatalf("Pub/Sub Receive error for subscription %s: %v", s.subscriptionID, err)
		}
	}()
}

// Close cleans up the Pub/Sub client resources.
func (s *PubSubSubscriber) Close() {
	if s.client != nil {
		s.client.Close()
	}
}
