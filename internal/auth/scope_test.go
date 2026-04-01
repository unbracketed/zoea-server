package auth

import "testing"

func TestHasScopeExact(t *testing.T) {
	id := AuthIdentity{Scopes: []string{"sessions.read"}}
	if !HasScope(id, "sessions.read") {
		t.Error("expected true for exact scope match")
	}
}

func TestHasScopeMissing(t *testing.T) {
	id := AuthIdentity{Scopes: []string{"sessions.read"}}
	if HasScope(id, "sessions.write") {
		t.Error("expected false for missing scope")
	}
}

func TestHasScopeAdmin(t *testing.T) {
	id := AuthIdentity{Scopes: []string{"admin"}}
	if !HasScope(id, "sessions.read") {
		t.Error("expected admin to cover sessions.read")
	}
	if !HasScope(id, "sessions.write") {
		t.Error("expected admin to cover sessions.write")
	}
}

func TestHasScopeEmpty(t *testing.T) {
	id := AuthIdentity{Scopes: nil}
	if HasScope(id, "sessions.read") {
		t.Error("expected false for empty scopes")
	}
}
