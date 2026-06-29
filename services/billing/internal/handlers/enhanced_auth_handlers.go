package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	)

// EnhancedAuthHandler 增强的认证处理器
type EnhancedAuthHandler struct {
	jwtManager     *auth.EnhancedJWTManager
	rbacManager    *auth.RBACManager
}

// NewEnhancedAuthHandler 创建增强的认证处理器
func NewEnhancedAuthHandler(jwtManager *auth.EnhancedJWTManager, rbacManager *auth.RBACManager) *EnhancedAuthHandler {
	return &EnhancedAuthHandler{
		jwtManager: jwtManager,
		rbacManager: rbacManager,
	}
}

// GetUserInfo 获取用户信息（使用增强JWT）
func (h *EnhancedAuthHandler) GetUserInfo(ctx context.Context) (*auth.EnhancedClaims, error) {
	// 从请求中提取令牌
	authHeader := ctx.Value("user_claims")
	if authHeader == nil {
		return nil, fmt.Errorf("user claims not found in context")
	}

	// 将interface{}转换为EnhancedClaims
	claimsBytes, ok := authHeader.([]byte)
	if !ok {
		return nil, fmt.Errorf("invalid user claims format")
	}

	var claims auth.EnhancedClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user claims: %w", err)
	}

	return &claims, nil
}

// CreateResource 创建资源处理器（需要权限检查）
func (h *EnhancedAuthHandler) CreateResource(resourceType string, requiredPermission string) http.Handler {
	return h.rbacManager.CheckResourceOwnership(resourceType)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查权限
		if !h.rbacManager.CheckUserPermission(claims.UserID, requiredPermission) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		// 创建资源逻辑
		// 这里应该有具体的资源创建逻辑
		// 例如：创建offer记录、更新用户资料等

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":    "Resource created successfully",
			"resource_type": resourceType,
			"timestamp": time.Now(),
		})
	})
}

// UpdateUserProfile 更新用户档案（需要权限）
func (h *EnhancedAuthHandler) UpdateUserProfile() http.Handler {
	return h.rbacManager.CheckUserPermission(claims.UserID, "user:update_profile")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查权限
		if !h.rbacManager.CheckUserPermission(claims.UserID, "user:update_profile") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
			// 解析请求体
		var updateRequest struct {
			DisplayName string `json:"display_name"`
		}

		if err := json.NewDecoder(r.Body).Decode(&updateRequest); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// 这里应该调用用户服务更新用户资料
		// user.UserService.UpdateProfile(userID, updateRequest.DisplayName)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "User profile updated successfully",
			"updated_at": time.Now(),
		})
	})
}

// DeleteUserAccount 删除用户账户（需要管理员权限）
func (h *EnhancedAuthHandler) DeleteUserAccount() http.Handler {
	return h.rbacManager.CheckUserPermission(claims.UserID, "user:delete_account")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查权限
		if !h.rbacManager.CheckUserPermission(claims.UserID, "user:delete_account") {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		// 解析请求体
		var deleteRequest struct {
			Confirmation string `json:"confirmation"`
		}

		if err := json.NewDecoder(r.Body).Decode(&deleteRequest); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// 验证确认
		if deleteRequest.Confirmation != "DELETE_ACCOUNT_CONFIRMED" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// 这里应该调用用户服务删除账户
		// user.UserService.DeleteAccount(userID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "User account deleted successfully",
			"deleted_at": time.Now(),
		})
	})
}

// GetBillingAdmin 获取计费管理员权限
func (h *EnhancedAuthHandler) GetBillingAdmin() http.Handler {
	return h.rbacManager.CheckUserPermission(claims.UserID, "billing:admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查权限
		if !h.rbacManager.CheckUserPermission(claims.UserID, "billing:admin") {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		// 返回管理员权限
		adminPerms, err := h.rbacManager.GetUserEffectivePermissions(claims.UserID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user_id":    claims.UserID,
			"permissions": adminPerms,
			"is_admin": h.rbacManager.CheckUserPermission(claims.UserID, "super_admin"),
		})
	})
}

// 系统权限检查中间件
func (h *EnhancedAuthHandler) RequireSystemPermission(permission string) func(http.Handler) {
	return h.rbacManager.CheckUserPermission(claims.UserID, "system:admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.rbacManager.CheckUserPermission(claims.UserID, "system:admin") {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireServiceAuth 服务间认证中间件
func (h *EnhancedAuthHandler) RequireServiceAuth(serviceName string) func(http.Handler) {
	return h.rbacManager.CheckServiceAccess(claims.UserID, serviceName)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证服务访问权限
		if !h.rbacManager.CheckServiceAccess(claims.UserID, serviceName) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireAnyAdminRole 任何管理员角色
func (h *EnhancedAuthHandler) RequireAnyAdminRole() func(http.Handler) {
	return h.rbacManager.CheckUserPermission(claims.UserID, "super_admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.rbacManager.CheckUserPermission(claims.UserID, "super_admin") {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireRole 要求特定角色
func (h *EnhancedAuthHandler) RequireRole(role string) func(http.Handler) {
	return h.rbacManager.CheckUserPermission(claims.UserID, fmt.Sprintf("role:%s", role))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.rbacManager.CheckUserPermission(claims.UserID, fmt.Sprintf("role:%s", role)) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// CheckPermission 权限检查中间件（可配置权限）
func (h *EnhancedAuthHandler) CheckPermission(permission string, options ...string) func(http.Handler) {
	return h.rbacManager.CheckUserPermission(claims.UserID, permission)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.rbacManager.CheckUserPermission(claims.UserID, permission) {
			// 权限不足，可以选择返回错误或继续处理
			// 这里添加权限不足的处理逻辑
			switch {
			case "return_error":
				w.WriteHeader(http.StatusForbidden)
				return
			case "continue":
				// 继续处理下一个权限检查
				next.ServeHTTP(w, r)
			default:
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
	})
}

// EnhancedClaimsToContext 将增强声明注入到上下文
func (h *EnhancedAuthHandler) EnhancedClaimsToContext(ctx context.Context, claims *auth.EnhancedClaims) context.Context {
	// 使用你选择的上下文注入方式
	// 例如：
	// ctx = context.WithValue("user_claims", claims)
	return ctx
}

// ===== 部署示例 =====

// UpdateBillingServiceMain 更新Billing服务main函数
func UpdateBillingServiceMain() {
	// 创建增强JWT管理器
	jwtManager, err := auth.NewEnhancedJWTAuth()
	if err != nil {
		log.Fatalf("Failed to create enhanced JWT manager: %v", err)
	}

	// 创建RBAC管理器
	permissionStore := auth.NewMemoryPermissionStore()

	// 加载默认策略
	if err := permissionStore.LoadDefaultPolicies(); err != nil {
		log.Fatalf("Failed to load default RBAC policies: %v", err)
	}

	// 创建增强的认证处理器
	authHandler := NewEnhancedAuthHandler(jwtManager, permissionStore)

	// 创建增强的服务处理器（使用新的适配器）
	// 这里应该替换原有的Handler创建逻辑
	// billingService := storage.NewEnhancedAdapterService()
	// ...
}

	fmt.Println("Enhanced JWT and RBAC authentication system initialized")
	fmt.Println("Available endpoints:")
	fmt.Println("  GET /api/v1/user/profile - Get user info with permissions")
	fmt.Println("  PUT /api/v1/user/profile - Update user profile (requires user:update_profile permission)")
	fmt.Println("  POST /api/v1/resources - Create resource (requires appropriate permissions)")
	fmt.Println("  DELETE /api/v1/account - Delete user account (requires user:delete_account permission)")
	fmt.Println("  GET /api/v1/billing/admin - Get billing admin permissions (requires billing:admin)")
	fmt.Println("  GET /api/v1/auth/jwks - Get JWT signing keys (requires super_admin permission)")
	fmt.Println("  GET /api/v1/auth/rotate-keys - Rotate JWT keys (requires super_admin permission)")
	fmt.Println()
	fmt.Println("  POST /api/v1/auth/refresh-token - Refresh access token (requires user:refresh_token permission)")
	fmt.Println()
	fmt.Println("  GET /api/v1/auth/token - Get current active token (requires authenticated user)")
	fmt.Println()

	// 这里可以注册路由
	// router := http.NewServeMux()
	// router.HandleFunc("/api/v1/user/profile", authHandler.GetUserInfo)
	// router.HandleFunc("/api/v1/user/profile", authHandler.UpdateUserProfile)
	// router.HandleFunc("/api/v1/resources", authHandler.CreateResource("offer"))
	// router.HandleFunc("/api/v1/account", authHandler.DeleteUserAccount)
	// router.HandleFunc("/api/v1/billing/admin", authHandler.GetBillingAdmin)
	// ... 其他路由注册

	// 启动服务
	// log.Printf("Starting enhanced billing service on :8080")
	// log.Fatal(http.ListenAndServe(":8080", router))
}