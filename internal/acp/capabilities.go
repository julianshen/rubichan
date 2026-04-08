package acp

import (
	"encoding/json"
	"fmt"
)

// CapabilityRegistry holds all registered capabilities (tools, skills, verdicts).
type CapabilityRegistry struct {
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
	cr.tools[t.Name] = t
}

// RegisterSkill registers a skill capability.
func (cr *CapabilityRegistry) RegisterSkill(s Skill) {
	cr.skills[s.Name] = s
}

// RegisterMethod registers a handler for an ACP method.
func (cr *CapabilityRegistry) RegisterMethod(method string, handler Handler) {
	cr.methods[method] = handler
}

// GetCapabilities returns all capabilities as CapabilityDefinition slice.
func (cr *CapabilityRegistry) GetCapabilities() ([]CapabilityDefinition, error) {
	var caps []CapabilityDefinition

	// Add tool capabilities
	for _, tool := range cr.tools {
		toolCap := ToolCapability{Tool: tool}
		data, err := json.Marshal(toolCap)
		if err != nil {
			return nil, fmt.Errorf("marshal tool capability: %w", err)
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
			return nil, fmt.Errorf("marshal skill capability: %w", err)
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
	methods := make([]string, 0, len(cr.methods))
	for m := range cr.methods {
		methods = append(methods, m)
	}
	return methods
}

// Call invokes a registered method handler.
func (cr *CapabilityRegistry) Call(method string, params json.RawMessage) (json.RawMessage, error) {
	handler, ok := cr.methods[method]
	if !ok {
		return nil, fmt.Errorf("method not found: %s", method)
	}
	return handler(params)
}
