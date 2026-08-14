package event_test

import (
	"context"
	"errors"
	"testing"

	"github.com/orjanda-framework/orjanda/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventBus_RegistrationAndEmit(t *testing.T) {
	bus := event.NewBus()
	ctx := context.Background()

	var order []string

	bus.On("LeaveRequest", event.EventBeforeSave, func(ctx context.Context, doc map[string]any) error {
		order = append(order, "handler1")
		return nil
	})

	bus.On("LeaveRequest", event.EventBeforeSave, func(ctx context.Context, doc map[string]any) error {
		order = append(order, "handler2")
		return nil
	})

	err := bus.Emit(ctx, "LeaveRequest", event.EventBeforeSave, map[string]any{"id": "123"})
	require.NoError(t, err)
	assert.Equal(t, []string{"handler1", "handler2"}, order)
}

func TestEventBus_FailFastAbortsRemainingHandlers(t *testing.T) {
	bus := event.NewBus()
	ctx := context.Background()

	var order []string

	bus.On("LeaveRequest", event.EventBeforeSave, func(ctx context.Context, doc map[string]any) error {
		order = append(order, "handler1")
		return errors.New("something went wrong")
	})

	bus.On("LeaveRequest", event.EventBeforeSave, func(ctx context.Context, doc map[string]any) error {
		order = append(order, "handler2")
		return nil
	})

	err := bus.Emit(ctx, "LeaveRequest", event.EventBeforeSave, map[string]any{"id": "123"})
	require.Error(t, err)
	assert.Equal(t, []string{"handler1"}, order, "handler2 must not be called after handler1 fails")
}

// TestEventBus_CrossAppOrderingIsRegistrationOrder pins the ordering contract
// TAD §7.1 step 3 relies on: the Bus has no notion of Applications — a shared
// {docType, event} pair executes handlers in registration order. The framework
// wiring (app.ResolveDAG + orjanda/testing.NewTestSite) installs a
// dependency's hooks before its dependent's, which is what makes the
// dependency's hook run first and lets the dependent observe/override its
// state (PRD §34.2 item 5). Registering in the reverse order must reverse
// execution, because the Bus does not and cannot know the app graph
// (REVIEW-2026-08-12 finding 15).
func TestEventBus_CrossAppOrderingIsRegistrationOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("dependency-first registration runs dependency hooks first", func(t *testing.T) {
		bus := event.NewBus()
		var order []string

		// App A (dependency) is installed first (TAD §7.1 step 2 resolved
		// order) and registers its hook on the Employee document.
		bus.On("Employee", event.EventBeforeSave, func(_ context.Context, doc map[string]any) error {
			doc["normalized"] = "A-set"
			order = append(order, "dep")
			return nil
		})
		// App B (dependent) is installed second and registers on the same pair.
		bus.On("Employee", event.EventBeforeSave, func(_ context.Context, doc map[string]any) error {
			doc["normalized"] = "B-overrode"
			order = append(order, "dependent")
			return nil
		})

		doc := map[string]any{}
		require.NoError(t, bus.Emit(ctx, "Employee", event.EventBeforeSave, doc))
		assert.Equal(t, []string{"dep", "dependent"}, order,
			"dependency-resolved install order must become execution order")
		assert.Equal(t, "B-overrode", doc["normalized"],
			"the later (dependent) hook must be able to override the dependency's state")
	})

	t.Run("reverse registration reverses execution", func(t *testing.T) {
		bus := event.NewBus()
		var order []string

		bus.On("Employee", event.EventBeforeSave, func(context.Context, map[string]any) error {
			order = append(order, "dependent")
			return nil
		})
		bus.On("Employee", event.EventBeforeSave, func(context.Context, map[string]any) error {
			order = append(order, "dep")
			return nil
		})

		require.NoError(t, bus.Emit(ctx, "Employee", event.EventBeforeSave, map[string]any{}))
		assert.Equal(t, []string{"dependent", "dep"}, order,
			"execution order is purely registration order; nothing else may reorder it")
	})
}
