package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/ScientificInternet/Google-Monetize/pkg/eventbus"
	"github.com/ScientificInternet/Google-Monetize/pkg/logger"
	"github.com/ScientificInternet/Google-Monetize/services/batchopen/internal/storage"
)

var (
	adapter *storage.FinalAdapter
	rdb     *redis.Client
	ctx     = context.Background()
	log     = logger.Get()
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Warn().Msg("Error loading .env file")
	}

	// Initialize FinalAdapter
	adapter, err = storage.NewFinalAdapter()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create final database adapter")
	}
	defer adapter.Close()

	if err := adapter.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("Error pinging the database at startup")
	}
	log.Info().Str("mode", adapter.GetAdapterMode()).Msg("FinalAdapter initialized successfully!")

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		log.Fatal().Msg("REDIS_URL is not set")
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Error parsing Redis URL")
	}
	rdb = redis.NewClient(opt)
	_, err = rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatal().Err(err).Msg("Error pinging Redis at startup")
	}
	log.Info().Msg("Successfully connected to Redis!")

	subscriber := eventbus.NewSubscriber(rdb)
	log.Info().Msg("Batchopen service starting...")
	subscriber.Subscribe(ctx, func(event eventbus.Event) {
		handleEvent(event)
	})
}

func handleEvent(event eventbus.Event) {
	switch event.Type {
	case "StartBatchopenTask":
		startBatchopenTask(event)
	default:
		log.Warn().Str("eventType", event.Type).Msg("No handler found for event type")
	}
}

func startBatchopenTask(event eventbus.Event) {
	payloadBytes, err := json.Marshal(event.Payload)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal payload for StartBatchopenTask event")
		return
	}

	var payload struct {
		OfferID string `json:"offerId"`
		UserID  string `json:"userId"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal payload for StartBatchopenTask event")
		return
	}

	taskID := uuid.New().String()
	simulationConfig := map[string]interface{}{"clicks": 100, "duration": 60}
	simulationConfigJSON, _ := json.Marshal(simulationConfig)

	// Use adapter to create task
	err = adapter.CreateBatchopenTask(ctx, taskID, payload.UserID, payload.OfferID, simulationConfigJSON)

	if err != nil {
		log.Error().Err(err).Msg("Failed to create BatchopenTask")
	} else {
		log.Info().Str("taskID", taskID).Msg("BatchopenTask created")
		// In a real app, you would start the simulation here
	}
}
