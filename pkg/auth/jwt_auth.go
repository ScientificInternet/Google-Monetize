// Package auth provides JWT authentication mechanisms
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTConfig holds configuration for JWT authentication
type JWTConfig struct {
	Issuer             string `json:"issuer"`
	SecretKey          string `json:"secret_key"`
	AccessTokenTTL     int    `json:"access_token_ttl_minutes"`     // Default: 15 minutes
	RefreshTokenTTL    int    `json:"refresh_token_ttl_hours"`     // Default: 168 hours (7 days)
	ResetTokenTTL      int    `json:"reset_token_ttl_hours"`       // Default: 1 hour
}

// TokenClaims represents JWT claims structure
type TokenClaims struct {
	UserID      string   `json:"user_id"`
	Email       string   `json:"email"`
	Name        string   `json:"name,omitempty"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
	ServiceID   string   `json:"service_id,omitempty"` // For service tokens
	TokenType   string   `json:"token_type"`            // access, refresh, reset, service
	SessionID   string   `json:"session_id,omitempty"`
	jwt.RegisteredClaims
}

// TokenPair represents access and refresh token pair
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
}

// JWTManager provides JWT token management
type JWTManager struct {
	config JWTConfig
}

// NewJWTManager creates a new JWT manager
func NewJWTManager(config JWTConfig) *JWTManager {
	// Set default values
	if config.AccessTokenTTL == 0 {
		config.AccessTokenTTL = 15 // 15 minutes
	}
	if config.RefreshTokenTTL == 0 {
		config.RefreshTokenTTL = 168 // 7 days
	}
	if config.ResetTokenTTL == 0 {
		config.ResetTokenTTL = 1 // 1 hour
	}

	return &JWTManager{
		config: config,
	}
}

// GenerateAccessToken generates a new JWT access token
func (j *JWTManager) GenerateAccessToken(userID, email, name, role string, permissions []string, sessionID string) (string, error) {
	claims := TokenClaims{
		UserID:      userID,
		Email:       email,
		Name:        name,
		Role:        role,
		Permissions: permissions,
		TokenType:   "access",
		SessionID:   sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.config.Issuer,
			Subject:   userID,
			Audience:  []string{"gm-api"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(j.config.AccessTokenTTL) * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			ID:        generateTokenID(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(j.config.SecretKey))
}

// GenerateRefreshToken generates a new JWT refresh token
func (j *JWTManager) GenerateRefreshToken(userID, sessionID string) (string, error) {
	claims := TokenClaims{
		UserID:    userID,
		TokenType: "refresh",
		SessionID: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.config.Issuer,
			Subject:   userID,
			Audience:  []string{"gm-api"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(j.config.RefreshTokenTTL) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			ID:        generateTokenID(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(j.config.SecretKey))
}

// GeneratePasswordResetToken generates a new password reset token
func (j *JWTManager) GeneratePasswordResetToken(userID string) (string, error) {
	claims := TokenClaims{
		UserID:    userID,
		TokenType: "reset",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.config.Issuer,
			Subject:   userID,
			Audience:  []string{"gm-api"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(j.config.ResetTokenTTL) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			ID:        generateTokenID(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(j.config.SecretKey))
}

// GenerateServiceToken generates a JWT token for service-to-service authentication
func (j *JWTManager) GenerateServiceToken(serviceID string, permissions []string, ttl time.Duration) (string, error) {
	claims := TokenClaims{
		ServiceID:   serviceID,
		Role:        "service",
		Permissions: permissions,
		TokenType:   "service",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.config.Issuer,
			Subject:   serviceID,
			Audience:  []string{"gm-api"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			ID:        generateTokenID(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(j.config.SecretKey))
}

// GenerateTokenPair generates both access and refresh tokens
func (j *JWTManager) GenerateTokenPair(userID, email, name, role string, permissions []string, sessionID string) (*TokenPair, error) {
	accessToken, err := j.GenerateAccessToken(userID, email, name, role, permissions, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := j.GenerateRefreshToken(userID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(j.config.AccessTokenTTL) * time.Minute),
		TokenType:    "Bearer",
	}, nil
}

// ValidateToken validates a JWT token and returns claims
func (j *JWTManager) ValidateToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(j.config.SecretKey), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*TokenClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Validate issuer
	if claims.Issuer != j.config.Issuer {
		return nil, fmt.Errorf("invalid token issuer")
	}

	// Validate audience
	if !containsString(claims.Audience, "gm-api") {
		return nil, fmt.Errorf("invalid token audience")
	}

	return claims, nil
}

// RefreshAccessToken generates a new access token from a valid refresh token
func (j *JWTManager) RefreshAccessToken(refreshTokenString string) (*TokenPair, error) {
	// Validate refresh token
	claims, err := j.ValidateToken(refreshTokenString)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	// Ensure it's a refresh token
	if claims.TokenType != "refresh" {
		return nil, fmt.Errorf("token is not a refresh token")
	}

	// Generate new token pair
	// Note: In a real implementation, you would fetch user details from database
	return j.GenerateTokenPair(claims.UserID, claims.Email, claims.Name, claims.Role, claims.Permissions, claims.SessionID)
}

// InvalidateToken adds a token to the blacklist (placeholder for Redis implementation)
func (j *JWTManager) InvalidateToken(tokenID string) error {
	// TODO: Implement token blacklist using Redis
	// This would store the token ID with an expiration time
	// matching the token's original expiration
	return nil
}

// IsTokenBlacklisted checks if a token is blacklisted (placeholder for Redis implementation)
func (j *JWTManager) IsTokenBlacklisted(tokenID string) bool {
	// TODO: Implement Redis check for blacklisted tokens
	return false
}

// ExtractTokenFromHeader extracts JWT token from Authorization header
func ExtractTokenFromHeader(authHeader string) (string, error) {
	if authHeader == "" {
		return "", fmt.Errorf("authorization header is empty")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", fmt.Errorf("authorization header format must be Bearer {token}")
	}

	return parts[1], nil
}

// PermissionCheckResult represents the result of a permission check
type PermissionCheckResult struct {
	HasPermission bool     `json:"has_permission"`
	Reason        string   `json:"reason,omitempty"`
	Required      []string `json:"required,omitempty"`
	Granted       []string `json:"granted,omitempty"`
}

// HasPermission checks if claims include a specific permission
func (c *TokenClaims) HasPermission(permission string) bool {
	for _, p := range c.Permissions {
		if p == permission || p == "*" {
			return true
		}
	}
	return false
}

// HasAnyPermission checks if claims include any of the specified permissions
func (c *TokenClaims) HasAnyPermission(permissions []string) PermissionCheckResult {
	result := PermissionCheckResult{
		Required: permissions,
		Granted:  []string{},
	}

	for _, permission := range permissions {
		if c.HasPermission(permission) {
			result.HasPermission = true
			result.Granted = append(result.Granted, permission)
		}
	}

	if !result.HasPermission {
		result.Reason = "None of the required permissions are granted"
	}

	return result
}

// HasAllPermissions checks if claims include all specified permissions
func (c *TokenClaims) HasAllPermissions(permissions []string) PermissionCheckResult {
	result := PermissionCheckResult{
		Required: permissions,
		Granted:  []string{},
	}

	for _, permission := range permissions {
		if c.HasPermission(permission) {
			result.Granted = append(result.Granted, permission)
		} else {
			result.HasPermission = false
			result.Reason = fmt.Sprintf("Missing required permission: %s", permission)
			return result
		}
	}

	result.HasPermission = true
	return result
}

// IsExpired checks if the token has expired
func (c *TokenClaims) IsExpired() bool {
	return time.Now().After(c.ExpiresAt.Time)
}

// IsServiceToken checks if this is a service token
func (c *TokenClaims) IsServiceToken() bool {
	return c.TokenType == "service"
}

// IsUserToken checks if this is a user token (access or refresh)
func (c *TokenClaims) IsUserToken() bool {
	return c.TokenType == "access" || c.TokenType == "refresh"
}

// ToJSON converts claims to JSON representation
func (c *TokenClaims) ToJSON() (map[string]interface{}, error) {
	data := map[string]interface{}{
		"user_id":      c.UserID,
		"email":        c.Email,
		"role":         c.Role,
		"permissions":  c.Permissions,
		"token_type":   c.TokenType,
		"exp":          c.ExpiresAt.Unix(),
		"iat":          c.IssuedAt.Unix(),
		"iss":          c.Issuer,
	}

	if c.Name != "" {
		data["name"] = c.Name
	}

	if c.ServiceID != "" {
		data["service_id"] = c.ServiceID
	}

	if c.SessionID != "" {
		data["session_id"] = c.SessionID
	}

	return data, nil
}

// generateTokenID generates a unique token ID
func generateTokenID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return base64.URLEncoding.EncodeToString(bytes)
}

// containsString checks if a string slice contains a specific string
func containsString(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// TokenBlacklist provides token blacklist functionality using Redis
type TokenBlacklist struct {
	// redisClient *redis.Client
	// In a real implementation, you would inject a Redis client
}

// NewTokenBlacklist creates a new token blacklist
func NewTokenBlacklist() *TokenBlacklist {
	return &TokenBlacklist{}
}

// AddToken adds a token to the blacklist
func (tb *TokenBlacklist) AddToken(tokenID string, expiresAt time.Time) error {
	// TODO: Implement Redis SET with expiration
	// redisClient.Set(ctx, "blacklist:"+tokenID, "1", time.Until(expiresAt))
	return nil
}

// IsBlacklisted checks if a token is blacklisted
func (tb *TokenBlacklist) IsBlacklisted(tokenID string) (bool, error) {
	// TODO: Implement Redis EXISTS check
	// result, err := redisClient.Exists(ctx, "blacklist:"+tokenID).Result()
	// return result > 0, err
	return false, nil
}

// RemoveToken removes a token from the blacklist
func (tb *TokenBlacklist) RemoveToken(tokenID string) error {
	// TODO: Implement Redis DEL
	// redisClient.Del(ctx, "blacklist:"+tokenID)
	return nil
}

// CleanupExpiredTokens removes expired tokens from the blacklist
func (tb *TokenBlacklist) CleanupExpiredTokens() error {
	// TODO: Implement cleanup logic if needed
	// Redis automatically handles expiration, but you might want to
	// periodically scan for cleanup if using a different backend
	return nil
}