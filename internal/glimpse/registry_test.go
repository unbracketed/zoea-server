package glimpse

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRegisterAndResolveAction(t *testing.T) {
	r := NewRegistry()
	p := &Pending{RequestID: "r1", SessionID: "s1"}
	if err := r.Register(p); err != nil {
		t.Fatalf("register: %v", err)
	}

	payload := map[string]any{
		"request_id": "r1",
		"action_id":  "continue",
		"raw":        map[string]any{"field": "value"},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var out Outcome
	go func() {
		defer wg.Done()
		out = p.Wait()
	}()

	// Give Wait a moment to enter select.
	time.Sleep(10 * time.Millisecond)
	if err := r.ResolveAction("r1", payload); err != nil {
		t.Fatalf("resolve action: %v", err)
	}

	wg.Wait()

	if out.Action == nil {
		t.Fatalf("expected action outcome, got %+v", out)
	}
	if out.Action["action_id"] != "continue" {
		t.Fatalf("payload not forwarded unchanged: %+v", out.Action)
	}
}

func TestRegisterBusyWhenSessionAlreadyHasActiveRender(t *testing.T) {
	r := NewRegistry()
	p1 := &Pending{RequestID: "r1", SessionID: "s1"}
	if err := r.Register(p1); err != nil {
		t.Fatalf("register p1: %v", err)
	}
	p2 := &Pending{RequestID: "r2", SessionID: "s1"}
	err := r.Register(p2)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
	if active := ActiveRequestIDFromBusy(err); active != "r1" {
		t.Fatalf("expected active r1, got %q", active)
	}
}

func TestRegisterDifferentSessionsAllowed(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&Pending{RequestID: "r1", SessionID: "s1"}); err != nil {
		t.Fatalf("register r1: %v", err)
	}
	if err := r.Register(&Pending{RequestID: "r2", SessionID: "s2"}); err != nil {
		t.Fatalf("register r2: %v", err)
	}
}

func TestForgetAllowsNextRender(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&Pending{RequestID: "r1", SessionID: "s1"}); err != nil {
		t.Fatalf("register r1: %v", err)
	}
	r.Forget("r1")
	if err := r.Register(&Pending{RequestID: "r2", SessionID: "s1"}); err != nil {
		t.Fatalf("expected register after forget, got %v", err)
	}
}

func TestResolveCancelled(t *testing.T) {
	r := NewRegistry()
	p := &Pending{RequestID: "r1", SessionID: "s1"}
	if err := r.Register(p); err != nil {
		t.Fatalf("register: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var out Outcome
	go func() {
		defer wg.Done()
		out = p.Wait()
	}()

	time.Sleep(10 * time.Millisecond)
	if err := r.ResolveCancelled("r1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	wg.Wait()

	if !out.Cancelled {
		t.Fatalf("expected cancelled outcome, got %+v", out)
	}
}

func TestSecondResolveIsRejected(t *testing.T) {
	r := NewRegistry()
	p := &Pending{RequestID: "r1", SessionID: "s1"}
	if err := r.Register(p); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := r.ResolveAction("r1", map[string]any{"action_id": "ok"}); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	err := r.ResolveCancelled("r1")
	if !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("expected ErrAlreadyResolved, got %v", err)
	}
}

func TestResolveUnknownReturnsError(t *testing.T) {
	r := NewRegistry()
	err := r.ResolveAction("nope", map[string]any{})
	if !errors.Is(err, ErrUnknownRequest) {
		t.Fatalf("expected ErrUnknownRequest, got %v", err)
	}
}

func TestWaitTimesOutAtDeadline(t *testing.T) {
	r := NewRegistry()
	p := &Pending{
		RequestID: "r1",
		SessionID: "s1",
		Deadline:  time.Now().Add(40 * time.Millisecond),
	}
	if err := r.Register(p); err != nil {
		t.Fatalf("register: %v", err)
	}

	start := time.Now()
	out := p.Wait()
	elapsed := time.Since(start)

	if !out.TimedOut {
		t.Fatalf("expected timed out, got %+v", out)
	}
	if elapsed < 30*time.Millisecond {
		t.Fatalf("returned too quickly: %v", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("waited too long: %v", elapsed)
	}
}

func TestExpiredDeadlineSurfacesAsTimeout(t *testing.T) {
	r := NewRegistry()
	p := &Pending{
		RequestID: "r1",
		SessionID: "s1",
		Deadline:  time.Now().Add(-1 * time.Second),
	}
	if err := r.Register(p); err != nil {
		t.Fatalf("register: %v", err)
	}
	out := p.Wait()
	if !out.TimedOut {
		t.Fatalf("expected timed out, got %+v", out)
	}
}

func TestDuplicateRequestIDRejected(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&Pending{RequestID: "r1", SessionID: "s1"}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := r.Register(&Pending{RequestID: "r1", SessionID: "s2"})
	if !errors.Is(err, ErrDuplicateRequestID) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}
