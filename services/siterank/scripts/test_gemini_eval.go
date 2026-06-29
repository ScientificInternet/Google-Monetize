package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/vertexai/genai"
	"google.golang.org/api/option"
)

// SimilarWebData 从JSON文件读取的数据结构
type SimilarWebData struct {
	GlobalRank struct {
		Rank int `json:"Rank"`
	} `json:"GlobalRank"`
	CategoryRank *struct {
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

// AIEvaluationResult AI评估结果
type AIEvaluationResult struct {
	RecommendationScore int                    `json:"recommendationScore"`
	Reasons             []string               `json:"reasons"`
	Industry            string                 `json:"industry"`
	TrafficInsights     map[string]interface{} `json:"trafficInsights"`
	AdInsights          map[string]interface{} `json:"adInsights"`
}

func main() {
	ctx := context.Background()

	// 1. 读取SimilarWeb数据
	dataFile := "nike_similarweb_data.json"
	data, err := os.ReadFile(dataFile)
	if err != nil {
		log.Fatalf("读取数据文件失败: %v", err)
	}

	var swData SimilarWebData
	if err := json.Unmarshal(data, &swData); err != nil {
		log.Fatalf("解析数据失败: %v", err)
	}

	fmt.Println("✅ 已加载Nike SimilarWeb数据")
	fmt.Printf("Global Rank: %d\n", swData.GlobalRank.Rank)
	fmt.Printf("Monthly Visits: %s\n", swData.Engagments.Visits)
	fmt.Printf("Bounce Rate: %s\n", swData.Engagments.BounceRate)
	fmt.Printf("Pages/Visit: %s\n", swData.Engagments.PagePerVisit)
	fmt.Println()

	// 2. 初始化Vertex AI客户端
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		log.Fatal("GCP_PROJECT_ID环境变量未设置")
	}

	location := "asia-northeast1"
	client, err := genai.NewClient(ctx, projectID, location, option.WithCredentialsFile(""))
	if err != nil {
		log.Fatalf("初始化Vertex AI客户端失败: %v", err)
	}
	defer client.Close()

	fmt.Println("✅ Vertex AI客户端初始化成功")
	fmt.Println()

	// 3. 构建prompt
	prompt := buildPrompt("nike.com", "Nike", "https://www.nike.com", &swData)

	fmt.Println("📝 生成的Prompt:")
	fmt.Println("----------------------------------------")
	fmt.Println(prompt[:1000] + "...")
	fmt.Println("----------------------------------------")
	fmt.Println()

	// 4. 调用Gemini API
	fmt.Println("🤖 正在调用Gemini API评估...")
	model := client.GenerativeModel("gemini-1.5-flash")
	model.SetTemperature(0.7)
	model.SetTopP(0.95)
	model.SetTopK(40)
	model.SetMaxOutputTokens(2048)
	model.ResponseMIMEType = "application/json"

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		log.Fatalf("Gemini API调用失败: %v", err)
	}

	if resp == nil || len(resp.Candidates) == 0 {
		log.Fatal("Gemini API未返回结果")
	}

	// 5. 解析响应
	var responseText string
	for _, part := range resp.Candidates[0].Content.Parts {
		if textPart, ok := part.(genai.Text); ok {
			responseText += string(textPart)
		}
	}

	fmt.Println("✅ Gemini API响应:")
	fmt.Println("----------------------------------------")
	fmt.Println(responseText)
	fmt.Println("----------------------------------------")
	fmt.Println()

	// 6. 解析JSON结果
	var result AIEvaluationResult
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		log.Fatalf("解析AI评估结果失败: %v", err)
	}

	// 7. 输出评估结果
	fmt.Println("🎯 AI评估结果:")
	fmt.Println("========================================")
	fmt.Printf("推荐指数: %d/100\n", result.RecommendationScore)
	fmt.Println("\n推荐理由:")
	for i, reason := range result.Reasons {
		fmt.Printf("  %d. %s\n", i+1, reason)
	}
	fmt.Printf("\n行业分类: %s\n", result.Industry)

	if insights, ok := result.TrafficInsights["summary"].(string); ok {
		fmt.Printf("\n流量洞察: %s\n", insights)
	}

	if channels, ok := result.AdInsights["bestChannels"].([]interface{}); ok {
		fmt.Println("\n推荐广告渠道:")
		for _, ch := range channels {
			fmt.Printf("  - %s\n", ch)
		}
	}

	if cpc, ok := result.AdInsights["estimatedCPC"].(string); ok {
		fmt.Printf("\n预估CPC: %s\n", cpc)
	}

	fmt.Println("========================================")

	// 8. 保存结果到文件
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	os.WriteFile("nike_ai_evaluation_result.json", resultJSON, 0644)
	fmt.Println("\n✅ 评估结果已保存到: nike_ai_evaluation_result.json")
}

func buildPrompt(domain, brandName, landingPageURL string, swData *SimilarWebData) string {
	prompt := fmt.Sprintf(`You are a professional advertising performance analyst with 10+ years of experience in evaluating digital advertising opportunities. Your task is to analyze this offer and provide actionable insights for advertisers.

# Offer Information
- Domain: %s
- Brand: %s
- Landing Page: %s

`, domain, brandName, landingPageURL)

	// 添加SimilarWeb数据
	prompt += "# Traffic & Engagement Data (SimilarWeb)\n\n"

	// Global Rank
	globalRank := swData.GlobalRank.Rank
	prompt += fmt.Sprintf("**Global Rank:** #%d\n", globalRank)
	if globalRank < 10000 {
		prompt += "  ↳ Top-tier traffic authority (excellent brand recognition)\n"
	} else if globalRank < 100000 {
		prompt += "  ↳ Strong traffic authority (good brand presence)\n"
	} else if globalRank < 1000000 {
		prompt += "  ↳ Moderate traffic authority\n"
	} else {
		prompt += "  ↳ Lower traffic authority (niche or emerging brand)\n"
	}

	// Category Rank
	if swData.CategoryRank != nil {
		prompt += fmt.Sprintf("**Category:** %s (Rank: #%s)\n", swData.CategoryRank.Category, swData.CategoryRank.Rank)
	}

	// Monthly Traffic
	prompt += fmt.Sprintf("\n**Monthly Visits:** %s\n", swData.Engagments.Visits)

	// Traffic Sources
	prompt += "\n**Traffic Sources:**\n"
	ts := swData.TrafficSources
	prompt += fmt.Sprintf("  • Direct: %.1f%% (brand loyalty indicator)\n", ts.Direct*100)
	prompt += fmt.Sprintf("  • Search: %.1f%% (organic discovery)\n", ts.Search*100)
	prompt += fmt.Sprintf("  • Social: %.1f%% (viral potential)\n", ts.Social*100)
	prompt += fmt.Sprintf("  • Paid Ads: %.1f%% (existing ad spend)\n", ts.PaidReferrals*100)
	prompt += fmt.Sprintf("  • Referral: %.1f%% (partnership ecosystem)\n", ts.Referrals*100)

	// Engagement Quality
	prompt += "\n**Engagement Quality:**\n"
	prompt += fmt.Sprintf("  • Bounce Rate: %s%%\n", swData.Engagments.BounceRate)
	prompt += fmt.Sprintf("  • Pages/Visit: %s\n", swData.Engagments.PagePerVisit)
	prompt += fmt.Sprintf("  • Avg Duration: %ss\n", swData.Engagments.TimeOnSite)

	prompt += `

# Your Analysis Task

Evaluate this offer's advertising potential across these dimensions:

1. **Recommendation Score (0-100)**
   - Consider: traffic volume, engagement quality, brand authority, monetization potential
   - 80-100: Highly recommended (premium opportunity)
   - 60-79: Recommended (solid opportunity)
   - 40-59: Conditional (requires optimization)
   - 0-39: Not recommended (high risk)

2. **Top 3 Reasons** (be specific and data-driven)
   - Each reason should reference specific metrics
   - Focus on actionable insights, not generic statements
   - Example: "Low 35% bounce rate indicates engaged audience ready to convert"

3. **Industry Classification**
   - Provide the most specific industry/vertical (e.g., "E-commerce - Fashion & Apparel")

4. **Traffic Insights**
   - Analyze traffic patterns, quality signals, and conversion indicators
   - Highlight any red flags or exceptional opportunities

5. **Ad Strategy Recommendations**
   - Suggest best advertising channels based on traffic sources
   - Estimate CPC range based on industry and competition
   - Assess conversion potential based on engagement metrics

# Output Format (strict JSON)

{
  "recommendationScore": <integer 0-100>,
  "reasons": [
    "<specific reason 1 with data reference>",
    "<specific reason 2 with data reference>",
    "<specific reason 3 with data reference>"
  ],
  "industry": "<specific industry/vertical>",
  "trafficInsights": {
    "summary": "<concise traffic pattern analysis in 1-2 sentences>",
    "quality": "<high|medium|low>",
    "keyMetric": "<most important metric and its implication>"
  },
  "adInsights": {
    "bestChannels": ["<channel 1>", "<channel 2>", "<channel 3>"],
    "estimatedCPC": "<realistic CPC range like $0.50-$1.20>",
    "conversionPotential": "<high|medium|low>"
  }
}

Provide your analysis now:`

	return prompt
}
