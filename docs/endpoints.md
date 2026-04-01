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
  "project_id": "my-project"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `user_id` | string | yes | Identifies the user |
| `project_id` | string | no | Optional project context |

**Response** `201`
```json
{
  "session_id": "s_000001",
  "status": "ready"
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

**Response** `200`
```json
{
  "messages": [ ... ]
}
```

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
