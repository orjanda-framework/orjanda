package runtime_test

import (
	"runtime"
	"sync"
	"testing"
	"time"

	agentruntime "github.com/orjanda-framework/orjanda/agent/runtime"
	"github.com/orjanda-framework/orjanda/auth"
)

// TestSessionManagerTTL_EvictsIdleSession proves the finding-3 fix: a session
// untouched for the TTL is evicted, so the SessionManager's memory stays
// bounded by live conversations instead of growing without limit.
func TestSessionManagerTTL_EvictsIdleSession(t *testing.T) {
	m := agentruntime.NewSessionManagerWithTTL(60 * time.Millisecond)
	s := m.New(auth.Identity{UserID: "u-1"})

	if got := m.Get(s.ID); got == nil {
		t.Fatal("session must be retrievable before the TTL elapses")
	}

	time.Sleep(80 * time.Millisecond)

	if got := m.Get(s.ID); got != nil {
		t.Fatal("session must be evicted after the TTL elapses")
	}
	if got := m.Len(); got != 0 {
		t.Fatalf("Len() = %d after eviction, want 0", got)
	}
}

// TestSessionManagerTTL_GetTouchesLastAccess proves eviction runs on
// *inactivity*: a Get within the TTL resets the clock, so the session
// outlives its original expiry.
func TestSessionManagerTTL_GetTouchesLastAccess(t *testing.T) {
	m := agentruntime.NewSessionManagerWithTTL(60 * time.Millisecond)
	s := m.New(auth.Identity{UserID: "u-1"})

	time.Sleep(30 * time.Millisecond)
	if m.Get(s.ID) == nil {
		t.Fatal("session vanished before TTL")
	}

	// 40ms after the touch: past the original creation+TTL, still inside the
	// touch+TTL window — the session must survive. EvictExpired does not
	// touch, so a zero count proves this specific session is still alive.
	time.Sleep(40 * time.Millisecond)
	if got := m.EvictExpired(); got != 0 {
		t.Fatalf("EvictExpired() = %d, want 0: a Get within the TTL must extend the session's life", got)
	}

	// Past the touched expiry now: the untouched-again session must be evicted.
	time.Sleep(40 * time.Millisecond)
	if got := m.EvictExpired(); got != 1 {
		t.Fatalf("EvictExpired() = %d, want 1 after a full TTL without a touch", got)
	}
	if m.Get(s.ID) != nil {
		t.Fatal("session must expire after a full TTL without a touch")
	}
}

// TestSessionManagerNoTTL_KeepsSessions proves the default manager (zero TTL)
// preserves the pre-fix behavior: sessions never expire on their own.
func TestSessionManagerNoTTL_KeepsSessions(t *testing.T) {
	m := agentruntime.NewSessionManager()
	s := m.New(auth.Identity{UserID: "u-1"})

	time.Sleep(80 * time.Millisecond)

	if m.Get(s.ID) == nil {
		t.Fatal("a zero-TTL SessionManager must not evict sessions")
	}
	if got := m.EvictExpired(); got != 0 {
		t.Fatalf("EvictExpired() on a zero-TTL manager = %d, want 0", got)
	}
}

// TestSessionManagerEvictExpired_SweepsAndCounts proves the explicit sweep
// removes exactly the expired sessions and reports how many.
func TestSessionManagerEvictExpired_SweepsAndCounts(t *testing.T) {
	m := agentruntime.NewSessionManagerWithTTL(100 * time.Millisecond)
	m.New(auth.Identity{UserID: "u-1"}) // created first; expires first

	time.Sleep(60 * time.Millisecond)
	live := m.New(auth.Identity{UserID: "u-2"}) // created second; stays alive

	time.Sleep(60 * time.Millisecond) // u-1 is now past its TTL, u-2 is not

	if got := m.EvictExpired(); got != 1 {
		t.Fatalf("EvictExpired() = %d, want 1 (only the expired session)", got)
	}
	if m.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", m.Len())
	}
	if m.Get(live.ID) == nil {
		t.Fatal("the still-live session must survive the sweep")
	}
}

// TestSessionManagerNew_SweepsOpportunistically proves New keeps the live set
// within one TTL window: creating a session after expired ones are idle evicts
// them without an explicit sweep.
func TestSessionManagerNew_SweepsOpportunistically(t *testing.T) {
	m := agentruntime.NewSessionManagerWithTTL(60 * time.Millisecond)
	old1 := m.New(auth.Identity{UserID: "u-1"})
	old2 := m.New(auth.Identity{UserID: "u-2"})

	time.Sleep(80 * time.Millisecond)
	m.New(auth.Identity{UserID: "u-3"}) // triggers the sweep

	if m.Len() != 1 {
		t.Fatalf("Len() = %d after opportunistic sweep, want 1", m.Len())
	}
	if m.Get(old1.ID) != nil || m.Get(old2.ID) != nil {
		t.Fatal("idle sessions must be swept when a new session is created")
	}
}

// TestSessionManagerConcurrent proves Get/New/EvictExpired are safe under
// concurrent access (run under -race).
func TestSessionManagerConcurrent(t *testing.T) {
	m := agentruntime.NewSessionManagerWithTTL(5 * time.Millisecond)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				s := m.New(auth.Identity{UserID: "u"})
				_ = m.Get(s.ID)
				m.Get("nonexistent")
				m.EvictExpired()
				runtime.Gosched()
			}
		}(i)
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
