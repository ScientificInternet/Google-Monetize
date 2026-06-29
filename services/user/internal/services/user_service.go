package services

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/models"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/repositories"
)

type UserService struct {
	userRepo repositories.UserRepositoryInterface
	redis    *redis.Client
}

func NewUserService(userRepo repositories.UserRepositoryInterface, redis *redis.Client) *UserService {
	return &UserService{
		userRepo: userRepo,
		redis:    redis,
	}
}

// GetUserProfile retrieves user profile with caching
func (s *UserService) GetUserProfile(ctx context.Context, userID string) (*models.User, error) {
	// Try to get from cache first
	cachedUser, err := s.getUserFromCache(ctx, userID)
	if err == nil && cachedUser != nil {
		return cachedUser, nil
	}

	// Get from Cloud SQL (primary source for business user data)
	user, err := s.userRepo.GetUserByID(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %s", userID)
	}

	// Cache the result
	s.cacheUser(ctx, user, 5*time.Minute)

	return user, nil
}

// GetUserCompleteInfo gets complete user information
func (s *UserService) GetUserCompleteInfo(ctx context.Context, userID string) (*models.CompleteUserInfo, error) {
	user, err := s.GetUserProfile(ctx, userID)
	if err != nil {
		return nil, err
	}

	// For now, return basic user info
	completeInfo := &models.CompleteUserInfo{
		User: *user,
		// TODO: Add permissions, tokens, subscription info
	}

	return completeInfo, nil
}

// SyncUser synchronizes user data
func (s *UserService) SyncUser(ctx context.Context, userID string) error {
	// Implementation placeholder
	return fmt.Errorf("sync functionality not implemented yet")
}

// StoreAdsRefreshToken stores Google Ads refresh token for user
func (s *UserService) StoreAdsRefreshToken(ctx context.Context, userID, loginCustomerID, encryptedToken string) error {
	// Use repository to store the refresh token
	return s.userRepo.StoreAdsRefreshToken(userID, loginCustomerID, encryptedToken)
}

// RevokeAdsRefreshToken revokes Google Ads refresh token for user
func (s *UserService) RevokeAdsRefreshToken(ctx context.Context, userID string) error {
	// Use repository to revoke/delete the refresh token
	return s.userRepo.RevokeAdsRefreshToken(userID)
}

// GetAdsTokens retrieves stored Google Ads tokens for user
func (s *UserService) GetAdsTokens(ctx context.Context, userID string) (map[string]interface{}, error) {
	// Use repository to get the tokens
	return s.userRepo.GetUserAdsTokens(userID)
}

// Helper functions for caching
func (s *UserService) getUserFromCache(ctx context.Context, userID string) (*models.User, error) {
	key := fmt.Sprintf("user:profile:%s", userID)
	_, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	// For simplicity, return nil (actual implementation would deserialize)
	return nil, fmt.Errorf("cache miss")
}

func (s *UserService) cacheUser(ctx context.Context, user *models.User, ttl time.Duration) {
	key := fmt.Sprintf("user:profile:%s", user.ID)
	// For simplicity, just set a simple value
	s.redis.Set(ctx, key, "cached_user_data", ttl)
}
