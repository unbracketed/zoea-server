// Package a2ui implements the per-session A2UI v0.9 message broker.
//
// Zoea retains a replayable history of A2UI batches per session so a
// late-subscribing client can reconstruct the current surface by replaying
// the buffered messages. The server treats each message as opaque JSON —
// it never interprets component semantics, only records and replays.
package a2ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ProtocolVersion is the only A2UI version this broker accepts.
const ProtocolVersion = "v0.9"

// AllowedCatalogIDs lists the catalog URLs accepted in createSurface.
// Any catalogId outside this set is rejected at validation time.
var AllowedCatalogIDs = map[string]struct{}{
	"https://a2ui.org/specification/v0_9/basic_catalog.json": {},
}

// Default limits enforced by Validate when the caller passes a zero-value
// Limits struct. Tests and the temporary injection endpoint may override
// these by supplying explicit Limits.
const (
	DefaultMaxMessagesPerBatch = 100
	DefaultMaxRetainedMessages = 2000
	DefaultMaxRequestBodyBytes = 256 * 1024
)

// Limits caps the broker's per-batch and per-session retention. All values
// are inclusive; a zero field falls back to the matching Default constant.
type Limits struct {
	MaxMessagesPerBatch int
	MaxRetainedMessages int
	MaxRequestBodyBytes int
}

func (l Limits) resolved() Limits {
	out := l
	if out.MaxMessagesPerBatch <= 0 {
		out.MaxMessagesPerBatch = DefaultMaxMessagesPerBatch
	}
	if out.MaxRetainedMessages <= 0 {
		out.MaxRetainedMessages = DefaultMaxRetainedMessages
	}
	if out.MaxRequestBodyBytes <= 0 {
		out.MaxRequestBodyBytes = DefaultMaxRequestBodyBytes
	}
	return out
}

// Errors returned by Validate and Append. Wrap a small sentinel so the
// HTTP layer can map to 400/413 without string matching.
var (
	ErrEmptyBatch         = errors.New("a2ui: batch must contain at least one message")
	ErrBatchTooLarge      = errors.New("a2ui: batch exceeds max messages per batch")
	ErrInvalidVersion     = errors.New("a2ui: every message must declare version " + ProtocolVersion)
	ErrInvalidCatalogID   = errors.New("a2ui: createSurface.catalogId is not in the allowed catalog list")
	ErrMessageMalformed   = errors.New("a2ui: message is not a JSON object")
	ErrSessionNotFound    = errors.New("a2ui: session has no retained state")
	ErrRetentionExceeded  = errors.New("a2ui: appending batch would exceed max retained messages")
)

// Snapshot is the broker's view of a session's retained A2UI state at a
// point in time. The returned Messages slice is a copy — callers may
// retain or marshal it freely. Groups preserves the per-batch
// (messageID, messages) grouping so a reconnecting client can re-bucket
// surfaces by owning assistant message. Submissions carries any
// recorded user responses so closed forms render in their post-submit
// state on reconnect.
type Snapshot struct {
	Version     string
	Seq         int64
	Messages    []json.RawMessage
	Groups      []SnapshotGroup
	Submissions []SubmissionRecord
	UpdatedAt   time.Time
}

// SnapshotGroup is the per-batch unit captured in retained state — the
// messages appended together plus the assistant message id they belong
// to (empty when the batch was injected without correlation).
type SnapshotGroup struct {
	MessageID string
	Messages  []json.RawMessage
}

// AppendResult reports the per-batch outcome of Append.
type AppendResult struct {
	Seq          int64
	MessageCount int
}

// session is the per-session retained state. Messages are stored in
// arrival order; Seq is the monotonic counter the broker hands back to
// callers and includes in WS frames. Groups records, for each appended
// batch, the (messageID, messages) pair so a reconnecting client can
// re-attach surfaces to the assistant message that emitted them.
//
// LatestResponseID is the most recent assistant responseId observed on
// the session's gateway stream — used as a fallback when an A2UI inject
// arrives without an explicit message_id, so surfaces still anchor to a
// real chat bubble. Submissions records, per surface, the user's
// recorded action+values so reconnecting clients can render closed
// forms in their post-submit state.
type session struct {
	mu               sync.Mutex
	lastSeq          int64
	messages         []json.RawMessage
	groups           []SnapshotGroup
	latestResponseID string
	submissions      map[string]SubmissionRecord
	updatedAt        time.Time
}

// SubmissionRecord captures one user response to an A2UI surface. The
// broker treats Values as opaque; clients render them in the closed
// form card.
type SubmissionRecord struct {
	SurfaceID  string
	MessageID  string
	ActionName string
	Status     string // "submitted" | "cancelled"
	Values     json.RawMessage
	At         time.Time
}

// State is the per-server registry of A2UI sessions. Safe for concurrent use.
type State struct {
	limits Limits

	mu       sync.Mutex
	sessions map[string]*session
}

// NewState returns an empty State. Pass a zero-value Limits for defaults.
func NewState(limits Limits) *State {
	return &State{
		limits:   limits.resolved(),
		sessions: map[string]*session{},
	}
}

// Limits returns the resolved limits in effect for this State.
func (s *State) Limits() Limits {
	return s.limits
}

// Validate checks that messages obey the broker's invariants without
// mutating any state. Used by HTTP handlers that want to reject a batch
// before broadcasting or persisting anything.
func (s *State) Validate(messages []json.RawMessage) error {
	if len(messages) == 0 {
		return ErrEmptyBatch
	}
	if len(messages) > s.limits.MaxMessagesPerBatch {
		return fmt.Errorf("%w: got %d, max %d", ErrBatchTooLarge, len(messages), s.limits.MaxMessagesPerBatch)
	}
	for i, raw := range messages {
		if err := validateMessage(raw); err != nil {
			return fmt.Errorf("message[%d]: %w", i, err)
		}
	}
	return nil
}

// Append validates, then atomically appends the batch to the session's
// history and assigns the next seq. Returns the assigned seq plus the
// message count appended. messageID, when non-empty, ties this batch to
// the assistant chat message that produced it; the broker stores it so
// reconnecting clients can re-bucket surfaces inline in the chat
// timeline.
func (s *State) Append(sessionID, messageID string, messages []json.RawMessage) (AppendResult, error) {
	if err := s.Validate(messages); err != nil {
		return AppendResult{}, err
	}

	sess := s.getOrCreate(sessionID)
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if len(sess.messages)+len(messages) > s.limits.MaxRetainedMessages {
		return AppendResult{}, fmt.Errorf("%w: have %d, batch %d, max %d",
			ErrRetentionExceeded, len(sess.messages), len(messages), s.limits.MaxRetainedMessages)
	}

	sess.lastSeq++
	seq := sess.lastSeq
	// Defensive copy so the caller can reuse / mutate their slice.
	groupMessages := make([]json.RawMessage, 0, len(messages))
	for _, m := range messages {
		clone := append(json.RawMessage(nil), m...)
		sess.messages = append(sess.messages, clone)
		groupMessages = append(groupMessages, clone)
	}
	sess.groups = append(sess.groups, SnapshotGroup{
		MessageID: messageID,
		Messages:  groupMessages,
	})
	sess.updatedAt = time.Now().UTC()

	return AppendResult{Seq: seq, MessageCount: len(messages)}, nil
}

// Snapshot returns a copy of the session's retained A2UI state. Returns
// (zero, false) when the session has no retained state.
func (s *State) Snapshot(sessionID string) (Snapshot, bool) {
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return Snapshot{}, false
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	if len(sess.messages) == 0 {
		return Snapshot{}, false
	}

	out := Snapshot{
		Version:     ProtocolVersion,
		Seq:         sess.lastSeq,
		Messages:    make([]json.RawMessage, len(sess.messages)),
		Groups:      make([]SnapshotGroup, 0, len(sess.groups)),
		Submissions: make([]SubmissionRecord, 0, len(sess.submissions)),
		UpdatedAt:   sess.updatedAt,
	}
	for i, m := range sess.messages {
		out.Messages[i] = append(json.RawMessage(nil), m...)
	}
	for _, g := range sess.groups {
		clone := SnapshotGroup{
			MessageID: g.MessageID,
			Messages:  make([]json.RawMessage, len(g.Messages)),
		}
		for i, m := range g.Messages {
			clone.Messages[i] = append(json.RawMessage(nil), m...)
		}
		out.Groups = append(out.Groups, clone)
	}
	for _, rec := range sess.submissions {
		clone := rec
		if len(rec.Values) > 0 {
			clone.Values = append(json.RawMessage(nil), rec.Values...)
		}
		out.Submissions = append(out.Submissions, clone)
	}
	return out, true
}

// Reset drops all retained state for a session. Idempotent.
func (s *State) Reset(sessionID string) {
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
}

// RecordLatestResponseID stores the most recent assistant responseId
// observed for this session. Empty input is ignored. The broker uses
// this as a fallback message_id when a caller injects an A2UI batch
// without specifying one, so the surface still anchors to a real chat
// bubble in the client.
func (s *State) RecordLatestResponseID(sessionID, responseID string) {
	if sessionID == "" || responseID == "" {
		return
	}
	sess := s.getOrCreate(sessionID)
	sess.mu.Lock()
	sess.latestResponseID = responseID
	sess.mu.Unlock()
}

// LatestResponseID returns the most recent assistant responseId
// recorded for this session, or "" if none is known.
func (s *State) LatestResponseID(sessionID string) string {
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return ""
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.latestResponseID
}

// RecordSubmission saves the user's response to an A2UI surface.
// Status is "submitted" or "cancelled". Values is opaque JSON. Repeat
// calls overwrite the prior record for the same surfaceID — the broker
// keeps only the latest response per surface.
func (s *State) RecordSubmission(sessionID string, rec SubmissionRecord) {
	if sessionID == "" || rec.SurfaceID == "" {
		return
	}
	if rec.At.IsZero() {
		rec.At = time.Now().UTC()
	}
	if rec.Status == "" {
		rec.Status = "submitted"
	}
	sess := s.getOrCreate(sessionID)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.submissions == nil {
		sess.submissions = map[string]SubmissionRecord{}
	}
	clone := rec
	if len(rec.Values) > 0 {
		clone.Values = append(json.RawMessage(nil), rec.Values...)
	}
	sess.submissions[rec.SurfaceID] = clone
	sess.updatedAt = time.Now().UTC()
}

func (s *State) getOrCreate(sessionID string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		sess = &session{}
		s.sessions[sessionID] = sess
	}
	return sess
}

// validateMessage enforces the per-message invariants:
//   - decodes as a JSON object
//   - declares version == ProtocolVersion
//   - if it carries createSurface, the catalogId is in AllowedCatalogIDs
func validateMessage(raw json.RawMessage) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ErrMessageMalformed
	}

	versionRaw, ok := probe["version"]
	if !ok {
		return ErrInvalidVersion
	}
	var version string
	if err := json.Unmarshal(versionRaw, &version); err != nil {
		return ErrInvalidVersion
	}
	if version != ProtocolVersion {
		return ErrInvalidVersion
	}

	if csRaw, ok := probe["createSurface"]; ok {
		var cs struct {
			CatalogID string `json:"catalogId"`
		}
		if err := json.Unmarshal(csRaw, &cs); err != nil {
			return ErrMessageMalformed
		}
		if cs.CatalogID != "" {
			if _, allowed := AllowedCatalogIDs[cs.CatalogID]; !allowed {
				return fmt.Errorf("%w: %q", ErrInvalidCatalogID, cs.CatalogID)
			}
		}
	}
	return nil
}
