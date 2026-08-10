package a2a

import "strings"

// Registry holds known agents' Agent Cards and does simple, deterministic
// capability matching — no LLM call, so dispatch decisions stay cheap and
// reproducible.
type Registry struct {
	agents map[string]AgentCard
	order  []string // insertion order, for deterministic Match output
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]AgentCard)}
}

// Add registers card under name, overwriting any existing entry.
func (r *Registry) Add(name string, card AgentCard) {
	if _, exists := r.agents[name]; !exists {
		r.order = append(r.order, name)
	}
	r.agents[name] = card
}

// Match scores every registered agent's skill tags and descriptions
// against taskDescription by substring/word overlap, returning agent
// names best-first. An agent with no overlapping words is excluded, not
// scored zero and returned last — a caller asking for the "best match"
// should get "no match" rather than an arbitrary agent.
func (r *Registry) Match(taskDescription string) []string {
	words := wordSet(taskDescription)

	type scored struct {
		name  string
		score int
	}
	var results []scored
	for _, name := range r.order {
		card := r.agents[name]
		score := 0
		for _, skill := range card.Skills {
			for _, tag := range skill.Tags {
				if words[strings.ToLower(tag)] {
					score++
				}
			}
			for w := range wordSet(skill.Description) {
				if words[w] {
					score++
				}
			}
		}
		if score > 0 {
			results = append(results, scored{name, score})
		}
	}

	// Stable sort by score descending, preserving insertion order on ties.
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].score > results[j-1].score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	names := make([]string, len(results))
	for i, s := range results {
		names[i] = s.name
	}
	return names
}

func wordSet(s string) map[string]bool {
	out := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(s)) {
		out[strings.Trim(w, ".,!?'\"")] = true
	}
	return out
}
