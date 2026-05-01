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
  "external_id": "telegram:12345"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `user_id` | string | yes | Identifies the user |
| `project_id` | string | no | Optional project context |
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

## Glimpse endpoints

These endpoints implement the BASIL Glimpse delivery and correlation layer. BASIL's `ZoeaTransport` posts a render request, Zoea broadcasts a `glimpse.render` event to the target session's WebSocket, the client submits an action or cancel callback, and Zoea returns a single terminal response to the waiting transport call. See [docs/specs/zoea-glimpse-integration-spec.md](specs/zoea-glimpse-integration-spec.md) and [docs/specs/zoea-glimpse-server-addendum.md](specs/zoea-glimpse-server-addendum.md).

**Presentation is client-owned.** The server guarantees delivery, correlation, timeout, and authorization. The client decides how to display the prompt — modal, side panel, separate surface, anything else — and Zoea never inspects the surface, the HTML, or the payloads.

The two scopes used here:

| Scope | Permissions |
|---|---|
| `glimpse.render` | Submit blocking render requests (BASIL → Zoea) |
| `glimpse.action` | Submit action/cancel callbacks (browser → Zoea) |

### `POST /api/glimpse/v1/render`

Blocking endpoint. Returns when the user submits an action, dismisses the prompt, the request times out, or the target session is busy.

**Scope:** `glimpse.render`

**Request body:**
```json
{
  "type": "render",
  "request": {
    "request_id": "9f2d...",
    "flow_id": "2d6b...",
    "surface": { "step_id": "pick_items", "title": "Pick items", "...": "..." },
    "timeout_seconds": 300,
    "hints": { "preferred_mode": "panel" }
  },
  "html": "<base64-encoded full HTML document>",
  "target": {
    "session_id": "s_000001",
    "conversation_id": "optional",
    "user_id": "optional"
  }
}
```

The `html` field is the complete self-contained HTML produced by BASIL, base64-encoded. Zoea forwards it unchanged to the target session. The `hints` field, if present, is forwarded verbatim — clients may use it as advisory presentation metadata or ignore it. Zoea never reads `surface`.

**Successful response — action** `200`
```json
{
  "type": "action",
  "request_id": "9f2d...",
  "payload": {
    "request_id": "9f2d...",
    "action_id": "continue",
    "raw": { "field_id": "value" }
  }
}
```

The `payload` is the raw `window.glimpse.send(...)` body, forwarded with no reshaping.

**Successful response — cancelled** `200`
```json
{
  "type": "cancelled",
  "request_id": "9f2d..."
}
```

**Successful response — timeout** `200`
```json
{
  "type": "error",
  "request_id": "9f2d...",
  "error": "no action received before timeout",
  "fatal": false
}
```

**Busy** `409`

When the target session already has an active Glimpse render:
```json
{
  "type": "busy",
  "request_id": "9f2d...",
  "active_request_id": "7a11...",
  "error": "session already has an active glimpse render"
}
```

**Errors:**

| Status | Condition |
|---|---|
| `400` | Missing `request_id`, `html`, or `target.session_id` |
| `404` | Target session does not exist |
| `409` | Session busy (above) or duplicate `request_id` |

---

### `POST /api/glimpse/v1/action`

Client callback that submits the user's form action.

**Scope:** `glimpse.action`

**Request body:**
```json
{
  "request_id": "9f2d...",
  "payload": {
    "request_id": "9f2d...",
    "action_id": "continue",
    "raw": { "field_id": "value" }
  }
}
```

The `payload.raw` field is forwarded to the waiting render call without any server-side reshaping. BASIL is responsible for interpreting groups, captures, and result structure.

**Response** `200`
```json
{ "ok": true }
```

**Errors:**

| Status | Condition |
|---|---|
| `400` | Missing `request_id` or `payload` |
| `403` | Caller lacks `glimpse.action` scope |
| `404` | Unknown `request_id` (no pending render) |
| `409` | Render already resolved (cancel/timeout/duplicate submission) |

---

### `POST /api/glimpse/v1/cancel`

Client callback that resolves a pending render as cancelled. Use when the user dismisses the prompt, navigates away, or otherwise abandons the surface — whatever shape that surface takes.

**Scope:** `glimpse.action`

**Request body:**
```json
{ "request_id": "9f2d..." }
```

**Response** `200`
```json
{ "ok": true }
```

**Errors:**

| Status | Condition |
|---|---|
| `400` | Missing `request_id` |
| `403` | Caller lacks `glimpse.action` scope |
| `404` | Unknown `request_id` |
| `409` | Render already resolved |

---

### WebSocket event: `glimpse.render`

Pushed over the existing `GET /v1/sessions/{id}/stream` WebSocket when a render is registered for that session. This is a delivery signal — *"a client for this session should present this Glimpse surface somehow"* — not a UI command tied to any particular shell. The client decides how to render it.

To run the BASIL HTML directly, the client decodes `html` (base64), displays it inside whatever container it prefers, installs a `window.glimpse.send` bridge, and POSTs the resulting payload to `/api/glimpse/v1/action`. Clients that present the prompt differently are free to do so; the only contract is that they eventually call `/action` or `/cancel`.

```json
{
  "type": "glimpse.render",
  "session_id": "s_000001",
  "timestamp": "2026-05-01T01:21:31.234Z",
  "data": {
    "request_id": "9f2d...",
    "flow_id": "2d6b...",
    "html": "<base64-encoded full HTML document>",
    "timeout_seconds": 300,
    "hints": { "preferred_mode": "panel" }
  }
}
```

`hints` is whatever BASIL provided in `request.hints`, forwarded verbatim. It is advisory only — clients may ignore it. Zoea does not interpret `surface` or any other BASIL-defined fields, so they do not appear in this event.

### WebSocket event: `glimpse.close`

Pushed when a render reaches a terminal state. This is a lifecycle event, not a layout instruction — clients may respond by closing a modal, clearing a panel, replacing content with a receipt, or doing nothing.

```json
{
  "type": "glimpse.close",
  "session_id": "s_000001",
  "timestamp": "2026-05-01T01:21:42.118Z",
  "data": {
    "request_id": "9f2d...",
    "reason": "completed",
    "status": "action",
    "action_id": "continue"
  }
}
```

| Field | Description |
|---|---|
| `request_id` | The render this close is for |
| `reason` | One of `completed`, `cancelled`, `timed_out`, `error` |
| `status` | Mirrors the terminal envelope returned to BASIL: `action`, `cancelled`, `timed_out`, `error` |
| `action_id` | When `status=action`, the action_id the user submitted (advisory; useful for receipt UIs) |

`status` and `action_id` are advisory. Clients that already track their own `/action` or `/cancel` response do not need them.

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
