package testutil

import (
	"context"
)

// MockTokenService is a mock implementation of token service for testing
type MockTokenService struct {
	CheckAndReserveTokensFunc func(ctx context.Context, userID string, amount int, description string) (string, error)
	CommitReservationFunc     func(ctx context.Context, reservationID string) error
	ReleaseReservationFunc    func(ctx context.Context, reservationID string) error
	GetBalanceFunc            func(ctx context.Context, userID string) (int64, error)
}

// CheckAndReserveTokens mocks the CheckAndReserveTokens method
func (m *MockTokenService) CheckAndReserveTokens(ctx context.Context, userID string, amount int, description string) (string, error) {
	if m.CheckAndReserveTokensFunc != nil {
		return m.CheckAndReserveTokensFunc(ctx, userID, amount, description)
	}
	return "test-reservation-id", nil
}

// CommitReservation mocks the CommitReservation method
func (m *MockTokenService) CommitReservation(ctx context.Context, reservationID string) error {
	if m.CommitReservationFunc != nil {
		return m.CommitReservationFunc(ctx, reservationID)
	}
	return nil
}

// ReleaseReservation mocks the ReleaseReservation method
func (m *MockTokenService) ReleaseReservation(ctx context.Context, reservationID string) error {
	if m.ReleaseReservationFunc != nil {
		return m.ReleaseReservationFunc(ctx, reservationID)
	}
	return nil
}

// GetBalance mocks the GetBalance method
func (m *MockTokenService) GetBalance(ctx context.Context, userID string) (int64, error) {
	if m.GetBalanceFunc != nil {
		return m.GetBalanceFunc(ctx, userID)
	}
	return 1000, nil
}

// MocksubscriptionsService is a mock implementation of subscription service
type MocksubscriptionsService struct {
	CreateFunc func(ctx context.Context, userID, planID string) error
	GetFunc    func(ctx context.Context, userID string) (interface{}, error)
	CancelFunc func(ctx context.Context, subscriptionID string) error
}

// Create mocks the Create method
func (m *MocksubscriptionsService) Create(ctx context.Context, userID, planID string) error {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, userID, planID)
	}
	return nil
}

// Get mocks the Get method
func (m *MocksubscriptionsService) Get(ctx context.Context, userID string) (interface{}, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, userID)
	}
	return NewTestsubscriptions(), nil
}

// Cancel mocks the Cancel method
func (m *MocksubscriptionsService) Cancel(ctx context.Context, subscriptionID string) error {
	if m.CancelFunc != nil {
		return m.CancelFunc(ctx, subscriptionID)
	}
	return nil
}

// MockEventPublisher is a mock implementation of event publisher
type MockEventPublisher struct {
	PublishFunc func(ctx context.Context, event interface{}) error
	Events      []interface{}
}

// Publish mocks the Publish method and stores events
func (m *MockEventPublisher) Publish(ctx context.Context, event interface{}) error {
	m.Events = append(m.Events, event)
	if m.PublishFunc != nil {
		return m.PublishFunc(ctx, event)
	}
	return nil
}

// GetEvents returns all published events
func (m *MockEventPublisher) GetEvents() []interface{} {
	return m.Events
}

// GetEventCount returns the number of published events
func (m *MockEventPublisher) GetEventCount() int {
	return len(m.Events)
}

// GetLastEvent returns the last published event
func (m *MockEventPublisher) GetLastEvent() interface{} {
	if len(m.Events) == 0 {
		return nil
	}
	return m.Events[len(m.Events)-1]
}

// Reset clears all stored events
func (m *MockEventPublisher) Reset() {
	m.Events = []interface{}{}
}

// NewMockTokenService creates a new MockTokenService
func NewMockTokenService() *MockTokenService {
	return &MockTokenService{}
}

// NewMocksubscriptionsService creates a new MocksubscriptionsService
func NewMocksubscriptionsService() *MocksubscriptionsService {
	return &MocksubscriptionsService{}
}

// NewMockEventPublisher creates a new MockEventPublisher
func NewMockEventPublisher() *MockEventPublisher {
	return &MockEventPublisher{
		Events: []interface{}{},
	}
}
