# API Endpoints

All endpoints are under the `/v1` prefix unless noted. Request and response bodies are JSON.

## Public endpoints

These never require authentication.

### `GET /healthz`

Health check.

**Response** `200`
```json
{"ok": true}
```

### `GET /readyz`

Readiness check.

**Response** `200`
```json
{"ok": true}
```

### `GET /v1/server-info`

Returns the server's configured defaults so clients can scope their behavior to "what this server is currently pointed at." Currently exposes the effective default working-dir; the value is empty when no `DEFAULT_WORKING_DIR` is set.

**Response** `200`
```json
{
  "default_working_dir": "/tmp/brown"
}
```

---

## Session endpoints

All session endpoints require authentication when auth is enabled. See [Authentication](authentication.md) for details.

### `POST /v1/sessions`

Create a new agent session.

**Scope:** `sessions.write`

**Request body:**
```json
{
  "user_id": "alice",
  "project_id": "my-project",
  "working_dir": "/Users/alice/src/my-project",
  "external_id": "telegram:12345"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `user_id` | string | yes | Identifies the user |
| `project_id` | string | no | Optional project context |
| `working_dir` | string | no | Optional Pi subprocess working directory; Pi session state/history still lives under `SESSIONS_BASE_DIR`. Ignored if server `DEFAULT_WORKING_DIR` is set. |
| `external_id` | string | no | Unique external identifier (e.g. for bridge lookup) |

**Response** `201`
```json
{
  "session_id": "s_000001",
  "status": "ready"
}
```

If `external_id` is already taken: `409 Conflict`
```json
{"error": "external_id already exists"}
```

---

### `GET /v1/sessions`

List sessions with optional filters.

**Scope:** `sessions.read`

**Query parameters:**

| Param | Type | Description |
|---|---|---|
| `user_id` | string | Filter by user |
| `external_id` | string | Exact match on external ID |
| `working_dir` | string | Exact match on the resolved working directory the session was created with. Useful for scoping the listing to the server's current `DEFAULT_WORKING_DIR` (see `GET /v1/server-info`). |
| `limit` | int | Max results (default 50, max 200) |
| `offset` | int | Pagination offset (default 0) |

**Response** `200`
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

---

### `GET /v1/sessions/{id}/state`

Get the current state of a session.

**Scope:** `sessions.read`

**Response** `200`
```json
{
  "state": { ... }
}
```

---

### `GET /v1/sessions/{id}/messages`

Get the message history of a session.

**Scope:** `sessions.read`

**Query parameters:**

| Param | Type | Description |
|---|---|---|
| `format` | string | `text` (default) returns flattened plain-text content; `raw` returns full Pi message JSON for rich UI rendering |

**Response** `200` (`format=text`, default)
```json
{
  "format": "text",
  "messages": [
    {"role": "user", "content": "Hello"},
    {"role": "assistant", "content": "Hi there"}
  ]
}
```

**Response** `200` (`format=raw`)

Returns the decoded Pi `get_messages` payload with no flattening. Each entry is the full Pi message object — including `content` blocks (`text`, `thinking`, tool calls, tool results), `usage`, `stopReason`, `timestamp`, `provider`, `model`, etc. — exactly as Pi produced it.

```json
{
  "format": "raw",
  "messages": [
    {
      "role": "user",
      "content": [{"type": "text", "text": "Hello"}],
      "timestamp": 1777598491267
    },
    {
      "role": "assistant",
      "content": [
        {"type": "thinking", "thinking": "..."},
        {"type": "text", "text": "Hi there"}
      ],
      "usage": {"input": 12, "output": 9, "cost": {"total": 0.001}},
      "stopReason": "stop",
      "timestamp": 1777598491274,
      "provider": "openai-codex",
      "model": "gpt-5.4"
    }
  ]
}
```

**Errors:**

| Status | Condition |
|---|---|
| `400` | Unknown `format` value |

---

### `POST /v1/sessions/{id}/messages`

Send a prompt to the agent.

**Scope:** `sessions.write`

**Request body:**
```json
{
  "message": "Explain this code",
  "streaming_behavior": ""
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `message` | string | yes | The prompt text |
| `streaming_behavior` | string | no | Optional streaming hint |

**Response** `202`
```json
{
  "accepted": true
}
```

The response is immediate — the agent processes the prompt asynchronously. Connect to the [stream endpoint](#get-v1sessionsidstream) to receive events.

---

### `POST /v1/sessions/{id}/abort`

Abort the agent's current operation.

**Scope:** `sessions.write`

**Response** `200`
```json
{
  "status": "aborted"
}
```

---

### `POST /v1/sessions/{id}/resume`

Resume a stored session by spawning a fresh Pi process bound to its original session-dir, so the on-disk transcript is reloaded. Use this after a server restart, or any time a client wants to open a session that has no live Pi process. Idempotent: if a live handle already exists, returns success without spawning a second process.

**Scope:** `sessions.write`

**Response** `200`
```json
{
  "session_id": "s_000001",
  "status": "ready"
}
```

**Errors**

| Status | Reason |
|--------|--------|
| `404` | No session with that ID in the store |
| `500` | Pi process failed to start (missing binary, working dir gone, etc.) |

---

### `DELETE /v1/sessions/{id}`

Delete a session and terminate its agent process.

**Scope:** `sessions.write`

**Response** `200`
```json
{
  "status": "deleted"
}
```

---

### `GET /v1/sessions/{id}/stream`

WebSocket endpoint for real-time agent events.

**Scope:** `sessions.read`

**Protocol:** WebSocket upgrade over HTTP.

**Authentication:** Via `Authorization` header on the upgrade request, or `?token=` query parameter for clients that can't set headers (e.g. browsers):

```
ws://localhost:8080/v1/sessions/s_000001/stream?token=sk_secret123
```

#### Server → client events

Events are JSON frames streamed from the agent. Each event includes a `session_id` field.

#### Client → server messages

| Message | Description |
|---|---|
| `{"type": "abort"}` | Abort the current agent operation |
| `{"type": "ui_response", "id": "...", ...}` | Respond to a UI prompt from the agent |
| `{"type": "a2ui.action", "data": {...}}` | Forward an A2UI v0.9 client action — see [A2UI session broker](#a2ui-session-broker) below |

**UI response fields:**

| Field | Type | Description |
|---|---|---|
| `id` | string | Required. The prompt ID to respond to |
| `cancelled` | bool | Cancel the prompt |
| `confirmed` | bool | Confirm/deny a yes/no prompt |
| `value` | any | Provide a value response |

**Connection behavior:**
- Server sends WebSocket pings every 20 seconds
- Connection closes when the session is deleted or the server shuts down

---

## A2UI session broker

Zoea brokers [A2UI v0.9](https://a2ui.org/) batches between the agent runtime and a teammate's browser session. The server retains a replayable per-session message history, broadcasts each new batch over the existing session WebSocket, and replays a snapshot to a late subscriber. See [docs/specs/zoea-a2ui-session-broker.md](specs/zoea-a2ui-session-broker.md) for the full design.

**Presentation is client-owned.** The server treats every A2UI message as opaque JSON — it never interprets component semantics. It only validates protocol version, batch size, and (when present) the `createSurface.catalogId` against an allowed catalog list.

Pinned protocol version: `v0.9`. The only catalog accepted today is `https://a2ui.org/specification/v0_9/basic_catalog.json`.

### `POST /v1/sessions/{id}/a2ui/messages`

**Temporary bridge endpoint.** Use for development and integration work until the runtime emits A2UI batches natively. Validates the batch, appends it to the session's retained state, assigns the next `seq`, and broadcasts an `agent.a2ui` event to subscribers.

**Scope:** `sessions.write`

**Request body:**
```json
{
  "messages": [
    {
      "version": "v0.9",
      "createSurface": {
        "surfaceId": "main",
        "catalogId": "https://a2ui.org/specification/v0_9/basic_catalog.json",
        "sendDataModel": true
      }
    }
  ]
}
```

**Response** `202`
```json
{
  "seq": 1,
  "message_count": 1
}
```

**Errors:**

| Status | Condition |
|---|---|
| `400` | Empty batch, malformed message, missing/wrong `version`, or unknown catalog id |
| `403` | Caller lacks `sessions.write` scope |
| `404` | Session not found |
| `413` | Batch too large or session retention buffer would overflow |

Default limits (see [the spec](specs/zoea-a2ui-session-broker.md#validation-rules) for rationale):

| Limit | Default |
|---|---|
| Max request body | 256 KB |
| Max messages per batch | 100 |
| Max retained messages per session | 2000 |

---

### WebSocket event: `agent.a2ui.snapshot`

Sent immediately after WebSocket connect when the session has retained A2UI state. The client should reset its A2UI surface to the snapshot's `messages`, then continue to apply each subsequent `agent.a2ui` batch in `seq` order.

```json
{
  "type": "agent.a2ui.snapshot",
  "session_id": "s_000001",
  "timestamp": "2026-05-01T12:00:00Z",
  "data": {
    "version": "v0.9",
    "seq": 12,
    "messages": [
      {
        "version": "v0.9",
        "createSurface": {
          "surfaceId": "main",
          "catalogId": "https://a2ui.org/specification/v0_9/basic_catalog.json",
          "sendDataModel": true
        }
      }
    ]
  }
}
```

### WebSocket event: `agent.a2ui`

Sent for each live batch appended to the session.

```json
{
  "type": "agent.a2ui",
  "session_id": "s_000001",
  "timestamp": "2026-05-01T12:00:02Z",
  "data": {
    "version": "v0.9",
    "seq": 13,
    "messages": [
      {
        "version": "v0.9",
        "updateComponents": {
          "surfaceId": "main",
          "components": []
        }
      }
    ]
  }
}
```

### WebSocket event: `agent.a2ui.error`

Sent in response to a malformed inbound `a2ui.action` frame (see below).

```json
{
  "type": "agent.a2ui.error",
  "session_id": "s_000001",
  "timestamp": "2026-05-01T12:00:03Z",
  "data": {
    "error": "a2ui.action: message.version must be v0.9"
  }
}
```

### Client → server message: `a2ui.action`

Sent by the client to forward a user-initiated A2UI action to the runtime. The server validates protocol shape, then forwards through a process-layer seam. If the runtime does not yet implement A2UI input, an `agent.a2ui.error` event is broadcast.

```json
{
  "type": "a2ui.action",
  "data": {
    "message": {
      "version": "v0.9",
      "action": {
        "name": "submit",
        "surfaceId": "main",
        "sourceComponentId": "submit_btn",
        "timestamp": "2026-05-01T12:00:05Z",
        "context": {}
      }
    },
    "client_data_model": {
      "version": "v0.9",
      "surfaces": { "main": {} }
    },
    "client_capabilities": {
      "v0.9": {
        "supportedCatalogIds": [
          "https://a2ui.org/specification/v0_9/basic_catalog.json"
        ]
      }
    }
  }
}
```

---

## Error responses

All errors return JSON with an `error` field:

```json
{"error": "session not found"}
```

| Status | Meaning |
|---|---|
| `400` | Bad request (missing field, invalid JSON) |
| `401` | Unauthorized (missing or invalid credentials) |
| `403` | Forbidden (valid credentials, insufficient scope) |
| `404` | Session not found |
| `405` | Method not allowed |
| `429` | Too many requests (rate limited) |
| `500` | Internal server error |

### Rate limiting

Unauthenticated requests are rate limited to 30 requests per 60 seconds per IP. When rate limited, the response includes a `Retry-After` header:

```
HTTP/1.1 429 Too Many Requests
Retry-After: 42
Content-Type: application/json

{"error": "too many requests", "retry_after": 42}
```
