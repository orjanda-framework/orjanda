package runtime

import (
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/auth"
)

// Session carries one agent conversation's state: the message transcript, the
// DocTypes that have appeared (the input to the TAD §11.1 discovery/operation
// tool split), and the record count of the most recent list/search result
// (the input to the TAD §12.1 bulk approval check).
type Session struct {
	// ID is a ULID identifying the session.
	ID string
	// UserID is the identity the session is bound to; an identity change
	// forces a fresh session (isolation).
	UserID string

	mu          sync.Mutex
	transcript  []llm.Message
	seen        map[string]bool // snake_case DocType keys
	targetCount int             // record count of the most recent list/search result
	targetDoc   string          // snake_case DocType that targetCount belongs to

	// lastAccess is touched by the SessionManager on every Get; it is the
	// inactivity clock the TTL eviction runs on.
	lastAccess time.Time
}

func newSession(id auth.Identity) *Session {
	return &Session{
		ID:         ulid.Make().String(),
		UserID:     id.UserID,
		seen:       make(map[string]bool),
		lastAccess: time.Now(),
	}
}

func (s *Session) addMessage(m llm.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcript = append(s.transcript, m)
}

// Transcript returns a copy of the message history.
func (s *Session) Transcript() []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]llm.Message(nil), s.transcript...)
}

// markSeen records that a DocType (snake_case) has appeared in the session,
// which attaches its operation tools to the next LLM call (TAD §11.1).
func (s *Session) markSeen(docType string) {
	if docType == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[docType] = true
}

// seenDocType reports whether the DocType's operation tools should be attached.
func (s *Session) seenDocType(docType string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[docType]
}

func (s *Session) setTargetCount(docType string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targetDoc = docType
	s.targetCount = n
}

// TargetCount returns the record count of the most recent list/search result
// (TAD §12.1 step 2), or 0 when docType does not match the DocType the count
// was recorded for. Scoping the count to its own DocType prevents a large
// list of one Document type from tripping the bulk approval on a later,
// unrelated read or write (TAD §12.1 step 2 applies the bulk check to the
// records the current call affects).
func (s *Session) TargetCount(docType string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if docType == "" || s.targetDoc != docType {
		return 0
	}
	return s.targetCount
}

// SessionManager owns live sessions keyed by id. Sessions are evicted once
// they go untouched for the manager's TTL (a zero TTL disables expiry), which
// bounds the memory an idle or abandoned conversation holds. Eviction runs
// lazily on New/Get and on explicit EvictExpired sweeps.
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewSessionManager builds a SessionManager with no TTL (sessions never
// expire on their own).
func NewSessionManager() *SessionManager {
	return NewSessionManagerWithTTL(0)
}

// NewSessionManagerWithTTL builds a SessionManager that evicts sessions
// untouched for ttl. A ttl <= 0 disables expiry.
func NewSessionManagerWithTTL(ttl time.Duration) *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session), ttl: ttl}
}

// New creates and registers a session bound to an identity. It opportunistically
// sweeps expired sessions so the live set stays within one TTL window of the
// session creation rate.
func (m *SessionManager) New(id auth.Identity) *Session {
	s := newSession(id)
	m.mu.Lock()
	m.evictExpiredLocked(time.Now())
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s
}

// Get returns a registered session by id, or nil. A hit touches the session's
// last-access clock; an expired session is evicted and returns nil.
func (m *SessionManager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return nil
	}
	now := time.Now()
	if m.expired(s, now) {
		delete(m.sessions, id)
		return nil
	}
	s.lastAccess = now
	return s
}

// EvictExpired removes every session whose last access predates the TTL and
// returns how many were evicted. It is called automatically (lazily by
// New/Get) and is exported so a caller can sweep on a timer without a lookup.
func (m *SessionManager) EvictExpired() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.evictExpiredLocked(time.Now())
}

// Remove deletes a session from the manager by id (no-op when unknown). The
// WebSocket handler calls it when a connection closes so an abandoned
// conversation is released immediately instead of lingering for the TTL
// (REVIEW-2026-08-12 finding 13).
func (m *SessionManager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

func (m *SessionManager) evictExpiredLocked(now time.Time) int {
	if m.ttl <= 0 {
		return 0
	}
	evicted := 0
	for id, s := range m.sessions {
		if m.expired(s, now) {
			delete(m.sessions, id)
			evicted++
		}
	}
	return evicted
}

func (m *SessionManager) expired(s *Session, now time.Time) bool {
	return m.ttl > 0 && now.Sub(s.lastAccess) > m.ttl
}

// Len returns the number of live (not yet evicted) sessions (test helper).
func (m *SessionManager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// Reset clears all sessions (test helper).
func (m *SessionManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = make(map[string]*Session)
}
