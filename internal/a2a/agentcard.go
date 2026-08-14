package a2a

import (
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
)

// BuildAgentCard walks reg and activeSkills and emits one Skill per
// registered tool plus one per active skill, so the card this instance
// advertises always reflects everything it can actually do — not just its
// built-in tools. activeSkills may be nil (e.g. no skill runtime, or skill
// discovery found nothing); the card simply advertises zero skill-derived
// entries in that case.
func BuildAgentCard(reg *tools.Registry, activeSkills []*skills.Skill, name, description, url string) AgentCard {
	toolList := reg.Tools()
	advertised := make([]Skill, 0, len(toolList)+len(activeSkills))
	for _, t := range toolList {
		advertised = append(advertised, Skill{
			ID:          t.Name(),
			Name:        t.Name(),
			Description: t.Description(),
		})
	}
	for _, sk := range activeSkills {
		if sk == nil || sk.Manifest == nil {
			continue
		}
		advertised = append(advertised, Skill{
			ID:          sk.Manifest.Name,
			Name:        sk.Manifest.Name,
			Description: sk.Manifest.Description,
			Tags:        sk.Manifest.Tags,
		})
	}
	return AgentCard{
		Name:         name,
		Description:  description,
		URL:          url,
		Skills:       advertised,
		Capabilities: Capabilities{Streaming: true},
	}
}
