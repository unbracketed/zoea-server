package auth

import (
	"encoding/json"
	"net/http"
)

// Middleware returns an HTTP middleware that enforces auth on every request.
// On success it stores the AuthIdentity in the request context.
// On failure it returns 401 Unauthorized.
func Middleware(cfg *AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, err := CheckAuth(cfg, r)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
				return
			}
			ctx := WithIdentity(r.Context(), identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireScope returns an HTTP middleware that checks the identity in context
// has the required scope. Returns 403 Forbidden if not.
func RequireScope(required string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity := FromContext(r.Context())
			if !HasScope(identity, required) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "insufficient scope"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
