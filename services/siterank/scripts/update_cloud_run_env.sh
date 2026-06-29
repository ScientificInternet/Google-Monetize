#!/bin/bash
#
# 更新Cloud Run服务的环境变量
# 从Secret Manager读取GEMINI_API_KEY并配置到siterank服务
#

set -e

PROJECT_ID="${GCP_PROJECT_ID}"
REGION="asia-northeast1"
SERVICE_NAME="siterank-preview"
SECRET_NAME="GEMINI_API_KEY"

echo "📝 更新 ${SERVICE_NAME} 服务的环境变量..."

# 更新Cloud Run服务,添加secret环境变量
gcloud run services update ${SERVICE_NAME} \
  --region=${REGION} \
  --update-secrets=GEMINI_API_KEY=${SECRET_NAME}:latest \
  --quiet

echo "✅ 环境变量已更新!"
echo ""
echo "验证配置:"
gcloud run services describe ${SERVICE_NAME} \
  --region=${REGION} \
  --format="value(spec.template.spec.containers[0].env)"

echo ""
echo "🎉 完成! GEMINI_API_KEY 已配置到 ${SERVICE_NAME} 服务"
