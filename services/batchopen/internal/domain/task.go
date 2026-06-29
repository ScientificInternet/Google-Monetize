package domain

import (
	"encoding/json"
	"time"
)

// Task represents a batchopen task.
type Task struct {
	ID               string          `json:"id"`
	UserID           string          `json:"userId"`
	OfferID          string          `json:"offerId"`
	SimulationConfig json.RawMessage `json:"simulationConfig"`
	Status           string          `json:"status"` // "queued", "running", "completed", "failed"
	Progress         float64         `json:"progress"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

// NewTask creates a new batchopen task with a "queued" status.
func NewTask(id, userID, offerID string, config json.RawMessage) *Task {
	now := time.Now()
	return &Task{
		ID:               id,
		UserID:           userID,
		OfferID:          offerID,
		SimulationConfig: config,
		Status:           "queued",
		Progress:         0.0,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

// Start marks the task as "running".
func (t *Task) Start() {
	t.Status = "running"
	t.UpdatedAt = time.Now()
}

// UpdateProgress updates the task's progress.
func (t *Task) UpdateProgress(progress float64) {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	t.Progress = progress
	t.UpdatedAt = time.Now()
}

// Complete marks the task as "completed".
func (t *Task) Complete() {
	t.Status = "completed"
	t.Progress = 100.0
	t.UpdatedAt = time.Now()
}

// Fail marks the task as "failed".
func (t *Task) Fail() {
	t.Status = "failed"
	t.UpdatedAt = time.Now()
}
