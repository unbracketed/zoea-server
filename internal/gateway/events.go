package gateway

import (
	"encoding/json"
	"time"
)

// Event is the normalized envelope sent to clients over WebSocket.
type Event struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	Timestamp string `json:"timestamp"`
	Data      any    `json:"data"`
}

// NewEvent creates an Event with the current timestamp.
func NewEvent(eventType string, data any) Event {
	return Event{
		Type:      eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Data:      data,
	}
}

// --- Per-event data structs ---

type MessageStart struct {
	Message json.RawMessage `json:"message,omitempty"`
}

type MessageEnd struct {
	Message json.RawMessage `json:"message,omitempty"`
}

type TextStart struct {
	ContentIndex int             `json:"content_index"`
	Message      json.RawMessage `json:"message,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

type TextDelta struct {
	ContentIndex int             `json:"content_index"`
	Delta        string          `json:"delta"`
	Message      json.RawMessage `json:"message,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

type TextEnd struct {
	ContentIndex int             `json:"content_index"`
	Content      string          `json:"content"`
	Message      json.RawMessage `json:"message,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

type ThinkingStart struct {
	ContentIndex int             `json:"content_index"`
	Message      json.RawMessage `json:"message,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

type ThinkingDelta struct {
	ContentIndex int             `json:"content_index"`
	Delta        string          `json:"delta"`
	Message      json.RawMessage `json:"message,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

type ThinkingEnd struct {
	ContentIndex int             `json:"content_index"`
	Message      json.RawMessage `json:"message,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

type ToolCallStart struct {
	ContentIndex int             `json:"content_index"`
	ToolName     string          `json:"tool_name,omitempty"`
	Message      json.RawMessage `json:"message,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

type ToolCallDelta struct {
	ContentIndex int             `json:"content_index"`
	Delta        string          `json:"delta"`
	Message      json.RawMessage `json:"message,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

type ToolCallEnd struct {
	ContentIndex int             `json:"content_index"`
	ToolCall     json.RawMessage `json:"tool_call,omitempty"`
	Message      json.RawMessage `json:"message,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

type MessageDone struct {
	Reason  string          `json:"reason"`
	Message json.RawMessage `json:"message,omitempty"`
	Partial json.RawMessage `json:"partial,omitempty"`
}

type MessageError struct {
	Reason  string          `json:"reason"`
	Message json.RawMessage `json:"message,omitempty"`
	Partial json.RawMessage `json:"partial,omitempty"`
}

type RunStart struct{}

type RunEnd struct {
	Messages json.RawMessage `json:"messages,omitempty"`
}

type TurnStart struct{}

type TurnEnd struct {
	Message     json.RawMessage `json:"message,omitempty"`
	ToolResults json.RawMessage `json:"tool_results,omitempty"`
}

type ToolExecStart struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Args       json.RawMessage `json:"args,omitempty"`
}

type ToolExecUpdate struct {
	ToolCallID    string          `json:"tool_call_id"`
	ToolName      string          `json:"tool_name"`
	PartialResult json.RawMessage `json:"partial_result,omitempty"`
}

type ToolExecEnd struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Result     json.RawMessage `json:"result,omitempty"`
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

type UIRequest struct {
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload"`
}

type ExtensionError struct {
	ExtensionPath string `json:"extension_path"`
	Event         string `json:"event"`
	Error         string `json:"error"`
}

type Unknown struct {
	EventType string          `json:"event_type"`
	Raw       json.RawMessage `json:"raw"`
}

// A2UIBatch carries one A2UI v0.9 message batch broadcast to subscribers
// when the agent (or the temporary injection endpoint) appends to the
// session's retained state. The server treats Messages as opaque JSON.
//
// MessageID, when set, ties this batch to a specific assistant chat
// message. Clients use it to render the surface inline inside that
// message's bubble instead of in a side panel — matching the A2UI guide
// where A2UI messages are emitted alongside the agent's text reply.
type A2UIBatch struct {
	Version   string          `json:"version"`
	Seq       int64           `json:"seq"`
	MessageID string          `json:"message_id,omitempty"`
	Messages  json.RawMessage `json:"messages"`
}

// A2UISnapshot replays the session's accumulated A2UI history to a
// late-subscribing client so it can rebuild the current surface. Sent at
// most once per WebSocket connect, immediately after subscribe, and only
// when retained state exists.
//
// Groups carries one entry per appended batch (in arrival order) so the
// client can re-bucket surfaces by their owning assistant message on
// reconnect. Messages remains the flat list (preserved for legacy clients
// that don't grouping support yet).
type A2UISnapshot struct {
	Version  string                `json:"version"`
	Seq      int64                 `json:"seq"`
	Messages json.RawMessage       `json:"messages"`
	Groups   []A2UISnapshotGroup   `json:"groups,omitempty"`
}

// A2UISnapshotGroup pairs a contiguous run of replayed messages with the
// assistant message id they were originally appended for (empty when the
// batch was injected with no correlation).
type A2UISnapshotGroup struct {
	MessageID string            `json:"message_id,omitempty"`
	Messages  []json.RawMessage `json:"messages"`
}

// A2UIAction relays an inbound a2ui.action frame to every session
// subscriber, including server-side agents (e.g. BASIL's flow runtime
// when subscribed via the session WebSocket) that need to consume the
// user's response without going through the Pi RPC pipe. Forwarding to
// Pi via SendA2UIAction continues independently — this broadcast is the
// "anyone watching the session" channel; Pi RPC is the "the agent that
// owns this session" channel.
//
// Message and the metadata fields stay opaque JSON so the broker
// remains catalog-agnostic.
type A2UIAction struct {
	Message            json.RawMessage `json:"message"`
	ClientDataModel    json.RawMessage `json:"client_data_model,omitempty"`
	ClientCapabilities json.RawMessage `json:"client_capabilities,omitempty"`
}
