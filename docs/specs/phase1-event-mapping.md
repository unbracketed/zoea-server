# Phase 1: Normalized Event Mapping — Implementation Plan

## Goal

Replace raw Pi RPC event forwarding with a stable, typed gateway event schema. Clients receive clean events they can build UI against without parsing Pi internals.

---

## Current Flow

```
Pi stdout (JSONL) → rpcHandle.readLoop → broadcastEvent(type, raw) → Event{Type, Raw}
    → WS handler writes: {"type":"agent.event","event_type":"...","raw":<full line>}
```

Everything after `broadcastEvent` sends the raw JSONL blob unchanged. Clients must understand Pi's internal event schema including nested `assistantMessageEvent` sub-types.

## Target Flow

```
Pi stdout (JSONL) → rpcHandle.readLoop → parseRPCEvent(line) → mapToGatewayEvents(parsed)
    → broadcastEvent(gatewayEvent) → WS handler writes normalized envelope
```

---

## Files to Create

### 1. `internal/rpc/events.go` — Pi RPC event structs

Typed Go structs for each Pi RPC event we care about. These mirror Pi's protocol exactly.

```go
// Top-level RPC event (what comes off stdout)
type RPCEvent struct {
    Type string          `json:"type"`
    Raw  json.RawMessage // keep original for fallback
}

// message_update payload
type MessageUpdateEvent struct {
    Type                  string                `json:"type"`
    Message               json.RawMessage       `json:"message"`
    AssistantMessageEvent AssistantMessageEvent  `json:"assistantMessageEvent"`
}

type AssistantMessageEvent struct {
    Type         string          `json:"type"`          // text_delta, thinking_delta, toolcall_start, etc.
    ContentIndex int             `json:"contentIndex"`
    Delta        string          `json:"delta,omitempty"`
    Content      string          `json:"content,omitempty"`
    Thinking     string          `json:"thinking,omitempty"`
    ToolCall     json.RawMessage `json:"toolCall,omitempty"`
    ToolName     string          `json:"toolName,omitempty"`
    Reason       string          `json:"reason,omitempty"`
    Partial      json.RawMessage `json:"partial,omitempty"`
}

// tool_execution_start
type ToolExecStartEvent struct {
    Type       string          `json:"type"`
    ToolCallID string          `json:"toolCallId"`
    ToolName   string          `json:"toolName"`
    Args       json.RawMessage `json:"args"`
}

// tool_execution_update
type ToolExecUpdateEvent struct {
    Type          string          `json:"type"`
    ToolCallID    string          `json:"toolCallId"`
    ToolName      string          `json:"toolName"`
    PartialResult json.RawMessage `json:"partialResult"`
}

// tool_execution_end
type ToolExecEndEvent struct {
    Type       string          `json:"type"`
    ToolCallID string          `json:"toolCallId"`
    ToolName   string          `json:"toolName"`
    Result     json.RawMessage `json:"result"`
    IsError    bool            `json:"isError"`
}

// compaction_start / compaction_end
type CompactionStartEvent struct {
    Type   string `json:"type"`
    Reason string `json:"reason"`
}

type CompactionEndEvent struct {
    Type      string          `json:"type"`
    Reason    string          `json:"reason"`
    Result    json.RawMessage `json:"result"`
    Aborted   bool            `json:"aborted"`
    WillRetry bool            `json:"willRetry"`
}

// auto_retry_start / auto_retry_end
type RetryStartEvent struct {
    Type         string `json:"type"`
    Attempt      int    `json:"attempt"`
    MaxAttempts  int    `json:"maxAttempts"`
    DelayMs      int    `json:"delayMs"`
    ErrorMessage string `json:"errorMessage"`
}

type RetryEndEvent struct {
    Type       string `json:"type"`
    Success    bool   `json:"success"`
    Attempt    int    `json:"attempt"`
    FinalError string `json:"finalError,omitempty"`
}

// queue_update
type QueueUpdateEvent struct {
    Type     string   `json:"type"`
    Steering []string `json:"steering"`
    FollowUp []string `json:"followUp"`
}

// agent_end
type AgentEndEvent struct {
    Type     string            `json:"type"`
    Messages []json.RawMessage `json:"messages"`
}

// extension_ui_request
type ExtensionUIRequestEvent struct {
    Type    string          `json:"type"`
    ID      string          `json:"id"`
    Method  string          `json:"method"`
    Payload json.RawMessage // remaining fields vary by method
}
```

### 2. `internal/rpc/mapper.go` — RPC-to-gateway event mapper

Single function that takes a raw JSONL line and returns zero or more gateway events.

```go
func MapRPCLine(raw []byte) []gateway.Event
```

Key logic:
- Parse top-level `type` field
- For `message_update`: sub-dispatch on `assistantMessageEvent.type`
  - `text_delta` → `agent.text.delta`
  - `text_start` → `agent.text.start`
  - `text_end` → `agent.text.end`
  - `thinking_delta` → `agent.thinking.delta`
  - `thinking_start` → `agent.thinking.start`
  - `thinking_end` → `agent.thinking.end`
  - `toolcall_start` → `agent.toolcall.start`
  - `toolcall_delta` → `agent.toolcall.delta`
  - `toolcall_end` → `agent.toolcall.end`
  - `done` → `agent.message.done`
  - `error` → `agent.message.error`
- For other top-level types: direct 1:1 mapping
- Unknown types: forward as `agent.unknown` with raw payload (don't drop)

### 3. `internal/gateway/events.go` — Gateway event types

The stable contract clients code against.

```go
// Event is the normalized envelope sent to clients.
type Event struct {
    Type      string `json:"type"`
    SessionID string `json:"session_id"`
    Timestamp string `json:"timestamp"`
    Data      any    `json:"data"`
}

// Per-event data structs:

type TextDelta struct {
    Delta string `json:"delta"`
}

type TextEnd struct {
    Content string `json:"content"`
}

type ThinkingDelta struct {
    Delta string `json:"delta"`
}

type ToolCallStart struct {
    ToolName string `json:"tool_name"`
}

type ToolCallDelta struct {
    Delta string `json:"delta"`
}

type ToolCallEnd struct {
    ToolCall json.RawMessage `json:"tool_call"`
}

type MessageDone struct {
    Reason string `json:"reason"`
}

type MessageError struct {
    Reason string `json:"reason"`
}

type ToolExecStart struct {
    ToolCallID string          `json:"tool_call_id"`
    ToolName   string          `json:"tool_name"`
    Args       json.RawMessage `json:"args"`
}

type ToolExecUpdate struct {
    ToolCallID    string          `json:"tool_call_id"`
    ToolName      string          `json:"tool_name"`
    PartialResult json.RawMessage `json:"partial_result"`
}

type ToolExecEnd struct {
    ToolCallID string          `json:"tool_call_id"`
    ToolName   string          `json:"tool_name"`
    Result     json.RawMessage `json:"result"`
    IsError    bool            `json:"is_error"`
}

type CompactionStart struct {
    Reason string `json:"reason"`
}

type CompactionEnd struct {
    Reason    string `json:"reason"`
    Aborted   bool   `json:"aborted"`
    WillRetry bool   `json:"will_retry"`
}

type RetryStart struct {
    Attempt     int    `json:"attempt"`
    MaxAttempts int    `json:"max_attempts"`
    DelayMs     int    `json:"delay_ms"`
    Error       string `json:"error"`
}

type RetryEnd struct {
    Success    bool   `json:"success"`
    Attempt    int    `json:"attempt"`
    FinalError string `json:"final_error,omitempty"`
}

type QueueUpdate struct {
    Steering []string `json:"steering"`
    FollowUp []string `json:"follow_up"`
}

type RunEnd struct {
    Messages json.RawMessage `json:"messages"`
}

type UIRequest struct {
    ID      string          `json:"id"`
    Method  string          `json:"method"`
    Payload json.RawMessage `json:"payload"`
}

type Unknown struct {
    EventType string          `json:"event_type"`
    Raw       json.RawMessage `json:"raw"`
}
```

---

## Files to Modify

### 4. `internal/process/types.go`

Change `Event` to use the gateway event type:

```go
import "github.com/brian/go-agent-gateway/internal/gateway"

// Replace process.Event with gateway.Event in AgentHandle.Subscribe
```

Or keep `process.Event` as-is but have the rpc_manager produce gateway events directly. Simpler: **change `process.Event` to carry `gateway.Event`**.

Decision: change `Event` to:
```go
type Event struct {
    gateway.Event
}
```

This avoids a separate translation layer in the WS handler.

### 5. `internal/process/rpc_manager.go`

Update `readLoop` to call the mapper:

```go
// Before:
h.broadcastEvent(env.Type, json.RawMessage(line))

// After:
events := rpc.MapRPCLine([]byte(line))
for _, e := range events {
    h.broadcastGatewayEvent(e)
}
```

### 6. `internal/process/noop_manager.go`

Update to emit gateway-typed events (keep working for tests).

### 7. `internal/api/handler.go`

Simplify WS send — just write the gateway event directly instead of wrapping in `{"type":"agent.event","raw":...}`:

```go
// Before:
payload := map[string]any{
    "type":       "agent.event",
    "session_id": sessionID,
    "event_type": event.Type,
    "raw":        json.RawMessage(event.Raw),
}

// After:
event.SessionID = sessionID
conn.WriteJSON(event)
```

---

## Testing

### Unit tests: `internal/rpc/mapper_test.go`

Test each mapping path with real Pi RPC JSON fixtures:

| Test | Input | Expected output |
|---|---|---|
| `TestMapTextDelta` | `message_update` with `text_delta` | `agent.text.delta` with `{delta}` |
| `TestMapThinkingDelta` | `message_update` with `thinking_delta` | `agent.thinking.delta` |
| `TestMapToolCallStart` | `message_update` with `toolcall_start` | `agent.toolcall.start` |
| `TestMapToolExecLifecycle` | `tool_execution_start/update/end` | `agent.tool.start/update/end` |
| `TestMapAgentStartEnd` | `agent_start`, `agent_end` | `agent.run.start`, `agent.run.end` |
| `TestMapTurnStartEnd` | `turn_start`, `turn_end` | `agent.turn.start`, `agent.turn.end` |
| `TestMapCompaction` | `compaction_start/end` | `agent.compaction.start/end` |
| `TestMapRetry` | `auto_retry_start/end` | `agent.retry.start/end` |
| `TestMapQueueUpdate` | `queue_update` | `agent.queue.update` |
| `TestMapExtensionUI` | `extension_ui_request` | `agent.ui.request` |
| `TestMapUnknownType` | `some_future_event` | `agent.unknown` with raw |
| `TestMapResponse` | `type: "response"` | empty (responses handled separately) |

Fixtures: copy real JSONL from Pi RPC docs into `internal/rpc/testdata/`.

### Integration test

Send a prompt through the gateway, collect WS events, assert:
- Received `agent.run.start`
- Received one or more `agent.text.delta`
- Received `agent.run.end`
- All events have `type`, `session_id`, `timestamp` fields

---

## Implementation Order

1. **Create `internal/gateway/events.go`** — gateway event types (pure types, no logic)
2. **Create `internal/rpc/events.go`** — Pi RPC event structs (pure types)
3. **Create `internal/rpc/mapper.go`** — mapping function
4. **Create `internal/rpc/mapper_test.go`** + `testdata/` — unit tests with fixtures
5. **Update `internal/process/types.go`** — switch Event to gateway type
6. **Update `internal/process/rpc_manager.go`** — call mapper in readLoop
7. **Update `internal/process/noop_manager.go`** — emit gateway events
8. **Update `internal/api/handler.go`** — simplify WS send
9. **Build + test** — `go build ./...` + `go test ./...`

Steps 1-4 are additive (no existing code changes). Steps 5-8 are the wiring swap. Should be safe to do in one pass.

---

## Estimated Effort

2-3 days including tests. The mapping logic is mechanical — the effort is in getting the struct shapes right and writing good test fixtures.
