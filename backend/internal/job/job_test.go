package job

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gresbase/gresbase/internal/tools/cron"
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

func TestSafeCallHandlerRecoversPanic(t *testing.T) {
	s := NewScheduler(nil)
	s.RegisterHandler("boom", func(ctx context.Context, data map[string]any) error {
		panic("kaboom")
	})

	s.mu.RLock()
	handler := s.handlers["boom"]
	s.mu.RUnlock()

	err := safeCallHandler(context.Background(), "panicking job", handler, nil)
	if err == nil {
		t.Fatal("expected a panicking handler to surface as an error (failed run)")
	}
	if !strings.Contains(err.Error(), "kaboom") {
		t.Errorf("expected panic value in error, got: %v", err)
	}

	// The scheduler must remain fully usable after a panic.
	s.RegisterHandler("ok", func(ctx context.Context, data map[string]any) error { return nil })
	s.mu.RLock()
	okHandler := s.handlers["ok"]
	s.mu.RUnlock()
	if err := safeCallHandler(context.Background(), "ok job", okHandler, nil); err != nil {
		t.Errorf("expected healthy handler to succeed after a prior panic, got: %v", err)
	}
}

func TestSchedulerSurvivesPanickingJob(t *testing.T) {
	// Schedule a due job whose handler panics, fire the scheduler tick, and
	// assert the process (and scheduler) survive. Without the deferred
	// recover()s, the panicking goroutine would crash the whole test binary.
	s := NewScheduler(nil)
	s.RegisterHandler("boom", func(ctx context.Context, data map[string]any) error {
		panic("kaboom")
	})

	schedule, err := cron.Parse("* * * * *")
	if err != nil {
		t.Fatalf("parse cron: %v", err)
	}
	due := time.Now().Add(-time.Minute)
	s.mu.Lock()
	s.jobs["panic-job"] = &Job{
		ID:        "panic-job",
		Name:      "Panic Job",
		Handler:   "boom",
		Enabled:   true,
		NextRunAt: &due,
		schedule:  schedule,
	}
	s.mu.Unlock()

	s.checkAndRun()

	// Give the spawned run goroutine time to panic and recover.
	time.Sleep(200 * time.Millisecond)

	// Scheduler is still alive and serving requests.
	if err := s.ValidateCron("*/5 * * * *"); err != nil {
		t.Errorf("scheduler unusable after panicking job: %v", err)
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
