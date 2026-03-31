package shell

import (
	"context"
	"strings"
	"sync"
)

// HintProvider generates argument hints via the LLM.
type HintProvider struct {
	agentTurn AgentTurnFunc
	cache     map[string][]Completion
	pending   map[string]bool // tracks in-flight requests
	mu        sync.RWMutex
}

// NewHintProvider creates a hint provider.
func NewHintProvider(agentTurn AgentTurnFunc) *HintProvider {
	return &HintProvider{
		agentTurn: agentTurn,
		cache:     make(map[string][]Completion),
		pending:   make(map[string]bool),
	}
}

const hintPrompt = `Given the partial command line:
%s

List the most likely flag or argument completions, one per line.
Output ONLY the flag/argument names, nothing else. Example:
--verbose
--quiet
--output`

// Hint returns argument hints for the current input. Non-blocking; returns
// cached results or empty if no hint is available yet. Triggers background
// LLM call if cache miss.
func (hp *HintProvider) Hint(input string) []Completion {
	if hp.agentTurn == nil {
		return nil
	}

	key := strings.TrimSpace(input)
	if key == "" {
		return nil
	}

	// Check cache
	hp.mu.RLock()
	if results, ok := hp.cache[key]; ok {
		hp.mu.RUnlock()
		return results
	}
	isPending := hp.pending[key]
	hp.mu.RUnlock()

	// Don't duplicate in-flight requests
	if isPending {
		return nil
	}

	// Mark as pending and trigger background LLM call
	hp.mu.Lock()
	hp.pending[key] = true
	hp.mu.Unlock()

	go hp.fetchHints(key)

	return nil
}

func (hp *HintProvider) fetchHints(key string) {
	prompt := strings.Replace(hintPrompt, "%s", key, 1)

	ctx := context.Background()
	events, err := hp.agentTurn(ctx, prompt)
	if err != nil {
		hp.mu.Lock()
		delete(hp.pending, key)
		hp.mu.Unlock()
		return
	}

	var response strings.Builder
	for event := range events {
		if event.Type == "text_delta" {
			response.WriteString(event.Text)
		}
	}

	// Parse response: one completion per line
	var results []Completion
	for _, line := range strings.Split(response.String(), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			results = append(results, Completion{Text: trimmed})
		}
	}

	hp.mu.Lock()
	hp.cache[key] = results
	delete(hp.pending, key)
	hp.mu.Unlock()
}
