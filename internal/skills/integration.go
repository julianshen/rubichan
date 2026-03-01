package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// SecurityFinding represents a single finding from a security scanner.
type SecurityFinding struct {
	Rule     string
	Message  string
	Severity string
}

// ScannerFunc is the function signature for security scanner implementations.
type ScannerFunc func(ctx context.Context, content string) ([]SecurityFinding, error)

// RegisteredScanner holds metadata and the scan function for a registered scanner.
type RegisteredScanner struct {
	SkillName string
	Name      string
	Scan      ScannerFunc
}

// SecurityRuleAdapter stores scanner registrations. It acts as an adapter
// between the skill system and the security engine, allowing security-rule
// skills to register custom scanners without depending on the security engine
// directly.
type SecurityRuleAdapter struct {
	mu       sync.RWMutex
	scanners []RegisteredScanner
}

// NewSecurityRuleAdapter creates a new SecurityRuleAdapter.
func NewSecurityRuleAdapter() *SecurityRuleAdapter {
	return &SecurityRuleAdapter{}
}

// RegisterScanner adds a scanner function associated with a skill.
func (a *SecurityRuleAdapter) RegisterScanner(skillName, scannerName string, fn ScannerFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.scanners = append(a.scanners, RegisteredScanner{
		SkillName: skillName,
		Name:      scannerName,
		Scan:      fn,
	})
}

// Scanners returns all registered scanners.
func (a *SecurityRuleAdapter) Scanners() []RegisteredScanner {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]RegisteredScanner, len(a.scanners))
	copy(result, a.scanners)
	return result
}

// UnregisterBySkill removes all scanners registered by the given skill name.
func (a *SecurityRuleAdapter) UnregisterBySkill(skillName string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var filtered []RegisteredScanner
	for _, s := range a.scanners {
		if s.SkillName != skillName {
			filtered = append(filtered, s)
		}
	}
	a.scanners = filtered
}

// PromptFragment holds the prompt configuration for an active prompt skill.
type PromptFragment struct {
	SkillName        string
	SystemPromptFile string // original file path from manifest
	ResolvedPrompt   string // content read from SystemPromptFile
	ContextFiles     []string
	MaxContextTokens int
	Source           Source // discovery source, used for budget priority
}

// PromptCollector gathers prompt fragments from active prompt skills and
// registers lifecycle hooks that inject prompt data during prompt building.
type PromptCollector struct {
	mu        sync.RWMutex
	fragments []PromptFragment
}

// NewPromptCollector creates a new PromptCollector.
func NewPromptCollector() *PromptCollector {
	return &PromptCollector{}
}

// Add registers a prompt fragment for a skill.
func (pc *PromptCollector) Add(fragment PromptFragment) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.fragments = append(pc.fragments, fragment)
}

// Fragments returns all registered prompt fragments.
func (pc *PromptCollector) Fragments() []PromptFragment {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	result := make([]PromptFragment, len(pc.fragments))
	copy(result, pc.fragments)
	return result
}

// BudgetedFragments returns prompt fragments that fit within the given budget.
// Fragments are sorted by source priority (highest first). If a budget is nil
// or has zero MaxTotalTokens, all fragments are returned unchanged.
// When over budget, lower-priority fragments are excluded first. A fragment
// that partially fits is truncated to use the remaining budget.
func (pc *PromptCollector) BudgetedFragments(budget *ContextBudget) []PromptFragment {
	pc.mu.RLock()
	all := make([]PromptFragment, len(pc.fragments))
	copy(all, pc.fragments)
	pc.mu.RUnlock()

	if budget == nil || budget.MaxTotalTokens <= 0 {
		return all
	}

	// Sort by source priority descending (highest priority first).
	sort.SliceStable(all, func(i, j int) bool {
		return sourceBudgetPriority(all[i].Source) > sourceBudgetPriority(all[j].Source)
	})

	var result []PromptFragment
	remainingTokens := budget.MaxTotalTokens

	for _, f := range all {
		if remainingTokens <= 0 {
			break
		}

		prompt := f.ResolvedPrompt

		// Apply per-skill limit.
		if budget.MaxPerSkillTokens > 0 {
			perSkillTokens := estimateTokens(prompt)
			if perSkillTokens > budget.MaxPerSkillTokens {
				// Truncate to per-skill limit.
				maxChars := budget.MaxPerSkillTokens * 4
				if maxChars < len(prompt) {
					prompt = prompt[:maxChars]
				}
			}
		}

		tokens := estimateTokens(prompt)
		if tokens <= remainingTokens {
			f.ResolvedPrompt = prompt
			result = append(result, f)
			remainingTokens -= tokens
		} else {
			// Partial fit: truncate to remaining budget.
			maxChars := remainingTokens * 4
			if maxChars > 0 && maxChars <= len(prompt) {
				f.ResolvedPrompt = prompt[:maxChars]
				result = append(result, f)
			} else if maxChars > len(prompt) {
				f.ResolvedPrompt = prompt
				result = append(result, f)
			}
			remainingTokens = 0
		}
	}

	return result
}

// RemoveBySkill removes the prompt fragment for the given skill name.
func (pc *PromptCollector) RemoveBySkill(skillName string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	var filtered []PromptFragment
	for _, f := range pc.fragments {
		if f.SkillName != skillName {
			filtered = append(filtered, f)
		}
	}
	pc.fragments = filtered
}

// WorkflowHandler is the function signature for workflow implementations.
type WorkflowHandler func(ctx context.Context, args map[string]any) (map[string]any, error)

// WorkflowRunner stores and executes named workflow handlers registered by
// workflow skills.
type WorkflowRunner struct {
	mu        sync.RWMutex
	workflows map[string]WorkflowHandler
}

// NewWorkflowRunner creates a new WorkflowRunner.
func NewWorkflowRunner() *WorkflowRunner {
	return &WorkflowRunner{
		workflows: make(map[string]WorkflowHandler),
	}
}

// Register stores a workflow handler under the given name.
func (wr *WorkflowRunner) Register(name string, handler WorkflowHandler) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.workflows[name] = handler
}

// Invoke executes the workflow handler registered under the given name.
// Returns an error if no workflow is registered with that name.
func (wr *WorkflowRunner) Invoke(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	wr.mu.RLock()
	handler, ok := wr.workflows[name]
	wr.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", name)
	}
	return handler(ctx, args)
}

// Unregister removes the workflow handler for the given name.
func (wr *WorkflowRunner) Unregister(name string) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	delete(wr.workflows, name)
}

// wirePromptSkill registers a HookOnBeforePromptBuild hook for a prompt skill
// that injects the skill's prompt fragment into the event data. It reads the
// SystemPromptFile content at wiring time so the agent doesn't need to do
// file I/O during prompt building. For instruction skills (SKILL.md), the
// InstructionBody is used directly as the resolved prompt.
func wirePromptSkill(rt *Runtime, sk *Skill) {
	fragment := PromptFragment{
		SkillName:        sk.Manifest.Name,
		SystemPromptFile: sk.Manifest.Prompt.SystemPromptFile,
		ContextFiles:     sk.Manifest.Prompt.ContextFiles,
		MaxContextTokens: sk.Manifest.Prompt.MaxContextTokens,
		Source:           sk.Source,
	}

	// Instruction skills (SKILL.md) have their body pre-parsed.
	if sk.InstructionBody != "" {
		fragment.ResolvedPrompt = sk.InstructionBody
	} else if sk.Manifest.Prompt.SystemPromptFile != "" {
		// Read the system prompt file content if a file path is specified and the
		// skill has a directory on disk. For built-in skills (Dir=""), the
		// SystemPromptFile value is used as inline content.
		if sk.Dir != "" {
			promptPath := filepath.Join(sk.Dir, sk.Manifest.Prompt.SystemPromptFile)
			data, err := os.ReadFile(promptPath)
			if err != nil {
				fragment.ResolvedPrompt = fmt.Sprintf("[error reading prompt file %q: %s]", promptPath, err)
			} else {
				fragment.ResolvedPrompt = string(data)
			}
		} else {
			// Built-in skills without a directory use SystemPromptFile as
			// inline content (no file to read).
			fragment.ResolvedPrompt = sk.Manifest.Prompt.SystemPromptFile
		}
	}

	rt.promptCollector.Add(fragment)

	priority := sourcePriority(sk.Source)
	rt.lifecycle.Register(HookOnBeforePromptBuild, sk.Manifest.Name, priority,
		func(event HookEvent) (HookResult, error) {
			return HookResult{
				Modified: map[string]any{
					"prompt_fragment": fragment.ResolvedPrompt,
				},
			}, nil
		},
	)
}

// wireSecurityRuleSkill wires up scanners from the manifest's SecurityRules config.
func wireSecurityRuleSkill(rt *Runtime, sk *Skill) {
	// The actual scanner functions are registered externally via
	// rt.securityAdapter.RegisterScanner(). Here we just ensure the skill
	// type is acknowledged during activation. Scanners may be pre-registered
	// by the backend or by the caller before activation.
}

// wireTransformSkill registers hooks from the backend that handle the
// transform type. The backend already provides HookOnAfterResponse handlers
// which are registered in the standard hook registration path in Activate.
func wireTransformSkill(rt *Runtime, sk *Skill) {
	// Transform skills rely on the backend providing HookOnAfterResponse hooks.
	// These are already registered in the standard Activate path via
	// backend.Hooks(). No additional wiring needed.
}

// wireWorkflowSkill notes that the skill is a workflow type. The actual
// workflow handler registration is done externally via
// rt.workflowRunner.Register().
func wireWorkflowSkill(rt *Runtime, sk *Skill) {
	// Workflow handlers are registered externally. The activation path
	// acknowledges the workflow type here.
}
