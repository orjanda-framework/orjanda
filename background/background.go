package background

import (
	"context"
	"sync"
	"time"
)

// Job is the interface that a background task must implement.
// See TAD §9.1 and PRD §44.3 (background jobs are post-MVP).
type Job interface {
	// Name returns the unique string identifier for this job type.
	Name() string
	// Handle processes the job payload. Returns an error if processing fails.
	Handle(ctx context.Context, payload []byte) error
}

// EnqueueOpts controls scheduling and retry behaviour.
type EnqueueOpts struct {
	// RunAt specifies when the job should execute. Zero value means run
	// immediately.
	RunAt time.Time
	// MaxRetries is the maximum number of retry attempts on failure.
	MaxRetries int
}

// Queue accepts job registrations and enqueue calls. The MVP implementation
// is a non-durable in-memory stub — it executes jobs synchronously in a
// goroutine and does not persist them across restarts.
// See TAD §9.1.
type Queue interface {
	// Enqueue schedules a job for execution.
	Enqueue(ctx context.Context, job string, payload []byte, opts EnqueueOpts) error
	// RegisterHandler registers the handler for a named job type.
	RegisterHandler(job string, j Job)
}

// ----------------------------------------------------------------------------
// In-memory (non-durable) Queue — MVP stub
// ----------------------------------------------------------------------------

// inMemoryQueue is the MVP no-op / in-memory Queue implementation.
// Jobs are executed asynchronously in goroutines. No persistence, no retries,
// no cross-restart durability. A durable DB-backed or Redis-backed Queue is a
// v0.2 drop-in behind this same interface (PRD §44.3).
type inMemoryQueue struct {
	mu       sync.RWMutex
	handlers map[string]Job
}

// NewInMemoryQueue creates a non-durable in-memory Queue stub.
func NewInMemoryQueue() Queue {
	return &inMemoryQueue{
		handlers: make(map[string]Job),
	}
}

// RegisterHandler registers a Job handler for the given job name.
func (q *inMemoryQueue) RegisterHandler(job string, j Job) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[job] = j
}

// Enqueue schedules a job. For the in-memory stub, it runs the handler in a
// goroutine if a handler is registered; otherwise it silently accepts the
// enqueue call (fire-and-forget).
func (q *inMemoryQueue) Enqueue(ctx context.Context, job string, payload []byte, opts EnqueueOpts) error {
	q.mu.RLock()
	handler, ok := q.handlers[job]
	q.mu.RUnlock()

	if !ok {
		// No handler registered — accept silently (Application may enqueue
		// before the handler is installed, or the handler is registered in a
		// future version). This is by design for the stub.
		return nil
	}

	go func() {
		delay := time.Until(opts.RunAt)
		if delay > 0 {
			time.Sleep(delay)
		}
		// Ignore errors in the MVP stub — no retry mechanism.
		_ = handler.Handle(ctx, payload)
	}()

	return nil
}
