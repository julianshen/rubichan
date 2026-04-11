// Package cmuxtest provides test helpers for code that depends on cmux.
package cmuxtest

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/internal/cmux"
)

// Call records a single invocation captured by MockClient.
type Call struct {
	Method string
	Params any
}

// MockClient records calls and returns canned responses.
// It implements the cmux.Caller interface.
type MockClient struct {
	mu       sync.Mutex
	results  map[string]any
	errors   map[string]string // method → error message (returns OK:false)
	calls    []Call
	identity *cmux.Identity
}

// NewMockClient creates a MockClient with a default mock identity.
func NewMockClient() *MockClient {
	return &MockClient{
		results: make(map[string]any),
		errors:  make(map[string]string),
		identity: &cmux.Identity{
			WindowID:    "mock-window",
			WorkspaceID: "mock-workspace",
			PaneID:      "mock-pane",
			SurfaceID:   "mock-surface",
		},
	}
}

// SetResult sets the canned result returned for method.
func (m *MockClient) SetResult(method string, result any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[method] = result
	delete(m.errors, method)
}

// SetError configures the method to return an OK:false response with the given error message.
func (m *MockClient) SetError(method, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[method] = errMsg
	delete(m.results, method)
}

// Call records the call and returns the canned result or error.
// It returns a transport error if neither result nor error has been set for method.
func (m *MockClient) Call(method string, params any) (*cmux.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, Call{Method: method, Params: params})

	if errMsg, ok := m.errors[method]; ok {
		return &cmux.Response{
			ID:    "mock-id",
			OK:    false,
			Error: errMsg,
		}, nil
	}

	result, ok := m.results[method]
	if !ok {
		return nil, fmt.Errorf("cmuxtest: no result set for method %q", method)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("cmuxtest: marshal result for %q: %w", method, err)
	}

	return &cmux.Response{
		ID:     "mock-id",
		OK:     true,
		Result: json.RawMessage(raw),
	}, nil
}

// Identity returns the mock identity.
func (m *MockClient) Identity() *cmux.Identity {
	return m.identity
}

// Calls returns a copy of all recorded calls.
func (m *MockClient) Calls() []Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Call, len(m.calls))
	copy(cp, m.calls)
	return cp
}

// Reset clears all recorded calls and canned results.
func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = nil
	m.results = make(map[string]any)
	m.errors = make(map[string]string)
}
