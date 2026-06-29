package domain

import "time"

// UserRegisteredEvent is the event received from the identity service.
// This service only cares about a subset of the fields.
type UserRegisteredEvent struct {
	UserID       string    `json:"user_id"`
	Email        string    `json:"email"`
	RegisteredAt time.Time `json:"registeredAt"`
}

// EventType returns the type of the event.
func (e UserRegisteredEvent) EventType() string {
	return "user.registered"
}
