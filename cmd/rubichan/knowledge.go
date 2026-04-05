package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/knowledgegraph"
	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

// knowledgeCmd returns the top-level "knowledge" command with subcommands for
// managing the project knowledge graph.
func knowledgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "Manage project knowledge graph",
		Long: "Commands for querying, ingesting, and managing the project knowledge graph.\n\n" +
			"The knowledge graph stores structured project information in .knowledge/\n" +
			"with an SQLite index at .knowledge/.index.db (not committed to git).",
	}

	cmd.AddCommand(knowledgeIngestCmd())
	cmd.AddCommand(knowledgeQueryCmd())
	cmd.AddCommand(knowledgeReindexCmd())
	cmd.AddCommand(knowledgeLintCmd())

	return cmd
}

// knowledgeIngestCmd returns the "knowledge ingest" command.
func knowledgeIngestCmd() *cobra.Command {
	var since string

	cmd := &cobra.Command{
		Use:   "ingest [llm|git|file|manual] [path]",
		Short: "Ingest knowledge from various sources",
		Long: "Extract knowledge entities from different sources:\n\n" +
			"  llm      - Use LLM to extract from raw text in a file\n" +
			"  git      - Analyze git commit history since a given time\n" +
			"  file     - Use LLM to analyze file content\n" +
			"  manual   - Read YAML frontmatter from a markdown file",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ := args[0]
			if !isValidIngestType(typ) {
				return fmt.Errorf("invalid ingest type: %s (expected llm|git|file|manual)", typ)
			}

			if len(args) < 2 && typ != "git" {
				return fmt.Errorf("%s ingest requires a path argument", typ)
			}

			path := ""
			if len(args) > 1 {
				path = args[1]
			}

			g, err := openGraph(context.Background(), ".")
			if err != nil {
				return fmt.Errorf("open knowledge graph: %w", err)
			}
			defer g.Close()

			count := 0

			switch typ {
			case "llm":
				content, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("read file: %w", err)
				}
				ingestor := knowledgegraph.NewLLMIngestor(nil) // TODO: get from config
				count, err = ingestor.Ingest(context.Background(), g.(*knowledgegraph.KnowledgeGraph), string(content), kg.SourceLLM)
				if err != nil {
					return err
				}

			case "git":
				if since == "" {
					since = "1w"
				}
				ingestor := knowledgegraph.NewGitIngestor(nil) // TODO: get from config
				count, err = ingestor.Ingest(context.Background(), g.(*knowledgegraph.KnowledgeGraph), ".", since)
				if err != nil {
					return err
				}

			case "file":
				ingestor := knowledgegraph.NewFileIngestor(nil) // TODO: get from config
				count, err = ingestor.Ingest(context.Background(), g.(*knowledgegraph.KnowledgeGraph), path)
				if err != nil {
					return err
				}

			case "manual":
				ingestor := knowledgegraph.NewManualIngestor()
				count, err = ingestor.Ingest(context.Background(), g.(*knowledgegraph.KnowledgeGraph), path)
				if err != nil {
					return err
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Ingested %d entities from %s\n", count, typ)
			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "For git ingest, time frame like '1w', '1m', etc. (default: 1w)")

	return cmd
}

// knowledgeQueryCmd returns the "knowledge query" command.
func knowledgeQueryCmd() *cobra.Command {
	var limit int
	var budget int

	cmd := &cobra.Command{
		Use:   "query <text>",
		Short: "Search the knowledge graph",
		Long: "Search for relevant knowledge entities using semantic or keyword search.\n\n" +
			"If an embedder (Ollama) is available, uses semantic search.\n" +
			"Otherwise falls back to full-text search.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			g, err := openGraph(context.Background(), ".")
			if err != nil {
				return fmt.Errorf("open knowledge graph: %w", err)
			}
			defer g.Close()

			results, err := g.Query(context.Background(), kg.QueryRequest{
				Text:        query,
				TokenBudget: budget,
				Limit:       limit,
			})
			if err != nil {
				return fmt.Errorf("query: %w", err)
			}

			if len(results) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No results found.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tKIND\tTITLE\tSCORE\tTOKENS")
			for _, r := range results {
				fmt.Fprintf(w, "%s\t%s\t%s\t%.2f\t%d\n",
					r.Entity.ID,
					r.Entity.Kind,
					r.Entity.Title,
					r.Score,
					r.EstimatedTokens,
				)
			}
			return w.Flush()
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum number of results to return")
	cmd.Flags().IntVar(&budget, "budget", 0, "Token budget for results (0 = no limit)")

	return cmd
}

// knowledgeReindexCmd returns the "knowledge reindex" command.
func knowledgeReindexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the SQLite index",
		Long:  "Scan .knowledge/ directory and rebuild the SQLite index from markdown files.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			g, err := openGraph(context.Background(), ".")
			if err != nil {
				return fmt.Errorf("open knowledge graph: %w", err)
			}
			defer g.Close()

			if err := g.RebuildIndex(context.Background()); err != nil {
				return fmt.Errorf("reindex: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Index rebuilt successfully.")
			return nil
		},
	}
}

// knowledgeLintCmd returns the "knowledge lint" command.
func knowledgeLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint",
		Short: "Check knowledge graph for issues",
		Long:  "Scan for orphaned relationships, duplicate titles, and other structural issues.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			g, err := openGraph(context.Background(), ".")
			if err != nil {
				return fmt.Errorf("open knowledge graph: %w", err)
			}
			defer g.Close()

			report, err := g.LintGraph(context.Background())
			if err != nil {
				return fmt.Errorf("lint: %w", err)
			}

			if len(report.OrphanedRelationships) == 0 &&
				len(report.DuplicateTitles) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "✓ Knowledge graph is clean")
				return nil
			}

			if len(report.OrphanedRelationships) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "\n⚠ Orphaned relationships (%d):\n", len(report.OrphanedRelationships))
				for _, rel := range report.OrphanedRelationships {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s -> %s (target missing)\n", rel.SourceID, rel.TargetID)
				}
			}

			if len(report.DuplicateTitles) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "\n⚠ Duplicate titles (%d):\n", len(report.DuplicateTitles))
				for _, dup := range report.DuplicateTitles {
					fmt.Fprintf(cmd.OutOrStdout(), "  '%s': %v\n", dup.Title, dup.IDs)
				}
			}

			return nil
		},
	}
}

// openGraph opens the knowledge graph at the current working directory.
// Auto-detects embedder: tries Ollama at localhost:11434, falls back to FTS5.
func openGraph(ctx context.Context, workDir string) (kg.Graph, error) {
	// Try to auto-detect embedder (Ollama first, fallback to NullEmbedder)
	embedder := kg.NullEmbedder{}

	// TODO: In a real implementation, would try to create an OllamaEmbedder
	// and test with HealthCheck, falling back to NullEmbedder if unavailable

	g, err := kg.Open(ctx, workDir,
		kg.WithEmbedder(embedder),
		kg.WithKnowledgeDir(workDir + "/.knowledge"),
	)
	if err != nil {
		return nil, err
	}

	return g, nil
}

// isValidIngestType checks if the ingest type is valid.
func isValidIngestType(typ string) bool {
	typ = strings.ToLower(typ)
	return typ == "llm" || typ == "git" || typ == "file" || typ == "manual"
}
