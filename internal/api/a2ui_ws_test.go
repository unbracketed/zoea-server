package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/unbracketed/zoea-server/internal/auth"
)

// newAuthedTestServer wraps the handler routes with a middleware that
// stamps an admin auth identity onto every request, then returns a live
// httptest.Server so the websocket upgrade can run end-to-end.
func newAuthedTestServer(t *testing.T, h *Handler) *httptest.Server {
	t.Helper()
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := auth.AuthIdentity{Method: "test", Subject: "tester", Scopes: []string{"admin"}}
		h.Routes().ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), id)))
	})
	srv := httptest.NewServer(wrapped)
	t.Cleanup(srv.Close)
	return srv
}

func dialSessionStream(t *testing.T, srv *httptest.Server, sid string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/sessions/" + sid + "/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	return conn
}

func TestA2UISnapshotReplayedOnConnect(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)

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
	rec := postJSON(t, h.Routes(), "/v1/sessions/"+sid+"/a2ui/messages", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("seed: %d body=%s", rec.Code, rec.Body.String())
	}

	srv := newAuthedTestServer(t, h)
	conn := dialSessionStream(t, srv, sid)
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var frame map[string]any
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if frame["type"] != "agent.a2ui.snapshot" {
		t.Fatalf("expected agent.a2ui.snapshot, got %v frame=%v", frame["type"], frame)
	}
	if frame["session_id"] != sid {
		t.Fatalf("session_id wrong: %v", frame["session_id"])
	}
	data, _ := frame["data"].(map[string]any)
	if data["version"] != "v0.9" {
		t.Fatalf("snapshot version: %v", data["version"])
	}
	if seq, _ := data["seq"].(float64); seq != 1 {
		t.Fatalf("snapshot seq: %v", data["seq"])
	}
	msgs, _ := data["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("snapshot messages: %v", data["messages"])
	}
}

func TestA2UISnapshotSkippedWhenNoState(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	srv := newAuthedTestServer(t, h)

	conn := dialSessionStream(t, srv, sid)
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	var frame map[string]any
	if err := conn.ReadJSON(&frame); err == nil {
		t.Fatalf("expected no frame, got %v", frame)
	}
}

func TestA2UIActionInboundForwardedToRuntime(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	srv := newAuthedTestServer(t, h)

	conn := dialSessionStream(t, srv, sid)
	defer conn.Close()

	frame := map[string]any{
		"type": "a2ui.action",
		"data": map[string]any{
			"message": map[string]any{
				"version": "v0.9",
				"action": map[string]any{
					"name":              "submit",
					"surfaceId":         "main",
					"sourceComponentId": "submit_btn",
					"timestamp":         "2026-05-01T12:00:05Z",
					"context":           map[string]any{},
				},
			},
		},
	}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("write action: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var got map[string]any
		if err := conn.ReadJSON(&got); err != nil {
			t.Fatalf("read echo: %v", err)
		}
		if got["type"] == "agent.a2ui.action_received" {
			return
		}
	}
}

func TestA2UIActionRejectsBadVersion(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	srv := newAuthedTestServer(t, h)

	conn := dialSessionStream(t, srv, sid)
	defer conn.Close()

	frame := map[string]any{
		"type": "a2ui.action",
		"data": map[string]any{
			"message": map[string]any{
				"version": "v0.8",
				"action":  map[string]any{"name": "submit", "surfaceId": "main"},
			},
		},
	}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("write action: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var got map[string]any
		if err := conn.ReadJSON(&got); err != nil {
			t.Fatalf("read error frame: %v", err)
		}
		if got["type"] == "agent.a2ui.error" {
			data, _ := got["data"].(map[string]any)
			errStr, _ := data["error"].(string)
			if !strings.Contains(errStr, "v0.9") {
				t.Fatalf("error should mention version: %v", data)
			}
			return
		}
	}
}

func TestA2UIActionRejectsMissingAction(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	srv := newAuthedTestServer(t, h)

	conn := dialSessionStream(t, srv, sid)
	defer conn.Close()

	frame := map[string]any{
		"type": "a2ui.action",
		"data": map[string]any{
			"message": map[string]any{
				"version": "v0.9",
			},
		},
	}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("write action: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var got map[string]any
		if err := conn.ReadJSON(&got); err != nil {
			t.Fatalf("read error frame: %v", err)
		}
		if got["type"] == "agent.a2ui.error" {
			return
		}
	}
}
