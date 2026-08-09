package llm_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orjanda-framework/orjanda/agent/llm"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// fakeProvider is a scriptable llm.Provider for Gateway tests.
type fakeProvider struct {
	name   string
	calls  atomic.Int32
	status atomic.Int32 // HTTP status for failover detection (0 = success)
}

func (f *fakeProvider) ChatCompletion(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.calls.Add(1)
	if code := int(f.status.Load()); code != 0 {
		return nil, orjerrors.Internal("upstream boom", &llm.HTTPStatusError{StatusCode: code, Message: "boom"})
	}
	return &llm.ChatResponse{Content: f.name, FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 10, PromptTokens: 6, CompletionTokens: 4}}, nil
}

func (f *fakeProvider) StreamChatCompletion(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	f.calls.Add(1)
	if code := int(f.status.Load()); code != 0 {
		return nil, orjerrors.Internal("upstream boom", &llm.HTTPStatusError{StatusCode: code, Message: "boom"})
	}
	ch := make(chan llm.ChatChunk, 1)
	ch <- llm.ChatChunk{Content: f.name}
	close(ch)
	return ch, nil
}

func (f *fakeProvider) SupportsToolCalling() bool      { return true }
func (f *fakeProvider) SupportsStructuredOutput() bool { return true }
func (f *fakeProvider) ModelInfo() llm.ModelInfo {
	return llm.ModelInfo{Name: f.name, SupportsTools: true, SupportsStructuredOutput: true}
}

func (f *fakeProvider) setStatus(status int) { f.status.Store(int32(status)) }

func TestGatewayFailsOverOn5xx(t *testing.T) {
	primary := &fakeProvider{name: "primary"}
	fallback := &fakeProvider{name: "fallback"}
	primary.setStatus(500)

	g := llm.NewGateway(primary, fallback)
	resp, err := g.ChatCompletion(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Content != "fallback" {
		t.Errorf("Content = %q, want %q", resp.Content, "fallback")
	}
	if primary.calls.Load() != 1 || fallback.calls.Load() != 1 {
		t.Errorf("calls: primary=%d fallback=%d, want 1 each", primary.calls.Load(), fallback.calls.Load())
	}
}

func TestGatewayDoesNotFailOverOn4xx(t *testing.T) {
	primary := &fakeProvider{name: "primary"}
	fallback := &fakeProvider{name: "fallback"}
	primary.setStatus(400)

	g := llm.NewGateway(primary, fallback)
	_, err := g.ChatCompletion(context.Background(), llm.ChatRequest{})
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if fallback.calls.Load() != 0 {
		t.Errorf("fallback called %d times, want 0 (no failover on 4xx)", fallback.calls.Load())
	}
	var httpErr *llm.HTTPStatusError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 400 {
		t.Errorf("err = %v, want HTTPStatusError 400", err)
	}
}

func TestGatewayCircuitBreakerOpens(t *testing.T) {
	primary := &fakeProvider{name: "primary"}
	fallback := &fakeProvider{name: "fallback"}
	primary.setStatus(503)

	g := llm.NewGatewayWithOptions(primary, llm.GatewayOptions{FailoverThreshold: 2, Cooldown: time.Minute}, fallback)

	// Two failures open the primary's circuit (each failing over to fallback).
	for i := 0; i < 2; i++ {
		if _, err := g.ChatCompletion(context.Background(), llm.ChatRequest{}); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	if got := primary.calls.Load(); got != 2 {
		t.Fatalf("primary calls after 2 failures = %d, want 2", got)
	}

	// Third call: primary is open, so only the fallback is tried.
	if _, err := g.ChatCompletion(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("call 3: %v", err)
	}
	if got := primary.calls.Load(); got != 2 {
		t.Errorf("primary calls after open = %d, want still 2 (circuit open)", got)
	}
	if got := fallback.calls.Load(); got != 3 {
		t.Errorf("fallback calls = %d, want 3", got)
	}
}

func TestGatewayCircuitBreakerRecovers(t *testing.T) {
	primary := &fakeProvider{name: "primary"}
	fallback := &fakeProvider{name: "fallback"}
	primary.setStatus(503)

	g := llm.NewGatewayWithOptions(primary, llm.GatewayOptions{FailoverThreshold: 1, Cooldown: 50 * time.Millisecond}, fallback)

	if _, err := g.ChatCompletion(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if got := primary.calls.Load(); got != 1 {
		t.Fatalf("primary calls = %d, want 1", got)
	}

	// After cooldown the breaker admits a probe request to the primary.
	time.Sleep(80 * time.Millisecond)
	primary.setStatus(0)
	if _, err := g.ChatCompletion(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("call 2 (probe): %v", err)
	}
	if got := primary.calls.Load(); got != 2 {
		t.Errorf("primary calls after recovery = %d, want 2 (probe admitted)", got)
	}
}

func TestGatewayStreamFailoverOn5xx(t *testing.T) {
	primary := &fakeProvider{name: "primary"}
	fallback := &fakeProvider{name: "fallback"}
	primary.setStatus(502)

	g := llm.NewGateway(primary, fallback)
	ch, err := g.StreamChatCompletion(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("StreamChatCompletion: %v", err)
	}
	var content strings.Builder
	for c := range ch {
		content.WriteString(c.Content)
	}
	if content.String() != "fallback" {
		t.Errorf("streamed content = %q, want %q", content.String(), "fallback")
	}
	if primary.calls.Load() != 1 || fallback.calls.Load() != 1 {
		t.Errorf("calls: primary=%d fallback=%d, want 1 each", primary.calls.Load(), fallback.calls.Load())
	}
}

func TestGatewayTokenTracking(t *testing.T) {
	g := llm.NewGateway(&fakeProvider{name: "primary"})
	g.TrackUsage("user-1", llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
	g.TrackUsage("user-1", llm.TokenUsage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3})
	g.TrackUsage("user-2", llm.TokenUsage{PromptTokens: 100, CompletionTokens: 0, TotalTokens: 100})

	if u := g.UsageFor("user-1"); u.TotalTokens != 18 {
		t.Errorf("UsageFor(user-1).TotalTokens = %d, want 18", u.TotalTokens)
	}
	if u := g.Usage(); u.TotalTokens != 118 {
		t.Errorf("Usage().TotalTokens = %d, want 118", u.TotalTokens)
	}
}

func TestGatewayAllProvidersOpen(t *testing.T) {
	primary := &fakeProvider{name: "primary"}
	primary.setStatus(500)

	g := llm.NewGatewayWithOptions(primary, llm.GatewayOptions{FailoverThreshold: 1, Cooldown: time.Minute})
	if _, err := g.ChatCompletion(context.Background(), llm.ChatRequest{}); err == nil {
		t.Fatal("expected error when the only provider is open")
	}
}
