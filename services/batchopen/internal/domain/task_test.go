package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewTask(t *testing.T) {
	id := "task-123"
	userID := "user-456"
	offerID := "offer-789"
	config := json.RawMessage(`{"url": "https://example.com"}`)

	task := NewTask(id, userID, offerID, config)

	if task.ID != id {
		t.Errorf("Expected ID to be %s, but got %s", id, task.ID)
	}
	if task.UserID != userID {
		t.Errorf("Expected UserID to be %s, but got %s", userID, task.UserID)
	}
	if task.OfferID != offerID {
		t.Errorf("Expected OfferID to be %s, but got %s", offerID, task.OfferID)
	}
	if string(task.SimulationConfig) != string(config) {
		t.Errorf("Expected SimulationConfig to be %s, but got %s", config, task.SimulationConfig)
	}
	if task.Status != "queued" {
		t.Errorf("Expected Status to be 'queued', but got %s", task.Status)
	}
	if task.Progress != 0.0 {
		t.Errorf("Expected Progress to be 0.0, but got %f", task.Progress)
	}
	if time.Since(task.CreatedAt) > time.Second {
		t.Errorf("Expected CreatedAt to be recent")
	}
}

func TestTask_Start(t *testing.T) {
	task := NewTask("id", "user", "offer", nil)
	time.Sleep(10 * time.Millisecond)
	task.Start()

	if task.Status != "running" {
		t.Errorf("Expected Status to be 'running', but got %s", task.Status)
	}
	if !task.UpdatedAt.After(task.CreatedAt) {
		t.Errorf("Expected UpdatedAt to be after CreatedAt")
	}
}

func TestTask_UpdateProgress(t *testing.T) {
	task := NewTask("id", "user", "offer", nil)
	progress := 50.5
	time.Sleep(10 * time.Millisecond)
	task.UpdateProgress(progress)

	if task.Progress != progress {
		t.Errorf("Expected Progress to be %f, but got %f", progress, task.Progress)
	}
	if !task.UpdatedAt.After(task.CreatedAt) {
		t.Errorf("Expected UpdatedAt to be after CreatedAt")
	}

	// Test clamping
	task.UpdateProgress(-10)
	if task.Progress != 0 {
		t.Errorf("Expected progress to be clamped to 0, but got %f", task.Progress)
	}
	task.UpdateProgress(110)
	if task.Progress != 100 {
		t.Errorf("Expected progress to be clamped to 100, but got %f", task.Progress)
	}
}

func TestTask_Complete(t *testing.T) {
	task := NewTask("id", "user", "offer", nil)
	time.Sleep(10 * time.Millisecond)
	task.Complete()

	if task.Status != "completed" {
		t.Errorf("Expected Status to be 'completed', but got %s", task.Status)
	}
	if task.Progress != 100.0 {
		t.Errorf("Expected Progress to be 100.0, but got %f", task.Progress)
	}
	if !task.UpdatedAt.After(task.CreatedAt) {
		t.Errorf("Expected UpdatedAt to be after CreatedAt")
	}
}

func TestTask_Fail(t *testing.T) {
	task := NewTask("id", "user", "offer", nil)
	time.Sleep(10 * time.Millisecond)
	task.Fail()

	if task.Status != "failed" {
		t.Errorf("Expected Status to be 'failed', but got %s", task.Status)
	}
	if !task.UpdatedAt.After(task.CreatedAt) {
		t.Errorf("Expected UpdatedAt to be after CreatedAt")
	}
}
