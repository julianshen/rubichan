package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/sourcegraph/conc/pool"
)

// SubagentConfig, SubagentResult, and SubagentSpawner are defined in
// pkg/agentsdk/ and re-exported via sdk_aliases.go.

// IsolationWorktree is the constant for worktree-based subagent isolation.
const IsolationWorktree = "worktree"

// WorktreeHandle represents an isolated worktree created for a subagent.
type WorktreeHandle struct {
	Dir  string // Filesystem path to the worktree
	Name string // Worktree name (for cleanup)
}

// WorktreeProvider creates and removes worktrees for subagent isolation.
// This interface decouples the agent package from internal/worktree.
type WorktreeProvider interface {
	CreateWorktree(ctx context.Context, name string) (*WorktreeHandle, error)
	HasWorktreeChanges(ctx context.Context, name string) (bool, error)
	RemoveWorktree(ctx context.Context, name string) error
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
	WorktreeProvider   WorktreeProvider   // Optional; required for isolation: "worktree"
	RateLimiter        *SharedRateLimiter // Optional; shared rate limiter propagated to children
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

	// Handle worktree isolation: create an isolated worktree for this subagent.
	var wtCleanup func()
	var workDir string
	if cfg.Isolation == IsolationWorktree {
		if s.WorktreeProvider == nil {
			return nil, fmt.Errorf("worktree isolation requested but no WorktreeProvider configured")
		}
		wtName := fmt.Sprintf("subagent-%s-%d", cfg.Name, time.Now().UnixNano())
		s.dispatchHook(ctx, skills.HookOnWorktreeCreate, map[string]any{
			"subagent_name": cfg.Name,
			"worktree_name": wtName,
		})
		wt, err := s.WorktreeProvider.CreateWorktree(ctx, wtName)
		if err != nil {
			return nil, fmt.Errorf("creating worktree for subagent: %w", err)
		}
		workDir = wt.Dir
		wtCleanup = func() {
			changed, err := s.WorktreeProvider.HasWorktreeChanges(ctx, wtName)
			if err != nil || changed {
				return // Preserve on error or dirty state.
			}
			s.dispatchHook(ctx, skills.HookOnWorktreeRemove, map[string]any{
				"subagent_name": cfg.Name,
				"worktree_name": wtName,
			})
			_ = s.WorktreeProvider.RemoveWorktree(ctx, wtName)
		}
	}
	if wtCleanup != nil {
		defer wtCleanup()
	}

	// Filter tools. Re-register the task tool with the child's depth so that
	// nested spawns enforce correct depth limits through fresh state.
	childTools := s.ParentTools.Filter(cfg.Tools)
	if taskTool, ok := childTools.Get("task"); ok {
		if tt, ok := taskTool.(*tools.TaskTool); ok {
			_ = childTools.Unregister("task")
			_ = childTools.Register(tt.WithDepth(cfg.Depth))
		}
	}

	// Build child config.
	childCfg := *s.Config
	childCfg.Agent.MaxTurns = cfg.MaxTurns
	if cfg.Model != "" {
		childCfg.Provider.Model = cfg.Model
	}
	if cfg.ContextBudget > 0 {
		childCfg.Agent.ContextBudget = cfg.ContextBudget
	}

	// Build options.
	var opts []AgentOption
	if workDir != "" {
		opts = append(opts, WithWorkingDir(workDir))
	}
	if s.ApprovalChecker != nil {
		opts = append(opts, WithApprovalChecker(s.ApprovalChecker))
	}
	if s.RateLimiter != nil {
		opts = append(opts, WithRateLimiter(s.RateLimiter))
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

	// Child agents use a deterministic deny-all approval callback for tools
	// that require interactive approval. The approval checker (set via opts
	// above) may auto-approve or auto-deny; this callback handles the
	// fallback when neither applies.
	denyAllApproval := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return false, nil
	}
	child := New(s.Provider, childTools, denyAllApproval, &childCfg, opts...)

	s.dispatchHook(ctx, skills.HookOnTaskCreated, map[string]any{
		"name":   cfg.Name,
		"prompt": prompt,
		"depth":  cfg.Depth,
	})

	// Run a single Turn — runLoop handles the full multi-turn loop internally,
	// calling the LLM and executing tools iteratively until a text-only response
	// or the turn limit is reached. This avoids appending empty user messages.
	result := &SubagentResult{Name: cfg.Name}
	var output strings.Builder
	toolSet := make(map[string]struct{})
	var toolsUsed []string

	eventCh, err := child.Turn(ctx, prompt)
	if err != nil {
		result.Error = err
		result.Output = output.String()
		result.ToolsUsed = toolsUsed
		return result, nil
	}

	var turnCount int
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
				turnCount++
			}
		case "error":
			result.Error = event.Error
		case "done":
			result.InputTokens += event.InputTokens
			result.OutputTokens += event.OutputTokens
		}
	}
	// At minimum one turn (the initial prompt), plus one for each tool call round.
	if turnCount == 0 {
		turnCount = 1
	}
	result.TurnCount = turnCount

	result.Output = output.String()
	result.ToolsUsed = toolsUsed

	s.dispatchHook(ctx, skills.HookOnTaskCompleted, map[string]any{
		"name":          result.Name,
		"output":        result.Output,
		"turn_count":    result.TurnCount,
		"input_tokens":  result.InputTokens,
		"output_tokens": result.OutputTokens,
		"tools_used":    append([]string(nil), result.ToolsUsed...),
		"error":         errString(result.Error),
	})

	return result, nil
}

// dispatchHook fires a skill lifecycle hook on the parent runtime. Failures
// are surfaced via the runtime's own logging and never affect spawn behavior.
func (s *DefaultSubagentSpawner) dispatchHook(ctx context.Context, phase skills.HookPhase, data map[string]any) {
	if s.ParentSkillRuntime == nil {
		return
	}
	_, _ = s.ParentSkillRuntime.DispatchHook(skills.HookEvent{
		Phase: phase,
		Ctx:   ctx,
		Data:  data,
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// SpawnParallel runs multiple subagent requests concurrently, returning one
// result per request in the same order. Individual spawn failures are captured
// in SubagentResult.Error rather than aborting the batch.
func (s *DefaultSubagentSpawner) SpawnParallel(
	ctx context.Context,
	requests []SubagentRequest,
	maxConcurrent int,
) ([]SubagentResult, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}

	results := make([]SubagentResult, len(requests))
	p := pool.New().WithMaxGoroutines(maxConcurrent)

	for i, req := range requests {
		i, req := i, req
		p.Go(func() {
			result, err := s.Spawn(ctx, req.Config, req.Prompt)
			if err != nil {
				results[i] = SubagentResult{
					Name:  req.Config.Name,
					Error: err,
				}
				return
			}
			results[i] = *result
		})
	}

	p.Wait()
	return results, nil
}

func defaultInheritSkills(inherit *bool) bool {
	if inherit == nil {
		return true
	}
	return *inherit
}
