package event

import (
	"context"
	"fmt"
	"sync"

	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// Handler is a lifecycle callback. It receives the current document state as
// a map (column-keyed) and may return an error to abort the operation.
// See TAD §2.5 and PRD §19.1.
type Handler func(ctx context.Context, doc map[string]any) error

// Bus is the in-process, synchronous event dispatcher for Document lifecycle
// events. See TAD §2.5.
type Bus interface {
	// On registers handler h for (docType, eventName).
	// Handlers are called in registration order within the same app;
	// cross-app ordering follows the app dependency graph (TAD §7.1 step 3).
	On(docType, eventName string, h Handler)

	// Emit fires all registered handlers for (docType, eventName) in order.
	// If any handler returns a non-nil error, Emit stops and returns that error
	// immediately — subsequent handlers are not called (PRD §19.2 semantics).
	Emit(ctx context.Context, docType, eventName string, doc map[string]any) error
}

// ---------------------------------------------------------------------------
// Event name constants (PRD §19.1 table).
// ---------------------------------------------------------------------------

const (
	EventBeforeValidate = "before_validate"
	EventAfterValidate  = "after_validate"
	EventBeforeInsert   = "before_insert"
	EventBeforeSave     = "before_save"
	EventAfterInsert    = "after_insert"
	EventAfterSave      = "after_save"
	EventBeforeUpdate   = "before_update"
	EventAfterUpdate    = "after_update"
	EventBeforeDelete   = "before_delete"
	EventAfterDelete    = "after_delete"
	EventOnSubmit       = "on_submit"
	EventOnCancel       = "on_cancel"
	EventOnWorkflow     = "on_workflow_transition"
)

// ---------------------------------------------------------------------------
// bus — concrete implementation
// ---------------------------------------------------------------------------

type registration struct {
	docType   string
	eventName string
	handler   Handler
}

type bus struct {
	mu       sync.RWMutex
	handlers []registration // ordered slice; append-order == registration order
}

// NewBus creates a new, empty event Bus.
func NewBus() Bus {
	return &bus{}
}

func (b *bus) On(docType, eventName string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, registration{
		docType:   docType,
		eventName: eventName,
		handler:   h,
	})
}

func (b *bus) Emit(ctx context.Context, docType, eventName string, doc map[string]any) error {
	b.mu.RLock()
	handlers := make([]Handler, 0)
	for _, r := range b.handlers {
		if r.docType == docType && r.eventName == eventName {
			handlers = append(handlers, r.handler)
		}
	}
	b.mu.RUnlock()

	for _, h := range handlers {
		if err := h(ctx, doc); err != nil {
			// Wrap in a validation error if not already an orjanda error,
			// so the Document Engine can surface a clean message.
			var ojErr orjerrors.Error
			if orjerrors.As(err, &ojErr) {
				return ojErr
			}
			return orjerrors.New(orjerrors.CodeValidation,
				fmt.Sprintf("hook %q/%q returned error: %v", docType, eventName, err),
				nil, err)
		}
	}
	return nil
}
