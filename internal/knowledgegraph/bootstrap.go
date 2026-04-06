package knowledgegraph

import (
	"fmt"
	"strings"
	"time"
)

// Questioner is an interface for prompting the user during the bootstrap process.
// It allows mocking for tests and decoupling from specific UI implementations.
type Questioner interface {
	// AskString prompts the user for a string response.
	AskString(prompt string) (string, error)

	// AskChoice prompts the user to select a single option from a list.
	AskChoice(prompt string, options []string) (string, error)

	// AskMultiSelect prompts the user to select multiple options from a list.
	AskMultiSelect(prompt string, options []string) ([]string, error)
}

// CollectBootstrapProfile runs the interactive questionnaire to collect user responses
// for knowledge graph initialization. It asks 10 questions in order and returns a
// BootstrapProfile with the user's answers.
//
// Questions asked:
// 1. Project name (string, must not be empty)
// 2. Backend technologies (multi-select)
// 3. Frontend technologies (multi-select)
// 4. Database technologies (multi-select)
// 5. Infrastructure technologies (multi-select)
// 6. Architecture style (single choice)
// 7. Pain points (comma-separated string)
// 8. Team size (single choice)
// 9. Team composition (single choice)
// 10. Is existing project? (yes/no)
func CollectBootstrapProfile(q Questioner) (*BootstrapProfile, error) {
	// Question 1: Project name
	projectName, err := q.AskString("What is your project name?")
	if err != nil {
		return nil, fmt.Errorf("failed to collect project name: %w", err)
	}
	projectName = strings.TrimSpace(projectName)
	if projectName == "" {
		return nil, fmt.Errorf("project name cannot be empty")
	}

	// Question 2: Backend technologies
	backendOptions := []string{"Go", "Python", "Node.js", "Java", "Rust", "Other"}
	backendTechs, err := q.AskMultiSelect("Select backend technologies:", backendOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to collect backend technologies: %w", err)
	}

	// Question 3: Frontend technologies
	frontendOptions := []string{"React", "Vue", "Svelte", "Next.js", "Other"}
	frontendTechs, err := q.AskMultiSelect("Select frontend technologies:", frontendOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to collect frontend technologies: %w", err)
	}

	// Question 4: Database technologies
	databaseOptions := []string{"PostgreSQL", "MongoDB", "Redis", "SQLite", "Other"}
	databaseTechs, err := q.AskMultiSelect("Select database technologies:", databaseOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to collect database technologies: %w", err)
	}

	// Question 5: Infrastructure technologies
	infraOptions := []string{"Kubernetes", "Docker", "AWS", "GCP", "Azure", "Other"}
	infraTechs, err := q.AskMultiSelect("Select infrastructure technologies:", infraOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to collect infrastructure technologies: %w", err)
	}

	// Question 6: Architecture style
	archOptions := []string{"Monolithic", "Microservices", "Serverless", "Hybrid"}
	archStyle, err := q.AskChoice("What is your architecture style?", archOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to collect architecture style: %w", err)
	}

	// Question 7: Pain points
	painPointsStr, err := q.AskString("Describe your pain points (comma-separated):")
	if err != nil {
		return nil, fmt.Errorf("failed to collect pain points: %w", err)
	}
	painPoints := parseCommaSeparated(painPointsStr)

	// Question 8: Team size
	teamSizeOptions := []string{"small", "medium", "large"}
	teamSize, err := q.AskChoice("What is your team size?", teamSizeOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to collect team size: %w", err)
	}

	// Question 9: Team composition
	teamCompOptions := []string{"frontend", "backend", "fullstack", "mixed"}
	teamComp, err := q.AskChoice("What is your team composition?", teamCompOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to collect team composition: %w", err)
	}

	// Question 10: Is existing project?
	existingAnswer, err := q.AskString("Is this an existing project?")
	if err != nil {
		return nil, fmt.Errorf("failed to collect existing project status: %w", err)
	}
	isExisting := strings.ToLower(strings.TrimSpace(existingAnswer)) == "yes"

	return &BootstrapProfile{
		ProjectName:         projectName,
		BackendTechs:        backendTechs,
		FrontendTechs:       frontendTechs,
		DatabaseTechs:       databaseTechs,
		InfrastructureTechs: infraTechs,
		ArchitectureStyle:   archStyle,
		PainPoints:          painPoints,
		TeamSize:            teamSize,
		TeamComposition:     teamComp,
		IsExisting:          isExisting,
		CreatedAt:           time.Now(),
	}, nil
}

// parseCommaSeparated splits a comma-separated string into trimmed parts.
func parseCommaSeparated(input string) []string {
	if input == "" {
		return []string{}
	}
	parts := strings.Split(input, ",")
	var result []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// BootstrapProfile captures user answers from the bootstrap questionnaire.
// It records the project's technology stack, architecture, team composition,
// and initial pain points for the knowledge graph initialization process.
type BootstrapProfile struct {
	// ProjectName is the identifier for the project (e.g., "myapp").
	ProjectName string `json:"project_name"`

	// BackendTechs lists backend technologies in use (e.g., ["Go", "Node.js"]).
	BackendTechs []string `json:"backend_techs"`

	// FrontendTechs lists frontend technologies in use (e.g., ["React", "Vue"]).
	FrontendTechs []string `json:"frontend_techs"`

	// DatabaseTechs lists database technologies in use (e.g., ["PostgreSQL", "MongoDB"]).
	DatabaseTechs []string `json:"database_techs"`

	// InfrastructureTechs lists infrastructure/deployment technologies (e.g., ["Kubernetes", "Docker"]).
	InfrastructureTechs []string `json:"infrastructure_techs"`

	// ArchitectureStyle describes the overall architecture pattern:
	// "Monolithic", "Microservices", "Serverless", or "Hybrid".
	ArchitectureStyle string `json:"architecture_style"`

	// PainPoints are the user's top challenges and pain points (e.g., ["scaling", "monitoring"]).
	PainPoints []string `json:"pain_points"`

	// TeamSize describes the team size: "small", "medium", or "large".
	TeamSize string `json:"team_size"`

	// TeamComposition describes the team's technical focus:
	// "frontend", "backend", "fullstack", or "mixed".
	TeamComposition string `json:"team_composition"`

	// IsExisting indicates whether this is bootstrapping an existing project
	// (true) or starting from scratch (false).
	IsExisting bool `json:"is_existing"`

	// CreatedAt is the timestamp when the profile was created.
	CreatedAt time.Time `json:"created_at"`
}

// ProposedEntity is a candidate entity discovered during the bootstrap analysis phase.
// It represents a potential knowledge graph node that was identified through code
// analysis, git history, or integration discovery.
type ProposedEntity struct {
	// ID is a unique identifier for the entity (e.g., "myapp-auth-module").
	ID string `json:"id"`

	// Kind categorizes the entity type: "module", "decision", "integration",
	// "architecture", "gotcha", or "pattern".
	Kind string `json:"kind"`

	// Title is a human-readable name for the entity.
	Title string `json:"title"`

	// Body is the detailed description of the entity.
	Body string `json:"body"`

	// SourceType indicates how the entity was discovered:
	// "module" (directory/package), "git" (from commits), "integration" (from config),
	// or "ast" (from code analysis).
	SourceType string `json:"source_type"`

	// Confidence is a score from 0.5 to 0.9 indicating how confident the system is
	// that this entity should be created.
	Confidence float64 `json:"confidence"`

	// Tags are labels associated with the entity (e.g., ["security", "auth"]).
	Tags []string `json:"tags"`
}

// AnalysisMetadata captures metrics from the bootstrap analysis phase.
// It records quantitative data about what was analyzed and discovered.
type AnalysisMetadata struct {
	// ModulesFound is the count of modules/packages discovered in the codebase.
	ModulesFound int `json:"modules_found"`

	// GitCommitsAnalyzed is the count of git commits that were analyzed.
	GitCommitsAnalyzed int `json:"git_commits_analyzed"`

	// IntegrationsDetected is the count of external integrations discovered.
	IntegrationsDetected int `json:"integrations_detected"`

	// AnalysisTimestamp is when the analysis phase completed.
	AnalysisTimestamp time.Time `json:"analysis_timestamp"`
}

// BootstrapMetadata is written to .knowledge/.bootstrap.json after entity creation.
// It serves as a checkpoint record of what was bootstrapped, when, and with what analysis.
type BootstrapMetadata struct {
	// Profile is the user's bootstrap questionnaire responses.
	Profile BootstrapProfile `json:"profile"`

	// CreatedEntities is the list of entity IDs that were created during bootstrap.
	CreatedEntities []string `json:"created_entities"`

	// AnalysisMetadata contains metrics from the analysis phase.
	AnalysisMetadata AnalysisMetadata `json:"analysis_metadata"`

	// BootstrappedAt is the timestamp when the bootstrap process completed.
	BootstrappedAt time.Time `json:"bootstrapped_at"`
}
