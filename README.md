# Go Agent Gateway

HTTP/WebSocket gateway that bridges clients to `pi --mode rpc` agent subprocesses. Create sessions, send prompts, stream events — all over a REST API.

## Quick start

```bash
go run ./cmd/gateway
```

No config needed for local dev. The gateway starts on `:8080` with full access from localhost.

```bash
# Create a session
curl -s localhost:8080/v1/sessions -H "Content-Type: application/json" \
  -d '{"user_id": "me"}' | jq

# Send a prompt
curl -s localhost:8080/v1/sessions/s_000001/messages -H "Content-Type: application/json" \
  -d '{"message": "Hello"}' | jq

# Stream events
npx wscat -c ws://localhost:8080/v1/sessions/s_000001/stream
```

## With auth

```bash
AUTH_API_KEYS="myapp:sk_secret:admin" go run ./cmd/gateway
```

Then pass `Authorization: Bearer sk_secret` on all requests.

## Endpoints

| Method | Path | Scope | Description |
|---|---|---|---|
| `GET` | `/healthz` | public | Health check |
| `GET` | `/readyz` | public | Readiness check |
| `POST` | `/v1/sessions` | `sessions.write` | Create session |
| `GET` | `/v1/sessions` | `sessions.read` | List sessions |
| `GET` | `/v1/sessions/{id}/state` | `sessions.read` | Get session state |
| `GET` | `/v1/sessions/{id}/messages` | `sessions.read` | Get message history |
| `POST` | `/v1/sessions/{id}/messages` | `sessions.write` | Send prompt |
| `POST` | `/v1/sessions/{id}/abort` | `sessions.write` | Abort operation |
| `GET` | `/v1/sessions/{id}/stream` | `sessions.read` | WebSocket event stream |
| `DELETE` | `/v1/sessions/{id}` | `sessions.write` | Delete session |

## Documentation

- [Quickstart](docs/quickstart.md) — get running in under a minute
- [API Endpoints](docs/endpoints.md) — full endpoint reference
- [Authentication](docs/authentication.md) — auth modes, scopes, API keys, proxy setup
- [Storage](docs/storage.md) — persistence, schema, external IDs, message history
- [Configuration](docs/configuration.md) — all environment variables

## Configuration

| Variable | Purpose | Default |
|---|---|---|
| `GATEWAY_LISTEN_ADDR` | Listen address | `:8080` |
| `PI_BIN_PATH` | Path to `pi` binary | `pi` |
| `PI_DEFAULT_ARGS` | Default args for pi subprocess | `--mode rpc --no-session` |
| `SESSIONS_BASE_DIR` | Working directory for sessions | `./.gateway-sessions` |
| `AUTH_API_KEYS` | API keys (enables auth) | empty |
| `GATEWAY_BEHIND_PROXY` | Treat all connections as remote | empty |
| `STORE_DRIVER` | Storage backend | `sqlite` |
| `STORE_DSN` | Database path / connection string | `./.gateway.db` |

## Tests

```bash
go test ./...
```