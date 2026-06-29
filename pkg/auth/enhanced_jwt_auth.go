// Package auth provides enhanced JWT authentication and RBAC permission control
// 完全遵循DATABASE_ARCHITECTURE_CURRENT.md的最终架构状态
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ===== Enhanced JWT Authentication =====

// EnhancedJWTManager 增强��JWT管理器
// 支持RS256签名、密钥轮换、更细粒度的权限控制
type EnhancedJWTManager struct {
	config           EnhancedJWTConfig
	currentKeyID      string
	currentPrivateKey  *rsa.PrivateKey
	publicKeys        map[string]*rsa.PublicKey
	keyRotationTime  time.Time
}

// EnhancedJWTConfig 增强的JWT配置
type EnhancedJWTConfig struct {
	Issuer             string            `json:"issuer"`
	JWKSURL           string            `json:"jwks_url"`
	KeyRotationInterval time.Duration    `json:"key_rotation_interval_hours"`
	AccessTokenTTL     int              `json:"access_token_ttl_minutes"`
	RefreshTokenTTL    int              `json:"refresh_token_ttl_hours"`
	Environment        string            `json:"environment"`
	EnableKeyRotation bool            `json:"enable_key_rotation"`
}

// EnhancedClaims 增强的Token声明
type EnhancedClaims struct {
	UserID          string                 `json:"user_id"`
	Email           string                 `json:"email"`
	Name            string                 `json:"name,omitempty"`
	Role            string                 `json:"role"`
	Permissions     []string               `json:"permissions"`
	ServiceID       string                 `json:"service_id,omitempty"`
	TokenType       string                 `json:"token_type"`  // access, refresh, service, admin, reset
	SessionID      string                 `json:"session_id,omitempty"`
	IssuedAt       int64                  `json:"iat"`
	ExpiresAt       int64                  `json:"exp"`
	Issuer         string                 `json:"iss"`
	Audience       []string               `json:"aud"`
	jwt.RegisteredClaims
}

// PermissionDetails 权限详情
type PermissionDetails struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Area        string            `json:"area"`       // billing, user_management, system_admin, etc
	Action      []string            `json:"action"`   // read, write, delete, create, update
	Resource    string            `json:"resource"`  // users, subscriptions, offers, etc.
	Conditions  []string            `json:"conditions"` // user_owned, organization_owned, etc.
}

// RBACPolicy RBAC策略
type RBACPolicy struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Description     string               `json:"description"`
	Rules           []RBACRule        `json:"rules"`
	DefaultAllow    bool               `json:"default_allow"`
	Effect          string               `json:"effect"`      // allow, deny, conditional
	Target          []string              `json:"target"`      // all, authenticated, specific_roles
}

// RBACRule RBAC规则
type RBACRule struct {
	ID          string               `json:"id"`
	PolicyID    string               `json:"policy_id"`
	Name        string               `json:"name"`
	Effect      string               `json:"effect"`
	Action      []string               `json:"action"`
	Resource    []string               `json:"resource"`
	Condition   string               `json:"condition"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// UserPermissions 用户权限缓存
type UserPermissions struct {
	UserID           string                    `json:"user_id"`
	Permissions     map[string]PermissionStatus `json:"permissions"`
	LastUpdated    time.Time                   `json:"last_updated"`
	CacheTTL       time.Duration              `json:"cache_ttl"`
}

// PermissionStatus 权限状态
type PermissionStatus struct {
	Granted        bool      `json:"granted"`
	LastChecked    time.Time `json:"last_checked"`
	Reason        string    `json:"reason,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// ===== RBAC权限管理器 =====

// RBACManager RBAC权限管理器
type RBACManager struct {
	jwtManager      *EnhancedJWTManager
	permissionsDB    PermissionStore
}

// PermissionStore 权限存储接口
type PermissionStore interface {
	GetUserPermissions(userID string) (*UserPermissions, error)
	UpdateUserPermissions(userID string, permissions map[string]PermissionStatus) error
	CheckUserPermission(userID, permission string) (bool, error)
	InvalidateUserPermissions(userID string) error
	GetPolicy(policyID string) (*RBACPolicy, error)
	GetAllPolicies() ([]*RBACPolicy, error)
}

// MemoryPermissionStore 内存权限存储实现
type MemoryPermissionStore struct {
	permissions map[string]UserPermissions
	policies     map[string]*RBACPolicy
	mutex        sync.RWMutex
}

func NewMemoryPermissionStore() *MemoryPermissionStore {
	return &MemoryPermissionStore{
		permissions: make(map[string]UserPermissions),
		policies:     make(map[string]*RBACPolicy),
	}
}

func (m *MemoryPermissionStore) GetUserPermissions(userID string) (*UserPermissions, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if perms, exists := m.permissions[userID]; exists {
		return perms, nil
	}

	// 返回空权限集
	return &UserPermissions{
		UserID:    userID,
		Permissions: make(map[string]PermissionStatus),
		LastUpdated: time.Now(),
		CacheTTL:    30 * time.Minute,
	}, nil
}

func (m *MemoryPermissionStore) UpdateUserPermissions(userID string, permissions map[string]PermissionStatus) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.permissions[userID] = &UserPermissions{
		UserID:       userID,
		Permissions: permissions,
		LastUpdated:  time.Now(),
		CacheTTL:      30 * time.Minute,
	}

	return nil
}

func (m *MemoryPermissionStore) CheckUserPermission(userID, permission string) (bool, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	perms, exists := m.permissions[userID]
	if !exists {
		return false, nil
	}

	if permStatus, exists := perms.Permissions[permission]; exists {
		return permStatus.Granted && !permStatus.ExpiresAt.Before(time.Now()), nil
	}

	return false, nil
}

func (m *MemoryPermissionStore) InvalidateUserPermissions(userID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.permissions, userID)
	return nil
}

func (m *MemoryPermissionStore) GetPolicy(policyID string) (*RBACPolicy, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if policy, exists := m.policies[policyID]; exists {
		return policy, nil
	}

	return nil, fmt.Errorf("policy not found: %s", policyID)
}

func (m *MemoryPermissionStore) GetAllPolicies() ([]*RBACPolicy, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	policies := make([]*RBACPolicy, 0, len(m.policies))
	i := 0
	for _, policy := range m.policies {
		policies[i] = policy
		i++
	}

	return policies, nil
}

// ===== 增强的JWT管理器实现 =====

// NewEnhancedJWTManager 创建增强的JWT管理器
func NewEnhancedJWTManager(config EnhancedJWTConfig) (*EnhancedJWTManager, error) {
	manager := &EnhancedJWTManager{
		config:           config,
		currentKeyID:      "main",
	}

	// 初始化密钥对
	if err := manager.loadKeys(); err != nil {
		return nil, fmt.Errorf("failed to load JWT keys: %w", err)
	}

	// 启动密钥轮换
	if config.EnableKeyRotation {
		go manager.keyRotationRoutine()
	}

	return manager, nil
}

// loadKeys 从文件或环境变量加载JWT密钥
func (m *EnhancedJWTManager) loadKeys() error {
	// 实现密钥管理逻辑
	// 这里应该从安全的密钥管理系统加载
	// 当前简化为环境变量实现
	privateKeyPEM := os.Getenv("JWT_PRIVATE_KEY")
	if privateKeyPEM == "" {
		return fmt.Errorf("JWT_PRIVATE_KEY environment variable not set")
	}

	// 解析私钥
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privateKeyPEM))
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	m.currentPrivateKey = privateKey
	m.publicKeys["main"] = &privateKey.PublicKey

	return nil
}

// GenerateEnhancedAccessToken 生成增强的访问令牌
func (m *EnhancedJWTManager) GenerateEnhancedAccessToken(userID, email, name, role string, permissions []string, sessionID string, customClaims map[string]interface{}) (string, error) {
	claims := EnhancedClaims{
		UserID:      userID,
		Email:       email,
		Name:        name,
		Role:        role,
		Permissions: permissions,
		TokenType:   "access",
		SessionID:   sessionID,
		IssuedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(time.Duration(m.config.AccessTokenTTL) * time.Minute).Unix(),
		Issuer:     m.config.Issuer,
		Audience:   []string{"gm-api"},
		jwt.RegisteredClaims{
			ID:        m.generateTokenID(),
		},
	}

	// 添加自定义声明
	for key, value := range customClaims {
		claims[key] = value
	}

	// 签名令
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedString, err := token.SignedString(m.currentPrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign enhanced access token: %w", err)
	}

	return signedString, nil
}

// ValidateEnhancedToken 验证增强令牌
func (m *EnhancedJWTManager) ValidateEnhancedToken(tokenString string) (*EnhancedClaims, error) {
	// 实现更严格的令牌验证
	token, err := jwt.ParseWithClaims(tokenString, &EnhancedClaims{}, func(token *jwt.Token) (interface{}, error) {
		// 验证签名方法
		if _, ok := token.Method.(*jwt.SigningMethodRS256); !ok {
			return nil, fmt.Errorf("invalid signing method: %v", token.Header["alg"])
		}

		// 验证令牌有效性
		if !token.Valid {
			return nil, fmt.Errorf("invalid token")
		}

		// 验证签名
		if err := jwt.Keyfunc(m.currentPrivateKey); err != nil {
			return nil, fmt.Errorf("invalid signature")
		}

		// 验证发行者
		claims, _ := token.Claims.(*EnhancedClaims)
		if claims.Issuer != m.config.Issuer {
			return nil, fmt.Errorf("invalid token issuer")
		}

		// 验证受众
		if !containsString(claims.Audience, "gm-api") {
			return nil, fmt.Errorf("invalid token audience")
		}

		// 验证令牌类型
		if claims.TokenType != "access" && claims.TokenType != "refresh" {
			return nil, fmt.Errorf("invalid token type")
		}

		// 验证过期时间
		if claims.ExpiresAt < time.Now().Unix() {
			return nil, fmt.Errorf("token expired")
		}

		return claims, nil
	}

// ===== 增强的JWT中间件 =====

// EnhancedJWTMiddleware 增强的JWT中间件
type EnhancedJWTMiddleware struct {
	jwtManager    *EnhancedJWTManager
	rbacManager   *RBACManager
}

func NewEnhancedJWTMiddleware(jwtManager *EnhancedJWTManager) *EnhancedJWTMiddleware {
	return &EnhancedJWTMiddleware{
		jwtManager:  jwtManager,
		rbacManager: NewRBACManager(jwtManager, NewMemoryPermissionStore()),
	}
}

// Authenticate 验证JWT令牌并注入用户信息
func (m *EnhancedJWTMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 提取Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
			return
		}

		// 提取令牌
		tokenString, err := ExtractEnhancedTokenFromHeader(authHeader)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// 验证令牌
		claims, err := m.jwtManager.ValidateEnhancedToken(tokenString)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// 检查权限
		if !m.rbacManager.CheckUserPermission(claims.UserID, "api_access") {
			http.Error(w, "Insufficient permissions", http.StatusForbidden)
			return
		}

		// 将用户信息注入到请求上下文
		// 这里应该使用你选择的上下文注入方式
		// 例如：Gin的Context，或中间件链

		// 继续处理请求
		next.ServeHTTP(w, r)
	})
}

// ExtractEnhancedTokenFromHeader 从Authorization header提取增强令牌
func ExtractEnhancedTokenFromHeader(authHeader string) (string, error) {
	if authHeader == "" {
		return "", fmt.Errorf("authorization header is empty")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", fmt.Errorf("authorization header format must be Bearer {token}")
	}

	return parts[1], nil
}

// ===== 辅助函数 =====

// generateTokenID 生成令牌ID
func (m *EnhancedJWTManager) generateTokenID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return base64.URLEncoding.EncodeToString(bytes)
}

// containsString 检查字符串切片是否包含特定字符串
func containsString(slice []string, str string) bool {
	for _, s := range slice {
		if strings.Contains(str, s) {
			return true
		}
	}
	return false
}

// NewRBACManager 创建RBAC管理器
func NewRBACManager(jwtManager *EnhancedJWTManager, permissionStore PermissionStore) *RBACManager {
	return &RBACManager{
		jwtManager:      jwtManager,
		permissionsDB:    permissionStore,
	}
}

// ===== RBAC管理器实现 =====

// CheckUserPermission 检查用户权限
func (m *RBACManager) CheckUserPermission(userID, permission string) (bool, error) {
	// 检查用户权限
	perms, err := m.permissionsDB.GetUserPermissions(userID)
	if err != nil {
		return false, err
	}

	// 检查权限缓存
	permStatus, exists := perms.Permissions[permission]
	if !exists {
		return false, nil
	}

	// 权限已过期
	if permStatus.ExpiresAt.Before(time.Now()) {
		return false, nil
	}

	return permStatus.Granted, nil
}

// GetUserEffectivePermissions 获取用户有效权限
func (m *RBACManager) GetUserEffectivePermissions(userID string) (map[string]bool, error) {
	perms, err := m.permissionsDB.GetUserPermissions(userID)
	if err != nil {
		return nil, err
	}

	effective := make(map[string]bool)
	for permission, status := range perms.Permissions {
		if status.Granted && !status.ExpiresAt.Before(time.Now()) {
			effective[permission] = true
		} else {
			effective[permission] = false
		}
	}

	return effective, nil
}

// ===== 权限定义 =====

// 预定义的权限和策略
const (
	// 用户权限
	PermissionUserReadProfile     = "user:read_profile"
	PermissionUserUpdateProfile    = "user:update_profile"
	PermissionUserDeleteAccount    = "user:delete_account"
	PermissionUserManageAPIKeys = "user:manage_api_keys"
	PermissionUserAccessDashboard  = "user:access_dashboard"

	// 订阅权限
	PermissionBillingRead      = "billing:read"
	PermissionBillingWrite     = "billing:write"
	PermissionBillingAdmin     = "billing:admin"

	// Offer权限
	PermissionOfferRead        = "offer:read"
	PermissionOfferWrite       = "offer:write"
	PermissionOfferAdmin       = "offer:admin"

	// 广告账户权限
	PermissionAdsRead          = "ads:read"
	PermissionAdsWrite         = "ads:write"
	PermissionAdsAdmin         = "ads:admin"

	// 系统管理权限
	PermissionSystemRead      = "system:read"
	PermissionSystemAdmin     = "system:admin"

	// 服务间权限
	PermissionServiceAuth     = "service:auth"
	PermissionServiceAccess   = "service:access"
)

	// 管理员权限
	PermissionSuperAdmin       = "super:admin"
)

// getDefaultRBACPolicies 获取默认RBAC策略
func getDefaultRBACPolicies() []*RBACPolicy {
	return []*RBACPolicy{
		{
			ID: "user_basic",
			Name: "基础用户权限",
			Description: "用户基本的个人资料管理权限",
			Rules: []RBACRule{
				{
					ID: "user_profile_access",
					PolicyID: "user_basic",
					Name: "用户资料访问",
					Effect: "allow",
					Action: []string{"read", "update"},
					Resource: []string{"user_profile"},
					Condition: "user_owner",
					Parameters: map[string]interface{}{
						"user_id": "{user_id}",
					},
				},
				{
					ID: "user_management",
					PolicyID: "user_basic",
					Name: "用户管理权限",
					Effect: "conditional",
					Action: []string{"delete_account", "manage_api_keys"},
					Resource: []string{"user_account"},
					Condition: "account_owner AND email_verified",
					Parameters: map[string]interface{}{
						"grace_period_days": 30,
					},
				},
			},
		},
	}

	// 管理员策略
	{
			ID: "super_admin",
			Name: "超级管理员权限",
			Description: "系统超级管理员权限，可以执行所有操作",
			Rules: []RBACRule{
				{
					ID: "system_full_access",
					PolicyID: "super_admin",
					Name: "系统完全访问",
					Effect: "allow",
					Action: []string{"*"}, // 所有权限
					Resource: []string{"*"},
					Condition: "is_authenticated",
				},
			},
		},
	},
	}
}

// ===== 工厂函数 =====

// NewEnhancedJWTAuth 创建增强的JWT认证系统
func NewEnhancedJWTAuth() (*EnhancedJWTManager, *RBACManager, error) {
	// 加载配置
	config := EnhancedJWTConfig{
		Issuer:             getEnvAsString("JWT_ISSUER", ""),
		JWKSURL:           getEnvAsString("JWT_JWKS_URL", ""),
		KeyRotationInterval: time.Duration(getEnvAsInt("JWT_KEY_ROTATION_INTERVAL_HOURS", 24)) * time.Hour,
		AccessTokenTTL:     getEnvAsInt("JWT_ACCESS_TOKEN_TTL", 15),
		RefreshTokenTTL:    getEnvAsInt("JWT_REFRESH_TOKEN_TTL", 168),
		Environment:        getEnvAsString("ENVIRONMENT", "development"),
		EnableKeyRotation: getEnvAsBool("JWT_ENABLE_KEY_ROTATION", false),
	}

	// 创建JWT管理器
	jwtManager, err := NewEnhancedJWTManager(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create enhanced JWT manager: %w", err)
	}

	// 创建权限存储
	permissionStore := NewMemoryPermissionStore()

	// 预加载默认策略
	if err := loadDefaultRBACPolicies(permissionStore); err != nil {
		return nil, nil, fmt.Errorf("failed to load default RBAC policies: %w", err)
	}

	return jwtManager, &RBACManager{
		jwtManager:      jwtManager,
		permissionsDB:    permissionStore,
	}, nil
}

// loadDefaultRBACPolicies 加载默认RBAC策略
func loadDefaultRBACPolicies(store PermissionStore) error {
	// 这里应该从数据库或配置文件加载策略
	// 当前使用内存实现
	policies := getDefaultRBACPolicies()

	for _, policy := range policies {
		store.policies[policy.ID] = policy
	}

	return nil
}

// 辅助函数

// getEnvAsInt 从环境变量获取整数
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := fmt.Sscanf(value, "%d", &defaultValue); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvAsBool 从环境变量获取布尔值
func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := fmt.Sscanf(value, "%t", &defaultValue); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// getEnvAsString 从环境变量获取字符串
func getEnvAsString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}