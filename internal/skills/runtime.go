package skills

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
)

// BackendFactory creates a SkillBackend from a manifest. Implementations
// choose the correct backend type (Starlark, Go plugin, process) based on the
// manifest's Implementation.Backend field. The dir parameter is the skill's
// directory on disk, needed by backends like Starlark (to locate .star files)
// and Go plugin (to sandbox file operations). The real factory is wired up
// during agent integration; tests supply a mock.
type BackendFactory func(manifest SkillManifest, dir string) (SkillBackend, error)

// SandboxFactory creates a PermissionChecker for a skill. This abstraction
// avoids a circular import between the skills and sandbox packages. The real
// factory calls sandbox.New; tests supply a mock.
type SandboxFactory func(skillName string, declared []Permission) PermissionChecker

// sourcePriority returns the hook dispatch priority for a skill source.
func sourcePriority(src Source) int {
	switch src {
	case SourceBuiltin:
		return PriorityBuiltin
	case SourceUser, SourceInline:
		return PriorityUser
	case SourceProject, SourceConfigured, SourceMCP:
		return PriorityProject
	default:
		return PriorityProject
	}
}

// Runtime is the central orchestrator of the skill system. It ties together
// discovery (Loader), permission enforcement (SandboxFactory), hook dispatch
// (LifecycleManager), tool registration (tools.Registry), and backend
// creation (BackendFactory).
type Runtime struct {
	mu                  sync.RWMutex
	loader              *Loader
	store               *store.Store
	lifecycle           *LifecycleManager
	registry            *tools.Registry
	skills              map[string]*Skill
	active              map[string]*Skill
	autoApprove         []string
	backendFactory      BackendFactory
	sandboxFactory      SandboxFactory
	promptCollector     *PromptCollector
	workflowRunner      *WorkflowRunner
	securityAdapter     *SecurityRuleAdapter
	contextBudget       *ContextBudget
	cmdRegistry         *commands.Registry
	agentDefRegistrar   AgentDefRegistrar
	discoveryWarnings   []string
	activationReports   []ActivationReport
	activationThreshold int
	promptBudgetReport  []PromptFragment
	toolAdmissionFunc   func(toolName string) bool
}

// NewRuntime creates a Runtime with the given dependencies. The autoApprove
// list names skills that bypass store permission checks. The backendFactory
// and sandboxFactory allow the caller to control backend and sandbox creation,
// making the runtime fully testable without import cycles.
func NewRuntime(
	loader *Loader,
	s *store.Store,
	registry *tools.Registry,
	autoApprove []string,
	backendFactory BackendFactory,
	sandboxFactory SandboxFactory,
) *Runtime {
	return &Runtime{
		loader:              loader,
		store:               s,
		lifecycle:           NewLifecycleManager(),
		registry:            registry,
		skills:              make(map[string]*Skill),
		active:              make(map[string]*Skill),
		autoApprove:         autoApprove,
		backendFactory:      backendFactory,
		sandboxFactory:      sandboxFactory,
		promptCollector:     NewPromptCollector(),
		workflowRunner:      NewWorkflowRunner(),
		securityAdapter:     NewSecurityRuleAdapter(),
		activationThreshold: 1,
	}
}

// Discover uses the Loader to find all available skills and stores them in the
// runtime's skill map. Each discovered skill starts in SkillStateInactive.
// The explicit parameter lists skill names explicitly requested (e.g. via --skills).
func (rt *Runtime) Discover(explicit []string) error {
	discovered, warnings, err := rt.loader.Discover(explicit)
	if err != nil {
		return fmt.Errorf("discover skills: %w", err)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.discoveryWarnings = append(rt.discoveryWarnings[:0], warnings...)

	for _, ds := range discovered {
		rt.skills[ds.Manifest.Name] = &Skill{
			Manifest:        ds.Manifest,
			State:           SkillStateInactive,
			Dir:             ds.Dir,
			Source:          ds.Source,
			InstructionBody: ds.InstructionBody,
		}
	}

	return nil
}

// EvaluateAndActivate evaluates triggers against the given context, then
// activates all matching skills that are not yet active.
func (rt *Runtime) EvaluateAndActivate(ctx TriggerContext) error {
	rt.mu.RLock()
	// Build a DiscoveredSkill slice from the current skill map for trigger evaluation.
	var candidates []DiscoveredSkill
	for _, sk := range rt.skills {
		candidates = append(candidates, DiscoveredSkill{
			Manifest: sk.Manifest,
			Dir:      sk.Dir,
			Source:   sk.Source,
			RootDir:  sk.Dir,
		})
	}
	rt.mu.RUnlock()

	reports := EvaluateTriggerReports(candidates, ctx, rt.activationThreshold)

	rt.mu.Lock()
	rt.activationReports = append(rt.activationReports[:0], reports...)
	rt.mu.Unlock()

	for _, report := range reports {
		if !report.Activated {
			continue
		}
		name := report.Skill.Manifest.Name
		rt.mu.RLock()
		_, alreadyActive := rt.active[name]
		rt.mu.RUnlock()
		if alreadyActive {
			continue
		}
		if err := rt.Activate(name); err != nil {
			return fmt.Errorf("activate skill %q: %w", name, err)
		}
	}

	return nil
}

// isAutoApproved returns true if the skill name is in the auto-approve list.
func (rt *Runtime) isAutoApproved(name string) bool {
	for _, n := range rt.autoApprove {
		if n == name {
			return true
		}
	}
	return false
}

// Activate transitions a skill from Inactive to Active. It creates a sandbox,
// a backend, loads the backend, registers tools, and registers hooks. If any
// step fails, the skill transitions to Error state.
//
// Backend creation and loading are performed outside the lock to avoid holding
// the mutex across potentially slow I/O (network, disk, process spawn).
func (rt *Runtime) Activate(name string) error {
	// Phase 1: Read skill metadata and transition to Activating under lock.
	rt.mu.Lock()
	sk, ok := rt.skills[name]
	if !ok {
		rt.mu.Unlock()
		return fmt.Errorf("skill %q not found", name)
	}

	// Guard against concurrent activation: if another goroutine already
	// activated or is activating this skill, return early.
	if _, alreadyActive := rt.active[name]; alreadyActive {
		rt.mu.Unlock()
		return nil
	}

	if err := sk.TransitionTo(SkillStateActivating); err != nil {
		rt.mu.Unlock()
		return fmt.Errorf("activate skill %q: %w", name, err)
	}

	// Snapshot the data we need outside the lock.
	manifest := *sk.Manifest
	permissions := sk.Manifest.Permissions
	source := sk.Source
	skillDir := sk.Dir
	autoApproved := rt.isAutoApproved(name)
	sandboxFactory := rt.sandboxFactory
	backendFactory := rt.backendFactory
	rt.mu.Unlock()

	// Phase 2: Create sandbox and backend outside the lock (may involve I/O).
	sb := sandboxFactory(name, permissions)

	if !autoApproved {
		for _, perm := range permissions {
			if err := sb.CheckPermission(perm); err != nil {
				rt.mu.Lock()
				_ = sk.TransitionTo(SkillStateError)
				_ = sk.TransitionTo(SkillStateInactive)
				rt.mu.Unlock()
				return fmt.Errorf("activate skill %q: %w", name, err)
			}
		}
	}

	backend, err := backendFactory(manifest, skillDir)
	if err != nil {
		rt.mu.Lock()
		_ = sk.TransitionTo(SkillStateError)
		_ = sk.TransitionTo(SkillStateInactive)
		rt.mu.Unlock()
		return fmt.Errorf("create backend for skill %q: %w", name, err)
	}

	if err := backend.Load(manifest, sb); err != nil {
		rt.mu.Lock()
		_ = sk.TransitionTo(SkillStateError)
		_ = sk.TransitionTo(SkillStateInactive)
		rt.mu.Unlock()
		return fmt.Errorf("load skill %q: %w", name, err)
	}

	// After a successful Load, any error must call backend.Unload() to release
	// resources (e.g. MCP child processes, network connections).
	var activated bool
	defer func() {
		if !activated {
			_ = backend.Unload()
		}
	}()

	// Phase 3: Register tools, hooks, and integrations under lock.
	// Wrap all backend tools with a capability broker so that per-call
	// permission enforcement applies uniformly — including process and
	// MCP backends that don't self-enforce.
	broker := NewCapabilityBroker(name, sb, permissions)

	rt.mu.Lock()
	defer rt.mu.Unlock()

	var registeredTools []tools.Tool
	for _, tool := range backend.Tools() {
		// Apply admission policy — skip tools that fail the gate.
		if rt.toolAdmissionFunc != nil && !rt.toolAdmissionFunc(tool.Name()) {
			continue
		}
		tool = NewBrokeredTool(tool, broker)
		if err := rt.registry.Register(tool); err != nil {
			for _, t := range registeredTools {
				_ = rt.registry.Unregister(t.Name())
			}
			_ = sk.TransitionTo(SkillStateError)
			_ = sk.TransitionTo(SkillStateInactive)
			return fmt.Errorf("register tool for skill %q: %w", name, err)
		}
		registeredTools = append(registeredTools, tool)
	}

	// Register commands from backend.
	var registeredCmds []commands.SlashCommand
	if rt.cmdRegistry != nil {
		for _, cmd := range backend.Commands() {
			if err := rt.cmdRegistry.Register(cmd); err != nil {
				// Roll back commands registered in this activation attempt.
				for _, c := range registeredCmds {
					_ = rt.cmdRegistry.Unregister(c.Name())
				}
				// Rollback tools on command registration failure.
				for _, t := range registeredTools {
					_ = rt.registry.Unregister(t.Name())
				}
				_ = sk.TransitionTo(SkillStateError)
				_ = sk.TransitionTo(SkillStateInactive)
				return fmt.Errorf("register command for skill %q: %w", name, err)
			}
			registeredCmds = append(registeredCmds, cmd)
		}
		for _, cmd := range manifestCommands(sk.Manifest) {
			if err := rt.cmdRegistry.Register(cmd); err != nil {
				for _, c := range registeredCmds {
					_ = rt.cmdRegistry.Unregister(c.Name())
				}
				for _, t := range registeredTools {
					_ = rt.registry.Unregister(t.Name())
				}
				_ = sk.TransitionTo(SkillStateError)
				_ = sk.TransitionTo(SkillStateInactive)
				return fmt.Errorf("register command for skill %q: %w", name, err)
			}
			registeredCmds = append(registeredCmds, cmd)
		}
	}

	// Register agent definitions from backend.
	var registeredAgentDefs []string
	if rt.agentDefRegistrar != nil {
		for _, def := range backend.Agents() {
			if err := rt.agentDefRegistrar.Register(def); err != nil {
				// Roll back agent defs.
				for _, n := range registeredAgentDefs {
					_ = rt.agentDefRegistrar.Unregister(n)
				}
				// Roll back commands.
				if rt.cmdRegistry != nil {
					for _, c := range registeredCmds {
						_ = rt.cmdRegistry.Unregister(c.Name())
					}
				}
				// Roll back tools.
				for _, t := range registeredTools {
					_ = rt.registry.Unregister(t.Name())
				}
				_ = sk.TransitionTo(SkillStateError)
				_ = sk.TransitionTo(SkillStateInactive)
				return fmt.Errorf("register agent def for skill %q: %w", name, err)
			}
			registeredAgentDefs = append(registeredAgentDefs, def.Name)
		}
		for _, def := range manifestAgentDefinitions(sk.Manifest) {
			if err := rt.agentDefRegistrar.Register(def); err != nil {
				for _, n := range registeredAgentDefs {
					_ = rt.agentDefRegistrar.Unregister(n)
				}
				if rt.cmdRegistry != nil {
					for _, c := range registeredCmds {
						_ = rt.cmdRegistry.Unregister(c.Name())
					}
				}
				for _, t := range registeredTools {
					_ = rt.registry.Unregister(t.Name())
				}
				_ = sk.TransitionTo(SkillStateError)
				_ = sk.TransitionTo(SkillStateInactive)
				return fmt.Errorf("register agent def for skill %q: %w", name, err)
			}
			registeredAgentDefs = append(registeredAgentDefs, def.Name)
		}
	}

	priority := sourcePriority(source)
	for phase, handler := range backend.Hooks() {
		rt.lifecycle.Register(phase, name, priority, handler)
	}

	for _, st := range sk.Manifest.Types {
		switch st {
		case SkillTypeTool:
			// Tools are already registered above via backend.Tools().
		case SkillTypePrompt:
			wirePromptSkill(rt, sk)
		case SkillTypeWorkflow:
			wireWorkflowSkill(rt, sk)
		case SkillTypeSecurityRule:
			wireSecurityRuleSkill(rt, sk)
		case SkillTypeTransform:
			wireTransformSkill(rt, sk)
		}
	}

	sk.Backend = backend
	if err := sk.TransitionTo(SkillStateActive); err != nil {
		return fmt.Errorf("activate skill %q: %w", name, err)
	}

	rt.active[name] = sk
	activated = true
	return nil
}

// Deactivate transitions a skill from Active to Inactive. It unregisters
// tools, unregisters hooks, calls backend.Unload, and clears the backend.
func (rt *Runtime) Deactivate(name string) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	sk, ok := rt.active[name]
	if !ok {
		return fmt.Errorf("skill %q is not active", name)
	}

	// Unregister tools.
	if sk.Backend != nil {
		for _, tool := range sk.Backend.Tools() {
			_ = rt.registry.Unregister(tool.Name())
		}
	}

	// Unregister commands.
	if rt.cmdRegistry != nil && sk.Backend != nil {
		for _, cmd := range sk.Backend.Commands() {
			_ = rt.cmdRegistry.Unregister(cmd.Name())
		}
	}
	if rt.cmdRegistry != nil {
		for _, cmd := range manifestCommands(sk.Manifest) {
			_ = rt.cmdRegistry.Unregister(cmd.Name())
		}
	}

	// Unregister agent definitions.
	if rt.agentDefRegistrar != nil && sk.Backend != nil {
		for _, def := range sk.Backend.Agents() {
			_ = rt.agentDefRegistrar.Unregister(def.Name)
		}
	}
	if rt.agentDefRegistrar != nil {
		for _, def := range manifestAgentDefinitions(sk.Manifest) {
			_ = rt.agentDefRegistrar.Unregister(def.Name)
		}
	}

	// Unregister hooks.
	rt.lifecycle.Unregister(name)

	// Clean up integration state for all skill types.
	for _, st := range sk.Manifest.Types {
		switch st {
		case SkillTypePrompt:
			rt.promptCollector.RemoveBySkill(name)
		case SkillTypeWorkflow:
			rt.workflowRunner.Unregister(name)
		case SkillTypeSecurityRule:
			rt.securityAdapter.UnregisterBySkill(name)
		}
	}

	// Unload backend. Even if Unload fails, we still transition to Inactive
	// and remove from active map to avoid a stuck skill.
	var unloadErr error
	if sk.Backend != nil {
		unloadErr = sk.Backend.Unload()
	}

	// Always transition to Inactive and clean up regardless of unload error.
	_ = sk.TransitionTo(SkillStateInactive)
	sk.Backend = nil
	delete(rt.active, name)

	if unloadErr != nil {
		return fmt.Errorf("unload skill %q: %w", name, unloadErr)
	}

	return nil
}

// GetActiveSkills returns a copy of the currently active skills.
func (rt *Runtime) GetActiveSkills() []*Skill {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	result := make([]*Skill, 0, len(rt.active))
	for _, sk := range rt.active {
		result = append(result, sk)
	}
	return result
}

// SetActivationThreshold configures the minimum score required for automatic activation.
func (rt *Runtime) SetActivationThreshold(threshold int) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if threshold <= 0 {
		threshold = 1
	}
	rt.activationThreshold = threshold
}

// GetActivationReports returns the most recent scored trigger evaluation results.
func (rt *Runtime) GetActivationReports() []ActivationReport {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	result := make([]ActivationReport, len(rt.activationReports))
	copy(result, rt.activationReports)
	return result
}

// GetActivationReport returns the most recent scored trigger evaluation for a named skill.
func (rt *Runtime) GetActivationReport(name string) (ActivationReport, bool) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	for _, report := range rt.activationReports {
		if report.Skill.Manifest != nil && report.Skill.Manifest.Name == name {
			return report, true
		}
	}
	return ActivationReport{}, false
}

// GetDiscoveryWarnings returns optional discovery warnings captured during the
// last Discover call, such as missing optional dependencies.
func (rt *Runtime) GetDiscoveryWarnings() []string {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	result := make([]string, len(rt.discoveryWarnings))
	copy(result, rt.discoveryWarnings)
	return result
}

// GetPromptFragments returns the prompt fragments collected from all active
// prompt skills.
func (rt *Runtime) GetPromptFragments() []PromptFragment {
	return rt.promptCollector.Fragments()
}

// InvokeWorkflow executes a named workflow handler. Returns an error if no
// workflow is registered with the given name.
func (rt *Runtime) InvokeWorkflow(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	return rt.workflowRunner.Invoke(ctx, name, args)
}

// DispatchHook sends a hook event to all registered handlers for the event's
// phase via the lifecycle manager. This is the public entry point used by
// the agent loop to fire hooks at key points (before tool call, after tool
// result, etc.).
func (rt *Runtime) DispatchHook(event HookEvent) (*HookResult, error) {
	return rt.lifecycle.Dispatch(event)
}

// RegisterHook adds a hook handler to the lifecycle manager at the given priority.
func (rt *Runtime) RegisterHook(phase HookPhase, name string, priority int, handler HookHandler) {
	rt.lifecycle.Register(phase, name, priority, handler)
}

// GetScanners returns the registered security scanners from all active
// security-rule skills.
func (rt *Runtime) GetScanners() []RegisteredScanner {
	return rt.securityAdapter.Scanners()
}

// GetSkillIndexes returns lightweight index information for all discovered
// skills (both active and inactive). This is designed for system prompt
// building where only name + description + types are needed, avoiding
// exposure of full manifests.
func (rt *Runtime) GetSkillIndexes() []SkillIndex {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	indexes := make([]SkillIndex, 0, len(rt.skills))
	for _, sk := range rt.skills {
		indexes = append(indexes, NewSkillIndex(sk.Manifest, sk.Source, sk.Dir))
	}
	return indexes
}

// GetAllSkillSummaries returns a summary of all discovered skills with their
// current state, sorted by name for deterministic output.
func (rt *Runtime) GetAllSkillSummaries() []SkillSummary {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	summaries := make([]SkillSummary, 0, len(rt.skills))
	for _, sk := range rt.skills {
		typesCopy := make([]SkillType, len(sk.Manifest.Types))
		copy(typesCopy, sk.Manifest.Types)
		summaries = append(summaries, SkillSummary{
			Name:        sk.Manifest.Name,
			Description: sk.Manifest.Description,
			Source:      sk.Source,
			State:       sk.State,
			Types:       typesCopy,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})

	return summaries
}

// SetContextBudget configures the global context budget for prompt fragments.
// Pass nil to disable budget enforcement.
func (rt *Runtime) SetContextBudget(budget *ContextBudget) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.contextBudget = budget
}

// SetCommandRegistry sets the command registry for skill-contributed commands.
func (rt *Runtime) SetCommandRegistry(reg *commands.Registry) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.cmdRegistry = reg
}

// SetAgentDefRegistrar sets the agent definition registrar for the runtime.
func (rt *Runtime) SetAgentDefRegistrar(reg AgentDefRegistrar) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.agentDefRegistrar = reg
}

// SetToolAdmissionFunc sets a function that gates which skill-contributed tools
// are registered into the tool registry. Tools whose names fail admission are
// silently skipped, applying the same policy path used for built-in tools.
func (rt *Runtime) SetToolAdmissionFunc(fn func(toolName string) bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.toolAdmissionFunc = fn
}

// GetBudgetedPromptFragments returns prompt fragments constrained by the
// configured context budget. If no budget is set, it returns all fragments.
func (rt *Runtime) GetBudgetedPromptFragments() []PromptFragment {
	rt.mu.RLock()
	budget := rt.contextBudget
	reports := make([]ActivationReport, len(rt.activationReports))
	copy(reports, rt.activationReports)
	rt.mu.RUnlock()

	rt.promptCollector.UpdateActivationScores(reports)
	fragments := rt.promptCollector.BudgetedFragments(budget)
	report := rt.promptCollector.BudgetReport(budget)

	rt.mu.Lock()
	rt.promptBudgetReport = append(rt.promptBudgetReport[:0], report...)
	rt.mu.Unlock()

	return fragments
}

// GetPromptBudgetReport returns the most recent prompt budgeting decisions.
func (rt *Runtime) GetPromptBudgetReport() []PromptFragment {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	result := make([]PromptFragment, len(rt.promptBudgetReport))
	copy(result, rt.promptBudgetReport)
	return result
}
