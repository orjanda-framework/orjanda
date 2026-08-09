package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/orjanda-framework/orjanda/agent/runtime"
	"github.com/orjanda-framework/orjanda/auth"
)

// AgentHandler serves the agent chat WebSocket endpoint (TAD §6.2):
// WS /api/v1/agent/stream. Client → server messages follow the §6.2 contract
// (message / approval_response); the runtime's streaming events are forwarded
// verbatim as server → client messages, including the extended §12.3
// approval_required payload with policy_reason.
type AgentHandler struct {
	// Base holds the runtime options shared across connections. Sink and
	// Approvals are per-connection and filled in by Stream.
	Base runtime.Options
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

// Stream upgrades the connection and runs the §6.2 message loop.
func (h *AgentHandler) Stream(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		slog.Warn("agent.ws.accept", "error", err)
		return
	}
	defer func() { _ = c.Close(websocket.StatusInternalError, "closing") }()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	connCtx := context.WithoutCancel(ctx)

	sink := &wsSink{ctx: connCtx, c: c}
	gw := &wsGateway{pending: make(map[string]chan runtime.ApprovalResponse)}

	opts := h.Base
	opts.Sink = sink
	opts.Approvals = gw
	rt, err := runtime.NewRuntime(opts)
	if err != nil {
		sink.Send(runtime.Event{Type: runtime.EventToolEnd, Content: "error: " + err.Error()})
		return
	}

	id := auth.FromContext(r.Context())
	idCtx := auth.NewContext(connCtx, id)

	var turnMu sync.Mutex
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
			go func(text string) {
				// Serialize turns so a slow run can't interleave with the next.
				turnMu.Lock()
				defer turnMu.Unlock()
				if _, err := rt.Execute(idCtx, text); err != nil {
					sink.Send(runtime.Event{Type: runtime.EventToolEnd, Content: "error: " + err.Error()})
				}
			}(msg.Text)
		case "approval_response":
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
	raw, err := json.Marshal(evt)
	if err != nil {
		slog.Warn("agent.ws.marshal", "error", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.c.Write(s.ctx, websocket.MessageText, raw); err != nil {
		slog.Warn("agent.ws.write", "error", err)
	}
}

// wsGateway blocks on an approval_required round trip until the matching
// approval_response arrives on the connection (TAD §12.3).
type wsGateway struct {
	mu      sync.Mutex
	pending map[string]chan runtime.ApprovalResponse
}

func (g *wsGateway) RequestApproval(ctx context.Context, payload runtime.ApprovalPayload) (runtime.ApprovalResponse, error) {
	ch := make(chan runtime.ApprovalResponse, 1)
	g.mu.Lock()
	g.pending[payload.ActionID] = ch
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.pending, payload.ActionID)
		g.mu.Unlock()
	}()

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return runtime.ApprovalResponse{}, ctx.Err()
	}
}

// submit delivers an approval_response to the matching pending request,
// reporting whether one was waiting for it.
func (g *wsGateway) submit(resp runtime.ApprovalResponse) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	ch, ok := g.pending[resp.ActionID]
	if !ok {
		return false
	}
	ch <- resp
	return true
}
