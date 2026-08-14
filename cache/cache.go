package cache

import (
	"container/list"
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
	Get(ctx context.Context, key string) (val []byte, found bool, err error)
	// Set stores value under key with the given TTL. A zero TTL means
	// the entry never expires.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete removes the entry for key. Not found is not an error.
	Delete(ctx context.Context, key string) error
}

// ----------------------------------------------------------------------------
// In-process LRU Store
// ----------------------------------------------------------------------------

// cacheEntry is a single cache entry.
type cacheEntry struct {
	value   []byte
	expires time.Time // zero means no expiry
}

// lruEntry is a list node's payload: the key (so the list can drive map
// eviction without re-keying) plus its entry.
type lruEntry struct {
	key   string
	entry *cacheEntry
}

// lruStore is a size-bounded, thread-safe in-process LRU cache (TAD §9.1:
// "MVP default: in-process LRU"). A doubly-linked list of keys
// (container/list) plus a key→node map gives O(1) Get/Set: a hit moves its
// node to the front, and eviction removes the back node — the least recently
// used. TTL expiry is lazy: an expired entry is dropped on the next Get, and
// as dead weight it is equally free as an eviction victim once it reaches the
// back (REVIEW-2026-08-12 finding 14).
type lruStore struct {
	mu       sync.Mutex
	maxItems int
	items    map[string]*list.Element
	order    *list.List
}

// NewLRUStore creates an in-process LRU cache with the given maximum entry
// count. When the store is full, the least-recently-used entry is evicted.
func NewLRUStore(maxItems int) Store {
	if maxItems <= 0 {
		maxItems = 1024
	}
	return &lruStore{
		maxItems: maxItems,
		items:    make(map[string]*list.Element, maxItems),
		order:    list.New(),
	}
}

// Get retrieves a value from the cache. A hit refreshes the entry's recency,
// so a frequently-read key outlives a write-once key (true LRU). Get takes
// the write lock because refreshing recency mutates the list.
func (s *lruStore) Get(_ context.Context, key string) (val []byte, found bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	el, ok := s.items[key]
	if !ok {
		return nil, false, nil
	}
	if s.expired(el) {
		// Expired — delete lazily.
		s.removeElement(el)
		return nil, false, nil
	}
	s.order.MoveToFront(el)
	cp := make([]byte, len(el.Value.(*lruEntry).entry.value))
	copy(cp, el.Value.(*lruEntry).entry.value)
	return cp, true, nil
}

// Set stores a value in the cache. A new key evicts the least-recently-used
// entry when the store is full; overwriting an existing key refreshes its
// recency in place — both O(1), no list scan (REVIEW-2026-08-12 finding 14).
func (s *lruStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cp := make([]byte, len(value))
	copy(cp, value)
	var expires time.Time
	if ttl > 0 {
		expires = time.Now().Add(ttl)
	}

	if el, ok := s.items[key]; ok {
		// Existing key: update in place and refresh recency.
		e := el.Value.(*lruEntry).entry
		e.value = cp
		e.expires = expires
		s.order.MoveToFront(el)
		return nil
	}

	if len(s.items) >= s.maxItems {
		s.evictLRU()
	}
	s.items[key] = s.order.PushFront(&lruEntry{
		key:   key,
		entry: &cacheEntry{value: cp, expires: expires},
	})
	return nil
}

// Delete removes a cache entry.
func (s *lruStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.items[key]; ok {
		s.removeElement(el)
	}
	return nil
}

// expired reports whether the node's entry has passed its TTL.
func (s *lruStore) expired(el *list.Element) bool {
	e := el.Value.(*lruEntry).entry
	return !e.expires.IsZero() && time.Now().After(e.expires)
}

// removeElement unlinks a node from both the list and the map.
func (s *lruStore) removeElement(el *list.Element) {
	delete(s.items, el.Value.(*lruEntry).key)
	s.order.Remove(el)
}

// evictLRU removes the least-recently-used entry (the list's back node).
// Must be called with the lock held and only when at capacity. An expired
// entry at the back is dead weight and is evicted as freely as a live one —
// it would miss on Get anyway.
func (s *lruStore) evictLRU() {
	if back := s.order.Back(); back != nil {
		s.removeElement(back)
	}
}
