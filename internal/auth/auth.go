package auth

import (
	"context"
	"strings"
)

// AuthIdentity represents the authenticated identity of a request.
type AuthIdentity struct {
	Method  string   // "local-dev", "api-key", "jwt", "anonymous"
	Subject string   // key name, JWT sub claim, or "local"
	Scopes  []string // ["sessions.read", "sessions.write"] or ["admin"]
}

// APIKey represents a configured static API key.
type APIKey struct {
	Name   string
	Key    string
	Scopes []string
}

// AuthConfig holds all auth-related configuration.
type AuthConfig struct {
	APIKeys     []APIKey
	JWKSUrl     string
	JWTIssuer   string
	JWTAudience string
	BehindProxy bool
}

// IsEnabled returns true when any credentials are configured.
func (c *AuthConfig) IsEnabled() bool {
	return len(c.APIKeys) > 0 || c.JWKSUrl != ""
}

// ParseAPIKeys parses the AUTH_API_KEYS env var format:
// "name:key:scope1,scope2;name2:key2:scope"
func ParseAPIKeys(raw string) []APIKey {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var keys []APIKey
	entries := strings.Split(raw, ";")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 3)
		if len(parts) != 3 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		key := strings.TrimSpace(parts[1])
		scopeStr := strings.TrimSpace(parts[2])
		if name == "" || key == "" || scopeStr == "" {
			continue
		}
		var scopes []string
		for _, s := range strings.Split(scopeStr, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				scopes = append(scopes, s)
			}
		}
		keys = append(keys, APIKey{Name: name, Key: key, Scopes: scopes})
	}
	return keys
}

// contextKey is an unexported type for context keys in this package.
type contextKey struct{}

var authIdentityKey = contextKey{}

// WithIdentity stores an AuthIdentity in the context.
func WithIdentity(ctx context.Context, id AuthIdentity) context.Context {
	return context.WithValue(ctx, authIdentityKey, id)
}

// FromContext retrieves the AuthIdentity from the context.
// Returns a zero-value AuthIdentity if none is set.
func FromContext(ctx context.Context) AuthIdentity {
	id, _ := ctx.Value(authIdentityKey).(AuthIdentity)
	return id
}
