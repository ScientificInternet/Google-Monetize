package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/services"
)

type UserHandler struct {
	userService *services.UserService
	syncService *services.SyncService
}

func NewUserHandler(userService *services.UserService, syncService *services.SyncService) *UserHandler {
	return &UserHandler{
		userService: userService,
		syncService: syncService,
	}
}

// GetUserProfile handles GET /api/v1/users/:userId/profile
func (h *UserHandler) GetUserProfile(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID is required"})
		return
	}

	user, err := h.userService.GetUserProfile(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": user})
}

// GetUserCompleteInfo handles GET /api/v1/users/:userId
func (h *UserHandler) GetUserCompleteInfo(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID is required"})
		return
	}

	userInfo, err := h.userService.GetUserCompleteInfo(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": userInfo})
}

// GetCurrentUserProfile handles GET /api/v1/user/profile
// 获取当前登录用户的资料信息（从JWT token中提取用户ID）
func (h *UserHandler) GetCurrentUserProfile(c *gin.Context) {
	// 从context中获取用户ID（由JWT middleware注入）
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}

	userIDStr, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID type in context"})
		return
	}

	user, err := h.userService.GetUserProfile(c.Request.Context(), userIDStr)
	if err != nil {
		// 如果用户不存在，返回404，让前端知道需要初始化
		if err.Error() == "user not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "user_profile_not_found",
				"message": "用户业务数据不存在，需要初始化",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, user)
}

// SyncUser handles POST /api/v1/sync/user/:userId
func (h *UserHandler) SyncUser(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID is required"})
		return
	}

	err := h.userService.SyncUser(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User sync completed successfully"})
}
