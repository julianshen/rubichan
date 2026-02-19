// cmd/rubichan/wiki.go
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/integrations"
	"github.com/julianshen/rubichan/internal/parser"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/wiki"
)

func wikiCmd() *cobra.Command {
	var (
		formatFlag      string
		outputFlag      string
		diagramsFlag    string
		concurrencyFlag int
	)

	cmd := &cobra.Command{
		Use:   "wiki [path]",
		Short: "Generate project documentation wiki",
		Long: `Analyze a codebase and generate a static documentation site with
architecture diagrams, module documentation, and improvement suggestions.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			p, err := provider.NewProvider(cfg)
			if err != nil {
				return fmt.Errorf("creating provider: %w", err)
			}

			llm := integrations.NewLLMCompleter(p, cfg.Provider.Model)
			psr := parser.NewParser()

			return wiki.Run(cmd.Context(), wiki.Config{
				Dir:         dir,
				OutputDir:   outputFlag,
				Format:      formatFlag,
				DiagramFmt:  diagramsFlag,
				Concurrency: concurrencyFlag,
			}, llm, psr)
		},
	}

	cmd.Flags().StringVar(&formatFlag, "format", "raw-md", "output format: raw-md, hugo, docusaurus")
	cmd.Flags().StringVar(&outputFlag, "output", "docs/wiki", "output directory")
	cmd.Flags().StringVar(&diagramsFlag, "diagrams", "mermaid", "diagram format (only mermaid supported)")
	cmd.Flags().IntVar(&concurrencyFlag, "concurrency", 5, "max parallel LLM calls")

	return cmd
}
