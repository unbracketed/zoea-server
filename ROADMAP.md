# Go Agent Gateway Roadmap

Current state: working Pi RPC bridge with REST API + WebSocket streaming. Normalized event mapping (Phase 1) and extension UI bridging (Phase 2) are complete.

---

## ✅ Phase 1: Normalized Event Mapping (DONE)

Raw Pi RPC events are mapped into a stable gateway event schema. Clients receive typed events (`agent.text.delta`, `agent.tool.start`, etc.) without coupling to Pi internals.

### Event schema

```json
{
  "type": "agent.text.delta",
  "session_id": "s_000001",
  "timestamp": "2026-04-01T12:00:00Z",
  "data": { "delta": "Hello" }
}
```

### Mapped event types

| Pi RPC event | Gateway event | Notes |
|---|---|---|
| `message_update` / `text_delta` | `agent.text.delta` | `{ delta }` |
| `message_update` / `text_start` | `agent.text.start` | |
| `message_update` / `text_end` | `agent.text.end` | `{ content }` |
| `message_update` / `thinking_delta` | `agent.thinking.delta` | `{ delta }` |
| `message_update` / `thinking_start` | `agent.thinking.start` | |
| `message_update` / `thinking_end` | `agent.thinking.end` | |
| `message_update` / `toolcall_start` | `agent.toolcall.start` | `{ tool_name }` |
| `message_update` / `toolcall_delta` | `agent.toolcall.delta` | `{ delta }` |
| `message_update` / `toolcall_end` | `agent.toolcall.end` | `{ tool_call }` |
| `message_update` / `done` | `agent.message.done` | `{ reason }` |
| `message_update` / `error` | `agent.message.error` | `{ reason }` |
| `agent_start` | `agent.run.start` | |
| `agent_end` | `agent.run.end` | `{ messages }` |
| `turn_start` | `agent.turn.start` | |
| `turn_end` | `agent.turn.end` | |
| `tool_execution_start` | `agent.tool.start` | `{ tool_name, args }` |
| `tool_execution_update` | `agent.tool.update` | `{ partial_result }` |
| `tool_execution_end` | `agent.tool.end` | `{ result, is_error }` |
| `compaction_start` | `agent.compaction.start` | `{ reason }` |
| `compaction_end` | `agent.compaction.end` | `{ summary, aborted }` |
| `auto_retry_start` | `agent.retry.start` | `{ attempt, delay_ms }` |
| `auto_retry_end` | `agent.retry.end` | `{ success, attempt }` |
| `queue_update` | `agent.queue.update` | `{ steering, follow_up }` |
| `extension_ui_request` | `agent.ui.request` | See Phase 2 |
| `extension_error` | `agent.extension.error` | `{ error }` |

---

## ✅ Phase 2: Extension UI Bridging (DONE)

Bidirectional extension UI support over WebSocket. Pi extensions can request user interaction (confirm, select, input, editor) and receive responses from the client.

**Server → Client:**
```json
{
  "type": "agent.ui.request",
  "session_id": "s_000001",
  "data": { "id": "uuid-1", "method": "confirm", "payload": ... }
}
```

**Client → Server:**
```json
{ "type": "ui_response", "id": "uuid-1", "confirmed": true }
```

Fire-and-forget methods (notify, setStatus, setWidget, setTitle, set_editor_text) are forwarded as events with no response expected.

---

## Phase 3: Auth Middleware

### JWT/OIDC

- Middleware on all `/v1/*` routes
- Validate JWT from `Authorization: Bearer <token>`
- Extract `user_id` from claims
- Enforce session ownership: `session.user_id == token.user_id`

### API key fallback

- `X-API-Key` header for server-to-server use (bridges use this)
- Configurable via `AUTH_API_KEYS` env var

### Config

- `AUTH_JWKS_URL` — JWKS endpoint for JWT validation
- `AUTH_ISSUER` — expected issuer claim
- `AUTH_AUDIENCE` — expected audience claim
- `AUTH_API_KEYS` — comma-separated static keys (dev/internal/bridge use)

---

## Phase 4: Persistent Storage

### Purpose

Gateway metadata, session index, and optional message cache. Pi session files remain source of truth for agent context.

### Schema (Postgres or SQLite)

```sql
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    project_id TEXT,
    external_id TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    pi_pid INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE session_messages (
    id SERIAL PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    role TEXT NOT NULL,
    content TEXT,
    model TEXT,
    usage_json JSONB,
    timestamp TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX idx_sessions_external ON sessions(external_id) WHERE external_id IS NOT NULL;
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_messages_session ON session_messages(session_id);
```

### Implementation

- New `internal/store/` package with interface + Postgres/SQLite implementations
- Session manager delegates to store for create/get/list/delete
- On `agent_end`, persist messages to `session_messages`
- `GET /v1/sessions` list endpoint (paginated, filtered by user)
- `GET /v1/sessions?external_id=telegram:12345` lookup by external ID (for bridges)

---

## Phase 5: Additional RPC Commands

Expose remaining Pi RPC commands as REST endpoints:

| Endpoint | RPC command |
|---|---|
| `POST /v1/sessions/{id}/steer` | `steer` |
| `POST /v1/sessions/{id}/follow-up` | `follow_up` |
| `POST /v1/sessions/{id}/model` | `set_model` |
| `POST /v1/sessions/{id}/thinking` | `set_thinking_level` |
| `GET /v1/sessions/{id}/models` | `get_available_models` |
| `GET /v1/sessions/{id}/stats` | `get_session_stats` |
| `POST /v1/sessions/{id}/compact` | `compact` |
| `POST /v1/sessions/{id}/fork` | `fork` |
| `POST /v1/sessions/{id}/new` | `new_session` |
| `POST /v1/sessions/{id}/bash` | `bash` |
| `GET /v1/sessions/{id}/commands` | `get_commands` |

---

## Phase 6: Channel Bridge Infrastructure

### Architecture

Bridges are **separate processes** that talk to the gateway via its REST + WS API. They are not embedded in the gateway.

```
Telegram Bridge ──┐
Discord Bridge  ──┼──→ Gateway REST + WS API ──→ pi --mode rpc
Email Bridge    ──┘         │
                         Storage
```

Each bridge is a stateless protocol adapter:
1. Receives a message from the platform
2. Maps platform user → gateway session (find-or-create by `external_id`)
3. `POST /v1/sessions/{id}/messages` to send the prompt
4. Subscribes to `WS /v1/sessions/{id}/stream` for response events
5. Collects `agent.text.delta` events, assembles the complete response
6. Sends the response back via the platform API

### Why separate processes

- Different dependency trees (Telegram SDK, Discord SDK, SMTP libs) stay out of the gateway binary
- Different lifecycles (long-poll, webhook, persistent WS, polling)
- Independent restart/deploy — crash one bridge without affecting others
- Each bridge is small (~500-1000 lines)
- Can be written in a different language if the SDK is better there

### Gateway API additions for bridges

- `external_id` field on session creation: `POST /v1/sessions {"user_id":"brian","external_id":"telegram:12345"}`
- Session lookup by external ID: `GET /v1/sessions?external_id=telegram:12345`
- These are added in Phase 4 (storage schema already includes `external_id`)

### Shared bridge toolkit (optional)

A small Go package `bridgekit/` with reusable helpers:
- **Response assembler** — collect `agent.text.delta` events into a complete message string
- **Chunker** — split long responses for platforms with message length limits (Telegram: 4096 chars, Discord: 2000 chars)
- **Gateway client** — typed HTTP + WS client for the gateway API
- **Session mapper** — find-or-create session by external ID pattern

Bridges import `bridgekit` but are otherwise standalone binaries.

---

## Phase 6a: Telegram Bridge

Separate project: `telegram-bridge/`

### Structure

```
telegram-bridge/
├── main.go             # startup, config, polling loop
├── bot.go              # Telegram Bot API (long-poll or webhook)
├── gateway.go          # gateway REST + WS client
├── responder.go        # collect deltas, send assembled response
└── config.go           # env-based config
```

### Behavior

| Telegram event | Gateway action |
|---|---|
| `/start` | Create session (`external_id=telegram:{chat_id}`) |
| `/reset` | Delete session, create new one |
| `/model <name>` | Send as slash command via prompt |
| Text message | Find session by `external_id`, send prompt, stream response |
| Photo/document | Not supported v1 (reply with "text only for now") |

### Response assembly

The bridge connects to `WS /v1/sessions/{id}/stream` and:
1. On `agent.run.start` — send "typing" action to Telegram
2. On `agent.text.delta` — accumulate text
3. On `agent.tool.start` — optionally send status message ("Using bash...")
4. On `agent.run.end` — send assembled text as Telegram message
5. If response > 4096 chars — chunk into multiple messages

### Long responses and tool use

Agent runs can be long (minutes with many tool calls). The bridge should:
- Send periodic "typing" indicators (every 5s while agent is running)
- Optionally send intermediate status updates for tool calls
- Send the final assembled response when `agent.run.end` arrives

### Config

- `TELEGRAM_BOT_TOKEN` — from BotFather
- `GATEWAY_URL` — e.g. `http://localhost:9090`
- `GATEWAY_API_KEY` — for auth (Phase 3)
- `TELEGRAM_ALLOWED_USERS` — optional allowlist of Telegram user IDs

### Estimated effort

2-3 days for a working Telegram bridge with text message support.

---

## Phase 6b: Future Bridges

Each follows the same pattern — separate binary, gateway API client, platform SDK:

| Bridge | Platform API | Key considerations |
|---|---|---|
| **Discord** | Discord.js or discordgo | Slash commands, threads, 2000 char limit, embeds for rich content |
| **Slack** | Slack Events API + Web API | App manifest, thread replies, blocks for structured content |
| **Email** | IMAP poll + SMTP send | Subject → session name, body → prompt, reply threading via Message-ID |
| **WhatsApp** | WhatsApp Business API or Baileys | Media support, delivery receipts, 24h messaging window |
| **Matrix** | Matrix client-server API | E2EE support, room-based sessions |

None of these require gateway changes beyond what Phase 6 provides — they all use the same REST + WS API.

---

## Phase 7: Hardening

- **Rate limiting** — per-user and per-session request limits
- **Process health checks** — periodic liveness probe, auto-restart policy
- **Backpressure** — bounded WS send buffers, drop policy for slow clients
- **Graceful shutdown** — drain active sessions on SIGTERM
- **Structured logging** — JSON logs with session_id, user_id, latency
- **Metrics** — Prometheus endpoint (`/metrics`) with counters/histograms per spec
- **Tracing** — OpenTelemetry spans on API requests and RPC commands

---

## Phase 8: Deployment

- Dockerfile for gateway (multi-stage build, non-root, includes pi binary)
- Dockerfile per bridge (lightweight, just the bridge binary)
- docker-compose with gateway + telegram-bridge for local dev
- Health/readiness probes (`/healthz`, `/readyz`)
- Persistent volume for Pi session files
- CI: build + test + lint on push

---

## Sequencing

| Phase | Status | Depends on | Effort |
|---|---|---|---|
| 1. Event mapping | ✅ Done | — | — |
| 2. Extension UI | ✅ Done | Phase 1 | — |
| 3. Auth | TODO | — | 1-2 days |
| 4. Persistence | TODO | — | 2-3 days |
| 5. Additional commands | TODO | — | 1-2 days |
| 6. Bridge infra | TODO | Phase 4 | 1-2 days |
| 6a. Telegram bridge | TODO | Phase 6 | 2-3 days |
| 6b. Future bridges | TODO | Phase 6 | 2-3 days each |
| 7. Hardening | TODO | Phases 1-6 | 3-5 days |
| 8. Deployment | TODO | Phase 7 | 1-2 days |

Phases 3, 4, and 5 can run in parallel. Phase 6 needs Phase 4 for `external_id` session lookup. Phase 6a (Telegram) can start as soon as Phase 6 lands. Phase 7 is the integration/polish pass. Phase 8 is packaging.

### Fast path to Telegram

If you want to get a Telegram bot working quickly:

1. Phase 4 (persistence + external_id) — 2-3 days
2. Phase 6 (bridge infra) — 1-2 days
3. Phase 6a (Telegram bridge) — 2-3 days

That's ~6-8 days to a working Telegram bot backed by a Pi coding agent. Auth (Phase 3) can be added after with an API key for the bridge.
