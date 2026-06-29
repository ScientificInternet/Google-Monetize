package models

import (
	"time"
)

// User represents the unified user model
type User struct {
	ID             string    `json:"id" db:"id"`
	Email          string    `json:"email" db:"email"`
	DisplayName    string    `json:"display_name" db:"display_name"`
	PhotoURL       string    `json:"photo_url" db:"photo_url"`
	Role           string    `json:"role" db:"role"`
	Onboarded      bool      `json:"onboarded" db:"onboarded"`
	IsActive       bool      `json:"is_active" db:"is_active"`
	OrganizationID *string   `json:"organization_id" db:"organization_id"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}

// UserPermissions represents user permissions based on subscription
type UserPermissions struct {
	ID                 string    `json:"id" db:"id"`
	UserID             string    `json:"user_id" db:"user_id"`
	IsAdmin            bool      `json:"is_admin" db:"is_admin"`
	SubscriptionPlan   string    `json:"subscription_plan" db:"subscription_plan"`
	CanUseAI           bool      `json:"can_use_ai" db:"can_use_ai"`
	CanCreateOffers    bool      `json:"can_create_offers" db:"can_create_offers"`
	CanManageAds       bool      `json:"can_manage_ads" db:"can_manage_ads"`
	CanAccessAnalytics bool      `json:"can_access_analytics" db:"can_access_analytics"`
	MaxOffersPerMonth  int       `json:"max_offers_per_month" db:"max_offers_per_month"`
	MaxTokensPerMonth  int       `json:"max_tokens_per_month" db:"max_tokens_per_month"`
	CanExportData      bool      `json:"can_export_data" db:"can_export_data"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`
}

// TokenBalance represents user's token balance
type TokenBalance struct {
	ID          string    `json:"id" db:"id"`
	UserID      string    `json:"user_id" db:"user_id"`
	Balance     int       `json:"balance" db:"balance"`
	Reserved    int       `json:"reserved" db:"reserved"`
	Available   int       `json:"available" db:"available"`
	TotalEarned int       `json:"total_earned" db:"total_earned"`
	TotalSpent  int       `json:"total_spent" db:"total_spent"`
	LastUpdated time.Time `json:"last_updated" db:"last_updated"`
}

// TokenReservation represents a token reservation
type TokenReservation struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	Amount      int        `json:"amount"`
	Reason      string     `json:"reason"`
	ReferenceID string     `json:"reference_id,omitempty"`
	Status      string     `json:"status"` // "reserved", "confirmed", "refunded", "expired"
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
}

// Subscription represents user subscription information
type Subscription struct {
	ID                  string     `json:"id" db:"id"`
	UserID              string     `json:"user_id" db:"user_id"`
	Plan                string     `json:"plan" db:"plan"`
	Status              string     `json:"status" db:"status"` // "active", "cancelled", "expired", "trial"
	IsTrial             bool       `json:"is_trial" db:"is_trial"`
	TrialEndsAt         *time.Time `json:"trial_ends_at" db:"trial_ends_at"`
	CurrentPeriodEndsAt *time.Time `json:"current_period_ends_at" db:"current_period_ends_at"`
	CancelledAt         *time.Time `json:"cancelled_at" db:"cancelled_at"`
	EndsAt              *time.Time `json:"ends_at" db:"ends_at"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at" db:"updated_at"`
}

// Organization represents user's organization
type Organization struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	OwnerID     string    `json:"owner_id" db:"owner_id"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// UserOrganization represents user-organization relationship
type UserOrganization struct {
	ID             string    `json:"id" db:"id"`
	UserID         string    `json:"user_id" db:"user_id"`
	OrganizationID string    `json:"organization_id" db:"organization_id"`
	Role           string    `json:"role" db:"role"` // "owner", "admin", "member"
	JoinedAt       time.Time `json:"joined_at" db:"joined_at"`
}

// UserActivity represents user activity tracking
type UserActivity struct {
	ID          string                 `json:"id" db:"id"`
	UserID      string                 `json:"user_id" db:"user_id"`
	Type        string                 `json:"type" db:"type"` // "login", "logout", "offer_created", "token_consumed", etc.
	Description string                 `json:"description" db:"description"`
	Metadata    map[string]interface{} `json:"metadata" db:"metadata"`
	IPAddress   string                 `json:"ip_address" db:"ip_address"`
	UserAgent   string                 `json:"user_agent" db:"user_agent"`
	CreatedAt   time.Time              `json:"created_at" db:"created_at"`
}

// UserStats represents aggregated user statistics
type UserStats struct {
	UserID           string     `json:"user_id"`
	TotalOffers      int        `json:"total_offers"`
	ActiveOffers     int        `json:"active_offers"`
	TotalEarnings    int        `json:"total_earnings"`
	TotalTokensSpent int        `json:"total_tokens_spent"`
	LastActiveAt     time.Time  `json:"last_active_at"`
	JoinDate         time.Time  `json:"join_date"`
	LoginCount       int        `json:"login_count"`
	LastLoginAt      *time.Time `json:"last_login_at"`
}

// SyncStatus represents data synchronization status
type SyncStatus struct {
	UserID          string    `json:"user_id"`
	LastSyncAt      time.Time `json:"last_sync_at"`
	SyncDirection   string    `json:"sync_direction"` // "to_gcp", "to_supabase", "bidirectional"`
	Status          string    `json:"status"`         // "success", "failed", "pending"
	ErrorMessage    string    `json:"error_message,omitempty"`
	Inconsistencies []string  `json:"inconsistencies,omitempty"`
}

// SyncConflict represents a data conflict between Supabase and GCP
type SyncConflict struct {
	UserID      string    `json:"user_id"`
	Table       string    `json:"table"`
	Field       string    `json:"field"`
	SupabaseVal string    `json:"supabase_value"`
	GCPVal      string    `json:"gcp_value"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CompleteUserInfo represents all user information in one structure
type CompleteUserInfo struct {
	User         User            `json:"user"`
	Permissions  UserPermissions `json:"permissions"`
	TokenBalance TokenBalance    `json:"token_balance"`
	Subscription Subscription    `json:"subscription"`
	Organization *Organization   `json:"organization,omitempty"`
	Stats        UserStats       `json:"stats"`
}

// UserSession represents the complete user session information
type UserSession struct {
	User           AuthUser         `json:"user"`
	Profile        User             `json:"profile"`
	Permissions    UserPermissions  `json:"permissions"`
	Subscription   SubscriptionInfo `json:"subscription"`
	Tokens         TokenBalance     `json:"tokens"`
	Organization   *Organization    `json:"organization,omitempty"`
	LastActivityAt string           `json:"last_activity_at"`
}

// AuthUser represents the authenticated user from Supabase Auth
type AuthUser struct {
	ID               string                 `json:"id"`
	Email            string                 `json:"email"`
	Phone            string                 `json:"phone"`
	EmailConfirmedAt *time.Time             `json:"email_confirmed_at"`
	PhoneConfirmedAt *time.Time             `json:"phone_confirmed_at"`
	LastSignInAt     *time.Time             `json:"last_sign_in_at"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	UserMetadata     map[string]interface{} `json:"user_metadata"`
	AppMetadata      map[string]interface{} `json:"app_metadata"`
}

// SubscriptionInfo represents subscription information for frontend
type SubscriptionInfo struct {
	ID                  string     `json:"id"`
	Plan                string     `json:"plan"`
	Status              string     `json:"status"`
	IsTrial             bool       `json:"is_trial"`
	TrialEndsAt         *time.Time `json:"trial_ends_at"`
	CurrentPeriodEndsAt *time.Time `json:"current_period_ends_at"`
	CanUseAI            bool       `json:"can_use_ai"`
	MaxOffersPerMonth   int        `json:"max_offers_per_month"`
	MaxTokensPerMonth   int        `json:"max_tokens_per_month"`
	Features            []string   `json:"features"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}
