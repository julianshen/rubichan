package knowledgegraph

import "time"

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
