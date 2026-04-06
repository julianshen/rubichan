package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/knowledgegraph"
)

// initKnowledgeGraphCmd returns the /initknowledgegraph skill command.
func initKnowledgeGraphCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "initknowledgegraph",
		Short: "Bootstrap a knowledge graph for your project",
		Long: `Bootstrap your project's knowledge graph through:
1. Interactive questionnaire about your project
2. Automatic analysis of your codebase
3. Discovery of modules, decisions, and integrations
4. Interactive refinement with the agent

The skill creates initial entities in .knowledge/ and starts an agent session for refinement.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInitKnowledgeGraph(cmd.Context())
		},
	}
}

// runInitKnowledgeGraph orchestrates the three-phase bootstrap.
func runInitKnowledgeGraph(ctx context.Context) error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	return runInitKnowledgeGraphWithQuestioner(ctx, cwd, NewInteractiveQuestioner())
}

// runInitKnowledgeGraphWithQuestioner orchestrates the bootstrap with a provided questioner.
// This is the main orchestration function used by both the CLI and tests.
func runInitKnowledgeGraphWithQuestioner(ctx context.Context, cwd string, q knowledgegraph.Questioner) error {
	fmt.Println("🚀 Knowledge Graph Bootstrap")

	// Phase 1: Questionnaire
	fmt.Println("📋 Phase 1: Project Questionnaire")
	fmt.Println()
	profile, err := knowledgegraph.CollectBootstrapProfile(q)
	if err != nil {
		return fmt.Errorf("questionnaire failed: %w", err)
	}

	// Phase 2: Analysis
	fmt.Println()
	fmt.Println("🔍 Phase 2: Codebase Analysis")
	fmt.Println()

	var allEntities []*knowledgegraph.ProposedEntity

	// Modules
	fmt.Print("  Scanning modules...")
	modules, err := knowledgegraph.DiscoverModules(cwd)
	if err == nil {
		fmt.Printf(" found %d\n", len(modules))
		allEntities = append(allEntities, modules...)
	} else {
		fmt.Printf(" error: %v\n", err)
	}

	// Decisions from git
	fmt.Print("  Analyzing git history...")
	decisions, err := knowledgegraph.DiscoverDecisionsFromGit(cwd, profile)
	if err == nil {
		fmt.Printf(" found %d\n", len(decisions))
		allEntities = append(allEntities, decisions...)
	} else {
		fmt.Printf(" error: %v\n", err)
	}

	// Integrations
	fmt.Print("  Detecting integrations...")
	integrations, err := knowledgegraph.DiscoverIntegrations(cwd)
	if err == nil {
		fmt.Printf(" found %d\n", len(integrations))
		allEntities = append(allEntities, integrations...)
	} else {
		fmt.Printf(" error: %v\n", err)
	}

	// Phase 3: Entity Creation
	fmt.Println()
	fmt.Println("✍️  Phase 3: Creating Entities")
	fmt.Println()

	knowledgeDir := filepath.Join(cwd, ".knowledge")

	// Check if exists
	if _, err := os.Stat(knowledgeDir); err == nil {
		fmt.Print("Knowledge graph exists. Enhance existing (y/n)? ")
		var response string
		_, _ = fmt.Scanln(&response)
		if strings.ToLower(response) != "y" {
			fmt.Println("Skipped.")
			return nil
		}
	}

	metadata, err := knowledgegraph.WriteBootstrapEntities(knowledgeDir, allEntities, profile)
	if err != nil {
		return fmt.Errorf("writing entities: %w", err)
	}

	fmt.Printf("✓ Created %d entities in .knowledge/\n", len(metadata.CreatedEntities))

	// Phase 4: Summary
	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()
	fmt.Println("✅ Bootstrap complete!")
	fmt.Println()
	fmt.Printf("📊 Summary:\n")
	fmt.Printf("   Project: %s\n", profile.ProjectName)
	fmt.Printf("   Entities created: %d\n", len(metadata.CreatedEntities))
	fmt.Printf("   Architecture: %s\n", profile.ArchitectureStyle)

	return nil
}

// InteractiveQuestioner implements knowledgegraph.Questioner with CLI prompts.
type InteractiveQuestioner struct{}

// NewInteractiveQuestioner creates a questioner that uses stdin for prompts.
func NewInteractiveQuestioner() knowledgegraph.Questioner {
	return &InteractiveQuestioner{}
}

// AskString prompts for a string response.
func (q *InteractiveQuestioner) AskString(prompt string) (string, error) {
	fmt.Printf("%s: ", prompt)
	var response string
	_, err := fmt.Scanln(&response)
	if err != nil {
		return "", err
	}
	return response, nil
}

// AskChoice prompts for single-choice selection.
func (q *InteractiveQuestioner) AskChoice(prompt string, options []string) (string, error) {
	fmt.Printf("%s\n", prompt)
	for i, opt := range options {
		fmt.Printf("  [%d] %s\n", i+1, opt)
	}
	fmt.Print("Select: ")
	var input string
	_, err := fmt.Scanln(&input)
	if err != nil {
		if len(options) > 0 {
			return options[0], nil
		}
		return "", err
	}

	idx, parseErr := strconv.Atoi(strings.TrimSpace(input))
	if parseErr != nil || idx < 1 || idx > len(options) {
		if len(options) > 0 {
			return options[0], nil
		}
	}
	return options[idx-1], nil
}

// AskMultiSelect prompts for multiple choices.
func (q *InteractiveQuestioner) AskMultiSelect(prompt string, options []string) ([]string, error) {
	fmt.Printf("%s (comma-separated indices, or empty for all)\n", prompt)
	for i, opt := range options {
		fmt.Printf("  [%d] %s\n", i+1, opt)
	}
	fmt.Print("Select: ")
	var input string
	_, err := fmt.Scanln(&input)
	if err != nil && err.Error() != "unexpected newline" {
		// Default to first option on error
		if len(options) > 0 {
			return []string{options[0]}, nil
		}
		return []string{}, nil
	}

	input = strings.TrimSpace(input)
	if input == "" {
		// Empty input means all options
		return options, nil
	}

	var selected []string
	indices := strings.Split(input, ",")
	for _, idx := range indices {
		if i, parseErr := strconv.Atoi(strings.TrimSpace(idx)); parseErr == nil && i > 0 && i <= len(options) {
			selected = append(selected, options[i-1])
		}
	}

	if len(selected) == 0 && len(options) > 0 {
		selected = []string{options[0]}
	}
	return selected, nil
}
