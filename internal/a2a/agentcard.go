package a2a

import "github.com/julianshen/rubichan/internal/tools"

// BuildAgentCard walks reg and emits one Skill per registered tool, so the
// card this instance advertises always reflects what it can actually do.
func BuildAgentCard(reg *tools.Registry, name, description, url string) AgentCard {
	toolList := reg.Tools()
	skills := make([]Skill, 0, len(toolList))
	for _, t := range toolList {
		skills = append(skills, Skill{
			ID:          t.Name(),
			Name:        t.Name(),
			Description: t.Description(),
		})
	}
	return AgentCard{
		Name:         name,
		Description:  description,
		URL:          url,
		Skills:       skills,
		Capabilities: Capabilities{Streaming: true},
	}
}
