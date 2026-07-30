package subagents

// These adapters bridge the agent and worktree types to the narrower
// interfaces the task tools expect. Moved verbatim from cmd/rubichan.

import (
	"context"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/worktree"
)

// spawnerAdapter bridges agent.DefaultSubagentSpawner to the tools.TaskSpawner
// interface, converting between type-specific config/result structs.
type spawnerAdapter struct {
	spawner *agent.DefaultSubagentSpawner
}

func (a *spawnerAdapter) Spawn(ctx context.Context, cfg tools.TaskSpawnConfig, prompt string) (*tools.TaskSpawnResult, error) {
	result, err := a.spawner.Spawn(ctx, agent.SubagentConfig{
		Name:          cfg.Name,
		SystemPrompt:  cfg.SystemPrompt,
		Tools:         cfg.Tools,
		MaxTurns:      cfg.MaxTurns,
		MaxTokens:     cfg.MaxTokens,
		Model:         cfg.Model,
		Depth:         cfg.Depth,
		MaxDepth:      cfg.MaxDepth,
		InheritSkills: cfg.InheritSkills,
		ExtraSkills:   cfg.ExtraSkills,
		DisableSkills: cfg.DisableSkills,
		Isolation:     cfg.Isolation,
	}, prompt)
	if err != nil {
		return nil, err
	}
	return &tools.TaskSpawnResult{
		Name:         result.Name,
		Output:       result.Output,
		ToolsUsed:    result.ToolsUsed,
		TurnCount:    result.TurnCount,
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		Error:        result.Error,
	}, nil
}

// agentDefLookupAdapter bridges agent.AgentDefRegistry to tools.TaskAgentDefLookup.
type agentDefLookupAdapter struct {
	reg *agent.AgentDefRegistry
}

func (a *agentDefLookupAdapter) GetAgentDef(name string) (*tools.TaskAgentDef, bool) {
	def, ok := a.reg.Get(name)
	if !ok {
		return nil, false
	}
	return &tools.TaskAgentDef{
		Name:          def.Name,
		SystemPrompt:  def.SystemPrompt,
		Tools:         def.Tools,
		MaxTurns:      def.MaxTurns,
		MaxDepth:      def.MaxDepth,
		Model:         def.Model,
		InheritSkills: def.InheritSkills,
		ExtraSkills:   def.ExtraSkills,
		DisableSkills: def.DisableSkills,
	}, true
}

// wakeManagerAdapter bridges agent.WakeManager to tools.BackgroundTaskManager.
type wakeManagerAdapter struct {
	wm *agent.WakeManager
}

func (a *wakeManagerAdapter) SubmitBackground(name string, cancel context.CancelFunc) string {
	return a.wm.Submit(name, cancel)
}

func (a *wakeManagerAdapter) CompleteBackground(taskID string, output string, err error) {
	a.wm.Complete(taskID, &agent.SubagentResult{
		Output: output,
		Error:  err,
	})
}

// wakeStatusAdapter bridges agent.WakeManager to tools.TaskStatusProvider.
type wakeStatusAdapter struct {
	wm *agent.WakeManager
}

func (a *wakeStatusAdapter) BackgroundTaskStatus() []tools.BackgroundTaskInfo {
	statuses := a.wm.Status()
	result := make([]tools.BackgroundTaskInfo, len(statuses))
	for i, s := range statuses {
		result[i] = tools.BackgroundTaskInfo{
			ID:        s.ID,
			AgentName: s.AgentName,
			Status:    s.Status,
		}
	}
	return result
}

// worktreeProviderAdapter bridges worktree.Manager to agent.WorktreeProvider.
type worktreeProviderAdapter struct {
	mgr *worktree.Manager
}

func (a *worktreeProviderAdapter) CreateWorktree(ctx context.Context, name string) (*agent.WorktreeHandle, error) {
	wt, err := a.mgr.Create(ctx, name)
	if err != nil {
		return nil, err
	}
	return &agent.WorktreeHandle{Dir: wt.Dir(), Name: wt.Name}, nil
}

func (a *worktreeProviderAdapter) HasWorktreeChanges(ctx context.Context, name string) (bool, error) {
	return a.mgr.HasChanges(ctx, name)
}

func (a *worktreeProviderAdapter) RemoveWorktree(ctx context.Context, name string) error {
	return a.mgr.Remove(ctx, name)
}
