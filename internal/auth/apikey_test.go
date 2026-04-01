package auth

import "testing"

var testKeys = []APIKey{
	{Name: "bridge", Key: "sk_abc123", Scopes: []string{"sessions.read", "sessions.write"}},
	{Name: "monitor", Key: "sk_def456", Scopes: []string{"sessions.read"}},
}

func TestExactMatch(t *testing.T) {
	key, ok := ValidateAPIKey(testKeys, "sk_abc123")
	if !ok {
		t.Fatal("expected match")
	}
	if key.Name != "bridge" {
		t.Errorf("expected name bridge, got %s", key.Name)
	}
}

func TestWrongKey(t *testing.T) {
	_, ok := ValidateAPIKey(testKeys, "sk_wrong")
	if ok {
		t.Error("expected no match for wrong key")
	}
}

func TestEmptyBearer(t *testing.T) {
	_, ok := ValidateAPIKey(testKeys, "")
	if ok {
		t.Error("expected no match for empty bearer")
	}
}

func TestSecondKey(t *testing.T) {
	key, ok := ValidateAPIKey(testKeys, "sk_def456")
	if !ok {
		t.Fatal("expected match for second key")
	}
	if key.Name != "monitor" {
		t.Errorf("expected name monitor, got %s", key.Name)
	}
}
