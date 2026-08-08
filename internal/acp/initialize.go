package acp

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion is the ACP major version this agent speaks.
//
// It is an integer, not a string. The spec's schema page describes it as a
// string; the initialization page is explicit that it is "a single integer that
// identifies a MAJOR protocol version", and that is the one to trust. The
// disagreement is the reason this was fetched rather than recalled.
const ProtocolVersion = 1

// AgentInfo identifies this agent to the client. Optional per the spec, which
// says an agent SHOULD provide a name and version.
type AgentInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// ClientInfo identifies the peer. Optional in the request.
type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// PromptCapabilities declares which content types this agent accepts in a
// prompt beyond plain text.
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// MCPCapabilities declares which MCP server transports this agent can connect
// to on the client's behalf.
type MCPCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

// AgentCapabilities is what this agent tells the client it can do.
type AgentCapabilities struct {
	LoadSession bool               `json:"loadSession"`
	Prompt      PromptCapabilities `json:"promptCapabilities"`
	MCP         MCPCapabilities    `json:"mcpCapabilities"`
}

// FSCapabilities is the client's offer to perform file I/O on the agent's
// behalf, via fs/read_text_file and fs/write_text_file.
type FSCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

// ClientCapabilities is what the peer says it can do for us. It is worth
// retaining rather than discarding: asking a client for a capability it never
// offered is the same class of mistake as calling a method the peer does not
// serve, which is what made the mode adapters deleted in #339 unusable.
type ClientCapabilities struct {
	FS       FSCapabilities `json:"fs"`
	Terminal bool           `json:"terminal"`
}

// InitializeParams is the client's half of the handshake.
type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         *ClientInfo        `json:"clientInfo,omitempty"`
}

// InitializeResult is the agent's half.
type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AgentInfo         AgentInfo         `json:"agentInfo,omitempty"`
	// AuthMethods is never omitted. A client reading an absent field cannot
	// tell "no authentication required" from "the agent forgot to say".
	AuthMethods []AuthMethod `json:"authMethods"`
}

// AuthMethod is one way a client may authenticate before opening a session.
type AuthMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// InitializeConfig supplies what the handshake answers with.
type InitializeConfig struct {
	AgentInfo    AgentInfo
	Capabilities AgentCapabilities
	AuthMethods  []AuthMethod
	// Handshake, when set, is marked complete once this handshake succeeds.
	// The session methods read it to enforce ACP's ordering rule.
	Handshake *Handshake

	// OnInitialized receives the peer's capabilities once negotiated, so the
	// agent can avoid requesting things the client never offered.
	OnInitialized func(ClientCapabilities, AgentInfo)
}

// RegisterInitialize wires the ACP handshake into a registry.
func RegisterInitialize(registry *CapabilityRegistry, cfg InitializeConfig) {
	registry.RegisterMethod(MethodInitialize, func(params json.RawMessage) (json.RawMessage, error) {
		// protocolVersion is decoded through a pointer because ACP marks it
		// required and an omitted or null integer would otherwise unmarshal to
		// 0 with no error. The agent would then answer version 1 to a client
		// that never said what it speaks, making the negotiation a fiction.
		var raw struct {
			ProtocolVersion    *int               `json:"protocolVersion"`
			ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
			ClientInfo         *ClientInfo        `json:"clientInfo,omitempty"`
		}
		if err := json.Unmarshal(params, &raw); err != nil {
			// Wrapped so the connection answers -32602: the client's payload
			// is wrong, which is something it can fix.
			return nil, fmt.Errorf("initialize: %w: %v", ErrInvalidParams, err)
		}
		if raw.ProtocolVersion == nil {
			return nil, fmt.Errorf("initialize: %w: protocolVersion is required", ErrInvalidParams)
		}
		req := InitializeParams{
			ProtocolVersion:    *raw.ProtocolVersion,
			ClientCapabilities: raw.ClientCapabilities,
			ClientInfo:         raw.ClientInfo,
		}
		_ = req.ProtocolVersion // read, never echoed; see below

		// The spec requires answering an unsupported version with the latest we
		// support rather than failing: version selection is the client's call,
		// and it cannot make it if we refuse to say what we speak. So the
		// requested version is read but never echoed — we always answer with
		// ours, which for a matching request is the same number anyway.
		result := InitializeResult{
			ProtocolVersion:   ProtocolVersion,
			AgentCapabilities: cfg.Capabilities,
			AgentInfo:         cfg.AgentInfo,
			AuthMethods:       cfg.AuthMethods,
		}
		if result.AuthMethods == nil {
			result.AuthMethods = []AuthMethod{}
		}

		// Marked before the result is marshalled but after every validation
		// above, so a rejected handshake does not unlock the session methods.
		cfg.Handshake.MarkComplete()

		if cfg.OnInitialized != nil {
			cfg.OnInitialized(req.ClientCapabilities, cfg.AgentInfo)
		}

		out, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("initialize: marshal result: %w", err)
		}
		return out, nil
	})
}
