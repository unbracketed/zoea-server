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

type TextStart struct{}

type TextDelta struct {
	Delta string `json:"delta"`
}

type TextEnd struct {
	Content string `json:"content"`
}

type ThinkingStart struct{}

type ThinkingDelta struct {
	Delta string `json:"delta"`
}

type ThinkingEnd struct{}

type ToolCallStart struct {
	ToolName string `json:"tool_name,omitempty"`
}

type ToolCallDelta struct {
	Delta string `json:"delta"`
}

type ToolCallEnd struct {
	ToolCall json.RawMessage `json:"tool_call,omitempty"`
}

type MessageDone struct {
	Reason string `json:"reason"`
}

type MessageError struct {
	Reason string `json:"reason"`
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
