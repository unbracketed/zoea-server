package process

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/unbracketed/zoea-server/internal/gateway"
)

// ErrA2UIUnsupported is returned by AgentHandle.SendA2UIAction when the
// runtime hasn't implemented native A2UI input yet. The HTTP/WS layer
// surfaces this as a clear error frame so the client can give up rather
// than wait for a response that will never come.
var ErrA2UIUnsupported = errors.New("a2ui: runtime does not yet support a2ui actions")

type StartOptions struct {
	SessionID  string
	UserID     string
	ProjectID  string
	WorkingDir string
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

// A2UIActionRequest carries an A2UI v0.9 client action plus the client's
// data model and capability declaration. The server forwards these to the
// runtime as opaque JSON envelopes; only the inbound HTTP/WS handler
// validates structure.
type A2UIActionRequest struct {
	Message            json.RawMessage `json:"message"`
	ClientDataModel    json.RawMessage `json:"client_data_model,omitempty"`
	ClientCapabilities json.RawMessage `json:"client_capabilities,omitempty"`
}

type AgentHandle interface {
	Prompt(ctx context.Context, req PromptRequest) error
	Abort(ctx context.Context) error
	GetState(ctx context.Context) (State, error)
	GetMessages(ctx context.Context) ([]Message, error)
	GetMessagesRaw(ctx context.Context) ([]json.RawMessage, error)
	Subscribe(ctx context.Context) (<-chan gateway.Event, func())
	SendUIResponse(ctx context.Context, resp UIResponse) error
	// SendA2UIAction forwards an A2UI v0.9 client action to the runtime. May
	// return ErrA2UIUnsupported when the runtime build doesn't yet accept
	// A2UI actions; callers should treat that as a soft failure.
	SendA2UIAction(ctx context.Context, req A2UIActionRequest) error
	// Broadcast pushes a synthetic event to all current subscribers. Used by
	// server-side bridges (e.g. the temporary A2UI injection endpoint) that
	// need to inject events into the existing WS stream without going through
	// the agent process.
	Broadcast(event gateway.Event)
	Close(ctx context.Context) error
}

type Manager interface {
	Start(ctx context.Context, opts StartOptions) (AgentHandle, error)
}
