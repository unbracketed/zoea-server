package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/unbracketed/zoea-server/internal/auth"
	"github.com/unbracketed/zoea-server/internal/introspect"
	"github.com/unbracketed/zoea-server/internal/process"
	"github.com/unbracketed/zoea-server/internal/session"
	"github.com/unbracketed/zoea-server/internal/store"
)

type Handler struct {
	sessions          *session.Manager
	defaultWorkingDir string
	config            *introspect.Config
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// TODO: tighten origin policy with auth middleware.
		return true
	},
}

func NewHandler(sm *session.Manager, defaultWorkingDir string, cfg *introspect.Config) *Handler {
	return &Handler{
		sessions:          sm,
		defaultWorkingDir: defaultWorkingDir,
		config:            cfg,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.handleHealth)
	mux.HandleFunc("/readyz", h.handleReady)
	mux.HandleFunc("/v1/server-info", h.handleServerInfo)
	mux.HandleFunc("/v1/config", h.handleConfig)
	mux.HandleFunc("/v1/sessions", h.handleSessions)
	mux.HandleFunc("/v1/sessions/", h.handleSessionByID)
	return mux
}

// handleConfig returns the boot-time snapshot of Pi's registered slash
// commands and tools. Available is false when introspection failed at
// startup; clients should degrade (no autocomplete, empty settings panel)
// rather than treat it as fatal.
func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if h.config == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"commands":  []any{},
			"tools":     []any{},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":   true,
		"captured_at": h.config.CapturedAt.Format(time.RFC3339Nano),
		"commands":    h.config.Commands,
		"tools":       h.config.Tools,
	})
}

// handleServerInfo exposes the server's effective default working-dir so
// clients (e.g. zoea-web-ui) can scope their session listings to "what
// this server is currently pointed at." Empty string means no default
// configured.
func (h *Handler) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ZOEA_WORKING_DIR": h.defaultWorkingDir,
	})
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
		WorkingDir: r.URL.Query().Get("working_dir"),
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
		WorkingDir   string `json:"working_dir,omitempty"`
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
			WorkingDir:   rec.WorkingDir,
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
	actionTail := ""
	if len(parts) > 2 {
		actionTail = strings.Join(parts[2:], "/")
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

	// Resume is the explicit handshake to spawn a Pi process for a stored
	// session that has no live handle (typically after a server restart).
	// Idempotent — re-resuming a live session is a no-op.
	if action == "resume" && r.Method == http.MethodPost {
		if !h.requireScope(w, r, "sessions.write") {
			return
		}
		s, err := h.sessions.Resume(r.Context(), sessionID)
		if err != nil {
			h.writeSessionErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"session_id": s.ID,
			"status":     "ready",
		})
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

	case action == "artifacts":
		h.handleArtifactRequest(w, r, s, actionTail)
		return

	default:
		writeMethodNotAllowed(w)
		return
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
