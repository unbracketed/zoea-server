package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/unbracketed/zoea-server/internal/gateway"
	"github.com/unbracketed/zoea-server/internal/process"
	"github.com/unbracketed/zoea-server/internal/store"
)

var ErrNotFound = errors.New("session not found")

type Session struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	ProjectID  string    `json:"project_id,omitempty"`
	ExternalID string    `json:"external_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastActive time.Time `json:"last_active"`
	handle     process.AgentHandle
}

type ListQuery struct {
	UserID     string
	ExternalID string
	Limit      int
	Offset     int
}

type Manager struct {
	mu      sync.RWMutex
	counter uint64
	handles map[string]process.AgentHandle // runtime handles only
	pm      process.Manager
	store   store.Store
}

func NewManager(pm process.Manager, st store.Store) *Manager {
	return &Manager{
		handles: map[string]process.AgentHandle{},
		pm:      pm,
		store:   st,
	}
}

// Init seeds the counter from persisted sessions.
func (m *Manager) Init(ctx context.Context) error {
	maxID, err := m.store.GetMaxSessionID(ctx)
	if err != nil {
		return fmt.Errorf("seed counter: %w", err)
	}
	if maxID != "" {
		// Parse number from "s_000005" → 5
		numStr := strings.TrimPrefix(maxID, "s_")
		numStr = strings.TrimLeft(numStr, "0")
		if numStr == "" {
			numStr = "0"
		}
		n, err := strconv.ParseUint(numStr, 10, 64)
		if err == nil && n > m.counter {
			m.counter = n
		}
	}
	return nil
}

func (m *Manager) Create(ctx context.Context, userID, projectID, externalID, workingDir string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.counter++
	sid := fmt.Sprintf("s_%06d", m.counter)

	h, err := m.pm.Start(ctx, process.StartOptions{
		SessionID:  sid,
		UserID:     userID,
		ProjectID:  projectID,
		WorkingDir: workingDir,
	})
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	rec := store.SessionRecord{
		ID:           sid,
		UserID:       userID,
		ProjectID:    projectID,
		ExternalID:   externalID,
		Status:       "active",
		CreatedAt:    now,
		LastActiveAt: now,
	}
	if err := m.store.CreateSession(ctx, rec); err != nil {
		_ = h.Close(ctx)
		return nil, err
	}

	m.handles[sid] = h

	s := &Session{
		ID:         sid,
		UserID:     userID,
		ProjectID:  projectID,
		ExternalID: externalID,
		CreatedAt:  now,
		LastActive: now,
		handle:     h,
	}

	// Start background message persistence listener.
	go m.persistMessagesOnRunEnd(sid, h)

	return s, nil
}

func (m *Manager) Get(sessionID string) (*Session, error) {
	m.mu.RLock()
	h, ok := m.handles[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}

	rec, err := m.store.GetSession(context.Background(), sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &Session{
		ID:         rec.ID,
		UserID:     rec.UserID,
		ProjectID:  rec.ProjectID,
		ExternalID: rec.ExternalID,
		CreatedAt:  rec.CreatedAt,
		LastActive: rec.LastActiveAt,
		handle:     h,
	}, nil
}

func (m *Manager) List(ctx context.Context, q ListQuery) ([]store.SessionRecord, error) {
	return m.store.ListSessions(ctx, store.ListSessionsQuery{
		UserID:     q.UserID,
		ExternalID: q.ExternalID,
		Limit:      q.Limit,
		Offset:     q.Offset,
	})
}

func (m *Manager) Delete(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	h, ok := m.handles[sessionID]
	if ok {
		delete(m.handles, sessionID)
	}
	m.mu.Unlock()

	// Always clean up the store record, even if no live handle exists.
	storeErr := m.store.DeleteSession(ctx, sessionID)

	if ok {
		return h.Close(ctx)
	}

	// No live handle — if the store record existed, that's still a success.
	if storeErr != nil {
		return ErrNotFound
	}
	return nil
}

func (s *Session) Prompt(ctx context.Context, req process.PromptRequest) error {
	s.LastActive = time.Now().UTC()
	return s.handle.Prompt(ctx, req)
}

func (s *Session) Abort(ctx context.Context) error {
	s.LastActive = time.Now().UTC()
	return s.handle.Abort(ctx)
}

func (s *Session) State(ctx context.Context) (process.State, error) {
	s.LastActive = time.Now().UTC()
	return s.handle.GetState(ctx)
}

func (s *Session) Messages(ctx context.Context) ([]process.Message, error) {
	s.LastActive = time.Now().UTC()
	return s.handle.GetMessages(ctx)
}

func (s *Session) MessagesRaw(ctx context.Context) ([]json.RawMessage, error) {
	s.LastActive = time.Now().UTC()
	return s.handle.GetMessagesRaw(ctx)
}

func (s *Session) Subscribe(ctx context.Context) (<-chan gateway.Event, func()) {
	s.LastActive = time.Now().UTC()
	return s.handle.Subscribe(ctx)
}

func (s *Session) SendUIResponse(ctx context.Context, resp process.UIResponse) error {
	s.LastActive = time.Now().UTC()
	return s.handle.SendUIResponse(ctx, resp)
}

// SendA2UIAction forwards an A2UI v0.9 client action to the underlying agent
// runtime. Returns process.ErrA2UIUnsupported when the runtime hasn't
// implemented A2UI input.
func (s *Session) SendA2UIAction(ctx context.Context, req process.A2UIActionRequest) error {
	s.LastActive = time.Now().UTC()
	return s.handle.SendA2UIAction(ctx, req)
}

// Broadcast pushes a synthetic event to all current WS subscribers of this
// session. Used by server-side bridges (e.g. the A2UI broker) that need to
// inject events without going through the agent process.
func (s *Session) Broadcast(event gateway.Event) {
	s.LastActive = time.Now().UTC()
	s.handle.Broadcast(event)
}

// persistMessagesOnRunEnd subscribes to events and persists messages on agent.run.end.
func (m *Manager) persistMessagesOnRunEnd(sessionID string, h process.AgentHandle) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, unsub := h.Subscribe(ctx)
	defer unsub()

	for evt := range events {
		if evt.Type != "agent.run.end" {
			continue
		}

		go func(e gateway.Event) {
			m.persistMessages(sessionID, e, h)
		}(evt)
	}
}

func (m *Manager) persistMessages(sessionID string, evt gateway.Event, h process.AgentHandle) {
	now := time.Now().UTC()

	// Update last active in store.
	_ = m.store.UpdateSessionActivity(context.Background(), sessionID, now)

	// Prefer raw Pi messages so we can persist full-fidelity transcript JSON.
	rawMsgs, err := h.GetMessagesRaw(context.Background())
	if err != nil {
		log.Printf("[persist] session %s: get_messages failed: %v", sessionID, err)

		// Fallback: try parsing raw messages from RunEnd event data.
		rawMsgs = parseRunEndRawMessages(evt)
		if len(rawMsgs) == 0 {
			return
		}
	}

	records := make([]store.MessageRecord, 0, len(rawMsgs))
	for _, raw := range rawMsgs {
		role, preview, model, usageJSON, ts := flattenRawMessage(raw)
		if ts.IsZero() {
			ts = now
		}
		records = append(records, store.MessageRecord{
			SessionID: sessionID,
			Role:      role,
			Content:   preview,
			Model:     model,
			UsageJSON: usageJSON,
			RawJSON:   string(raw),
			Timestamp: ts,
		})
	}

	if err := m.store.ReplaceSessionMessages(context.Background(), sessionID, records); err != nil {
		log.Printf("[persist] session %s: store messages failed: %v", sessionID, err)
	}
}

// flattenRawMessage extracts metadata fields from a raw Pi message.
// Best-effort and non-fatal — missing fields are returned as zero values.
func flattenRawMessage(raw json.RawMessage) (role string, preview string, model string, usageJSON string, ts time.Time) {
	var m struct {
		Role      string          `json:"role"`
		Content   interface{}     `json:"content"`
		Model     string          `json:"model"`
		Usage     json.RawMessage `json:"usage"`
		Timestamp int64           `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", "", "", "", time.Time{}
	}
	role = m.Role
	preview = flattenContentInterface(m.Content)
	model = m.Model
	if len(m.Usage) > 0 {
		usageJSON = string(m.Usage)
	}
	if m.Timestamp > 0 {
		// Pi timestamps are milliseconds since epoch.
		ts = time.UnixMilli(m.Timestamp).UTC()
	}
	return role, preview, model, usageJSON, ts
}

// flattenContentInterface mirrors process.flattenContent but lives here to avoid
// adding more exported surface to the process package.
func flattenContentInterface(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			obj, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if t, ok := obj["text"].(string); ok {
				parts = append(parts, t)
				continue
			}
			if t, ok := obj["thinking"].(string); ok {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

// parseRunEndRawMessages extracts raw Pi messages from an agent.run.end event.
func parseRunEndRawMessages(evt gateway.Event) []json.RawMessage {
	b, err := json.Marshal(evt.Data)
	if err != nil {
		return nil
	}
	var runEnd gateway.RunEnd
	if err := json.Unmarshal(b, &runEnd); err != nil || len(runEnd.Messages) == 0 {
		return nil
	}
	var rawMsgs []json.RawMessage
	if err := json.Unmarshal(runEnd.Messages, &rawMsgs); err != nil {
		return nil
	}
	return rawMsgs
}
