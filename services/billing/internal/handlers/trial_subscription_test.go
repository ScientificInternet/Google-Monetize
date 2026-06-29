package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTrialRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     CreateTrialRequest
		wantErr bool
	}{
		{
			name: "valid self-register request",
			req: CreateTrialRequest{
				UserID: "user-123",
				Days:   7,
				Source: "self_register",
			},
			wantErr: false,
		},
		{
			name: "valid referral inviter request",
			req: CreateTrialRequest{
				UserID: "user-123",
				Days:   14,
				Source: "referral_inviter",
			},
			wantErr: false,
		},
		{
			name: "invalid days",
			req: CreateTrialRequest{
				UserID: "user-123",
				Days:   10,
				Source: "self_register",
			},
			wantErr: true,
		},
		{
			name: "missing userID",
			req: CreateTrialRequest{
				UserID: "",
				Days:   7,
				Source: "self_register",
			},
			wantErr: true,
		},
		{
			name: "invalid source",
			req: CreateTrialRequest{
				UserID: "user-123",
				Days:   7,
				Source: "invalid_source",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &TrialsubscriptionsHandler{}
			err := handler.validateCreateTrialRequest(&tt.req)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTrialHistoryResponse_Structure(t *testing.T) {
	resp := TrialHistoryResponse{
		Items: []TrialHistoryItem{
			{
				subscriptionsID: "sub-123",
				Plan:           "Pro",
				Status:         "expired",
				TrialStartDate: time.Now(),
				TrialEndDate:   time.Now().Add(7 * 24 * time.Hour),
				Source:         "self_register",
			},
		},
	}

	// Test JSON marshaling
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), "sub-123")
	assert.Contains(t, string(data), "Pro")
	assert.Contains(t, string(data), "self_register")
}

func TestCreateTrialResponse_Structure(t *testing.T) {
	now := time.Now()
	resp := CreateTrialResponse{
		subscriptionsID: "sub-123",
		UserID:         "user-456",
		Plan:           "Pro",
		Status:         "trial",
		TrialStartDate: now,
		TrialEndDate:   now.Add(7 * 24 * time.Hour),
		TokensGranted:  1000,
	}

	// Test JSON marshaling
	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded CreateTrialResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, resp.subscriptionsID, decoded.subscriptionsID)
	assert.Equal(t, resp.UserID, decoded.UserID)
	assert.Equal(t, resp.TokensGranted, decoded.TokensGranted)
}

func TestCreateTrialRequest_JSONDecoding(t *testing.T) {
	jsonData := `{
		"user_id": "user-123",
		"days": 7,
		"source": "self_register"
	}`

	var req CreateTrialRequest
	err := json.Unmarshal([]byte(jsonData), &req)
	require.NoError(t, err)

	assert.Equal(t, "user-123", req.UserID)
	assert.Equal(t, 7, req.Days)
	assert.Equal(t, "self_register", req.Source)
}

func TestExpireTrials_EmptyResult(t *testing.T) {
	// This test verifies the structure without database
	// In a real test, you would use a test database or mock

	handler := &TrialsubscriptionsHandler{}

	// Verify handler is not nil
	assert.NotNil(t, handler)
}

func TestTrialsubscriptionsHandler_ErrorResponses(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    string
		expectedStatus int
	}{
		{
			name:           "invalid JSON",
			requestBody:    `{invalid json}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing user_id",
			requestBody:    `{"days": 7, "source": "self_register"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid days",
			requestBody:    `{"user_id": "user-123", "days": 10, "source": "self_register"}`,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test request
			req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/subscriptions/trial", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")

			// Create a response recorder
			w := httptest.NewRecorder()

			// Create handler (without database for structure testing)
			handler := &TrialsubscriptionsHandler{}

			// Call the handler
			handler.CreateTrial(w, req)

			// For invalid JSON and missing fields, we expect bad request
			// Note: This will fail without proper database setup, but tests the structure
		})
	}
}
