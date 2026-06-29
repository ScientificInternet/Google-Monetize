package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ScientificInternet/Google-Monetize/pkg/middleware"
	"github.com/ScientificInternet/Google-Monetize/services/billing/internal/tokens"
)

// MockTokenService implements a mock token service for testing
type MockTokenService struct {
	balance      int64
	balanceError error
	summary      tokens.BalanceSummary
	summaryError error
	reserveID    string
	reserveError error
	confirmError error
	refundError  error
}

func (m *MockTokenService) GetBalance(ctx context.Context, userID string) (int64, error) {
	return m.balance, m.balanceError
}

func (m *MockTokenService) GetBalanceSummary(ctx context.Context, userID string) (tokens.BalanceSummary, error) {
	return m.summary, m.summaryError
}

func (m *MockTokenService) CheckAndReserveTokens(ctx context.Context, userID string, amount int, description string) (string, error) {
	return m.reserveID, m.reserveError
}

func (m *MockTokenService) ConfirmTokenDeduction(ctx context.Context, reservationID string) error {
	return m.confirmError
}

func (m *MockTokenService) RefundTokens(ctx context.Context, userID, reservationID string, amount int, reason string) error {
	return m.refundError
}

func TestTokensHandler_GetBalance(t *testing.T) {
	tests := []struct {
		name           string
		setupContext   func(r *http.Request) *http.Request
		mockService    *MockTokenService
		expectedStatus int
		expectBalance  bool
	}{
		{
			name: "successful balance query",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			mockService: &MockTokenService{
				summary: tokens.BalanceSummary{
					TotalBalance:            10000,
					Balance:                 10000,
					TodayConsumed:           500,
					ThisMonthConsumed:       2000,
					PendingTasksCount:       3,
					EstimatedCostForPending: 150,
				},
			},
			expectedStatus: http.StatusOK,
			expectBalance:  true,
		},
		{
			name: "missing user ID",
			setupContext: func(r *http.Request) *http.Request {
				// Don't set user ID
				return r
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusUnauthorized,
			expectBalance:  false,
		},
		{
			name: "service error",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			mockService: &MockTokenService{
				summaryError: errors.New("database error"),
			},
			expectedStatus: http.StatusInternalServerError,
			expectBalance:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			handler := &TokensHandler{
				tokenService: tt.mockService,
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/tokens/balance", nil)
			req = tt.setupContext(req)
			w := httptest.NewRecorder()

			// Execute
			handler.getBalance(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectBalance {
				var response map[string]interface{}
				if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if _, exists := response["balance"]; !exists {
					t.Error("Expected 'balance' field in response")
				}
				if _, exists := response["totalBalance"]; !exists {
					t.Error("Expected 'totalBalance' field in response")
				}
			}
		})
	}
}

func TestTokensHandler_ReserveTokens(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		setupContext   func(r *http.Request) *http.Request
		requestBody    interface{}
		mockService    *MockTokenService
		expectedStatus int
		expectTxID     bool
	}{
		{
			name:   "successful reservation",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserReserveTokensRequest{
				Amount: 100,
				TaskID: "task-123",
			},
			mockService: &MockTokenService{
				reserveID: "txn-456",
			},
			expectedStatus: http.StatusAccepted,
			expectTxID:     true,
		},
		{
			name:   "invalid method",
			method: http.MethodGet,
			setupContext: func(r *http.Request) *http.Request {
				return r
			},
			requestBody:    nil,
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusMethodNotAllowed,
			expectTxID:     false,
		},
		{
			name:   "missing user ID",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				return r
			},
			requestBody: UserReserveTokensRequest{
				Amount: 100,
				TaskID: "task-123",
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusUnauthorized,
			expectTxID:     false,
		},
		{
			name:   "invalid amount (negative)",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserReserveTokensRequest{
				Amount: -100,
				TaskID: "task-123",
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusBadRequest,
			expectTxID:     false,
		},
		{
			name:   "invalid amount (zero)",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserReserveTokensRequest{
				Amount: 0,
				TaskID: "task-123",
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusBadRequest,
			expectTxID:     false,
		},
		{
			name:   "missing task ID",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserReserveTokensRequest{
				Amount: 100,
				TaskID: "",
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusBadRequest,
			expectTxID:     false,
		},
		{
			name:   "insufficient tokens",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserReserveTokensRequest{
				Amount: 1000,
				TaskID: "task-123",
			},
			mockService: &MockTokenService{
				reserveError: errors.New("insufficient tokens: have 0, need 1000"),
			},
			expectedStatus: http.StatusPaymentRequired,
			expectTxID:     false,
		},
		{
			name:   "service error",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserReserveTokensRequest{
				Amount: 100,
				TaskID: "task-123",
			},
			mockService: &MockTokenService{
				reserveError: errors.New("database error"),
			},
			expectedStatus: http.StatusInternalServerError,
			expectTxID:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			handler := &TokensHandler{
				tokenService: tt.mockService,
			}

			var body bytes.Buffer
			if tt.requestBody != nil {
				json.NewEncoder(&body).Encode(tt.requestBody)
			}

			req := httptest.NewRequest(tt.method, "/api/v1/billing/tokens/reserve", &body)
			req = tt.setupContext(req)
			w := httptest.NewRecorder()

			// Execute
			handler.reserveTokens(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.expectTxID {
				var response UserReserveTokensResponse
				if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if response.TxID == "" {
					t.Error("Expected non-empty txId")
				}
				if response.Status != "reserved" {
					t.Errorf("Expected status 'reserved', got '%s'", response.Status)
				}
			}
		})
	}
}

func TestTokensHandler_CommitTokens(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		setupContext   func(r *http.Request) *http.Request
		requestBody    interface{}
		mockService    *MockTokenService
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:   "successful commit",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserCommitTokensRequest{
				TxID: "txn-456",
			},
			mockService: &MockTokenService{
				balance: 9900,
			},
			expectedStatus: http.StatusOK,
			expectSuccess:  true,
		},
		{
			name:   "invalid method",
			method: http.MethodGet,
			setupContext: func(r *http.Request) *http.Request {
				return r
			},
			requestBody:    nil,
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusMethodNotAllowed,
			expectSuccess:  false,
		},
		{
			name:   "missing user ID",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				return r
			},
			requestBody: UserCommitTokensRequest{
				TxID: "txn-456",
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusUnauthorized,
			expectSuccess:  false,
		},
		{
			name:   "missing txID",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserCommitTokensRequest{
				TxID: "",
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusBadRequest,
			expectSuccess:  false,
		},
		{
			name:   "service error",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserCommitTokensRequest{
				TxID: "txn-456",
			},
			mockService: &MockTokenService{
				confirmError: errors.New("database error"),
			},
			expectedStatus: http.StatusInternalServerError,
			expectSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			handler := &TokensHandler{
				tokenService: tt.mockService,
			}

			var body bytes.Buffer
			if tt.requestBody != nil {
				json.NewEncoder(&body).Encode(tt.requestBody)
			}

			req := httptest.NewRequest(tt.method, "/api/v1/billing/tokens/commit", &body)
			req = tt.setupContext(req)
			w := httptest.NewRecorder()

			// Execute
			handler.commitTokens(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectSuccess {
				var response UserCommitTokensResponse
				if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if response.Status != "committed" {
					t.Errorf("Expected status 'committed', got '%s'", response.Status)
				}
				if response.TxID == "" {
					t.Error("Expected non-empty txID")
				}
			}
		})
	}
}

func TestTokensHandler_ReleaseTokens(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		setupContext   func(r *http.Request) *http.Request
		requestBody    interface{}
		mockService    *MockTokenService
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:   "successful release",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserReleaseTokensRequest{
				TxID:   "txn-456",
				Amount: 100,
				TaskID: "task-123",
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusOK,
			expectSuccess:  true,
		},
		{
			name:   "invalid method",
			method: http.MethodGet,
			setupContext: func(r *http.Request) *http.Request {
				return r
			},
			requestBody:    nil,
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusMethodNotAllowed,
			expectSuccess:  false,
		},
		{
			name:   "missing user ID",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				return r
			},
			requestBody: UserReleaseTokensRequest{
				TxID:   "txn-456",
				Amount: 100,
				TaskID: "task-123",
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusUnauthorized,
			expectSuccess:  false,
		},
		{
			name:   "missing txID",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserReleaseTokensRequest{
				TxID:   "",
				Amount: 100,
				TaskID: "task-123",
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusBadRequest,
			expectSuccess:  false,
		},
		{
			name:   "invalid amount (negative)",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserReleaseTokensRequest{
				TxID:   "txn-456",
				Amount: -100,
				TaskID: "task-123",
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusBadRequest,
			expectSuccess:  false,
		},
		{
			name:   "invalid amount (zero)",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserReleaseTokensRequest{
				TxID:   "txn-456",
				Amount: 0,
				TaskID: "task-123",
			},
			mockService:    &MockTokenService{},
			expectedStatus: http.StatusBadRequest,
			expectSuccess:  false,
		},
		{
			name:   "service error",
			method: http.MethodPost,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, "user123")
				return r.WithContext(ctx)
			},
			requestBody: UserReleaseTokensRequest{
				TxID:   "txn-456",
				Amount: 100,
				TaskID: "task-123",
			},
			mockService: &MockTokenService{
				refundError: errors.New("database error"),
			},
			expectedStatus: http.StatusInternalServerError,
			expectSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			handler := &TokensHandler{
				tokenService: tt.mockService,
			}

			var body bytes.Buffer
			if tt.requestBody != nil {
				json.NewEncoder(&body).Encode(tt.requestBody)
			}

			req := httptest.NewRequest(tt.method, "/api/v1/billing/tokens/release", &body)
			req = tt.setupContext(req)
			w := httptest.NewRecorder()

			// Execute
			handler.releaseTokens(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectSuccess {
				var response UserReleaseTokensResponse
				if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if response.Status != "released" {
					t.Errorf("Expected status 'released', got '%s'", response.Status)
				}
				if response.TxID == "" {
					t.Error("Expected non-empty txID")
				}
			}
		})
	}
}
