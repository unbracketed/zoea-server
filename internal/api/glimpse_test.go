package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

func renderBody(sessionID, requestID, html string, timeoutSec int) []byte {
	body := map[string]any{
		"type": "render",
		"request": map[string]any{
			"request_id":      requestID,
			"flow_id":         "flow-1",
			"surface":         map[string]any{"step_id": "pick", "title": "Pick something"},
			"timeout_seconds": timeoutSec,
			"hints":           map[string]any{"preferred_mode": "panel"},
		},
		"html": html,
		"target": map[string]any{
			"session_id": sessionID,
		},
	}
	b, _ := json.Marshal(body)
	return b
}

func postJSON(t *testing.T, h http.Handler, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := adminCtx(httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGlimpseRender_BlocksUntilAction(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)

	routes := h.Routes()

	type result struct {
		status int
		body   map[string]any
	}
	resCh := make(chan result, 1)

	go func() {
		rec := postJSON(t, routes, "/api/glimpse/v1/render",
			renderBody(sid, "req-1", "PGh0bWw+", 10))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		resCh <- result{rec.Code, body}
	}()

	// Wait until the registry has the pending render.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := h.glimpse.Get("req-1"); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := h.glimpse.Get("req-1"); err != nil {
		t.Fatalf("render did not register: %v", err)
	}

	// Submit action from "browser".
	actionBody, _ := json.Marshal(map[string]any{
		"request_id": "req-1",
		"payload": map[string]any{
			"request_id": "req-1",
			"action_id":  "continue",
			"raw":        map[string]any{"title_in": "hello"},
		},
	})
	actRec := postJSON(t, routes, "/api/glimpse/v1/action", actionBody)
	if actRec.Code != http.StatusOK {
		t.Fatalf("action status: %d body=%s", actRec.Code, actRec.Body.String())
	}

	select {
	case r := <-resCh:
		if r.status != http.StatusOK {
			t.Fatalf("render status: %d body=%v", r.status, r.body)
		}
		if r.body["type"] != "action" {
			t.Fatalf("expected type=action, got %v", r.body["type"])
		}
		payload, ok := r.body["payload"].(map[string]any)
		if !ok {
			t.Fatalf("payload missing or wrong type: %v", r.body["payload"])
		}
		if payload["action_id"] != "continue" {
			t.Fatalf("expected action_id=continue, got %v", payload["action_id"])
		}
		raw, ok := payload["raw"].(map[string]any)
		if !ok || raw["title_in"] != "hello" {
			t.Fatalf("raw payload not forwarded unchanged: %+v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("render did not complete in time")
	}
}

func TestGlimpseRender_Cancelled(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	resCh := make(chan map[string]any, 1)
	go func() {
		rec := postJSON(t, routes, "/api/glimpse/v1/render",
			renderBody(sid, "req-c", "PGh0bWw+", 10))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		resCh <- body
	}()

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := h.glimpse.Get("req-c"); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancelBody, _ := json.Marshal(map[string]any{"request_id": "req-c"})
	rec := postJSON(t, routes, "/api/glimpse/v1/cancel", cancelBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status: %d body=%s", rec.Code, rec.Body.String())
	}

	select {
	case body := <-resCh:
		if body["type"] != "cancelled" {
			t.Fatalf("expected cancelled, got %v body=%v", body["type"], body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("render did not return after cancel")
	}
}

func TestGlimpseRender_Timeout(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	// timeout=1 second is the smallest unit the spec exposes; we accept it.
	body := map[string]any{
		"type": "render",
		"request": map[string]any{
			"request_id":      "req-t",
			"flow_id":         "flow-1",
			"timeout_seconds": 1,
		},
		"html": "PGh0bWw+",
		"target": map[string]any{
			"session_id": sid,
		},
	}
	b, _ := json.Marshal(body)

	start := time.Now()
	rec := postJSON(t, routes, "/api/glimpse/v1/render", b)
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if elapsed < 800*time.Millisecond {
		t.Fatalf("returned before timeout elapsed: %v", elapsed)
	}

	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["type"] != "error" {
		t.Fatalf("expected type=error on timeout, got %v body=%s", resp["type"], rec.Body.String())
	}
	if !strings.Contains(resp["error"].(string), "timeout") {
		t.Fatalf("expected timeout error, got %v", resp["error"])
	}
}

func TestGlimpseRender_BusyOnSecondConcurrentRender(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	go func() {
		_ = postJSON(t, routes, "/api/glimpse/v1/render",
			renderBody(sid, "req-a", "PGh0bWw+", 10))
	}()

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := h.glimpse.Get("req-a"); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	rec := postJSON(t, routes, "/api/glimpse/v1/render",
		renderBody(sid, "req-b", "PGh0bWw+", 10))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["type"] != "busy" {
		t.Fatalf("expected type=busy, got %v", body["type"])
	}
	if body["active_request_id"] != "req-a" {
		t.Fatalf("expected active_request_id=req-a, got %v", body["active_request_id"])
	}

	// Cleanup so the goroutine exits.
	cancelBody, _ := json.Marshal(map[string]any{"request_id": "req-a"})
	_ = postJSON(t, routes, "/api/glimpse/v1/cancel", cancelBody)
}

func TestGlimpseRender_UnknownTargetReturns404(t *testing.T) {
	h, _, _ := newTestHandler(t)
	routes := h.Routes()

	rec := postJSON(t, routes, "/api/glimpse/v1/render",
		renderBody("does-not-exist", "req-x", "PGh0bWw+", 10))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGlimpseAction_UnknownRequest(t *testing.T) {
	h, _, _ := newTestHandler(t)
	routes := h.Routes()
	body, _ := json.Marshal(map[string]any{
		"request_id": "nope",
		"payload":    map[string]any{"action_id": "continue"},
	})
	rec := postJSON(t, routes, "/api/glimpse/v1/action", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGlimpseAction_AlreadyResolved(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	go func() {
		_ = postJSON(t, routes, "/api/glimpse/v1/render",
			renderBody(sid, "req-d", "PGh0bWw+", 10))
	}()

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := h.glimpse.Get("req-d"); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	first, _ := json.Marshal(map[string]any{
		"request_id": "req-d",
		"payload": map[string]any{
			"request_id": "req-d",
			"action_id":  "continue",
			"raw":        map[string]any{},
		},
	})
	rec := postJSON(t, routes, "/api/glimpse/v1/action", first)
	if rec.Code != http.StatusOK {
		t.Fatalf("first action: %d body=%s", rec.Code, rec.Body.String())
	}

	// The render goroutine forgets req-d after Wait() returns; the second
	// action may race with that — accept either ErrUnknownRequest (404) or
	// ErrAlreadyResolved (409). Both are correct refusals.
	rec2 := postJSON(t, routes, "/api/glimpse/v1/action", first)
	if rec2.Code != http.StatusConflict && rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 409 or 404 on second action, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestGlimpseRender_ValidationErrors(t *testing.T) {
	h, _, _ := newTestHandler(t)
	routes := h.Routes()

	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "missing request_id",
			body: map[string]any{
				"type":    "render",
				"request": map[string]any{},
				"html":    "x",
				"target":  map[string]any{"session_id": "s1"},
			},
			want: "request_id",
		},
		{
			name: "missing html",
			body: map[string]any{
				"type":    "render",
				"request": map[string]any{"request_id": "r1"},
				"target":  map[string]any{"session_id": "s1"},
			},
			want: "html",
		},
		{
			name: "missing session_id",
			body: map[string]any{
				"type":    "render",
				"request": map[string]any{"request_id": "r1"},
				"html":    "x",
				"target":  map[string]any{},
			},
			want: "session_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(tc.body)
			rec := postJSON(t, routes, "/api/glimpse/v1/render", b)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("expected error mentioning %q, got %s", tc.want, rec.Body.String())
			}
		})
	}
}

func TestGlimpseRender_BroadcastsRenderEventToSubscribers(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	s, err := sm.Get(sid)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}

	// Subscribe before launching the render so we definitely catch the event.
	events, unsub := s.Subscribe(testCtx(t))
	defer unsub()

	go func() {
		_ = postJSON(t, routes, "/api/glimpse/v1/render",
			renderBody(sid, "req-e", "PGh0bWwgZW5jb2RlZD4=", 10))
	}()

	saw := false
	for !saw {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("event channel closed before glimpse.render arrived")
			}
			if e.Type == "glimpse.render" {
				saw = true
				// Round-trip through JSON to validate the payload shape.
				b, err := json.Marshal(e.Data)
				if err != nil {
					t.Fatalf("marshal event data: %v", err)
				}
				var data map[string]any
				if err := json.Unmarshal(b, &data); err != nil {
					t.Fatalf("unmarshal event data: %v", err)
				}
				if data["request_id"] != "req-e" {
					t.Fatalf("unexpected request_id: %v", data["request_id"])
				}
				if data["html"] != "PGh0bWwgZW5jb2RlZD4=" {
					t.Fatalf("html not preserved as base64: %v", data["html"])
				}
				// Server must not lift fields out of surface — presentation
				// is client-owned, and the server doesn't interpret schema.
				if _, ok := data["step_id"]; ok {
					t.Fatalf("server leaked step_id into render event: %v", data["step_id"])
				}
				if _, ok := data["title"]; ok {
					t.Fatalf("server leaked title into render event: %v", data["title"])
				}
				// Hints must be forwarded unchanged.
				hints, ok := data["hints"].(map[string]any)
				if !ok {
					t.Fatalf("hints missing or wrong type: %v", data["hints"])
				}
				if hints["preferred_mode"] != "panel" {
					t.Fatalf("hint not forwarded: %v", hints)
				}
			}
		case <-time.After(2 * time.Second):
			t.Fatal("did not see glimpse.render event")
		}
	}

	// Cleanup the still-running render call.
	cancelBody, _ := json.Marshal(map[string]any{"request_id": "req-e"})
	_ = postJSON(t, routes, "/api/glimpse/v1/cancel", cancelBody)
}

func TestGlimpseClose_CarriesActionMetadata(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	s, _ := sm.Get(sid)
	events, unsub := s.Subscribe(testCtx(t))
	defer unsub()

	go func() {
		_ = postJSON(t, routes, "/api/glimpse/v1/render",
			renderBody(sid, "req-close-action", "PGh0bWw+", 10))
	}()

	// Wait for registration, then submit an action.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := h.glimpse.Get("req-close-action"); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	body, _ := json.Marshal(map[string]any{
		"request_id": "req-close-action",
		"payload": map[string]any{
			"request_id": "req-close-action",
			"action_id":  "submit",
			"raw":        map[string]any{},
		},
	})
	_ = postJSON(t, routes, "/api/glimpse/v1/action", body)

	// Drain events until we see glimpse.close.
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("event channel closed before glimpse.close")
			}
			if e.Type != "glimpse.close" {
				continue
			}
			b, _ := json.Marshal(e.Data)
			var data map[string]any
			_ = json.Unmarshal(b, &data)
			if data["request_id"] != "req-close-action" {
				t.Fatalf("wrong request_id: %v", data["request_id"])
			}
			if data["status"] != "action" {
				t.Fatalf("expected status=action, got %v", data["status"])
			}
			if data["action_id"] != "submit" {
				t.Fatalf("expected action_id=submit, got %v", data["action_id"])
			}
			return
		case <-time.After(2 * time.Second):
			t.Fatal("did not see glimpse.close event")
		}
	}
}

func TestGlimpseClose_CarriesCancelledStatus(t *testing.T) {
	h, sm, _ := newTestHandler(t)
	sid := createTestSession(t, sm)
	routes := h.Routes()

	s, _ := sm.Get(sid)
	events, unsub := s.Subscribe(testCtx(t))
	defer unsub()

	go func() {
		_ = postJSON(t, routes, "/api/glimpse/v1/render",
			renderBody(sid, "req-close-cancel", "PGh0bWw+", 10))
	}()

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := h.glimpse.Get("req-close-cancel"); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	body, _ := json.Marshal(map[string]any{"request_id": "req-close-cancel"})
	_ = postJSON(t, routes, "/api/glimpse/v1/cancel", body)

	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("event channel closed before glimpse.close")
			}
			if e.Type != "glimpse.close" {
				continue
			}
			b, _ := json.Marshal(e.Data)
			var data map[string]any
			_ = json.Unmarshal(b, &data)
			if data["status"] != "cancelled" {
				t.Fatalf("expected status=cancelled, got %v", data["status"])
			}
			if _, ok := data["action_id"]; ok {
				t.Fatalf("action_id must be omitted when cancelled, got %v", data["action_id"])
			}
			return
		case <-time.After(2 * time.Second):
			t.Fatal("did not see glimpse.close event")
		}
	}
}
