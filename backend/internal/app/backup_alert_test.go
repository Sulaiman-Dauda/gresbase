package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// recordingMailer captures backup failure alert sends for assertions.
type recordingMailer struct {
	mu    sync.Mutex
	sends []recordedAlert
}

type recordedAlert struct {
	to       string
	errMsg   string
	when     string
	instance string
}

func (m *recordingMailer) send(to, errMsg, when, instance string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = append(m.sends, recordedAlert{to: to, errMsg: errMsg, when: when, instance: instance})
	return nil
}

func (m *recordingMailer) recipients() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.sends))
	for i, s := range m.sends {
		out[i] = s.to
	}
	return out
}

func newTestAlerter(mail *recordingMailer, emails []string, clock *time.Time) *backupFailureAlerter {
	return &backupFailureAlerter{
		enabled:    func() bool { return true },
		listEmails: func(ctx context.Context) ([]string, error) { return emails, nil },
		send:       mail.send,
		instance:   "https://gresbase.example.com",
		interval:   backupAlertMinInterval,
		now:        func() time.Time { return *clock },
	}
}

func TestBackupFailureAlertEmailsAllSuperusers(t *testing.T) {
	mail := &recordingMailer{}
	superusers := []string{"a@example.com", "b@example.com", "c@example.com"}
	now := time.Now()
	alerter := newTestAlerter(mail, superusers, &now)

	alerter.notify(context.Background(), errors.New("disk full"))

	got := mail.recipients()
	if len(got) != len(superusers) {
		t.Fatalf("expected alert to all %d superusers, got %d: %v", len(superusers), len(got), got)
	}
	for i, email := range superusers {
		if got[i] != email {
			t.Errorf("recipient %d: expected %s, got %s", i, email, got[i])
		}
	}
	for _, s := range mail.sends {
		if s.errMsg != "disk full" {
			t.Errorf("expected error message in alert, got %q", s.errMsg)
		}
		if s.when == "" {
			t.Error("expected timestamp in alert")
		}
		if s.instance != "https://gresbase.example.com" {
			t.Errorf("expected instance address in alert, got %q", s.instance)
		}
	}
}

func TestBackupFailureAlertThrottledWithinAnHour(t *testing.T) {
	mail := &recordingMailer{}
	now := time.Now()
	alerter := newTestAlerter(mail, []string{"admin@example.com"}, &now)

	alerter.notify(context.Background(), errors.New("disk full"))
	if len(mail.recipients()) != 1 {
		t.Fatalf("expected first failure to alert, got %d sends", len(mail.recipients()))
	}

	// Second failure 30 minutes later: suppressed.
	now = now.Add(30 * time.Minute)
	alerter.notify(context.Background(), errors.New("disk still full"))
	if len(mail.recipients()) != 1 {
		t.Fatalf("expected second failure within the hour to be suppressed, got %d sends", len(mail.recipients()))
	}

	// Failure after the throttle window: alerts again.
	now = now.Add(31 * time.Minute)
	alerter.notify(context.Background(), errors.New("disk STILL full"))
	if len(mail.recipients()) != 2 {
		t.Fatalf("expected failure after an hour to alert again, got %d sends", len(mail.recipients()))
	}
}

func TestBackupFailureAlertSkippedWhenMailerNotConfigured(t *testing.T) {
	mail := &recordingMailer{}
	now := time.Now()
	alerter := newTestAlerter(mail, []string{"admin@example.com"}, &now)
	alerter.enabled = func() bool { return false }

	alerter.notify(context.Background(), errors.New("disk full"))
	if len(mail.recipients()) != 0 {
		t.Fatalf("expected no alert without a configured mailer, got %d sends", len(mail.recipients()))
	}
}

func TestBackupFailureAlertNilError(t *testing.T) {
	mail := &recordingMailer{}
	now := time.Now()
	alerter := newTestAlerter(mail, []string{"admin@example.com"}, &now)

	alerter.notify(context.Background(), nil)
	if len(mail.recipients()) != 0 {
		t.Fatalf("expected no alert for nil error, got %d sends", len(mail.recipients()))
	}
}
