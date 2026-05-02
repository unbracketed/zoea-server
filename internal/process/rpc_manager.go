package process

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/unbracketed/zoea-server/internal/gateway"
	"github.com/unbracketed/zoea-server/internal/rpc"
)

type RPCProcessManager struct {
	binPath           string
	baseArgs          []string
	sessionsBaseDir   string
	defaultWorkingDir string
}

func NewRPCProcessManager(binPath string, baseArgs []string, sessionsBaseDir string, defaultWorkingDir string) *RPCProcessManager {
	return &RPCProcessManager{
		binPath:           binPath,
		baseArgs:          append([]string{}, baseArgs...),
		sessionsBaseDir:   sessionsBaseDir,
		defaultWorkingDir: strings.TrimSpace(defaultWorkingDir),
	}
}

func (m *RPCProcessManager) Start(_ context.Context, opts StartOptions) (AgentHandle, error) {
	sessionDir := filepath.Join(m.sessionsBaseDir, opts.UserID, opts.SessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}
	absSessionDir, err := filepath.Abs(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("resolve session dir: %w", err)
	}

	workingDir := absSessionDir
	if m.defaultWorkingDir != "" {
		workingDir = m.defaultWorkingDir
	} else if strings.TrimSpace(opts.WorkingDir) != "" {
		workingDir = strings.TrimSpace(opts.WorkingDir)
	}
	workingDir, err = filepath.Abs(workingDir)
	if err != nil {
		return nil, fmt.Errorf("resolve working dir: %w", err)
	}
	info, err := os.Stat(workingDir)
	if err != nil {
		return nil, fmt.Errorf("stat working dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("working dir is not a directory: %s", workingDir)
	}

	args := withArgValue(append([]string{}, m.baseArgs...), "--session-dir", absSessionDir)

	cmd := exec.Command(m.binPath, args...)
	cmd.Dir = workingDir
	cmd.Env = os.Environ()
	_ = opts // session/user metadata is tracked via the agent handle, not env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start pi process: %w", err)
	}

	h := &rpcHandle{
		cmd:         cmd,
		stdin:       stdin,
		pending:     map[string]chan rpcEnvelope{},
		subscribers: map[uint64]chan gateway.Event{},
		done:        make(chan struct{}),
	}

	go h.readLoop(stdout)
	go h.readStderr(stderr)
	go h.waitLoop()

	return h, nil
}

func withArgValue(args []string, key, value string) []string {
	for i := 0; i < len(args); i++ {
		if args[i] != key {
			continue
		}
		if i+1 < len(args) {
			args[i+1] = value
			return args
		}
		return append(args, value)
	}
	return append(args, key, value)
}

type rpcEnvelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Command string          `json:"command,omitempty"`
	Success *bool           `json:"success,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type rpcHandle struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	writeMu sync.Mutex
	mu      sync.Mutex

	closed      bool
	nextID      uint64
	nextSubID   uint64
	pending     map[string]chan rpcEnvelope
	subscribers map[uint64]chan gateway.Event

	done chan struct{}
}

func (h *rpcHandle) Prompt(ctx context.Context, req PromptRequest) error {
	payload := map[string]any{
		"type":    "prompt",
		"message": req.Message,
	}
	if strings.TrimSpace(req.StreamingBehavior) != "" {
		payload["streamingBehavior"] = strings.TrimSpace(req.StreamingBehavior)
	}
	_, err := h.sendCommand(ctx, payload)
	return err
}

func (h *rpcHandle) Abort(ctx context.Context) error {
	_, err := h.sendCommand(ctx, map[string]any{"type": "abort"})
	return err
}

func (h *rpcHandle) GetState(ctx context.Context) (State, error) {
	resp, err := h.sendCommand(ctx, map[string]any{"type": "get_state"})
	if err != nil {
		return State{}, err
	}
	var data struct {
		IsStreaming   bool   `json:"isStreaming"`
		ThinkingLevel string `json:"thinkingLevel"`
		Model         *struct {
			ID string `json:"id"`
		} `json:"model"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return State{}, fmt.Errorf("decode get_state data: %w", err)
	}
	st := State{IsStreaming: data.IsStreaming, ThinkingLevel: data.ThinkingLevel}
	if data.Model != nil {
		st.Model = data.Model.ID
	}
	return st, nil
}

func (h *rpcHandle) GetMessages(ctx context.Context) ([]Message, error) {
	rawMsgs, err := h.getMessagesResponse(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(rawMsgs))
	for _, raw := range rawMsgs {
		var m struct {
			Role    string      `json:"role"`
			Content interface{} `json:"content"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		out = append(out, Message{Role: m.Role, Content: flattenContent(m.Content)})
	}
	return out, nil
}

func (h *rpcHandle) GetMessagesRaw(ctx context.Context) ([]json.RawMessage, error) {
	return h.getMessagesResponse(ctx)
}

func (h *rpcHandle) getMessagesResponse(ctx context.Context) ([]json.RawMessage, error) {
	resp, err := h.sendCommand(ctx, map[string]any{"type": "get_messages"})
	if err != nil {
		return nil, err
	}
	var data struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("decode get_messages data: %w", err)
	}
	return data.Messages, nil
}

func flattenContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			obj, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if t, ok := obj["text"].(string); ok {
				parts = append(parts, t)
				continue
			}
			if t, ok := obj["thinking"].(string); ok {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func (h *rpcHandle) SendUIResponse(_ context.Context, resp UIResponse) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return errors.New("agent handle is closed")
	}
	h.mu.Unlock()

	payload := map[string]any{
		"type": "extension_ui_response",
		"id":   resp.ID,
	}
	if resp.Cancelled {
		payload["cancelled"] = true
	} else if resp.Confirmed != nil {
		payload["confirmed"] = *resp.Confirmed
	} else if resp.Value != nil {
		payload["value"] = resp.Value
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal ui response: %w", err)
	}

	h.writeMu.Lock()
	_, err = h.stdin.Write(append(b, '\n'))
	h.writeMu.Unlock()
	return err
}

// SendA2UIAction forwards an A2UI v0.9 client action to the Pi runtime
// via the same JSON-line RPC protocol used for prompts.
//
// The current Pi runtime does not yet implement an "a2ui_action" handler,
// so a real send will surface as an error from sendCommand. We map any
// such error to ErrA2UIUnsupported so the HTTP/WS layer can return a
// stable "not supported" frame rather than leaking runtime-specific
// wording. Once Pi gains native support, the runtime will return
// success and this method becomes a transparent forwarder.
func (h *rpcHandle) SendA2UIAction(ctx context.Context, req A2UIActionRequest) error {
	payload := map[string]any{
		"type": "a2ui_action",
	}
	if len(req.Message) > 0 {
		payload["message"] = req.Message
	}
	if len(req.ClientDataModel) > 0 {
		payload["client_data_model"] = req.ClientDataModel
	}
	if len(req.ClientCapabilities) > 0 {
		payload["client_capabilities"] = req.ClientCapabilities
	}
	if _, err := h.sendCommand(ctx, payload); err != nil {
		return fmt.Errorf("%w: %v", ErrA2UIUnsupported, err)
	}
	return nil
}

func (h *rpcHandle) Subscribe(ctx context.Context) (<-chan gateway.Event, func()) {
	h.mu.Lock()
	h.nextSubID++
	id := h.nextSubID
	ch := make(chan gateway.Event, 128)
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
		select {
		case <-ctx.Done():
			unsubscribe()
		case <-h.done:
			unsubscribe()
		}
	}()

	return ch, unsubscribe
}

func (h *rpcHandle) Close(ctx context.Context) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()

	_ = h.cmd.Process.Signal(os.Interrupt)

	t := time.NewTimer(3 * time.Second)
	defer t.Stop()
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		_ = h.cmd.Process.Kill()
		return ctx.Err()
	case <-t.C:
		_ = h.cmd.Process.Kill()
		return nil
	}
}

func (h *rpcHandle) sendCommand(ctx context.Context, payload map[string]any) (rpcEnvelope, error) {
	id := atomic.AddUint64(&h.nextID, 1)
	idStr := fmt.Sprintf("req-%d", id)
	payload["id"] = idStr

	respCh := make(chan rpcEnvelope, 1)

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return rpcEnvelope{}, errors.New("agent handle is closed")
	}
	h.pending[idStr] = respCh
	h.mu.Unlock()

	b, err := json.Marshal(payload)
	if err != nil {
		h.removePending(idStr)
		return rpcEnvelope{}, fmt.Errorf("marshal command: %w", err)
	}

	h.writeMu.Lock()
	_, err = h.stdin.Write(append(b, '\n'))
	h.writeMu.Unlock()
	if err != nil {
		h.removePending(idStr)
		return rpcEnvelope{}, fmt.Errorf("write command: %w", err)
	}

	select {
	case <-ctx.Done():
		h.removePending(idStr)
		return rpcEnvelope{}, ctx.Err()
	case <-h.done:
		h.removePending(idStr)
		return rpcEnvelope{}, errors.New("pi process exited")
	case resp := <-respCh:
		if resp.Success != nil && !*resp.Success {
			if resp.Error != "" {
				return rpcEnvelope{}, errors.New(resp.Error)
			}
			return rpcEnvelope{}, errors.New("rpc command failed")
		}
		return resp, nil
	}
}

func (h *rpcHandle) removePending(id string) {
	h.mu.Lock()
	delete(h.pending, id)
	h.mu.Unlock()
}

func (h *rpcHandle) readLoop(stdout io.Reader) {
	r := bufio.NewReader(stdout)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			return
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		var env rpcEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}

		if env.Type == "response" {
			h.mu.Lock()
			ch, ok := h.pending[env.ID]
			if ok {
				delete(h.pending, env.ID)
			}
			h.mu.Unlock()
			if ok {
				ch <- env
				close(ch)
			}
			continue
		}

		for _, ge := range rpc.MapRPCLine([]byte(line)) {
			h.broadcastGatewayEvent(ge)
		}
	}
}

func (h *rpcHandle) readStderr(stderr io.Reader) {
	r := bufio.NewReader(stderr)
	for {
		_, err := r.ReadString('\n')
		if err != nil {
			return
		}
	}
}

func (h *rpcHandle) waitLoop() {
	_ = h.cmd.Wait()
	h.mu.Lock()
	for id, ch := range h.pending {
		delete(h.pending, id)
		close(ch)
	}
	for id, ch := range h.subscribers {
		delete(h.subscribers, id)
		close(ch)
	}
	h.closed = true
	h.mu.Unlock()
	close(h.done)
}

func (h *rpcHandle) broadcastGatewayEvent(e gateway.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subscribers {
		select {
		case ch <- e:
		default:
		}
	}
}

// Broadcast satisfies the AgentHandle interface and lets server-side bridges
// (e.g. the A2UI broker) inject synthetic events into the existing WS stream.
func (h *rpcHandle) Broadcast(e gateway.Event) {
	h.broadcastGatewayEvent(e)
}
