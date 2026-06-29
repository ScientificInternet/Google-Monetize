package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleOnboardingStepCompleted tests the onboarding step completion handler
func TestHandleOnboardingStepCompleted(t *testing.T) {
	tests := []struct {
		name        string
		payload     OnboardingStepCompletedPayload
		setupMock   func(sqlmock.Sqlmock)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful onboarding reward",
			payload: OnboardingStepCompletedPayload{
				UserID:       "user-123",
				StepID:       "step-1",
				RewardTokens: 200,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()

				// Mark step as completed
				mock.ExpectExec(`INSERT INTO "UserChecklistProgress"`).
					WithArgs("user-123", "step-1").
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Query current balance
				mock.ExpectQuery(`SELECT balance FROM "user_tokens"`).
					WithArgs("user-123").
					WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(1000))

				// Upsert user token balance
				mock.ExpectExec(`INSERT INTO "user_tokens"`).
					WithArgs("user-123", 200).
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Upsert user token pool
				mock.ExpectExec(`INSERT INTO "user_tokensPool"`).
					WithArgs("user-123", 200).
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Insert token transaction
				mock.ExpectExec(`INSERT INTO "token_transactions"`).
					WithArgs("user-123", 200, int64(1000), int64(1200), sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Insert credit lot
				mock.ExpectExec(`INSERT INTO "TokenCreditLot"`).
					WithArgs("user-123", 200, sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectCommit()
			},
			wantErr: false,
		},
		{
			name: "no reward for step",
			payload: OnboardingStepCompletedPayload{
				UserID:       "user-123",
				StepID:       "step-no-reward",
				RewardTokens: 0,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				// No database operations expected
			},
			wantErr: false,
		},
		{
			name: "negative reward tokens",
			payload: OnboardingStepCompletedPayload{
				UserID:       "user-123",
				StepID:       "step-negative",
				RewardTokens: -100,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				// No database operations expected
			},
			wantErr: false,
		},
		{
			name: "new user onboarding",
			payload: OnboardingStepCompletedPayload{
				UserID:       "user-new",
				StepID:       "step-first",
				RewardTokens: 50,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()

				mock.ExpectExec(`INSERT INTO "UserChecklistProgress"`).
					WithArgs("user-new", "step-first").
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectQuery(`SELECT balance FROM "user_tokens"`).
					WithArgs("user-new").
					WillReturnError(sql.ErrNoRows)

				mock.ExpectExec(`INSERT INTO "user_tokens"`).
					WithArgs("user-new", 50).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectExec(`INSERT INTO "user_tokensPool"`).
					WithArgs("user-new", 50).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectExec(`INSERT INTO "token_transactions"`).
					WithArgs("user-new", 50, int64(0), int64(50), sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectExec(`INSERT INTO "TokenCreditLot"`).
					WithArgs("user-new", 50, sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectCommit()
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()

			tt.setupMock(mock)

			payload, err := json.Marshal(tt.payload)
			require.NoError(t, err)

			// Act
			err = HandleOnboardingStepCompleted(context.Background(), db, payload)

			// Assert
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}

			// Verify all expectations were met
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestCreditsubscriptionsTokens tests crediting subscription tokens
func TestCreditsubscriptionsTokens(t *testing.T) {
	tests := []struct {
		name      string
		userID    string
		amount    int
		desc      string
		meta      map[string]any
		setupMock func(sqlmock.Sqlmock)
		wantErr   bool
	}{
		{
			name:   "successful subscription credit",
			userID: "user-123",
			amount: 10000,
			desc:   "Pro plan subscription",
			meta:   map[string]any{"planId": "pro"},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()

				mock.ExpectQuery(`SELECT balance FROM "user_tokens"`).
					WithArgs("user-123").
					WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(5000))

				mock.ExpectExec(`INSERT INTO "user_tokens"`).
					WithArgs("user-123", 10000).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectExec(`INSERT INTO "user_tokensPool"`).
					WithArgs("user-123", 10000).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectExec(`INSERT INTO "token_transactions"`).
					WithArgs("user-123", 10000, int64(5000), int64(15000), "Pro plan subscription", sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectExec(`INSERT INTO "TokenCreditLot"`).
					WithArgs("user-123", 10000, nil, sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectCommit()
			},
			wantErr: false,
		},
		{
			name:   "zero amount - no operation",
			userID: "user-123",
			amount: 0,
			desc:   "Zero credit",
			meta:   nil,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No database operations expected
			},
			wantErr: false,
		},
		{
			name:   "negative amount - no operation",
			userID: "user-123",
			amount: -100,
			desc:   "Negative credit",
			meta:   nil,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No database operations expected
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()

			tt.setupMock(mock)

			// Act
			err = CreditsubscriptionsTokens(context.Background(), db, tt.userID, tt.amount, tt.desc, tt.meta)

			// Assert
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify all expectations were met
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestCreditPurchasedTokens tests crediting purchased tokens
func TestCreditPurchasedTokens(t *testing.T) {
	tests := []struct {
		name      string
		userID    string
		amount    int
		desc      string
		meta      map[string]any
		setupMock func(sqlmock.Sqlmock)
		wantErr   bool
	}{
		{
			name:   "successful purchased credit",
			userID: "user-123",
			amount: 5000,
			desc:   "Token purchase",
			meta:   map[string]any{"orderId": "order-123"},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()

				mock.ExpectQuery(`SELECT balance FROM "user_tokens"`).
					WithArgs("user-123").
					WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(2000))

				mock.ExpectExec(`INSERT INTO "user_tokens"`).
					WithArgs("user-123", 5000).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectExec(`INSERT INTO "user_tokensPool"`).
					WithArgs("user-123", 5000).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectExec(`INSERT INTO "token_transactions"`).
					WithArgs("user-123", 5000, int64(2000), int64(7000), "Token purchase", sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectExec(`INSERT INTO "TokenCreditLot"`).
					WithArgs("user-123", 5000, sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectCommit()
			},
			wantErr: false,
		},
		{
			name:   "zero amount - no operation",
			userID: "user-123",
			amount: 0,
			desc:   "Zero purchase",
			meta:   nil,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No database operations expected
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()

			tt.setupMock(mock)

			// Act
			err = CreditPurchasedTokens(context.Background(), db, tt.userID, tt.amount, tt.desc, tt.meta)

			// Assert
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify all expectations were met
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestIsUniqueViolation tests the unique violation error detection
func TestIsUniqueViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "unique violation error - correct position",
			// The function checks err.Error()[12:17] == "23505"
			// Position:  0123456789012345678
			// We need:   xxxxxxxxxxxx23505xxx
			err:  &mockError{msg: "pq: duplicat23505 key violates unique constraint"},
			want: true,
		},
		{
			name: "other error",
			err:  &mockError{msg: "some other error"},
			want: false,
		},
		{
			name: "short error message",
			err:  &mockError{msg: "short"},
			want: false,
		},
		{
			name: "error with 23505 in wrong position",
			err:  &mockError{msg: "23505 at start"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUniqueViolation(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// mockError is a simple error implementation for testing
type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}
