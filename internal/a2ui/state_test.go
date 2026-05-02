package a2ui

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func createSurfaceMsg(t *testing.T, surfaceID, catalogID string) json.RawMessage {
	t.Helper()
	return mustRaw(t, map[string]any{
		"version": ProtocolVersion,
		"createSurface": map[string]any{
			"surfaceId":      surfaceID,
			"catalogId":      catalogID,
			"sendDataModel": true,
		},
	})
}

func updateComponentsMsg(t *testing.T, surfaceID string) json.RawMessage {
	t.Helper()
	return mustRaw(t, map[string]any{
		"version": ProtocolVersion,
		"updateComponents": map[string]any{
			"surfaceId":  surfaceID,
			"components": []any{},
		},
	})
}

func TestAppendAssignsMonotonicSeq(t *testing.T) {
	s := NewState(Limits{})
	r1, err := s.Append("s1", []json.RawMessage{updateComponentsMsg(t, "main")})
	if err != nil {
		t.Fatalf("first append: %v", err)
	}
	if r1.Seq != 1 {
		t.Fatalf("first seq: got %d want 1", r1.Seq)
	}

	r2, err := s.Append("s1", []json.RawMessage{updateComponentsMsg(t, "main"), updateComponentsMsg(t, "main")})
	if err != nil {
		t.Fatalf("second append: %v", err)
	}
	if r2.Seq != 2 {
		t.Fatalf("second seq: got %d want 2", r2.Seq)
	}
	if r2.MessageCount != 2 {
		t.Fatalf("second count: got %d want 2", r2.MessageCount)
	}

	// Different sessions get independent counters.
	r3, err := s.Append("s2", []json.RawMessage{updateComponentsMsg(t, "main")})
	if err != nil {
		t.Fatalf("session2: %v", err)
	}
	if r3.Seq != 1 {
		t.Fatalf("session2 seq: got %d want 1", r3.Seq)
	}
}

func TestSnapshotReturnsAccumulatedMessages(t *testing.T) {
	s := NewState(Limits{})
	if _, ok := s.Snapshot("s1"); ok {
		t.Fatal("snapshot should be absent before any append")
	}

	_, _ = s.Append("s1", []json.RawMessage{
		createSurfaceMsg(t, "main", "https://a2ui.org/specification/v0_9/basic_catalog.json"),
	})
	_, _ = s.Append("s1", []json.RawMessage{updateComponentsMsg(t, "main")})

	snap, ok := s.Snapshot("s1")
	if !ok {
		t.Fatal("expected snapshot")
	}
	if snap.Version != ProtocolVersion {
		t.Fatalf("version: %q", snap.Version)
	}
	if snap.Seq != 2 {
		t.Fatalf("seq: %d", snap.Seq)
	}
	if len(snap.Messages) != 2 {
		t.Fatalf("messages: %d", len(snap.Messages))
	}
	if snap.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be set")
	}
}

func TestSnapshotIsCopy(t *testing.T) {
	s := NewState(Limits{})
	_, _ = s.Append("s1", []json.RawMessage{updateComponentsMsg(t, "main")})

	snap, _ := s.Snapshot("s1")
	// Mutate the returned slice — subsequent snapshot must be unaffected.
	snap.Messages[0] = json.RawMessage(`{"tampered":true}`)

	snap2, _ := s.Snapshot("s1")
	if string(snap2.Messages[0]) == `{"tampered":true}` {
		t.Fatal("snapshot leaked internal slice; mutations propagated")
	}
}

func TestResetDropsState(t *testing.T) {
	s := NewState(Limits{})
	_, _ = s.Append("s1", []json.RawMessage{updateComponentsMsg(t, "main")})
	s.Reset("s1")
	if _, ok := s.Snapshot("s1"); ok {
		t.Fatal("expected snapshot to be gone after reset")
	}
	// Idempotent.
	s.Reset("s1")

	// Counter restarts after reset.
	r, err := s.Append("s1", []json.RawMessage{updateComponentsMsg(t, "main")})
	if err != nil {
		t.Fatalf("append after reset: %v", err)
	}
	if r.Seq != 1 {
		t.Fatalf("expected seq=1 after reset, got %d", r.Seq)
	}
}

func TestValidateRejectsEmptyBatch(t *testing.T) {
	s := NewState(Limits{})
	if err := s.Validate(nil); !errors.Is(err, ErrEmptyBatch) {
		t.Fatalf("expected ErrEmptyBatch, got %v", err)
	}
}

func TestValidateRejectsBatchTooLarge(t *testing.T) {
	s := NewState(Limits{MaxMessagesPerBatch: 2})
	msgs := []json.RawMessage{
		updateComponentsMsg(t, "main"),
		updateComponentsMsg(t, "main"),
		updateComponentsMsg(t, "main"),
	}
	if err := s.Validate(msgs); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("expected ErrBatchTooLarge, got %v", err)
	}
}

func TestValidateRejectsMissingVersion(t *testing.T) {
	s := NewState(Limits{})
	bad := mustRaw(t, map[string]any{
		"updateComponents": map[string]any{"surfaceId": "main"},
	})
	if err := s.Validate([]json.RawMessage{bad}); !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("expected ErrInvalidVersion, got %v", err)
	}
}

func TestValidateRejectsWrongVersion(t *testing.T) {
	s := NewState(Limits{})
	bad := mustRaw(t, map[string]any{
		"version":          "v0.8",
		"updateComponents": map[string]any{"surfaceId": "main"},
	})
	if err := s.Validate([]json.RawMessage{bad}); !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("expected ErrInvalidVersion, got %v", err)
	}
}

func TestValidateRejectsMalformedMessage(t *testing.T) {
	s := NewState(Limits{})
	if err := s.Validate([]json.RawMessage{json.RawMessage(`"not-an-object"`)}); !errors.Is(err, ErrMessageMalformed) {
		t.Fatalf("expected ErrMessageMalformed, got %v", err)
	}
}

func TestValidateRejectsUnknownCatalog(t *testing.T) {
	s := NewState(Limits{})
	bad := createSurfaceMsg(t, "main", "https://example.com/evil-catalog.json")
	err := s.Validate([]json.RawMessage{bad})
	if !errors.Is(err, ErrInvalidCatalogID) {
		t.Fatalf("expected ErrInvalidCatalogID, got %v", err)
	}
	if !strings.Contains(err.Error(), "evil-catalog") {
		t.Fatalf("error should mention offending catalog: %v", err)
	}
}

func TestValidateAllowsCreateSurfaceWithoutCatalog(t *testing.T) {
	// Spec text shows createSurface with catalogId, but per the broker
	// design catalogId is optional — only validated when present.
	s := NewState(Limits{})
	msg := mustRaw(t, map[string]any{
		"version": ProtocolVersion,
		"createSurface": map[string]any{
			"surfaceId": "main",
		},
	})
	if err := s.Validate([]json.RawMessage{msg}); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestAppendEnforcesRetentionLimit(t *testing.T) {
	s := NewState(Limits{MaxRetainedMessages: 3})
	if _, err := s.Append("s1", []json.RawMessage{
		updateComponentsMsg(t, "main"),
		updateComponentsMsg(t, "main"),
	}); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	// Pushes total to 4 — over the limit.
	_, err := s.Append("s1", []json.RawMessage{
		updateComponentsMsg(t, "main"),
		updateComponentsMsg(t, "main"),
	})
	if !errors.Is(err, ErrRetentionExceeded) {
		t.Fatalf("expected ErrRetentionExceeded, got %v", err)
	}

	// Counter must not advance on rejection.
	snap, _ := s.Snapshot("s1")
	if snap.Seq != 1 {
		t.Fatalf("seq advanced despite rejection: %d", snap.Seq)
	}
	if len(snap.Messages) != 2 {
		t.Fatalf("messages mutated despite rejection: %d", len(snap.Messages))
	}
}

func TestAppendDefensivelyClonesMessages(t *testing.T) {
	s := NewState(Limits{})
	original := updateComponentsMsg(t, "main")
	_, err := s.Append("s1", []json.RawMessage{original})
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	// Mutate the original buffer — broker copy must remain intact.
	for i := range original {
		original[i] = 'X'
	}

	snap, _ := s.Snapshot("s1")
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(snap.Messages[0], &probe); err != nil {
		t.Fatalf("snapshot message corrupted by caller mutation: %v", err)
	}
}

func TestConcurrentAppendsAssignDistinctSeqs(t *testing.T) {
	s := NewState(Limits{})
	const n = 50

	var wg sync.WaitGroup
	seqs := make(chan int64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Append("s1", []json.RawMessage{updateComponentsMsg(t, "main")})
			if err != nil {
				t.Errorf("append: %v", err)
				return
			}
			seqs <- r.Seq
		}()
	}
	wg.Wait()
	close(seqs)

	seen := map[int64]bool{}
	for s := range seqs {
		if seen[s] {
			t.Fatalf("duplicate seq: %d", s)
		}
		seen[s] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d distinct seqs, got %d", n, len(seen))
	}
}

func TestLimitsDefaults(t *testing.T) {
	s := NewState(Limits{})
	got := s.Limits()
	if got.MaxMessagesPerBatch != DefaultMaxMessagesPerBatch {
		t.Fatalf("MaxMessagesPerBatch: %d", got.MaxMessagesPerBatch)
	}
	if got.MaxRetainedMessages != DefaultMaxRetainedMessages {
		t.Fatalf("MaxRetainedMessages: %d", got.MaxRetainedMessages)
	}
	if got.MaxRequestBodyBytes != DefaultMaxRequestBodyBytes {
		t.Fatalf("MaxRequestBodyBytes: %d", got.MaxRequestBodyBytes)
	}
}
