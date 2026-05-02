package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unbracketed/zoea-server/internal/auth"
	"github.com/unbracketed/zoea-server/internal/gateway"
	"github.com/unbracketed/zoea-server/internal/process"
	"github.com/unbracketed/zoea-server/internal/session"
	"github.com/unbracketed/zoea-server/internal/store"
)

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

func postJSON(t *testing.T, h http.Handler, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := adminCtx(httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newTestHandler(t *testing.T) (*Handler, *session.Manager, store.Store) {
	return newTestHandlerWithPM(t, process.NewNoopProcessManager())
}

func newTestHandlerWithPM(t *testing.T, pm process.Manager) (*Handler, *session.Manager, store.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.Init(context.Background()); err != nil {
		t.Fatalf("init store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	sm := session.NewManager(pm, st)
	if err := sm.Init(context.Background()); err != nil {
		t.Fatalf("init sessions: %v", err)
	}
	return NewHandler(sm), sm, st
}

type recordingProcessManager struct {
	lastOpts process.StartOptions
}

func (m *recordingProcessManager) Start(_ context.Context, opts process.StartOptions) (process.AgentHandle, error) {
	m.lastOpts = opts
	return recordingHandle{}, nil
}

type recordingHandle struct{}

func (recordingHandle) Prompt(context.Context, process.PromptRequest) error { return nil }
func (recordingHandle) Abort(context.Context) error                         { return nil }
func (recordingHandle) GetState(context.Context) (process.State, error)     { return process.State{}, nil }
func (recordingHandle) GetMessages(context.Context) ([]process.Message, error) {
	return nil, nil
}
func (recordingHandle) GetMessagesRaw(context.Context) ([]json.RawMessage, error) {
	return nil, nil
}
func (recordingHandle) Subscribe(context.Context) (<-chan gateway.Event, func()) {
	ch := make(chan gateway.Event)
	close(ch)
	return ch, func() {}
}
func (recordingHandle) SendUIResponse(context.Context, process.UIResponse) error { return nil }
func (recordingHandle) SendA2UIAction(context.Context, process.A2UIActionRequest) error {
	return nil
}
func (recordingHandle) Broadcast(gateway.Event)     {}
func (recordingHandle) Close(context.Context) error { return nil }

func adminCtx(r *http.Request) *http.Request {
	id := auth.AuthIdentity{Method: "test", Subject: "tester", Scopes: []string{"admin"}}
	return r.WithContext(auth.WithIdentity(r.Context(), id))
}

func createTestSession(t *testing.T, sm *session.Manager) string {
	t.Helper()
	s, err := sm.Create(context.Background(), "alice", "", "", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return s.ID
}

func TestCreateSessionPassesWorkingDir(t *testing.T) {
	pm := &recordingProcessManager{}
	h, _, _ := newTestHandlerWithPM(t, pm)
	workingDir := t.TempDir()

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(`{"user_id":"alice","working_dir":"`+workingDir+`"}`)))
	req.Header.Set("Content-Type", "application/json")
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if pm.lastOpts.UserID != "alice" {
		t.Fatalf("user_id: got %q", pm.lastOpts.UserID)
	}
	if pm.lastOpts.WorkingDir != workingDir {
		t.Fatalf("working_dir: got %q want %q", pm.lastOpts.WorkingDir, workingDir)
	}
}

// promptAndAwaitPersist sends a prompt and waits for the run.end event before
// returning, so the persist goroutine has time to finish writing without
// racing the next DB query in the test.
func promptAndAwaitPersist(t *testing.T, s *session.Session, msg string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, unsub := s.Subscribe(ctx)
	defer unsub()

	if err := s.Prompt(context.Background(), process.PromptRequest{Message: msg}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	for {
		select {
		case e, ok := <-events:
			if !ok {
				return
			}
			if e.Type == "agent.run.end" {
				// Give the background persist a small window to complete.
				return
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for run.end")
		}
	}
}

func TestMessagesEndpointDefaultsToText(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)

	s, _ := sm.Get(sid)
	promptAndAwaitPersist(t, s, "hi")

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/sessions/"+sid+"/messages", nil))
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Format   string `json:"format"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if resp.Format != "text" {
		t.Fatalf("format: got %q", resp.Format)
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(resp.Messages))
	}
	if resp.Messages[0].Content != "hi" {
		t.Fatalf("first content: got %q", resp.Messages[0].Content)
	}
}

func TestMessagesEndpointFormatRaw(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)

	s, _ := sm.Get(sid)
	promptAndAwaitPersist(t, s, "hi")

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/sessions/"+sid+"/messages?format=raw", nil))
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Format   string            `json:"format"`
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if resp.Format != "raw" {
		t.Fatalf("format: got %q", resp.Format)
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(resp.Messages))
	}

	// Each entry must be a JSON object with role + content array (rich shape).
	for i, raw := range resp.Messages {
		var msg struct {
			Role    string            `json:"role"`
			Content []json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("msg %d not valid JSON: %v", i, err)
		}
		if msg.Role == "" {
			t.Fatalf("msg %d: empty role", i)
		}
		if len(msg.Content) == 0 {
			t.Fatalf("msg %d: expected content array, got empty", i)
		}
	}
}

func TestMessagesEndpointInvalidFormat(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/sessions/"+sid+"/messages?format=banana", nil))
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid format") {
		t.Fatalf("expected invalid format error, got %s", rec.Body.String())
	}
}
