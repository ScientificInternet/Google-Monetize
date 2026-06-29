// Package auth provides service-to-service authentication mechanisms
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ServiceAuthConfig holds configuration for service authentication
type ServiceAuthConfig struct {
	ServiceID     string `json:"service_id"`
	APIKey        string `json:"api_key"`
	SigningSecret string `json:"signing_secret"`
	RequestTTL    int    `json:"request_ttl_seconds"` // Default: 300 seconds (5 minutes)
}

// ServiceCredentials represents service authentication credentials
type ServiceCredentials struct {
	ServiceID     string `json:"service_id"`
	APIKey        string `json:"api_key"`
	SigningSecret string `json:"signing_secret"`
	Permissions   []string `json:"permissions"`
	RateLimit     int    `json:"rate_limit_per_minute"`
}

// ServiceAuth provides authentication and authorization for services
type ServiceAuth struct {
	config      ServiceAuthConfig
	credentials map[string]ServiceCredentials // service_id -> credentials
}

// NewServiceAuth creates a new service authentication instance
func NewServiceAuth(config ServiceAuthConfig) *ServiceAuth {
	return &ServiceAuth{
		config:      config,
		credentials: make(map[string]ServiceCredentials),
	}
}

// RegisterService registers a service with its credentials
func (sa *ServiceAuth) RegisterService(creds ServiceCredentials) error {
	if creds.ServiceID == "" || creds.APIKey == "" || creds.SigningSecret == "" {
		return fmt.Errorf("service_id, api_key, and signing_secret are required")
	}

	sa.credentials[creds.ServiceID] = creds
	return nil
}

// AuthHeaders represents the authentication headers for service requests
type AuthHeaders struct {
	ServiceID   string `json:"service_id"`
	APIKey      string `json:"api_key"`
	Timestamp   string `json:"timestamp"`
	Signature   string `json:"signature"`
	TraceID     string `json:"trace_id,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
}

// GenerateAuthHeaders generates authentication headers for a service request
func (sa *ServiceAuth) GenerateAuthHeaders(targetServiceID string, traceID, requestID string) (AuthHeaders, error) {
	// Get credentials for the target service
	creds, exists := sa.credentials[targetServiceID]
	if !exists {
		return AuthHeaders{}, fmt.Errorf("service %s not registered", targetServiceID)
	}

	// Generate timestamp
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	// Create signature string
	signatureString := fmt.Sprintf("%s:%s:%s:%s",
		sa.config.ServiceID, creds.APIKey, timestamp, targetServiceID)

	// Generate HMAC signature
	signature := generateHMACSignature(signatureString, creds.SigningSecret)

	headers := AuthHeaders{
		ServiceID: sa.config.ServiceID,
		APIKey:    creds.APIKey,
		Timestamp: timestamp,
		Signature: signature,
		TraceID:   traceID,
		RequestID: requestID,
	}

	return headers, nil
}

// SetAuthHeaders sets authentication headers on an HTTP request
func (sa *ServiceAuth) SetAuthHeaders(req *http.Request, targetServiceID string, traceID, requestID string) error {
	headers, err := sa.GenerateAuthHeaders(targetServiceID, traceID, requestID)
	if err != nil {
		return err
	}

	req.Header.Set("X-Service-ID", headers.ServiceID)
	req.Header.Set("X-API-Key", headers.APIKey)
	req.Header.Set("X-Timestamp", headers.Timestamp)
	req.Header.Set("X-Signature", headers.Signature)

	if headers.TraceID != "" {
		req.Header.Set("X-Trace-ID", headers.TraceID)
	}

	if headers.RequestID != "" {
		req.Header.Set("X-Request-ID", headers.RequestID)
	}

	return nil
}

// ValidateAuthHeaders validates authentication headers from incoming requests
func (sa *ServiceAuth) ValidateAuthHeaders(headers AuthHeaders) (*ServiceCredentials, error) {
	// Find service by API key
	var creds ServiceCredentials
	var found bool

	for _, c := range sa.credentials {
		if c.APIKey == headers.APIKey {
			creds = c
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("invalid API key")
	}

	// Validate timestamp (prevent replay attacks)
	timestamp, err := strconv.ParseInt(headers.Timestamp, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp format")
	}

	requestTime := time.Unix(timestamp, 0)
	now := time.Now()

	// Check if request is too old or too far in the future
	ttl := time.Duration(sa.config.RequestTTL) * time.Second
	if ttl == 0 {
		ttl = 5 * time.Minute // Default 5 minutes
	}

	if now.Sub(requestTime) > ttl || requestTime.Sub(now) > time.Minute {
		return nil, fmt.Errorf("request timestamp out of valid range")
	}

	// Validate signature
	expectedSignatureString := fmt.Sprintf("%s:%s:%s:%s",
		headers.ServiceID, headers.APIKey, headers.Timestamp, creds.ServiceID)
	expectedSignature := generateHMACSignature(expectedSignatureString, creds.SigningSecret)

	if !hmac.Equal([]byte(headers.Signature), []byte(expectedSignature)) {
		return nil, fmt.Errorf("invalid signature")
	}

	return &creds, nil
}

// ValidateHTTPRequest validates authentication from HTTP request headers
func (sa *ServiceAuth) ValidateHTTPRequest(req *http.Request) (*ServiceCredentials, error) {
	headers := AuthHeaders{
		ServiceID: req.Header.Get("X-Service-ID"),
		APIKey:    req.Header.Get("X-API-Key"),
		Timestamp: req.Header.Get("X-Timestamp"),
		Signature: req.Header.Get("X-Signature"),
		TraceID:   req.Header.Get("X-Trace-ID"),
		RequestID: req.Header.Get("X-Request-ID"),
	}

	return sa.ValidateAuthHeaders(headers)
}

// HasPermission checks if a service has a specific permission
func (sa *ServiceAuth) HasPermission(creds *ServiceCredentials, permission string) bool {
	for _, p := range creds.Permissions {
		if p == permission || p == "*" {
			return true
		}
	}
	return false
}

// CheckRateLimit checks if a service has exceeded its rate limit
type RateLimitChecker struct {
	requests map[string][]time.Time // service_id -> request timestamps
}

func NewRateLimitChecker() *RateLimitChecker {
	return &RateLimitChecker{
		requests: make(map[string][]time.Time),
	}
}

func (rlc *RateLimitChecker) CheckRateLimit(serviceID string, limit int, window time.Duration) bool {
	now := time.Now()

	// Clean old requests
	if requests, exists := rlc.requests[serviceID]; exists {
		var validRequests []time.Time
		for _, reqTime := range requests {
			if now.Sub(reqTime) < window {
				validRequests = append(validRequests, reqTime)
			}
		}
		rlc.requests[serviceID] = validRequests
	}

	// Check current request count
	currentCount := len(rlc.requests[serviceID])
	if currentCount >= limit {
		return false
	}

	// Add current request
	rlc.requests[serviceID] = append(rlc.requests[serviceID], now)
	return true
}

// generateHMACSignature generates HMAC-SHA256 signature
func generateHMACSignature(data, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// JWTServiceAuth provides JWT-based service authentication
type JWTServiceAuth struct {
	issuer    string
	secretKey []byte
}

// NewJWTServiceAuth creates a new JWT service authentication instance
func NewJWTServiceAuth(issuer, secretKey string) *JWTServiceAuth {
	return &JWTServiceAuth{
		issuer:    issuer,
		secretKey: []byte(secretKey),
	}
}

// ServiceClaims represents claims for service JWT tokens
type ServiceClaims struct {
	ServiceID   string   `json:"service_id"`
	Permissions []string `json:"permissions"`
	RateLimit   int      `json:"rate_limit"`
	IssuedAt    int64    `json:"iat"`
	ExpiresAt   int64    `json:"exp"`
}

// GenerateServiceToken generates a JWT token for service authentication
func (jsa *JWTServiceAuth) GenerateServiceToken(creds ServiceCredentials, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := ServiceClaims{
		ServiceID:   creds.ServiceID,
		Permissions: creds.Permissions,
		RateLimit:   creds.RateLimit,
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(ttl).Unix(),
	}

	// This is a simplified JWT implementation
	// In production, use a proper JWT library like github.com/golang-jwt/jwt
	tokenData, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	// Simple encoding (not secure for production)
	token := fmt.Sprintf("%s.%s", jsa.issuer, string(tokenData))
	signature := generateHMACSignature(token, string(jsa.secretKey))

	return fmt.Sprintf("%s.%s", token, signature), nil
}

// ValidateServiceToken validates a service JWT token
func (jsa *JWTServiceAuth) ValidateServiceToken(token string) (*ServiceClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	// Verify signature
	expectedSignature := generateHMACSignature(parts[0]+"."+parts[1], string(jsa.secretKey))
	if parts[2] != expectedSignature {
		return nil, fmt.Errorf("invalid token signature")
	}

	// Parse claims
	var claims ServiceClaims
	if err := json.Unmarshal([]byte(parts[1]), &claims); err != nil {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Check expiration
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("token expired")
	}

	return &claims, nil
}

// ClientAuth provides authentication for outbound service calls
type ClientAuth struct {
	serviceAuth *ServiceAuth
	serviceID   string
}

// NewClientAuth creates a new client authentication instance
func NewClientAuth(serviceAuth *ServiceAuth, serviceID string) *ClientAuth {
	return &ClientAuth{
		serviceAuth: serviceAuth,
		serviceID:   serviceID,
	}
}

// AuthenticateRequest authenticates an outbound HTTP request
func (ca *ClientAuth) AuthenticateRequest(req *http.Request, targetServiceID, traceID, requestID string) error {
	return ca.serviceAuth.SetAuthHeaders(req, targetServiceID, traceID, requestID)
}

// HTTPClient wraps an HTTP client with automatic authentication
type HTTPClient struct {
	client      *http.Client
	clientAuth  *ClientAuth
	traceIDGen  func() string
	requestIDGen func() string
}

// NewHTTPClient creates a new authenticated HTTP client
func NewHTTPClient(serviceAuth *ServiceAuth, serviceID string) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		clientAuth: NewClientAuth(serviceAuth, serviceID),
		traceIDGen: func() string {
			return fmt.Sprintf("trace_%d_%s", time.Now().UnixNano(),
				strings.Repeat("x", 8)) // Simplified trace ID
		},
		requestIDGen: func() string {
			return fmt.Sprintf("req_%d_%s", time.Now().UnixNano(),
				strings.Repeat("y", 8)) // Simplified request ID
		},
	}
}

// Do executes an HTTP request with automatic authentication
func (c *HTTPClient) Do(req *http.Request, targetServiceID string) (*http.Response, error) {
	traceID := c.traceIDGen()
	requestID := c.requestIDGen()

	// Set authentication headers
	if err := c.clientAuth.AuthenticateRequest(req, targetServiceID, traceID, requestID); err != nil {
		return nil, err
	}

	// Execute request
	return c.client.Do(req)
}