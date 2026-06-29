//go:build integration
// +build integration

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	defaultBillingURL = "https://billing-preview-644672509127.asia-northeast1.run.app"
)

var billingURL string

func init() {
	billingURL = os.Getenv("BILLING_URL")
	if billingURL == "" {
		billingURL = defaultBillingURL
	}
}

// TestBillingHealthCheck tests the health check endpoint
func TestBillingHealthCheck(t *testing.T) {
	resp, err := http.Get(billingURL + "/healthz")
	require.NoError(t, err, "Should be able to call health check")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Health check should return 200")
}

// TestBillingHealth tests the health endpoint
func TestBillingHealth(t *testing.T) {
	resp, err := http.Get(billingURL + "/health")
	require.NoError(t, err, "Should be able to call health endpoint")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Health endpoint should return 200")
}

// TestBillingReadiness tests the readiness endpoint
func TestBillingReadiness(t *testing.T) {
	resp, err := http.Get(billingURL + "/readyz")
	require.NoError(t, err, "Should be able to call readiness endpoint")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Readiness check should return 200")
}

// TestBillingMetrics tests the Prometheus metrics endpoint
func TestBillingMetrics(t *testing.T) {
	resp, err := http.Get(billingURL + "/metrics")
	require.NoError(t, err, "Should be able to call metrics endpoint")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Metrics should return 200")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	metrics := string(body)
	assert.Contains(t, metrics, "go_", "Should contain Go metrics")
}

// TestBillingGetBalance tests getting user balance (requires auth)
func TestBillingGetBalance(t *testing.T) {
	token := os.Getenv("TEST_AUTH_TOKEN")
	if token == "" {
		t.Skip("Skipping: TEST_AUTH_TOKEN not set")
	}

	req, err := http.NewRequest("GET", billingURL+"/api/v1/billing/balance", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should return 200 or 401 (if token invalid)
	assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized,
		fmt.Sprintf("Expected 200 or 401, got %d", resp.StatusCode))

	if resp.StatusCode == http.StatusOK {
		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		assert.Contains(t, result, "balance", "Response should contain balance")
	}
}

// TestBillingReserveTokens tests token reservation (requires auth)
func TestBillingReserveTokens(t *testing.T) {
	token := os.Getenv("TEST_AUTH_TOKEN")
	if token == "" {
		t.Skip("Skipping: TEST_AUTH_TOKEN not set")
	}

	payload := map[string]interface{}{
		"amount": 10,
		"reason": "integration test",
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequest("POST", billingURL+"/api/v1/billing/reserve", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should return 200, 400, or 401
	assert.True(t, resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusBadRequest ||
		resp.StatusCode == http.StatusUnauthorized,
		fmt.Sprintf("Expected 200, 400, or 401, got %d", resp.StatusCode))
}

// TestBillingServiceAvailability tests overall service availability
func TestBillingServiceAvailability(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		method   string
	}{
		{"health check", "/healthz", "GET"},
		{"health", "/health", "GET"},
		{"readiness", "/readyz", "GET"},
		{"metrics", "/metrics", "GET"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp *http.Response
			var err error

			if tt.method == "GET" {
				resp, err = http.Get(billingURL + tt.endpoint)
			} else {
				resp, err = http.Post(billingURL+tt.endpoint, "application/json", nil)
			}

			require.NoError(t, err, "Should be able to call "+tt.endpoint)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode,
				fmt.Sprintf("%s should return 200", tt.endpoint))
		})
	}
}

// TestBillingResponseTime tests service response time
func TestBillingResponseTime(t *testing.T) {
	start := time.Now()
	resp, err := http.Get(billingURL + "/healthz")
	duration := time.Since(start)

	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Less(t, duration, 5*time.Second, "Health check should respond within 5 seconds")
	t.Logf("Response time: %v", duration)
}

// TestBillingConcurrentRequests tests handling concurrent requests
func TestBillingConcurrentRequests(t *testing.T) {
	const numRequests = 10

	results := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			resp, err := http.Get(billingURL + "/healthz")
			if err != nil {
				results <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				results <- fmt.Errorf("expected 200, got %d", resp.StatusCode)
				return
			}
			results <- nil
		}()
	}

	// Collect results
	successCount := 0
	for i := 0; i < numRequests; i++ {
		err := <-results
		if err == nil {
			successCount++
		} else {
			t.Logf("Request failed: %v", err)
		}
	}

	assert.GreaterOrEqual(t, successCount, numRequests*8/10,
		"At least 80% of concurrent requests should succeed")
	t.Logf("Success rate: %d/%d", successCount, numRequests)
}
