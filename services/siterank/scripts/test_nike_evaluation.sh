#!/bin/bash
# Nike.com AI评估完整测试Case
# 用途: 测试AI评估功能的所有维度分析能力

set -e

# 配置
DOMAIN="nike.com"
FIREBASE_TOKEN="${FIREBASE_TOKEN:-}"
SITERANK_API="${SITERANK_API:-https://siterank-preview-885pd7lz.a.run.app}"

echo "=========================================="
echo "Nike.com AI评估测试 - v2.1"
echo "=========================================="

# Step 1: 获取SimilarWeb原始数据
echo -e "\n[Step 1] 获取nike.com的SimilarWeb数据..."
SIMILARWEB_DATA=$(curl -s "https://data.similarweb.com/api/v1/data?domain=nike.com")

echo "SimilarWeb数据预览:"
echo "$SIMILARWEB_DATA" | jq '{
  GlobalRank: .GlobalRank.Rank,
  CategoryRank: .CategoryRank.Rank,
  Category: .Category,
  MonthlyVisits: .Engagments.Visits,
  BounceRate: .Engagments.BounceRate,
  TimeOnSite: .Engagments.TimeOnSite,
  PagePerVisit: .Engagments.PagePerVisit,
  TopCountries: .TopCountryShares[0:3],
  TrafficSources: .TrafficSources
}'

# Step 2: 创建Offer (如果不存在)
echo -e "\n[Step 2] 创建或获取Nike.com Offer..."
if [ -z "$FIREBASE_TOKEN" ]; then
  echo "❌ 错误: 需要设置FIREBASE_TOKEN环境变量"
  echo "获取方式: 在浏览器登录后，从开发者工具的Network面板中复制Authorization header的Bearer token"
  exit 1
fi

# 查找现有offer
EXISTING_OFFER=$(curl -s -X GET \
  "$SITERANK_API/api/v1/offers?domain=nike.com" \
  -H "Authorization: Bearer $FIREBASE_TOKEN" | jq -r '.[0].id // empty')

if [ -n "$EXISTING_OFFER" ]; then
  OFFER_ID="$EXISTING_OFFER"
  echo "✅ 找到现有Offer: $OFFER_ID"
else
  # 创建新offer
  CREATE_RESPONSE=$(curl -s -X POST \
    "$SITERANK_API/api/v1/offers" \
    -H "Authorization: Bearer $FIREBASE_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{
      "domain": "nike.com",
      "brandName": "Nike",
      "landingPageURL": "https://www.nike.com"
    }')

  OFFER_ID=$(echo "$CREATE_RESPONSE" | jq -r '.id')
  echo "✅ 创建新Offer: $OFFER_ID"
fi

# Step 3: 触发AI评估
echo -e "\n[Step 3] 触发AI评估 (包含所有新维度)..."
EVAL_RESPONSE=$(curl -s -X POST \
  "$SITERANK_API/api/v1/offers/$OFFER_ID/evaluate" \
  -H "Authorization: Bearer $FIREBASE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "includeAI": true,
    "forceRefresh": true
  }')

EVALUATION_ID=$(echo "$EVAL_RESPONSE" | jq -r '.evaluationId // .id')
echo "✅ 评估任务已创建: $EVALUATION_ID"
echo "状态: $(echo "$EVAL_RESPONSE" | jq -r '.status')"

# Step 4: 等待评估完成
echo -e "\n[Step 4] 等待评估完成 (预计60秒)..."
MAX_WAIT=120
ELAPSED=0
STATUS="pending"

while [ "$STATUS" != "completed" ] && [ $ELAPSED -lt $MAX_WAIT ]; do
  sleep 5
  ELAPSED=$((ELAPSED + 5))

  EVAL_STATUS=$(curl -s -X GET \
    "$SITERANK_API/api/v1/evaluations/$EVALUATION_ID" \
    -H "Authorization: Bearer $FIREBASE_TOKEN")

  STATUS=$(echo "$EVAL_STATUS" | jq -r '.status')
  echo "  [$ELAPSED s] 状态: $STATUS"

  if [ "$STATUS" = "failed" ]; then
    echo "❌ 评估失败:"
    echo "$EVAL_STATUS" | jq '.error'
    exit 1
  fi
done

if [ "$STATUS" != "completed" ]; then
  echo "❌ 评估超时 (${MAX_WAIT}秒)"
  exit 1
fi

# Step 5: 获取完整评估结果
echo -e "\n[Step 5] 获取AI评估结果..."
FINAL_RESULT=$(curl -s -X GET \
  "$SITERANK_API/api/v1/evaluations/$EVALUATION_ID" \
  -H "Authorization: Bearer $FIREBASE_TOKEN")

echo "=========================================="
echo "AI评估结果分析"
echo "=========================================="

# 提取各个维度
SCORE=$(echo "$FINAL_RESULT" | jq -r '.aiRecommendationScore')
REASONS=$(echo "$FINAL_RESULT" | jq -r '.aiRecommendationReasons[]')
INDUSTRY=$(echo "$FINAL_RESULT" | jq -r '.aiIndustry')
PRODUCT_TYPE=$(echo "$FINAL_RESULT" | jq -r '.aiProductType // "未分析"')
ESTIMATED_AOV=$(echo "$FINAL_RESULT" | jq -r '.aiEstimatedAOV // "未分析"')

echo -e "\n📊 推荐指数: $SCORE/100"
echo -e "\n📝 推荐理由:"
echo "$REASONS" | nl -w2 -s'. '

echo -e "\n🏭 所属行业: $INDUSTRY"
echo -e "📦 产品类型: $PRODUCT_TYPE"
echo -e "💰 平均客单价: $ESTIMATED_AOV"

echo -e "\n📈 流量洞察:"
echo "$FINAL_RESULT" | jq '.aiTrafficInsights'

echo -e "\n🔍 搜索意图分析:"
echo "$FINAL_RESULT" | jq '.aiSearchInsights'

echo -e "\n🌍 地理市场分析:"
echo "$FINAL_RESULT" | jq '.aiGeoInsights'

echo -e "\n💡 广告策略:"
echo "$FINAL_RESULT" | jq '.aiAdInsights'

echo -e "\n⚠️  投放风险评估:"
echo "$FINAL_RESULT" | jq '.aiRiskAssessment'

# Step 6: 验证关键维度
echo -e "\n=========================================="
echo "维度完整性验证"
echo "=========================================="

check_field() {
  local field=$1
  local name=$2
  local value=$(echo "$FINAL_RESULT" | jq -r "$field")

  if [ "$value" != "null" ] && [ -n "$value" ] && [ "$value" != "未分析" ]; then
    echo "✅ $name: 已提供"
  else
    echo "❌ $name: 缺失"
  fi
}

check_field '.aiProductType' '产品类型'
check_field '.aiEstimatedAOV' '平均客单价'
check_field '.aiSearchInsights.brandVsNonBrand' '品牌词/非品牌词比例'
check_field '.aiSearchInsights.dominantIntent' '用户搜索意图'
check_field '.aiGeoInsights.topMarkets' '投放国家推荐'
check_field '.aiGeoInsights.concentration' '地理分布集中度'
check_field '.aiAdInsights.estimatedCPC' '同类产品CPC估算'
check_field '.aiRiskAssessment.policyCompliance' 'Google Ads政策合规性'
check_field '.aiRiskAssessment.prohibitedCategories' '禁止类别检查'

# 保存完整结果
OUTPUT_FILE="nike_evaluation_result_$(date +%Y%m%d_%H%M%S).json"
echo "$FINAL_RESULT" | jq '.' > "$OUTPUT_FILE"
echo -e "\n✅ 完整评估结果已保存到: $OUTPUT_FILE"

echo -e "\n=========================================="
echo "测试完成"
echo "=========================================="
