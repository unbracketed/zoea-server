package rpc

import (
	"encoding/json"

	"github.com/unbracketed/zoea-server/internal/gateway"
)

// MapRPCLine takes a raw JSONL line from Pi's stdout and returns
// zero or more normalized gateway events.
// Returns nil for response lines (handled separately by command correlation).
func MapRPCLine(raw []byte) []gateway.Event {
	var env RPCEvent
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	env.Raw = raw

	// Responses are handled by command correlation, not event stream.
	if env.Type == "response" {
		return nil
	}

	switch env.Type {
	case "message_update":
		return mapMessageUpdate(raw)
	case "agent_start":
		return one("agent.run.start", gateway.RunStart{})
	case "agent_end":
		return mapAgentEnd(raw)
	case "turn_start":
		return one("agent.turn.start", gateway.TurnStart{})
	case "turn_end":
		return mapTurnEnd(raw)
	case "message_start":
		return mapMessageStart(raw)
	case "message_end":
		return mapMessageEnd(raw)
	case "tool_execution_start":
		return mapToolExecStart(raw)
	case "tool_execution_update":
		return mapToolExecUpdate(raw)
	case "tool_execution_end":
		return mapToolExecEnd(raw)
	case "compaction_start":
		return mapCompactionStart(raw)
	case "compaction_end":
		return mapCompactionEnd(raw)
	case "auto_retry_start":
		return mapRetryStart(raw)
	case "auto_retry_end":
		return mapRetryEnd(raw)
	case "queue_update":
		return mapQueueUpdate(raw)
	case "extension_ui_request":
		return mapExtensionUIRequest(raw)
	case "extension_error":
		return mapExtensionError(raw)
	default:
		return one("agent.unknown", gateway.Unknown{
			EventType: env.Type,
			Raw:       raw,
		})
	}
}

func mapMessageUpdate(raw []byte) []gateway.Event {
	var mu MessageUpdateEvent
	if err := json.Unmarshal(raw, &mu); err != nil {
		return nil
	}
	ame := mu.AssistantMessageEvent

	switch ame.Type {
	case "text_start":
		return one("agent.text.start", gateway.TextStart{
			ContentIndex: ame.ContentIndex,
			Message:      mu.Message,
			Partial:      ame.Partial,
		})
	case "text_delta":
		return one("agent.text.delta", gateway.TextDelta{
			ContentIndex: ame.ContentIndex,
			Delta:        ame.Delta,
			Message:      mu.Message,
			Partial:      ame.Partial,
		})
	case "text_end":
		return one("agent.text.end", gateway.TextEnd{
			ContentIndex: ame.ContentIndex,
			Content:      ame.Content,
			Message:      mu.Message,
			Partial:      ame.Partial,
		})
	case "thinking_start":
		return one("agent.thinking.start", gateway.ThinkingStart{
			ContentIndex: ame.ContentIndex,
			Message:      mu.Message,
			Partial:      ame.Partial,
		})
	case "thinking_delta":
		return one("agent.thinking.delta", gateway.ThinkingDelta{
			ContentIndex: ame.ContentIndex,
			Delta:        ame.Delta,
			Message:      mu.Message,
			Partial:      ame.Partial,
		})
	case "thinking_end":
		return one("agent.thinking.end", gateway.ThinkingEnd{
			ContentIndex: ame.ContentIndex,
			Message:      mu.Message,
			Partial:      ame.Partial,
		})
	case "toolcall_start":
		return one("agent.toolcall.start", gateway.ToolCallStart{
			ContentIndex: ame.ContentIndex,
			ToolName:     ame.ToolName,
			Message:      mu.Message,
			Partial:      ame.Partial,
		})
	case "toolcall_delta":
		return one("agent.toolcall.delta", gateway.ToolCallDelta{
			ContentIndex: ame.ContentIndex,
			Delta:        ame.Delta,
			Message:      mu.Message,
			Partial:      ame.Partial,
		})
	case "toolcall_end":
		return one("agent.toolcall.end", gateway.ToolCallEnd{
			ContentIndex: ame.ContentIndex,
			ToolCall:     ame.ToolCall,
			Message:      mu.Message,
			Partial:      ame.Partial,
		})
	case "done":
		return one("agent.message.done", gateway.MessageDone{
			Reason:  ame.Reason,
			Message: mu.Message,
			Partial: ame.Partial,
		})
	case "error":
		return one("agent.message.error", gateway.MessageError{
			Reason:  ame.Reason,
			Message: mu.Message,
			Partial: ame.Partial,
		})
	case "start":
		// "start" is the initial message generation signal; map it but it's low-value.
		return one("agent.message.start", gateway.MessageStart{Message: mu.Message})
	default:
		return one("agent.unknown", gateway.Unknown{
			EventType: "message_update." + ame.Type,
			Raw:       raw,
		})
	}
}

func mapMessageStart(raw []byte) []gateway.Event {
	var env struct {
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return one("agent.message.start", gateway.MessageStart{})
	}
	return one("agent.message.start", gateway.MessageStart{Message: env.Message})
}

func mapMessageEnd(raw []byte) []gateway.Event {
	var env struct {
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return one("agent.message.end", gateway.MessageEnd{})
	}
	return one("agent.message.end", gateway.MessageEnd{Message: env.Message})
}

func mapAgentEnd(raw []byte) []gateway.Event {
	var ae AgentEndEvent
	if err := json.Unmarshal(raw, &ae); err != nil {
		return one("agent.run.end", gateway.RunEnd{})
	}
	return one("agent.run.end", gateway.RunEnd{Messages: ae.Messages})
}

func mapTurnEnd(raw []byte) []gateway.Event {
	var te TurnEndEvent
	if err := json.Unmarshal(raw, &te); err != nil {
		return one("agent.turn.end", gateway.TurnEnd{})
	}
	return one("agent.turn.end", gateway.TurnEnd{
		Message:     te.Message,
		ToolResults: te.ToolResults,
	})
}

func mapToolExecStart(raw []byte) []gateway.Event {
	var e ToolExecStartEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil
	}
	return one("agent.tool.start", gateway.ToolExecStart{
		ToolCallID: e.ToolCallID,
		ToolName:   e.ToolName,
		Args:       e.Args,
	})
}

func mapToolExecUpdate(raw []byte) []gateway.Event {
	var e ToolExecUpdateEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil
	}
	return one("agent.tool.update", gateway.ToolExecUpdate{
		ToolCallID:    e.ToolCallID,
		ToolName:      e.ToolName,
		PartialResult: e.PartialResult,
	})
}

func mapToolExecEnd(raw []byte) []gateway.Event {
	var e ToolExecEndEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil
	}
	return one("agent.tool.end", gateway.ToolExecEnd{
		ToolCallID: e.ToolCallID,
		ToolName:   e.ToolName,
		Result:     e.Result,
		IsError:    e.IsError,
	})
}

func mapCompactionStart(raw []byte) []gateway.Event {
	var e CompactionStartEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return one("agent.compaction.start", gateway.CompactionStart{})
	}
	return one("agent.compaction.start", gateway.CompactionStart{Reason: e.Reason})
}

func mapCompactionEnd(raw []byte) []gateway.Event {
	var e CompactionEndEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return one("agent.compaction.end", gateway.CompactionEnd{})
	}
	return one("agent.compaction.end", gateway.CompactionEnd{
		Reason:    e.Reason,
		Aborted:   e.Aborted,
		WillRetry: e.WillRetry,
	})
}

func mapRetryStart(raw []byte) []gateway.Event {
	var e RetryStartEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil
	}
	return one("agent.retry.start", gateway.RetryStart{
		Attempt:     e.Attempt,
		MaxAttempts: e.MaxAttempts,
		DelayMs:     e.DelayMs,
		Error:       e.ErrorMessage,
	})
}

func mapRetryEnd(raw []byte) []gateway.Event {
	var e RetryEndEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil
	}
	return one("agent.retry.end", gateway.RetryEnd{
		Success:    e.Success,
		Attempt:    e.Attempt,
		FinalError: e.FinalError,
	})
}

func mapQueueUpdate(raw []byte) []gateway.Event {
	var e QueueUpdateEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil
	}
	steering := e.Steering
	if steering == nil {
		steering = []string{}
	}
	followUp := e.FollowUp
	if followUp == nil {
		followUp = []string{}
	}
	return one("agent.queue.update", gateway.QueueUpdate{
		Steering: steering,
		FollowUp: followUp,
	})
}

func mapExtensionUIRequest(raw []byte) []gateway.Event {
	var e ExtensionUIRequestEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil
	}
	return one("agent.ui.request", gateway.UIRequest{
		ID:      e.ID,
		Method:  e.Method,
		Payload: raw,
	})
}

func mapExtensionError(raw []byte) []gateway.Event {
	var e ExtensionErrorEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil
	}
	return one("agent.extension.error", gateway.ExtensionError{
		ExtensionPath: e.ExtensionPath,
		Event:         e.Event,
		Error:         e.Error,
	})
}

func one(eventType string, data any) []gateway.Event {
	return []gateway.Event{gateway.NewEvent(eventType, data)}
}
