package process

import (
	"context"
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
