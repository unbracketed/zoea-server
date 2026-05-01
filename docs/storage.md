# Storage

The server persists session metadata and message history to a local database. This allows sessions to survive process restarts (metadata), enables listing and querying sessions, and supports bridge workflows via `external_id` lookup.

Pi session files remain the source of truth for agent runtime context. The server store is an index and cache layer — it does not replace Pi's own session state.

## Configuration

| Variable | Purpose | Default |
|---|---|---|
| `STORE_DRIVER` | Storage backend | `sqlite` |
| `STORE_DSN` | Database path / connection string | `./.zoea.db` |

The database file and schema are created automatically on first startup. No manual setup required.

```bash
# Default — database in current directory
go run ./cmd/server

# Custom location
STORE_DSN=/var/lib/zoea/data.db go run ./cmd/server

# In-memory (testing only, data lost on restart)
STORE_DSN=":memory:" go run ./cmd/server
```

## What is stored

### Sessions table

Every call to `POST /v1/sessions` persists a row:

| Column | Type | Description |
|---|---|---|
| `id` | TEXT (PK) | Session ID (`s_000001`, `s_000002`, ...) |
| `user_id` | TEXT | User who created the session |
| `project_id` | TEXT | Optional project context |
| `external_id` | TEXT | Optional unique external identifier (e.g. `telegram:12345`) |
| `status` | TEXT | Session status (`active`) |
| `pi_pid` | INTEGER | Pi subprocess PID (when running) |
| `created_at` | TEXT | ISO 8601 creation timestamp |
| `last_active_at` | TEXT | ISO 8601 last activity timestamp |

### Session messages table

Message history is captured at the end of each agent run:

| Column | Type | Description |
|---|---|---|
| `id` | INTEGER (PK) | Auto-increment row ID |
| `session_id` | TEXT (FK) | References `sessions.id` |
| `role` | TEXT | Message role (`user`, `assistant`) |
| `content` | TEXT | Flattened plain-text preview (best-effort, for display/search/debug) |
| `model` | TEXT | Model used (when available) |
| `usage_json` | TEXT | Token usage JSON (when available) |
| `raw_json` | TEXT | Full Pi message JSON — source of truth for rich retrieval |
| `timestamp` | TEXT | ISO 8601 timestamp |

`raw_json` stores the unflattened Pi message object so that future rich retrievals (e.g. for a web UI) can reconstruct the original transcript structure — thinking blocks, tool calls, tool results, usage, stop reason, provider and model metadata. `content` remains a simple text preview for legacy clients and quick display.

## Schema

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    project_id TEXT,
    external_id TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    pi_pid INTEGER,
    created_at TEXT NOT NULL,
    last_active_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS session_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT,
    model TEXT,
    usage_json TEXT,
    raw_json TEXT,
    timestamp TEXT NOT NULL,
    FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_external
ON sessions(external_id)
WHERE external_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_session ON session_messages(session_id);
```

## External IDs

The `external_id` field enables bridge integrations (Telegram, Discord, etc.) to map platform-specific identifiers to server sessions.

- Set on creation: `POST /v1/sessions {"user_id":"brian", "external_id":"telegram:12345"}`
- Look up by external ID: `GET /v1/sessions?external_id=telegram:12345`
- Enforced unique — creating a second session with the same `external_id` returns `409 Conflict`
- Optional — sessions without an `external_id` are allowed and can coexist freely

Convention for bridge external IDs: `platform:platform_id` (e.g. `telegram:98765`, `discord:guild:channel:user`).

## Message persistence

Messages are persisted automatically when an agent run completes:

1. A background listener watches for `agent.run.end` events on each session
2. On run end, the server calls `get_messages` on the Pi subprocess to get the full raw history
3. For each raw message, the server extracts a flattened preview (`content`), `role`, `model`, `usage_json`, and `timestamp`, and stores the full unflattened JSON in `raw_json`
4. The entire message history is written to `session_messages`, replacing any previous snapshot
5. If `get_messages` fails, the server falls back to parsing raw messages from the `agent.run.end` event payload

This means `session_messages` always reflects the latest complete conversation state. Messages are replaced (not appended) because Pi manages its own context window, including compaction.

### Schema migrations

When the server starts, it runs additive migrations to bring older databases up to the latest schema. Currently:

- `ALTER TABLE session_messages ADD COLUMN raw_json TEXT` — adds the raw transcript column to pre-existing DBs

Migrations are idempotent and tolerate columns that already exist.

## Session ID continuity

On startup, the server reads the highest existing session ID from the store and seeds its counter. This prevents ID collisions after a restart:

```
# Before restart: last session was s_000042
# After restart: next session will be s_000043
```

## Cascade deletes

Deleting a session (`DELETE /v1/sessions/{id}`) removes both the session row and all associated message rows via foreign key cascade.

## Listing sessions

```bash
# All sessions
curl localhost:8080/v1/sessions

# Filter by user
curl "localhost:8080/v1/sessions?user_id=alice"

# Find by external ID
curl "localhost:8080/v1/sessions?external_id=telegram:12345"

# Paginate
curl "localhost:8080/v1/sessions?limit=10&offset=20"
```

See [API Endpoints](endpoints.md) for full request/response details.

## Limitations

- **No process resurrection** — persisted session metadata survives a restart, but the Pi subprocess does not. Active sessions from a previous server process will exist in the store but won't have a running agent. A future phase may add session reattachment.
- **SQLite only** — the `Store` interface is backend-agnostic, but only SQLite is implemented. Postgres support can be added by implementing the same interface.
- **No data retention policies** — old sessions and messages are kept indefinitely. Manual cleanup or a future TTL feature is needed for long-running deployments.
- **Single-writer** — SQLite supports one writer at a time. This is fine for single-process deployments but won't scale to multiple server instances. Use Postgres for multi-instance setups (when available).
