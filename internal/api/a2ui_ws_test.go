package api

import (
	"encoding/json"
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

// Snapshot replay carries the per-batch message_id grouping so a
// reconnecting client can re-bucket surfaces inline in the chat
// timeline (the canonical chat-channel A2UI flow).
func TestA2UISnapshotReplayIncludesMessageIDGroups(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)

	body, _ := json.Marshal(map[string]any{
		"message_id": "asst_xyz",
		"messages": []map[string]any{
			{
				"version": "v0.9",
				"createSurface": map[string]any{
					"surfaceId":     "main",
					"catalogId":     "https://a2ui.org/specification/v0_9/basic_catalog.json",
					"sendDataModel": true,
				},
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
		t.Fatalf("expected agent.a2ui.snapshot, got %v", frame["type"])
	}
	data, _ := frame["data"].(map[string]any)
	groups, ok := data["groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("expected one group, got %v", data["groups"])
	}
	g0, _ := groups[0].(map[string]any)
	if g0["message_id"] != "asst_xyz" {
		t.Fatalf("group message_id: got %v want asst_xyz", g0["message_id"])
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

// Server-side agents (e.g. BASIL's a2ui flow runtime when subscribed
// directly to the session WS) close the action loop by reading the
// agent.a2ui.action broadcast — the relay event the handler emits as
// soon as a valid a2ui.action frame is received, before forwarding to
// the Pi RPC seam.
func TestA2UIActionRelayBroadcastToSubscribers(t *testing.T) {
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
			"client_data_model": map[string]any{
				"surfaces": map[string]any{
					"main": map[string]any{"inputs": map[string]any{"name": "Alice"}},
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
			t.Fatalf("read relay event: %v", err)
		}
		if got["type"] != "agent.a2ui.action" {
			continue
		}
		data, _ := got["data"].(map[string]any)
		// The relay carries the original action message verbatim plus
		// the optional metadata fields.
		msg, ok := data["message"].(map[string]any)
		if !ok {
			t.Fatalf("relay missing message: %v", data)
		}
		action, ok := msg["action"].(map[string]any)
		if !ok {
			t.Fatalf("relay message missing action: %v", msg)
		}
		if action["surfaceId"] != "main" {
			t.Fatalf("relay surfaceId: got %v want main", action["surfaceId"])
		}
		cdm, ok := data["client_data_model"].(map[string]any)
		if !ok {
			t.Fatalf("relay missing client_data_model: %v", data)
		}
		_ = cdm
		return
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

// a2ui.submit is the canonical chat-channel path for surface
// submissions: the server translates the form payload into a normal
// user-turn prompt, so the agent's existing turn-taking handles it.
// The noop runtime echoes a synthetic agent.text.delta when Prompt is
// called, which gives us a deterministic signal that the prompt landed.
func TestA2UISubmitTriggersUserPrompt(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	srv := newAuthedTestServer(t, h)

	conn := dialSessionStream(t, srv, sid)
	defer conn.Close()

	frame := map[string]any{
		"type": "a2ui.submit",
		"data": map[string]any{
			"message_id":  "asst_form_owner",
			"surface_id":  "main",
			"action_name": "submit",
			"text":        "Booked the flight",
			"values": map[string]any{
				"name": "Alice",
				"date": "2026-06-01",
			},
		},
	}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("write submit: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	sawPrompt := false
	for {
		var got map[string]any
		if err := conn.ReadJSON(&got); err != nil {
			t.Fatalf("read events: %v (sawPrompt=%v)", err, sawPrompt)
		}
		if got["type"] == "agent.text.delta" {
			sawPrompt = true
			break
		}
	}
	if !sawPrompt {
		t.Fatal("expected a synthetic prompt-derived event from noop runtime")
	}

	// The transcript should now contain the user message we synthesised
	// (carrying the a2ui-submission JSON envelope).
	s, err := sm.Get(sid)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	msgs, err := s.Messages(testCtx(t))
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	foundUser := false
	for _, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, "a2ui_submission") && strings.Contains(m.Content, "Alice") {
			foundUser = true
			break
		}
	}
	if !foundUser {
		t.Fatalf("expected user message carrying a2ui_submission, got %v", msgs)
	}
}

func TestA2UISubmitRejectsMissingSurfaceID(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	srv := newAuthedTestServer(t, h)

	conn := dialSessionStream(t, srv, sid)
	defer conn.Close()

	frame := map[string]any{
		"type": "a2ui.submit",
		"data": map[string]any{
			"action_name": "submit",
			"values":      map[string]any{},
		},
	}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("write submit: %v", err)
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
			if !strings.Contains(errStr, "surface_id") {
				t.Fatalf("error should mention surface_id: %v", errStr)
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
