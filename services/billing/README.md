# Billing 服务

## 概述

Billing 服务是 gm 项目的核心计费系统，负责 Token 余额管理、订阅管理和计费计划管理。本服务实现了 **两阶段提交 (Two-Phase Commit)** 模式来保证 Token 交易的一致性，是项目中事务处理的典范。

> 最后更新: 2025-10-23 重新构建测试

### 核心功能

- ✅ **Token 余额管理**: 用户积分系统
- ✅ **两阶段提交**: Reserve → Commit/Release 保证一致性
- ✅ **订阅管理**: 试用、激活、取消
- ✅ **计费计划**: 多种计费方案
- ✅ **交易记录**: 完整的审计日志
- ✅ **事件驱动**: 订阅事件投影
- ✅ **Onboarding 集成**: 新用户流程
- ✅ **Token 修复**: 审计和修复工具
- ✅ **应急模式**: BILLING_MINIMAL 支持

---

## 两阶段提交机制

### 为什么需要两阶段提交？

在分布式系统中，Token 消耗操作需要保证：
1. **原子性**: 要么全部成功，要么全部失败
2. **一致性**: Token 余额始终准确
3. **隔离性**: 并发操作不会冲突
4. **持久性**: 交易记录永久保存

### 两阶段提交流程

```
Phase 1: Reserve (预留)
┌─────────────────────────────────────┐
│ 1. 客户端请求预留 100 tokens        │
│ 2. 检查余额是否充足                 │
│ 3. 创建 pending 交易记录            │
│ 4. 返回 txId                        │
└─────────────────────────────────────┘
              ↓
        执行任务 (adscenter, browser-exec, etc.)
              ↓
Phase 2a: Commit (提交) - 任务成功
┌─────────────────────────────────────┐
│ 1. 客户端提交 txId                  │
│ 2. 验证 txId 有效性                │
│ 3. 扣除 tokens                      │
│ 4. 更新交易状态为 committed         │
│ 5. 返回新余额                       │
└─────────────────────────────────────┘

Phase 2b: Release (释放) - 任务失败
┌─────────────────────────────────────┐
│ 1. 客户端释放 txId                  │
│ 2. 验证 txId 有效性                │
│ 3. 取消交易                         │
│ 4. 更新交易状态为 released          │
│ 5. 返回确认                         │
└─────────────────────────────────────┘
```

### API 示例

#### 1. Reserve (预留)

```bash
POST /api/v1/billing/tokens/reserve
Authorization: Bearer <jwt_token>
Content-Type: application/json

{
  "amount": 100,
  "taskId": "task-123",
  "description": "Bulk ad operation"
}

Response 202:
{
  "txId": "tx-456",
  "status": "reserved",
  "reservedAt": "2025-10-08T10:00:00Z"
}
```

#### 2a. Commit (提交 - 任务成功)

```bash
POST /api/v1/billing/tokens/commit
Authorization: Bearer <jwt_token>
Content-Type: application/json

{
  "txId": "tx-456",
  "amount": 100,
  "taskId": "task-123"
}

Response 200:
{
  "txId": "tx-456",
  "debitId": "debit-789",
  "status": "committed",
  "balance": 900,
  "committedAt": "2025-10-08T10:05:00Z"
}
```

#### 2b. Release (释放 - 任务失败)

```bash
POST /api/v1/billing/tokens/release
Authorization: Bearer <jwt_token>
Content-Type: application/json

{
  "txId": "tx-456",
  "amount": 100,
  "taskId": "task-123",
  "reason": "Task failed"
}

Response 200:
{
  "txId": "tx-456",
  "status": "released",
  "releasedAt": "2025-10-08T10:05:00Z"
}
```

### 错误处理

```bash
# 余额不足
Response 400:
{
  "error": "insufficient_funds",
  "message": "Insufficient token balance",
  "required": 100,
  "available": 50
}

# 无效的 txId
Response 400:
{
  "error": "invalid_transaction",
  "message": "Transaction not found or already processed"
}

# 重复提交
Response 409:
{
  "error": "transaction_already_committed",
  "message": "Transaction has already been committed"
}
```

---

## 技术栈

- **语言**: Go 1.25.1
- **框架**: Chi Router
- **数据库**: Cloud SQL PostgreSQL (pgx/v5)
- **缓存**: Redis (gm-redis)
- **消息队列**: Pub/Sub
- **部署**: GCP Cloud Run (asia-northeast1)
- **认证**: Supabase JWT
- **事务模式**: 两阶段提交

---

## 本地开发

### 前置条件

- Go 1.25+
- Docker (可选)
- GCP 服务账号密钥: `secrets/gcp_codex_dev.json`
- 访问 Secret Manager 的权限

### 环境变量

```bash
# 数据库
DATABASE_URL=postgresql://...

# Redis
REDIS_URL=redis://...

# Pub/Sub
PUBSUB_PROJECT_ID=${GCP_PROJECT_ID}

# Stripe (支付)
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...

# Supabase
SUPABASE_URL=https://jzzvizacfyipzdyiqfzb.supabase.co
SUPABASE_SERVICE_KEY=...

# 应急模式
BILLING_MINIMAL=false  # true 时启用最小模式
```

### 启动服务

```bash
# 进入服务目录
cd services/billing

# 安装依赖
go mod download

# 运行服务
go run main.go
```

### 数据库迁移

Billing 服务使用独立迁移文件 + DB Migrator Job 模式：

```bash
# 1. 构建 migrator 镜像
gcloud builds submit \
  --config=deployments/cloudbuild/build-migrator.yaml \
  --substitutions=_SERVICE=billing,_ENV=preview

# 2. 执行迁移
gcloud run jobs execute db-migrator-preview \
  --region=asia-northeast1 --wait

# 3. 启动服务（跳过内嵌迁移）
export BILLING_SKIP_MIGRATIONS=1
go run main.go
```

**迁移文件位置**: `internal/migrations/*.sql`

**数据库表**:
- `User` - 用户表
- `Subscription` - 订阅管理
- `UserToken` - Token 余额
- `TokenTransaction` - Token 交易记录
- `UserTokenPool` - Token 池
- `TokenCreditLot` - 积分批次
- `TokenCreditAllocation` - 积分分配
- `TokenRepairAudit` - 修复审计

---

## API 端点

### 认证

所有 API 端点需要 Supabase JWT Token：

```bash
Authorization: Bearer <supabase_jwt_token>
```

### Token 管理

```bash
# 获取 Token 余额
GET /api/v1/billing/tokens/balance

# 预留 Tokens
POST /api/v1/billing/tokens/reserve

# 提交 Tokens
POST /api/v1/billing/tokens/commit

# 释放 Tokens
POST /api/v1/billing/tokens/release

# 列出交易记录
GET /api/v1/billing/tokens/transactions
```

### 订阅管理

```bash
# 获取订阅信息
GET /api/v1/billing/subscriptions

# 创建订阅
POST /api/v1/billing/subscriptions

# 更新订阅
PUT /api/v1/billing/subscriptions/{id}

# 取消订阅
DELETE /api/v1/billing/subscriptions/{id}
```

### 计费计划

```bash
# 获取计费计划
GET /api/v1/billing/plans
```

### 健康检查

```bash
# 健康检查
GET /health
GET /healthz

# 就绪检查
GET /readyz

# Prometheus 指标
GET /metrics
```

完整 API 文档请参考 `openapi.yaml`。

---

## 应急模式 (BILLING_MINIMAL)

### 什么是应急模式？

当 billing 服务出现问题时，可以启用应急模式来保证系统可用性：

```bash
# 启用应急模式
export BILLING_MINIMAL=true
```

### 应急模式行为

- ✅ **跳过 Token 检查**: 所有操作都允许
- ✅ **记录操作日志**: 用于事后审计
- ⚠️ **不扣除 Tokens**: 需要事后补扣
- ⚠️ **仅用于紧急情况**: 不应长期使用

### 应急模式恢复

1. 修复 billing 服务问题
2. 关闭应急模式: `BILLING_MINIMAL=false`
3. 运行 Token 修复工具补扣 Tokens
4. 审计修复结果

---

## 安全注意事项

### Token 交易安全

1. **幂等性**: 使用 `taskId` 防止重复扣费
2. **原子性**: 两阶段提交保证一致性
3. **审计**: 所有交易记录永久保存
4. **隔离**: 用户资源严格隔离

### 防止滥用

```go
// 速率限制
- Reserve: 100 次/分钟/用户
- Commit: 100 次/分钟/用户
- Release: 100 次/分钟/用户

// 金额限制
- 单次预留: 最大 10,000 tokens
- 单次提交: 必须匹配预留金额
```

### 审计日志

所有 Token 操作都记录在 `TokenTransaction` 表中：

```sql
SELECT 
    id,
    user_id,
    amount,
    type,  -- reserve, commit, release
    status,
    task_id,
    created_at
FROM token_transactions
WHERE user_id = 'user-123'
ORDER BY created_at DESC;
```

---

## 配置说明

### Token 配置

```yaml
# config.yaml
tokens:
  initial_balance: 1000      # 新用户初始余额
  trial_tokens: 500          # 试用期 tokens
  reserve_timeout: 3600      # 预留超时（秒）
```

### 订阅配置

```yaml
subscriptions:
  trial_period_days: 14      # 试用期天数
  grace_period_days: 3       # 宽限期天数
```

---

## 部署

### Preview 环境

```bash
# 推送到 main 分支自动触发部署
git push origin main

# 服务名: billing-preview
```

### 生产环境

```bash
# 推送到 production 分支自动触发部署
git push origin production

# 服务名: billing
```

---

## 故障排查

### 常见问题

#### 1. Token 余额不一致

```bash
# 检查交易记录
psql $DATABASE_URL -c "
SELECT 
    SUM(CASE WHEN type = 'credit' THEN amount ELSE -amount END) as calculated_balance,
    (SELECT balance FROM user_tokens WHERE user_id = 'user-123') as stored_balance
FROM token_transactions
WHERE user_id = 'user-123';
"

# 运行修复工具
go run cmd/repair-tokens/main.go --user-id=user-123
```

#### 2. 交易卡在 pending 状态

```bash
# 查找超时的预留
psql $DATABASE_URL -c "
SELECT * FROM token_transactions
WHERE status = 'pending'
  AND created_at < NOW() - INTERVAL '1 hour';
"

# 自动释放超时预留
go run cmd/cleanup-pending/main.go
```

#### 3. 两阶段提交失败

```bash
# 检查日志
gcloud run services logs read billing-preview \
  --filter="textPayload:two-phase-commit" \
  --limit=100

# 常见原因
- 网络超时
- 数据库连接失败
- 并发冲突
```

#### 4. 应急模式未生效

```bash
# 检查环境变量
gcloud run services describe billing-preview \
  --region=asia-northeast1 \
  --format="value(spec.template.spec.containers[0].env)"

# 确认 BILLING_MINIMAL=true
```

---

## 开发指南

### 代码结构

```
services/billing/
├── cmd/
│   ├── migrator/          # 数据库迁移工具
│   └── server/            # 服务器入口
├── internal/
│   ├── config/            # 配置管理
│   ├── domain/            # 领域模型
│   │   ├── subscription.go
│   │   └── subscription_test.go
│   ├── events/            # 事件基础设施
│   ├── handlers/          # HTTP 处理器
│   │   ├── http.go
│   │   ├── tokens.go
│   │   └── token_reservation.go
│   ├── migrations/        # SQL 迁移文件 ⭐
│   ├── projectors/        # 事件投影器
│   └── tokens/            # Token 服务 ⭐
│       └── service.go
├── main.go                # 主入口
└── openapi.yaml           # API 规范
```

### 添加新功能

#### 示例：添加 Token 转账功能

```go
// 1. 在 internal/tokens/service.go 中添加方法
func (s *Service) Transfer(ctx context.Context, fromUserID, toUserID string, amount int) error {
    // 两阶段提交
    // 1. Reserve from sender
    // 2. Credit to receiver
    // 3. Commit both
}

// 2. 在 internal/handlers/tokens.go 中添加处理器
func (h *Handler) HandleTransfer(w http.ResponseWriter, r *http.Request) {
    // 解析请求
    // 调用 service.Transfer
    // 返回响应
}

// 3. 在 openapi.yaml 中定义 API
// 4. 编写测试
// 5. 更新文档
```

### 测试

```bash
# 运行所有测试
go test ./...

# 运行 Token Service 测试
go test ./internal/tokens/...

# 生成覆盖率报告
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### 代码规范

- **事务处理**: 必须使用两阶段提交
- **幂等性**: 所有操作必须幂等
- **审计**: 所有 Token 操作必须记录
- **错误处理**: 明确区分业务错误和系统错误
- **测试**: Token Service 测试覆盖率 >80%

---

## 监控和告警

### Prometheus 指标

- `tokens_reserved_total`: Token 预留总数
- `tokens_committed_total`: Token 提交总数
- `tokens_released_total`: Token 释放总数
- `token_balance_gauge`: Token 余额
- `subscription_active_gauge`: 活跃订阅数

### 告警规则

- Token 余额异常: 负数或超大值
- 预留超时率: >5%
- 提交失败率: >1%
- 应急模式启用: 立即告警

---

## 架构亮点

### 为什么两阶段提交是典范？

1. **保证一致性**
   - 原子性: 要么全部成功，要么全部失败
   - 隔离性: 并发操作不会冲突
   - 持久性: 交易记录永久保存

2. **容错性强**
   - 任务失败时自动释放
   - 超时自动清理
   - 应急模式保证可用性

3. **审计完整**
   - 所有交易有记录
   - 支持事后审计
   - 修复工具完善

4. **易于扩展**
   - 支持多种 Token 类型
   - 支持复杂计费规则
   - 支持订阅管理

### 其他服务应该学习什么？

- ✅ 两阶段提交模式
- ✅ 幂等性设计
- ✅ 审计日志设计
- ✅ 应急模式设计

---

## 贡献指南

### 提交代码

1. 创建功能分支
2. 编写代码和测试
3. 确保测试覆盖率 >80%
4. 运行 `go test ./...`
5. 创建 Pull Request

### 提交信息规范

- `feat(tokens)`: Token 相关功能
- `feat(subscription)`: 订阅相关功能
- `fix(two-phase-commit)`: 两阶段提交修复
- `refactor`: 代码重构
- `test`: 测试相关

---

## 相关资源

- [OpenAPI 规范](./openapi.yaml)
- [架构分析报告](../../docs/ArchitectureReviewV1/billing-analysis.md)
- [两阶段提交模式](https://en.wikipedia.org/wiki/Two-phase_commit_protocol)
- [数据库迁移指南](../../docs/SupabaseGo/MustKnowV6.md)

---

**最后更新**: 2025-10-08  
**维护者**: 后端团队  
**事务模式**: 两阶段提交  
**状态**: ✅ 生产就绪 | 🌟 事务处理典范
