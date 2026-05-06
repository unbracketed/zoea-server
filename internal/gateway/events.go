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

