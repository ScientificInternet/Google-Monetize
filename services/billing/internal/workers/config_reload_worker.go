package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/redis/go-redis/v9"
)

// ============================================================================
// Config Reload Worker - 监听配置更新事件并刷新缓存
// ============================================================================

type ConfigReloadWorker struct {
	redisClient      *redis.Client
	pubsubClient     *pubsub.Client
	subscriptionName string
}

func NewConfigReloadWorker(redisClient *redis.Client, pubsubClient *pubsub.Client) *ConfigReloadWorker {
	return &ConfigReloadWorker{
		redisClient:      redisClient,
		pubsubClient:     pubsubClient,
		subscriptionName: "subscription-config-updated-billing",
	}
}

// Start 启动Worker监听Pub/Sub消息
func (w *ConfigReloadWorker) Start(ctx context.Context) error {
	sub := w.pubsubClient.subscriptions(w.subscriptionName)

	// 配置订阅参数
	sub.ReceiveSettings.MaxOutstandingMessages = 10
	sub.ReceiveSettings.MaxOutstandingBytes = 1e8
	sub.ReceiveSettings.NumGoroutines = 1

	log.Printf("[ConfigReloadWorker] Starting to listen on subscription: %s", w.subscriptionName)

	err := sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		if err := w.handleMessage(ctx, msg); err != nil {
			log.Printf("[ConfigReloadWorker] Error handling message: %v", err)
			msg.Nack()
			return
		}
		msg.Ack()
	})

	if err != nil {
		return fmt.Errorf("subscription receive error: %w", err)
	}

	return nil
}

// handleMessage 处理单个Pub/Sub消息
func (w *ConfigReloadWorker) handleMessage(ctx context.Context, msg *pubsub.Message) error {
	var event map[string]interface{}
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	eventType, ok := event["event"].(string)
	if !ok || eventType != "config_updated" {
		return fmt.Errorf("invalid event type: %v", event["event"])
	}

	tier, ok := event["tier"].(string)
	if !ok {
		return fmt.Errorf("missing tier in event")
	}

	version := int(event["version"].(float64))

	// 刷新缓存
	if err := w.invalidateCache(ctx, tier); err != nil {
		return fmt.Errorf("failed to invalidate cache: %w", err)
	}

	log.Printf("[ConfigReloadWorker] Reloaded config for tier: %s (version: %d)", tier, version)
	return nil
}

// invalidateCache 失效Redis缓存
func (w *ConfigReloadWorker) invalidateCache(ctx context.Context, tier string) error {
	cacheKeys := []string{
		fmt.Sprintf("subscription:config:%s", tier),
		"subscription:plans:all",
	}

	deleted, err := w.redisClient.Del(ctx, cacheKeys...).Result()
	if err != nil {
		return err
	}

	log.Printf("[ConfigReloadWorker] Invalidated %d cache keys for tier: %s", deleted, tier)
	return nil
}

// StartWithRetry 启动Worker，支持自动重连
func (w *ConfigReloadWorker) StartWithRetry(ctx context.Context) {
	retryDelay := 5 * time.Second
	maxRetryDelay := 60 * time.Second

	for {
		select {
		case <-ctx.Done():
			log.Printf("[ConfigReloadWorker] Context cancelled, stopping worker")
			return
		default:
		}

		err := w.Start(ctx)
		if err != nil {
			log.Printf("[ConfigReloadWorker] Worker error: %v, retrying in %v", err, retryDelay)
			time.Sleep(retryDelay)

			// 指数退避
			retryDelay *= 2
			if retryDelay > maxRetryDelay {
				retryDelay = maxRetryDelay
			}
		}
	}
}
