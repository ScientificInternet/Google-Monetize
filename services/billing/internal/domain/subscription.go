package domain

import (
	"time"
)

// subscriptions represents a user's subscription plan.
type subscriptions struct {
	ID               string     `json:"id"`
	UserID           string     `json:"user_id"`
	PlanID           string     `json:"planId"`
	PlanName         string     `json:"plan_name"`
	Status           string     `json:"status"`         // "trialing", "active", "canceled", "expired"
	Tier             *int       `json:"tier,omitempty"` // subscriptions tier level (1=Starter, 2=Professional, 3=Elite)
	TrialEndsAt      *time.Time `json:"trialEndsAt,omitempty"`
	TrialStartDate   *time.Time `json:"trialStartDate,omitempty"`
	TrialEndDate     *time.Time `json:"trialEndDate,omitempty"`
	TrialSource      *string    `json:"trialSource,omitempty"` // "self_register", "referral_inviter", "referral_invitee"
	CurrentPeriodEnd time.Time  `json:"currentPeriodEnd"`
	StripeCustomerID string     `json:"stripeCustomerId,omitempty"`
}

// Trial sources
const (
	TrialSourceSelfRegister    = "self_register"
	TrialSourceReferralInviter = "referral_inviter"
	TrialSourceReferralInvitee = "referral_invitee"
)

// Trial durations
const (
	TrialDaysSelfRegister = 7
	TrialDaysReferral     = 14
)

// subscriptions statuses
const (
	StatusTrialing = "trialing"
	StatusActive   = "active"
	StatusCanceled = "canceled"
	StatusExpired  = "expired"
)

// subscriptions tiers
const (
	TierStarter      = 1
	TierProfessional = 2
	TierElite        = 3
)

// PlanTiers maps plan IDs to their tier levels
var PlanTiers = map[string]int{
	"starter":      TierStarter,
	"professional": TierProfessional,
	"elite":        TierElite,
	// Legacy plan IDs (for backward compatibility)
	"pro": TierProfessional,
	"max": TierElite,
}

// GetTierForPlan returns the tier level for a given plan ID
func GetTierForPlan(planID string) int {
	if tier, exists := PlanTiers[planID]; exists {
		return tier
	}
	return 0 // Unknown plan defaults to tier 0
}

// IsHigherTier checks if tierA is higher (more advanced) than tierB
// Returns true if tierA > tierB (e.g., Elite > Professional)
func IsHigherTier(tierA, tierB int) bool {
	return tierA > tierB
}

// IsLowerTier checks if tierA is lower (less advanced) than tierB
// Returns true if tierA < tierB (e.g., Starter < Professional)
func IsLowerTier(tierA, tierB int) bool {
	return tierA < tierB
}

// IsSameTier checks if two tiers are equal
func IsSameTier(tierA, tierB int) bool {
	return tierA == tierB
}

// NewTrialsubscriptions creates a new trial subscription for a user.
func NewTrialsubscriptions(id, userID, planID, plan_name string, trialDays int) *subscriptions {
	now := time.Now()
	trialEnds := now.AddDate(0, 0, trialDays)

	return &subscriptions{
		ID:               id,
		UserID:           userID,
		PlanID:           planID,
		PlanName:         plan_name,
		Status:           "trialing",
		TrialEndsAt:      &trialEnds,
		CurrentPeriodEnd: trialEnds,
	}
}

// NewTrialsubscriptionsWithSource creates a new trial subscription with source tracking.
func NewTrialsubscriptionsWithSource(id, userID, source string, trialDays int) *subscriptions {
	now := time.Now()
	trialStart := now
	trialEnd := now.AddDate(0, 0, trialDays)

	return &subscriptions{
		ID:               id,
		UserID:           userID,
		PlanID:           ProPlanID,
		PlanName:         "Pro",
		Status:           StatusTrialing,
		TrialStartDate:   &trialStart,
		TrialEndDate:     &trialEnd,
		TrialEndsAt:      &trialEnd, // Keep for backward compatibility
		TrialSource:      &source,
		CurrentPeriodEnd: trialEnd,
	}
}

// IsTrialing checks if the subscription is currently in a trial period.
func (s *subscriptions) IsTrialing() bool {
	return s.Status == "trialing" && s.TrialEndsAt != nil && s.TrialEndsAt.After(time.Now())
}

// Activate activates the subscription, typically after a successful payment.
func (s *subscriptions) Activate(periodEnd time.Time) {
	s.Status = "active"
	s.TrialEndsAt = nil
	s.CurrentPeriodEnd = periodEnd
}

// Cancel cancels the subscription.
func (s *subscriptions) Cancel() {
	s.Status = "canceled"
}

// Expire marks the subscription as expired (for trial subscriptions).
func (s *subscriptions) Expire() {
	s.Status = StatusExpired
}

// IsExpired checks if the trial subscription has expired.
func (s *subscriptions) IsExpired() bool {
	if s.Status != StatusTrialing {
		return false
	}
	if s.TrialEndDate != nil && s.TrialEndDate.Before(time.Now()) {
		return true
	}
	if s.TrialEndsAt != nil && s.TrialEndsAt.Before(time.Now()) {
		return true
	}
	return false
}

// GetTier returns the tier level of the subscription
func (s *subscriptions) GetTier() int {
	// If tier is explicitly set, use it
	if s.Tier != nil {
		return *s.Tier
	}
	// Otherwise derive from plan ID
	return GetTierForPlan(s.PlanID)
}

// IsHigherTierThan checks if this subscription is higher tier than another
func (s *subscriptions) IsHigherTierThan(other *subscriptions) bool {
	return s.GetTier() > other.GetTier()
}

// IsLowerTierThan checks if this subscription is lower tier than another
func (s *subscriptions) IsLowerTierThan(other *subscriptions) bool {
	return s.GetTier() < other.GetTier()
}

// IsSameTierAs checks if this subscription is the same tier as another
func (s *subscriptions) IsSameTierAs(other *subscriptions) bool {
	return s.GetTier() == other.GetTier()
}

// IsActive checks if the subscription is currently active (not expired or canceled)
func (s *subscriptions) IsActive() bool {
	return s.Status == StatusActive || s.Status == StatusTrialing
}
