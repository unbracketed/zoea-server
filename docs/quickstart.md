# Quickstart

Get the server running in under a minute.

## Prerequisites

- Go 1.24+
- [pi](https://github.com/mariozechner/pi-coding-agent) installed and on your `PATH`

## Run locally

```bash
go run ./cmd/server
```

That's it. No config needed for local development. The server starts on `:7777` and grants full access to connections from `127.0.0.1` / `::1`.

```
zoea-server listening on :7777 (auth: disabled, local-only access)
```

## Create a session

```bash
curl -s http://localhost:7777/v1/sessions \
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
curl -s http://localhost:7777/v1/sessions/s_000001/messages \
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
npx wscat -c ws://localhost:7777/v1/sessions/s_000001/stream
```

Events arrive as JSON frames. You can send `{"type": "abort"}` to cancel an in-progress response.

## Check session state

```bash
curl -s http://localhost:7777/v1/sessions/s_000001/state | jq
```

## Get message history

```bash
curl -s http://localhost:7777/v1/sessions/s_000001/messages | jq
```

## Health check

```bash
curl -s http://localhost:7777/healthz | jq
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
| `ZOEA_LISTEN_ADDR` | Listen address | `:7777` |
| `PI_BIN_PATH` | Path to `pi` binary | `pi` |
| `PI_DEFAULT_ARGS` | Default args for pi subprocess | `--mode rpc` |
| `ZOEA_PI_SESSION_DIR` | Base directory for Pi session state/history | `./.zoea-sessions` |
| `ZOEA_WORKING_DIR` | Default working directory for all Pi subprocesses | empty |
| `AUTH_API_KEYS` | API keys for auth (enables auth) | empty |
| `ZOEA_BEHIND_PROXY` | Treat all connections as remote | empty |
| `STORE_DRIVER` | Storage backend | `sqlite` |
| `ZOEA_STORE_DSN` | Database path / connection string | `./.zoea.db` |

See [Authentication](authentication.md) for auth configuration details and [Storage](storage.md) for persistence details.

## Run with auth enabled

```bash
AUTH_API_KEYS="myapp:sk_secret123:admin" go run ./cmd/server
```

```
zoea-server listening on :7777 (auth: api-key, 1 keys configured)
```

Now all non-health endpoints require a bearer token:

```bash
curl -s http://localhost:7777/v1/sessions \
  -H "Authorization: Bearer sk_secret123" \
  -H "Content-Type: application/json" \
  -d '{"user_id": "me"}' | jq
```

## Next steps

- [API Endpoints](endpoints.md) — full endpoint reference
- [Authentication](authentication.md) — auth modes, scopes, and configuration
