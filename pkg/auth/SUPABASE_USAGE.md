# Supabase JWT验证使用指南

## 概述

`pkg/auth/supabase.go` 提供了Supabase JWT token的验证功能，可以在Go微服务中验证来自Supabase Auth的JWT token。

## 功能特性

- ✅ 自动从Supabase获取JWKS (JSON Web Key Set)
- ✅ 缓存公钥（5分钟TTL）
- ✅ RS256签名验证
- ✅ 提取用户ID和email
- ✅ 线程安全
- ✅ Context支持

## 环境变量

需要设置以下环境变量之一：

```bash
NEXT_PUBLIC_SUPABASE_URL=https://jzzvizacfyipzdyiqfzb.supabase.co
# 或
SUPABASE_URL=https://jzzvizacfyipzdyiqfzb.supabase.co
```

## 基本使用

### 1. 验证Token并提取用户ID

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    
    "github.com/ScientificInternet/Google-Monetize/pkg/auth"
)

func handler(w http.ResponseWriter, r *http.Request) {
    // 从请求中提取用户ID
    userID, err := auth.ExtractSupabaseUserID(context.Background(), r)
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }
    
    fmt.Fprintf(w, "User ID: %s", userID)
}
```

### 2. 提取完整用户信息

```go
func handler(w http.ResponseWriter, r *http.Request) {
    // 提取用户ID和email
    info, err := auth.ExtractSupabaseInfo(context.Background(), r)
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }
    
    fmt.Fprintf(w, "User ID: %s, Email: %s", info.UserID, info.Email)
}
```

### 3. 使用自定义Verifier

```go
func main() {
    // 创建自定义verifier
    verifier := auth.NewSupabaseVerifier("https://jzzvizacfyipzdyiqfzb.supabase.co")
    
    // 预加载JWKS
    if err := verifier.FetchJWKS(context.Background()); err != nil {
        log.Fatal(err)
    }
    
    // 使用verifier
    http.HandleFunc("/api/protected", func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")
        tokenString := strings.TrimPrefix(authHeader, "Bearer ")
        
        userID, err := verifier.ExtractSupabaseUserID(r.Context(), tokenString)
        if err != nil {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        
        fmt.Fprintf(w, "User ID: %s", userID)
    })
}
```

## 中间件使用

### 使用标准库 (net/http)

```go
package main

import (
    "net/http"
    
    "github.com/ScientificInternet/Google-Monetize/pkg/middleware"
)

func main() {
    mux := http.NewServeMux()
    
    // 受保护的路由
    mux.Handle("/api/protected", middleware.SupabaseAuth()(
        http.HandlerFunc(protectedHandler),
    ))
    
    // 可选认证的路由
    mux.Handle("/api/optional", middleware.SupabaseAuthOptional()(
        http.HandlerFunc(optionalHandler),
    ))
    
    http.ListenAndServe(":8080", mux)
}

func protectedHandler(w http.ResponseWriter, r *http.Request) {
    // 从context获取用户ID
    userID, ok := auth.ExtractSupabaseUserID(r.Context(), r)
    if !ok {
        http.Error(w, "User ID not found", http.StatusInternalServerError)
        return
    }
    
    w.Write([]byte("User ID: " + userID))
}

func optionalHandler(w http.ResponseWriter, r *http.Request) {
    userID, ok := auth.ExtractSupabaseUserID(r.Context(), r)
    if ok {
        w.Write([]byte("Authenticated user: " + userID))
    } else {
        w.Write([]byte("Anonymous user"))
    }
}
```

### 使用Gin框架

```go
package main

import (
    "github.com/gin-gonic/gin"
    "github.com/ScientificInternet/Google-Monetize/pkg/auth"
)

// SupabaseAuthMiddleware 是Gin的Supabase认证中间件
func SupabaseAuthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        authHeader := c.GetHeader("Authorization")
        if authHeader == "" {
            c.JSON(401, gin.H{"error": "Missing authorization header"})
            c.Abort()
            return
        }
        
        tokenString := strings.TrimPrefix(authHeader, "Bearer ")
        verifier := auth.GetSupabaseVerifier()
        
        userID, err := verifier.ExtractSupabaseUserID(c.Request.Context(), tokenString)
        if err != nil {
            c.JSON(401, gin.H{"error": "Invalid or expired token"})
            c.Abort()
            return
        }
        
        // 设置用户ID到context
        c.Set("user_id", userID)
        c.Next()
    }
}

func main() {
    r := gin.Default()
    
    // 受保护的路由组
    protected := r.Group("/api")
    protected.Use(SupabaseAuthMiddleware())
    {
        protected.GET("/profile", func(c *gin.Context) {
            userID := c.GetString("user_id")
            c.JSON(200, gin.H{"user_id": userID})
        })
    }
    
    r.Run(":8080")
}
```

### 使用Echo框架

```go
package main

import (
    "github.com/labstack/echo/v4"
    "github.com/ScientificInternet/Google-Monetize/pkg/auth"
)

// SupabaseAuthMiddleware 是Echo的Supabase认证中间件
func SupabaseAuthMiddleware() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            authHeader := c.Request().Header.Get("Authorization")
            if authHeader == "" {
                return echo.NewHTTPError(401, "Missing authorization header")
            }
            
            tokenString := strings.TrimPrefix(authHeader, "Bearer ")
            verifier := auth.GetSupabaseVerifier()
            
            userID, err := verifier.ExtractSupabaseUserID(c.Request().Context(), tokenString)
            if err != nil {
                return echo.NewHTTPError(401, "Invalid or expired token")
            }
            
            // 设置用户ID到context
            c.Set("user_id", userID)
            return next(c)
        }
    }
}

func main() {
    e := echo.New()
    
    // 受保护的路由组
    protected := e.Group("/api")
    protected.Use(SupabaseAuthMiddleware())
    protected.GET("/profile", func(c echo.Context) error {
        userID := c.Get("user_id").(string)
        return c.JSON(200, map[string]string{"user_id": userID})
    })
    
    e.Start(":8080")
}
```

## 测试

### 单元测试

```go
package auth_test

import (
    "context"
    "testing"
    
    "github.com/ScientificInternet/Google-Monetize/pkg/auth"
)

func TestSupabaseVerifier(t *testing.T) {
    verifier := auth.NewSupabaseVerifier("https://jzzvizacfyipzdyiqfzb.supabase.co")
    
    // 测试JWKS获取
    err := verifier.FetchJWKS(context.Background())
    if err != nil {
        t.Fatalf("Failed to fetch JWKS: %v", err)
    }
    
    // 测试token验证（需要有效的token）
    validToken := "eyJhbGci..." // 从Supabase获取的有效token
    userID, err := verifier.ExtractSupabaseUserID(context.Background(), validToken)
    if err != nil {
        t.Fatalf("Failed to extract user ID: %v", err)
    }
    
    if userID == "" {
        t.Fatal("User ID is empty")
    }
}
```

### 集成测试

```bash
# 获取测试token
curl -X POST 'https://jzzvizacfyipzdyiqfzb.supabase.co/auth/v1/token?grant_type=password' \
  -H "apikey: YOUR_ANON_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "test@example.com",
    "password": "password123"
  }'

# 测试API
curl -X GET 'http://localhost:8080/api/protected' \
  -H "Authorization: Bearer YOUR_ACCESS_TOKEN"
```

## 错误处理

```go
func handler(w http.ResponseWriter, r *http.Request) {
    userID, err := auth.ExtractSupabaseUserID(context.Background(), r)
    if err != nil {
        switch err {
        case auth.ErrUnauthenticated:
            http.Error(w, "Missing or invalid authorization header", http.StatusUnauthorized)
        case auth.ErrSupabaseTokenInvalid:
            http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
        case auth.ErrSupabaseJWKSFetch:
            http.Error(w, "Authentication service unavailable", http.StatusServiceUnavailable)
        default:
            http.Error(w, "Authentication failed", http.StatusUnauthorized)
        }
        return
    }
    
    // 处理请求
    fmt.Fprintf(w, "User ID: %s", userID)
}
```

## 性能优化

### 1. JWKS缓存

JWKS会自动缓存5分钟，减少对Supabase的请求：

```go
verifier := auth.NewSupabaseVerifier("https://jzzvizacfyipzdyiqfzb.supabase.co")
// 首次调用会获取JWKS
verifier.FetchJWKS(ctx)
// 后续5分钟内的调用会使用缓存
```

### 2. 预加载JWKS

在服务启动时预加载JWKS：

```go
func main() {
    // 预加载JWKS
    verifier := auth.GetSupabaseVerifier()
    if err := verifier.FetchJWKS(context.Background()); err != nil {
        log.Printf("Warning: Failed to preload JWKS: %v", err)
    }
    
    // 启动服务
    http.ListenAndServe(":8080", handler)
}
```

### 3. 使用全局Verifier

使用全局verifier实例避免重复创建：

```go
// 推荐：使用全局verifier
verifier := auth.GetSupabaseVerifier()

// 不推荐：每次创建新verifier
verifier := auth.NewSupabaseVerifier(url) // 避免这样做
```

## 安全注意事项

1. **HTTPS Only**: 生产环境必须使用HTTPS
2. **Token过期**: Supabase token默认1小时过期，前端需要自动刷新
3. **环境变量**: 不要在代码中硬编码Supabase URL
4. **错误日志**: 不要在日志中输出完整的token
5. **CORS**: 正确配置CORS策略

## 故障排查

### 问题1: "failed to fetch supabase jwks"

**原因**: 无法连接到Supabase JWKS端点

**解决**:
- 检查网络连接
- 验证SUPABASE_URL环境变量
- 检查防火墙设置

### 问题2: "invalid supabase token"

**原因**: Token无效或已过期

**解决**:
- 检查token格式（应该是JWT格式）
- 验证token未过期
- 确认token是从正确的Supabase项目获取的

### 问题3: "unexpected signing method"

**原因**: Token使用了错误的签名算法

**解决**:
- 确认Supabase使用RS256算法
- 检查token是否被篡改

## 相关文档

- [Supabase Auth文档](https://supabase.com/docs/guides/auth)
- [JWT规范](https://jwt.io/)
- [JWKS规范](https://datatracker.ietf.org/doc/html/rfc7517)

## 示例项目

完整的示例项目请参考：
- `services/billing` - 使用Supabase Auth的计费服务
- `services/offer` - 使用Supabase Auth的报价服务
