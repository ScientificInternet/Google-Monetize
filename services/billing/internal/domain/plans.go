package domain

// Plan defines the structure for a subscription plan.
type Plan struct {
	ID             string
	Name           string
	IncludedTokens int64
}

// Plan IDs
const (
	FreePlanID = "free"
	ProPlanID  = "pro"
	MaxPlanID  = "max"
)

// AvailablePlans maps plan IDs to their definitions.
var AvailablePlans = map[string]Plan{
	FreePlanID: {
		ID:             FreePlanID,
		Name:           "Free",
		IncludedTokens: 1000,
	},
	ProPlanID: {
		ID:             ProPlanID,
		Name:           "Pro",
		IncludedTokens: 10000,
	},
	MaxPlanID: {
		ID:             MaxPlanID,
		Name:           "Max",
		IncludedTokens: 100000,
	},
}

// TokenConsumptionRules defines the cost for various actions.
const (
	SiterankCachedQueryCost   = 1
	SiterankRealtimeQueryCost = 5
	SiterankAIEvaluationCost  = 10
	BatchopenHTTPCost         = 1
	BatchopenPuppeteerCost    = 2
	AdscenterAIComplianceCost = 25
	WorkflowStartCost         = 5
	OnboardingStepReward      = 200 // Total reward, can be broken down
	DailyCheckInReward        = 10
)
