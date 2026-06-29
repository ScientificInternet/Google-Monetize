package repositories

import (
	"github.com/ScientificInternet/Google-Monetize/pkg/database"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/models"
)

// UserRepositoryInterface 定义用户仓储的统一接口
type UserRepositoryInterface interface {
	// Basic CRUD operations
	GetUserFromGCP(userID string) (*models.User, error)
	GetUserFromSupabase(userID string) (*models.User, error)
	CreateUserInGCP(user *models.User) error
	CreateUserInSupabase(user *models.User) error
	UpdateUserInGCP(user *models.User) error
	UpdateUserInSupabase(user *models.User) error
	DeleteUserFromGCP(userID string) error
	DeleteUserFromSupabase(userID string) error

	// Query operations
	GetUsersWithPagination(offset, limit int) ([]*models.User, error)
	SearchUsers(query string, offset, limit int) ([]*models.User, error)
	GetUserByEmail(email string) (*models.User, error)

	// Aggregate operations
	CountUsers() (int, error)
	CountActiveUsers() (int, error)

	// Filtered queries
	GetUsersByRole(role string, offset, limit int) ([]*models.User, error)
	GetUsersByOrganization(orgID string, offset, limit int) ([]*models.User, error)

	// Batch operations
	BatchCreateUsers(users []*models.User) error

	// Lifecycle management
	Close() error
}

// AdapterRepository 定义适配器仓储的额外接口
type AdapterRepository interface {
	UserRepositoryInterface
	GetAdapterMode() database.AdapterMode
}