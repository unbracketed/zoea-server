package rpc

import "encoding/json"

// RPCEvent is the top-level envelope from Pi's JSONL stdout.
type RPCEvent struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

// MessageUpdateEvent wraps a message_update line.
type MessageUpdateEvent struct {
	Type                  string                `json:"type"`
	Message               json.RawMessage       `json:"message"`
	AssistantMessageEvent AssistantMessageEvent `json:"assistantMessageEvent"`
}

// AssistantMessageEvent is the nested delta inside message_update.
type AssistantMessageEvent struct {
	Type         string          `json:"type"`
	ContentIndex int             `json:"contentIndex"`
	Delta        string          `json:"delta,omitempty"`
	Content      string          `json:"content,omitempty"`
	Thinking     string          `json:"thinking,omitempty"`
	ToolCall     json.RawMessage `json:"toolCall,omitempty"`
	ToolName     string          `json:"toolName,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

// ToolExecStartEvent represents tool_execution_start.
type ToolExecStartEvent struct {
	Type       string          `json:"type"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Args       json.RawMessage `json:"args"`
}

// ToolExecUpdateEvent represents tool_execution_update.
type ToolExecUpdateEvent struct {
	Type          string          `json:"type"`
	ToolCallID    string          `json:"toolCallId"`
	ToolName      string          `json:"toolName"`
	PartialResult json.RawMessage `json:"partialResult"`
}

// ToolExecEndEvent represents tool_execution_end.
type ToolExecEndEvent struct {
	Type       string          `json:"type"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Result     json.RawMessage `json:"result"`
	IsError    bool            `json:"isError"`
}

// CompactionStartEvent represents compaction_start.
type CompactionStartEvent struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// CompactionEndEvent represents compaction_end.
type CompactionEndEvent struct {
	Type      string          `json:"type"`
	Reason    string          `json:"reason"`
	Result    json.RawMessage `json:"result"`
	Aborted   bool            `json:"aborted"`
	WillRetry bool            `json:"willRetry"`
}

// RetryStartEvent represents auto_retry_start.
type RetryStartEvent struct {
	Type         string `json:"type"`
	Attempt      int    `json:"attempt"`
	MaxAttempts  int    `json:"maxAttempts"`
	DelayMs      int    `json:"delayMs"`
	ErrorMessage string `json:"errorMessage"`
}

// RetryEndEvent represents auto_retry_end.
type RetryEndEvent struct {
	Type       string `json:"type"`
	Success    bool   `json:"success"`
	Attempt    int    `json:"attempt"`
	FinalError string `json:"finalError,omitempty"`
}

// QueueUpdateEvent represents queue_update.
type QueueUpdateEvent struct {
	Type     string   `json:"type"`
	Steering []string `json:"steering"`
	FollowUp []string `json:"followUp"`
}

// AgentEndEvent represents agent_end.
type AgentEndEvent struct {
	Type     string            `json:"type"`
	Messages json.RawMessage   `json:"messages"`
}

// TurnEndEvent represents turn_end.
type TurnEndEvent struct {
	Type        string          `json:"type"`
	Message     json.RawMessage `json:"message"`
	ToolResults json.RawMessage `json:"toolResults"`
}

// ExtensionUIRequestEvent represents extension_ui_request.
type ExtensionUIRequestEvent struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Method string `json:"method"`
}

// ExtensionErrorEvent represents extension_error.
type ExtensionErrorEvent struct {
	Type          string `json:"type"`
	ExtensionPath string `json:"extensionPath"`
	Event         string `json:"event"`
	Error         string `json:"error"`
}
