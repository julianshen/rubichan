package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SkillSearchResult represents a single result from a registry search.
type SkillSearchResult struct {
	Name        string
	Version     string
	Description string
}

// SkillInstallResult contains the outcome of a skill installation.
type SkillInstallResult struct {
	Name      string
	Version   string
	Activated bool
}

// SkillListEntry represents an installed skill.
type SkillListEntry struct {
	Name        string
	Version     string
	Source      string
	InstalledAt string
}

// SkillManagerAccess abstracts skill management operations. Defined here to
// break the import cycle between tools/ and skills/. The concrete adapter
// lives in internal/agent/.
type SkillManagerAccess interface {
	Search(ctx context.Context, query string) ([]SkillSearchResult, error)
	Install(ctx context.Context, source string) (SkillInstallResult, error)
	List() ([]SkillListEntry, error)
	Remove(name string) error
}

// SkillManagerTool exposes skill search, install, list, and remove operations
// as an agent tool so the LLM can discover and install skills during a
// conversation.
type SkillManagerTool struct {
	manager SkillManagerAccess
}

// NewSkillManagerTool creates a SkillManagerTool backed by the given manager.
func NewSkillManagerTool(mgr SkillManagerAccess) *SkillManagerTool {
	return &SkillManagerTool{manager: mgr}
}

func (t *SkillManagerTool) Name() string { return "skill_manager" }

func (t *SkillManagerTool) Description() string {
	return "Search, install, list, and remove skills from the registry. " +
		"Actions: search (query), install (source: name, name@version, git URL, or local path), list, remove (name)."
}

func (t *SkillManagerTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"enum": ["search", "install", "list", "remove"],
				"description": "The action to perform"
			},
			"query": {
				"type": "string",
				"description": "Search query (required for search)"
			},
			"source": {
				"type": "string",
				"description": "Skill name, name@version, git URL, or local path (required for install)"
			},
			"name": {
				"type": "string",
				"description": "Skill name (required for remove)"
			}
		},
		"required": ["action"]
	}`)
}

type skillManagerInput struct {
	Action string `json:"action"`
	Query  string `json:"query"`
	Source string `json:"source"`
	Name   string `json:"name"`
}

func (t *SkillManagerTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	if t.manager == nil {
		return ToolResult{Content: "skill_manager tool not initialized", IsError: true}, nil
	}

	var in skillManagerInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	switch in.Action {
	case "search":
		return t.execSearch(ctx, in)
	case "install":
		return t.execInstall(ctx, in)
	case "list":
		return t.execList()
	case "remove":
		return t.execRemove(in)
	default:
		return ToolResult{
			Content: fmt.Sprintf("unknown action: %s (use search, install, list, or remove)", in.Action),
			IsError: true,
		}, nil
	}
}

func (t *SkillManagerTool) execSearch(ctx context.Context, in skillManagerInput) (ToolResult, error) {
	if in.Query == "" {
		return ToolResult{Content: "query is required for search action", IsError: true}, nil
	}

	results, err := t.manager.Search(ctx, in.Query)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("search failed: %s", err), IsError: true}, nil
	}

	if len(results) == 0 {
		return ToolResult{Content: fmt.Sprintf("No skills found matching %q.", in.Query)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d skill(s):\n\n", len(results)))
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("- %s (v%s): %s\n", r.Name, r.Version, r.Description))
	}
	return ToolResult{Content: sb.String()}, nil
}

func (t *SkillManagerTool) execInstall(ctx context.Context, in skillManagerInput) (ToolResult, error) {
	if in.Source == "" {
		return ToolResult{Content: "source is required for install action", IsError: true}, nil
	}

	result, err := t.manager.Install(ctx, in.Source)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("install failed: %s", err), IsError: true}, nil
	}

	msg := fmt.Sprintf("Installed skill %q (v%s).", result.Name, result.Version)
	if result.Activated {
		msg += " Skill activated in current session."
	}
	return ToolResult{Content: msg}, nil
}

func (t *SkillManagerTool) execList() (ToolResult, error) {
	entries, err := t.manager.List()
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("list failed: %s", err), IsError: true}, nil
	}

	if len(entries) == 0 {
		return ToolResult{Content: "No skills installed."}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d skill(s) installed:\n\n", len(entries)))
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("- %s (v%s) [%s] installed %s\n", e.Name, e.Version, e.Source, e.InstalledAt))
	}
	return ToolResult{Content: sb.String()}, nil
}

func (t *SkillManagerTool) execRemove(in skillManagerInput) (ToolResult, error) {
	if in.Name == "" {
		return ToolResult{Content: "name is required for remove action", IsError: true}, nil
	}

	if err := t.manager.Remove(in.Name); err != nil {
		return ToolResult{Content: fmt.Sprintf("remove failed: %s", err), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("Skill %q removed.", in.Name)}, nil
}
