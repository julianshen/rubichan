// cmd/rubichan/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tui"

	// Register providers via init() side effects.
	_ "github.com/julianshen/rubichan/internal/provider/anthropic"
	_ "github.com/julianshen/rubichan/internal/provider/openai"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"

	configPath   string
	modelFlag    string
	providerFlag string
)

func versionString() string {
	return fmt.Sprintf("rubichan %s (commit: %s, built: %s)", version, commit, date)
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "rubichan",
		Short: "An AI coding assistant",
		Long:  "rubichan — an interactive AI coding assistant powered by LLMs.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runInteractive()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config file")
	rootCmd.PersistentFlags().StringVar(&modelFlag, "model", "", "override model name")
	rootCmd.PersistentFlags().StringVar(&providerFlag, "provider", "", "override provider name")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(versionString())
		},
	}

	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runInteractive() error {
	// Determine config path
	cfgPath := configPath
	if cfgPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		cfgPath = filepath.Join(home, ".config", "rubichan", "config.toml")
	}

	// Load config
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Apply flag overrides
	if modelFlag != "" {
		cfg.Provider.Model = modelFlag
	}
	if providerFlag != "" {
		cfg.Provider.Default = providerFlag
	}

	// Create provider
	p, err := provider.NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	// Create tool registry
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	registry := tools.NewRegistry()
	if err := registry.Register(tools.NewFileTool(cwd)); err != nil {
		return fmt.Errorf("registering file tool: %w", err)
	}
	if err := registry.Register(tools.NewShellTool(cwd, 120*time.Second)); err != nil {
		return fmt.Errorf("registering shell tool: %w", err)
	}

	// Create approval function (auto-approve for now; TODO: real TUI approval)
	approvalFunc := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return true, nil
	}

	// Create agent
	a := agent.New(p, registry, approvalFunc, cfg)

	// Create TUI model and run
	model := tui.NewModel(a, "rubichan", cfg.Provider.Model)
	prog := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	return nil
}
