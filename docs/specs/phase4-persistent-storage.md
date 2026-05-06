# Phase 4: Persistent Storage — Implementation Plan

## Goal

Add durable storage for session metadata and message history so the server can:

1. List and query sessions without depending on in-memory state
2. Resolve sessions by `external_id` (required for bridges)
3. Persist message history on run completion

Pi session files remain the source of truth for agent context/runtime. Storage is a server index/cache layer.

---

## Current State

- Sessions exist only in memory (`internal/session/manager.go`)
- Session IDs are process-local (`s_000001`, `s_000002`, ...)
- No persistent session index
- No session list endpoint (`GET /v1/sessions` unsupported)
- No `external_id` support
- Message history is fetched live from Pi only (`get_messages`)

---

## Scope for Phase 4

### In scope

- New `internal/store` package with storage interface + SQLite implementation
- Persistent `sessions` + `session_messages` tables
- Wire session manager to persist create/get/list/delete metadata
- Add `GET /v1/sessions` endpoint with filtering/pagination
- Add `external_id` on session creation and lookup by `external_id`
- Persist messages on `agent.run.end`

### Out of scope

- Session process resurrection after server restart
- Full Postgres implementation (leave interface-ready; SQLite first)
- Data retention/TTL policies

---

## Storage Design

## Backend choice (v1)

Use SQLite first for fast local/prod portability and zero external dependency.

- Driver: `modernc.org/sqlite` (pure Go, no CGO)
- DB file path via env var (default: `./.zoea.db`)

Keep the store interface backend-agnostic so Postgres can be added later.

### Schema

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
    timestamp TEXT NOT NULL,
    FOREIGN KEY(session_id) REFERENCES sessions(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_external
ON sessions(external_id)
WHERE external_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_session ON session_messages(session_id);
```

Notes:
- SQLite uses `TEXT` for timestamp/json fields.
- `external_id` uniqueness is enforced when present.

---

## API Changes

### `POST /v1/sessions`

Add optional request field:

```json
{
  "user_id": "alice",
  "project_id": "my-project",
  "external_id": "telegram:12345"
}
```

Validation:
- `user_id` required
- `external_id` optional but must be unique when set

### `GET /v1/sessions`

Add new list endpoint.

Query params:
- `user_id` (optional)
- `external_id` (optional, exact match)
- `limit` (optional, default 50, max 200)
- `offset` (optional, default 0)

Response shape:

```json
{
  "sessions": [
    {
      "session_id": "s_000001",
      "user_id": "alice",
      "project_id": "my-project",
      "external_id": "telegram:12345",
      "status": "active",
      "created_at": "2026-04-01T12:00:00Z",
      "last_active_at": "2026-04-01T12:01:00Z"
    }
  ]
}
```

Scope: `sessions.read`

---

## Internal Architecture Changes

### 1) New package: `internal/store/`

Files:
- `store.go` — interfaces + DTOs
- `sqlite.go` — SQLite implementation
- `schema.sql` — bootstrap schema (embedded)

Core interface:

```go
type Store interface {
    Init(ctx context.Context) error

    CreateSession(ctx context.Context, s SessionRecord) error
    GetSession(ctx context.Context, id string) (SessionRecord, error)
    ListSessions(ctx context.Context, q ListSessionsQuery) ([]SessionRecord, error)
    DeleteSession(ctx context.Context, id string) error
    UpdateSessionActivity(ctx context.Context, id string, t time.Time) error

    InsertMessages(ctx context.Context, msgs []MessageRecord) error
    GetMaxSessionID(ctx context.Context) (string, error)
}
```

### 2) `internal/config/config.go`

Add store config:
- `STORE_DRIVER` (default `sqlite`)
- `ZOEA_STORE_DSN` (default `./.zoea.db`)

### 3) `cmd/server/main.go`

- Construct store from config
- Initialize schema on startup
- Inject store into session manager

### 4) `internal/session/manager.go`

Refactor manager to use both:
- runtime map (`session_id -> process handle`) for active subprocess control
- persistent store for metadata and list queries

Add/modify methods:
- `Create(ctx, userID, projectID, externalID)`
- `List(ctx, query)`
- `GetByExternalID(ctx, externalID)` (or query via `List`)
- update `LastActive` in store whenever session methods are called

Counter initialization:
- On startup, parse max existing session ID from store and seed `counter` to avoid ID collisions.

### 5) Message persistence hook

On each created session, start a background listener over `Subscribe()`:
- watch for `agent.run.end`
- parse `RunEnd.Messages` payload
- flatten message content to text
- insert rows into `session_messages`

If `RunEnd.Messages` is empty/unparseable, fallback to `GetMessages()` and persist snapshot rows.

### 6) `internal/api/handler.go`

- `handleSessions` supports both `POST` and `GET`
- `POST` accepts `external_id`
- `GET` returns persisted sessions from store-backed manager

---

## Error Handling

- Duplicate `external_id` → `409 Conflict` (`{"error":"external_id already exists"}`)
- Invalid pagination params → `400 Bad Request`
- Store unavailable on startup → fail fast (server does not start)
- Store write failures during run-end persistence → log and continue (do not kill session)

---

## Testing Plan

### Unit tests

1. `internal/store/sqlite_test.go`
   - schema init
   - create/get/list/delete session
   - `external_id` uniqueness
   - message insert/query helpers

2. `internal/session/manager_test.go`
   - create persists metadata + runtime handle
   - list from store
   - delete removes runtime + store row
   - counter resumes from max persisted session id

3. `internal/api/handler_test.go`
   - `GET /v1/sessions` auth + pagination
   - `GET /v1/sessions?external_id=...`
   - `POST /v1/sessions` with `external_id`
   - duplicate external_id returns 409

### Integration test

- Start server with SQLite file
- Create session with external_id
- Send prompt via noop/real process
- Assert:
  - list endpoint returns session
  - lookup by external_id works
  - run-end writes to `session_messages`

---

## Implementation Order

1. Add `internal/store` interface + SQLite backend + schema bootstrap
2. Extend config for store driver/DSN
3. Wire store initialization in `cmd/server/main.go`
4. Refactor `session.Manager` to persist metadata + seed counter from store
5. Extend `POST /v1/sessions` for `external_id`
6. Add `GET /v1/sessions` with query filters/pagination
7. Add run-end message persistence worker
8. Add/update tests
9. Update docs (`docs/endpoints.md`, `docs/quickstart.md`, `docs/configuration.md`)

---

## Acceptance Criteria

- Session metadata survives server restart
- `GET /v1/sessions` returns persisted sessions with filters
- `external_id` can be used to find session deterministically
- Message rows are written when an agent run ends
- Existing endpoints continue working unchanged for active sessions

---

## Estimated Effort

- Core store + wiring: 1.5–2 days
- API + tests + docs: 1–1.5 days

Total: **~3 days** for production-ready Phase 4 v1.
