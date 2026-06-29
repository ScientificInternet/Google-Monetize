package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// NotificationType represents the type of notification
type NotificationType string

const (
	NotificationTypePendingActivated  NotificationType = "subscription.pending_activated"
	NotificationTypeTokenExpiringSoon NotificationType = "token.expiring_soon"
	NotificationTypeTokenExpired      NotificationType = "token.expired"
)

// NotificationClient handles sending notifications to users via console service
type NotificationClient struct {
	consoleURL string
	httpClient *http.Client
}

// NewNotificationClient creates a new notification client
func NewNotificationClient(consoleURL string) *NotificationClient {
	return &NotificationClient{
		consoleURL: consoleURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SendNotificationRequest represents the request to send a notification
type SendNotificationRequest struct {
	UserID string                 `json:"user_id"`
	Type   NotificationType       `json:"type"`
	Title  string                 `json:"title"`
	Body   string                 `json:"body"`
	Data   map[string]interface{} `json:"data,omitempty"`
}

// SendNotification sends a notification to a specific user
func (c *NotificationClient) SendNotification(ctx context.Context, req SendNotificationRequest) error {
	// For now, we'll use a simple HTTP POST to console service
	// In production, this might go through a message queue or event bus

	payload := map[string]interface{}{
		"user_id": req.UserID,
		"type":   req.Type,
		"title":  req.Title,
		"body":   req.Body,
		"data":   req.Data,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	endpoint := fmt.Sprintf("%s/internal/v1/notifications/send", c.consoleURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("notification service returned status %d", resp.StatusCode)
	}

	return nil
}

// SendPendingActivatedNotification sends notification when a pending subscription is activated
func (c *NotificationClient) SendPendingActivatedNotification(ctx context.Context, userID, planID string, tokensGranted int) error {
	return c.SendNotification(ctx, SendNotificationRequest{
		UserID: userID,
		Type:   NotificationTypePendingActivated,
		Title:  "套餐已激活",
		Body:   fmt.Sprintf("您的 %s 套餐已激活，获得 %d tokens", planID, tokensGranted),
		Data: map[string]interface{}{
			"planId":        planID,
			"tokensGranted": tokensGranted,
		},
	})
}

// SendTokenExpiringSoonNotification sends notification when tokens are expiring soon
func (c *NotificationClient) SendTokenExpiringSoonNotification(ctx context.Context, userID string, amount int, expiresAt time.Time) error {
	daysRemaining := int(time.Until(expiresAt).Hours() / 24)
	return c.SendNotification(ctx, SendNotificationRequest{
		UserID: userID,
		Type:   NotificationTypeTokenExpiringSoon,
		Title:  "Token即将过期",
		Body:   fmt.Sprintf("您有 %d tokens 将在 %d 天后过期", amount, daysRemaining),
		Data: map[string]interface{}{
			"amount":    amount,
			"expiresAt": expiresAt.Format(time.RFC3339),
		},
	})
}

// SendTokenExpiredNotification sends notification when tokens have expired
func (c *NotificationClient) SendTokenExpiredNotification(ctx context.Context, userID string, amount int) error {
	return c.SendNotification(ctx, SendNotificationRequest{
		UserID: userID,
		Type:   NotificationTypeTokenExpired,
		Title:  "Token已过期",
		Body:   fmt.Sprintf("您的 %d tokens 已过期并从余额中扣除", amount),
		Data: map[string]interface{}{
			"amount": amount,
		},
	})
}
