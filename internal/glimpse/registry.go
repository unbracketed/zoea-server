// Package glimpse implements the BASIL Glimpse browser delivery layer.
//
// Zoea is the thin transport between BASIL's `ZoeaTransport` (which composes
// flow steps into self-contained HTML) and a teammate's browser session. BASIL
// posts a render request, Zoea routes it to the active session's WS stream,
// the browser POSTs back the raw `window.glimpse.send(...)` payload, and Zoea
// returns one terminal response (action/cancelled/busy/error) to the waiting
// transport call.
//
// This file owns the in-memory pending-render registry. The HTTP layer lives
// in internal/api/glimpse.go.
package glimpse

import (
	"errors"
	"sync"
	"time"
)

// Status values for a pending render.
const (
	StatusPending   = "pending"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
	StatusTimedOut  = "timed_out"
	StatusErrored   = "errored"
)

// Outcome captures the terminal state returned to the waiting render call.
// Exactly one of Action / Cancelled / TimedOut / Err* is set.
type Outcome struct {
	// Action holds the unmodified browser payload (request_id, action_id, raw).
	Action map[string]any

	// Cancelled is true when the user dismissed the prompt.
	Cancelled bool

	// TimedOut is true when the deadline elapsed before any submission.
	TimedOut bool

	// Err is non-nil when the render failed with a server-side error.
	Err error
}

// Pending tracks one in-flight render keyed by RequestID.
type Pending struct {
	RequestID      string
	FlowID         string
	SessionID      string
	ConversationID string
	UserID         string
	Deadline       time.Time
	Status         string
	StartedAt      time.Time

	// resolve delivers the terminal outcome to the waiting render goroutine.
	// Buffered size 1; a single send wins, others are dropped.
	resolve chan Outcome

	// once guards Status transitions and resolve sends so the registry can
	// drop late submissions safely.
	once sync.Once
}

// Wait blocks until the pending render reaches a terminal state or the
// deadline elapses. Returns the final outcome. The caller is the goroutine
// servicing POST /api/glimpse/v1/render — it owns the HTTP response.
func (p *Pending) Wait() Outcome {
	var timer *time.Timer
	var timeoutC <-chan time.Time
	if !p.Deadline.IsZero() {
		d := time.Until(p.Deadline)
		if d <= 0 {
			// Already expired; surface as timeout.
			p.once.Do(func() { p.Status = StatusTimedOut })
			return Outcome{TimedOut: true}
		}
		timer = time.NewTimer(d)
		timeoutC = timer.C
		defer timer.Stop()
	}

	select {
	case out := <-p.resolve:
		return out
	case <-timeoutC:
		p.once.Do(func() { p.Status = StatusTimedOut })
		return Outcome{TimedOut: true}
	}
}

// Registry tracks all pending Glimpse renders across the server.
//
// Concurrency rules (v1):
//   - one active render per (SessionID) target
//   - request_id must be globally unique while pending
//   - resolve transitions are guarded by Pending.once so late
//     /action and /cancel calls are dropped instead of double-resolving
type Registry struct {
	mu           sync.Mutex
	byID         map[string]*Pending // request_id -> pending
	activeForSes map[string]string   // session_id -> request_id (active)
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		byID:         map[string]*Pending{},
		activeForSes: map[string]string{},
	}
}

// ErrBusy means the target session already has an active Glimpse prompt.
var ErrBusy = errors.New("glimpse: target session already has an active render")

// ErrDuplicateRequestID means a render with the same request_id is already
// pending. Treated as a server-side error rather than a busy condition.
var ErrDuplicateRequestID = errors.New("glimpse: request_id already pending")

// ErrUnknownRequest means the request_id is not in the registry. The caller
// is responsible for translating this into a 404.
var ErrUnknownRequest = errors.New("glimpse: unknown request_id")

// ErrAlreadyResolved means the pending render has already reached a
// terminal state. The caller is responsible for translating this into a 409.
var ErrAlreadyResolved = errors.New("glimpse: render already resolved")

// Register adds a new pending render. Returns ErrBusy if the target session
// already has an active render, or ErrDuplicateRequestID on id collision.
func (r *Registry) Register(p *Pending) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byID[p.RequestID]; exists {
		return ErrDuplicateRequestID
	}
	if active, busy := r.activeForSes[p.SessionID]; busy {
		// Surface the existing request id so the caller can include it in
		// the 409 response body.
		return &busyError{ActiveRequestID: active}
	}

	if p.resolve == nil {
		p.resolve = make(chan Outcome, 1)
	}
	if p.StartedAt.IsZero() {
		p.StartedAt = time.Now().UTC()
	}
	p.Status = StatusPending

	r.byID[p.RequestID] = p
	r.activeForSes[p.SessionID] = p.RequestID
	return nil
}

// Get returns the Pending for a request_id or ErrUnknownRequest.
func (r *Registry) Get(requestID string) (*Pending, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[requestID]
	if !ok {
		return nil, ErrUnknownRequest
	}
	return p, nil
}

// ResolveAction completes a pending render with a successful action submission.
// The payload is the raw `window.glimpse.send(...)` body, forwarded unchanged.
// Returns ErrAlreadyResolved if the render is no longer pending.
func (r *Registry) ResolveAction(requestID string, payload map[string]any) error {
	return r.resolveLocked(requestID, StatusCompleted, Outcome{Action: payload})
}

// ResolveCancelled completes a pending render as cancelled.
func (r *Registry) ResolveCancelled(requestID string) error {
	return r.resolveLocked(requestID, StatusCancelled, Outcome{Cancelled: true})
}

// ResolveError completes a pending render with a server-side error.
func (r *Registry) ResolveError(requestID string, err error) error {
	return r.resolveLocked(requestID, StatusErrored, Outcome{Err: err})
}

// Forget removes a pending render after it has been resolved (or its render
// goroutine has returned). Safe to call multiple times.
func (r *Registry) Forget(requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[requestID]
	if !ok {
		return
	}
	delete(r.byID, requestID)
	if active, ok := r.activeForSes[p.SessionID]; ok && active == requestID {
		delete(r.activeForSes, p.SessionID)
	}
}

func (r *Registry) resolveLocked(requestID, newStatus string, out Outcome) error {
	r.mu.Lock()
	p, ok := r.byID[requestID]
	r.mu.Unlock()
	if !ok {
		return ErrUnknownRequest
	}

	resolved := false
	p.once.Do(func() {
		p.Status = newStatus
		select {
		case p.resolve <- out:
		default:
		}
		resolved = true
	})
	if !resolved {
		return ErrAlreadyResolved
	}
	return nil
}

// busyError surfaces the active request id alongside ErrBusy.
type busyError struct {
	ActiveRequestID string
}

func (b *busyError) Error() string { return "glimpse: target session busy" }

// Is lets callers test errors.Is(err, ErrBusy).
func (b *busyError) Is(target error) bool { return target == ErrBusy }

// ActiveRequestIDFromBusy returns the active request id when err wraps a busy
// condition. Returns "" otherwise.
func ActiveRequestIDFromBusy(err error) string {
	var b *busyError
	if errors.As(err, &b) {
		return b.ActiveRequestID
	}
	return ""
}
