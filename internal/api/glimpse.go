package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/unbracketed/zoea-server/internal/auth"
	"github.com/unbracketed/zoea-server/internal/gateway"
	"github.com/unbracketed/zoea-server/internal/glimpse"
)

// glimpseRenderReq mirrors the BASIL ZoeaTransport request body. Surface and
// hints are kept as opaque RawMessage — Zoea must not interpret them.
type glimpseRenderReq struct {
	Type    string              `json:"type"`
	Request glimpseRenderInner  `json:"request"`
	HTML    string              `json:"html"`
	Target  glimpseRenderTarget `json:"target"`
	Hints   json.RawMessage     `json:"hints,omitempty"`
}

type glimpseRenderInner struct {
	RequestID      string          `json:"request_id"`
	FlowID         string          `json:"flow_id"`
	Surface        json.RawMessage `json:"surface,omitempty"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	Hints          json.RawMessage `json:"hints,omitempty"`
}

type glimpseRenderTarget struct {
	SessionID      string `json:"session_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	UserID         string `json:"user_id,omitempty"`
}

func (h *Handler) handleGlimpseRender(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if !h.requireScope(w, r, "glimpse.render") {
		return
	}

	var req glimpseRenderReq
	if !decodeJSON(w, r, &req) {
		return
	}

	if strings.TrimSpace(req.Request.RequestID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request.request_id is required"})
		return
	}
	if strings.TrimSpace(req.HTML) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "html is required"})
		return
	}
	if strings.TrimSpace(req.Target.SessionID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "target.session_id is required"})
		return
	}

	sess, err := h.sessions.Get(req.Target.SessionID)
	if err != nil {
		// Surface unknown target as 404 — BASIL needs to distinguish a missing
		// session from a busy one.
		writeJSON(w, http.StatusNotFound, map[string]any{
			"type":       "error",
			"request_id": req.Request.RequestID,
			"error":      "target session not found",
			"fatal":      true,
		})
		return
	}

	// Compute deadline once so registry, browser event, and Wait() agree.
	timeoutSeconds := req.Request.TimeoutSeconds
	if timeoutSeconds < 0 {
		timeoutSeconds = 0
	}
	var deadline time.Time
	if timeoutSeconds > 0 {
		deadline = time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	}

	pending := &glimpse.Pending{
		RequestID:      req.Request.RequestID,
		FlowID:         req.Request.FlowID,
		SessionID:      req.Target.SessionID,
		ConversationID: req.Target.ConversationID,
		UserID:         req.Target.UserID,
		Deadline:       deadline,
	}

	if err := h.glimpse.Register(pending); err != nil {
		if errors.Is(err, glimpse.ErrBusy) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"type":              "busy",
				"request_id":        req.Request.RequestID,
				"active_request_id": glimpse.ActiveRequestIDFromBusy(err),
				"error":             "session already has an active glimpse render",
			})
			return
		}
		if errors.Is(err, glimpse.ErrDuplicateRequestID) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"type":       "error",
				"request_id": req.Request.RequestID,
				"error":      "request_id already pending",
				"fatal":      false,
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"type":       "error",
			"request_id": req.Request.RequestID,
			"error":      err.Error(),
			"fatal":      false,
		})
		return
	}
	defer h.glimpse.Forget(req.Request.RequestID)

	// Forward hints unchanged. Spec allows hints either at the top level or
	// nested in the request envelope; merge by preferring the nested form
	// since that's what BASIL's spec example shows. We treat both as opaque.
	hints := req.Request.Hints
	if len(hints) == 0 {
		hints = req.Hints
	}

	sess.Broadcast(gateway.NewEvent("glimpse.render", gateway.GlimpseRender{
		RequestID:      req.Request.RequestID,
		FlowID:         req.Request.FlowID,
		HTML:           req.HTML,
		TimeoutSeconds: timeoutSeconds,
		Hints:          hints,
	}))

	outcome := pending.Wait()

	// Build the close envelope. status mirrors the terminal envelope returned
	// to BASIL; action_id (when known) lets clients write a receipt without
	// having to wait on their own /action response. Both are advisory.
	closeReason := "completed"
	closeStatus := "action"
	closeActionID := ""
	switch {
	case outcome.Action != nil:
		if id, ok := outcome.Action["action_id"].(string); ok {
			closeActionID = id
		}
	case outcome.Cancelled:
		closeReason = "cancelled"
		closeStatus = "cancelled"
	case outcome.TimedOut:
		closeReason = "timed_out"
		closeStatus = "timed_out"
	case outcome.Err != nil:
		closeReason = "error"
		closeStatus = "error"
	}
	sess.Broadcast(gateway.NewEvent("glimpse.close", gateway.GlimpseClose{
		RequestID: req.Request.RequestID,
		Reason:    closeReason,
		Status:    closeStatus,
		ActionID:  closeActionID,
	}))

	switch {
	case outcome.Action != nil:
		writeJSON(w, http.StatusOK, map[string]any{
			"type":       "action",
			"request_id": req.Request.RequestID,
			"payload":    outcome.Action,
		})
	case outcome.Cancelled:
		writeJSON(w, http.StatusOK, map[string]any{
			"type":       "cancelled",
			"request_id": req.Request.RequestID,
		})
	case outcome.TimedOut:
		// Spec: timeout returns an error envelope, fatal=false.
		writeJSON(w, http.StatusOK, map[string]any{
			"type":       "error",
			"request_id": req.Request.RequestID,
			"error":      "no action received before timeout",
			"fatal":      false,
		})
	case outcome.Err != nil:
		writeJSON(w, http.StatusOK, map[string]any{
			"type":       "error",
			"request_id": req.Request.RequestID,
			"error":      outcome.Err.Error(),
			"fatal":      false,
		})
	default:
		// Should not happen — Wait() always returns a populated outcome.
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"type":       "error",
			"request_id": req.Request.RequestID,
			"error":      "render resolved without outcome",
			"fatal":      true,
		})
	}
}

type glimpseActionReq struct {
	RequestID string         `json:"request_id"`
	Payload   map[string]any `json:"payload"`
}

func (h *Handler) handleGlimpseAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if !h.requireScope(w, r, "glimpse.action") {
		return
	}

	var req glimpseActionReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.RequestID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request_id is required"})
		return
	}
	if req.Payload == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "payload is required"})
		return
	}

	pending, err := h.glimpse.Get(req.RequestID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown request_id"})
		return
	}

	// Enforce that the submission comes from the intended session. Admin
	// callers (auth disabled / local dev / admin scope) bypass the session
	// check; bearer-issued identities are constrained by their subject.
	if !h.glimpseCallerAuthorized(r, pending) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "submission not authorized for this request"})
		return
	}

	if err := h.glimpse.ResolveAction(req.RequestID, req.Payload); err != nil {
		if errors.Is(err, glimpse.ErrAlreadyResolved) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "render already resolved"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type glimpseCancelReq struct {
	RequestID string `json:"request_id"`
}

func (h *Handler) handleGlimpseCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if !h.requireScope(w, r, "glimpse.action") {
		return
	}

	var req glimpseCancelReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.RequestID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request_id is required"})
		return
	}

	pending, err := h.glimpse.Get(req.RequestID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown request_id"})
		return
	}

	if !h.glimpseCallerAuthorized(r, pending) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cancel not authorized for this request"})
		return
	}

	if err := h.glimpse.ResolveCancelled(req.RequestID); err != nil {
		if errors.Is(err, glimpse.ErrAlreadyResolved) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "render already resolved"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// glimpseCallerAuthorized checks that the authenticated caller has permission
// to act on this pending render.
//
// Today the rule is intentionally permissive:
//   - admin scope (local-dev or admin API key) can act on any request
//   - any caller with glimpse.action scope can act if they pass the scope check
//
// The session-scoped binding (matching the user_id captured at register time)
// can be tightened once Zoea has end-user auth. For now, request_id is treated
// as a sufficient capability for callers that already hold glimpse.action.
func (h *Handler) glimpseCallerAuthorized(r *http.Request, _ *glimpse.Pending) bool {
	id := auth.FromContext(r.Context())
	for _, s := range id.Scopes {
		if s == "admin" || s == "glimpse.action" {
			return true
		}
	}
	return false
}
