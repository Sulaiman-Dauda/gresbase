package events

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// Priority levels for hooks.
const (
	PriorityHighest = -100
	PriorityHigh    = -50
	PriorityDefault = 0
	PriorityLow     = 50
	PriorityLowest  = 100
)

// Event is the base interface for all event types.
type Event interface {
	// Next calls the next handler in the chain.
	Next() error

	// IsPropagated returns whether the event should continue propagating.
	IsPropagated() bool

	// Abort stops further propagation.
	Abort()

	baseEvent() *BaseEvent
}

// TagProvider allows events to expose matching tags for tagged handlers.
type TagProvider interface {
	EventTags() []string
}

// BaseEvent provides a default Event implementation.
type BaseEvent struct {
	mu       sync.RWMutex
	aborted  bool
	index    int
	handlers []Handler
	self     Event
}

// Next calls the next handler in the chain.
func (e *BaseEvent) Next() error {
	if e.aborted {
		return nil
	}

	for e.index < len(e.handlers) {
		handler := e.handlers[e.index]
		e.index++
		if handler.Func == nil {
			continue
		}
		if e.self != nil {
			return handler.Func(e.self)
		}
		return handler.Func(e)
	}

	return nil
}

// IsPropagated returns whether the event should continue.
func (e *BaseEvent) IsPropagated() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return !e.aborted
}

// Abort stops further propagation.
func (e *BaseEvent) Abort() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.aborted = true
}

// setHandlers sets the handler chain for this event.
func (e *BaseEvent) setHandlers(handlers []Handler) {
	e.handlers = handlers
	e.index = 0
}

func (e *BaseEvent) setSelf(event Event) {
	e.self = event
}

func (e *BaseEvent) baseEvent() *BaseEvent {
	return e
}

// Handler represents a handler function with metadata.
type Handler struct {
	ID       string
	Func     func(e Event) error
	Priority int
	Tags     []string
}

// Hook manages a chain of event handlers.
type Hook struct {
	mu       sync.RWMutex
	handlers []Handler
}

// TaggedHook binds handlers with one or more static tags.
type TaggedHook struct {
	hook *Hook
	tags []string
}

// NewHook creates a new Hook.
func NewHook() *Hook {
	return &Hook{}
}

// Bind adds a handler to the hook.
func (h *Hook) Bind(handler Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.handlers = append(h.handlers, handler)

	// Sort by priority
	sortHandlers(h.handlers)
}

// BindFunc is a convenience method that binds a function handler with a generated ID.
func (h *Hook) BindFunc(fn func(e Event) error) {
	h.Bind(Handler{
		ID:       generateHandlerID(),
		Func:     fn,
		Priority: PriorityDefault,
	})
}

// Tagged returns a tagged view of the hook that binds handlers with the provided tags.
func (h *Hook) Tagged(tags ...string) *TaggedHook {
	return &TaggedHook{hook: h, tags: tags}
}

// Bind registers the provided handler using the tagged hook tags.
func (th *TaggedHook) Bind(handler Handler) {
	handler.Tags = uniqueTags(append(handler.Tags, th.tags...))
	th.hook.Bind(handler)
}

// BindFunc registers a function handler using the tagged hook tags.
func (th *TaggedHook) BindFunc(fn func(e Event) error) {
	th.Bind(Handler{ID: generateHandlerID(), Func: fn, Priority: PriorityDefault})
}

// Unbind removes a handler by ID.
func (h *Hook) Unbind(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i, handler := range h.handlers {
		if handler.ID == id {
			h.handlers = append(h.handlers[:i], h.handlers[i+1:]...)
			return
		}
	}
}

// Trigger fires the hook with the given event.
func (h *Hook) Trigger(event Event, fn func(e Event) error) error {
	h.mu.RLock()
	handlers := make([]Handler, 0, len(h.handlers)+1)
	eventTags := eventTags(event)
	for _, handler := range h.handlers {
		if shouldRunHandler(handler, eventTags) {
			handlers = append(handlers, handler)
		}
	}
	h.mu.RUnlock()

	if fn != nil {
		handlers = append(handlers, Handler{ID: "__final__", Func: fn, Priority: PriorityLowest})
	}

	// Set up the handler chain
	be := event.baseEvent()
	be.setHandlers(handlers)
	be.setSelf(event)

	return event.Next()
}

// Handlers returns a copy of all handlers.
func (h *Hook) Handlers() []Handler {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]Handler, len(h.handlers))
	copy(result, h.handlers)
	return result
}

// Len returns the number of handlers.
func (h *Hook) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.handlers)
}

func generateHandlerID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// sortHandlers sorts handlers by priority, then by registration order.
func sortHandlers(handlers []Handler) {
	for i := 0; i < len(handlers); i++ {
		for j := i + 1; j < len(handlers); j++ {
			if handlers[i].Priority > handlers[j].Priority {
				handlers[i], handlers[j] = handlers[j], handlers[i]
			}
		}
	}
}

func eventTags(event Event) map[string]struct{} {
	tagger, ok := event.(TagProvider)
	if !ok {
		return nil
	}
	tags := tagger.EventTags()
	if len(tags) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		result[tag] = struct{}{}
	}
	return result
}

func shouldRunHandler(handler Handler, eventTags map[string]struct{}) bool {
	if len(handler.Tags) == 0 {
		return true
	}
	if len(eventTags) == 0 {
		return false
	}
	for _, tag := range handler.Tags {
		if _, ok := eventTags[tag]; ok {
			return true
		}
	}
	return false
}

func uniqueTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result
}
