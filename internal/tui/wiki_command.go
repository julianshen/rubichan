package tui

import (
	"context"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/wiki"
)

// WikiCommandConfig holds dependencies for the /wiki command.
type WikiCommandConfig struct {
	WorkDir string
	LLM     wiki.LLMCompleter
}

type wikiCommand struct {
	cfg WikiCommandConfig
}

// NewWikiCommand creates a /wiki slash command that opens the wiki form overlay.
func NewWikiCommand(cfg WikiCommandConfig) commands.SlashCommand {
	return &wikiCommand{cfg: cfg}
}

func (c *wikiCommand) Name() string        { return "wiki" }
func (c *wikiCommand) Description() string { return "Generate project documentation wiki" }
func (c *wikiCommand) Arguments() []commands.ArgumentDef { return nil }

func (c *wikiCommand) Complete(_ context.Context, _ []string) []commands.Candidate {
	return nil
}

func (c *wikiCommand) Execute(_ context.Context, _ []string) (commands.Result, error) {
	return commands.Result{Action: commands.ActionOpenWiki}, nil
}
