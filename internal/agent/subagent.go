package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
)

// SubagentConfig defines how a child agent is created.
type SubagentConfig struct {
	Name          string   // Identifier (e.g., "explorer")
	SystemPrompt  string   // Additional system prompt (appended to base)
	Tools         []string // Whitelist of tool names (nil = all parent tools)
	MaxTurns      int      // Turn limit (0 = default 10)
	MaxTokens     int      // Output token budget (0 = inherit)
	Model         string   // Override model (empty = inherit parent)
	Depth         int      // Current nesting level (0 = top-level)
	MaxDepth      int      // Maximum nesting (0 = default 3)
	InheritSkills *bool    // Nil/default = inherit currently active parent skills
	ExtraSkills   []string
	DisableSkills []string
}

// SubagentResult is returned when a child agent completes.
type SubagentResult struct {
	Name         string   // Which agent definition was used
	Output       string   // Final text output from the child
	ToolsUsed    []string // Tools the child called
	TurnCount    int      // How many turns the child took
	InputTokens  int      // Total input tokens consumed
	OutputTokens int      // Total output tokens consumed
	Error        error    // Non-nil if the child failed
}

// SubagentSpawner creates and runs child agents.
type SubagentSpawner interface {
	Spawn(ctx context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error)
}

const (
	defaultSubagentMaxTurns = 10
	defaultSubagentMaxDepth = 3
)

// DefaultSubagentSpawner creates real child Agent instances.
type DefaultSubagentSpawner struct {
	Provider           provider.LLMProvider
	ParentTools        *tools.Registry
	ParentSkillRuntime *skills.Runtime
	Config             *config.Config
	ApprovalChecker    ApprovalChecker
	AgentDefs          *AgentDefRegistry
}

// Spawn creates and runs a child agent with the given configuration and
// initial prompt. The child is ephemeral (no persistence store) and runs
// its own turn loop until it produces a text-only response or exhausts
// its turn limit.
func (s *DefaultSubagentSpawner) Spawn(ctx context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error) {
	if cfg.MaxTurns == 0 {
		cfg.MaxTurns = defaultSubagentMaxTurns
	}
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = defaultSubagentMaxDepth
	}
	if cfg.Depth >= cfg.MaxDepth {
		return nil, fmt.Errorf("subagent depth %d exceeds max depth %d", cfg.Depth, cfg.MaxDepth)
	}
	if s.Provider == nil {
		return nil, fmt.Errorf("subagent spawner has no provider configured")
	}

	// Filter tools.
	childTools := s.ParentTools.Filter(cfg.Tools)

	// Build child config.
	childCfg := *s.Config
	childCfg.Agent.MaxTurns = cfg.MaxTurns
	if cfg.Model != "" {
		childCfg.Provider.Model = cfg.Model
	}

	// Build options.
	var opts []AgentOption
	if s.ApprovalChecker != nil {
		opts = append(opts, WithApprovalChecker(s.ApprovalChecker))
	}
	if cfg.SystemPrompt != "" {
		opts = append(opts, WithExtraSystemPrompt("Subagent Instructions", cfg.SystemPrompt))
	}
	if s.ParentSkillRuntime != nil {
		snapshot := s.ParentSkillRuntime.SnapshotForSubagent(skills.SubagentSkillPolicy{
			InheritActive: defaultInheritSkills(cfg.InheritSkills),
			Include:       cfg.ExtraSkills,
			Exclude:       cfg.DisableSkills,
		})
		if snapshot != nil {
			opts = append(opts, WithSkillRuntime(snapshot))
		}
	}

	// Create child agent (no store — ephemeral).
	child := New(s.Provider, childTools, nil, &childCfg, opts...)

	// Run turn loop.
	result := &SubagentResult{Name: cfg.Name}
	var output strings.Builder
	toolSet := make(map[string]struct{})
	var toolsUsed []string

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		var userMsg string
		if turn == 0 {
			userMsg = prompt
		}
		eventCh, err := child.Turn(ctx, userMsg)
		if err != nil {
			result.Error = err
			result.TurnCount = turn + 1
			result.Output = output.String()
			result.ToolsUsed = toolsUsed
			return result, nil
		}

		var hasTool bool
		for event := range eventCh {
			switch event.Type {
			case "text_delta":
				output.WriteString(event.Text)
			case "tool_call":
				if event.ToolCall != nil {
					if _, seen := toolSet[event.ToolCall.Name]; !seen {
						toolSet[event.ToolCall.Name] = struct{}{}
						toolsUsed = append(toolsUsed, event.ToolCall.Name)
					}
					hasTool = true
				}
			case "error":
				result.Error = event.Error
			case "done":
				result.InputTokens += event.InputTokens
				result.OutputTokens += event.OutputTokens
			}
		}
		result.TurnCount = turn + 1
		if !hasTool {
			break
		}
	}

	result.Output = output.String()
	result.ToolsUsed = toolsUsed
	return result, nil
}

func defaultInheritSkills(inherit *bool) bool {
	if inherit == nil {
		return true
	}
	return *inherit
}
