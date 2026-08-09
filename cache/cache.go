package cache

import (
	"context"
	"sync"
	"time"
)

// Store is the cache interface used throughout Orjanda.
// The in-process LRU default is provided by NewLRUStore.
// A Redis-backed Store is a drop-in replacement for horizontal scaling.
// See TAD §9.1 and PRD §33.2.
type Store interface {
	// Get retrieves the value for key. Returns (value, true, nil) on hit,
	// (nil, false, nil) on miss, and (nil, false, err) on error.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// Set stores value under key with the given TTL. A zero TTL means
	// the entry never expires.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete removes the entry for key. Not found is not an error.
	Delete(ctx context.Context, key string) error
}

// ----------------------------------------------------------------------------
// In-process LRU Store
// ----------------------------------------------------------------------------

// entry is a single cache entry.
type entry struct {
	value   []byte
	expires time.Time // zero means no expiry
}

// lruStore is a simple, size-bounded in-process LRU cache.
// The implementation uses a doubly-linked list of keys inside a map;
// for MVP the LRU eviction is approximated by a fixed-size FIFO (insertion
// order) because full LRU requires a doubly-linked list or container/list
// which adds complexity without measurable benefit at MVP scale.
type lruStore struct {
	mu       sync.RWMutex
	maxItems int
	items    map[string]*entry
	order    []string // insertion order, used for FIFO eviction
}

// NewLRUStore creates an in-process LRU cache with the given maximum entry count.
// When the store is full, the least-recently-added entry is evicted.
func NewLRUStore(maxItems int) Store {
	if maxItems <= 0 {
		maxItems = 1024
	}
	return &lruStore{
		maxItems: maxItems,
		items:    make(map[string]*entry, maxItems),
		order:    make([]string, 0, maxItems),
	}
}

// Get retrieves a value from the cache.
func (s *lruStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.RLock()
	e, ok := s.items[key]
	s.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	if !e.expires.IsZero() && time.Now().After(e.expires) {
		// Expired — delete lazily.
		s.mu.Lock()
		delete(s.items, key)
		s.mu.Unlock()
		return nil, false, nil
	}
	// Return a copy to prevent mutation of cached bytes.
	cp := make([]byte, len(e.value))
	copy(cp, e.value)
	return cp, true, nil
}

// Set stores a value in the cache.
func (s *lruStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Evict if at capacity and key is new.
	if _, exists := s.items[key]; !exists && len(s.items) >= s.maxItems {
		s.evictOldest()
	}

	var expires time.Time
	if ttl > 0 {
		expires = time.Now().Add(ttl)
	}

	cp := make([]byte, len(value))
	copy(cp, value)
	s.items[key] = &entry{value: cp, expires: expires}

	if _, exists := s.items[key]; !exists {
		s.order = append(s.order, key)
	} else {
		// Key already tracked; update in-place (no re-ordering for simplicity).
		// Re-add to end for LRU approximation.
		s.removeFromOrder(key)
		s.order = append(s.order, key)
	}

	return nil
}

// Delete removes a cache entry.
func (s *lruStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
	s.removeFromOrder(key)
	return nil
}

// evictOldest removes the oldest insertion-order entry. Must be called with
// the write lock held.
func (s *lruStore) evictOldest() {
	for len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		if _, ok := s.items[oldest]; ok {
			delete(s.items, oldest)
			return
		}
	}
}

// removeFromOrder removes a key from the order slice.
func (s *lruStore) removeFromOrder(key string) {
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}
