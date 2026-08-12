package runtime

import (
	"sync"

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
}

func newSession(id auth.Identity) *Session {
	return &Session{
		ID:     ulid.Make().String(),
		UserID: id.UserID,
		seen:   make(map[string]bool),
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

// SessionManager owns live sessions keyed by id.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionManager builds an empty SessionManager.
func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session)}
}

// New creates and registers a session bound to an identity.
func (m *SessionManager) New(id auth.Identity) *Session {
	s := newSession(id)
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s
}

// Get returns a registered session by id, or nil.
func (m *SessionManager) Get(id string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

// Reset clears all sessions (test helper).
func (m *SessionManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = make(map[string]*Session)
}
