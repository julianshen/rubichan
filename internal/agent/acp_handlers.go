package agent

import (
	"github.com/julianshen/rubichan/internal/acp"
)

// agentVersion is reported to peers in the initialize handshake.
const agentVersion = "1.0.0"

// acpAgentCapabilities is what this agent tells an ACP client it can do.
//
// It is a single function rather than a literal at each use because two places
// now depend on it: the handshake publishes it, and session/prompt gates
// incoming content on it. Restating it in both would let the agent advertise
// one set and enforce another, which is the specific dishonesty the prompt
// decoder exists to prevent.
//
// Every field is false, and stays false until the method behind it is
// registered. embeddedContext means the agent accepts embedded
// ContentBlock::Resource data in session/prompt; loadSession means it serves
// session/load. Neither exists yet, and advertising a capability whose method
// is unregistered tells a client it may send something that will fail — the
// same defect as the deleted adapters, pointed the other way. These flip on in
// the slice that registers the handler.
func acpAgentCapabilities() acp.AgentCapabilities {
	return acp.AgentCapabilities{}
}

// NewACPRegistry builds the capability registry an ACP peer is served from:
// the agent's tools plus its method handlers. This is a composition-root
// operation — the agent core holds no ACP state.
//
// It returns a registry rather than a server because the transport is now
// acp.Conn, which takes a registry and owns the framing. Callers wrap it with
// acp.NewConn(r, w, registry) where a mode actually serves ACP.
func NewACPRegistry(a *Agent) *acp.CapabilityRegistry {
	registry, _ := newACPRegistry(a)
	return registry
}

// newACPRegistry also returns the handshake the registry's initialize marks, so
// a caller wiring session methods can gate them on it. Returning it rather than
// reaching back into the registry keeps the ordering rule explicit at the
// composition root instead of hidden inside the registry.
func newACPRegistry(a *Agent) (*acp.CapabilityRegistry, *acp.Handshake) {
	registry := acp.NewCapabilityRegistry()
	handshake := acp.NewHandshake()
	a.registerACPCapabilities(registry, handshake)
	return registry, handshake
}

// registerACPCapabilities registers all ACP capabilities and method
// handlers on the given registry.
func (a *Agent) registerACPCapabilities(registry *acp.CapabilityRegistry, handshake *acp.Handshake) {
	for _, toolName := range a.tools.Names() {
		tool, ok := a.tools.Get(toolName)
		if !ok {
			continue
		}
		registry.RegisterTool(acp.Tool{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		})
	}

	acp.RegisterInitialize(registry, acp.InitializeConfig{
		AgentInfo:    acp.AgentInfo{Name: "rubichan", Version: agentVersion},
		Capabilities: acpAgentCapabilities(),
		Handshake:    handshake,
		OnInitialized: func(caps acp.ClientCapabilities, _ acp.AgentInfo) {
			a.setClientCapabilities(caps)
		},
	})

}

// setClientCapabilities records what the peer offered during the handshake.
func (a *Agent) setClientCapabilities(caps acp.ClientCapabilities) {
	a.acpClientCapsMu.Lock()
	defer a.acpClientCapsMu.Unlock()
	a.acpClientCaps = caps
}

// ClientCapabilities reports what the connected ACP client said it can do.
// Zero until a handshake completes, which is the safe default: it means the
// agent assumes the client offers nothing rather than assuming it offers
// everything.
func (a *Agent) ClientCapabilities() acp.ClientCapabilities {
	a.acpClientCapsMu.RLock()
	defer a.acpClientCapsMu.RUnlock()
	return a.acpClientCaps
}
