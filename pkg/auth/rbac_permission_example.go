// Package auth 提供RBAC权限检查示例
// 展示如何使用增强的JWT认证和RBAC权限控制
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// RBACPermissionCheckExample RBAC权限检查示例
type RBACPermissionCheckExample struct {
	jwtManager    *EnhancedJWTManager
	rbacManager   *RBACManager
}

// NewRBACPermissionCheckExample 创建RBAC权限检查示例
func NewRBACPermissionCheckExample() *RBACPermissionCheckExample {
	jwtManager, err := NewEnhancedJWTAuth()
	if err != nil {
		log.Fatalf("Failed to create enhanced JWT auth: %v", err)
	}

	rbacManager, err := NewRBACManager(jwtManager, nil)
	if err != nil {
		log.Fatalf("Failed to create RBAC manager: %v", err)
	}

	return &RBACPermissionCheckExample{
		jwtManager: jwtManager,
		rbacManager: rbacManager,
	}
}

// === 权限检查中间件示例 ===

// CheckUserPermission 检查用户权限中间件
func (h *RBACPermissionCheckExample) CheckUserPermission(permission string) func(http.Handler) {
	return h.jwtManager.AuthenticateMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// 验证JWT令牌
		claims, err := h.jwtManager.ValidateEnhancedToken(tokenString)
		if err != nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			})
		}

		// 检查权限
		if !h.rbacManager.CheckUserPermission(claims.UserID, permission) {
			// 权限不足
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			})
		}

		// 权限充足，继续处理
		return next
	})
}

// CheckAdminPermission 检查管理员权限中间件
func (h *RBACPermissionCheckExample) CheckAdminPermission() func(http.Handler) {
	return h.jwtManager.AuthenticateMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// 验证JWT令牌
		claims, err := h.jwtManager.ValidateEnhancedToken(tokenString)
		if err != nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			})
		}

		// 检查管理员权限
		if !h.rbacManager.CheckUserPermission(claims.UserID, "super_admin") {
			// 权限不足
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Insufficient admin permissions", http.StatusForbidden)
				return
			})
		}

		// 权限充足，继续处理
		return next
	})
}

// CheckResourceOwnership 检查资源所有权中间件
func (h *RBACPermissionCheckExample) CheckResourceOwnership(resourceType string) func(http.Handler) {
	return h.jwtManager.AuthenticateMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// 验证JWT令牌
		claims, err := h.jwtManager.ValidateEnhancedToken(tokenString)
		if err != nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			})
		}

		// 提取资源ID（从URL参数）
		resourceID := r.URL.Query().Get("resource_id")
		if resourceID == "" {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Resource ID required", http.StatusBadRequest)
				return
			})
		}

		// 检查用户是否有访问该资源的权限
		permission := fmt.Sprintf("%s:%s", resourceType, resourceID)
		if !h.rbacManager.CheckUserPermission(claims.UserID, permission) {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, fmt.Sprintf("Access denied to resource: %s", resourceID), http.StatusForbidden)
				return
			})
		}

		// 权限充足，继续处理
		return next
	})
}

// GetUserPermissionsHandler 获取用户权限处理器
func (h *RBACPermissionCheckExample) GetUserPermissionsHandler() http.HandlerFunc {
	return h.jwtManager.AuthenticateMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// 验证JWT令牌
		claims, err := h.jwtManager.ValidateEnhancedToken(tokenString)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

			// 获取用户权限
		userID := claims.UserID
		permissions, err := h.rbacManager.GetUserEffectivePermissions(userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// 返回权限列表
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user_id":    userID,
			"permissions": permissions,
		})
	})
}

// UpdateUserPermissionsHandler 更新用户权限处理器
func (h *RBACPermissionCheckExample) UpdateUserPermissionsHandler() http.HandlerFunc {
	return h.jwtManager.AuthenticateMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// 验证JWT令牌
		claims, err := h.jwtManager.ValidateEnhancedToken(tokenString)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// 解析请求体
		var permissionUpdate struct {
			Permission string `json:"permission"`
			Granted     bool   `json:"granted"`
		}

		if err := json.NewDecoder(r.Body).Decode(&permissionUpdate); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// 更新权限（这里简化了，实际应该有审批流程）
		userID := claims.UserID
		_, err = h.rbacManager.UpdateUserPermission(userID, permissionUpdate.Permission, permissionUpdate.Granted)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "User permissions updated",
		})
	})
}

// CreateResourceHandler 创建资源处理器（需要所有权检查）
func (h *RBACPermissionCheckExample) CreateResourceHandler(resourceType string) func(http.Handler) {
	return h.CheckResourceOwnership(resourceType)(func(w http.ResponseWriter, r *http.Request) {
		// 资源创建逻辑
		// 这里应该：
		// 1. 验证用户认证
		// 2. 检查权限
		// 3. 创建资源
		// 4. 设置资源所有者
		// 5. 记录审计日志
	})
}

// === 使用示例 ===

// ExampleUsage 展示如何使用权限检查
func (h *RBACPermissionCheckExample) ExampleUsage() {
	fmt.Println("=== Enhanced JWT + RBAC Authentication System ===")
	fmt.Println("1. Enhanced JWT Manager with:")
	fmt.Println("   - RS256 key signing")
	fmt.Println("   - Key rotation support")
	fmt.Println("   - Environment-based configuration")
	fmt.Println("   - Token validation with enhanced claims")
	fmt.Println()
	fmt.Println("2. RBAC Manager with:")
	fmt.Println("   - Policy-based permission system")
	fmt.Println("   - Memory and Database permission store")
	fmt.Println("   - Hierarchical role management")
	fmt.Println("   - Custom policy evaluation engine")
	fmt.Println()

	fmt.Println("3. Usage Examples:")
	fmt.Println()

	// 基础权限检查
	fmt.Println("   // 检查用户权限")
	fmt.Println("   authMiddleware.CheckUserPermission(user, 'user:update_profile')")
	fmt.Println("   authMiddleware.CheckAdminPermission(user)")
	fmt.Println()

	// 资源所有权检查")
	fmt.Println("   authMiddleware.CheckResourceOwnership('offer', resourceID)")
	fmt.Println("   authMiddleware.CheckResourceOwnership('subscription', subscriptionID)")
	fmt.Println()

	// 动态权限检查")
	fmt.Println("   // 从数据库动态加载权限")
	fmt.Println("   permissions, err := rbacManager.GetUserEffectivePermissions(userID)")
	fmt.Println("   // 可以结合业务逻辑进行更细粒度的权限控制")
	fmt.Println()

	// 权限管理API")
	fmt.Println("   // 更新用户权限")
	fmt.Println("   authMiddleware.UpdateUserPermissions(userID)")
	fmt.Println("   // 权限失效化")
	fmt.Println("   // 自动权限检查和缓存失效")

	// 服务间认证（Service-to-Service auth）
	fmt.Println("   // 使用JWT管理器生成服务令牌")
	fmt.Println("   serviceToken, err := jwtManager.GenerateServiceToken('analytics-service', []string{'read', 'write'}, ttl)")
	fmt.Println("   // 服务调用时验证服务令牌")
	fmt.Println()
	fmt.Println()
}
}