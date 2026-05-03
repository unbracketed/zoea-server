package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/unbracketed/zoea-server/internal/a2ui"
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
	WorkingDir string    `json:"working_dir,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastActive time.Time `json:"last_active"`
	handle     process.AgentHandle
}

type ListQuery struct {
	UserID     string
	ExternalID string
	WorkingDir string
	Limit      int
	Offset     int
}

type Manager struct {
	mu        sync.RWMutex
	counter   uint64
	handles   map[string]process.AgentHandle // runtime handles only
	pm        process.Manager
	store     store.Store
	a2uiState *a2ui.State
}

func NewManager(pm process.Manager, st store.Store) *Manager {
	return &Manager{
		handles: map[string]process.AgentHandle{},
		pm:      pm,
		store:   st,
	}
}

// AttachA2UIState wires the broker so the manager can fall back to the
// session's most recent assistant responseId when an A2UI inject
// arrives without an explicit message_id. Must be called before Create
// for the per-session tracker goroutine to start.
func (m *Manager) AttachA2UIState(state *a2ui.State) {
	m.a2uiState = state
}

// trackResponseIDsForA2UI subscribes to the session's gateway stream
// and records the latest assistant responseId on every agent.message.*
// event. This is best-effort: if the message JSON has no responseId
// (e.g. older Pi build), we just skip it and the broker keeps its
// previous value.
func (m *Manager) trackResponseIDsForA2UI(sessionID string, h process.AgentHandle) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, unsub := h.Subscribe(ctx)
	defer unsub()

	for evt := range events {
		switch evt.Type {
		case "agent.message.start", "agent.message.end":
		default:
			continue
		}
		id := extractResponseID(evt)
		if id != "" {
			m.a2uiState.RecordLatestResponseID(sessionID, id)
		}
	}
}

// extractResponseID pulls responseId out of a MessageStart/End data
// payload. The data field is opaque from the rpc mapper's perspective,
// so we re-marshal and probe.
func extractResponseID(evt gateway.Event) string {
	b, err := json.Marshal(evt.Data)
	if err != nil {
		return ""
	}
	var probe struct {
		Message struct {
			ResponseID string `json:"responseId"`
		} `json:"message"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return ""
	}
	return probe.Message.ResponseID
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

	startOpts := process.StartOptions{
		SessionID:  sid,
		UserID:     userID,
		ProjectID:  projectID,
		WorkingDir: workingDir,
	}

	// Resolve the effective working-dir up front so we can persist it
	// alongside the session metadata. Resume reads this back to spawn Pi
	// in the same cwd, and the session-dir slug is derived from it.
	resolvedWorkingDir, err := m.pm.ResolveWorkingDir(startOpts)
	if err != nil {
		return nil, err
	}

	h, err := m.pm.Start(ctx, startOpts)
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
		WorkingDir:   resolvedWorkingDir,
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
		WorkingDir: resolvedWorkingDir,
		CreatedAt:  now,
		LastActive: now,
		handle:     h,
	}

	// Start background message persistence listener.
	go m.bumpLastActiveOnRunEnd(sid, h)
	// Watch the same gateway stream for assistant responseIds so the
	// A2UI broker can fall back to "latest assistant turn" when an
	// inject arrives without an explicit message_id.
	if m.a2uiState != nil {
		go m.trackResponseIDsForA2UI(sid, h)
	}

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
		WorkingDir: rec.WorkingDir,
		CreatedAt:  rec.CreatedAt,
		LastActive: rec.LastActiveAt,
		handle:     h,
	}, nil
}

// Resume re-attaches a stored session that has no live agent process — the
// usual case after a server restart. Spawns a fresh Pi process pointed at
// the original session-dir so the on-disk transcript is reloaded, and
// re-registers the runtime handle. Idempotent: if a handle already exists
// for the session, returns it without spawning a second process.
func (m *Manager) Resume(ctx context.Context, sessionID string) (*Session, error) {
	rec, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	m.mu.Lock()
	if h, ok := m.handles[sessionID]; ok {
		m.mu.Unlock()
		return &Session{
			ID:         rec.ID,
			UserID:     rec.UserID,
			ProjectID:  rec.ProjectID,
			ExternalID: rec.ExternalID,
			WorkingDir: rec.WorkingDir,
			CreatedAt:  rec.CreatedAt,
			LastActive: rec.LastActiveAt,
			handle:     h,
		}, nil
	}

	h, err := m.pm.Start(ctx, process.StartOptions{
		SessionID:  rec.ID,
		UserID:     rec.UserID,
		ProjectID:  rec.ProjectID,
		WorkingDir: rec.WorkingDir,
	})
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("resume session: %w", err)
	}
	m.handles[sessionID] = h
	m.mu.Unlock()

	now := time.Now().UTC()
	_ = m.store.UpdateSessionActivity(ctx, sessionID, now)

	go m.bumpLastActiveOnRunEnd(sessionID, h)
	if m.a2uiState != nil {
		go m.trackResponseIDsForA2UI(sessionID, h)
	}

	return &Session{
		ID:         rec.ID,
		UserID:     rec.UserID,
		ProjectID:  rec.ProjectID,
		ExternalID: rec.ExternalID,
		WorkingDir: rec.WorkingDir,
		CreatedAt:  rec.CreatedAt,
		LastActive: now,
		handle:     h,
	}, nil
}

func (m *Manager) List(ctx context.Context, q ListQuery) ([]store.SessionRecord, error) {
	return m.store.ListSessions(ctx, store.ListSessionsQuery{
		UserID:     q.UserID,
		ExternalID: q.ExternalID,
		WorkingDir: q.WorkingDir,
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

// bumpLastActiveOnRunEnd watches for agent.run.end events and updates the
// session's last_active timestamp in the store. Pi owns the transcript on
// disk (via --session-dir + --continue on resume), so the server no longer
// mirrors messages into SQLite — that mirror was destructive on resume,
// since Pi could legitimately have a shorter transcript than the prior run.
func (m *Manager) bumpLastActiveOnRunEnd(sessionID string, h process.AgentHandle) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, unsub := h.Subscribe(ctx)
	defer unsub()

	for evt := range events {
		if evt.Type != "agent.run.end" {
			continue
		}
		_ = m.store.UpdateSessionActivity(context.Background(), sessionID, time.Now().UTC())
	}
}
