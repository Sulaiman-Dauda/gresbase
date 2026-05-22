package job

import (
	"context"
	"testing"
	"time"
)

func TestNewScheduler(t *testing.T) {
	s := NewScheduler(nil)
	if s == nil {
		t.Fatal("expected non-nil scheduler")
	}
}

func TestJobStruct(t *testing.T) {
	now := time.Now()
	job := Job{
		ID:        "test-job-1",
		Name:      "Test Job",
		CronExpr:  "*/5 * * * *",
		Handler:   "test-handler",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if job.ID != "test-job-1" {
		t.Error("job ID mismatch")
	}
	if !job.Enabled {
		t.Error("job should be enabled")
	}
	if job.CronExpr != "*/5 * * * *" {
		t.Error("cron expression mismatch")
	}
	if job.Handler != "test-handler" {
		t.Error("handler mismatch")
	}
}

func TestSchedulerRegisterHandler(t *testing.T) {
	s := NewScheduler(nil)
	called := false

	s.RegisterHandler("test-handler", func(ctx context.Context, data map[string]any) error {
		called = true
		return nil
	})

	if !called {
		// Handler registration doesn't call it, just ensures no panic
	}
}

func TestSchedulerValidateCron(t *testing.T) {
	s := NewScheduler(nil)

	err := s.ValidateCron("*/5 * * * *")
	if err != nil {
		t.Errorf("expected valid cron expression: %v", err)
	}

	err = s.ValidateCron("invalid")
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestSchedulerGetNextRuns(t *testing.T) {
	s := NewScheduler(nil)

	times, err := s.GetNextRuns("*/5 * * * *", 3)
	if err != nil {
		t.Fatalf("failed to get next runs: %v", err)
	}
	if len(times) != 3 {
		t.Errorf("expected 3 next run times, got %d", len(times))
	}
}
