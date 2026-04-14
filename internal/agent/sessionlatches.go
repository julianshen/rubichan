package agent

import "sync"

// sessionLatches freezes capability values for the lifetime of an agent
// session. Each latch follows a one-way ratchet: the first non-empty
// value wins, subsequent calls return the stored value regardless of
// what was passed. This prevents mid-session capability changes from
// altering the system prompt's dynamic section, which would invalidate
// ~50-70K tokens of the provider's session prompt cache.
type sessionLatches struct {
	mu sync.Mutex

	// toolHintSet is true once latchToolHint has been called at least once.
	// toolHintEnabled is the stored value.
	toolHintSet     bool
	toolHintEnabled bool

	// reasoningEffort is empty until the first non-empty value is latched.
	reasoningEffort string
}

// newSessionLatches returns a sessionLatches with all latches unset.
func newSessionLatches() *sessionLatches {
	return &sessionLatches{}
}

// latchToolHint records the tool-discovery-hint preference on the first
// call and returns that value on all subsequent calls. The first call's
// argument is the latched value.
func (l *sessionLatches) latchToolHint(want bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.toolHintSet {
		l.toolHintSet = true
		l.toolHintEnabled = want
	}
	return l.toolHintEnabled
}

// latchReasoningEffort records the reasoning effort level on the first
// non-empty call and returns that value on all subsequent calls. Empty
// strings never latch — callers can pass "" to mean "use whatever is
// already latched, or empty if nothing has been latched yet."
func (l *sessionLatches) latchReasoningEffort(want string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reasoningEffort == "" && want != "" {
		l.reasoningEffort = want
	}
	return l.reasoningEffort
}
