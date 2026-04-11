package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/julianshen/rubichan/internal/session"
	"github.com/julianshen/rubichan/internal/transport/bridge"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

func logBridgeError(sessionID, eventType string, err error) {
	log.Printf("ws: bridge publish error: session=%s type=%s err=%v", sessionID, eventType, err)
}

// AgentFactory creates a new agent for a session.
type AgentFactory func(sessionID string, opts SessionCreatePayload) (*agentsdk.Agent, error)

// Hub manages WebSocket clients and routes agent events to them.
// It implements session.EventSink and agentsdk.UIRequestHandler.
type Hub struct {
	sessions  map[string]*SessionState
	clients   map[*Client]*SessionState
	bridges   []EventBridge
	bufferCap int

	agentFactory AgentFactory

	mu sync.RWMutex
}

// SessionState tracks one agent session.
type SessionState struct {
	ID      string
	Agent   *agentsdk.Agent
	Clients map[*Client]struct{}
	Buffer  *RingBuffer
	seq     atomic.Int64
	uiWait  map[string]chan agentsdk.UIResponse
	cancel  context.CancelFunc
	mu      sync.Mutex
}

// EventBridge is an alias for bridge.EventBridge.
// Bridges registered with the Hub receive all session events.
type EventBridge = bridge.EventBridge

// HubConfig configures the Hub.
type HubConfig struct {
	ReconnectBufferSize int
	AgentFactory        AgentFactory
}

// NewHub creates a new Hub.
func NewHub(cfg HubConfig) *Hub {
	if cfg.ReconnectBufferSize <= 0 {
		cfg.ReconnectBufferSize = 1000
	}
	return &Hub{
		sessions:     make(map[string]*SessionState),
		clients:      make(map[*Client]*SessionState),
		bufferCap:    cfg.ReconnectBufferSize,
		agentFactory: cfg.AgentFactory,
	}
}

// AddBridge registers an event bridge.
func (h *Hub) AddBridge(b EventBridge) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bridges = append(h.bridges, b)
}

// registerClient adds a client to a session.
func (h *Hub) registerClient(c *Client, sessionID string) (*SessionState, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ss, ok := h.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}

	ss.mu.Lock()
	ss.Clients[c] = struct{}{}
	ss.mu.Unlock()

	h.clients[c] = ss
	return ss, nil
}

// snapshotAndRegisterClientResult holds the buffered events and the session state.
type snapshotAndRegisterClientResult struct {
	entries []BufferEntry
	session *SessionState
}

// snapshotAndRegisterClient atomically snapshots the buffer and registers a client.
// This prevents the race where events broadcast between snapshot and register are missed.
// Lock order: h.mu → ss.mu (matches other code paths).
func (h *Hub) snapshotAndRegisterClient(c *Client, sessionID string, lastSeq int64) (*snapshotAndRegisterClientResult, error) {
	// Look up session under h.mu, then release immediately.
	h.mu.Lock()
	ss, ok := h.sessions[sessionID]
	h.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	// Snapshot buffer while holding ss.mu.
	entries := ss.Buffer.Since(lastSeq)

	// Check for gap.
	if lastSeq > 0 && len(entries) == 0 && ss.Buffer.Len() > 0 && ss.Buffer.OldestSeq() > lastSeq {
		return nil, fmt.Errorf("reconnect buffer does not contain events since sequence %d", lastSeq)
	}

	// Register client while still holding ss.mu to prevent live broadcasts
	// from arriving before registration.
	ss.Clients[c] = struct{}{}

	// Register in hub's client map. Must acquire h.mu again.
	h.mu.Lock()
	h.clients[c] = ss
	h.mu.Unlock()

	return &snapshotAndRegisterClientResult{entries: entries, session: ss}, nil
}

// unregisterClient removes a client from its session.
func (h *Hub) unregisterClient(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ss, ok := h.clients[c]
	if !ok {
		return
	}

	ss.mu.Lock()
	delete(ss.Clients, c)
	ss.mu.Unlock()

	delete(h.clients, c)
}

// CreateSession creates a new agent session.
func (h *Hub) CreateSession(ctx context.Context, id string, opts SessionCreatePayload, bufferSize int) (*SessionState, error) {
	if bufferSize <= 0 {
		bufferSize = h.bufferCap
	}
	if bufferSize <= 0 {
		bufferSize = 1000
	}

	var agent *agentsdk.Agent
	if h.agentFactory != nil {
		var err error
		agent, err = h.agentFactory(id, opts)
		if err != nil {
			return nil, fmt.Errorf("create agent: %w", err)
		}
	}

	ss := &SessionState{
		ID:      id,
		Agent:   agent,
		Clients: make(map[*Client]struct{}),
		Buffer:  NewRingBuffer(bufferSize),
		uiWait:  make(map[string]chan agentsdk.UIResponse),
	}

	h.mu.Lock()
	h.sessions[id] = ss
	h.mu.Unlock()

	return ss, nil
}

// GetSession returns a session by ID.
func (h *Hub) GetSession(id string) (*SessionState, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ss, ok := h.sessions[id]
	return ss, ok
}

// ListSessions returns all session IDs and their statuses.
func (h *Hub) ListSessions() []SessionInfoPayload {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]SessionInfoPayload, 0, len(h.sessions))
	for _, ss := range h.sessions {
		result = append(result, SessionInfoPayload{
			SessionID: ss.ID,
			Status:    "active",
		})
	}
	return result
}

// broadcastToSession sends an envelope to all clients of a session and to bridges.
// The caller must NOT hold h.mu — this method acquires h.mu.RLock internally.
func (h *Hub) broadcastToSession(ss *SessionState, env Envelope) {
	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("ws: failed to marshal broadcast envelope: session=%s type=%s err=%v", ss.ID, env.Type, err)
		return
	}

	// Buffer for reconnection.
	ss.Buffer.Push(BufferEntry{Seq: env.Seq, Payload: data})

	// Snapshot clients under lock, then send outside lock to avoid
	// holding the lock during I/O (c.Send may call c.close on slow clients).
	ss.mu.Lock()
	clients := make([]*Client, 0, len(ss.Clients))
	for c := range ss.Clients {
		clients = append(clients, c)
	}
	ss.mu.Unlock()

	for _, c := range clients {
		c.Send(data)
	}

	// Snapshot bridges under lock, then publish outside lock.
	h.mu.RLock()
	bridges := make([]EventBridge, len(h.bridges))
	copy(bridges, h.bridges)
	h.mu.RUnlock()

	bridgeEnv := bridge.Envelope{
		Type:      env.Type,
		SessionID: env.SessionID,
		Seq:       env.Seq,
		Timestamp: env.Timestamp,
		RequestID: env.RequestID,
		Payload:   env.Payload,
	}
	for _, b := range bridges {
		if err := b.Publish(context.Background(), ss.ID, bridgeEnv); err != nil {
			// Log bridge errors rather than silently dropping them.
			logBridgeError(ss.ID, env.Type, err)
		}
	}
}

// BroadcastTurnEvent converts a TurnEvent to an envelope and broadcasts it.
func (h *Hub) BroadcastTurnEvent(ss *SessionState, evt agentsdk.TurnEvent) {
	seq := ss.seq.Add(1)
	payload, err := marshalTurnEvent(evt)
	if err != nil {
		log.Printf("ws: failed to marshal turn event: session=%s type=%s err=%v", ss.ID, evt.Type, err)
		return
	}
	env := Envelope{
		Type:      TypeTurnEvent,
		SessionID: ss.ID,
		Seq:       seq,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	h.broadcastToSession(ss, env)
}

// Emit implements session.EventSink — broadcasts session events to all clients.
// If the event carries a SessionID, it is broadcast only to that session.
// Otherwise, it is broadcast to all sessions.
func (h *Hub) Emit(evt session.Event) {
	payload, err := json.Marshal(evt)
	if err != nil {
		log.Printf("ws: failed to marshal session event: type=%s err=%v", evt.Type, err)
		return
	}

	// If event has a session ID, broadcast only to that session.
	// SessionID is normalized by WithSessionID() so no need to trim here.
	if evt.SessionID != "" {
		ss, ok := h.GetSession(evt.SessionID)
		if !ok {
			log.Printf("ws: Emit: session %q not found, dropping event", evt.SessionID)
			return
		}
		seq := ss.seq.Add(1)
		env := Envelope{
			Type:      TypeEvent,
			SessionID: ss.ID,
			Seq:       seq,
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		}
		h.broadcastToSession(ss, env)
		return
	}

	// Fallback: broadcast to all sessions (for backward compatibility).
	h.mu.RLock()
	sessions := make([]*SessionState, 0, len(h.sessions))
	for _, ss := range h.sessions {
		sessions = append(sessions, ss)
	}
	h.mu.RUnlock()

	for _, ss := range sessions {
		seq := ss.seq.Add(1)
		env := Envelope{
			Type:      TypeEvent,
			SessionID: ss.ID,
			Seq:       seq,
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		}
		h.broadcastToSession(ss, env)
	}
}

// SessionEmitter returns an EventSink bound to a specific session.
func (h *Hub) SessionEmitter(sessionID string) session.EventSink {
	return session.SinkFunc(func(evt session.Event) {
		ss, ok := h.GetSession(sessionID)
		if !ok {
			log.Printf("ws: SessionEmitter: session %q not found, dropping event", sessionID)
			return
		}
		payload, err := json.Marshal(evt)
		if err != nil {
			log.Printf("ws: SessionEmitter: marshal error for session %q: %v", sessionID, err)
			return
		}
		seq := ss.seq.Add(1)
		env := Envelope{
			Type:      TypeEvent,
			SessionID: ss.ID,
			Seq:       seq,
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		}
		h.broadcastToSession(ss, env)
	})
}

// sessionIDKey is the context key for passing session IDs to Hub.Request.
type sessionIDKey struct{}

// ContextWithSessionID returns a new context carrying the given session ID.
// Pass this context to Agent.Turn so that Hub.Request can route UI requests
// to the correct session.
func ContextWithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, id)
}

// Request implements agentsdk.UIRequestHandler — forwards UIRequest to WebSocket
// clients and blocks until a UIResponse arrives.
// The context must carry a session ID (via ContextWithSessionID) so the request
// is routed to the correct session. Falls back to first session with clients
// only when a single session exists.
func (h *Hub) Request(ctx context.Context, req agentsdk.UIRequest) (agentsdk.UIResponse, error) {
	// Try to extract session ID from context.
	if id, ok := ctx.Value(sessionIDKey{}).(string); ok {
		ss, found := h.GetSession(id)
		if !found {
			return agentsdk.UIResponse{}, fmt.Errorf("session %q not found", id)
		}
		return h.requestFromSession(ctx, ss, req)
	}

	// Fallback: find the single session with connected clients.
	// Snapshot sessions under h.mu.RLock, then check client counts outside
	// to avoid nested lock acquisition (h.mu -> ss.mu).
	h.mu.RLock()
	allSessions := make([]*SessionState, 0, len(h.sessions))
	for _, ss := range h.sessions {
		allSessions = append(allSessions, ss)
	}
	h.mu.RUnlock()

	var targetSession *SessionState
	for _, ss := range allSessions {
		ss.mu.Lock()
		hasClients := len(ss.Clients) > 0
		ss.mu.Unlock()
		if hasClients {
			if targetSession != nil {
				return agentsdk.UIResponse{}, fmt.Errorf("multiple sessions active; use ContextWithSessionID to specify target")
			}
			targetSession = ss
		}
	}

	if targetSession == nil {
		return agentsdk.UIResponse{}, fmt.Errorf("no connected clients for UI request")
	}

	return h.requestFromSession(ctx, targetSession, req)
}

// SessionUIHandler returns a UIRequestHandler bound to a specific session.
func (h *Hub) SessionUIHandler(sessionID string) agentsdk.UIRequestHandler {
	return agentsdk.UIRequestFunc(func(ctx context.Context, req agentsdk.UIRequest) (agentsdk.UIResponse, error) {
		ss, ok := h.GetSession(sessionID)
		if !ok {
			return agentsdk.UIResponse{}, fmt.Errorf("session %q not found", sessionID)
		}
		return h.requestFromSession(ctx, ss, req)
	})
}

// requestFromSession sends a UIRequest to a session's clients and waits for a response.
func (h *Hub) requestFromSession(ctx context.Context, ss *SessionState, req agentsdk.UIRequest) (agentsdk.UIResponse, error) {
	// Create a response channel.
	responseCh := make(chan agentsdk.UIResponse, 1)
	ss.mu.Lock()
	ss.uiWait[req.ID] = responseCh
	ss.mu.Unlock()

	defer func() {
		ss.mu.Lock()
		delete(ss.uiWait, req.ID)
		ss.mu.Unlock()
	}()

	// Broadcast UIRequest to all connected clients.
	payload, err := json.Marshal(req)
	if err != nil {
		return agentsdk.UIResponse{}, fmt.Errorf("marshal UIRequest: %w", err)
	}
	seq := ss.seq.Add(1)
	env := Envelope{
		Type:      TypeUIRequest,
		SessionID: ss.ID,
		Seq:       seq,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	h.broadcastToSession(ss, env)

	// Wait for response or cancellation.
	select {
	case resp := <-responseCh:
		return resp, nil
	case <-ctx.Done():
		// Context canceled. The defer will clean up ss.uiWait[req.ID], preventing
		// goroutine leaks if a response arrives after timeout.
		// The buffered channel allows handleUIResponse to send without blocking.
		log.Printf("ws: UI request timeout: id=%s session=%s", req.ID, ss.ID)
		return agentsdk.UIResponse{}, ctx.Err()
	}
}

// handleClientMessage processes an incoming message from a WebSocket client.
func (h *Hub) handleClientMessage(c *Client, env Envelope) {
	switch env.Type {
	case TypeUserMessage:
		h.handleUserMessage(c, env)
	case TypeUIResponse:
		h.handleUIResponse(c, env)
	case TypeSessionCreate:
		h.handleSessionCreate(c, env)
	case TypeSessionResume:
		h.handleSessionResume(c, env)
	case TypeSessionList:
		h.handleSessionList(c)
	case TypeCancel:
		h.handleCancel(c, env)
	case TypePing:
		h.handlePing(c)
	default:
		h.sendError(c, "unknown_type", fmt.Sprintf("unknown message type: %q", env.Type))
	}
}

func (h *Hub) handleUserMessage(c *Client, env Envelope) {
	var payload UserMessagePayload
	if err := env.ParsePayload(&payload); err != nil {
		h.sendError(c, "invalid_payload", "failed to parse user_message payload")
		return
	}
	if payload.Text == "" {
		h.sendError(c, "empty_message", "message text cannot be empty")
		return
	}

	h.mu.RLock()
	ss := h.clients[c]
	h.mu.RUnlock()

	if ss == nil {
		h.sendError(c, "no_session", "client is not attached to a session")
		return
	}

	if ss.Agent == nil {
		h.sendError(c, "no_agent", "session has no agent configured")
		return
	}

	ctx, cancel := context.WithCancel(ContextWithSessionID(context.Background(), ss.ID))
	ss.mu.Lock()
	if ss.cancel != nil {
		ss.cancel() // cancel any in-progress turn
	}
	ss.cancel = cancel
	ss.mu.Unlock()

	turnCh, err := ss.Agent.Turn(ctx, payload.Text)
	if err != nil {
		cancel()
		h.sendError(c, "turn_error", err.Error())
		return
	}

	// Consume turn events in a goroutine and broadcast.
	go func() {
		defer cancel()
		for evt := range turnCh {
			h.BroadcastTurnEvent(ss, evt)
		}
	}()
}

func (h *Hub) handleUIResponse(c *Client, env Envelope) {
	var resp agentsdk.UIResponse
	if err := env.ParsePayload(&resp); err != nil {
		h.sendError(c, "invalid_payload", "failed to parse ui_response payload")
		return
	}

	h.mu.RLock()
	ss := h.clients[c]
	h.mu.RUnlock()

	if ss == nil {
		return
	}

	ss.mu.Lock()
	ch, ok := ss.uiWait[resp.RequestID]
	ss.mu.Unlock()

	if !ok {
		log.Printf("ws: received UIResponse for unknown request: id=%s", resp.RequestID)
		return
	}
	select {
	case ch <- resp:
	default:
		log.Printf("ws: duplicate UIResponse for request: id=%s (ignoring)", resp.RequestID)
	}
}

func (h *Hub) handleSessionCreate(c *Client, env Envelope) {
	var payload SessionCreatePayload
	if err := env.ParsePayload(&payload); err != nil {
		h.sendError(c, "invalid_payload", "failed to parse session_create payload")
		return
	}

	id := "ws-" + uuid.NewString()
	ss, err := h.CreateSession(context.Background(), id, payload, 0)
	if err != nil {
		h.sendError(c, "create_failed", err.Error())
		return
	}

	if _, err := h.registerClient(c, id); err != nil {
		h.sendError(c, "register_failed", err.Error())
		return
	}

	info := SessionInfoPayload{SessionID: ss.ID, Status: "active", Model: payload.Model}
	h.sendEnvelope(c, TypeSessionInfo, ss.ID, info)
}

func (h *Hub) handleSessionResume(c *Client, env Envelope) {
	var payload SessionResumePayload
	if err := env.ParsePayload(&payload); err != nil {
		h.sendError(c, "invalid_payload", "failed to parse session_resume payload")
		return
	}

	// Atomically snapshot buffer and register the client.
	// This eliminates the race where events broadcast between snapshot and register are missed.
	result, err := h.snapshotAndRegisterClient(c, payload.SessionID, payload.LastSeq)
	if err != nil {
		h.sendError(c, "resume_failed", err.Error())
		return
	}

	// Send session info.
	info := SessionInfoPayload{SessionID: result.session.ID, Status: "active"}
	h.sendEnvelope(c, TypeSessionInfo, result.session.ID, info)

	// Replay buffered events. These are already in c.send before any
	// live broadcast can arrive (registration happened within ss.mu lock).
	for _, entry := range result.entries {
		c.Send(entry.Payload)
	}
}

func (h *Hub) handleSessionList(c *Client) {
	sessions := h.ListSessions()
	payload, err := json.Marshal(sessions)
	if err != nil {
		h.sendError(c, "marshal_error", "failed to serialize session list")
		return
	}
	env := Envelope{
		Type:      TypeSessionListResult,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	data, err := json.Marshal(env)
	if err != nil {
		h.sendError(c, "marshal_error", "failed to serialize envelope")
		return
	}
	c.Send(data)
}

func (h *Hub) handleCancel(c *Client, _ Envelope) {
	h.mu.RLock()
	ss := h.clients[c]
	h.mu.RUnlock()

	if ss == nil {
		h.sendError(c, "no_session", "cannot cancel: not attached to a session")
		return
	}

	ss.mu.Lock()
	if ss.cancel != nil {
		ss.cancel()
		ss.cancel = nil
	}
	ss.mu.Unlock()
}

func (h *Hub) handlePing(c *Client) {
	env := Envelope{
		Type:      TypePong,
		Timestamp: time.Now().UTC(),
	}
	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("ws: handlePing: marshal failed: %v", err)
		return
	}
	c.Send(data)
}

func (h *Hub) sendError(c *Client, code, message string) {
	h.sendEnvelope(c, TypeError, "", ErrorPayload{Code: code, Message: message})
}

func (h *Hub) sendEnvelope(c *Client, msgType, sessionID string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("ws: sendEnvelope: marshal payload failed: type=%s err=%v", msgType, err)
		return
	}
	env := Envelope{
		Type:      msgType,
		SessionID: sessionID,
		Timestamp: time.Now().UTC(),
		Payload:   raw,
	}
	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("ws: sendEnvelope: marshal envelope failed: type=%s err=%v", msgType, err)
		return
	}
	c.Send(data)
}

// marshalTurnEvent serializes a TurnEvent for wire transmission.
// The error field is converted to a string since errors aren't JSON-serializable.
type wireTurnEvent struct {
	Type         string                    `json:"type"`
	Text         string                    `json:"text,omitempty"`
	ToolCall     *agentsdk.ToolCallEvent   `json:"tool_call,omitempty"`
	ToolResult   *agentsdk.ToolResultEvent `json:"tool_result,omitempty"`
	ToolProgress *wireToolProgress         `json:"tool_progress,omitempty"`
	UIRequest    *agentsdk.UIRequest       `json:"ui_request,omitempty"`
	UIUpdate     *agentsdk.UIUpdate        `json:"ui_update,omitempty"`
	UIResponse   *agentsdk.UIResponse      `json:"ui_response,omitempty"`
	Error        string                    `json:"error,omitempty"`
	InputTokens  int                       `json:"input_tokens,omitempty"`
	OutputTokens int                       `json:"output_tokens,omitempty"`
	DiffSummary  string                    `json:"diff_summary,omitempty"`
}

type wireToolProgress struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Stage   string `json:"stage"`
	Content string `json:"content,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

func marshalTurnEvent(evt agentsdk.TurnEvent) (json.RawMessage, error) {
	w := wireTurnEvent{
		Type:         evt.Type,
		Text:         evt.Text,
		ToolCall:     evt.ToolCall,
		ToolResult:   evt.ToolResult,
		UIRequest:    evt.UIRequest,
		UIUpdate:     evt.UIUpdate,
		UIResponse:   evt.UIResponse,
		InputTokens:  evt.InputTokens,
		OutputTokens: evt.OutputTokens,
		DiffSummary:  evt.DiffSummary,
	}
	if evt.Error != nil {
		w.Error = evt.Error.Error()
	}
	if evt.ToolProgress != nil {
		w.ToolProgress = &wireToolProgress{
			ID:      evt.ToolProgress.ID,
			Name:    evt.ToolProgress.Name,
			Stage:   evt.ToolProgress.Stage.String(),
			Content: evt.ToolProgress.Content,
			IsError: evt.ToolProgress.IsError,
		}
	}
	return json.Marshal(w)
}

// Close shuts down all sessions, bridges, and client connections.
// Safe to call multiple times.
func (h *Hub) Close() error {
	h.mu.Lock()

	// Cancel all active sessions.
	for _, ss := range h.sessions {
		ss.mu.Lock()
		if ss.cancel != nil {
			ss.cancel()
			ss.cancel = nil
		}
		ss.mu.Unlock()
	}

	// Close bridges.
	for _, b := range h.bridges {
		b.Close()
	}
	h.bridges = nil

	// Collect clients to close, then close outside lock.
	clientsToClose := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clientsToClose = append(clientsToClose, c)
	}

	// Clear maps so late-arriving goroutines see empty state.
	clear(h.sessions)
	clear(h.clients)
	h.mu.Unlock()

	for _, c := range clientsToClose {
		c.close()
	}
	return nil
}
