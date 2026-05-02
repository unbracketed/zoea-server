package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func a2uiInjectBody(t *testing.T, messages []map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"messages": messages})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestA2UIInjectAppendsAndBroadcasts(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	s, err := sm.Get(sid)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}

	events, unsub := s.Subscribe(testCtx(t))
	defer unsub()

	body := a2uiInjectBody(t, []map[string]any{
		{
			"version": "v0.9",
			"createSurface": map[string]any{
				"surfaceId":     "main",
				"catalogId":     "https://a2ui.org/specification/v0_9/basic_catalog.json",
				"sendDataModel": true,
			},
		},
	})

	rec := postJSON(t, routes, "/v1/sessions/"+sid+"/a2ui/messages", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if seq, _ := resp["seq"].(float64); seq != 1 {
		t.Fatalf("expected seq=1, got %v", resp["seq"])
	}

	// We should see an agent.a2ui broadcast carrying the same batch.
	saw := false
	deadline := time.After(2 * time.Second)
	for !saw {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event channel closed before agent.a2ui")
			}
			if ev.Type != "agent.a2ui" {
				continue
			}
			b, _ := json.Marshal(ev.Data)
			var data map[string]any
			_ = json.Unmarshal(b, &data)
			if data["version"] != "v0.9" {
				t.Fatalf("version: %v", data["version"])
			}
			if seq, _ := data["seq"].(float64); seq != 1 {
				t.Fatalf("seq: %v", data["seq"])
			}
			messagesJSON, _ := data["messages"].(string)
			if messagesJSON != "" {
				t.Fatalf("messages should not be a JSON-encoded string: %v", messagesJSON)
			}
			messages, ok := data["messages"].([]any)
			if !ok {
				t.Fatalf("messages should decode as array, got %T", data["messages"])
			}
			if len(messages) != 1 {
				t.Fatalf("messages count: %d", len(messages))
			}
			saw = true
		case <-deadline:
			t.Fatal("did not see agent.a2ui event")
		}
	}
}

func TestA2UIInjectRejectsEmptyBatch(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	rec := postJSON(t, routes, "/v1/sessions/"+sid+"/a2ui/messages", []byte(`{"messages":[]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "at least one") {
		t.Fatalf("expected empty-batch error, got %s", rec.Body.String())
	}
}

func TestA2UIInjectRejectsWrongVersion(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	body := a2uiInjectBody(t, []map[string]any{
		{
			"version":          "v0.8",
			"updateComponents": map[string]any{"surfaceId": "main"},
		},
	})
	rec := postJSON(t, routes, "/v1/sessions/"+sid+"/a2ui/messages", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestA2UIInjectRejectsUnknownCatalog(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	body := a2uiInjectBody(t, []map[string]any{
		{
			"version": "v0.9",
			"createSurface": map[string]any{
				"surfaceId": "main",
				"catalogId": "https://example.com/evil-catalog.json",
			},
		},
	})
	rec := postJSON(t, routes, "/v1/sessions/"+sid+"/a2ui/messages", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestA2UIInjectRequiresWriteScope(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	body := a2uiInjectBody(t, []map[string]any{
		{
			"version":          "v0.9",
			"updateComponents": map[string]any{"surfaceId": "main"},
		},
	})

	// Use a request that has no admin scope — should fail.
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+sid+"/a2ui/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without admin scope, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestA2UIInjectAssignsMonotonicSeqAcrossBatches(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	body := a2uiInjectBody(t, []map[string]any{
		{
			"version":          "v0.9",
			"updateComponents": map[string]any{"surfaceId": "main"},
		},
	})
	for i := 1; i <= 3; i++ {
		rec := postJSON(t, routes, "/v1/sessions/"+sid+"/a2ui/messages", body)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("batch %d: status %d body=%s", i, rec.Code, rec.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if seq := int(resp["seq"].(float64)); seq != i {
			t.Fatalf("batch %d: seq=%d", i, seq)
		}
	}
}
