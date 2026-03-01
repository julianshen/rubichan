package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/parser"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/wiki"
)

// WikiManifest returns the skill manifest for the built-in wiki skill.
func WikiManifest() skills.SkillManifest {
	return skills.SkillManifest{
		Name:        "wiki",
		Version:     "1.0.0",
		Description: "Generate project documentation wiki with architecture diagrams",
		Types:       []skills.SkillType{skills.SkillTypeTool},
	}
}

// WikiBackend implements skills.SkillBackend for the built-in wiki skill.
type WikiBackend struct {
	WorkDir string
	LLM     wiki.LLMCompleter

	tools []tools.Tool
}

// Load creates the generate_wiki tool.
func (b *WikiBackend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error {
	b.tools = []tools.Tool{
		&generateWikiTool{workDir: b.WorkDir, llm: b.LLM},
	}
	return nil
}

// Tools returns the wiki tool created during Load.
func (b *WikiBackend) Tools() []tools.Tool {
	return b.tools
}

// Hooks returns nil — wiki does not register any hooks.
func (b *WikiBackend) Hooks() map[skills.HookPhase]skills.HookHandler {
	return nil
}

// Commands returns nil — wiki does not provide slash commands.
func (b *WikiBackend) Commands() []commands.SlashCommand { return nil }

// Unload is a no-op for wiki.
func (b *WikiBackend) Unload() error {
	return nil
}

// generateWikiTool implements tools.Tool for wiki generation.
type generateWikiTool struct {
	workDir string
	llm     wiki.LLMCompleter
}

func (t *generateWikiTool) Name() string { return "generate_wiki" }

func (t *generateWikiTool) Description() string {
	return "Generate project documentation wiki with architecture diagrams and module docs"
}

func (t *generateWikiTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Root directory of the project to document (default: current working directory)"
		},
		"format": {
			"type": "string",
			"enum": ["raw-md", "hugo", "docusaurus"],
			"description": "Output format (default: raw-md)"
		},
		"outdir": {
			"type": "string",
			"description": "Output directory for generated wiki (default: docs/wiki)"
		},
		"concurrency": {
			"type": "integer",
			"description": "Maximum parallel LLM calls (default: 5)"
		}
	},
	"additionalProperties": false
}`)
}

type generateWikiInput struct {
	Path        string `json:"path"`
	Format      string `json:"format"`
	OutDir      string `json:"outdir"`
	Concurrency int    `json:"concurrency"`
}

func (t *generateWikiTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var params generateWikiInput
	if err := json.Unmarshal(input, &params); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	dir := params.Path
	if dir == "" {
		dir = t.workDir
	}
	format := params.Format
	if format == "" {
		format = "raw-md"
	}
	outDir := params.OutDir
	if outDir == "" {
		outDir = "docs/wiki"
	}
	concurrency := params.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	psr := parser.NewParser()

	err := wiki.Run(ctx, wiki.Config{
		Dir:         dir,
		OutputDir:   outDir,
		Format:      format,
		DiagramFmt:  "mermaid",
		Concurrency: concurrency,
	}, t.llm, psr)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("wiki generation failed: %v", err), IsError: true}, nil
	}

	return tools.ToolResult{
		Content: fmt.Sprintf("Wiki generated successfully in %s (format: %s)", outDir, format),
	}, nil
}
