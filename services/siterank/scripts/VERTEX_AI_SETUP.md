# Vertex AI Gemini API 配置说明

## 技术方案

Siterank服务使用 **Vertex AI Gemini API** 进行AI评估,而非Gemini Developer API。

### 选择原因

1. **地区支持**: Gemini Developer API在中国地区不可用,Vertex AI无地区限制
2. **企业级**: Vertex AI是GCP的企业级AI平台,更适合生产环境
3. **统一认证**: 使用GCP服务账号,无需额外管理API key
4. **本地测试一致性**: 本地和生产环境使用相同的技术方案

## 配置要求

### 1. 启用API

```bash
gcloud services enable aiplatform.googleapis.com --project=${GCP_PROJECT_ID}
```

### 2. 服务账号权限

Cloud Run服务账号需要以下权限:
- `roles/aiplatform.user` - 调用Vertex AI API

```bash
gcloud projects add-iam-policy-binding ${GCP_PROJECT_ID} \
  --member="serviceAccount:codex-dev@${GCP_PROJECT_ID}.iam.gserviceaccount.com" \
  --role="roles/aiplatform.user"
```

### 3. 本地开发配置

本地开发需要配置Application Default Credentials (ADC):

```bash
# 方式1: 使用用户凭证
gcloud auth application-default login

# 方式2: 使用服务账号密钥(仅限测试)
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account-key.json"
```

## 代码实现

### 初始化客户端

```go
import "cloud.google.com/go/vertexai/genai"

// 使用ADC自动认证
client, err := genai.NewClient(ctx, projectID, "asia-northeast1")
```

### 使用的模型

- **模型名称**: `gemini-1.5-flash-002`
- **区域**: `asia-northeast1`
- **配置**:
  - Temperature: 0.7
  - TopP: 0.95
  - TopK: 40
  - MaxOutputTokens: 2048
  - ResponseMIMEType: "application/json"

## 测试验证

### 本地测试

```bash
cd /Users/jason/Documents/Kiro/gm/services/siterank/scripts
export GCP_PROJECT_ID="${GCP_PROJECT_ID}"
go run test_gemini_eval_real.go
```

### Cloud Run测试

部署后访问评估API:
```bash
SERVICE_URL="https://siterank-preview-644672509127.asia-northeast1.run.app"
curl -X POST "${SERVICE_URL}/api/v1/evaluations" \
  -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  -H "Content-Type: application/json" \
  -d '{"domain":"nike.com","brand_name":"Nike"}'
```

## 监控

Vertex AI API调用会自动记录到Cloud Logging:
```bash
gcloud logging read "resource.type=aiplatform.googleapis.com/Endpoint" \
  --limit=50 \
  --format=json
```

## 成本

- Vertex AI Gemini 1.5 Flash 定价: https://cloud.google.com/vertex-ai/pricing
- 输入: ~$0.00125 / 1K tokens
- 输出: ~$0.00375 / 1K tokens

## 故障排查

### 错误: "Permission denied"

检查服务账号权限:
```bash
gcloud projects get-iam-policy ${GCP_PROJECT_ID} \
  --flatten="bindings[].members" \
  --filter="bindings.members:serviceAccount:codex-dev@*"
```

### 错误: "Model not found"

确认使用正确的模型名称和区域:
- 模型: `gemini-1.5-flash-002` (不是 `models/gemini-1.5-flash`)
- 区域: `asia-northeast1`

### 本地测试失败

确认ADC配置:
```bash
gcloud auth application-default print-access-token
```

## 参考文档

- [Vertex AI Gemini API](https://cloud.google.com/vertex-ai/docs/generative-ai/model-reference/gemini)
- [Go Client Library](https://pkg.go.dev/cloud.google.com/go/vertexai/genai)
- [Authentication](https://cloud.google.com/docs/authentication/application-default-credentials)
