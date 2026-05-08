// Package introspect runs a one-shot Pi process at server boot to capture
// the set of slash commands and tools registered for the server's working
// directory. The result is cached in memory and served via GET /v1/config.
//
// Why a throwaway session: pi.getCommands() and pi.getAllTools() only run
// inside the Pi process, and the data is server-instance-scoped (same
// working dir for every session under our v1 assumption). Running once at
// boot avoids per-user-session round-trips and transcript pollution.
package introspect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/unbracketed/zoea-server/internal/gateway"
	"github.com/unbracketed/zoea-server/internal/process"
)

const (
	introspectUserID    = "__zoea_introspect__"
	introspectSessionID = "s_introspect"
	introspectCommand   = "/zoea-introspect"
	introspectCustomTag = "zoea-introspect"
)

// Config is the cached snapshot served to clients. Commands and Tools are
// passed through verbatim from Pi (pi.getCommands() / pi.getAllTools()) so
// the client controls how to group/filter by source/scope.
type Config struct {
	Commands []json.RawMessage `json:"commands"`
	Tools    []json.RawMessage `json:"tools"`
	// CapturedAt is the wall-clock time the snapshot was taken; useful for
	// "when did the server last discover this?" UI hints.
	CapturedAt time.Time `json:"captured_at"`
}

// Run spawns a single Pi process via mgr, sends /zoea-introspect, waits for
// the run to end, harvests the structured custom message, and returns the
// parsed Config. The Pi process is closed before returning.
func Run(ctx context.Context, mgr process.Manager, workingDir string) (*Config, error) {
	handle, err := mgr.Start(ctx, process.StartOptions{
		UserID:     introspectUserID,
		SessionID:  introspectSessionID,
		WorkingDir: workingDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start introspect agent: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = handle.Close(closeCtx)
	}()

	events, unsubscribe := handle.Subscribe(ctx)
	defer unsubscribe()

	// Wait for the zoea-introspect custom message itself, not agent.run.end.
	// /zoea-introspect emits a custom message and returns without invoking
	// the model, so no run is ever started and agent.run.end never fires —
	// waiting on it would always hit the deadline. We unblock as soon as the
	// custom message ends, with agent.run.end kept as a fallback in case the
	// command's behavior changes.
	done := make(chan struct{})
	go func() {
		closeOnce := func() {
			select {
			case <-done:
			default:
				close(done)
			}
		}
		for ev := range events {
			if ev.Type == "agent.run.end" {
				closeOnce()
				return
			}
			if ev.Type != "agent.message.end" {
				continue
			}
			me, ok := ev.Data.(gateway.MessageEnd)
			if !ok || len(me.Message) == 0 {
				continue
			}
			var probe struct {
				Role       string `json:"role"`
				CustomType string `json:"customType"`
			}
			if err := json.Unmarshal(me.Message, &probe); err != nil {
				continue
			}
			if probe.Role == "custom" && probe.CustomType == introspectCustomTag {
				closeOnce()
				return
			}
		}
	}()

	if err := handle.Prompt(ctx, process.PromptRequest{Message: introspectCommand}); err != nil {
		return nil, fmt.Errorf("send introspect prompt: %w", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	raw, err := handle.GetMessagesRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("read introspect messages: %w", err)
	}

	cfg, err := parseIntrospectMessage(raw)
	if err != nil {
		return nil, err
	}
	cfg.CapturedAt = time.Now().UTC()
	return cfg, nil
}

// parseIntrospectMessage walks raw transcript messages looking for the
// most recent custom message tagged "zoea-introspect" and decodes its
// details payload.
func parseIntrospectMessage(messages []json.RawMessage) (*Config, error) {
	type customMsg struct {
		Role       string `json:"role"`
		CustomType string `json:"customType"`
		Details    struct {
			Version  int               `json:"version"`
			Commands []json.RawMessage `json:"commands"`
			Tools    []json.RawMessage `json:"tools"`
		} `json:"details"`
	}

	for i := len(messages) - 1; i >= 0; i-- {
		var m customMsg
		if err := json.Unmarshal(messages[i], &m); err != nil {
			continue
		}
		if m.Role != "custom" || m.CustomType != introspectCustomTag {
			continue
		}
		commands := m.Details.Commands
		if commands == nil {
			commands = []json.RawMessage{}
		}
		tools := m.Details.Tools
		if tools == nil {
			tools = []json.RawMessage{}
		}
		return &Config{Commands: commands, Tools: tools}, nil
	}
	return nil, errors.New("introspect: no zoea-introspect custom message found in transcript")
}
