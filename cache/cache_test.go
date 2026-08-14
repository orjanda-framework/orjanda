package cache_test

import (
	"context"
	"fmt"
	"sync"
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

// TestLRUStore_GetRefreshesRecency is the finding-14 regression test: a Get
// must make its key the most-recently-used, so the next eviction takes the
// *least recently used* entry — not the oldest-inserted one. The old
// FIFO-with-re-append implementation never reordered on Get and evicted "a"
// here (insertion order); a true LRU evicts "b".
func TestLRUStore_GetRefreshesRecency(t *testing.T) {
	ctx := context.Background()
	store := cache.NewLRUStore(3)

	require.NoError(t, store.Set(ctx, "a", []byte("1"), 0))
	require.NoError(t, store.Set(ctx, "b", []byte("2"), 0))
	require.NoError(t, store.Set(ctx, "c", []byte("3"), 0))

	// Touching "a" makes it the most-recently-used; "b" is now the LRU.
	_, ok, err := store.Get(ctx, "a")
	require.NoError(t, err)
	assert.True(t, ok)

	// Inserting a 4th key must evict "b", not "a".
	require.NoError(t, store.Set(ctx, "d", []byte("4"), 0))

	_, ok, _ = store.Get(ctx, "a")
	assert.True(t, ok, "recently-read key must survive eviction (true LRU)")
	_, ok, _ = store.Get(ctx, "b")
	assert.False(t, ok, "least-recently-used key must be evicted")
	_, ok, _ = store.Get(ctx, "d")
	assert.True(t, ok, "newest entry must be present")
}

// TestLRUStore_OverwriteRefreshesRecency proves a Set on an existing key both
// updates the value and refreshes recency (O(1), no list scan).
func TestLRUStore_OverwriteRefreshesRecency(t *testing.T) {
	ctx := context.Background()
	store := cache.NewLRUStore(3)

	require.NoError(t, store.Set(ctx, "a", []byte("1"), 0))
	require.NoError(t, store.Set(ctx, "b", []byte("2"), 0))
	require.NoError(t, store.Set(ctx, "c", []byte("3"), 0))

	// Overwriting "a" makes it most-recently-used.
	require.NoError(t, store.Set(ctx, "a", []byte("1x"), 0))

	val, ok, err := store.Get(ctx, "a")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []byte("1x"), val, "overwrite must update the value")

	require.NoError(t, store.Set(ctx, "d", []byte("4"), 0))
	_, ok, _ = store.Get(ctx, "a")
	assert.True(t, ok, "overwritten key must survive eviction (recency refreshed)")
	_, ok, _ = store.Get(ctx, "b")
	assert.False(t, ok, "least-recently-used key must be evicted")
}

// TestLRUStore_ExpiredEntriesDoNotBlockCapacity proves expired entries are
// dead weight: once they reach the back they are evicted freely, and fresh
// inserts are never blocked by expired entries occupying capacity.
func TestLRUStore_ExpiredEntriesDoNotBlockCapacity(t *testing.T) {
	ctx := context.Background()
	store := cache.NewLRUStore(3)

	require.NoError(t, store.Set(ctx, "a", []byte("1"), 10*time.Millisecond))
	require.NoError(t, store.Set(ctx, "b", []byte("2"), 10*time.Millisecond))
	require.NoError(t, store.Set(ctx, "c", []byte("3"), 10*time.Millisecond))
	time.Sleep(50 * time.Millisecond)

	// All expired. Inserting live entries evicts the dead back nodes.
	require.NoError(t, store.Set(ctx, "d", []byte("4"), 0))
	require.NoError(t, store.Set(ctx, "e", []byte("5"), 0))
	require.NoError(t, store.Set(ctx, "f", []byte("6"), 0))

	for _, k := range []string{"d", "e", "f"} {
		_, ok, err := store.Get(ctx, k)
		require.NoError(t, err)
		assert.True(t, ok, "live entry %s must be present", k)
	}
	for _, k := range []string{"a", "b", "c"} {
		_, ok, _ := store.Get(ctx, k)
		assert.False(t, ok, "expired entry %s must be gone", k)
	}
}

// TestLRUStore_Concurrent hammers Get/Set/Delete from many goroutines to
// prove the store is safe under concurrency (run with -race).
func TestLRUStore_Concurrent(t *testing.T) {
	ctx := context.Background()
	store := cache.NewLRUStore(64)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				key := fmt.Sprintf("key-%d", id*100+i%80)
				switch i % 3 {
				case 0:
					_ = store.Set(ctx, key, []byte(key), time.Duration(i%10)*time.Millisecond)
				case 1:
					_, _, _ = store.Get(ctx, key)
				default:
					_ = store.Delete(ctx, key)
				}
			}
		}(g)
	}
	wg.Wait()
}
