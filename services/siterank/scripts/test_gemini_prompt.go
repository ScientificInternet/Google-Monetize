package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/ScientificInternet/Google-Monetize/services/siterank/internal/aievaluator"
	"github.com/ScientificInternet/Google-Monetize/services/siterank/internal/similarweb"
)

func main() {
	ctx := context.Background()

	// Get project ID from environment
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GCP_PROJECT_ID")
	}

	// Initialize AI evaluator
	log.Println("Initializing Vertex AI Gemini client...")
	aiEval, err := aievaluator.NewService(ctx, projectID)
	if err != nil {
		log.Fatalf("Failed to create AI evaluator: %v", err)
	}
	defer aiEval.Close()

	log.Printf("Using model: %s", aiEval.GetModelName())
	log.Printf("Prompt version: %s", aiEval.GetPromptVersion())

	// Test Case 1: Premium Tier (expected score: 85-100)
	testPremiumOffer(ctx, aiEval)

	// Test Case 2: Recommended Tier (expected score: 70-84)
	testRecommendedOffer(ctx, aiEval)

	// Test Case 3: High-Risk Tier (expected score: 0-49)
	testHighRiskOffer(ctx, aiEval)
}

func testPremiumOffer(ctx context.Context, aiEval *aievaluator.Service) {
	fmt.Println("\n" + "="*80)
	fmt.Println("TEST CASE 1: Premium Tier Offer (Expected: 85-100)")
	fmt.Println("=" * 80)

	globalRank := int64(8500)
	categoryRank := int64(120)
	totalVisits := float64(25000000)
	bounceRate := float64(0.35)
	pagesPerVisit := float64(4.2)
	avgDuration := float64(195.0)
	direct := float64(0.38)
	search := float64(0.42)
	social := float64(0.08)
	paid := float64(0.08)
	referrals := float64(0.04)

	input := &aievaluator.EvaluationInput{
		Domain:         "example-premium.com",
		BrandName:      "Premium Brand",
		LandingPageURL: "https://example-premium.com/offers",
		SimilarWebData: &similarweb.SimilarWebData{
			GlobalRank:   &globalRank,
			Category:     "E-commerce & Shopping > Fashion & Apparel",
			CategoryRank: &categoryRank,
			TotalVisits:  &totalVisits,
			TrafficSources: &similarweb.TrafficSources{
				Direct:    &direct,
				Search:    &search,
				Social:    &social,
				Paid:      &paid,
				Referrals: &referrals,
			},
			EngagementMetrics: &similarweb.EngagementMetrics{
				BounceRate:       &bounceRate,
				PagesPerVisit:    &pagesPerVisit,
				AvgVisitDuration: &avgDuration,
			},
		},
	}

	evaluateAndPrint(ctx, aiEval, input, 85, 100)
}

func testRecommendedOffer(ctx context.Context, aiEval *aievaluator.Service) {
	fmt.Println("\n" + "="*80)
	fmt.Println("TEST CASE 2: Recommended Tier Offer (Expected: 70-84)")
	fmt.Println("=" * 80)

	globalRank := int64(150000)
	categoryRank := int64(3500)
	totalVisits := float64(850000)
	bounceRate := float64(0.52)
	pagesPerVisit := float64(2.8)
	avgDuration := float64(125.0)
	direct := float64(0.18)
	search := float64(0.38)
	social := float64(0.12)
	paid := float64(0.28)
	referrals := float64(0.04)

	input := &aievaluator.EvaluationInput{
		Domain:         "example-moderate.com",
		BrandName:      "Moderate Brand",
		LandingPageURL: "https://example-moderate.com/offers",
		SimilarWebData: &similarweb.SimilarWebData{
			GlobalRank:   &globalRank,
			Category:     "E-commerce & Shopping > Home & Garden",
			CategoryRank: &categoryRank,
			TotalVisits:  &totalVisits,
			TrafficSources: &similarweb.TrafficSources{
				Direct:    &direct,
				Search:    &search,
				Social:    &social,
				Paid:      &paid,
				Referrals: &referrals,
			},
			EngagementMetrics: &similarweb.EngagementMetrics{
				BounceRate:       &bounceRate,
				PagesPerVisit:    &pagesPerVisit,
				AvgVisitDuration: &avgDuration,
			},
		},
	}

	evaluateAndPrint(ctx, aiEval, input, 70, 84)
}

func testHighRiskOffer(ctx context.Context, aiEval *aievaluator.Service) {
	fmt.Println("\n" + "="*80)
	fmt.Println("TEST CASE 3: High-Risk Tier Offer (Expected: 0-49)")
	fmt.Println("=" * 80)

	globalRank := int64(3200000)
	categoryRank := int64(85000)
	totalVisits := float64(38000)
	bounceRate := float64(0.76)
	pagesPerVisit := float64(1.3)
	avgDuration := float64(32.0)
	direct := float64(0.06)
	search := float64(0.22)
	social := float64(0.15)
	paid := float64(0.52)
	referrals := float64(0.05)

	input := &aievaluator.EvaluationInput{
		Domain:         "example-risky.com",
		BrandName:      "Emerging Brand",
		LandingPageURL: "https://example-risky.com/offers",
		SimilarWebData: &similarweb.SimilarWebData{
			GlobalRank:   &globalRank,
			Category:     "E-commerce & Shopping > General Merchandise",
			CategoryRank: &categoryRank,
			TotalVisits:  &totalVisits,
			TrafficSources: &similarweb.TrafficSources{
				Direct:    &direct,
				Search:    &search,
				Social:    &social,
				Paid:      &paid,
				Referrals: &referrals,
			},
			EngagementMetrics: &similarweb.EngagementMetrics{
				BounceRate:       &bounceRate,
				PagesPerVisit:    &pagesPerVisit,
				AvgVisitDuration: &avgDuration,
			},
		},
	}

	evaluateAndPrint(ctx, aiEval, input, 0, 49)
}

func evaluateAndPrint(ctx context.Context, aiEval *aievaluator.Service, input *aievaluator.EvaluationInput, expectedMin, expectedMax int) {
	fmt.Printf("\nEvaluating: %s (Brand: %s)\n", input.Domain, input.BrandName)

	result, err := aiEval.EvaluateOffer(ctx, input)
	if err != nil {
		log.Printf("❌ FAILED: %v\n", err)
		return
	}

	// Print result
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println("\nAI Evaluation Result:")
	fmt.Println(string(resultJSON))

	// Validate score range
	score := result.RecommendationScore
	fmt.Printf("\n📊 Recommendation Score: %d/100\n", score)

	if score >= expectedMin && score <= expectedMax {
		fmt.Printf("✅ PASS: Score %d is within expected range [%d-%d]\n", score, expectedMin, expectedMax)
	} else {
		fmt.Printf("⚠️  WARNING: Score %d is outside expected range [%d-%d]\n", score, expectedMin, expectedMax)
	}

	// Validate reasons
	fmt.Printf("\n📝 Reasons (%d):\n", len(result.Reasons))
	for i, reason := range result.Reasons {
		fmt.Printf("  %d. %s\n", i+1, reason)
	}

	if len(result.Reasons) == 3 {
		fmt.Println("✅ PASS: Exactly 3 reasons provided")
	} else {
		fmt.Printf("❌ FAIL: Expected 3 reasons, got %d\n", len(result.Reasons))
	}

	// Validate industry
	fmt.Printf("\n🏭 Industry: %s\n", result.Industry)

	// Validate traffic insights
	if result.TrafficInsights != nil {
		fmt.Println("\n📈 Traffic Insights:")
		if summary, ok := result.TrafficInsights["summary"].(string); ok {
			fmt.Printf("  Summary: %s\n", summary)
		}
		if quality, ok := result.TrafficInsights["quality"].(string); ok {
			fmt.Printf("  Quality: %s\n", quality)
		}
		if keyMetric, ok := result.TrafficInsights["keyMetric"].(string); ok {
			fmt.Printf("  Key Metric: %s\n", keyMetric)
		}
	}

	// Validate ad insights
	if result.AdInsights != nil {
		fmt.Println("\n💰 Ad Insights:")
		if channels, ok := result.AdInsights["bestChannels"].([]interface{}); ok {
			fmt.Printf("  Best Channels: %v\n", channels)
		}
		if cpc, ok := result.AdInsights["estimatedCPC"].(string); ok {
			fmt.Printf("  Estimated CPC: %s\n", cpc)
		}
		if potential, ok := result.AdInsights["conversionPotential"].(string); ok {
			fmt.Printf("  Conversion Potential: %s\n", potential)
		}
	}

	fmt.Println("\n" + "-"*80)
}
