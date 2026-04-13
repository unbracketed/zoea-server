# Configuration

All configuration is done via environment variables. The defaults are intended to work out of the box for local development.

| Variable | Purpose | Default |
|---|---|---|
| `GATEWAY_LISTEN_ADDR` | Listen address | `:8080` |
| `PI_BIN_PATH` | Path to `pi` binary | `pi` |
| `PI_DEFAULT_ARGS` | Default args for `pi` subprocess | `--mode rpc --no-session` |
| `SESSIONS_BASE_DIR` | Working directory for sessions | `./.gateway-sessions` |
| `AUTH_API_KEYS` | API keys for auth (enables auth) | empty |
| `GATEWAY_BEHIND_PROXY` | Treat all connections as remote | empty |
| `STORE_DRIVER` | Storage backend | `sqlite` |
| `STORE_DSN` | Database path / connection string | `./.gateway.db` |

## Storage

Session metadata and message history are persisted to a SQLite database by default. The database file is created automatically on first run.

```bash
# Custom database location
STORE_DSN=/var/lib/gateway/sessions.db go run ./cmd/gateway
```

## Example

```bash
GATEWAY_LISTEN_ADDR=:9090 \
AUTH_API_KEYS="myapp:sk_secret:admin" \
go run ./cmd/gateway
```

See [Authentication](authentication.md) for API key format and auth behavior, and [Storage](storage.md) for persistence details.
