package main

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/provider/ollama"
)

var defaultOllamaBaseURL = ollama.DefaultBaseURL

// ollamaCmd returns the top-level "ollama" command with list, pull, rm, and
// status subcommands for managing local Ollama models.
func ollamaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Manage Ollama models",
		Long:  "List, pull, remove, and check status of locally available Ollama models.",
	}

	cmd.PersistentFlags().String("base-url", "", "Ollama API base URL (default: http://localhost:11434)")

	cmd.AddCommand(ollamaListCmd())
	cmd.AddCommand(ollamaPullCmd())
	cmd.AddCommand(ollamaRmCmd())
	cmd.AddCommand(ollamaStatusCmd())

	return cmd
}

// resolveOllamaBaseURL returns the Ollama base URL from the --base-url flag
// or the default http://localhost:11434.
func resolveOllamaBaseURL(cmd *cobra.Command) string {
	baseURL, _ := cmd.Flags().GetString("base-url")
	if baseURL != "" {
		return baseURL
	}
	return defaultOllamaBaseURL
}

// formatBytes formats a byte count into a human-readable string
// (e.g., "4.0 GB", "512.0 MB").
func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func ollamaListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List locally available models",
		Long:  "Display a table of all locally available Ollama models with name, size, and modification date.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			baseURL := resolveOllamaBaseURL(cmd)
			client := ollama.NewClient(baseURL)

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			models, err := client.ListModels(ctx)
			if err != nil {
				return fmt.Errorf("listing models: %w", err)
			}

			if len(models) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No models available.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSIZE\tMODIFIED")
			for _, m := range models {
				fmt.Fprintf(w, "%s\t%s\t%s\n",
					m.Name,
					formatBytes(m.Size),
					m.ModifiedAt.Format(time.RFC3339),
				)
			}
			return w.Flush()
		},
	}
}

func ollamaStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check if Ollama is running",
		Long:  "Check the Ollama server status, display version and model count.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			baseURL := resolveOllamaBaseURL(cmd)
			client := ollama.NewClient(baseURL)

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			version, err := client.Version(ctx)
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Ollama is not running at %s\n", baseURL)
				return fmt.Errorf("ollama not reachable: %w", err)
			}

			models, err := client.ListModels(ctx)
			if err != nil {
				return fmt.Errorf("listing models: %w", err)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "Status:\trunning\n")
			fmt.Fprintf(w, "Version:\t%s\n", version)
			fmt.Fprintf(w, "Models:\t%d\n", len(models))
			return w.Flush()
		},
	}
}

func ollamaPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull <model>",
		Short: "Pull a model",
		Long:  "Download an Ollama model by name, showing progress as it downloads.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			baseURL := resolveOllamaBaseURL(cmd)
			client := ollama.NewClient(baseURL)

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			ch, err := client.PullModel(ctx, name)
			if err != nil {
				return fmt.Errorf("pulling model %q: %w", name, err)
			}

			errOut := cmd.ErrOrStderr()
			for progress := range ch {
				if progress.Total > 0 {
					pct := float64(progress.Completed) / float64(progress.Total) * 100
					fmt.Fprintf(errOut, "%s: %.1f%% (%s/%s)\n",
						progress.Status,
						pct,
						formatBytes(progress.Completed),
						formatBytes(progress.Total),
					)
				} else {
					fmt.Fprintf(errOut, "%s\n", progress.Status)
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Successfully pulled %q\n", name)
			return nil
		},
	}
}

func ollamaRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <model>",
		Short: "Remove a model",
		Long:  "Delete a locally available Ollama model by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			baseURL := resolveOllamaBaseURL(cmd)
			client := ollama.NewClient(baseURL)

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if err := client.DeleteModel(ctx, name); err != nil {
				return fmt.Errorf("removing model %q: %w", name, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed model %q\n", name)
			return nil
		},
	}
}
