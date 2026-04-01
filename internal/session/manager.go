package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/brian/go-agent-gateway/internal/gateway"
	"github.com/brian/go-agent-gateway/internal/process"
)

var ErrNotFound = errors.New("session not found")

type Session struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	ProjectID  string    `json:"project_id"`
	CreatedAt  time.Time `json:"created_at"`
	LastActive time.Time `json:"last_active"`
	handle     process.AgentHandle
}

type Manager struct {
	mu       sync.RWMutex
	counter  uint64
	sessions map[string]*Session
	pm       process.Manager
}

func NewManager(pm process.Manager) *Manager {
	return &Manager{
		sessions: map[string]*Session{},
		pm:       pm,
	}
}

func (m *Manager) Create(ctx context.Context, userID, projectID string) (*Session, error) {
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
	s := &Session{
		ID:         sid,
		UserID:     userID,
		ProjectID:  projectID,
		CreatedAt:  now,
		LastActive: now,
		handle:     h,
	}
	m.sessions[sid] = s
	return s, nil
}

func (m *Manager) Get(sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

func (m *Manager) Delete(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	return s.handle.Close(ctx)
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
