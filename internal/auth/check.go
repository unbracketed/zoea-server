package auth

import (
	"errors"
	"net/http"
	"strings"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

// publicPaths that never require authentication.
var publicPaths = map[string]bool{
	"/healthz": true,
	"/readyz":  true,
}

// CheckAuth is the single auth decision function. All auth decisions flow through here.
// Implements the decision matrix:
//
//  1. Public path → allow (anonymous)
//  2. No credentials configured + local connection → allow (local-dev)
//  3. No credentials configured + remote connection → deny
//  4. Valid API key bearer → allow (api-key with scopes)
//  5. Valid JWT bearer → allow (jwt with claims) [not yet implemented]
//  6. None of the above → deny
func CheckAuth(cfg *AuthConfig, r *http.Request) (AuthIdentity, error) {
	// 1. Public paths always pass.
	if publicPaths[r.URL.Path] {
		return AuthIdentity{Method: "anonymous", Subject: "anonymous"}, nil
	}

	// 2 & 3. No credentials configured.
	if !cfg.IsEnabled() {
		if IsLocalConnection(r, cfg.BehindProxy) {
			return AuthIdentity{
				Method:  "local-dev",
				Subject: "local",
				Scopes:  []string{"admin"},
			}, nil
		}
		return AuthIdentity{}, ErrUnauthorized
	}

	// Extract bearer token from Authorization header or ?token= query param.
	bearer := extractBearer(r)

	if bearer == "" {
		return AuthIdentity{}, ErrUnauthorized
	}

	// 4. API key check.
	if len(cfg.APIKeys) > 0 {
		if key, ok := ValidateAPIKey(cfg.APIKeys, bearer); ok {
			return AuthIdentity{
				Method:  "api-key",
				Subject: key.Name,
				Scopes:  key.Scopes,
			}, nil
		}
	}

	// 5. JWT check (placeholder — not yet implemented).
	// if cfg.JWKSUrl != "" { ... }

	// 6. Nothing matched.
	return AuthIdentity{}, ErrUnauthorized
}

// extractBearer gets the bearer token from the Authorization header,
// falling back to the ?token= query parameter (for WebSocket clients).
func extractBearer(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}

	// Fallback: query parameter for WebSocket clients that can't set headers.
	if tok := r.URL.Query().Get("token"); tok != "" {
		return tok
	}

	return ""
}
