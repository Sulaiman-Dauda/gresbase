package cron

import (
	"testing"
	"time"
)

func TestParseEvery(t *testing.T) {
	tests := []struct {
		expr    string
		wantErr bool
	}{
		{"@every 1h", false},
		{"@every 30m", false},
		{"@every 5s", false},
		{"@every 1h30m", false},
		{"@every invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		sched, err := Parse(tt.expr)
		if tt.wantErr {
			if err == nil {
				t.Errorf("Parse(%q) expected error, got nil", tt.expr)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", tt.expr, err)
			continue
		}
		if sched == nil {
			t.Errorf("Parse(%q) returned nil schedule", tt.expr)
		}
	}
}

func TestParseStandard(t *testing.T) {
	tests := []string{
		"* * * * *",
		"*/5 * * * *",
		"0 0 * * *",

		"30 9 1 * *",
	}

	for _, expr := range tests {
		sched, err := Parse(expr)
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", expr, err)
			continue
		}
		if sched == nil {
			t.Errorf("Parse(%q) returned nil schedule", expr)
		}
	}
}

func TestScheduleNext(t *testing.T) {
	sched, err := Parse("*/5 * * * *")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	next := sched.Next(now)

	if next.Before(now) || next.Equal(now) {
		t.Error("next run should be in the future")
	}
	if next.Sub(now) > 5*time.Minute+time.Second {
		t.Errorf("next run too far in future: %v", next.Sub(now))
	}
}

func TestScheduleNextN(t *testing.T) {
	sched, err := Parse("@every 1h")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	times := sched.NextN(now, 3)

	if len(times) != 3 {
		t.Fatalf("expected 3 times, got %d", len(times))
	}

	for i := 1; i < len(times); i++ {
		if times[i].Before(times[i-1]) {
			t.Error("times should be monotonically increasing")
		}
	}
}

func TestScheduleDaily(t *testing.T) {
	sched, err := Parse("@daily")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	next := sched.Next(now)

	if next.Before(now) {
		t.Error("daily next should be in future")
	}

	// Should be midnight or close to it
	if next.Hour() != 0 {
		t.Logf("daily next at %s (hour=%d)", next.Format(time.RFC3339), next.Hour())
	}
}
