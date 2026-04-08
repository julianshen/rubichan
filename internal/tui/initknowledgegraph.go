package tui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/julianshen/rubichan/internal/knowledgegraph"
)

// InitKnowledgeGraphOverlay is a questionnaire overlay for knowledge graph bootstrap.
// It collects project information through a multi-step form and then runs the bootstrap.
type InitKnowledgeGraphOverlay struct {
	form      *huh.Form
	cancelled bool
	profile   *knowledgegraph.BootstrapProfile
	width     int
	height    int

	// Form state - captures user input
	projectName   string
	backendTechs  []string
	frontendTechs []string
	databaseTechs []string
	infraTechs    []string
	archStyle     string
	painPoints    string
	teamSize      string
	teamComp      string
	isExisting    bool
}

// NewInitKnowledgeGraphOverlay creates a new knowledge graph bootstrap questionnaire.
func NewInitKnowledgeGraphOverlay(width, height int) *InitKnowledgeGraphOverlay {
	i := &InitKnowledgeGraphOverlay{
		width:  width,
		height: height,
	}
	i.buildForm()
	return i
}

// buildForm constructs the 10-question form.
func (i *InitKnowledgeGraphOverlay) buildForm() {
	i.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Project name").
				Description("Identifier for your project (e.g., myapp)").
				Value(&i.projectName).
				Validate(func(s string) error {
					if s == "" {
						return errors.New("project name cannot be empty")
					}
					return nil
				}),
		),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Backend technologies").
				Options(
					huh.NewOption("Go", "Go"),
					huh.NewOption("Python", "Python"),
					huh.NewOption("Node.js", "Node.js"),
					huh.NewOption("Java", "Java"),
					huh.NewOption("Rust", "Rust"),
					huh.NewOption("Other", "Other"),
				).
				Value(&i.backendTechs),
		),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Frontend technologies").
				Options(
					huh.NewOption("React", "React"),
					huh.NewOption("Vue", "Vue"),
					huh.NewOption("Svelte", "Svelte"),
					huh.NewOption("Next.js", "Next.js"),
					huh.NewOption("Other", "Other"),
				).
				Value(&i.frontendTechs),
		),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Database technologies").
				Options(
					huh.NewOption("PostgreSQL", "PostgreSQL"),
					huh.NewOption("MongoDB", "MongoDB"),
					huh.NewOption("Redis", "Redis"),
					huh.NewOption("SQLite", "SQLite"),
					huh.NewOption("Other", "Other"),
				).
				Value(&i.databaseTechs),
		),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Infrastructure technologies").
				Options(
					huh.NewOption("Kubernetes", "Kubernetes"),
					huh.NewOption("Docker", "Docker"),
					huh.NewOption("AWS", "AWS"),
					huh.NewOption("GCP", "GCP"),
					huh.NewOption("Azure", "Azure"),
					huh.NewOption("Other", "Other"),
				).
				Value(&i.infraTechs),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Architecture style").
				Options(
					huh.NewOption("Monolithic", "Monolithic"),
					huh.NewOption("Microservices", "Microservices"),
					huh.NewOption("Serverless", "Serverless"),
					huh.NewOption("Hybrid", "Hybrid"),
				).
				Value(&i.archStyle),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Pain points").
				Description("Comma-separated list of challenges (e.g., scaling, monitoring)").
				Value(&i.painPoints),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Team size").
				Options(
					huh.NewOption("Small (1-5)", "small"),
					huh.NewOption("Medium (5-15)", "medium"),
					huh.NewOption("Large (15+)", "large"),
				).
				Value(&i.teamSize),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Team composition").
				Options(
					huh.NewOption("Frontend focused", "frontend"),
					huh.NewOption("Backend focused", "backend"),
					huh.NewOption("Full-stack", "fullstack"),
					huh.NewOption("Mixed", "mixed"),
				).
				Value(&i.teamComp),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Is this an existing project?").
				Value(&i.isExisting),
		),
	)
}

// Update handles input for the questionnaire.
func (i *InitKnowledgeGraphOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyEscape {
			i.cancelled = true
			return i, nil
		}
	case tea.WindowSizeMsg:
		i.width = msg.Width
		i.height = msg.Height
	}

	form, cmd := i.form.Update(msg)
	i.form = form.(*huh.Form)

	if i.form.State == huh.StateCompleted {
		// Build profile from form responses - this marks the form as done
		i.profile = &knowledgegraph.BootstrapProfile{
			ProjectName:         i.projectName,
			BackendTechs:        i.backendTechs,
			FrontendTechs:       i.frontendTechs,
			DatabaseTechs:       i.databaseTechs,
			InfrastructureTechs: i.infraTechs,
			ArchitectureStyle:   i.archStyle,
			PainPoints:          parseCommaSeparated(i.painPoints),
			TeamSize:            i.teamSize,
			TeamComposition:     i.teamComp,
			IsExisting:          i.isExisting,
		}
		i.cancelled = true
	} else if i.form.State == huh.StateAborted {
		// User cancelled the form
		i.cancelled = true
		i.profile = nil
	}

	return i, cmd
}

// View renders the questionnaire form.
func (i *InitKnowledgeGraphOverlay) View() string {
	return i.form.View()
}

// Done returns true when the form is complete.
func (i *InitKnowledgeGraphOverlay) Done() bool {
	return i.cancelled
}

// Result returns the InitKnowledgeGraphResult with the bootstrap profile.
func (i *InitKnowledgeGraphOverlay) Result() any {
	if i.profile == nil {
		return nil
	}
	return InitKnowledgeGraphResult{Profile: i.profile}
}

// parseCommaSeparated splits a comma-separated string into trimmed parts.
func parseCommaSeparated(input string) []string {
	if input == "" {
		return []string{}
	}
	var result []string
	for _, part := range strings.Split(input, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
