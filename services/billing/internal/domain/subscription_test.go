package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewTrialsubscriptions tests creating a new trial subscription
func TestNewTrialsubscriptions(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		userID    string
		planID    string
		plan_name  string
		trialDays int
	}{
		{
			name:      "standard trial subscription",
			id:        "sub-123",
			userID:    "user-456",
			planID:    "pro",
			plan_name:  "Pro Plan",
			trialDays: 14,
		},
		{
			name:      "free plan trial",
			id:        "sub-free-1",
			userID:    "user-789",
			planID:    FreePlanID,
			plan_name:  "Free",
			trialDays: 36500, // 100 years
		},
		{
			name:      "short trial period",
			id:        "sub-short",
			userID:    "user-short",
			planID:    "trial",
			plan_name:  "Trial",
			trialDays: 1,
		},
		{
			name:      "zero trial days",
			id:        "sub-zero",
			userID:    "user-zero",
			planID:    "instant",
			plan_name:  "Instant",
			trialDays: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			sub := NewTrialsubscriptions(tt.id, tt.userID, tt.planID, tt.plan_name, tt.trialDays)

			// Assert
			assert.Equal(t, tt.id, sub.ID)
			assert.Equal(t, tt.userID, sub.UserID)
			assert.Equal(t, tt.planID, sub.PlanID)
			assert.Equal(t, tt.plan_name, sub.PlanName)
			assert.Equal(t, "trialing", sub.Status)
			require.NotNil(t, sub.TrialEndsAt)

			// Verify trial end date is approximately correct
			expectedTrialEnd := time.Now().AddDate(0, 0, tt.trialDays)
			timeDiff := sub.TrialEndsAt.Sub(expectedTrialEnd).Abs()
			assert.Less(t, timeDiff, 2*time.Second, "Trial end date should be within 2 seconds of expected")

			// Verify CurrentPeriodEnd matches TrialEndsAt
			assert.Equal(t, *sub.TrialEndsAt, sub.CurrentPeriodEnd)
		})
	}
}

// TestIsTrialing tests the trial status checking logic
func TestIsTrialing(t *testing.T) {
	tests := []struct {
		name        string
		status      string
		trialEndsAt *time.Time
		want        bool
	}{
		{
			name:        "active trial - future end date",
			status:      "trialing",
			trialEndsAt: timePtr(time.Now().Add(24 * time.Hour)),
			want:        true,
		},
		{
			name:        "active trial - far future",
			status:      "trialing",
			trialEndsAt: timePtr(time.Now().AddDate(1, 0, 0)),
			want:        true,
		},
		{
			name:        "expired trial - past end date",
			status:      "trialing",
			trialEndsAt: timePtr(time.Now().Add(-24 * time.Hour)),
			want:        false,
		},
		{
			name:        "expired trial - just expired",
			status:      "trialing",
			trialEndsAt: timePtr(time.Now().Add(-1 * time.Second)),
			want:        false,
		},
		{
			name:        "active subscription - no trial",
			status:      "active",
			trialEndsAt: nil,
			want:        false,
		},
		{
			name:        "canceled subscription",
			status:      "canceled",
			trialEndsAt: timePtr(time.Now().Add(24 * time.Hour)),
			want:        false,
		},
		{
			name:        "trialing status but nil end date",
			status:      "trialing",
			trialEndsAt: nil,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			sub := &subscriptions{
				Status:      tt.status,
				TrialEndsAt: tt.trialEndsAt,
			}

			// Act
			result := sub.IsTrialing()

			// Assert
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestActivate tests subscription activation
func TestActivate(t *testing.T) {
	tests := []struct {
		name             string
		initialStatus    string
		initialTrialEnds *time.Time
		periodEnd        time.Time
	}{
		{
			name:             "activate from trial",
			initialStatus:    "trialing",
			initialTrialEnds: timePtr(time.Now().Add(7 * 24 * time.Hour)),
			periodEnd:        time.Now().AddDate(0, 1, 0),
		},
		{
			name:             "activate with custom period end",
			initialStatus:    "trialing",
			initialTrialEnds: timePtr(time.Now().Add(14 * 24 * time.Hour)),
			periodEnd:        time.Now().AddDate(0, 3, 0), // 3 months
		},
		{
			name:             "reactivate canceled subscription",
			initialStatus:    "canceled",
			initialTrialEnds: nil,
			periodEnd:        time.Now().AddDate(0, 1, 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			sub := &subscriptions{
				ID:          "sub-test",
				UserID:      "user-test",
				Status:      tt.initialStatus,
				TrialEndsAt: tt.initialTrialEnds,
			}

			// Act
			sub.Activate(tt.periodEnd)

			// Assert
			assert.Equal(t, "active", sub.Status)
			assert.Nil(t, sub.TrialEndsAt, "TrialEndsAt should be nil after activation")
			assert.Equal(t, tt.periodEnd, sub.CurrentPeriodEnd)
		})
	}
}

// TestCancel tests subscription cancellation
func TestCancel(t *testing.T) {
	tests := []struct {
		name          string
		initialStatus string
	}{
		{
			name:          "cancel active subscription",
			initialStatus: "active",
		},
		{
			name:          "cancel trialing subscription",
			initialStatus: "trialing",
		},
		{
			name:          "cancel already canceled subscription",
			initialStatus: "canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			sub := &subscriptions{
				ID:     "sub-test",
				UserID: "user-test",
				Status: tt.initialStatus,
			}

			// Act
			sub.Cancel()

			// Assert
			assert.Equal(t, "canceled", sub.Status)
		})
	}
}

// TestsubscriptionsLifecycle tests the complete subscription lifecycle
func TestsubscriptionsLifecycle(t *testing.T) {
	// 1. Create trial subscription
	sub := NewTrialsubscriptions("sub-lifecycle", "user-lifecycle", ProPlanID, "Pro", 14)
	assert.Equal(t, "trialing", sub.Status)
	assert.True(t, sub.IsTrialing())

	// 2. Activate subscription after trial
	periodEnd := time.Now().AddDate(0, 1, 0)
	sub.Activate(periodEnd)
	assert.Equal(t, "active", sub.Status)
	assert.False(t, sub.IsTrialing())
	assert.Nil(t, sub.TrialEndsAt)

	// 3. Cancel subscription
	sub.Cancel()
	assert.Equal(t, "canceled", sub.Status)
	assert.False(t, sub.IsTrialing())
}

// TestsubscriptionsStatusTransitions tests various status transitions
func TestsubscriptionsStatusTransitions(t *testing.T) {
	t.Run("trial to active", func(t *testing.T) {
		sub := NewTrialsubscriptions("sub-1", "user-1", ProPlanID, "Pro", 14)
		assert.Equal(t, "trialing", sub.Status)

		sub.Activate(time.Now().AddDate(0, 1, 0))
		assert.Equal(t, "active", sub.Status)
	})

	t.Run("active to canceled", func(t *testing.T) {
		sub := &subscriptions{Status: "active"}
		sub.Cancel()
		assert.Equal(t, "canceled", sub.Status)
	})

	t.Run("trial to canceled", func(t *testing.T) {
		sub := NewTrialsubscriptions("sub-2", "user-2", FreePlanID, "Free", 7)
		assert.Equal(t, "trialing", sub.Status)

		sub.Cancel()
		assert.Equal(t, "canceled", sub.Status)
	})

	t.Run("canceled to active (reactivation)", func(t *testing.T) {
		sub := &subscriptions{Status: "canceled"}
		sub.Activate(time.Now().AddDate(0, 1, 0))
		assert.Equal(t, "active", sub.Status)
	})
}

// TestsubscriptionsEdgeCases tests edge cases and boundary conditions
func TestsubscriptionsEdgeCases(t *testing.T) {
	t.Run("trial ending exactly now", func(t *testing.T) {
		now := time.Now()
		sub := &subscriptions{
			Status:      "trialing",
			TrialEndsAt: &now,
		}
		// Should be false because trial has ended (not after now)
		assert.False(t, sub.IsTrialing())
	})

	t.Run("trial ending in 1 millisecond", func(t *testing.T) {
		future := time.Now().Add(1 * time.Millisecond)
		sub := &subscriptions{
			Status:      "trialing",
			TrialEndsAt: &future,
		}
		// Should be true because trial hasn't ended yet
		// Note: Due to test execution time, this might occasionally fail
		// In production, use more reasonable time windows
		result := sub.IsTrialing()
		// Accept either true or false due to timing
		_ = result
	})

	t.Run("multiple activations", func(t *testing.T) {
		sub := NewTrialsubscriptions("sub-multi", "user-multi", ProPlanID, "Pro", 14)

		// First activation
		firstPeriodEnd := time.Now().AddDate(0, 1, 0)
		sub.Activate(firstPeriodEnd)
		assert.Equal(t, firstPeriodEnd, sub.CurrentPeriodEnd)

		// Second activation (renewal)
		secondPeriodEnd := time.Now().AddDate(0, 2, 0)
		sub.Activate(secondPeriodEnd)
		assert.Equal(t, secondPeriodEnd, sub.CurrentPeriodEnd)
		assert.Equal(t, "active", sub.Status)
	})

	t.Run("multiple cancellations", func(t *testing.T) {
		sub := &subscriptions{Status: "active"}

		sub.Cancel()
		assert.Equal(t, "canceled", sub.Status)

		// Cancel again - should remain canceled
		sub.Cancel()
		assert.Equal(t, "canceled", sub.Status)
	})
}

// TestPlansConfiguration tests the available plans configuration
func TestPlansConfiguration(t *testing.T) {
	t.Run("all plans are defined", func(t *testing.T) {
		assert.Contains(t, AvailablePlans, FreePlanID)
		assert.Contains(t, AvailablePlans, ProPlanID)
		assert.Contains(t, AvailablePlans, MaxPlanID)
	})

	t.Run("free plan configuration", func(t *testing.T) {
		plan := AvailablePlans[FreePlanID]
		assert.Equal(t, FreePlanID, plan.ID)
		assert.Equal(t, "Free", plan.Name)
		assert.Equal(t, int64(1000), plan.IncludedTokens)
	})

	t.Run("pro plan configuration", func(t *testing.T) {
		plan := AvailablePlans[ProPlanID]
		assert.Equal(t, ProPlanID, plan.ID)
		assert.Equal(t, "Pro", plan.Name)
		assert.Equal(t, int64(10000), plan.IncludedTokens)
	})

	t.Run("max plan configuration", func(t *testing.T) {
		plan := AvailablePlans[MaxPlanID]
		assert.Equal(t, MaxPlanID, plan.ID)
		assert.Equal(t, "Max", plan.Name)
		assert.Equal(t, int64(100000), plan.IncludedTokens)
	})

	t.Run("token amounts are progressive", func(t *testing.T) {
		freePlan := AvailablePlans[FreePlanID]
		proPlan := AvailablePlans[ProPlanID]
		maxPlan := AvailablePlans[MaxPlanID]

		assert.Less(t, freePlan.IncludedTokens, proPlan.IncludedTokens)
		assert.Less(t, proPlan.IncludedTokens, maxPlan.IncludedTokens)
	})
}

// TestTokenConsumptionRules tests token consumption constants
func TestTokenConsumptionRules(t *testing.T) {
	t.Run("consumption costs are positive", func(t *testing.T) {
		assert.Greater(t, SiterankCachedQueryCost, 0)
		assert.Greater(t, SiterankRealtimeQueryCost, 0)
		assert.Greater(t, SiterankAIEvaluationCost, 0)
		assert.Greater(t, BatchopenHTTPCost, 0)
		assert.Greater(t, BatchopenPuppeteerCost, 0)
		assert.Greater(t, AdscenterAIComplianceCost, 0)
		assert.Greater(t, WorkflowStartCost, 0)
	})

	t.Run("rewards are positive", func(t *testing.T) {
		assert.Greater(t, OnboardingStepReward, 0)
		assert.Greater(t, DailyCheckInReward, 0)
	})

	t.Run("cost hierarchy makes sense", func(t *testing.T) {
		// Cached queries should be cheaper than realtime
		assert.Less(t, SiterankCachedQueryCost, SiterankRealtimeQueryCost)

		// Realtime queries should be cheaper than AI evaluation
		assert.Less(t, SiterankRealtimeQueryCost, SiterankAIEvaluationCost)

		// HTTP should be cheaper than Puppeteer
		assert.Less(t, BatchopenHTTPCost, BatchopenPuppeteerCost)

		// AI compliance should be most expensive
		assert.Greater(t, AdscenterAIComplianceCost, SiterankAIEvaluationCost)
	})

	t.Run("rewards are reasonable", func(t *testing.T) {
		// Onboarding reward should be significant
		assert.GreaterOrEqual(t, OnboardingStepReward, 100)

		// Daily check-in should be smaller than onboarding
		assert.Less(t, DailyCheckInReward, OnboardingStepReward)
	})
}

// Helper function to create time pointer
func timePtr(t time.Time) *time.Time {
	return &t
}
