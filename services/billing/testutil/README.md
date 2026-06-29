# Billing 服务测试工具包

本目录包含 billing 服务的测试工具和辅助函数，专注于两阶段提交机制的测试。

## 文件说明

### fixtures.go

提供测试数据生成器和构建器。

**主要功能**:
- `NewTestUserToken()`: 创建默认测试用户 Token
- `NewTestUserTokenWithBalance(balance)`: 创建指定余额的 Token
- `NewTestTokenTransaction()`: 创建测试交易
- `NewTestReservedTransaction()`: 创建预留状态交易
- `NewTestCommittedTransaction()`: 创建已提交交易
- `NewTestReleasedTransaction()`: 创建已释放交易
- `NewTestSubscription()`: 创建测试订阅
- `NewUserTokenBuilder()`: 创建 Token 构建器
- `NewTokenTransactionBuilder()`: 创建交易构建器

**使用示例**:

```go
// 创建默认测试 Token
token := testutil.NewTestUserToken()

// 创建特定余额的 Token
token := testutil.NewTestUserTokenWithBalance(500)

// 使用构建器创建自定义 Token
token := testutil.NewUserTokenBuilder().
    WithUserID("user-123").
    WithBalance(2000).
    Build()

// 创建预留交易
tx := testutil.NewTestReservedTransaction()

// 使用构建器创建自定义交易
tx := testutil.NewTokenTransactionBuilder().
    WithID("tx-123").
    WithUserID("user-123").
    WithType("deduct").
    WithAmount(-100).
    WithStatus("reserved").
    WithDescription("Test reservation").
    Build()
```

### mocks.go

提供 Mock 对象用于单元测试。

**主要 Mock**:
- `MockTokenService`: Mock Token 服务
- `MockSubscriptionService`: Mock 订阅服务
- `MockEventPublisher`: Mock 事件发布器

**使用示例**:

```go
// Mock Token Service
tokenService := testutil.NewMockTokenService()

// 自定义行为 - 余额不足
tokenService.CheckAndReserveTokensFunc = func(ctx context.Context, userID string, amount int, description string) (string, error) {
    return "", errors.New("insufficient tokens")
}

// 测试
reservationID, err := tokenService.CheckAndReserveTokens(ctx, "user-1", 100, "test")
assert.Error(t, err)

// Mock Event Publisher
publisher := testutil.NewMockEventPublisher()

// 发布事件
publisher.Publish(ctx, event)

// 验证事件
assert.Equal(t, 1, publisher.GetEventCount())
assert.NotNil(t, publisher.GetLastEvent())
```

### database.go

提供测试数据库连接和管理工具。

**主要功能**:
- `NewTestDB(t)`: 创建测试数据库连接
- `SetupTestTables()`: 创建测试表
- `Cleanup()`: 清理测试数据
- `TeardownTestTables()`: 删除测试表
- `InsertTestUser()`: 插入测试用户
- `InsertTestUserToken()`: 插入测试 Token
- `GetTestUserToken()`: 获取测试 Token
- `InsertTestTransaction()`: 插入测试交易
- `GetTestTransaction()`: 获取测试交易

**使用示例**:

```go
func TestTokenService(t *testing.T) {
    // 创建测试数据库
    testDB := testutil.NewTestDB(t)
    defer testDB.Close()
    
    // 设置测试表
    testDB.SetupTestTables()
    defer testDB.TeardownTestTables()
    
    // 清理测试数据
    defer testDB.Cleanup()
    
    // 插入测试数据
    token := testutil.NewTestUserToken()
    err := testDB.InsertTestUserToken(context.Background(), token)
    assert.NoError(t, err)
    
    // 测试 Token Service
    service := tokens.NewService(testDB.Pool)
    reservationID, err := service.CheckAndReserveTokens(ctx, token.UserID, 100, "test")
    assert.NoError(t, err)
    assert.NotEmpty(t, reservationID)
    
    // 验证余额
    updatedToken, err := testDB.GetTestUserToken(ctx, token.UserID)
    assert.NoError(t, err)
    assert.Equal(t, int64(900), updatedToken.Balance)
}
```

## 环境配置

### 测试数据库

设置 `TEST_DATABASE_URL` 环境变量：

```bash
export TEST_DATABASE_URL="postgresql://postgres:postgres@localhost:5432/billing_test?sslmode=disable"
```

### 创建测试数据库

```bash
# 使用 Docker 运行 PostgreSQL
docker run -d --name postgres-test \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  postgres:15

# 创建测试数据库
psql -h localhost -U postgres -c "CREATE DATABASE billing_test;"
```

## 测试两阶段提交

### 测试场景

#### 1. 正常流程

```go
func TestTwoPhaseCommit_Success(t *testing.T) {
    // Arrange
    testDB := testutil.NewTestDB(t)
    defer testDB.Close()
    defer testDB.Cleanup()
    
    token := testutil.NewTestUserTokenWithBalance(1000)
    testDB.InsertTestUserToken(ctx, token)
    
    service := tokens.NewService(testDB.Pool)
    
    // Act - Phase 1: Reserve
    reservationID, err := service.CheckAndReserveTokens(ctx, token.UserID, 100, "test")
    assert.NoError(t, err)
    
    // Assert - Balance deducted
    updatedToken, _ := testDB.GetTestUserToken(ctx, token.UserID)
    assert.Equal(t, int64(900), updatedToken.Balance)
    
    // Act - Phase 2: Commit
    err = service.CommitReservation(ctx, reservationID)
    assert.NoError(t, err)
}
```

#### 2. 余额不足

```go
func TestTwoPhaseCommit_InsufficientBalance(t *testing.T) {
    // Arrange
    token := testutil.NewTestUserTokenWithBalance(50)
    testDB.InsertTestUserToken(ctx, token)
    
    service := tokens.NewService(testDB.Pool)
    
    // Act
    _, err := service.CheckAndReserveTokens(ctx, token.UserID, 100, "test")
    
    // Assert
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "insufficient tokens")
}
```

#### 3. 释放预留

```go
func TestTwoPhaseCommit_Release(t *testing.T) {
    // Arrange
    token := testutil.NewTestUserTokenWithBalance(1000)
    testDB.InsertTestUserToken(ctx, token)
    
    service := tokens.NewService(testDB.Pool)
    
    // Act - Reserve
    reservationID, _ := service.CheckAndReserveTokens(ctx, token.UserID, 100, "test")
    
    // Act - Release
    err := service.ReleaseReservation(ctx, reservationID)
    assert.NoError(t, err)
    
    // Assert - Balance restored
    updatedToken, _ := testDB.GetTestUserToken(ctx, token.UserID)
    assert.Equal(t, int64(1000), updatedToken.Balance)
}
```

## 测试最佳实践

### 1. 测试隔离

每个测试应该独立运行，不依赖其他测试：

```go
func TestTokenService(t *testing.T) {
    t.Run("Reserve", func(t *testing.T) {
        testDB := testutil.NewTestDB(t)
        defer testDB.Close()
        defer testDB.Cleanup()
        
        // 测试逻辑
    })
    
    t.Run("Commit", func(t *testing.T) {
        testDB := testutil.NewTestDB(t)
        defer testDB.Close()
        defer testDB.Cleanup()
        
        // 测试逻辑
    })
}
```

### 2. 并发测试

测试两阶段提交的并发安全性：

```go
func TestTwoPhaseCommit_Concurrent(t *testing.T) {
    testDB := testutil.NewTestDB(t)
    defer testDB.Close()
    defer testDB.Cleanup()
    
    token := testutil.NewTestUserTokenWithBalance(1000)
    testDB.InsertTestUserToken(ctx, token)
    
    service := tokens.NewService(testDB.Pool)
    
    // 并发预留
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            service.CheckAndReserveTokens(ctx, token.UserID, 100, "test")
        }()
    }
    wg.Wait()
    
    // 验证最终余额
    updatedToken, _ := testDB.GetTestUserToken(ctx, token.UserID)
    assert.GreaterOrEqual(t, updatedToken.Balance, int64(0))
}
```

### 3. 事务回滚测试

```go
func TestTwoPhaseCommit_Rollback(t *testing.T) {
    testDB := testutil.NewTestDB(t)
    defer testDB.Close()
    defer testDB.Cleanup()
    
    token := testutil.NewTestUserTokenWithBalance(1000)
    testDB.InsertTestUserToken(ctx, token)
    
    // 模拟事务失败
    // 验证余额未变化
}
```

## 运行测试

```bash
# 运行所有测试
go test ./...

# 运行 Token Service 测试
go test ./internal/tokens/...

# 生成覆盖率报告
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# 运行特定测试
go test -run TestTwoPhaseCommit ./internal/tokens/...
```

## 测试覆盖率目标

- **Token Service**: >80%
- **Subscription Service**: >70%
- **Event Handlers**: >60%
- **总体**: >70%

---

**最后更新**: 2025-10-08  
**维护者**: 后端团队
