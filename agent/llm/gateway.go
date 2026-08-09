package llm

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Gateway implements PRD §26.4 Provider Resilience on top of one or more
// Provider implementations:
//
//   - Automatic failover: a 5xx (or 5xx-class, any HTTPStatusError with
//     StatusCode >= 500) from a provider triggers a retry on the next
//     provider in the ordered list.
//   - Circuit breaker: after FailoverThreshold consecutive failures a provider
//     is skipped for a Cooldown period (matching PRD §26.4 "After N consecutive
//     failures, stop sending to a provider for a cooldown period").
//   - Token budget tracking: usage is accumulated globally and can be
//     attributed per key (session/user id) via TrackUsage/UsageFor.
//
// A non-5xx error (e.g. 401 auth, 400 validation) is returned immediately
// without trying fallbacks — a fallback provider would fail the same way.
//
// Gateway satisfies the Provider interface (TAD §2.7), so it composes as a
// normal provider wherever the runtime expects one.
type Gateway struct {
	providers []Provider
	opts      GatewayOptions
	breakers  []*circuitBreaker

	mu     sync.Mutex
	tokens map[string]TokenUsage
}

// GatewayOptions tunes failover behavior. Zero values select the defaults.
type GatewayOptions struct {
	// FailoverThreshold is the number of consecutive failures that opens a
	// provider's circuit. Default: 3.
	FailoverThreshold int
	// Cooldown is how long an open circuit stays open before a probe request
	// is allowed through. Default: 30s.
	Cooldown time.Duration
}

// NewGateway builds a Gateway with primary first, followed by fallbacks in
// priority order. Failover proceeds down the list.
func NewGateway(primary Provider, fallbacks ...Provider) *Gateway {
	return NewGatewayWithOptions(primary, GatewayOptions{}, fallbacks...)
}

// NewGatewayWithOptions is NewGateway with explicit resilience tuning.
func NewGatewayWithOptions(primary Provider, opts GatewayOptions, fallbacks ...Provider) *Gateway {
	if primary == nil {
		panic("llm: Gateway requires a primary provider")
	}
	threshold := opts.FailoverThreshold
	if threshold <= 0 {
		threshold = 3
	}
	cooldown := opts.Cooldown
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	providers := append([]Provider{primary}, fallbacks...)
	g := &Gateway{
		providers: providers,
		opts:      GatewayOptions{FailoverThreshold: threshold, Cooldown: cooldown},
		tokens:    make(map[string]TokenUsage),
	}
	for range providers {
		g.breakers = append(g.breakers, &circuitBreaker{
			threshold: threshold,
			cooldown:  cooldown,
		})
	}
	return g
}

func (g *Gateway) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	var lastErr error
	for i, p := range g.providers {
		if !g.breakers[i].allow() {
			continue
		}
		resp, err := p.ChatCompletion(ctx, req)
		if err == nil {
			g.breakers[i].recordSuccess()
			g.track("", resp.Usage)
			return resp, nil
		}
		lastErr = err
		if !isFailoverError(err) {
			return nil, err
		}
		g.breakers[i].recordFailure(time.Now())
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("llm: all providers are open by the circuit breaker")
}

func (g *Gateway) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error) {
	var lastErr error
	for i, p := range g.providers {
		if !g.breakers[i].allow() {
			continue
		}
		ch, err := p.StreamChatCompletion(ctx, req)
		if err == nil {
			g.breakers[i].recordSuccess()
			return ch, nil
		}
		lastErr = err
		if !isFailoverError(err) {
			return nil, err
		}
		g.breakers[i].recordFailure(time.Now())
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("llm: all providers are open by the circuit breaker")
}

func (g *Gateway) SupportsToolCalling() bool      { return g.providers[0].SupportsToolCalling() }
func (g *Gateway) SupportsStructuredOutput() bool { return g.providers[0].SupportsStructuredOutput() }
func (g *Gateway) ModelInfo() ModelInfo           { return g.providers[0].ModelInfo() }

// TrackUsage attributes token usage to a key (e.g. a session or user id) so
// the Safety Layer can enforce per-session/per-user budgets (PRD §26.4).
func (g *Gateway) TrackUsage(key string, usage TokenUsage) {
	g.track(key, usage)
}

// UsageFor returns the accumulated token usage for a key.
func (g *Gateway) UsageFor(key string) TokenUsage {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tokens[key]
}

// Usage returns the total token usage across all calls and keys.
func (g *Gateway) Usage() TokenUsage {
	g.mu.Lock()
	defer g.mu.Unlock()
	total := TokenUsage{}
	for _, u := range g.tokens {
		total.PromptTokens += u.PromptTokens
		total.CompletionTokens += u.CompletionTokens
		total.TotalTokens += u.TotalTokens
	}
	return total
}

func (g *Gateway) track(key string, usage TokenUsage) {
	if usage.TotalTokens == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	cur := g.tokens[key]
	cur.PromptTokens += usage.PromptTokens
	cur.CompletionTokens += usage.CompletionTokens
	cur.TotalTokens += usage.TotalTokens
	g.tokens[key] = cur
}

// isFailoverError reports whether err is (or wraps) a provider HTTP error with
// a status >= 500, i.e. the only condition PRD §26.4 says should fail over.
func isFailoverError(err error) bool {
	var h *HTTPStatusError
	if errors.As(err, &h) {
		return h.StatusCode >= 500
	}
	return false
}

// circuitBreaker implements the closed/open/half-open state machine: it stays
// closed while failures stay under the threshold, opens for Cooldown once the
// threshold is reached, and admits a probe request after the cooldown expires
// (the half-open state). A probe success resets it to closed.
type circuitBreaker struct {
	threshold int
	cooldown  time.Duration

	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

func (b *circuitBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.openUntil.After(time.Now())
}

func (b *circuitBreaker) recordFailure(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.threshold {
		b.openUntil = now.Add(b.cooldown)
		b.failures = 0
	}
}

func (b *circuitBreaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
}
