package knowledgegraph

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Confidence score constants for different entity discovery methods.
// These represent the system's confidence that discovered entities should be created.
const (
	// ConfidenceModule represents confidence in entities discovered from directory structure.
	// Modules are reliably discovered through filesystem scanning.
	ConfidenceModule = 0.9

	// ConfidenceDecision represents confidence in architectural decisions extracted from git commits.
	// Keyword matching in commit messages is moderately reliable.
	ConfidenceDecision = 0.7

	// ConfidenceIntegration represents confidence in dependencies discovered from import statements.
	// Import scanning is highly reliable when matched against known integrations.
	ConfidenceIntegration = 0.85
)

// Entity kind constants define the valid kinds for discovered entities.
const (
	KindModule       = "module"
	KindDecision     = "decision"
	KindIntegration  = "integration"
	KindArchitecture = "architecture"
	KindGotcha       = "gotcha"
	KindPattern      = "pattern"
)

// Entity source type constants indicate how an entity was discovered.
const (
	SourceTypeModule      = "module"
	SourceTypeGit         = "git"
	SourceTypeIntegration = "integration"
	SourceTypeAST         = "ast"
)

// DefaultGitCommitLimit limits commit history analysis to avoid excessive processing.
// This can be tuned based on performance requirements for large repositories.
const DefaultGitCommitLimit = 30

// Package-level regex variables compiled once at init, avoiding recompilation per use.
var (
	singleImportRegex = regexp.MustCompile(`import\s+"([^"]+)"`)
	blockImportRegex  = regexp.MustCompile(`import\s*\(([\s\S]*?)\)`)
	pathRegex         = regexp.MustCompile(`"([^"]+)"`)
)

// Directories to skip during filesystem walks to improve performance.
var skipDirs = map[string]bool{
	".git":          true,
	".knowledge":    true,
	"node_modules":  true,
	"vendor":        true,
	".terraform":    true,
	"__pycache__":   true,
	".pytest_cache": true,
	"dist":          true,
	"build":         true,
	".venv":         true,
	"venv":          true,
}

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

	// Ensure pain points is always a slice (even if empty), never nil
	if painPoints == nil {
		painPoints = []string{}
	}

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

// DiscoverModules walks the codebase and identifies top-level packages/directories.
// It scans pkg/, internal/, cmd/, src/, app/, backend/, and frontend/ directories
// and creates ProposedEntity nodes for each module found.
//
// Returns a slice of module entities with Kind="module" and Confidence=0.9.
// If a directory cannot be read, it is skipped gracefully.
func DiscoverModules(rootDir string) ([]*ProposedEntity, error) {
	moduleDirs := []string{"pkg", "internal", "cmd", "src", "app", "backend", "frontend"}
	var entities []*ProposedEntity

	for _, baseDir := range moduleDirs {
		basePath := filepath.Join(rootDir, baseDir)
		entries, err := os.ReadDir(basePath)
		if err != nil {
			// Directory doesn't exist or unreadable, skip gracefully
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			moduleName := entry.Name()
			moduleTitle := formatModuleTitle(moduleName)

			entity := &ProposedEntity{
				ID:         moduleName,
				Kind:       KindModule,
				Title:      moduleTitle,
				Body:       fmt.Sprintf("Module discovered in %s/%s", baseDir, moduleName),
				SourceType: SourceTypeModule,
				Confidence: ConfidenceModule,
				Tags:       []string{KindModule},
			}

			entities = append(entities, entity)
		}
	}

	return entities, nil
}

// formatModuleTitle converts snake_case module name to Title Case.
// For example, "user_management" becomes "User Management".
func formatModuleTitle(name string) string {
	// Replace underscores with spaces
	withSpaces := strings.ReplaceAll(name, "_", " ")

	// Split by spaces and capitalize each word
	words := strings.Fields(withSpaces)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
		}
	}

	return strings.Join(words, " ")
}

// DiscoverDecisionsFromGit analyzes recent git commits for architectural decisions.
// It examines the last DefaultGitCommitLimit commits and searches for keywords:
// "architecture", "decision", "pattern", "refactor", and pain points from the profile.
//
// Returns a slice of decision entities with Kind="decision" and SourceType="git".
// Confidence is set to ConfidenceDecision. If git is not available or the repo is invalid,
// an empty slice is returned with nil error (graceful degradation).
func DiscoverDecisionsFromGit(rootDir string, profile *BootstrapProfile) ([]*ProposedEntity, error) {
	// Run git log command. Limit to DefaultGitCommitLimit commits to avoid excessive
	// processing on large repositories. If git is unavailable or not a git repo, we
	// gracefully return an empty slice rather than erroring.
	cmd := exec.Command("git", "-C", rootDir, "log", "--oneline", fmt.Sprintf("-%d", DefaultGitCommitLimit))
	output, err := cmd.Output()
	if err != nil {
		// Git not available or not a git repo, return empty slice (graceful degradation)
		return []*ProposedEntity{}, nil
	}

	// Keywords to search for in commit messages
	keywords := []string{
		"architecture",
		"decision",
		"pattern",
		"refactor",
	}

	// Add pain points as keywords (lowercase)
	if profile != nil {
		for _, painPoint := range profile.PainPoints {
			keywords = append(keywords, strings.ToLower(painPoint))
		}
	}

	var entities []*ProposedEntity
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Check if line contains any keyword
		lowerLine := strings.ToLower(line)
		for _, keyword := range keywords {
			if strings.Contains(lowerLine, keyword) {
				// Extract commit message (everything after the hash)
				parts := strings.SplitN(line, " ", 2)
				if len(parts) < 2 {
					continue
				}

				commitMsg := parts[1]
				entity := &ProposedEntity{
					ID:         strings.TrimSpace(parts[0]), // Use commit hash as ID
					Kind:       KindDecision,
					Title:      commitMsg,
					Body:       fmt.Sprintf("Discovered from git commit: %s", line),
					SourceType: SourceTypeGit,
					Confidence: ConfidenceDecision,
					Tags:       []string{SourceTypeGit, KindDecision},
				}

				entities = append(entities, entity)
				break // Don't add duplicate entity for same commit
			}
		}
	}

	return entities, nil
}

// knownIntegrations maps import paths to human-readable integration names.
var knownIntegrations = map[string]string{
	"github.com/lib/pq":                   "PostgreSQL (pq driver)",
	"github.com/redis/go-redis":           "Redis",
	"go.mongodb.org/mongo-driver":         "MongoDB",
	"github.com/go-sql-driver/mysql":      "MySQL",
	"github.com/jackc/pgx/v5":             "PostgreSQL (pgx driver)",
	"github.com/elastic/go-elasticsearch": "Elasticsearch",
	"github.com/go-gorm/gorm":             "GORM (ORM)",
	"github.com/goreleaser/goreleaser":    "GoReleaser",
}

// DiscoverIntegrations scans Go files for imported libraries and external dependencies.
// It walks the codebase, finds import statements, and matches them against the
// knownIntegrations map to identify external services and databases.
// Skips performance-critical directories like vendor/, node_modules/, and .git/.
//
// Returns a slice of integration entities with Kind="integration" and Confidence=ConfidenceIntegration.
// Unknown imports are skipped.
func DiscoverIntegrations(rootDir string) ([]*ProposedEntity, error) {
	var entities []*ProposedEntity
	seenIntegrations := make(map[string]bool) // Track already-found integrations

	// Walk the directory tree looking for .go files, skipping irrelevant directories
	// for performance (vendor/, node_modules/, .git/, etc.)
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip unreadable files
		}

		// Skip directories listed in skipDirs map (vendor/, node_modules/, .git/, etc.)
		if info.IsDir() {
			if skipDirs[filepath.Base(path)] {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Read the file
		content, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip unreadable files
		}

		// Find import statements
		imports := extractImports(string(content))

		for _, importPath := range imports {
			// Check against known integrations (direct match first)
			if title, ok := knownIntegrations[importPath]; ok {
				// Skip if we've already added this integration
				if seenIntegrations[importPath] {
					continue
				}
				seenIntegrations[importPath] = true

				entity := &ProposedEntity{
					ID:         strings.ReplaceAll(importPath, "/", "-"),
					Kind:       KindIntegration,
					Title:      title,
					Body:       fmt.Sprintf("Imported from: %s", importPath),
					SourceType: SourceTypeIntegration,
					Confidence: ConfidenceIntegration,
					Tags:       []string{KindIntegration, "dependency"},
				}

				entities = append(entities, entity)
				continue
			}

			// Try matching without version suffix (e.g., "github.com/redis/go-redis/v8" -> "github.com/redis/go-redis")
			basePath := importPath
			if strings.Contains(basePath, "/v") {
				// Remove version suffix like /v8, /v2, etc.
				parts := strings.Split(basePath, "/")
				for i := len(parts) - 1; i >= 0; i-- {
					if strings.HasPrefix(parts[i], "v") && len(parts[i]) > 1 {
						// This looks like a version, remove it
						basePath = strings.Join(parts[:i], "/")
						break
					}
				}

				if title, ok := knownIntegrations[basePath]; ok {
					// Skip if we've already added this integration
					if seenIntegrations[basePath] {
						continue
					}
					seenIntegrations[basePath] = true

					entity := &ProposedEntity{
						ID:         strings.ReplaceAll(basePath, "/", "-"),
						Kind:       KindIntegration,
						Title:      title,
						Body:       fmt.Sprintf("Imported from: %s", importPath),
						SourceType: SourceTypeIntegration,
						Confidence: ConfidenceIntegration,
						Tags:       []string{KindIntegration, "dependency"},
					}

					entities = append(entities, entity)
				}
			}
		}

		return nil
	})

	if err != nil {
		return []*ProposedEntity{}, fmt.Errorf("failed to walk directory: %w", err)
	}

	return entities, nil
}

// extractImports uses pre-compiled regex patterns to find import statements in Go source code.
// It handles both single-line (import "path") and multi-line (import ( ... )) import blocks.
// Uses package-level regex variables to avoid recompilation on each call.
func extractImports(content string) []string {
	var imports []string

	// Find single imports: import "path"
	matches := singleImportRegex.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) > 1 {
			imports = append(imports, match[1])
		}
	}

	// Find import blocks: import ( ... )
	blockMatches := blockImportRegex.FindAllStringSubmatch(content, -1)
	for _, match := range blockMatches {
		if len(match) > 1 {
			block := match[1]
			// Extract individual imports from the block
			lines := strings.Split(block, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				// Skip empty lines and comments
				if line == "" || strings.HasPrefix(line, "//") {
					continue
				}
				// Extract path from quoted string using pre-compiled regex
				pathMatches := pathRegex.FindStringSubmatch(line)
				if len(pathMatches) > 1 {
					imports = append(imports, pathMatches[1])
				}
			}
		}
	}

	return imports
}

// WriteBootstrapEntities writes proposed entities to .knowledge/ and returns bootstrap metadata.
// It creates a directory structure with entity files organized by kind, then writes a .bootstrap.json
// metadata file recording what was created and when.
func WriteBootstrapEntities(knowledgeDir string, entities []*ProposedEntity, profile *BootstrapProfile) (*BootstrapMetadata, error) {
	// Create the .knowledge/ directory structure if it doesn't exist
	if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create knowledge directory: %w", err)
	}

	// Track created entity IDs
	createdEntities := []string{}

	// Write each entity to its corresponding kind subdirectory
	for _, entity := range entities {
		// Create kind-specific subdirectory (e.g., .knowledge/module/)
		kindDir := filepath.Join(knowledgeDir, entity.Kind)
		if err := os.MkdirAll(kindDir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create kind directory %q: %w", kindDir, err)
		}

		// Build the markdown file with YAML frontmatter
		markdown := buildEntityMarkdown(entity)

		// Write entity file
		entityPath := filepath.Join(kindDir, entity.ID+".md")
		if err := os.WriteFile(entityPath, []byte(markdown), 0o644); err != nil {
			return nil, fmt.Errorf("failed to write entity file %q: %w", entityPath, err)
		}

		// Append to created entities list
		createdEntities = append(createdEntities, entity.ID)
	}

	// Create bootstrap metadata
	metadata := &BootstrapMetadata{
		Profile:         *profile,
		CreatedEntities: createdEntities,
		BootstrappedAt:  time.Now(),
	}

	// Marshal metadata to JSON
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bootstrap metadata: %w", err)
	}

	// Write .bootstrap.json file
	bootstrapPath := filepath.Join(knowledgeDir, ".bootstrap.json")
	if err := os.WriteFile(bootstrapPath, metadataJSON, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write bootstrap metadata file: %w", err)
	}

	return metadata, nil
}

// buildEntityMarkdown constructs a markdown file with YAML frontmatter for an entity.
// Frontmatter includes: id, kind, layer (always "base"), title, tags, source (always "bootstrap"), confidence
func buildEntityMarkdown(entity *ProposedEntity) string {
	// Build YAML frontmatter
	frontmatter := fmt.Sprintf(`---
id: %s
kind: %s
layer: base
title: %s
tags: %s
source: bootstrap
confidence: %g
---

%s`, entity.ID, entity.Kind, entity.Title, formatTagsYAML(entity.Tags), entity.Confidence, entity.Body)

	return frontmatter
}

// formatTagsYAML formats tags as a YAML array in the frontmatter.
// For example: ["security", "auth"]
func formatTagsYAML(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	result := "["
	for i, tag := range tags {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("\"%s\"", tag)
	}
	result += "]"
	return result
}
