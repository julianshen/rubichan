// Package skillruntime builds the skill runtime: it discovers skills from the
// user, project and built-in sources, wires each backend to the integrations
// it is allowed to use, and activates whatever the current mode and project
// files trigger.
//
// It lives outside cmd/ because which backends exist, what they are handed,
// and which skills switch on are product behaviour. The caller supplies the
// composition inputs — config, working directory, config directory, and the
// skill selection the CLI flags resolved to.
package skillruntime

import (
	"context"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/integrations"
	"github.com/julianshen/rubichan/internal/skills"
	starengine "github.com/julianshen/rubichan/internal/skills/starlark"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/skillsdk"
)

// noopPromptBackend is a no-op backend for prompt-only skills that have no
// implementation backend. It satisfies the SkillBackend interface so prompt
// skills can be activated through the normal runtime flow.
type noopPromptBackend struct{}

func (*noopPromptBackend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error {
	return nil
}
func (*noopPromptBackend) Tools() []tools.Tool                            { return nil }
func (*noopPromptBackend) Hooks() map[skills.HookPhase]skills.HookHandler { return nil }
func (*noopPromptBackend) Commands() []commands.SlashCommand              { return nil }
func (*noopPromptBackend) Agents() []*skills.AgentDefinition              { return nil }
func (*noopPromptBackend) Workflows() map[string]skills.WorkflowHandler   { return nil }
func (*noopPromptBackend) Unload() error                                  { return nil }

type starlarkGitRunnerAdapter struct {
	runner *integrations.GitRunner
}

func (a *starlarkGitRunnerAdapter) Diff(ctx context.Context, args ...string) (string, error) {
	return a.runner.Diff(ctx, args...)
}

func (a *starlarkGitRunnerAdapter) Log(ctx context.Context, args ...string) ([]starengine.GitLogEntry, error) {
	commits, err := a.runner.Log(ctx, args...)
	if err != nil {
		return nil, err
	}
	entries := make([]starengine.GitLogEntry, len(commits))
	for i, c := range commits {
		entries[i] = starengine.GitLogEntry{Hash: c.Hash, Author: c.Author, Message: c.Message}
	}
	return entries, nil
}

func (a *starlarkGitRunnerAdapter) Status(ctx context.Context) ([]starengine.GitStatusEntry, error) {
	statuses, err := a.runner.Status(ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]starengine.GitStatusEntry, len(statuses))
	for i, s := range statuses {
		entries[i] = starengine.GitStatusEntry{Path: s.Path, Status: s.Status}
	}
	return entries, nil
}

// pluginLLMCompleterAdapter bridges integrations.LLMCompleter to the
// goplugin.PluginLLMCompleter interface. The stored context propagates
// cancellation from the parent (e.g. headless timeout) to LLM calls.
type pluginLLMCompleterAdapter struct {
	ctx       context.Context
	completer *integrations.LLMCompleter
}

func (a *pluginLLMCompleterAdapter) Complete(prompt string) (string, error) {
	return a.completer.Complete(a.ctx, prompt)
}

// pluginHTTPFetcherAdapter bridges integrations.HTTPFetcher to the
// goplugin.PluginHTTPFetcher interface.
type pluginHTTPFetcherAdapter struct {
	ctx     context.Context
	fetcher *integrations.HTTPFetcher
}

func (a *pluginHTTPFetcherAdapter) Fetch(url string) (string, error) {
	return a.fetcher.Fetch(a.ctx, url)
}

// pluginGitRunnerAdapter bridges integrations.GitRunner to the
// goplugin.PluginGitRunner interface (skillsdk types).
type pluginGitRunnerAdapter struct {
	ctx    context.Context
	runner *integrations.GitRunner
}

func (a *pluginGitRunnerAdapter) Diff(args ...string) (string, error) {
	return a.runner.Diff(a.ctx, args...)
}

func (a *pluginGitRunnerAdapter) Log(args ...string) ([]skillsdk.GitCommit, error) {
	commits, err := a.runner.Log(a.ctx, args...)
	if err != nil {
		return nil, err
	}
	entries := make([]skillsdk.GitCommit, len(commits))
	for i, c := range commits {
		entries[i] = skillsdk.GitCommit{Hash: c.Hash, Author: c.Author, Message: c.Message}
	}
	return entries, nil
}

func (a *pluginGitRunnerAdapter) Status() ([]skillsdk.GitFileStatus, error) {
	statuses, err := a.runner.Status(a.ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]skillsdk.GitFileStatus, len(statuses))
	for i, s := range statuses {
		entries[i] = skillsdk.GitFileStatus{Path: s.Path, Status: s.Status}
	}
	return entries, nil
}

// pluginSkillInvokerAdapter bridges integrations.SkillInvoker to the
// goplugin.PluginSkillInvoker interface.
type pluginSkillInvokerAdapter struct {
	ctx     context.Context
	invoker *integrations.SkillInvoker
}

func (a *pluginSkillInvokerAdapter) Invoke(name string, input map[string]any) (map[string]any, error) {
	return a.invoker.Invoke(a.ctx, name, input)
}
