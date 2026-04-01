# Quickstart

Get the gateway running in under a minute.

## Prerequisites

- Go 1.24+
- [pi](https://github.com/mariozechner/pi-coding-agent) installed and on your `PATH`

## Run locally

```bash
go run ./cmd/gateway
```

That's it. No config needed for local development. The gateway starts on `:8080` and grants full access to connections from `127.0.0.1` / `::1`.

```
gateway listening on :8080 (auth: disabled, local-only access)
```

## Create a session

```bash
curl -s http://localhost:8080/v1/sessions \
  -H "Content-Type: application/json" \
  -d '{"user_id": "me"}' | jq
```

```json
{
  "session_id": "s_000001",
  "status": "ready"
}
```

## Send a message

```bash
curl -s http://localhost:8080/v1/sessions/s_000001/messages \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello, what can you do?"}' | jq
```

```json
{
  "accepted": true
}
```

## Stream events via WebSocket

```bash
npx wscat -c ws://localhost:8080/v1/sessions/s_000001/stream
```

Events arrive as JSON frames. You can send `{"type": "abort"}` to cancel an in-progress response.

## Check session state

```bash
curl -s http://localhost:8080/v1/sessions/s_000001/state | jq
```

## Get message history

```bash
curl -s http://localhost:8080/v1/sessions/s_000001/messages | jq
```

## Health check

```bash
curl -s http://localhost:8080/healthz | jq
```

```json
{
  "ok": true
}
```

## Configuration

All config is via environment variables. Defaults work for local dev.

| Variable | Purpose | Default |
|---|---|---|
| `GATEWAY_LISTEN_ADDR` | Listen address | `:8080` |
| `PI_BIN_PATH` | Path to `pi` binary | `pi` |
| `PI_DEFAULT_ARGS` | Default args for pi subprocess | `--mode rpc --no-session` |
| `SESSIONS_BASE_DIR` | Working directory for sessions | `./.gateway-sessions` |
| `AUTH_API_KEYS` | API keys for auth (enables auth) | empty |
| `GATEWAY_BEHIND_PROXY` | Treat all connections as remote | empty |

See [Authentication](authentication.md) for auth configuration details.

## Run with auth enabled

```bash
AUTH_API_KEYS="myapp:sk_secret123:admin" go run ./cmd/gateway
```

```
gateway listening on :8080 (auth: api-key, 1 keys configured)
```

Now all non-health endpoints require a bearer token:

```bash
curl -s http://localhost:8080/v1/sessions \
  -H "Authorization: Bearer sk_secret123" \
  -H "Content-Type: application/json" \
  -d '{"user_id": "me"}' | jq
```

## Next steps

- [API Endpoints](endpoints.md) — full endpoint reference
- [Authentication](authentication.md) — auth modes, scopes, and configuration
