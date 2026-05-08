package introspect

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseIntrospectMessage_HappyPath(t *testing.T) {
	transcript := []json.RawMessage{
		mustJSON(t, map[string]any{"role": "user", "content": "/zoea-introspect"}),
		mustJSON(t, map[string]any{
			"role":       "custom",
			"customType": "zoea-introspect",
			"display":    false,
			"content":    "ok",
			"details": map[string]any{
				"version": 1,
				"commands": []map[string]any{
					{"name": "model", "source": "extension"},
					{"name": "my-skill", "source": "skill"},
				},
				"tools": []map[string]any{
					{"name": "bash", "description": "run shell"},
				},
			},
		}),
	}

	cfg, err := parseIntrospectMessage(transcript)
	if err != nil {
		t.Fatalf("parseIntrospectMessage: %v", err)
	}
	if got := len(cfg.Commands); got != 2 {
		t.Fatalf("commands len: got %d want 2", got)
	}
	if got := len(cfg.Tools); got != 1 {
		t.Fatalf("tools len: got %d want 1", got)
	}
	// Round-trip preserves the upstream Pi shape verbatim.
	if !strings.Contains(string(cfg.Commands[0]), `"name":"model"`) {
		t.Fatalf("commands[0] missing expected field: %s", cfg.Commands[0])
	}
}

func TestParseIntrospectMessage_PicksMostRecent(t *testing.T) {
	// Two zoea-introspect messages; the latter must win (handles the
	// hypothetical case of a re-run within the same throwaway session).
	older := mustJSON(t, map[string]any{
		"role":       "custom",
		"customType": "zoea-introspect",
		"details":    map[string]any{"commands": []map[string]any{{"name": "old"}}, "tools": []map[string]any{}},
	})
	newer := mustJSON(t, map[string]any{
		"role":       "custom",
		"customType": "zoea-introspect",
		"details":    map[string]any{"commands": []map[string]any{{"name": "new"}}, "tools": []map[string]any{}},
	})
	cfg, err := parseIntrospectMessage([]json.RawMessage{older, newer})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(string(cfg.Commands[0]), `"name":"new"`) {
		t.Fatalf("expected most-recent command, got: %s", cfg.Commands[0])
	}
}

func TestParseIntrospectMessage_IgnoresOtherCustomTypes(t *testing.T) {
	transcript := []json.RawMessage{
		mustJSON(t, map[string]any{
			"role":       "custom",
			"customType": "zoea-tools-status",
			"details":    map[string]any{"unrelated": true},
		}),
	}
	if _, err := parseIntrospectMessage(transcript); err == nil {
		t.Fatal("expected error when only unrelated custom messages present")
	}
}

func TestParseIntrospectMessage_EmptyTranscript(t *testing.T) {
	if _, err := parseIntrospectMessage(nil); err == nil {
		t.Fatal("expected error for empty transcript")
	}
}

func TestParseIntrospectMessage_NormalizesNilSlices(t *testing.T) {
	// Pi may emit details with no commands/tools fields at all (e.g. an
	// empty install). Result must be empty slices, not nil, so JSON
	// encoding produces [] rather than null for clients.
	transcript := []json.RawMessage{
		mustJSON(t, map[string]any{
			"role":       "custom",
			"customType": "zoea-introspect",
			"details":    map[string]any{"version": 1},
		}),
	}
	cfg, err := parseIntrospectMessage(transcript)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Commands == nil || cfg.Tools == nil {
		t.Fatalf("nil slices not normalized: commands=%v tools=%v", cfg.Commands, cfg.Tools)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
