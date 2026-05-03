package process

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/unbracketed/zoea-server/internal/gateway"
)

func NewNoopProcessManager() Manager {
	return &noopManager{}
}

type noopManager struct{}

func (m *noopManager) Start(_ context.Context, _ StartOptions) (AgentHandle, error) {
	return &noopHandle{subscribers: map[uint64]chan gateway.Event{}}, nil
}

func (m *noopManager) ResolveWorkingDir(opts StartOptions) (string, error) {
	return opts.WorkingDir, nil
}

type noopHandle struct {
	mu          sync.Mutex
	messages    []Message
	state       State
	subID       uint64
	subscribers map[uint64]chan gateway.Event
}

func (h *noopHandle) Prompt(_ context.Context, req PromptRequest) error {
	h.mu.Lock()
	h.messages = append(h.messages,
		Message{Role: "user", Content: req.Message},
		Message{Role: "assistant", Content: "[scaffold] prompt accepted: " + req.Message},
	)
	h.state.IsStreaming = false
	h.broadcastLocked(gateway.NewEvent("agent.run.start", gateway.RunStart{}))
	h.broadcastLocked(gateway.NewEvent("agent.text.delta", gateway.TextDelta{Delta: "[scaffold] prompt accepted"}))
	h.broadcastLocked(gateway.NewEvent("agent.run.end", gateway.RunEnd{}))
	h.mu.Unlock()
	return nil
}

func (h *noopHandle) Abort(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state.IsStreaming = false
	return nil
}

func (h *noopHandle) GetState(_ context.Context) (State, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state, nil
}

func (h *noopHandle) GetMessages(_ context.Context) ([]Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Message, len(h.messages))
	copy(out, h.messages)
	return out, nil
}

func (h *noopHandle) GetMessagesRaw(_ context.Context) ([]json.RawMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]json.RawMessage, 0, len(h.messages))
	for _, m := range h.messages {
		b, err := json.Marshal(map[string]any{
			"role":    m.Role,
			"content": []map[string]any{{"type": "text", "text": m.Content}},
		})
		if err != nil {
			continue
		}
		out = append(out, b)
	}
	return out, nil
}

func (h *noopHandle) Subscribe(ctx context.Context) (<-chan gateway.Event, func()) {
	h.mu.Lock()
	h.subID++
	id := h.subID
	ch := make(chan gateway.Event, 32)
	h.subscribers[id] = ch
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		if existing, ok := h.subscribers[id]; ok {
			delete(h.subscribers, id)
			close(existing)
		}
		h.mu.Unlock()
	}

	go func() {
		<-ctx.Done()
		unsubscribe()
	}()

	return ch, unsubscribe
}

func (h *noopHandle) SendUIResponse(_ context.Context, _ UIResponse) error {
	return nil
}

// SendA2UIAction on the noop manager records the action by broadcasting a
// synthetic event so tests can observe it, then returns nil. Production
// runtimes will instead forward to a real Pi-side handler.
func (h *noopHandle) SendA2UIAction(_ context.Context, req A2UIActionRequest) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.broadcastLocked(gateway.NewEvent("agent.a2ui.action_received", req))
	return nil
}

func (h *noopHandle) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, ch := range h.subscribers {
		delete(h.subscribers, id)
		close(ch)
	}
	return nil
}

func (h *noopHandle) broadcastLocked(e gateway.Event) {
	for _, ch := range h.subscribers {
		select {
		case ch <- e:
		default:
		}
	}
}

// Broadcast satisfies the AgentHandle interface for synthetic events from
// server-side bridges.
func (h *noopHandle) Broadcast(e gateway.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.broadcastLocked(e)
}
