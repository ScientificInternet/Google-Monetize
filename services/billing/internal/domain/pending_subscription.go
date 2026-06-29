package domain

import (
	"time"
)

// Pendingsubscriptions represents a subscription waiting to be activated
// due to tier conflicts with an active subscription
type Pendingsubscriptions struct {
	ID                     string     `json:"id"`
	UserID                 string     `json:"user_id"`
	PlanID                 string     `json:"planId"`
	PlanTier               int        `json:"planTier"`
	Status                 string     `json:"status"` // "pending", "activated", "expired", "canceled"
	Source                 string     `json:"source"` // "referral_inviter", "referral_invitee", "purchase", "admin"
	BlockingsubscriptionsID *string    `json:"blockingsubscriptionsId,omitempty"`
	TokenQuota             int        `json:"tokenQuota"`
	CreatedAt              time.Time  `json:"created_at"`
	ExpiresAt              time.Time  `json:"expiresAt"`
	ActivatedAt            *time.Time `json:"activatedAt,omitempty"`
	CanceledAt             *time.Time `json:"canceledAt,omitempty"`
	Metadata               string     `json:"metadata,omitempty"` // JSONB stored as string
}

// Pendingsubscriptions statuses
const (
	PendingStatusPending   = "pending"
	PendingStatusActivated = "activated"
	PendingStatusExpired   = "expired"
	PendingStatusCanceled  = "canceled"
)

// Pendingsubscriptions sources
const (
	PendingSourceReferralInviter = "referral_inviter"
	PendingSourceReferralInvitee = "referral_invitee"
	PendingSourcePurchase        = "purchase"
	PendingSourceAdmin           = "admin"
)

// Pending subscription expiry duration (180 days)
const PendingExpiryDays = 180

// NewPendingsubscriptions creates a new pending subscription
func NewPendingsubscriptions(id, userID, planID string, planTier int, source string, blockingsubscriptionsID *string, tokenQuota int) *Pendingsubscriptions {
	now := time.Now()
	expiresAt := now.AddDate(0, 0, PendingExpiryDays)

	return &Pendingsubscriptions{
		ID:                     id,
		UserID:                 userID,
		PlanID:                 planID,
		PlanTier:               planTier,
		Status:                 PendingStatusPending,
		Source:                 source,
		BlockingsubscriptionsID: blockingsubscriptionsID,
		TokenQuota:             tokenQuota,
		CreatedAt:              now,
		ExpiresAt:              expiresAt,
	}
}

// IsPending checks if the pending subscription is still in pending status
func (ps *Pendingsubscriptions) IsPending() bool {
	return ps.Status == PendingStatusPending
}

// IsExpired checks if the pending subscription has expired
func (ps *Pendingsubscriptions) IsExpired() bool {
	return ps.Status == PendingStatusExpired || ps.ExpiresAt.Before(time.Now())
}

// IsActivated checks if the pending subscription has been activated
func (ps *Pendingsubscriptions) IsActivated() bool {
	return ps.Status == PendingStatusActivated
}

// IsCanceled checks if the pending subscription has been canceled
func (ps *Pendingsubscriptions) IsCanceled() bool {
	return ps.Status == PendingStatusCanceled
}

// Activate marks the pending subscription as activated
func (ps *Pendingsubscriptions) Activate() {
	now := time.Now()
	ps.Status = PendingStatusActivated
	ps.ActivatedAt = &now
}

// Expire marks the pending subscription as expired
func (ps *Pendingsubscriptions) Expire() {
	ps.Status = PendingStatusExpired
}

// Cancel marks the pending subscription as canceled
func (ps *Pendingsubscriptions) Cancel() {
	now := time.Now()
	ps.Status = PendingStatusCanceled
	ps.CanceledAt = &now
}

// CanBeActivated checks if the pending subscription can be activated
// A pending subscription can be activated if:
// 1. It's in pending status
// 2. It hasn't expired
// 3. It's no longer blocked (blocking subscription has ended or is lower tier)
func (ps *Pendingsubscriptions) CanBeActivated() bool {
	return ps.IsPending() && !ps.IsExpired()
}
