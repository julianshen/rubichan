// Package a2atools exposes A2A (Agent2Agent) protocol operations as
// ordinary tools in rubichan's tool registry, mirroring internal/tools/mcp's
// tool_wrapper.go — the agent loop sees a2a_discover/a2a_call/a2a_dispatch
// as tools like any other, with no special-cased handling.
package a2atools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// DiscoverTool fetches an A2A agent's Agent Card.
type DiscoverTool struct {
	cfg        *config.Config
	httpClient *http.Client // overridable in tests; nil means defaultA2AHTTPClient (SSRF-guarded)
}

// NewDiscoverTool creates a DiscoverTool that resolves configured agent
// names against cfg.A2A.Agents.
func NewDiscoverTool(cfg *config.Config) *DiscoverTool {
	return &DiscoverTool{cfg: cfg}
}

func (d *DiscoverTool) Name() string { return "a2a_discover" }

func (d *DiscoverTool) Description() string {
	return "Fetch an A2A (Agent2Agent) agent's Agent Card, listing its name, " +
		"description, and skills. Pass a configured agent name or a raw URL."
}

func (d *DiscoverTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"agent": {
				"type": "string",
				"description": "A configured agent name (from [[a2a.agents]]) or a raw base URL"
			}
		},
		"required": ["agent"]
	}`)
}

type discoverInput struct {
	Agent string `json:"agent"`
}

// resolveAgentURL resolves ref against cfg.A2A.Agents by name, falling back
// to treating ref as a raw URL. apiKey is empty unless ref matched a
// configured entry with credentials.
func resolveAgentURL(cfg *config.Config, ref string) (url, apiKey string, err error) {
	for _, a := range cfg.A2A.Agents {
		if a.Name == ref {
			// config.OpenAICompatibleEnvVar's name-to-env-var transform
			// (uppercase, non-alnum -> underscore, "_API_KEY" suffix) isn't
			// actually OpenAI-specific — it's how this codebase derives a
			// canonical env var name from any configured entry name, so
			// "research-bot" resolves against RESEARCH_BOT_API_KEY the same
			// way an OpenAI-compatible entry named "openrouter" resolves
			// against OPENROUTER_API_KEY. Without this, an agent configured
			// with api_key_source = "env" would always fail to resolve: an
			// empty env var name makes config.ResolveAPIKey's "env" case
			// reject immediately, regardless of what's actually set in the
			// environment.
			key, kerr := config.ResolveAPIKey(a.APIKeySource, a.APIKey, config.OpenAICompatibleEnvVar(a.Name))
			if kerr != nil {
				key = "" // credential resolution failing is not fatal for discovery
			}
			return a.URL, key, nil
		}
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "mem://") {
		return ref, "", nil
	}
	return "", "", fmt.Errorf("agent %q is not a configured [[a2a.agents]] name and not a URL", ref)
}

func (d *DiscoverTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	var in discoverInput
	if err := json.Unmarshal(input, &in); err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	url, apiKey, err := resolveAgentURL(d.cfg, in.Agent)
	if err != nil {
		return agentsdk.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client := a2a.NewClient(url, apiKey, httpClientOrDefault(d.httpClient))
	card, err := client.Discover(ctx)
	if err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("discover failed: %v", err), IsError: true}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Agent: %s\nDescription: %s\nSkills:\n", card.Name, card.Description)
	for _, s := range card.Skills {
		fmt.Fprintf(&b, "- %s (%s): %s\n", s.Name, s.ID, s.Description)
	}
	return agentsdk.ToolResult{Content: b.String()}, nil
}
