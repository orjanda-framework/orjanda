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
