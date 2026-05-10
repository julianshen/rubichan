package skills

import (
	"context"
	"fmt"
)

// SubagentSpawner is the interface for spawning sub-agents.
// This is satisfied by agent.DefaultSubagentSpawner.
type SubagentSpawner interface {
	Spawn(ctx context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error)
}

// ForkedSkillExecutor runs forked skills in isolated sub-agents.
type ForkedSkillExecutor struct {
	spawner         SubagentSpawner
	defaultMaxTurns int
	defaultMaxDepth int
}

// ForkedSkillExecutorConfig configures the executor.
type ForkedSkillExecutorConfig struct {
	Spawner         SubagentSpawner
	DefaultMaxTurns int
	DefaultMaxDepth int
}

// NewForkedSkillExecutor creates a new executor.
func NewForkedSkillExecutor(cfg ForkedSkillExecutorConfig) (*ForkedSkillExecutor, error) {
	if cfg.Spawner == nil {
		return nil, fmt.Errorf("forked skill executor: spawner required")
	}
	maxTurns := cfg.DefaultMaxTurns
	if maxTurns == 0 {
		maxTurns = 10
	}
	maxDepth := cfg.DefaultMaxDepth
	if maxDepth == 0 {
		maxDepth = 3
	}
	return &ForkedSkillExecutor{
		spawner:         cfg.Spawner,
		defaultMaxTurns: maxTurns,
		defaultMaxDepth: maxDepth,
	}, nil
}

// Execute runs a skill in a sub-agent if it's forked. Returns (result, handled, error).
// If the skill is not forked, returns (nil, false, nil) so the caller can handle it inline.
func (e *ForkedSkillExecutor) Execute(ctx context.Context, skill *Skill, prompt string) (*SubagentResult, bool, error) {
	if skill == nil || skill.Manifest == nil {
		return nil, false, nil
	}
	if skill.Manifest.ExecutionMode != ExecutionModeFork {
		return nil, false, nil
	}

	// Build system prompt from instruction body or manifest description.
	systemPrompt := skill.InstructionBody
	if systemPrompt == "" {
		systemPrompt = skill.Manifest.Description
	}

	cfg := SubagentConfig{
		Name:         skill.Manifest.Name,
		MaxTurns:     e.defaultMaxTurns,
		MaxDepth:     e.defaultMaxDepth,
		Depth:        1,
		SystemPrompt: systemPrompt,
	}

	result, err := e.spawner.Spawn(ctx, cfg, prompt)
	if err != nil {
		return nil, true, fmt.Errorf("forked skill %q: %w", skill.Manifest.Name, err)
	}

	return result, true, nil
}
