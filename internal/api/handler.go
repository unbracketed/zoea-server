package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/unbracketed/zoea-server/internal/a2ui"
	"github.com/unbracketed/zoea-server/internal/auth"
	"github.com/unbracketed/zoea-server/internal/gateway"
	"github.com/unbracketed/zoea-server/internal/process"
	"github.com/unbracketed/zoea-server/internal/session"
	"github.com/unbracketed/zoea-server/internal/store"
)

type Handler struct {
	sessions *session.Manager
	a2ui     *a2ui.State
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// TODO: tighten origin policy with auth middleware.
		return true
	},
}

func NewHandler(sm *session.Manager) *Handler {
	return &Handler{
		sessions: sm,
		a2ui:     a2ui.NewState(a2ui.Limits{}),
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.handleHealth)
	mux.HandleFunc("/readyz", h.handleReady)
	mux.HandleFunc("/v1/sessions", h.handleSessions)
	mux.HandleFunc("/v1/sessions/", h.handleSessionByID)
	return mux
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleReady(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleCreateSession(w, r)
	case http.MethodGet:
		h.handleListSessions(w, r)
	default:
		writeMethodNotAllowed(w)
	}
}

func (h *Handler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "sessions.write") {
		return
	}

	var req struct {
		UserID     string `json:"user_id"`
		ProjectID  string `json:"project_id"`
		ExternalID string `json:"external_id"`
		WorkingDir string `json:"working_dir"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.UserID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "user_id is required"})
		return
	}

	s, err := h.sessions.Create(r.Context(), req.UserID, req.ProjectID, req.ExternalID, req.WorkingDir)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "external_id already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"session_id": s.ID,
		"status":     "ready",
	})
}

func (h *Handler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "sessions.read") {
		return
	}

	q := session.ListQuery{
		UserID:     r.URL.Query().Get("user_id"),
		ExternalID: r.URL.Query().Get("external_id"),
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.Limit = n
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit"})
			return
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.Offset = n
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid offset"})
			return
		}
	}

	records, err := h.sessions.List(r.Context(), q)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	type sessionEntry struct {
		SessionID    string `json:"session_id"`
		UserID       string `json:"user_id"`
		ProjectID    string `json:"project_id,omitempty"`
		ExternalID   string `json:"external_id,omitempty"`
		Status       string `json:"status"`
		CreatedAt    string `json:"created_at"`
		LastActiveAt string `json:"last_active_at"`
	}

	entries := make([]sessionEntry, 0, len(records))
	for _, rec := range records {
		entries = append(entries, sessionEntry{
			SessionID:    rec.ID,
			UserID:       rec.UserID,
			ProjectID:    rec.ProjectID,
			ExternalID:   rec.ExternalID,
			Status:       rec.Status,
			CreatedAt:    rec.CreatedAt.Format(time.RFC3339),
			LastActiveAt: rec.LastActiveAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"sessions": entries})
}

func (h *Handler) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	sessionID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	subAction := ""
	if len(parts) > 2 {
		subAction = parts[2]
	}

	// Delete doesn't require a live handle — allow deleting store-only records.
	if action == "" && r.Method == http.MethodDelete {
		if !h.requireScope(w, r, "sessions.write") {
			return
		}
		err := h.sessions.Delete(r.Context(), sessionID)
		if err != nil {
			h.writeSessionErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
		return
	}

	s, err := h.sessions.Get(sessionID)
	if err != nil {
		h.writeSessionErr(w, err)
		return
	}

	switch {

	case action == "state" && r.Method == http.MethodGet:
		if !h.requireScope(w, r, "sessions.read") {
			return
		}
		st, err := s.State(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"state": st})
		return

	case action == "messages" && r.Method == http.MethodGet:
		if !h.requireScope(w, r, "sessions.read") {
			return
		}
		format := r.URL.Query().Get("format")
		if format == "" {
			format = "text"
		}
		switch format {
		case "text":
			msgs, err := s.Messages(r.Context())
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"format":   "text",
				"messages": msgs,
			})
		case "raw":
			msgs, err := s.MessagesRaw(r.Context())
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			if msgs == nil {
				msgs = []json.RawMessage{}
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"format":   "raw",
				"messages": msgs,
			})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid format"})
		}
		return

	case action == "messages" && r.Method == http.MethodPost:
		if !h.requireScope(w, r, "sessions.write") {
			return
		}
		var req struct {
			Message           string `json:"message"`
			StreamingBehavior string `json:"streaming_behavior"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.Message) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "message is required"})
			return
		}
		err := s.Prompt(r.Context(), process.PromptRequest{
			Message:           req.Message,
			StreamingBehavior: req.StreamingBehavior,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
		return

	case action == "abort" && r.Method == http.MethodPost:
		if !h.requireScope(w, r, "sessions.write") {
			return
		}
		err := s.Abort(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "aborted"})
		return

	case action == "stream" && r.Method == http.MethodGet:
		if !h.requireScope(w, r, "sessions.read") {
			return
		}
		h.handleSessionStream(w, r, sessionID, s)
		return

	case action == "a2ui" && subAction == "messages" && r.Method == http.MethodPost:
		if !h.requireScope(w, r, "sessions.write") {
			return
		}
		h.handleA2UIInjectMessages(w, r, sessionID, s)
		return

	default:
		writeMethodNotAllowed(w)
		return
	}
}

// handleA2UIInjectMessages is the temporary server-side bridge described in
// docs/specs/zoea-a2ui-session-broker.md. It accepts an A2UI v0.9 batch,
// validates it, appends to the session's retained state, broadcasts an
// agent.a2ui event, and returns the assigned seq. Removed once the runtime
// emits A2UI batches natively.
func (h *Handler) handleA2UIInjectMessages(w http.ResponseWriter, r *http.Request, sessionID string, s *session.Session) {
	limits := h.a2ui.Limits()

	r.Body = http.MaxBytesReader(w, r.Body, int64(limits.MaxRequestBodyBytes))
	defer r.Body.Close()

	var req struct {
		Messages []json.RawMessage `json:"messages"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || errors.Is(err, io.ErrUnexpectedEOF) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
				"error": "request body exceeds limit",
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}

	result, err := h.a2ui.Append(sessionID, req.Messages)
	if err != nil {
		writeJSON(w, h.a2uiErrorStatus(err), map[string]any{"error": err.Error()})
		return
	}

	// Build the broadcast event from the same canonical bytes we just stored
	// so a late subscriber's snapshot and live tail agree on shape.
	messagesJSON, marshalErr := json.Marshal(req.Messages)
	if marshalErr != nil {
		// Should not happen — we just decoded the same slice.
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": marshalErr.Error()})
		return
	}
	s.Broadcast(gateway.NewEvent("agent.a2ui", gateway.A2UIBatch{
		Version:  a2ui.ProtocolVersion,
		Seq:      result.Seq,
		Messages: messagesJSON,
	}))

	writeJSON(w, http.StatusAccepted, map[string]any{
		"seq":          result.Seq,
		"message_count": result.MessageCount,
	})
}

// a2uiErrorStatus picks the right HTTP status for broker validation errors.
// Validation problems are 400; retention overflow is 413 since the issue is
// payload size relative to the session's bounded buffer.
func (h *Handler) a2uiErrorStatus(err error) int {
	switch {
	case errors.Is(err, a2ui.ErrEmptyBatch),
		errors.Is(err, a2ui.ErrInvalidVersion),
		errors.Is(err, a2ui.ErrInvalidCatalogID),
		errors.Is(err, a2ui.ErrMessageMalformed):
		return http.StatusBadRequest
	case errors.Is(err, a2ui.ErrBatchTooLarge),
		errors.Is(err, a2ui.ErrRetentionExceeded):
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}

func (h *Handler) handleSessionStream(w http.ResponseWriter, r *http.Request, sessionID string, s *session.Session) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	events, unsubscribe := s.Subscribe(ctx)
	defer unsubscribe()

	// Replay the session's retained A2UI history so a late subscriber can
	// reconstruct the current surface. Subscribe before snapshot so a batch
	// arriving between the two is delivered live by the subscriber rather
	// than missed by both paths; the client keys by seq and is responsible
	// for skipping duplicates if both arrive.
	if snap, ok := h.a2ui.Snapshot(sessionID); ok {
		messagesJSON, err := json.Marshal(snap.Messages)
		if err == nil {
			replay := gateway.NewEvent("agent.a2ui.snapshot", gateway.A2UISnapshot{
				Version:  snap.Version,
				Seq:      snap.Seq,
				Messages: messagesJSON,
			})
			replay.SessionID = sessionID
			if err := conn.WriteJSON(replay); err != nil {
				return
			}
		}
	}

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			switch msg["type"] {
			case "abort":
				_ = s.Abort(ctx)
			case "ui_response":
				h.handleWSUIResponse(ctx, s, msg)
			case "a2ui.action":
				h.handleWSA2UIAction(ctx, conn, s, msg)
			}
		}
	}()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-readDone:
			return
		case <-ticker.C:
			_ = conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
		case event, ok := <-events:
			if !ok {
				return
			}
			event.SessionID = sessionID
			if err := conn.WriteJSON(event); err != nil {
				return
			}
		}
	}
}

// handleWSA2UIAction validates an inbound a2ui.action frame, then routes it
// through the process-layer seam. Errors land back on the WS as a synthetic
// agent.a2ui.error event broadcast through the session — that keeps all
// writes on the main loop's writer goroutine.
func (h *Handler) handleWSA2UIAction(ctx context.Context, _ *websocket.Conn, s *session.Session, msg map[string]any) {
	dataAny, ok := msg["data"].(map[string]any)
	if !ok {
		s.Broadcast(a2uiActionError("missing data field"))
		return
	}

	messageRaw, err := json.Marshal(dataAny["message"])
	if err != nil || len(messageRaw) == 0 || string(messageRaw) == "null" {
		s.Broadcast(a2uiActionError("missing message field"))
		return
	}
	if err := validateA2UIActionMessage(messageRaw); err != nil {
		s.Broadcast(a2uiActionError(err.Error()))
		return
	}

	req := process.A2UIActionRequest{Message: messageRaw}
	if v, ok := dataAny["client_data_model"]; ok && v != nil {
		if b, err := json.Marshal(v); err == nil {
			req.ClientDataModel = b
		}
	}
	if v, ok := dataAny["client_capabilities"]; ok && v != nil {
		if b, err := json.Marshal(v); err == nil {
			req.ClientCapabilities = b
		}
	}

	if err := s.SendA2UIAction(ctx, req); err != nil {
		s.Broadcast(a2uiActionError(err.Error()))
	}
}

// validateA2UIActionMessage enforces the minimum invariants a runtime can
// rely on: well-formed JSON object, version v0.9, and an action sub-object
// with a name and surfaceId.
func validateA2UIActionMessage(raw json.RawMessage) error {
	var probe struct {
		Version string `json:"version"`
		Action  *struct {
			Name      string `json:"name"`
			SurfaceID string `json:"surfaceId"`
		} `json:"action"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return errors.New("a2ui.action: message is not a JSON object")
	}
	if probe.Version != a2ui.ProtocolVersion {
		return errors.New("a2ui.action: message.version must be " + a2ui.ProtocolVersion)
	}
	if probe.Action == nil {
		return errors.New("a2ui.action: message.action is required")
	}
	if strings.TrimSpace(probe.Action.Name) == "" {
		return errors.New("a2ui.action: message.action.name is required")
	}
	if strings.TrimSpace(probe.Action.SurfaceID) == "" {
		return errors.New("a2ui.action: message.action.surfaceId is required")
	}
	return nil
}

func a2uiActionError(reason string) gateway.Event {
	return gateway.NewEvent("agent.a2ui.error", map[string]any{
		"error": reason,
	})
}

func (h *Handler) handleWSUIResponse(ctx context.Context, s *session.Session, msg map[string]any) {
	id, _ := msg["id"].(string)
	if id == "" {
		return
	}

	resp := process.UIResponse{ID: id}

	if cancelled, ok := msg["cancelled"].(bool); ok && cancelled {
		resp.Cancelled = true
	} else if confirmed, ok := msg["confirmed"].(bool); ok {
		c := confirmed
		resp.Confirmed = &c
	} else if value, exists := msg["value"]; exists {
		resp.Value = value
	}

	_ = s.SendUIResponse(ctx, resp)
}

func (h *Handler) writeSessionErr(w http.ResponseWriter, err error) {
	if errors.Is(err, session.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "session not found"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) requireScope(w http.ResponseWriter, r *http.Request, scope string) bool {
	identity := auth.FromContext(r.Context())
	if !auth.HasScope(identity, scope) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "insufficient scope"})
		return false
	}
	return true
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
}
