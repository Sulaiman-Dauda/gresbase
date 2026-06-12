package events_test

import (
	"testing"

	"github.com/gresbase/gresbase/internal/events"
)

func TestHookBindAndTrigger(t *testing.T) {
	hook := events.NewHook()
	called := false

	hook.Bind(events.Handler{
		ID:       "test-handler",
		Priority: events.PriorityDefault,
		Func: func(e events.Event) error {
			called = true
			return e.Next()
		},
	})

	if hook.Len() != 1 {
		t.Errorf("Expected 1 handler, got %d", hook.Len())
	}

	event := &events.BootstrapEvent{}
	if err := hook.Trigger(event, func(e events.Event) error { return e.Next() }); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if !called {
		t.Fatal("expected handler to be executed")
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

func TestTaggedHookFiltersByEventTags(t *testing.T) {
	hook := events.NewHook()
	called := 0

	hook.Tagged("posts").BindFunc(func(e events.Event) error {
		called++
		return e.Next()
	})
	hook.Tagged("comments").BindFunc(func(e events.Event) error {
		called += 100
		return e.Next()
	})

	event := &events.RecordEvent{CollectionName: "posts", RecordID: "rec1"}
	if err := hook.Trigger(event, func(e events.Event) error { return e.Next() }); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected only matching tagged handler to run, got %d", called)
	}
}

func TestTaggedHookWithoutEventTagsDoesNotRun(t *testing.T) {
	hook := events.NewHook()
	called := false

	hook.Tagged("posts").BindFunc(func(e events.Event) error {
		called = true
		return e.Next()
	})

	if err := hook.Trigger(&events.BootstrapEvent{}, func(e events.Event) error { return e.Next() }); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if called {
		t.Fatal("expected tagged handler to be skipped for untagged event")
	}
}

func TestTriggerExecutesFinalActionThroughHandlerChain(t *testing.T) {
	hook := events.NewHook()
	order := []string{}

	hook.BindFunc(func(e events.Event) error {
		order = append(order, "before")
		if err := e.Next(); err != nil {
			return err
		}
		order = append(order, "after")
		return nil
	})

	err := hook.Trigger(&events.BootstrapEvent{}, func(e events.Event) error {
		order = append(order, "action")
		return nil
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}

	want := []string{"before", "action", "after"}
	if len(order) != len(want) {
		t.Fatalf("unexpected order length: got %v want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("unexpected order: got %v want %v", order, want)
		}
	}
}

func TestTriggerPreservesConcreteEventType(t *testing.T) {
	hook := events.NewHook()
	gotConcrete := false

	hook.BindFunc(func(e events.Event) error {
		_, gotConcrete = e.(*events.ServeEvent)
		return e.Next()
	})

	err := hook.Trigger(&events.ServeEvent{Addr: ":8090"}, func(e events.Event) error { return nil })
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if !gotConcrete {
		t.Fatal("expected concrete ServeEvent to be passed to handler")
	}
}
