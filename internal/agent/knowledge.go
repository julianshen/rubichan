package agent

import (
	"strings"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

// WithKnowledgeGraph attaches a knowledge graph context selector
// for automatic knowledge injection into the agent's system prompt.
func WithKnowledgeGraph(selector kg.ContextSelector) AgentOption {
	return func(a *Agent) {
		a.knowledgeSelector = selector
	}
}

// renderKnowledgeSection formats knowledge entities for inclusion in the system prompt.
// Returns a markdown-formatted section with entity titles, bodies, and relationships.
func renderKnowledgeSection(entities []kg.ScoredEntity) string {
	if len(entities) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Project Knowledge\n\n")

	for i, se := range entities {
		if i > 0 {
			sb.WriteString("\n")
		}

		// Entity header with kind, title, and ID
		sb.WriteString("### [")
		sb.WriteString(string(se.Entity.Kind))
		sb.WriteString("] ")
		sb.WriteString(se.Entity.Title)
		sb.WriteString(" (")
		sb.WriteString(se.Entity.ID)
		sb.WriteString(")\n\n")

		// Entity body
		sb.WriteString(se.Entity.Body)
		sb.WriteString("\n")

		// Relationships
		if len(se.Entity.Relationships) > 0 {
			sb.WriteString("\n**Relationships**: ")
			for j, rel := range se.Entity.Relationships {
				if j > 0 {
					sb.WriteString("; ")
				}
				sb.WriteString(string(rel.Kind))
				sb.WriteString(": ")
				sb.WriteString(rel.Target)
			}
			sb.WriteString("\n")
		}

		// Score and tokens (for debugging)
		if se.Score > 0 {
			sb.WriteString("\n")
		}
	}

	return strings.TrimSpace(sb.String())
}
