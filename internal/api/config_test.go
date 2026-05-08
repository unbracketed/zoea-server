package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/unbracketed/zoea-server/internal/introspect"
	"github.com/unbracketed/zoea-server/internal/process"
	"github.com/unbracketed/zoea-server/internal/session"
	"github.com/unbracketed/zoea-server/internal/store"
)

func newHandlerWithConfig(t *testing.T, cfg *introspect.Config) *Handler {
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
	sm := session.NewManager(process.NewNoopProcessManager(), st)
	if err := sm.Init(context.Background()); err != nil {
		t.Fatalf("init sm: %v", err)
	}
	return NewHandler(sm, "", cfg)
}

func TestHandleConfig_Available(t *testing.T) {
	captured := time.Now().UTC()
	cfg := &introspect.Config{
		Commands:   []json.RawMessage{json.RawMessage(`{"name":"my-cmd"}`)},
		Tools:      []json.RawMessage{json.RawMessage(`{"name":"bash"}`)},
		CapturedAt: captured,
	}
	h := newHandlerWithConfig(t, cfg)

	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, adminCtx(httptest.NewRequest(http.MethodGet, "/v1/config", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Available  bool              `json:"available"`
		CapturedAt string            `json:"captured_at"`
		Commands   []json.RawMessage `json:"commands"`
		Tools      []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Available {
		t.Fatal("available: got false want true")
	}
	if body.CapturedAt == "" {
		t.Fatal("captured_at empty")
	}
	if len(body.Commands) != 1 || len(body.Tools) != 1 {
		t.Fatalf("counts: commands=%d tools=%d", len(body.Commands), len(body.Tools))
	}
}

func TestHandleConfig_DegradedWhenIntrospectionFailed(t *testing.T) {
	h := newHandlerWithConfig(t, nil)

	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, adminCtx(httptest.NewRequest(http.MethodGet, "/v1/config", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	var body struct {
		Available bool              `json:"available"`
		Commands  []json.RawMessage `json:"commands"`
		Tools     []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Available {
		t.Fatal("available: got true want false")
	}
	if body.Commands == nil || body.Tools == nil {
		t.Fatal("commands/tools must be empty arrays, not null")
	}
}

func TestHandleConfig_RejectsNonGET(t *testing.T) {
	h := newHandlerWithConfig(t, nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, adminCtx(httptest.NewRequest(http.MethodPost, "/v1/config", nil)))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d want 405", rec.Code)
	}
}
