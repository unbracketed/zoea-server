package process

import (
	"context"
	"encoding/json"

	"github.com/unbracketed/zoea-server/internal/gateway"
)

type StartOptions struct {
	SessionID string
	UserID    string
	ProjectID string
}

type PromptRequest struct {
	Message           string
	StreamingBehavior string
}

type State struct {
	IsStreaming   bool   `json:"is_streaming"`
	Model         string `json:"model,omitempty"`
	ThinkingLevel string `json:"thinking_level,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// UIResponse is sent back to Pi for extension_ui_request dialog methods.
type UIResponse struct {
	ID        string `json:"id"`
	Value     any    `json:"value,omitempty"`
	Confirmed *bool  `json:"confirmed,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

type AgentHandle interface {
	Prompt(ctx context.Context, req PromptRequest) error
	Abort(ctx context.Context) error
	GetState(ctx context.Context) (State, error)
	GetMessages(ctx context.Context) ([]Message, error)
	GetMessagesRaw(ctx context.Context) ([]json.RawMessage, error)
	Subscribe(ctx context.Context) (<-chan gateway.Event, func())
	SendUIResponse(ctx context.Context, resp UIResponse) error
	// Broadcast pushes a synthetic event to all current subscribers. Used by
	// server-side bridges (e.g. Glimpse) that need to inject events into the
	// existing WS stream without going through the agent process.
	Broadcast(event gateway.Event)
	Close(ctx context.Context) error
}

type Manager interface {
	Start(ctx context.Context, opts StartOptions) (AgentHandle, error)
}
