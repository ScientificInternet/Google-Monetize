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

// RawSimilarWebData 从JSON文件读取的原始数据结构
type RawSimilarWebData struct {
	GlobalRank struct {
		Rank int `json:"Rank"`
	} `json:"GlobalRank"`
	CategoryRank struct {
		Rank     string `json:"Rank"`
		Category string `json:"Category"`
	} `json:"CategoryRank"`
	Engagments struct {
		BounceRate   string `json:"BounceRate"`
		PagePerVisit string `json:"PagePerVisit"`
		Visits       string `json:"Visits"`
		TimeOnSite   string `json:"TimeOnSite"`
	} `json:"Engagments"`
	TrafficSources struct {
		Social        float64 `json:"Social"`
		PaidReferrals float64 `json:"Paid Referrals"`
		Mail          float64 `json:"Mail"`
		Referrals     float64 `json:"Referrals"`
		Search        float64 `json:"Search"`
		Direct        float64 `json:"Direct"`
	} `json:"TrafficSources"`
}

func main() {
	ctx := context.Background()

	// 1. 读取Nike SimilarWeb数据
	fmt.Println("📊 加载Nike真实SimilarWeb数据...")
	dataFile := "nike_similarweb_data.json"
	data, err := os.ReadFile(dataFile)
	if err != nil {
		log.Fatalf("❌ 读取数据文件失败: %v", err)
	}

	var rawData RawSimilarWebData
	if err := json.Unmarshal(data, &rawData); err != nil {
		log.Fatalf("❌ 解析数据失败: %v", err)
	}

	fmt.Printf("✅ 数据已加载\n")
	fmt.Printf("   Global Rank: %d\n", rawData.GlobalRank.Rank)
	fmt.Printf("   Category: %s (Rank: %s)\n", rawData.CategoryRank.Category, rawData.CategoryRank.Rank)
	fmt.Printf("   Monthly Visits: %s\n", rawData.Engagments.Visits)
	fmt.Printf("   Bounce Rate: %s\n", rawData.Engagments.BounceRate)
	fmt.Printf("   Pages/Visit: %s\n", rawData.Engagments.PagePerVisit)
	fmt.Printf("   Direct Traffic: %.1f%%\n\n", rawData.TrafficSources.Direct*100)

	// 2. 转换为内部数据结构
	globalRank := rawData.GlobalRank.Rank
	categoryRankInt := parseCategoryRank(rawData.CategoryRank.Rank)
	totalVisits := parseFloat(rawData.Engagments.Visits)
	bounceRate := parseFloat(rawData.Engagments.BounceRate)
	pagesPerVisit := parseFloat(rawData.Engagments.PagePerVisit)
	avgDuration := parseFloat(rawData.Engagments.TimeOnSite)

	swData := &similarweb.SimilarWebData{
		GlobalRank:   &globalRank,
		CategoryRank: &categoryRankInt,
		Category:     rawData.CategoryRank.Category,
		TotalVisits:  &totalVisits,
		TrafficSources: &similarweb.TrafficSources{
			Direct:    &rawData.TrafficSources.Direct,
			Search:    &rawData.TrafficSources.Search,
			Social:    &rawData.TrafficSources.Social,
			Paid:      &rawData.TrafficSources.PaidReferrals,
			Referrals: &rawData.TrafficSources.Referrals,
		},
		EngagementMetrics: &similarweb.EngagementMetrics{
			BounceRate:       &bounceRate,
			PagesPerVisit:    &pagesPerVisit,
			AvgVisitDuration: &avgDuration,
		},
	}

	// 3. 初始化AI评估服务
	fmt.Println("🤖 初始化Gemini AI服务...")
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GCP_PROJECT_ID")
	}

	service, err := aievaluator.NewService(ctx, projectID)
	if err != nil {
		log.Fatalf("❌ AI服务初始化失败: %v", err)
	}
	defer service.Close()
	fmt.Println("✅ Gemini AI服务已初始化\n")

	// 4. 执行AI评估
	fmt.Println("🔍 开始AI评估...")
	input := &aievaluator.EvaluationInput{
		Domain:         "nike.com",
		BrandName:      "Nike",
		LandingPageURL: "https://www.nike.com",
		SimilarWebData: swData,
	}

	result, err := service.EvaluateOffer(ctx, input)
	if err != nil {
		log.Fatalf("❌ AI评估失败: %v", err)
	}

	// 5. 输出评估结果
	fmt.Println("✅ AI评估完成!\n")
	fmt.Println("=" + repeat("=", 60))
	fmt.Println("🎯 Nike AI评估结果")
	fmt.Println("=" + repeat("=", 60))
	fmt.Printf("\n📊 推荐指数: %d/100\n", result.RecommendationScore)

	fmt.Println("\n💡 推荐理由:")
	for i, reason := range result.Reasons {
		fmt.Printf("   %d. %s\n", i+1, reason)
	}

	fmt.Printf("\n🏢 行业分类: %s\n", result.Industry)

	if result.TrafficInsights != nil {
		fmt.Println("\n📈 流量洞察:")
		if summary, ok := result.TrafficInsights["summary"].(string); ok {
			fmt.Printf("   总结: %s\n", summary)
		}
		if quality, ok := result.TrafficInsights["quality"].(string); ok {
			fmt.Printf("   质量: %s\n", quality)
		}
		if keyMetric, ok := result.TrafficInsights["keyMetric"].(string); ok {
			fmt.Printf("   关键指标: %s\n", keyMetric)
		}
	}

	if result.AdInsights != nil {
		fmt.Println("\n🎯 广告策略:")
		if channels, ok := result.AdInsights["bestChannels"].([]interface{}); ok {
			fmt.Println("   推荐渠道:")
			for _, ch := range channels {
				fmt.Printf("      • %s\n", ch)
			}
		}
		if cpc, ok := result.AdInsights["estimatedCPC"].(string); ok {
			fmt.Printf("   预估CPC: %s\n", cpc)
		}
		if potential, ok := result.AdInsights["conversionPotential"].(string); ok {
			fmt.Printf("   转化潜力: %s\n", potential)
		}
	}

	fmt.Println("\n" + repeat("=", 60))

	// 6. 保存结果
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	outputFile := "nike_ai_evaluation_result.json"
	os.WriteFile(outputFile, resultJSON, 0644)
	fmt.Printf("\n💾 评估结果已保存: %s\n", outputFile)
}

func parseCategoryRank(s string) int {
	var rank int
	fmt.Sscanf(s, "%d", &rank)
	return rank
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
