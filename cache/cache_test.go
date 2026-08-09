package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/orjanda-framework/orjanda/cache"
)

// Phase 2 Completion Criterion 6:
// cache.Store Get/Set/Delete round-trip correctly with TTL expiry.

func TestLRUStore_GetSetDelete(t *testing.T) {
	ctx := context.Background()
	store := cache.NewLRUStore(128)

	// Set a value.
	err := store.Set(ctx, "key1", []byte("value1"), 0)
	require.NoError(t, err)

	// Get the value.
	val, ok, err := store.Get(ctx, "key1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []byte("value1"), val)

	// Delete the key.
	err = store.Delete(ctx, "key1")
	require.NoError(t, err)

	// Should be a miss now.
	val, ok, err = store.Get(ctx, "key1")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestLRUStore_TTLExpiry(t *testing.T) {
	ctx := context.Background()
	store := cache.NewLRUStore(128)

	// Set with a very short TTL.
	err := store.Set(ctx, "short-lived", []byte("data"), 10*time.Millisecond)
	require.NoError(t, err)

	// Should be present immediately.
	_, ok, err := store.Get(ctx, "short-lived")
	require.NoError(t, err)
	assert.True(t, ok)

	// Wait for expiry.
	time.Sleep(50 * time.Millisecond)

	// Should be a miss now.
	_, ok, err = store.Get(ctx, "short-lived")
	require.NoError(t, err)
	assert.False(t, ok, "entry should be expired")
}

func TestLRUStore_MissReturnsNil(t *testing.T) {
	ctx := context.Background()
	store := cache.NewLRUStore(128)

	val, ok, err := store.Get(ctx, "nonexistent")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestLRUStore_Overwrite(t *testing.T) {
	ctx := context.Background()
	store := cache.NewLRUStore(128)

	require.NoError(t, store.Set(ctx, "k", []byte("v1"), 0))
	require.NoError(t, store.Set(ctx, "k", []byte("v2"), 0))

	val, ok, err := store.Get(ctx, "k")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []byte("v2"), val, "overwrite should update value")
}

func TestLRUStore_Eviction(t *testing.T) {
	ctx := context.Background()
	// Create a store with capacity of 3.
	store := cache.NewLRUStore(3)

	require.NoError(t, store.Set(ctx, "a", []byte("1"), 0))
	require.NoError(t, store.Set(ctx, "b", []byte("2"), 0))
	require.NoError(t, store.Set(ctx, "c", []byte("3"), 0))
	// Adding a 4th should evict the oldest (a).
	require.NoError(t, store.Set(ctx, "d", []byte("4"), 0))

	_, ok, _ := store.Get(ctx, "a")
	assert.False(t, ok, "oldest entry should have been evicted")

	_, ok, _ = store.Get(ctx, "d")
	assert.True(t, ok, "newest entry should still be present")
}

func TestLRUStore_DeleteNonexistent(t *testing.T) {
	ctx := context.Background()
	store := cache.NewLRUStore(128)

	// Delete a key that doesn't exist — should not error.
	err := store.Delete(ctx, "not-here")
	assert.NoError(t, err)
}

func TestLRUStore_ZeroTTL_NeverExpires(t *testing.T) {
	ctx := context.Background()
	store := cache.NewLRUStore(128)

	require.NoError(t, store.Set(ctx, "forever", []byte("yes"), 0))

	// Immediately retrieve — should still be present.
	val, ok, err := store.Get(ctx, "forever")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []byte("yes"), val)
}

func TestLRUStore_ValueIsolation(t *testing.T) {
	ctx := context.Background()
	store := cache.NewLRUStore(128)

	original := []byte("original")
	require.NoError(t, store.Set(ctx, "k", original, 0))

	// Mutate original after Set — cache should be unaffected.
	original[0] = 'X'

	val, ok, err := store.Get(ctx, "k")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, byte('o'), val[0], "cache should store a copy, not a reference")
}
