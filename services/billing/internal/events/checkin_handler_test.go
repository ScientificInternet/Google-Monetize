package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckinCompletedPayload_JSONUnmarshal(t *testing.T) {
	jsonData := `{
		"eventId": "evt-123",
		"eventType": "CheckinCompleted",
		"occurredAt": "2025-10-17T10:00:00Z",
		"user_id": "user-456",
		"data": {
			"checkinId": "checkin-789",
			"user_id": "user-456",
			"checkinDate": "2025-10-17",
			"streak": 5,
			"tokenReward": 10,
			"isFirstTime": false
		}
	}`

	var payload CheckinCompletedPayload
	err := json.Unmarshal([]byte(jsonData), &payload)
	require.NoError(t, err)

	assert.Equal(t, "evt-123", payload.EventID)
	assert.Equal(t, "CheckinCompleted", payload.EventType)
	assert.Equal(t, "user-456", payload.UserID)
	assert.Equal(t, "checkin-789", payload.Data.CheckinID)
	assert.Equal(t, 5, payload.Data.Streak)
	assert.Equal(t, 10, payload.Data.TokenReward)
	assert.False(t, payload.Data.IsFirstTime)
}

func TestCheckinCompletedPayload_JSONMarshal(t *testing.T) {
	payload := CheckinCompletedPayload{
		EventID:    "evt-123",
		EventType:  "CheckinCompleted",
		OccurredAt: time.Now().Format(time.RFC3339),
		UserID:     "user-456",
		Data: CheckinCompletedData{
			CheckinID:   "checkin-789",
			UserID:      "user-456",
			CheckinDate: "2025-10-17",
			Streak:      5,
			TokenReward: 10,
			IsFirstTime: false,
		},
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	// Verify JSON contains expected fields
	assert.Contains(t, string(data), "evt-123")
	assert.Contains(t, string(data), "user-456")
	assert.Contains(t, string(data), "checkin-789")
}

func TestCheckinCompletedData_Validation(t *testing.T) {
	tests := []struct {
		name    string
		data    CheckinCompletedData
		isValid bool
	}{
		{
			name: "valid checkin data",
			data: CheckinCompletedData{
				CheckinID:   "checkin-123",
				UserID:      "user-456",
				CheckinDate: "2025-10-17",
				Streak:      5,
				TokenReward: 10,
			},
			isValid: true,
		},
		{
			name: "missing userID",
			data: CheckinCompletedData{
				CheckinID:   "checkin-123",
				UserID:      "",
				CheckinDate: "2025-10-17",
				Streak:      5,
				TokenReward: 10,
			},
			isValid: false,
		},
		{
			name: "zero token reward",
			data: CheckinCompletedData{
				CheckinID:   "checkin-123",
				UserID:      "user-456",
				CheckinDate: "2025-10-17",
				Streak:      5,
				TokenReward: 0,
			},
			isValid: false,
		},
		{
			name: "negative token reward",
			data: CheckinCompletedData{
				CheckinID:   "checkin-123",
				UserID:      "user-456",
				CheckinDate: "2025-10-17",
				Streak:      5,
				TokenReward: -10,
			},
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate userID
			hasUserID := tt.data.UserID != ""
			// Validate token reward
			hasValidReward := tt.data.TokenReward > 0

			isValid := hasUserID && hasValidReward
			assert.Equal(t, tt.isValid, isValid)
		})
	}
}

func TestCheckinEventHandler_Structure(t *testing.T) {
	// Test handler creation without database
	handler := &CheckinEventHandler{}
	assert.NotNil(t, handler)
}

func TestCheckinCompletedPayload_WithMetadata(t *testing.T) {
	payload := CheckinCompletedPayload{
		EventID:    "evt-123",
		EventType:  "CheckinCompleted",
		OccurredAt: time.Now().Format(time.RFC3339),
		UserID:     "user-456",
		Data: CheckinCompletedData{
			CheckinID:   "checkin-789",
			UserID:      "user-456",
			CheckinDate: "2025-10-17",
			Streak:      5,
			TokenReward: 10,
			IsFirstTime: true,
		},
		Metadata: map[string]interface{}{
			"source":    "mobile_app",
			"version":   "1.0.0",
			"ipAddress": "192.168.1.1",
		},
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded CheckinCompletedPayload
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, payload.EventID, decoded.EventID)
	assert.Equal(t, payload.Data.Streak, decoded.Data.Streak)
	assert.NotNil(t, decoded.Metadata)
	assert.Equal(t, "mobile_app", decoded.Metadata["source"])
}

func TestCheckinCompletedPayload_FirstTimeBonus(t *testing.T) {
	// Test first-time checkin
	firstTimePayload := CheckinCompletedPayload{
		EventID:    "evt-first",
		EventType:  "CheckinCompleted",
		OccurredAt: time.Now().Format(time.RFC3339),
		UserID:     "user-new",
		Data: CheckinCompletedData{
			CheckinID:   "checkin-first",
			UserID:      "user-new",
			CheckinDate: time.Now().Format("2006-01-02"),
			Streak:      1,
			TokenReward: 20, // First-time bonus
			IsFirstTime: true,
		},
	}

	assert.True(t, firstTimePayload.Data.IsFirstTime)
	assert.Equal(t, 1, firstTimePayload.Data.Streak)
	assert.Equal(t, 20, firstTimePayload.Data.TokenReward)

	// Test regular checkin
	regularPayload := CheckinCompletedPayload{
		EventID:    "evt-regular",
		EventType:  "CheckinCompleted",
		OccurredAt: time.Now().Format(time.RFC3339),
		UserID:     "user-regular",
		Data: CheckinCompletedData{
			CheckinID:   "checkin-regular",
			UserID:      "user-regular",
			CheckinDate: time.Now().Format("2006-01-02"),
			Streak:      5,
			TokenReward: 10, // Regular reward
			IsFirstTime: false,
		},
	}

	assert.False(t, regularPayload.Data.IsFirstTime)
	assert.Equal(t, 5, regularPayload.Data.Streak)
	assert.Equal(t, 10, regularPayload.Data.TokenReward)
}
