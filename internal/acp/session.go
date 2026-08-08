package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
)

// StopReason tells the client why a turn ended. ACP closes session/prompt with
// exactly one of these; there is no "unknown".
type StopReason string

const (
	StopEndTurn         StopReason = "end_turn"
	StopMaxTokens       StopReason = "max_tokens"
	StopMaxTurnRequests StopReason = "max_turn_requests"
	StopRefusal         StopReason = "refusal"
	StopCancelled       StopReason = "cancelled"
)

// PromptRequest is one turn's worth of work, handed to the agent.
//
// Cwd travels with it rather than being looked up by the handler because the
// session owns it: a turn is executed against the directory its session was
// opened for, and passing it explicitly keeps that from being re-derived
// somewhere it could disagree.
type PromptRequest struct {
	SessionID string
	Cwd       string
	Prompt    []ContentBlock
}

// PromptFunc executes a turn and reports why it stopped.
//
// An error means the agent failed, which is distinct from a turn that ended for
// a protocol reason: StopRefusal says the model declined, an error says the
// attempt broke. Conflating them tells the client a lie in whichever direction
// it is collapsed.
type PromptFunc func(ctx context.Context, req PromptRequest) (StopReason, error)

// SessionConfig supplies what the session methods need from the agent.
type SessionConfig struct {
	// Capabilities is the same value the handshake answers with. It is shared
	// rather than restated so the content this agent accepts cannot drift from
	// the content it claims to accept.
	Capabilities AgentCapabilities

	// WorkingDir is the only directory this agent can serve a session for. The
	// agent freezes its working directory at construction, so a session opened
	// against anything else would run every file and shell tool somewhere the
	// client did not ask for.
	//
	// An empty value therefore refuses every session/new rather than accepting
	// all of them: it means the caller never said which directory it serves,
	// and guessing is what this field exists to prevent.
	WorkingDir string

	// Handshake gates the session methods on initialize having completed. A
	// nil value disables the check, which is what a registry serving no
	// handshake wants.
	Handshake *Handshake

	// Prompt runs a turn. A nil Prompt means session/prompt is unserved, and
	// the method reports that rather than pretending to have run.
	Prompt PromptFunc
}

// NewSessionParams is the client's request to open a session.
type NewSessionParams struct {
	Cwd string `json:"cwd"`
	// MCPServers is decoded as a slice, not a raw blob, so that its length is
	// the number of servers requested. Measuring a json.RawMessage instead
	// measures bytes, and the empty list "[]" is two of them.
	MCPServers            []json.RawMessage `json:"mcpServers,omitempty"`
	AdditionalDirectories []string          `json:"additionalDirectories,omitempty"`
}

// NewSessionResult identifies the session the client may now prompt.
type NewSessionResult struct {
	SessionID string `json:"sessionId"`
}

// PromptParams is one session/prompt call.
type PromptParams struct {
	SessionID string          `json:"sessionId"`
	Prompt    json.RawMessage `json:"prompt"`
}

// PromptResult closes a turn.
type PromptResult struct {
	StopReason StopReason `json:"stopReason"`
}

// session is the state a sessionId stands for.
type session struct {
	cwd string
	// active marks a turn in flight. ACP allows one turn per session: a second
	// would interleave its session/update stream with the first's and append to
	// the same conversation in an order neither client asked for.
	active bool
}

// sessionStore holds live sessions. Sessions are reached from handler
// goroutines — Conn serves each inbound request on its own — so every access is
// guarded.
type sessionStore struct {
	mu sync.RWMutex
	// claimed marks the single session slot as taken, including the window
	// between reserving it and the session actually existing.
	claimed  bool
	sessions map[string]session
}

func (s *sessionStore) add(id string, sess session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = sess
}

// claimSole reserves the single session this agent can host, or reports that it
// is already taken. Checking and claiming happen under one lock: two
// session/new calls arriving together would otherwise both observe an empty
// store and both succeed.
//
// The claim is a flag rather than a placeholder entry in the map, so a lookup
// can never find a session that was only reserved.
func (s *sessionStore) claimSole() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimed {
		return fmt.Errorf("%w: this agent hosts one session at a time and already has one; close it or open a second connection",
			ErrInvalidParams)
	}
	s.claimed = true
	return nil
}

// releaseSole undoes a claim whose session was never created. Without it, a
// failure between claiming and adding would leave the connection permanently
// unable to open the session it is allowed.
func (s *sessionStore) releaseSole() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimed = false
}

// beginTurn marks a session busy, or reports that it already is. Test and set
// happen under one lock so two prompts arriving together cannot both find the
// session idle.
func (s *sessionStore) beginTurn(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return fmt.Errorf("%w: unknown sessionId %q", ErrInvalidParams, id)
	}
	if sess.active {
		return fmt.Errorf("%w: session %q already has an active turn; wait for it to finish or cancel it", ErrInvalidParams, id)
	}
	sess.active = true
	s.sessions[id] = sess
	return nil
}

// endTurn releases a session for the next prompt. Deferred by the handler, so a
// turn that fails or panics does not leave the session permanently busy.
func (s *sessionStore) endTurn(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.active = false
		s.sessions[id] = sess
	}
}

func (s *sessionStore) get(id string) (session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

// RegisterSession wires session/new and session/prompt into a registry.
func RegisterSession(registry *CapabilityRegistry, cfg SessionConfig) {
	store := &sessionStore{sessions: make(map[string]session)}

	registry.RegisterMethod(MethodSessionNew, func(params json.RawMessage) (json.RawMessage, error) {
		if !cfg.Handshake.IsComplete() {
			return nil, fmt.Errorf("session/new: %w: initialize must complete first", ErrInvalidParams)
		}
		var req NewSessionParams
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("session/new: %w: %v", ErrInvalidParams, err)
		}
		// The spec requires an absolute cwd. A relative one resolves against
		// two different working directories on the two sides of the connection,
		// so it names two different places while looking like agreement.
		if req.Cwd == "" {
			return nil, fmt.Errorf("session/new: %w: cwd is required", ErrInvalidParams)
		}
		if !filepath.IsAbs(req.Cwd) {
			return nil, fmt.Errorf("session/new: %w: cwd must be an absolute path, got %q", ErrInvalidParams, req.Cwd)
		}

		// Both of these ask the agent to do something it has no implementation
		// for. Taking them and doing nothing would leave the client prompting in
		// the belief that its MCP tools are connected and its extra directories
		// are readable — the same silence as dropping an undeclared content
		// block, and just as invisible from the far side.
		//
		// Empty is not a request, so an empty list is accepted: a client that
		// always sends "mcpServers":[] is asking for nothing.
		if len(req.MCPServers) > 0 {
			return nil, fmt.Errorf("session/new: %w: mcpServers is not supported; this agent connects no MCP servers on a client's behalf", ErrInvalidParams)
		}
		if len(req.AdditionalDirectories) > 0 {
			return nil, fmt.Errorf("session/new: %w: additionalDirectories is not supported; this agent works only in cwd", ErrInvalidParams)
		}

		// The agent's working directory is frozen at construction, so a session
		// for any other directory cannot be served. Validating that cwd was
		// absolute and then ignoring it was worse than not validating at all:
		// it made the field look honoured.
		if filepath.Clean(req.Cwd) != filepath.Clean(cfg.WorkingDir) {
			return nil, fmt.Errorf(
				"session/new: %w: this agent serves only %q, not %q; its working directory is fixed at startup",
				ErrInvalidParams, cfg.WorkingDir, req.Cwd)
		}

		// ACP defines a session as an independent context with its own history.
		// This agent owns exactly one conversation, so a second session id would
		// hand the client something that looks isolated and silently shares the
		// first session's history, tool state and workspace. Refusing is the
		// honest position until a session can own its own agent state.
		if err := store.claimSole(); err != nil {
			return nil, fmt.Errorf("session/new: %w", err)
		}

		id, err := newSessionID()
		if err != nil {
			store.releaseSole()
			return nil, fmt.Errorf("session/new: %w", err)
		}
		store.add(id, session{cwd: req.Cwd})

		return json.Marshal(NewSessionResult{SessionID: id})
	})

	registry.RegisterMethod(MethodSessionPrompt, func(params json.RawMessage) (json.RawMessage, error) {
		if !cfg.Handshake.IsComplete() {
			return nil, fmt.Errorf("session/prompt: %w: initialize must complete first", ErrInvalidParams)
		}
		var req PromptParams
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("session/prompt: %w: %v", ErrInvalidParams, err)
		}

		sess, ok := store.get(req.SessionID)
		if !ok {
			return nil, fmt.Errorf("session/prompt: %w: unknown sessionId %q", ErrInvalidParams, req.SessionID)
		}

		blocks, err := DecodePromptContent(req.Prompt, cfg.Capabilities.Prompt)
		if err != nil {
			return nil, fmt.Errorf("session/prompt: %w", err)
		}

		if cfg.Prompt == nil {
			return nil, fmt.Errorf("session/prompt: no prompt handler is registered")
		}

		// ACP allows one turn per session. Claimed after validation so a
		// rejected payload does not occupy the session, and released on every
		// exit so a failed turn does not wedge it shut.
		if err := store.beginTurn(req.SessionID); err != nil {
			return nil, fmt.Errorf("session/prompt: %w", err)
		}
		defer store.endTurn(req.SessionID)

		// context.Background, and that is a real limitation rather than a
		// convenience: Handler takes only params, so there is no request context
		// to derive from and no way to cancel a running turn. ACP's
		// session/cancel notification therefore cannot be honoured yet. Fixing
		// it means threading a context through Handler — every handler in the
		// package — which is a structural change and not this slice's.
		stop, err := cfg.Prompt(context.Background(), PromptRequest{
			SessionID: req.SessionID,
			Cwd:       sess.cwd,
			Prompt:    blocks,
		})
		if err != nil {
			return nil, fmt.Errorf("session/prompt: %w", err)
		}

		return json.Marshal(PromptResult{StopReason: stop})
	})
}

// newSessionID mints an opaque, unguessable session identifier. A counter would
// be simpler, but session ids appear in client logs and in any shared
// transcript, and a predictable one invites a peer to address a session it was
// never given.
func newSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
