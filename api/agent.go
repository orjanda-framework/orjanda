package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/orjanda-framework/orjanda/agent/runtime"
	"github.com/orjanda-framework/orjanda/agent/safety"
	"github.com/orjanda-framework/orjanda/auth"
)

// defaultApprovalTimeout bounds a single approval_required round trip on an
// otherwise idle connection: a human who never answers must not pin the turn
// (and its goroutine) forever. TAD §12.3 is silent on a timeout, so this is the
// framework's default (REVIEW-2026-08-12 finding 13).
const defaultApprovalTimeout = 5 * time.Minute

// maxQueuedTurns bounds the number of message turns awaiting execution per
// connection. The read loop enqueues non-blockingly and drops messages beyond
// this cap, so a flooding client cannot grow memory or goroutines without
// bound; the runtime's per-user safety rate limit still applies to every turn
// that does execute (REVIEW-2026-08-12 finding 13).
const maxQueuedTurns = 16

// AgentHandler serves the agent chat WebSocket endpoint (TAD §6.2):
// WS /api/v1/agent/stream. Client → server messages follow the §6.2 contract
// (message / approval_response); the runtime's streaming events are forwarded
// verbatim as server → client messages, including the extended §12.3
// approval_required payload with policy_reason.
type AgentHandler struct {
	// Base holds the runtime options shared across connections. Sink and
	// Approvals are per-connection and filled in by Stream.
	Base runtime.Options
	// AllowedOrigins is the browser-origin allowlist for the upgrade request,
	// sourced from the same CORSOrigins list that governs the HTTP API
	// (PRD §12.2). It backs the WebSocket origin check; "*" allows any origin.
	AllowedOrigins []string
	// ApprovalTimeout bounds one approval round trip on an idle connection
	// (0 = defaultApprovalTimeout).
	ApprovalTimeout time.Duration
}

// wsClientMessage is a client → server message (TAD §6.2). Payload carries the
// human's Modify substitution on an approval_response (PRD §38.2).
type wsClientMessage struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	ActionID string         `json:"action_id,omitempty"`
	Approved bool           `json:"approved,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
}

// authorizeOrigin enforces the same origin policy as the CORS middleware
// (api/middleware/cors.go, PRD §12.2) on the WebSocket upgrade. Browsers send
// an Origin header on every WS upgrade; a cross-origin attacker page would too,
// so a mismatched origin is rejected unless it appears on the configured
// allowlist ("*" allows any). Requests without an Origin header (CLI, tests)
// are not browser-initiated and cannot be hijacked via a page, so they pass.
// This closes the cross-site WebSocket hijacking vector the
// AcceptOptions.InsecureSkipVerify flag previously left wide open
// (REVIEW-2026-08-12 finding 13).
func authorizeOrigin(r *http.Request, allowed []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if u, err := url.Parse(origin); err == nil && strings.EqualFold(u.Host, r.Host) {
		return true // same-origin
	}
	for _, o := range allowed {
		if o == "*" || strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

// Stream upgrades the connection and runs the §6.2 message loop.
func (h *AgentHandler) Stream(w http.ResponseWriter, r *http.Request) {
	if !authorizeOrigin(r, h.AllowedOrigins) {
		http.Error(w, "websocket origin not allowed", http.StatusForbidden)
		return
	}
	// Origin is verified above against the CORS allowlist, including the "*"
	// wildcard that coder/websocket's glob-based OriginPatterns cannot express;
	// the AcceptOptions origin check is therefore intentionally disabled.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		slog.Warn("agent.ws.accept", "error", err)
		return
	}
	defer func() { _ = c.Close(websocket.StatusInternalError, "closing") }()

	// The execution context is the cancellable request context: when the
	// connection closes, Read fails, cancel() fires, and the turn worker plus
	// any in-flight approval round trips abort instead of leaking. The old
	// context.WithoutCancel stripped that cancellation, so an approval whose
	// client never answered (or a turn on a dropped connection) blocked the
	// goroutine forever (REVIEW-2026-08-12 finding 13).
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sink := &wsSink{ctx: ctx, c: c}
	timeout := h.ApprovalTimeout
	if timeout <= 0 {
		timeout = defaultApprovalTimeout
	}
	gw := &wsGateway{
		pending: make(map[string]chan runtime.ApprovalResponse),
		early:   make(map[string]runtime.ApprovalResponse),
		timeout: timeout,
	}

	opts := h.Base
	opts.Sink = sink
	opts.Approvals = gw
	rt, err := runtime.NewRuntime(opts)
	if err != nil {
		sink.Send(runtime.Event{Type: runtime.EventToolEnd, Content: "error: " + err.Error()})
		return
	}

	id := auth.FromContext(r.Context())
	idCtx := auth.NewContext(ctx, id)

	// One session per connection (TAD §11.1/§12.1 continuity): a turn may
	// reference an earlier turn's result, so the transcript, seen DocTypes,
	// and target count must survive across the connection's messages. The
	// session is released when the connection closes rather than lingering
	// for the SessionTTL (REVIEW-2026-08-12 finding 13).
	sess := rt.NewSession(id)
	defer rt.RemoveSession(sess.ID)
	sessionCtx := safety.WithSession(idCtx, sess.ID)

	// Turns run on a single worker draining a bounded queue. Turns were already
	// serialized (a turn may reference the previous one), so one worker replaces
	// the goroutine-per-message pattern that let a flooding client spawn
	// unbounded goroutines blocked on the turn mutex; the queue cap is the
	// per-connection rate limit (REVIEW-2026-08-12 finding 13). The worker stops
	// the moment the connection context cancels.
	turns := make(chan wsClientMessage, maxQueuedTurns)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-turns:
				if ctx.Err() != nil {
					return
				}
				if _, err := rt.Execute(sessionCtx, msg.Text); err != nil {
					sink.Send(runtime.Event{Type: runtime.EventToolEnd, Content: "error: " + err.Error()})
				}
			}
		}
	}()

	for {
		_, raw, err := c.Read(ctx)
		if err != nil {
			return
		}
		var msg wsClientMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			slog.Warn("agent.ws.unmarshal", "error", err)
			continue
		}
		switch msg.Type {
		case "message":
			// Non-blocking enqueue: a saturated worker means the client is
			// flooding, so the excess message is dropped with an event instead
			// of buffered without bound.
			select {
			case turns <- msg:
			default:
				sink.Send(runtime.Event{Type: runtime.EventToolEnd, Content: "too many pending requests; try again"})
			}
		case "approval_response":
			// Handled inline, not queued behind turns: it unblocks a pending
			// approval round trip and must never wait behind the worker.
			if msg.ActionID == "" {
				continue
			}
			gw.submit(runtime.ApprovalResponse{
				ActionID: msg.ActionID,
				Approved: msg.Approved,
				Payload:  msg.Payload,
			})
		}
	}
}

// wsSink forwards runtime events to the connection as §6.2 server messages.
type wsSink struct {
	ctx context.Context
	c   *websocket.Conn
	mu  sync.Mutex
}

func (s *wsSink) Send(evt runtime.Event) {
	if s.ctx.Err() != nil {
		return // connection is closing; drop rather than log noise
	}
	raw, err := json.Marshal(evt)
	if err != nil {
		slog.Warn("agent.ws.marshal", "error", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.c.Write(s.ctx, websocket.MessageText, raw); err != nil {
		if s.ctx.Err() == nil {
			slog.Warn("agent.ws.write", "error", err)
		}
	}
}

// wsGateway blocks on an approval_required round trip until the matching
// approval_response arrives on the connection (TAD §12.3). The round trip is
// bounded both by the connection context (cancelled when the connection
// closes) and by timeout, so a client that neither answers nor disconnects
// cannot pin the turn forever (REVIEW-2026-08-12 finding 13).
//
// Responses may race ahead of the request's registration: the runtime emits
// approval_required and only then calls RequestApproval, so a client that
// answers during the gap would otherwise have its response dropped and the
// round trip stall until timeout. early buffers those out-of-order responses
// for consumption on registration.
type wsGateway struct {
	mu      sync.Mutex
	pending map[string]chan runtime.ApprovalResponse
	early   map[string]runtime.ApprovalResponse
	timeout time.Duration
}

// maxEarlyResponses bounds the early-arrival buffer. Turns on a connection are
// serialized by the single worker, so at most one approval round trip is
// pending at a time; 16 generously covers responses that raced ahead while
// keeping a client that floods unknown action ids from growing the buffer
// without bound (REVIEW-2026-08-12 finding 13).
const maxEarlyResponses = 16

func (g *wsGateway) RequestApproval(ctx context.Context, payload runtime.ApprovalPayload) (runtime.ApprovalResponse, error) {
	ch := make(chan runtime.ApprovalResponse, 1)
	g.mu.Lock()
	if resp, ok := g.early[payload.ActionID]; ok {
		delete(g.early, payload.ActionID)
		g.mu.Unlock()
		return resp, nil
	}
	g.pending[payload.ActionID] = ch
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.pending, payload.ActionID)
		g.mu.Unlock()
	}()

	var timeout <-chan time.Time
	if g.timeout > 0 {
		t := time.NewTimer(g.timeout)
		defer t.Stop()
		timeout = t.C
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return runtime.ApprovalResponse{}, ctx.Err()
	case <-timeout:
		return runtime.ApprovalResponse{}, fmt.Errorf("approval %s timed out after %s", payload.ActionID, g.timeout)
	}
}

// submit delivers an approval_response to the matching pending request,
// buffering it when the request has not registered yet so an early answer is
// not dropped (it is consumed by RequestApproval on registration). It reports
// whether the response was accepted; a full early buffer is the only case in
// which one is dropped.
func (g *wsGateway) submit(resp runtime.ApprovalResponse) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ch, ok := g.pending[resp.ActionID]; ok {
		ch <- resp
		return true
	}
	if len(g.early) >= maxEarlyResponses {
		return false
	}
	g.early[resp.ActionID] = resp
	return true
}
