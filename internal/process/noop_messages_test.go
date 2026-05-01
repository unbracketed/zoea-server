package process

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNoopGetMessagesRaw(t *testing.T) {
	m := NewNoopProcessManager()
	h, err := m.Start(context.Background(), StartOptions{SessionID: "s1", UserID: "u1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer h.Close(context.Background())

	// Empty initially.
	raw, err := h.GetMessagesRaw(context.Background())
	if err != nil {
		t.Fatalf("raw empty: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("expected 0, got %d", len(raw))
	}

	if err := h.Prompt(context.Background(), PromptRequest{Message: "hi"}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	raw, err = h.GetMessagesRaw(context.Background())
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(raw))
	}

	// Each entry must decode as a JSON object with role + content array.
	for i, r := range raw {
		var msg struct {
			Role    string            `json:"role"`
			Content []json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(r, &msg); err != nil {
			t.Fatalf("msg %d not valid JSON: %v", i, err)
		}
		if msg.Role == "" {
			t.Fatalf("msg %d: empty role", i)
		}
		if len(msg.Content) == 0 {
			t.Fatalf("msg %d: empty content array", i)
		}
	}

	// Flat GetMessages still works.
	flat, err := h.GetMessages(context.Background())
	if err != nil {
		t.Fatalf("flat: %v", err)
	}
	if len(flat) != 2 {
		t.Fatalf("expected 2 flat messages, got %d", len(flat))
	}
	if flat[0].Role != "user" || flat[0].Content != "hi" {
		t.Fatalf("unexpected user msg: %+v", flat[0])
	}
}
