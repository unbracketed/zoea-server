# Phase 2: Extension UI Bridging — Implementation Plan

## Goal

Enable Pi extensions to interact with the end user through the gateway. When an extension calls `ctx.ui.confirm()`, `ctx.ui.select()`, etc., the dialog surfaces on the client over WebSocket and the user's response routes back to the Pi process over stdin.

Fire-and-forget methods (`notify`, `setStatus`, `setWidget`, `setTitle`, `set_editor_text`) are already forwarded as `agent.ui.request` events from Phase 1. This phase adds the return path for dialog methods.

---

## Current State

### What works

Phase 1 maps `extension_ui_request` from Pi's stdout to `agent.ui.request` gateway events. Clients receive them over WebSocket.

The WS read loop currently handles one inbound message type:

```go
if msg["type"] == "abort" {
    _ = s.Abort(ctx)
}
```

### What's missing

1. No way for the client to send `extension_ui_response` back to Pi
2. The `AgentHandle` interface has no method for writing raw JSONL to Pi's stdin
3. No timeout tracking on the gateway side (Pi handles its own timeouts, but the gateway should surface expiry to clients)

---

## Design

### Protocol over WebSocket

**Server → Client** (dialog request, already working):

```json
{
  "type": "agent.ui.request",
  "session_id": "s_000001",
  "timestamp": "2026-04-01T12:00:00Z",
  "data": {
    "id": "uuid-1",
    "method": "confirm",
    "payload": <full original extension_ui_request JSON>
  }
}
```

The `data.payload` field carries the complete Pi RPC event so the client has access to all fields (`title`, `message`, `options`, `timeout`, `placeholder`, `prefill`, etc.) without the gateway needing to parse every method variant.

**Client → Server** (dialog response):

```json
{
  "type": "ui_response",
  "id": "uuid-1",
  "value": "Allow"
}
```

Or for confirm:

```json
{
  "type": "ui_response",
  "id": "uuid-1",
  "confirmed": true
}
```

Or for cancellation:

```json
{
  "type": "ui_response",
  "id": "uuid-1",
  "cancelled": true
}
```

The gateway translates this to Pi's expected format and writes to stdin:

```json
{"type": "extension_ui_response", "id": "uuid-1", "value": "Allow"}
```

### Method categories

| Method | Type | Client must respond? |
|---|---|---|
| `select` | Dialog | Yes — `value` (string) or `cancelled` |
| `confirm` | Dialog | Yes — `confirmed` (bool) or `cancelled` |
| `input` | Dialog | Yes — `value` (string) or `cancelled` |
| `editor` | Dialog | Yes — `value` (string) or `cancelled` |
| `notify` | Fire-and-forget | No |
| `setStatus` | Fire-and-forget | No |
| `setWidget` | Fire-and-forget | No |
| `setTitle` | Fire-and-forget | No |
| `set_editor_text` | Fire-and-forget | No |

The gateway doesn't need to distinguish these — it forwards all `extension_ui_request` events to the client and forwards any `ui_response` messages back to Pi. Pi handles timeout and default resolution internally.

---

## Files to Create

### 1. `internal/process/ui_writer.go`

New method on `AgentHandle` to write raw JSONL to Pi's stdin (for extension UI responses):

```go
// SendUIResponse writes an extension_ui_response to the Pi process stdin.
SendUIResponse(ctx context.Context, response UIResponse) error
```

With the type:

```go
type UIResponse struct {
    ID        string `json:"id"`
    Value     any    `json:"value,omitempty"`
    Confirmed *bool  `json:"confirmed,omitempty"`
    Cancelled bool   `json:"cancelled,omitempty"`
}
```

Implementation on `rpcHandle`: marshal to `{"type": "extension_ui_response", "id": "...", ...}` and write to stdin. This is NOT a command with id correlation — Pi doesn't send a response back for `extension_ui_response`. It's fire-and-forget from gateway to Pi.

Implementation on `noopHandle`: no-op (return nil).

---

## Files to Modify

### 2. `internal/process/types.go`

Add to `AgentHandle` interface:

```go
SendUIResponse(ctx context.Context, resp UIResponse) error
```

Add `UIResponse` struct.

### 3. `internal/process/rpc_manager.go`

Implement `SendUIResponse` on `rpcHandle`:

```go
func (h *rpcHandle) SendUIResponse(ctx context.Context, resp UIResponse) error {
    payload := map[string]any{
        "type": "extension_ui_response",
        "id":   resp.ID,
    }
    if resp.Cancelled {
        payload["cancelled"] = true
    } else if resp.Confirmed != nil {
        payload["confirmed"] = *resp.Confirmed
    } else if resp.Value != nil {
        payload["value"] = resp.Value
    }

    b, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("marshal ui response: %w", err)
    }

    h.writeMu.Lock()
    _, err = h.stdin.Write(append(b, '\n'))
    h.writeMu.Unlock()
    return err
}
```

Key difference from `sendCommand`: no `id` correlation, no response channel, no waiting. This is a one-way write.

### 4. `internal/process/noop_manager.go`

Implement `SendUIResponse` as a no-op:

```go
func (h *noopHandle) SendUIResponse(_ context.Context, _ UIResponse) error {
    return nil
}
```

### 5. `internal/session/manager.go`

Add passthrough on `Session`:

```go
func (s *Session) SendUIResponse(ctx context.Context, resp process.UIResponse) error {
    s.LastActive = time.Now().UTC()
    return s.handle.SendUIResponse(ctx, resp)
}
```

### 6. `internal/api/handler.go`

Update the WS read loop to handle `ui_response` messages:

```go
go func() {
    defer close(readDone)
    for {
        var msg map[string]any
        if err := conn.ReadJSON(&msg); err != nil {
            return
        }
        switch msg["type"] {
        case "abort":
            _ = s.Abort(ctx)
        case "ui_response":
            h.handleWSUIResponse(ctx, s, msg)
        }
    }
}()
```

New helper:

```go
func (h *Handler) handleWSUIResponse(ctx context.Context, s *session.Session, msg map[string]any) {
    id, _ := msg["id"].(string)
    if id == "" {
        return
    }

    resp := process.UIResponse{ID: id}

    if cancelled, ok := msg["cancelled"].(bool); ok && cancelled {
        resp.Cancelled = true
    } else if confirmed, ok := msg["confirmed"].(bool); ok {
        resp.Confirmed = &confirmed
    } else if value, ok := msg["value"]; ok {
        resp.Value = value
    }

    _ = s.SendUIResponse(ctx, resp)
}
```

---

## Testing

### Unit tests: `internal/process/ui_writer_test.go`

Test `UIResponse` serialization:

| Test | Input | Expected JSONL |
|---|---|---|
| `TestUIResponseValue` | `{ID: "uuid-1", Value: "Allow"}` | `{"type":"extension_ui_response","id":"uuid-1","value":"Allow"}` |
| `TestUIResponseConfirmed` | `{ID: "uuid-2", Confirmed: boolPtr(true)}` | `{"type":"extension_ui_response","id":"uuid-2","confirmed":true}` |
| `TestUIResponseCancelled` | `{ID: "uuid-3", Cancelled: true}` | `{"type":"extension_ui_response","id":"uuid-3","cancelled":true}` |
| `TestUIResponseEmptyID` | `{ID: ""}` | error or no-op |

### Integration test

1. Start gateway + pi subprocess
2. Load an extension that calls `ctx.ui.confirm("Allow?", "Run command?")`
3. Observe `agent.ui.request` arrives on WebSocket with `method: "confirm"`
4. Send `{"type": "ui_response", "id": "<matching>", "confirmed": true}` over WS
5. Verify extension receives the confirmation and continues

This requires a test extension — can use a simple pi extension that calls `ctx.ui.confirm()` and writes the result to stdout. Alternatively, test with a mock Pi process that emits a canned `extension_ui_request` and expects `extension_ui_response` on stdin.

### Mock process test: `internal/api/handler_test.go`

Spin up a mock process that:
1. Writes `extension_ui_request` to its stdout
2. Reads stdin for `extension_ui_response`
3. Validates the response matches

Wire it through the gateway WS endpoint end-to-end.

---

## Implementation Order

1. **Add `UIResponse` struct + `SendUIResponse` to `AgentHandle`** in `types.go`
2. **Implement on `rpcHandle`** in `rpc_manager.go` (stdin write, no correlation)
3. **Implement on `noopHandle`** in `noop_manager.go` (no-op)
4. **Add passthrough on `Session`** in `session/manager.go`
5. **Update WS read loop** in `api/handler.go` (dispatch `ui_response`, parse fields)
6. **Unit tests** for serialization
7. **Build + test** — `go build ./...` + `go test ./...`

Steps 1-4 are additive. Step 5 is a small change to the existing WS read goroutine. No breaking changes.

---

## Estimated Effort

1-2 days including tests. The implementation is straightforward — it's a one-way stdin write plus WS message dispatch. The complexity is low because Pi handles all timeout/default resolution logic internally.
