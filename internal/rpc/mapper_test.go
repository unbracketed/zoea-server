package rpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/unbracketed/zoea-server/internal/gateway"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return b
}

func requireOne(t *testing.T, events []gateway.Event, expectedType string) gateway.Event {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != expectedType {
		t.Fatalf("expected type %q, got %q", expectedType, events[0].Type)
	}
	if events[0].Timestamp == "" {
		t.Fatal("expected non-empty timestamp")
	}
	return events[0]
}

func dataAs[T any](t *testing.T, e gateway.Event) T {
	t.Helper()
	b, err := json.Marshal(e.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("unmarshal data into %T: %v", v, err)
	}
	return v
}

// --- message_update sub-types ---

func TestMapTextDelta(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "text_delta.json")), "agent.text.delta")
	d := dataAs[gateway.TextDelta](t, e)
	if d.Delta != "Hello" {
		t.Fatalf("expected delta 'Hello', got %q", d.Delta)
	}
	if d.ContentIndex != 0 {
		t.Fatalf("expected content_index 0, got %d", d.ContentIndex)
	}
	if len(d.Message) == 0 {
		t.Fatal("expected non-empty message")
	}
	if len(d.Partial) == 0 {
		t.Fatal("expected non-empty partial")
	}
}

func TestMapTextStart(t *testing.T) {
	requireOne(t, MapRPCLine(loadFixture(t, "text_start.json")), "agent.text.start")
}

func TestMapTextEnd(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "text_end.json")), "agent.text.end")
	d := dataAs[gateway.TextEnd](t, e)
	if d.Content != "Hello world" {
		t.Fatalf("expected content 'Hello world', got %q", d.Content)
	}
}

func TestMapThinkingDelta(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "thinking_delta.json")), "agent.thinking.delta")
	d := dataAs[gateway.ThinkingDelta](t, e)
	if d.Delta != "Let me think..." {
		t.Fatalf("expected delta 'Let me think...', got %q", d.Delta)
	}
	if len(d.Message) == 0 {
		t.Fatal("expected non-empty message")
	}
	if len(d.Partial) == 0 {
		t.Fatal("expected non-empty partial")
	}
}

func TestMapThinkingStart(t *testing.T) {
	requireOne(t, MapRPCLine(loadFixture(t, "thinking_start.json")), "agent.thinking.start")
}

func TestMapThinkingEnd(t *testing.T) {
	requireOne(t, MapRPCLine(loadFixture(t, "thinking_end.json")), "agent.thinking.end")
}

func TestMapToolCallStart(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "toolcall_start.json")), "agent.toolcall.start")
	d := dataAs[gateway.ToolCallStart](t, e)
	if d.ToolName != "bash" {
		t.Fatalf("expected tool_name 'bash', got %q", d.ToolName)
	}
}

func TestMapToolCallDelta(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "toolcall_delta.json")), "agent.toolcall.delta")
	d := dataAs[gateway.ToolCallDelta](t, e)
	if d.Delta == "" {
		t.Fatal("expected non-empty delta")
	}
}

func TestMapToolCallEnd(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "toolcall_end.json")), "agent.toolcall.end")
	d := dataAs[gateway.ToolCallEnd](t, e)
	if len(d.ToolCall) == 0 {
		t.Fatal("expected non-empty tool_call")
	}
	if d.ContentIndex != 1 {
		t.Fatalf("expected content_index 1, got %d", d.ContentIndex)
	}
	if len(d.Message) == 0 {
		t.Fatal("expected non-empty message")
	}
	if len(d.Partial) == 0 {
		t.Fatal("expected non-empty partial")
	}
}

func TestMapMessageDone(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "message_done.json")), "agent.message.done")
	d := dataAs[gateway.MessageDone](t, e)
	if d.Reason != "toolUse" {
		t.Fatalf("expected reason 'toolUse', got %q", d.Reason)
	}
}

func TestMapMessageError(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "message_error.json")), "agent.message.error")
	d := dataAs[gateway.MessageError](t, e)
	if d.Reason != "aborted" {
		t.Fatalf("expected reason 'aborted', got %q", d.Reason)
	}
}

// --- message lifecycle events ---

func TestMapMessageStart(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "message_start.json")), "agent.message.start")
	d := dataAs[gateway.MessageStart](t, e)
	if len(d.Message) == 0 {
		t.Fatal("expected non-empty message")
	}
}

func TestMapMessageEnd(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "message_end.json")), "agent.message.end")
	d := dataAs[gateway.MessageEnd](t, e)
	if len(d.Message) == 0 {
		t.Fatal("expected non-empty message")
	}
}

// --- top-level lifecycle events ---

func TestMapAgentStart(t *testing.T) {
	requireOne(t, MapRPCLine(loadFixture(t, "agent_start.json")), "agent.run.start")
}

func TestMapAgentEnd(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "agent_end.json")), "agent.run.end")
	d := dataAs[gateway.RunEnd](t, e)
	if len(d.Messages) == 0 {
		t.Fatal("expected non-empty messages")
	}
}

func TestMapTurnStart(t *testing.T) {
	requireOne(t, MapRPCLine(loadFixture(t, "turn_start.json")), "agent.turn.start")
}

func TestMapTurnEnd(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "turn_end.json")), "agent.turn.end")
	d := dataAs[gateway.TurnEnd](t, e)
	if len(d.Message) == 0 {
		t.Fatal("expected non-empty message")
	}
	if len(d.ToolResults) == 0 {
		t.Fatal("expected non-empty tool_results")
	}
}

// --- tool execution ---

func TestMapToolExecStart(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "tool_exec_start.json")), "agent.tool.start")
	d := dataAs[gateway.ToolExecStart](t, e)
	if d.ToolCallID != "call_abc123" {
		t.Fatalf("expected tool_call_id 'call_abc123', got %q", d.ToolCallID)
	}
	if d.ToolName != "bash" {
		t.Fatalf("expected tool_name 'bash', got %q", d.ToolName)
	}
}

func TestMapToolExecUpdate(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "tool_exec_update.json")), "agent.tool.update")
	d := dataAs[gateway.ToolExecUpdate](t, e)
	if d.ToolCallID != "call_abc123" {
		t.Fatalf("expected tool_call_id 'call_abc123', got %q", d.ToolCallID)
	}
	if len(d.PartialResult) == 0 {
		t.Fatal("expected non-empty partial_result")
	}
}

func TestMapToolExecEnd(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "tool_exec_end.json")), "agent.tool.end")
	d := dataAs[gateway.ToolExecEnd](t, e)
	if d.ToolCallID != "call_abc123" {
		t.Fatalf("expected tool_call_id 'call_abc123', got %q", d.ToolCallID)
	}
	if d.IsError {
		t.Fatal("expected is_error false")
	}
}

// --- compaction ---

func TestMapCompactionStart(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "compaction_start.json")), "agent.compaction.start")
	d := dataAs[gateway.CompactionStart](t, e)
	if d.Reason != "threshold" {
		t.Fatalf("expected reason 'threshold', got %q", d.Reason)
	}
}

func TestMapCompactionEnd(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "compaction_end.json")), "agent.compaction.end")
	d := dataAs[gateway.CompactionEnd](t, e)
	if d.Reason != "threshold" {
		t.Fatalf("expected reason 'threshold', got %q", d.Reason)
	}
	if d.Aborted {
		t.Fatal("expected aborted false")
	}
}

// --- retry ---

func TestMapRetryStart(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "retry_start.json")), "agent.retry.start")
	d := dataAs[gateway.RetryStart](t, e)
	if d.Attempt != 1 {
		t.Fatalf("expected attempt 1, got %d", d.Attempt)
	}
	if d.MaxAttempts != 3 {
		t.Fatalf("expected max_attempts 3, got %d", d.MaxAttempts)
	}
	if d.DelayMs != 2000 {
		t.Fatalf("expected delay_ms 2000, got %d", d.DelayMs)
	}
	if d.Error != "529 overloaded" {
		t.Fatalf("expected error '529 overloaded', got %q", d.Error)
	}
}

func TestMapRetryEndSuccess(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "retry_end.json")), "agent.retry.end")
	d := dataAs[gateway.RetryEnd](t, e)
	if !d.Success {
		t.Fatal("expected success true")
	}
	if d.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", d.Attempt)
	}
}

func TestMapRetryEndFail(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "retry_end_fail.json")), "agent.retry.end")
	d := dataAs[gateway.RetryEnd](t, e)
	if d.Success {
		t.Fatal("expected success false")
	}
	if d.FinalError == "" {
		t.Fatal("expected non-empty final_error")
	}
}

// --- queue ---

func TestMapQueueUpdate(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "queue_update.json")), "agent.queue.update")
	d := dataAs[gateway.QueueUpdate](t, e)
	if len(d.Steering) != 1 {
		t.Fatalf("expected 1 steering, got %d", len(d.Steering))
	}
	if len(d.FollowUp) != 1 {
		t.Fatalf("expected 1 follow_up, got %d", len(d.FollowUp))
	}
}

// --- extension ---

func TestMapExtensionUIRequest(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "extension_ui_request.json")), "agent.ui.request")
	d := dataAs[gateway.UIRequest](t, e)
	if d.ID != "uuid-1" {
		t.Fatalf("expected id 'uuid-1', got %q", d.ID)
	}
	if d.Method != "confirm" {
		t.Fatalf("expected method 'confirm', got %q", d.Method)
	}
}

func TestMapExtensionError(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "extension_error.json")), "agent.extension.error")
	d := dataAs[gateway.ExtensionError](t, e)
	if d.Error != "Something broke" {
		t.Fatalf("expected error 'Something broke', got %q", d.Error)
	}
	if d.ExtensionPath != "/path/to/ext.ts" {
		t.Fatalf("expected path '/path/to/ext.ts', got %q", d.ExtensionPath)
	}
}

// --- edge cases ---

func TestMapResponseReturnsNil(t *testing.T) {
	events := MapRPCLine(loadFixture(t, "response.json"))
	if events != nil {
		t.Fatalf("expected nil for response, got %d events", len(events))
	}
}

func TestMapUnknownType(t *testing.T) {
	e := requireOne(t, MapRPCLine(loadFixture(t, "unknown_event.json")), "agent.unknown")
	d := dataAs[gateway.Unknown](t, e)
	if d.EventType != "some_future_event" {
		t.Fatalf("expected event_type 'some_future_event', got %q", d.EventType)
	}
	if len(d.Raw) == 0 {
		t.Fatal("expected non-empty raw")
	}
}

func TestMapInvalidJSON(t *testing.T) {
	events := MapRPCLine([]byte("not json at all"))
	if events != nil {
		t.Fatalf("expected nil for invalid JSON, got %d events", len(events))
	}
}

func TestMapEmptyLine(t *testing.T) {
	events := MapRPCLine([]byte(""))
	if events != nil {
		t.Fatalf("expected nil for empty line, got %d events", len(events))
	}
}
