package acp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// CapabilityRegistry holds all registered capabilities (tools, skills, verdicts).
// All methods are thread-safe and can be called concurrently.
type CapabilityRegistry struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	skills  map[string]Skill
	methods map[string]Handler
}

// Handler is a function that processes an ACP method call.
type Handler func(params json.RawMessage) (json.RawMessage, error)

// NewCapabilityRegistry creates a new registry.
func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{
		tools:   make(map[string]Tool),
		skills:  make(map[string]Skill),
		methods: make(map[string]Handler),
	}
}

// RegisterTool registers a tool capability.
func (cr *CapabilityRegistry) RegisterTool(t Tool) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.tools[t.Name] = t
}

// RegisterSkill registers a skill capability.
func (cr *CapabilityRegistry) RegisterSkill(s Skill) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.skills[s.Name] = s
}

// RegisterMethod registers a handler for an ACP method.
func (cr *CapabilityRegistry) RegisterMethod(method string, handler Handler) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.methods[method] = handler
}

// GetCapabilities returns all capabilities as CapabilityDefinition slice.
func (cr *CapabilityRegistry) GetCapabilities() ([]CapabilityDefinition, error) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	var caps []CapabilityDefinition

	// Add tool capabilities
	for _, tool := range cr.tools {
		toolCap := ToolCapability{Tool: tool}
		data, err := json.Marshal(toolCap)
		if err != nil {
			return nil, fmt.Errorf("marshal tool '%s': %w", tool.Name, err)
		}
		caps = append(caps, CapabilityDefinition{
			Type:       "tool",
			Name:       tool.Name,
			Definition: json.RawMessage(data),
		})
	}

	// Add skill capabilities
	for _, skill := range cr.skills {
		skillCap := SkillCapability{Skill: skill}
		data, err := json.Marshal(skillCap)
		if err != nil {
			return nil, fmt.Errorf("marshal skill '%s': %w", skill.Name, err)
		}
		caps = append(caps, CapabilityDefinition{
			Type:       "skill",
			Name:       skill.Name,
			Definition: json.RawMessage(data),
		})
	}

	return caps, nil
}

// GetMethods returns all registered method names.
func (cr *CapabilityRegistry) GetMethods() []string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	methods := make([]string, 0, len(cr.methods))
	for m := range cr.methods {
		methods = append(methods, m)
	}
	return methods
}

// ErrMethodNotFound reports that no handler is registered for a method. It is a
// sentinel because callers must distinguish "nobody serves this" from "the
// handler failed" to pick a JSON-RPC error code, and matching on message text
// misclassifies any handler error that happens to mention a missing method.
var ErrMethodNotFound = errors.New("acp: method not found")

// Call invokes a registered method handler.
func (cr *CapabilityRegistry) Call(method string, params json.RawMessage) (json.RawMessage, error) {
	cr.mu.RLock()
	handler, ok := cr.methods[method]
	cr.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMethodNotFound, method)
	}
	return handler(params)
}
