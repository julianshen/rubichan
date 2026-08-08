package acp

import "sync"

// Handshake records whether initialize has completed on a connection.
//
// ACP requires the handshake before any other request: it is where the protocol
// version is agreed and both sides declare capabilities. A session opened
// without it belongs to a peer whose contract is unknown — the agent would be
// running turns for a client whose protocol version it never agreed and whose
// capabilities it never learned.
//
// It is a shared object rather than a flag on either config because two
// registrations need it: RegisterInitialize marks it, RegisterSession reads it,
// and neither can see the other's state.
type Handshake struct {
	mu       sync.RWMutex
	complete bool
}

// NewHandshake returns a handshake that has not happened yet.
func NewHandshake() *Handshake {
	return &Handshake{}
}

// MarkComplete records a successful initialize.
func (h *Handshake) MarkComplete() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.complete = true
}

// IsComplete reports whether initialize has succeeded. A nil Handshake reports
// true: it means the caller did not ask for the ordering to be enforced, which
// is what keeps this optional for tests and for registries that serve no
// session methods.
func (h *Handshake) IsComplete() bool {
	if h == nil {
		return true
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.complete
}
