# Configuration

All configuration is done via environment variables. The defaults are intended to work out of the box for local development.

| Variable | Purpose | Default |
|---|---|---|
| `ZOEA_LISTEN_ADDR` | Listen address | `:7777` |
| `PI_BIN_PATH` | Path to `pi` binary | `pi` |
| `PI_DEFAULT_ARGS` | Default args for `pi` subprocess | `--mode rpc` |
| `ZOEA_PI_SESSION_DIR` | Base directory for Pi session state/history | `./.zoea-sessions` |
| `ZOEA_WORKING_DIR` | Default working directory for all Pi subprocesses | empty |
| `ZOEA_OUTPUT_DIR` | Where the server reads tool artifacts from. Relative paths resolve from each session's working dir. | `.zoea/output` |
| `AUTH_API_KEYS` | API keys for auth (enables auth) | empty |
| `ZOEA_BEHIND_PROXY` | Treat all connections as remote | empty |
| `STORE_DRIVER` | Storage backend | `sqlite` |
| `ZOEA_STORE_DSN` | Database path / connection string | `./.zoea.db` |

If `ZOEA_WORKING_DIR` is set, every Pi subprocess starts there. Pi session state/history still lives under `ZOEA_PI_SESSION_DIR`. Per-request `working_dir` values are ignored while `ZOEA_WORKING_DIR` is set.

## Storage

Session metadata and message history are persisted to a SQLite database by default. The database file is created automatically on first run.

```bash
# Custom database location
ZOEA_STORE_DSN=/var/lib/zoea/sessions.db go run ./cmd/server
```

## Example

```bash
ZOEA_LISTEN_ADDR=:9090 \
AUTH_API_KEYS="myapp:sk_secret:admin" \
go run ./cmd/server
```

See [Authentication](authentication.md) for API key format and auth behavior, and [Storage](storage.md) for persistence details.

## Artifact endpoints

Tools produced via `zoea-core` write artifacts under `<output_dir>/<run_id>/artifacts/`. The server exposes them at:

- `GET /v1/sessions/{session_id}/artifacts/{run_id}` → parsed `results.jsonl` for the run
- `GET /v1/sessions/{session_id}/artifacts/{run_id}/{name...}` → raw artifact bytes (uses recorded `media_type`, falls back to `application/octet-stream`)

Artifacts persist across server restarts as long as the session's working dir + `ZOEA_OUTPUT_DIR` resolution is stable.
