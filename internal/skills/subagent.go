package skills

import "sort"

// SubagentSkillPolicy controls which currently active skills are visible to a child agent.
type SubagentSkillPolicy struct {
	InheritActive bool
	Include       []string
	Exclude       []string
}

// SnapshotForSubagent creates an isolated runtime view containing only the selected
// active skills from the parent runtime. The snapshot shares immutable backend
// references and hook handlers with the parent but has its own prompt collector,
// lifecycle registry, and active-skill maps.
func (rt *Runtime) SnapshotForSubagent(policy SubagentSkillPolicy) *Runtime {
	if rt == nil {
		return nil
	}

	rt.mu.RLock()
	defer rt.mu.RUnlock()

	selected := make(map[string]struct{})
	if policy.InheritActive {
		for name := range rt.active {
			selected[name] = struct{}{}
		}
	}
	for _, name := range policy.Include {
		if _, ok := rt.active[name]; ok {
			selected[name] = struct{}{}
		}
	}
	for _, name := range policy.Exclude {
		delete(selected, name)
	}
	if len(selected) == 0 {
		return nil
	}

	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)

	child := &Runtime{
		lifecycle:           NewLifecycleManager(),
		skills:              make(map[string]*Skill, len(names)),
		active:              make(map[string]*Skill, len(names)),
		promptCollector:     NewPromptCollector(),
		workflowRunner:      NewWorkflowRunner(),
		securityAdapter:     NewSecurityRuleAdapter(),
		activationThreshold: rt.activationThreshold,
	}

	for _, report := range rt.activationReports {
		if report.Skill.Manifest == nil {
			continue
		}
		if _, ok := selected[report.Skill.Manifest.Name]; ok {
			child.activationReports = append(child.activationReports, report)
		}
	}

	for _, name := range names {
		sk := rt.active[name]
		if sk == nil || sk.Manifest == nil {
			continue
		}

		manifestCopy := *sk.Manifest
		skillCopy := &Skill{
			Manifest:        &manifestCopy,
			State:           SkillStateActive,
			Dir:             sk.Dir,
			Source:          sk.Source,
			Backend:         sk.Backend,
			InstructionBody: sk.InstructionBody,
		}
		child.skills[name] = skillCopy
		child.active[name] = skillCopy

		if skillCopy.Backend != nil {
			priority := sourcePriority(skillCopy.Source)
			for phase, handler := range skillCopy.Backend.Hooks() {
				child.lifecycle.Register(phase, name, priority, handler)
			}
		}

		for _, st := range skillCopy.Manifest.Types {
			switch st {
			case SkillTypePrompt:
				wirePromptSkill(child, skillCopy)
			case SkillTypeWorkflow:
				wireWorkflowSkill(child, skillCopy)
			case SkillTypeSecurityRule:
				wireSecurityRuleSkill(child, skillCopy)
			case SkillTypeTransform:
				wireTransformSkill(child, skillCopy)
			}
		}
	}

	return child
}
