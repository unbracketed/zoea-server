package auth

import (
	"context"
	"testing"
)

func TestParseAPIKeys(t *testing.T) {
	raw := "telegram-bridge:sk_abc123:sessions.read,sessions.write;monitoring:sk_def456:sessions.read"
	keys := ParseAPIKeys(raw)
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0].Name != "telegram-bridge" || keys[0].Key != "sk_abc123" {
		t.Errorf("unexpected first key: %+v", keys[0])
	}
	if len(keys[0].Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(keys[0].Scopes))
	}
	if keys[1].Name != "monitoring" || keys[1].Key != "sk_def456" {
		t.Errorf("unexpected second key: %+v", keys[1])
	}
}

func TestParseAPIKeysEmpty(t *testing.T) {
	keys := ParseAPIKeys("")
	if keys != nil {
		t.Errorf("expected nil for empty input, got %v", keys)
	}
}

func TestParseAPIKeysSingle(t *testing.T) {
	keys := ParseAPIKeys("admin:sk_admin:admin")
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].Scopes[0] != "admin" {
		t.Errorf("expected admin scope, got %s", keys[0].Scopes[0])
	}
}

func TestContextRoundTrip(t *testing.T) {
	id := AuthIdentity{Method: "api-key", Subject: "test", Scopes: []string{"admin"}}
	ctx := WithIdentity(context.Background(), id)
	got := FromContext(ctx)
	if got.Method != "api-key" || got.Subject != "test" {
		t.Errorf("context round-trip failed: %+v", got)
	}
}

func TestFromContextEmpty(t *testing.T) {
	got := FromContext(context.Background())
	if got.Method != "" {
		t.Errorf("expected zero value, got %+v", got)
	}
}
