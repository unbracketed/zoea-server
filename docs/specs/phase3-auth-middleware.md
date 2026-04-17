# Phase 3: Auth Middleware — Implementation Plan

## Goal

Single auth gate middleware that protects all server routes. Zero-config for local dev, API key auth for bridges and services, optional JWT for external identity providers, proxy-aware.

Inspired by Moltis's single `check_auth()` design — one function, one place, every request.

---

## Design

### Three-Tier Model

| Tier | Condition | Behavior |
|---|---|---|
| **1 — Local dev** | No credentials configured + local connection | Full access, no auth |
| **2 — Remote lockout** | No credentials configured + remote/proxied | 401 Unauthorized |
| **3 — Full auth** | Any credentials configured | Auth always required |

This means `go run ./cmd/server` works immediately with no config. The moment you set `AUTH_API_KEYS` or `AUTH_JWKS_URL`, or deploy behind a proxy, auth is enforced.

### Decision Matrix

Single `CheckAuth()` function, evaluated in order:

| # | Condition | Result | Identity |
|---|---|---|---|
| 1 | Public path (`/healthz`, `/readyz`) | **Allow** | anonymous |
| 2 | No credentials configured + local connection | **Allow** | local-dev |
| 3 | No credentials configured + remote connection | **Deny** | — |
| 4 | Valid `Authorization: Bearer <api_key>` matching a configured key | **Allow** | api-key (with scopes) |
| 5 | Valid `Authorization: Bearer <jwt>` with valid JWKS signature | **Allow** | jwt (claims) |
| 6 | None of the above | **Deny** | — |

### Local Connection Detection

A connection is local only when **all** checks pass (borrowed from Moltis):

1. `ZOEA_BEHIND_PROXY` is **not** set
2. No proxy headers present (`X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP`, `Forwarded`)
3. The TCP remote address is loopback (`127.0.0.1`, `::1`)

If **any** check fails → remote.

### API Key Scopes

| Scope | Permissions |
|---|---|
| `sessions.read` | Get state, get messages, list sessions, get models, get stats |
| `sessions.write` | Create sessions, send prompts, steer, follow-up, abort, delete, set model |
| `admin` | Superset of all scopes |

Scope enforcement is per-endpoint. Endpoints declare their required scope; the middleware checks the key's scopes against it.

### Auth Identity

Every authenticated request carries an `AuthIdentity` in context:

```go
type AuthIdentity struct {
    Method string   // "local-dev", "api-key", "jwt"
    Subject string  // key name, JWT sub claim, or "local"
    Scopes  []string // ["sessions.read", "sessions.write"] or ["admin"]
}
```

Handlers can read identity from context for logging and session ownership checks.

---

## Configuration

| Env var | Purpose | Default |
|---|---|---|
| `AUTH_API_KEYS` | Static API keys. Format: `name:key:scope1,scope2;name2:key2:scope` | empty (no keys) |
| `AUTH_JWKS_URL` | JWKS endpoint URL for JWT validation | empty (JWT disabled) |
| `AUTH_JWT_ISSUER` | Expected JWT `iss` claim | empty (not checked) |
| `AUTH_JWT_AUDIENCE` | Expected JWT `aud` claim | empty (not checked) |
| `ZOEA_BEHIND_PROXY` | Force all connections to be treated as remote | empty (false) |

### API key format

```bash
# Single key with all access
AUTH_API_KEYS="telegram-bridge:sk_abc123:admin"

# Multiple keys with scoped access
AUTH_API_KEYS="telegram-bridge:sk_abc123:sessions.read,sessions.write;monitoring:sk_def456:sessions.read"
```

### Credentials configured = auth enabled

Auth is considered enabled when `AUTH_API_KEYS` is non-empty OR `AUTH_JWKS_URL` is non-empty. When neither is set, only local connections are allowed.

---

## Public Paths

No auth required, always pass through:

| Path | Purpose |
|---|---|
| `GET /healthz` | Health check |
| `GET /readyz` | Readiness check |

---

## Rate Limiting

Applied only to unauthenticated requests (pre-auth failures). Authenticated requests are not rate limited in v1.

| Scope | Limit |
|---|---|
| Any unauthenticated request | 30 per 60s per IP |

When behind proxy, rate limit keys on forwarded client IP (`X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP`), falling back to TCP remote address.

Returns `429 Too Many Requests` with `Retry-After` header.

---

## WebSocket Auth

WebSocket upgrade requests pass through `CheckAuth()` like any HTTP request. The `Authorization` header works for the upgrade handshake.

For clients that can't set HTTP headers on WebSocket upgrades (browsers), support token as query parameter:

```
ws://localhost:9090/v1/sessions/s_000001/stream?token=sk_abc123
```

The middleware checks `?token=` as a fallback when `Authorization` header is absent.

---

## Files to Create

### 1. `internal/auth/auth.go` — Core auth types and config

```go
type AuthIdentity struct {
    Method  string
    Subject string
    Scopes  []string
}

type APIKey struct {
    Name   string
    Key    string
    Scopes []string
}

type AuthConfig struct {
    APIKeys      []APIKey
    JWKSUrl      string
    JWTIssuer    string
    JWTAudience  string
    BehindProxy  bool
}

func (c *AuthConfig) IsEnabled() bool
```

### 2. `internal/auth/check.go` — The single auth decision function

```go
// CheckAuth is the only function that decides whether a request is authenticated.
// All auth decisions flow through here.
func CheckAuth(cfg *AuthConfig, r *http.Request) (AuthIdentity, error)
```

Implements the decision matrix: public paths → local dev → api key → jwt → deny.

### 3. `internal/auth/local.go` — Local connection detection

```go
func IsLocalConnection(r *http.Request, behindProxy bool) bool
```

Three checks: no proxy env, no proxy headers, loopback remote addr.

### 4. `internal/auth/apikey.go` — API key validation

```go
func ValidateAPIKey(keys []APIKey, bearer string) (APIKey, bool)
```

Constant-time comparison using `crypto/subtle.ConstantTimeCompare`.

### 5. `internal/auth/jwt.go` — JWT validation (optional)

```go
func ValidateJWT(jwksURL, issuer, audience, bearer string) (AuthIdentity, error)
```

Only active when `AUTH_JWKS_URL` is configured. Uses a JWKS client to fetch and cache keys.

### 6. `internal/auth/middleware.go` — HTTP middleware

```go
func Middleware(cfg *AuthConfig) func(http.Handler) http.Handler
```

Wraps any handler. On success, sets `AuthIdentity` in request context. On failure, returns 401.

### 7. `internal/auth/scope.go` — Scope checking

```go
func HasScope(identity AuthIdentity, required string) bool
```

Returns true if identity has the required scope or `admin`.

### 8. `internal/auth/ratelimit.go` — Pre-auth rate limiter

```go
func RateLimitMiddleware(behindProxy bool) func(http.Handler) http.Handler
```

Per-IP sliding window. Only applied to unauthenticated requests.

---

## Files to Modify

### 9. `internal/config/config.go`

Add auth config fields:

```go
type Config struct {
    ListenAddr      string
    PiBinPath       string
    PiArgs          []string
    SessionsBaseDir string
    Auth            auth.AuthConfig
}
```

Parse `AUTH_API_KEYS`, `AUTH_JWKS_URL`, `AUTH_JWT_ISSUER`, `AUTH_JWT_AUDIENCE`, `ZOEA_BEHIND_PROXY` from env.

### 10. `cmd/server/main.go`

Wire the middleware:

```go
h := api.NewHandler(sm)
handler := auth.Middleware(&cfg.Auth)(h.Routes())
srv := &http.Server{Addr: cfg.ListenAddr, Handler: handler}
```

Log auth mode on startup:
```
zoea-server listening on :9090 (auth: api-key, 2 keys configured)
zoea-server listening on :9090 (auth: disabled, local-only access)
```

### 11. `internal/api/handler.go`

Add scope checks on endpoints:

```go
case action == "messages" && r.Method == http.MethodPost:
    if !auth.HasScope(auth.FromContext(r.Context()), "sessions.write") {
        writeJSON(w, http.StatusForbidden, map[string]any{"error": "insufficient scope"})
        return
    }
    // ...
```

Endpoint scope map:

| Endpoint | Required scope |
|---|---|
| `POST /v1/sessions` | `sessions.write` |
| `GET /v1/sessions/{id}/state` | `sessions.read` |
| `GET /v1/sessions/{id}/messages` | `sessions.read` |
| `POST /v1/sessions/{id}/messages` | `sessions.write` |
| `POST /v1/sessions/{id}/abort` | `sessions.write` |
| `GET /v1/sessions/{id}/stream` | `sessions.read` |
| `DELETE /v1/sessions/{id}` | `sessions.write` |

---

## Testing

### Unit tests: `internal/auth/check_test.go`

| Test | Scenario | Expected |
|---|---|---|
| `TestPublicPathAllowed` | `GET /healthz`, no credentials | Allow, anonymous |
| `TestLocalDevNoCredentials` | Loopback IP, no keys configured | Allow, local-dev |
| `TestRemoteNoCredentials` | Non-loopback IP, no keys configured | Deny |
| `TestBehindProxyNoCredentials` | Loopback IP, `BEHIND_PROXY=true`, no keys | Deny |
| `TestProxyHeadersForceRemote` | Loopback IP, `X-Forwarded-For` present, no keys | Deny |
| `TestValidAPIKey` | Valid bearer key with `sessions.write` | Allow, scopes match |
| `TestInvalidAPIKey` | Wrong bearer token | Deny |
| `TestAPIKeyScopeInsufficient` | Valid key with `sessions.read`, endpoint needs `sessions.write` | Allow auth, deny scope |
| `TestAdminScopeCoversAll` | Key with `admin` scope, any endpoint | Allow |
| `TestNoAuthHeader` | No Authorization header, credentials configured | Deny |
| `TestWSTokenParam` | `?token=sk_abc123` on WS upgrade, no header | Allow |

### Unit tests: `internal/auth/local_test.go`

| Test | Scenario | Expected |
|---|---|---|
| `TestLoopbackIPv4` | RemoteAddr `127.0.0.1:12345` | Local |
| `TestLoopbackIPv6` | RemoteAddr `[::1]:12345` | Local |
| `TestNonLoopback` | RemoteAddr `192.168.1.5:12345` | Remote |
| `TestXForwardedFor` | Loopback + `X-Forwarded-For` header | Remote |
| `TestXRealIP` | Loopback + `X-Real-IP` header | Remote |
| `TestCFConnectingIP` | Loopback + `CF-Connecting-IP` header | Remote |
| `TestForwardedHeader` | Loopback + `Forwarded` header | Remote |
| `TestBehindProxyFlag` | Loopback + `behindProxy=true` | Remote |

### Unit tests: `internal/auth/apikey_test.go`

| Test | Scenario | Expected |
|---|---|---|
| `TestExactMatch` | Correct key | Match |
| `TestWrongKey` | Incorrect key | No match |
| `TestEmptyBearer` | Empty string | No match |
| `TestConstantTime` | Verify uses `subtle.ConstantTimeCompare` | (code inspection) |

### Integration test

1. Start server with `AUTH_API_KEYS=test:sk_test123:admin`
2. `POST /v1/sessions` without auth → 401
3. `POST /v1/sessions` with `Authorization: Bearer sk_test123` → 201
4. `GET /healthz` without auth → 200 (public path)
5. WS upgrade with `?token=sk_test123` → success

---

## Implementation Order

1. **`internal/auth/auth.go`** — types (`AuthIdentity`, `APIKey`, `AuthConfig`)
2. **`internal/auth/local.go`** — `IsLocalConnection()` + tests
3. **`internal/auth/apikey.go`** — `ValidateAPIKey()` + tests
4. **`internal/auth/scope.go`** — `HasScope()` + tests
5. **`internal/auth/check.go`** — `CheckAuth()` + tests (the decision matrix)
6. **`internal/auth/middleware.go`** — HTTP middleware + context helpers
7. **`internal/auth/ratelimit.go`** — pre-auth rate limiter
8. **`internal/config/config.go`** — parse auth env vars
9. **`cmd/server/main.go`** — wire middleware
10. **`internal/api/handler.go`** — add scope checks to endpoints
11. **`internal/auth/jwt.go`** — JWT validation (can be deferred if not needed immediately)
12. **Build + test** — `go build ./...` + `go test ./...`

Steps 1-7 are additive (new package, no existing code changes). Steps 8-10 are wiring. Step 11 (JWT) is optional and can ship later.

---

## Dependencies

- `crypto/subtle` (stdlib) — constant-time key comparison
- No external dependencies for API key auth
- JWT validation (step 11) will need a JWKS library — `github.com/MicahParks/keyfunc/v3` or similar. Defer until needed.

---

## Estimated Effort

- API key + local dev + middleware + scope checks: **2-3 days**
- JWT support (optional, can defer): **1 day additional**
- Rate limiter: **0.5 day**

Total: **2-4 days** depending on whether JWT ships in this phase or later.
