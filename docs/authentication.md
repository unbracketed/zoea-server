# Authentication

The server uses a single auth middleware that protects all routes. It's designed for zero-config local development with mandatory auth for remote and production deployments.

## Three-tier model

| Tier | Condition | Behavior |
|---|---|---|
| **Local dev** | No credentials configured + local connection | Full access, no auth needed |
| **Remote lockout** | No credentials configured + remote connection | `401 Unauthorized` on every request |
| **Full auth** | Any credentials configured | Auth always required |

This means `go run ./cmd/server` works immediately with no config. The moment you set `AUTH_API_KEYS`, auth is enforced on all non-public endpoints.

## Configuration

| Variable | Purpose | Default |
|---|---|---|
| `AUTH_API_KEYS` | Static API keys (see format below) | empty |
| `AUTH_JWKS_URL` | JWKS endpoint for JWT validation | empty (JWT disabled) |
| `AUTH_JWT_ISSUER` | Expected JWT `iss` claim | empty (not checked) |
| `AUTH_JWT_AUDIENCE` | Expected JWT `aud` claim | empty (not checked) |
| `ZOEA_BEHIND_PROXY` | Force all connections to be treated as remote | empty (false) |

Auth is **enabled** when `AUTH_API_KEYS` or `AUTH_JWKS_URL` is non-empty. When neither is set, only local connections are allowed.

## API keys

### Format

```
name:key:scope1,scope2
```

Multiple keys are separated by `;`:

```bash
# Single admin key
AUTH_API_KEYS="myapp:sk_secret123:admin"

# Multiple keys with different scopes
AUTH_API_KEYS="telegram-bot:sk_abc123:sessions.read,sessions.write;dashboard:sk_def456:sessions.read"
```

### Using API keys

Pass the key as a Bearer token in the `Authorization` header:

```bash
curl http://localhost:8080/v1/sessions \
  -H "Authorization: Bearer sk_abc123" \
  -H "Content-Type: application/json" \
  -d '{"user_id": "alice"}'
```

For WebSocket connections where headers can't be set (browsers), use the `?token=` query parameter:

```
ws://localhost:8080/v1/sessions/s_000001/stream?token=sk_abc123
```

### Key comparison

Keys are compared using `crypto/subtle.ConstantTimeCompare` to prevent timing attacks.

## Scopes

Each API key has one or more scopes that control which endpoints it can access.

| Scope | Permissions |
|---|---|
| `sessions.read` | Get state, get messages, list sessions, stream events |
| `sessions.write` | Create sessions, send prompts, abort, delete |
| `glimpse.render` | Submit blocking BASIL Glimpse render requests (BASIL → Zoea) |
| `glimpse.action` | Submit Glimpse action/cancel callbacks (browser → Zoea) |
| `admin` | Superset — grants all permissions |

### Endpoint scope requirements

| Endpoint | Required scope |
|---|---|
| `GET /healthz` | none (public) |
| `GET /readyz` | none (public) |
| `POST /v1/sessions` | `sessions.write` |
| `GET /v1/sessions/{id}/state` | `sessions.read` |
| `GET /v1/sessions/{id}/messages` | `sessions.read` |
| `POST /v1/sessions/{id}/messages` | `sessions.write` |
| `POST /v1/sessions/{id}/abort` | `sessions.write` |
| `GET /v1/sessions/{id}/stream` | `sessions.read` |
| `DELETE /v1/sessions/{id}` | `sessions.write` |
| `POST /api/glimpse/v1/render` | `glimpse.render` |
| `POST /api/glimpse/v1/action` | `glimpse.action` |
| `POST /api/glimpse/v1/cancel` | `glimpse.action` |

A request with valid credentials but insufficient scope receives `403 Forbidden`:

```json
{"error": "insufficient scope"}
```

## Local connection detection

A connection is considered local only when **all** of these are true:

1. `ZOEA_BEHIND_PROXY` is **not** set
2. No proxy headers are present (`X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP`, `Forwarded`)
3. The TCP remote address is loopback (`127.0.0.1` or `::1`)

If **any** check fails, the connection is treated as remote.

This prevents a local-only server from being accidentally exposed through a reverse proxy.

## Behind a proxy

When deploying behind nginx, Caddy, Cloudflare, or any reverse proxy:

```bash
ZOEA_BEHIND_PROXY=1 AUTH_API_KEYS="myapp:sk_secret:admin" go run ./cmd/server
```

Setting `ZOEA_BEHIND_PROXY` ensures:
- All connections are treated as remote (no local-dev bypass)
- Rate limiting uses forwarded client IP (`X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP`) instead of the proxy's IP

## Rate limiting

Requests that fail authentication are rate limited to **30 per 60 seconds per IP**.

Authenticated requests are not rate limited.

When rate limited, the response includes a `Retry-After` header:

```
HTTP/1.1 429 Too Many Requests
Retry-After: 45

{"error": "too many requests", "retry_after": 45}
```

## Auth identity

Every authenticated request carries an identity in the request context, available to handlers:

```go
identity := auth.FromContext(r.Context())
// identity.Method  — "local-dev", "api-key", "jwt", "anonymous"
// identity.Subject — key name, JWT sub, "local", "anonymous"
// identity.Scopes  — ["sessions.read", "sessions.write"] or ["admin"]
```

## Decision flow

The auth middleware evaluates these rules in order — first match wins:

| # | Condition | Result |
|---|---|---|
| 1 | Public path (`/healthz`, `/readyz`) | Allow (anonymous) |
| 2 | No credentials configured + local connection | Allow (local-dev, admin scope) |
| 3 | No credentials configured + remote connection | Deny (401) |
| 4 | Valid API key bearer token | Allow (api-key, key's scopes) |
| 5 | Valid JWT token (when JWKS configured) | Allow (jwt, token claims) |
| 6 | None of the above | Deny (401) |

## JWT support

JWT validation via JWKS is stubbed but not yet implemented. When `AUTH_JWKS_URL` is set, the server will fetch signing keys and validate JWT bearer tokens. This will be completed in a future release.

## Examples

### Local development (no config)

```bash
go run ./cmd/server
# → Full access from localhost, remote connections rejected
```

### Single admin key

```bash
AUTH_API_KEYS="bot:sk_mykey:admin" go run ./cmd/server
# → All endpoints require Bearer sk_mykey
```

### Scoped keys for different services

```bash
AUTH_API_KEYS="telegram:sk_tg_key:sessions.read,sessions.write;grafana:sk_graf:sessions.read" \
  go run ./cmd/server
# → telegram key can read and write
# → grafana key can only read
```

### Production behind nginx

```bash
ZOEA_BEHIND_PROXY=1 \
AUTH_API_KEYS="frontend:sk_prod_key:admin" \
ZOEA_LISTEN_ADDR=:9090 \
  go run ./cmd/server
```
