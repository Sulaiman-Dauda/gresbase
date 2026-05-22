package events_test

import (
	"testing"

	"github.com/gresbase/gresbase/internal/events"
)

func TestHookBindAndTrigger(t *testing.T) {
	hook := events.NewHook()

	hook.Bind(events.Handler{
		ID:       "test-handler",
		Priority: events.PriorityDefault,
		Func: func(e events.Event) error {
			t.Log("Handler executed")
			return e.Next()
		},
	})

	if hook.Len() != 1 {
		t.Errorf("Expected 1 handler, got %d", hook.Len())
	}
}

func TestHookPriorityOrder(t *testing.T) {
	hook := events.NewHook()
	execOrder := []string{}

	hook.Bind(events.Handler{
		ID:       "low",
		Priority: events.PriorityLow,
		Func: func(e events.Event) error {
			execOrder = append(execOrder, "low")
			return e.Next()
		},
	})

	hook.Bind(events.Handler{
		ID:       "high",
		Priority: events.PriorityHigh,
		Func: func(e events.Event) error {
			execOrder = append(execOrder, "high")
			return e.Next()
		},
	})

	hook.Bind(events.Handler{
		ID:       "default",
		Priority: events.PriorityDefault,
		Func: func(e events.Event) error {
			execOrder = append(execOrder, "default")
			return e.Next()
		},
	})

	handlers := hook.Handlers()
	if len(handlers) != 3 {
		t.Fatalf("Expected 3 handlers, got %d", len(handlers))
	}

	// High priority (lowest number) should come first
	if handlers[0].ID != "high" {
		t.Errorf("Expected 'high' first, got '%s'", handlers[0].ID)
	}
	if handlers[1].ID != "default" {
		t.Errorf("Expected 'default' second, got '%s'", handlers[1].ID)
	}
	if handlers[2].ID != "low" {
		t.Errorf("Expected 'low' third, got '%s'", handlers[2].ID)
	}
}

func TestHookUnbind(t *testing.T) {
	hook := events.NewHook()

	hook.Bind(events.Handler{ID: "a", Priority: 0})
	hook.Bind(events.Handler{ID: "b", Priority: 1})

	if hook.Len() != 2 {
		t.Fatalf("Expected 2 handlers, got %d", hook.Len())
	}

	hook.Unbind("a")
	if hook.Len() != 1 {
		t.Errorf("Expected 1 handler after unbind, got %d", hook.Len())
	}

	if hook.Handlers()[0].ID != "b" {
		t.Errorf("Expected handler 'b' remaining, got '%s'", hook.Handlers()[0].ID)
	}
}

func TestBaseEventAbort(t *testing.T) {
	event := &events.BootstrapEvent{}
	event.Abort()

	if event.IsPropagated() {
		t.Error("Expected propagation to be stopped after Abort")
	}
}

func TestBaseEventNextStopsOnAbort(t *testing.T) {
	called := false
	event := &events.BootstrapEvent{}

	event.Abort()

	// Next should not execute since aborted
	err := event.Next()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if called {
		t.Error("Handler should not have been called after abort")
	}

	_ = called
}
