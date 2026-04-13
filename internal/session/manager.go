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

	"github.com/brian/go-agent-gateway/internal/gateway"
	"github.com/brian/go-agent-gateway/internal/process"
	"github.com/brian/go-agent-gateway/internal/store"
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
	mu       sync.RWMutex
	counter  uint64
	handles  map[string]process.AgentHandle // runtime handles only
	pm       process.Manager
	store    store.Store
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

func (m *Manager) Create(ctx context.Context, userID, projectID, externalID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.counter++
	sid := fmt.Sprintf("s_%06d", m.counter)

	h, err := m.pm.Start(ctx, process.StartOptions{
		SessionID: sid,
		UserID:    userID,
		ProjectID: projectID,
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

func (s *Session) Subscribe(ctx context.Context) (<-chan gateway.Event, func()) {
	s.LastActive = time.Now().UTC()
	return s.handle.Subscribe(ctx)
}

func (s *Session) SendUIResponse(ctx context.Context, resp process.UIResponse) error {
	s.LastActive = time.Now().UTC()
	return s.handle.SendUIResponse(ctx, resp)
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

	// Try to get messages from pi.
	msgs, err := h.GetMessages(context.Background())
	if err != nil {
		log.Printf("[persist] session %s: get_messages failed: %v", sessionID, err)

		// Fallback: try parsing from RunEnd event data.
		msgs = parseRunEndMessages(evt)
		if len(msgs) == 0 {
			return
		}
	}

	records := make([]store.MessageRecord, 0, len(msgs))
	for _, msg := range msgs {
		records = append(records, store.MessageRecord{
			SessionID: sessionID,
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: now,
		})
	}

	if err := m.store.ReplaceSessionMessages(context.Background(), sessionID, records); err != nil {
		log.Printf("[persist] session %s: store messages failed: %v", sessionID, err)
	}
}

// parseRunEndMessages attempts to extract messages from the RunEnd event data.
func parseRunEndMessages(evt gateway.Event) []process.Message {
	b, err := json.Marshal(evt.Data)
	if err != nil {
		return nil
	}
	var runEnd gateway.RunEnd
	if err := json.Unmarshal(b, &runEnd); err != nil || len(runEnd.Messages) == 0 {
		return nil
	}

	var rawMsgs []struct {
		Role    string      `json:"role"`
		Content interface{} `json:"content"`
	}
	if err := json.Unmarshal(runEnd.Messages, &rawMsgs); err != nil {
		return nil
	}

	out := make([]process.Message, 0, len(rawMsgs))
	for _, m := range rawMsgs {
		content := ""
		switch v := m.Content.(type) {
		case string:
			content = v
		default:
			if b, err := json.Marshal(v); err == nil {
				content = string(b)
			}
		}
		out = append(out, process.Message{Role: m.Role, Content: content})
	}
	return out
}
