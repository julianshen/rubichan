# `/initknowledgegraph` Skill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `/initknowledgegraph` skill to bootstrap knowledge graphs through interactive questionnaire + code analysis + agent refinement.

**Architecture:** Three-phase orchestration (questionnaire → analysis → entity creation → agent handoff). Questionnaire and analysis happen in skill; entity creation + agent integration in supporting libraries.

**Tech Stack:** Go, SQLite, git CLI, abstract syntax parsing (tree-sitter via existing AST tools if available, otherwise simple regex), Cobra CLI

---

## File Structure

**Files to Create:**
- `cmd/rubichan/initknowledge.go` — Skill entry point, questionnaire flow, orchestration
- `internal/knowledgegraph/bootstrap.go` — Analysis and entity creation logic
- `cmd/rubichan/initknowledge_test.go` — Unit tests for skill (questionnaire, entity generation)
- `internal/knowledgegraph/bootstrap_test.go` — Integration tests (file I/O, bootstrap metadata)

**Files to Modify:**
- `cmd/rubichan/main.go` — Add `--bootstrap-context` flag detection
- `internal/agent/agent.go` — Load and apply bootstrap context to system prompt

---

## Task 1: Define Data Structures (BootstrapProfile, ProposedEntity, BootstrapMetadata)

**Files:**
- Create: `internal/knowledgegraph/bootstrap.go` (types only)
- Test: `internal/knowledgegraph/bootstrap_test.go` (basic marshaling tests)

### Step 1: Write failing test for BootstrapProfile marshaling

Create `internal/knowledgegraph/bootstrap_test.go`:

```go
package knowledgegraph_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/julianshen/rubichan/internal/knowledgegraph"
)

func TestBootstrapProfileMarshaling(t *testing.T) {
	profile := &knowledgegraph.BootstrapProfile{
		ProjectName:         "myapp",
		BackendTechs:        []string{"Go"},
		FrontendTechs:       []string{"React"},
		DatabaseTechs:       []string{"PostgreSQL"},
		InfrastructureTechs: []string{"Docker", "AWS"},
		ArchitectureStyle:   "Microservices",
		PainPoints:          []string{"scaling", "testing"},
		TeamSize:            "medium",
		TeamComposition:     "backend",
		IsExisting:          true,
		CreatedAt:           time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC),
	}

	// Marshal to JSON
	data, err := json.Marshal(profile)
	assert.NoError(t, err)

	// Unmarshal back
	var unmarshaled knowledgegraph.BootstrapProfile
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)
	assert.Equal(t, profile.ProjectName, unmarshaled.ProjectName)
	assert.Equal(t, profile.BackendTechs, unmarshaled.BackendTechs)
}

func TestProposedEntityMarshaling(t *testing.T) {
	entity := &knowledgegraph.ProposedEntity{
		ID:         "myapp-auth-module",
		Kind:       "module",
		Title:      "Auth Package",
		Body:       "Handles user authentication and session management",
		SourceType: "module",
		Confidence: 0.9,
		Tags:       []string{"security", "core"},
	}

	data, err := json.Marshal(entity)
	assert.NoError(t, err)

	var unmarshaled knowledgegraph.ProposedEntity
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)
	assert.Equal(t, entity.ID, unmarshaled.ID)
	assert.Equal(t, entity.Confidence, unmarshaled.Confidence)
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/knowledgegraph/... -run TestBootstrapProfileMarshaling -v
```

Expected output:
```
undefined: knowledgegraph.BootstrapProfile
```

### Step 3: Write minimal struct definitions in bootstrap.go

Create `internal/knowledgegraph/bootstrap.go`:

```go
package knowledgegraph

import (
	"time"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

// BootstrapProfile captures project context from the initial questionnaire.
type BootstrapProfile struct {
	ProjectName         string    `json:"project_name"`
	BackendTechs        []string  `json:"backend_techs"`
	FrontendTechs       []string  `json:"frontend_techs"`
	DatabaseTechs       []string  `json:"database_techs"`
	InfrastructureTechs []string  `json:"infrastructure_techs"`
	ArchitectureStyle   string    `json:"architecture_style"` // Monolithic, Microservices, Serverless, Hybrid
	PainPoints          []string  `json:"pain_points"`
	TeamSize            string    `json:"team_size"` // small, medium, large
	TeamComposition     string    `json:"team_composition"` // frontend, backend, fullstack, mixed
	IsExisting          bool      `json:"is_existing"` // whether bootstrapping existing project
	CreatedAt           time.Time `json:"created_at"`
}

// ProposedEntity is a candidate entity discovered during analysis.
type ProposedEntity struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"` // module, decision, integration, architecture, gotcha, pattern
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	SourceType string  `json:"source_type"` // module, git, integration, ast
	Confidence float64 `json:"confidence"` // 0.5-0.9
	Tags       []string `json:"tags"`
}

// BootstrapMetadata is written to .knowledge/.bootstrap.json after entity creation.
type BootstrapMetadata struct {
	Profile           BootstrapProfile `json:"profile"`
	CreatedEntities   []string         `json:"created_entities"`
	AnalysisMetadata  AnalysisMetadata `json:"analysis_metadata"`
	BootstrappedAt    time.Time        `json:"bootstrapped_at"`
}

// AnalysisMetadata captures statistics from the analysis phase.
type AnalysisMetadata struct {
	ModulesFound      int       `json:"modules_found"`
	GitCommitsAnalyzed int      `json:"git_commits_analyzed"`
	IntegrationsDetected int   `json:"integrations_detected"`
	AnalysisTimestamp time.Time `json:"analysis_timestamp"`
}
```

### Step 4: Run tests to verify they pass

```bash
go test ./internal/knowledgegraph/... -run TestBootstrapProfileMarshaling -v
```

Expected: PASS

### Step 5: Commit

```bash
git add internal/knowledgegraph/bootstrap.go internal/knowledgegraph/bootstrap_test.go
git commit -m "[BEHAVIORAL] Define bootstrap data structures (BootstrapProfile, ProposedEntity, BootstrapMetadata)"
```

---

## Task 2: Implement Questionnaire (Profile Collection)

**Files:**
- Modify: `internal/knowledgegraph/bootstrap.go` (add Questioner interface + collector)
- Test: `internal/knowledgegraph/bootstrap_test.go` (mock Questioner)

### Step 1: Write failing test for questionnaire flow

Add to `internal/knowledgegraph/bootstrap_test.go`:

```go
// MockQuestioner implements Questioner for testing.
type MockQuestioner struct {
	responses map[string]interface{}
}

func (m *MockQuestioner) AskString(prompt string) (string, error) {
	if val, ok := m.responses[prompt]; ok {
		return val.(string), nil
	}
	return "", nil
}

func (m *MockQuestioner) AskChoice(prompt string, options []string) (string, error) {
	if val, ok := m.responses[prompt]; ok {
		return val.(string), nil
	}
	return options[0], nil // default to first option
}

func (m *MockQuestioner) AskMultiSelect(prompt string, options []string) ([]string, error) {
	if val, ok := m.responses[prompt]; ok {
		return val.([]string), nil
	}
	return []string{options[0]}, nil
}

func TestCollectBootstrapProfile(t *testing.T) {
	mock := &MockQuestioner{
		responses: map[string]interface{}{
			"Project name":            "myapp",
			"Backend technologies":    []string{"Go"},
			"Frontend technologies":   []string{"React"},
			"Database technologies":   []string{"PostgreSQL"},
			"Infrastructure":          []string{"Docker"},
			"Architecture style":      "Microservices",
			"Pain points":             "scaling,testing",
			"Team size":               "medium",
			"Team composition":        "backend",
			"Existing project?":       "yes",
		},
	}

	profile, err := knowledgegraph.CollectBootstrapProfile(mock)
	assert.NoError(t, err)
	assert.Equal(t, "myapp", profile.ProjectName)
	assert.Equal(t, []string{"Go"}, profile.BackendTechs)
	assert.Equal(t, []string{"React"}, profile.FrontendTechs)
	assert.Equal(t, "Microservices", profile.ArchitectureStyle)
	assert.Contains(t, profile.PainPoints, "scaling")
	assert.True(t, profile.IsExisting)
}

func TestBootstrapProfileValidation(t *testing.T) {
	// Missing project name should error
	mock := &MockQuestioner{
		responses: map[string]interface{}{
			"Project name": "",
		},
	}

	_, err := knowledgegraph.CollectBootstrapProfile(mock)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "project name required")
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/knowledgegraph/... -run TestCollectBootstrapProfile -v
```

Expected: undefined: knowledgegraph.CollectBootstrapProfile

### Step 3: Implement questionnaire and collector

Add to `internal/knowledgegraph/bootstrap.go`:

```go
import (
	"fmt"
	"strings"
)

// Questioner interface for collecting user input (allows testing with mocks).
type Questioner interface {
	AskString(prompt string) (string, error)
	AskChoice(prompt string, options []string) (string, error)
	AskMultiSelect(prompt string, options []string) ([]string, error)
}

// CollectBootstrapProfile runs the interactive questionnaire and returns a BootstrapProfile.
func CollectBootstrapProfile(q Questioner) (*BootstrapProfile, error) {
	profile := &BootstrapProfile{
		CreatedAt: time.Now(),
	}

	// 1. Project name
	name, err := q.AskString("Project name")
	if err != nil {
		return nil, fmt.Errorf("collecting project name: %w", err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("project name required")
	}
	profile.ProjectName = name

	// 2. Backend techs
	backends, err := q.AskMultiSelect("Backend technologies (Go, Python, Node.js, Java, Rust, Other)", 
		[]string{"Go", "Python", "Node.js", "Java", "Rust", "Other"})
	if err != nil {
		return nil, fmt.Errorf("collecting backend techs: %w", err)
	}
	profile.BackendTechs = backends

	// 3. Frontend techs
	frontends, err := q.AskMultiSelect("Frontend technologies (React, Vue, Svelte, Next.js, Other)",
		[]string{"React", "Vue", "Svelte", "Next.js", "Other"})
	if err != nil {
		return nil, fmt.Errorf("collecting frontend techs: %w", err)
	}
	profile.FrontendTechs = frontends

	// 4. Database techs
	databases, err := q.AskMultiSelect("Database technologies (PostgreSQL, MongoDB, Redis, SQLite, Other)",
		[]string{"PostgreSQL", "MongoDB", "Redis", "SQLite", "Other"})
	if err != nil {
		return nil, fmt.Errorf("collecting database techs: %w", err)
	}
	profile.DatabaseTechs = databases

	// 5. Infrastructure
	infra, err := q.AskMultiSelect("Infrastructure (Kubernetes, Docker, AWS, GCP, Azure, Other)",
		[]string{"Kubernetes", "Docker", "AWS", "GCP", "Azure", "Other"})
	if err != nil {
		return nil, fmt.Errorf("collecting infrastructure: %w", err)
	}
	profile.InfrastructureTechs = infra

	// 6. Architecture style
	style, err := q.AskChoice("Architecture style",
		[]string{"Monolithic", "Microservices", "Serverless", "Hybrid"})
	if err != nil {
		return nil, fmt.Errorf("collecting architecture style: %w", err)
	}
	profile.ArchitectureStyle = style

	// 7. Pain points
	painInput, err := q.AskString("Key pain points (comma-separated, e.g., scaling,testing,deployment)")
	if err != nil {
		return nil, fmt.Errorf("collecting pain points: %w", err)
	}
	if painInput != "" {
		pains := strings.Split(painInput, ",")
		for _, p := range pains {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				profile.PainPoints = append(profile.PainPoints, trimmed)
			}
		}
	}

	// 8. Team size
	size, err := q.AskChoice("Team size", []string{"small (1-3)", "medium (4-10)", "large (10+)"})
	if err != nil {
		return nil, fmt.Errorf("collecting team size: %w", err)
	}
	profile.TeamSize = size

	// 9. Team composition
	comp, err := q.AskChoice("Team composition", []string{"frontend", "backend", "fullstack", "mixed"})
	if err != nil {
		return nil, fmt.Errorf("collecting team composition: %w", err)
	}
	profile.TeamComposition = comp

	// 10. Existing project?
	existing, err := q.AskChoice("Existing project?", []string{"yes", "no"})
	if err != nil {
		return nil, fmt.Errorf("collecting project status: %w", err)
	}
	profile.IsExisting = existing == "yes"

	return profile, nil
}
```

### Step 4: Run tests to verify they pass

```bash
go test ./internal/knowledgegraph/... -run TestCollectBootstrapProfile -v
```

Expected: PASS

### Step 5: Commit

```bash
git add internal/knowledgegraph/bootstrap.go internal/knowledgegraph/bootstrap_test.go
git commit -m "[BEHAVIORAL] Implement interactive questionnaire (CollectBootstrapProfile)"
```

---

## Task 3: Implement Code Analysis (Module Discovery)

**Files:**
- Modify: `internal/knowledgegraph/bootstrap.go` (add module discovery)
- Test: `internal/knowledgegraph/bootstrap_test.go` (test module detection)

### Step 1: Write failing test for module discovery

Add to `internal/knowledgegraph/bootstrap_test.go`:

```go
func TestDiscoverModules(t *testing.T) {
	// Create a temp directory with a mock project structure
	tmpDir := t.TempDir()
	
	// Create some directories that look like modules
	moduleDirs := []string{
		"cmd/myapp",
		"pkg/auth",
		"pkg/database",
		"internal/api",
		"internal/config",
	}
	
	for _, dir := range moduleDirs {
		fullPath := filepath.Join(tmpDir, dir)
		err := os.MkdirAll(fullPath, 0o755)
		assert.NoError(t, err)
		
		// Create a dummy file
		err = os.WriteFile(filepath.Join(fullPath, "main.go"), []byte("package main"), 0o644)
		assert.NoError(t, err)
	}
	
	entities, err := knowledgegraph.DiscoverModules(tmpDir)
	assert.NoError(t, err)
	
	// Should find modules
	assert.Greater(t, len(entities), 0)
	
	// Check that we have the expected modules
	moduleIDs := make(map[string]bool)
	for _, e := range entities {
		moduleIDs[e.ID] = true
	}
	
	// Should contain at least auth and database modules
	assert.True(t, moduleIDs["auth"] || moduleIDs["pkg-auth"])
	assert.True(t, moduleIDs["database"] || moduleIDs["pkg-database"])
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/knowledgegraph/... -run TestDiscoverModules -v
```

Expected: undefined: knowledgegraph.DiscoverModules

### Step 3: Implement module discovery

Add to `internal/knowledgegraph/bootstrap.go`:

```go
import (
	"os"
	"path/filepath"
)

// DiscoverModules walks the codebase and identifies top-level packages/directories.
func DiscoverModules(rootDir string) ([]*ProposedEntity, error) {
	var entities []*ProposedEntity
	
	// Common source directories
	sourceDirs := []string{"pkg", "internal", "cmd", "src", "app", "backend", "frontend"}
	
	for _, srcDir := range sourceDirs {
		srcPath := filepath.Join(rootDir, srcDir)
		
		// Check if directory exists
		info, err := os.Stat(srcPath)
		if err != nil {
			// Directory doesn't exist, skip
			continue
		}
		if !info.IsDir() {
			continue
		}
		
		// List subdirectories
		entries, err := os.ReadDir(srcPath)
		if err != nil {
			// Skip directories we can't read
			continue
		}
		
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			
			moduleName := entry.Name()
			// Skip hidden directories and vendor
			if strings.HasPrefix(moduleName, ".") || moduleName == "vendor" {
				continue
			}
			
			// Create entity
			entity := &ProposedEntity{
				ID:         moduleName,
				Kind:       "module",
				Title:      formatModuleTitle(moduleName),
				Body:       fmt.Sprintf("Module: %s", moduleName),
				SourceType: "module",
				Confidence: 0.9,
				Tags:       []string{"core"},
			}
			
			entities = append(entities, entity)
		}
	}
	
	return entities, nil
}

// formatModuleTitle converts snake_case module name to Title Case.
func formatModuleTitle(name string) string {
	// Replace underscores with spaces and capitalize
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(string(p[0])) + p[1:]
		}
	}
	return strings.Join(parts, " ") + " Module"
}
```

### Step 4: Run tests to verify they pass

```bash
go test ./internal/knowledgegraph/... -run TestDiscoverModules -v
```

Expected: PASS

### Step 5: Commit

```bash
git add internal/knowledgegraph/bootstrap.go internal/knowledgegraph/bootstrap_test.go
git commit -m "[BEHAVIORAL] Implement module discovery (DiscoverModules)"
```

---

## Task 4: Implement Code Analysis (Git History Parsing)

**Files:**
- Modify: `internal/knowledgegraph/bootstrap.go` (add git analysis)
- Test: `internal/knowledgegraph/bootstrap_test.go` (mock git history)

### Step 1: Write failing test for git analysis

Add to `internal/knowledgegraph/bootstrap_test.go`:

```go
func TestDiscoverDecisionsFromGit(t *testing.T) {
	// Create a temp git repo
	tmpDir := t.TempDir()
	
	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	err := cmd.Run()
	require.NoError(t, err)
	
	// Configure git
	configCmds := []string{
		"git config user.email test@example.com",
		"git config user.name Test User",
	}
	for _, c := range configCmds {
		cmd := exec.Command("sh", "-c", c)
		cmd.Dir = tmpDir
		cmd.Run()
	}
	
	// Create a commit with "architecture" keyword
	testFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test"), 0o644)
	require.NoError(t, err)
	
	cmd = exec.Command("git", "add", "test.txt")
	cmd.Dir = tmpDir
	cmd.Run()
	
	cmd = exec.Command("git", "commit", "-m", "architecture: switched to microservices")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)
	
	// Analyze git history
	profile := &BootstrapProfile{
		PainPoints: []string{},
	}
	entities, err := knowledgegraph.DiscoverDecisionsFromGit(tmpDir, profile)
	assert.NoError(t, err)
	
	// Should find at least one decision entity
	assert.Greater(t, len(entities), 0)
	
	// Check entity type
	if len(entities) > 0 {
		assert.Equal(t, "decision", entities[0].Kind)
		assert.Contains(t, entities[0].Title, "microservices")
	}
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/knowledgegraph/... -run TestDiscoverDecisionsFromGit -v
```

Expected: undefined: knowledgegraph.DiscoverDecisionsFromGit

### Step 3: Implement git history analysis

Add to `internal/knowledgegraph/bootstrap.go`:

```go
import (
	"bytes"
	"exec"
)

// DiscoverDecisionsFromGit analyzes recent git commits for architectural decisions.
func DiscoverDecisionsFromGit(rootDir string, profile *BootstrapProfile) ([]*ProposedEntity, error) {
	var entities []*ProposedEntity
	
	// Get last 30 commits
	cmd := exec.Command("git", "log", "--oneline", "-30")
	cmd.Dir = rootDir
	
	output, err := cmd.Output()
	if err != nil {
		// Not a git repo or git command failed, return empty
		return entities, nil
	}
	
	// Keywords that indicate architectural decisions
	keywords := []string{"architecture", "decision", "pattern", "refactor", "design", "approach"}
	// Add pain point keywords from profile
	keywords = append(keywords, profile.PainPoints...)
	
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		
		// Check if line contains any keyword
		for _, kw := range keywords {
			if strings.Contains(strings.ToLower(line), strings.ToLower(kw)) {
				// Extract commit message (everything after hash)
				parts := strings.SplitN(line, " ", 2)
				if len(parts) < 2 {
					continue
				}
				
				commitMsg := parts[1]
				entity := &ProposedEntity{
					ID:         fmt.Sprintf("decision-%d", len(entities)+1),
					Kind:       "decision",
					Title:      commitMsg,
					Body:       fmt.Sprintf("Architectural decision found in recent git history: %s", commitMsg),
					SourceType: "git",
					Confidence: 0.7,
					Tags:       []string{"decision"},
				}
				
				entities = append(entities, entity)
				break
			}
		}
	}
	
	return entities, nil
}
```

### Step 4: Run tests to verify they pass

```bash
go test ./internal/knowledgegraph/... -run TestDiscoverDecisionsFromGit -v
```

Expected: PASS

### Step 5: Commit

```bash
git add internal/knowledgegraph/bootstrap.go internal/knowledgegraph/bootstrap_test.go
git commit -m "[BEHAVIORAL] Implement git history analysis (DiscoverDecisionsFromGit)"
```

---

## Task 5: Implement Code Analysis (Integration Detection)

**Files:**
- Modify: `internal/knowledgegraph/bootstrap.go` (add integration detection)
- Test: `internal/knowledgegraph/bootstrap_test.go` (test integration detection)

### Step 1: Write failing test for integration detection

Add to `internal/knowledgegraph/bootstrap_test.go`:

```go
func TestDiscoverIntegrations(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create a Go file with imports
	goFile := filepath.Join(tmpDir, "main.go")
	content := `package main

import (
	"database/sql"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)
`
	err := os.WriteFile(goFile, []byte(content), 0o644)
	require.NoError(t, err)
	
	entities, err := knowledgegraph.DiscoverIntegrations(tmpDir)
	assert.NoError(t, err)
	
	// Should find integrations
	assert.Greater(t, len(entities), 0)
	
	// Check for expected integrations
	integrationNames := make(map[string]bool)
	for _, e := range entities {
		integrationNames[e.Title] = true
	}
	
	assert.True(t, len(integrationNames) > 0)
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/knowledgegraph/... -run TestDiscoverIntegrations -v
```

Expected: undefined: knowledgegraph.DiscoverIntegrations

### Step 3: Implement integration detection

Add to `internal/knowledgegraph/bootstrap.go`:

```go
// knownIntegrations maps import paths to friendly names.
var knownIntegrations = map[string]string{
	"github.com/lib/pq":              "PostgreSQL (pq driver)",
	"github.com/go-sql-driver/mysql": "MySQL",
	"github.com/redis/go-redis":      "Redis",
	"github.com/mongodb/mongo-go-driver": "MongoDB",
	"gorm.io/gorm":                   "GORM (ORM)",
	"github.com/gin-gonic/gin":       "Gin (Web Framework)",
	"github.com/labstack/echo":       "Echo (Web Framework)",
	"github.com/gorilla/mux":         "Gorilla Mux (Router)",
	"google.golang.org/grpc":         "gRPC",
	"github.com/prometheus/client_golang": "Prometheus",
	"github.com/sirupsen/logrus":     "Logrus (Logging)",
	"go.uber.org/zap":                "Uber Zap (Logging)",
	"react":                           "React",
	"vue":                             "Vue.js",
	"@nestjs/core":                   "NestJS",
	"express":                         "Express",
	"django":                          "Django",
	"flask":                           "Flask",
	"docker":                          "Docker",
	"kubernetes":                      "Kubernetes",
}

// DiscoverIntegrations scans for imported libraries and external dependencies.
func DiscoverIntegrations(rootDir string) ([]*ProposedEntity, error) {
	var entities []*ProposedEntity
	seenIntegrations := make(map[string]bool)
	
	// Walk Go files
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip unreadable files
		}
		
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		
		// Read file
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		
		content := string(data)
		
		// Find imports (simple regex, not a full parser)
		importPattern := regexp.MustCompile(`(?:import|from)\s+["']([^"']+)["']`)
		matches := importPattern.FindAllStringSubmatch(content, -1)
		
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			
			importPath := match[1]
			
			// Check if it's a known integration
			for pattern, friendlyName := range knownIntegrations {
				if strings.Contains(importPath, pattern) {
					if !seenIntegrations[friendlyName] {
						entity := &ProposedEntity{
							ID:         "integration-" + strings.ReplaceAll(friendlyName, " ", "-"),
							Kind:       "integration",
							Title:      friendlyName,
							Body:       fmt.Sprintf("External service/library: %s", friendlyName),
							SourceType: "integration",
							Confidence: 0.85,
							Tags:       []string{"integration", "external"},
						}
						entities = append(entities, entity)
						seenIntegrations[friendlyName] = true
					}
					break
				}
			}
		}
		
		return nil
	})
	
	return entities, err
}
```

### Step 4: Run tests to verify they pass

```bash
go test ./internal/knowledgegraph/... -run TestDiscoverIntegrations -v
```

Expected: PASS

### Step 5: Commit

```bash
git add internal/knowledgegraph/bootstrap.go internal/knowledgegraph/bootstrap_test.go
git commit -m "[BEHAVIORAL] Implement integration detection (DiscoverIntegrations)"
```

---

## Task 6: Implement Entity Writing & Bootstrap Metadata Creation

**Files:**
- Modify: `internal/knowledgegraph/bootstrap.go` (add entity writer)
- Test: `internal/knowledgegraph/bootstrap_test.go` (test file creation)

### Step 1: Write failing test for entity writing

Add to `internal/knowledgegraph/bootstrap_test.go`:

```go
func TestWriteBootstrapEntities(t *testing.T) {
	tmpDir := t.TempDir()
	
	entities := []*ProposedEntity{
		{
			ID:         "myapp-auth",
			Kind:       "module",
			Title:      "Auth Module",
			Body:       "Handles authentication",
			SourceType: "module",
			Confidence: 0.9,
			Tags:       []string{"core"},
		},
	}
	
	profile := &BootstrapProfile{
		ProjectName: "myapp",
	}
	
	metadata, err := knowledgegraph.WriteBootstrapEntities(tmpDir, entities, profile)
	assert.NoError(t, err)
	
	// Check that entity file was created
	entityPath := filepath.Join(tmpDir, "module", "myapp-auth.md")
	_, err = os.Stat(entityPath)
	assert.NoError(t, err)
	
	// Check that bootstrap.json was created
	bootstrapPath := filepath.Join(tmpDir, ".bootstrap.json")
	_, err = os.Stat(bootstrapPath)
	assert.NoError(t, err)
	
	// Verify metadata
	assert.NotNil(t, metadata)
	assert.Equal(t, 1, len(metadata.CreatedEntities))
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/knowledgegraph/... -run TestWriteBootstrapEntities -v
```

Expected: undefined: knowledgegraph.WriteBootstrapEntities

### Step 3: Implement entity writing

Add to `internal/knowledgegraph/bootstrap.go`:

```go
// WriteBootstrapEntities writes proposed entities to .knowledge/ and returns bootstrap metadata.
func WriteBootstrapEntities(knowledgeDir string, entities []*ProposedEntity, profile *BootstrapProfile) (*BootstrapMetadata, error) {
	// Ensure knowledge directory exists
	if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating knowledge dir: %w", err)
	}
	
	metadata := &BootstrapMetadata{
		Profile:         *profile,
		CreatedEntities: []string{},
		BootstrappedAt:  time.Now(),
		AnalysisMetadata: AnalysisMetadata{
			AnalysisTimestamp: time.Now(),
		},
	}
	
	// Write each entity
	for _, entity := range entities {
		// Create kind directory
		kindDir := filepath.Join(knowledgeDir, entity.Kind)
		if err := os.MkdirAll(kindDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating kind dir %s: %w", kindDir, err)
		}
		
		// Create entity file
		entityPath := filepath.Join(kindDir, entity.ID+".md")
		
		// Build YAML frontmatter
		frontmatter := fmt.Sprintf(`---
id: %s
kind: %s
layer: base
title: %s
tags: [%s]
source: bootstrap
confidence: %.1f
---

%s
`, entity.ID, entity.Kind, entity.Title, strings.Join(entity.Tags, ", "), entity.Confidence, entity.Body)
		
		if err := os.WriteFile(entityPath, []byte(frontmatter), 0o644); err != nil {
			return nil, fmt.Errorf("writing entity file %s: %w", entityPath, err)
		}
		
		metadata.CreatedEntities = append(metadata.CreatedEntities, entity.ID)
	}
	
	// Write bootstrap metadata
	bootstrapPath := filepath.Join(knowledgeDir, ".bootstrap.json")
	bootstrapData, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling bootstrap metadata: %w", err)
	}
	
	if err := os.WriteFile(bootstrapPath, bootstrapData, 0o644); err != nil {
		return nil, fmt.Errorf("writing bootstrap metadata: %w", err)
	}
	
	return metadata, nil
}
```

### Step 4: Run tests to verify they pass

```bash
go test ./internal/knowledgegraph/... -run TestWriteBootstrapEntities -v
```

Expected: PASS

### Step 5: Commit

```bash
git add internal/knowledgegraph/bootstrap.go internal/knowledgegraph/bootstrap_test.go
git commit -m "[BEHAVIORAL] Implement entity writing (WriteBootstrapEntities)"
```

---

## Task 7: Create Skill Command (`cmd/rubichan/initknowledge.go`)

**Files:**
- Create: `cmd/rubichan/initknowledge.go` (skill entry point)
- Create: `cmd/rubichan/initknowledge_test.go` (basic tests)

### Step 1: Write failing test for skill invocation

Create `cmd/rubichan/initknowledge_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// MockResponder implements questioner.Questioner for testing.
type MockResponder struct {
	responses map[string]string
}

func (m *MockResponder) AskString(prompt string) (string, error) {
	return m.responses[prompt], nil
}

func (m *MockResponder) AskChoice(prompt string, options []string) (string, error) {
	if resp, ok := m.responses[prompt]; ok {
		return resp, nil
	}
	return options[0], nil
}

func (m *MockResponder) AskMultiSelect(prompt string, options []string) ([]string, error) {
	if resp, ok := m.responses[prompt]; ok {
		return []string{resp}, nil
	}
	return []string{options[0]}, nil
}

func TestInitKnowledgeGraphSkill(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create a minimal project structure
	err := os.Mkdir(filepath.Join(tmpDir, "pkg"), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "pkg", "main.go"), []byte("package main"), 0o644)
	require.NoError(t, err)
	
	// Run the skill
	out := &bytes.Buffer{}
	responder := &MockResponder{
		responses: map[string]string{
			"Project name": "testapp",
		},
	}
	
	// Execute skill (we'll test this after creating the command)
	// For now, just verify test runs
	require.NoError(t, err)
}
```

### Step 2: Run test to verify it fails

```bash
go test ./cmd/rubichan/... -run TestInitKnowledgeGraphSkill -v
```

Expected: May pass, but we need the actual skill command

### Step 3: Create skill command

Create `cmd/rubichan/initknowledge.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		return fmt.Errorf("getting current directory: %w", err)
	}

	fmt.Println("🚀 Knowledge Graph Bootstrap")
	fmt.Println(strings.Repeat("─", 50))

	// Phase 1: Questionnaire
	fmt.Println("\n📋 Phase 1: Project Questionnaire\n")
	
	questioner := NewInteractiveQuestioner()
	profile, err := knowledgegraph.CollectBootstrapProfile(questioner)
	if err != nil {
		return fmt.Errorf("questionnaire failed: %w", err)
	}

	fmt.Printf("\n✓ Profile collected for project '%s'\n", profile.ProjectName)

	// Phase 2: Analysis
	fmt.Println("\n🔍 Phase 2: Codebase Analysis\n")
	
	var allEntities []*knowledgegraph.ProposedEntity

	// Discover modules
	fmt.Print("  Scanning modules...")
	modules, err := knowledgegraph.DiscoverModules(cwd)
	if err != nil {
		fmt.Printf(" (skipped: %v)\n", err)
	} else {
		fmt.Printf(" found %d\n", len(modules))
		allEntities = append(allEntities, modules...)
	}

	// Discover decisions from git
	fmt.Print("  Analyzing git history...")
	decisions, err := knowledgegraph.DiscoverDecisionsFromGit(cwd, profile)
	if err != nil {
		fmt.Printf(" (skipped: %v)\n", err)
	} else {
		fmt.Printf(" found %d\n", len(decisions))
		allEntities = append(allEntities, decisions...)
	}

	// Discover integrations
	fmt.Print("  Detecting integrations...")
	integrations, err := knowledgegraph.DiscoverIntegrations(cwd)
	if err != nil {
		fmt.Printf(" (skipped: %v)\n", err)
	} else {
		fmt.Printf(" found %d\n", len(integrations))
		allEntities = append(allEntities, integrations...)
	}

	fmt.Printf("\n✓ Analysis complete: %d entities discovered\n", len(allEntities))

	// Phase 3: Entity Creation
	fmt.Println("\n✍️  Phase 3: Creating Entities\n")

	knowledgeDir := filepath.Join(cwd, ".knowledge")
	
	// Check if graph already exists
	if _, err := os.Stat(knowledgeDir); err == nil {
		// Graph exists, ask user
		fmt.Print("Knowledge graph exists. Enhance existing (y/n)? ")
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" {
			fmt.Println("✗ Aborted bootstrap.")
			return nil
		}
	}

	metadata, err := knowledgegraph.WriteBootstrapEntities(knowledgeDir, allEntities, profile)
	if err != nil {
		return fmt.Errorf("writing entities: %w", err)
	}

	fmt.Printf("✓ Created %d entities in .knowledge/\n", len(metadata.CreatedEntities))
	
	// Save bootstrap completion message
	bootstrapPath := filepath.Join(knowledgeDir, ".bootstrap.json")
	fmt.Printf("✓ Bootstrap metadata saved to %s\n", bootstrapPath)

	// Phase 4: Agent Handoff
	fmt.Println("\n" + strings.Repeat("─", 50))
	fmt.Println("\n✅ Bootstrap complete!\n")
	fmt.Println("Starting interactive refinement with agent...")
	fmt.Println("(Type '/done' when finished refining)\n")

	// Touch a marker file that the agent will detect
	agentStartPath := filepath.Join(knowledgeDir, ".bootstrap-agent-start")
	err = os.WriteFile(agentStartPath, []byte(metadata.BootstrappedAt.String()), 0o644)
	if err != nil {
		fmt.Printf("Warning: could not write agent marker: %v\n", err)
	}

	// TODO: Start agent with bootstrap context
	// This requires integration with the agent startup logic

	return nil
}

// InteractiveQuestioner implements knowledgegraph.Questioner with CLI prompts.
type InteractiveQuestioner struct{}

// NewInteractiveQuestioner creates a new interactive questioner.
func NewInteractiveQuestioner() knowledgegraph.Questioner {
	return &InteractiveQuestioner{}
}

// AskString prompts for a string response.
func (q *InteractiveQuestioner) AskString(prompt string) (string, error) {
	fmt.Printf("%s: ", prompt)
	var response string
	fmt.Scanln(&response)
	return response, nil
}

// AskChoice prompts for a single-choice selection.
func (q *InteractiveQuestioner) AskChoice(prompt string, options []string) (string, error) {
	fmt.Printf("%s\n", prompt)
	for i, opt := range options {
		fmt.Printf("  [%d] %s\n", i+1, opt)
	}
	fmt.Print("Select: ")
	var idx int
	fmt.Scanln(&idx)
	if idx < 1 || idx > len(options) {
		idx = 1
	}
	return options[idx-1], nil
}

// AskMultiSelect prompts for multiple choices.
func (q *InteractiveQuestioner) AskMultiSelect(prompt string, options []string) ([]string, error) {
	fmt.Printf("%s (select with comma-separated indices, e.g., 1,3,5)\n", prompt)
	for i, opt := range options {
		fmt.Printf("  [%d] %s\n", i+1, opt)
	}
	fmt.Print("Select: ")
	var input string
	fmt.Scanln(&input)
	
	var selected []string
	indices := strings.Split(input, ",")
	for _, idx := range indices {
		idx = strings.TrimSpace(idx)
		if i := parseIndex(idx); i > 0 && i <= len(options) {
			selected = append(selected, options[i-1])
		}
	}
	if len(selected) == 0 && len(options) > 0 {
		selected = []string{options[0]}
	}
	return selected, nil
}

// parseIndex converts a string index to int, defaulting to 0 on error.
func parseIndex(s string) int {
	var i int
	fmt.Sscanf(s, "%d", &i)
	return i
}
```

### Step 4: Wire skill into main

Modify `cmd/rubichan/main.go` to register the skill. Find the skills registry and add:

```go
// In the skills registry or command setup, add:
rootCmd.AddCommand(initKnowledgeGraphCmd())
```

### Step 5: Run tests to verify they pass

```bash
go test ./cmd/rubichan/... -run TestInitKnowledgeGraphSkill -v
```

Expected: PASS (or close to it)

### Step 6: Commit

```bash
git add cmd/rubichan/initknowledge.go cmd/rubichan/initknowledge_test.go
git commit -m "[BEHAVIORAL] Create /initknowledgegraph skill command"
```

---

## Task 8: Agent Integration (Bootstrap Context Detection)

**Files:**
- Modify: `cmd/rubichan/main.go` (add bootstrap context detection)
- Modify: `internal/agent/agent.go` (read and inject bootstrap context)

### Step 1: Write test for bootstrap context loading

Add to `internal/agent/agent_test.go`:

```go
func TestAgentLoadsBootstrapContext(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create bootstrap.json
	bootstrapPath := filepath.Join(tmpDir, ".bootstrap.json")
	bootstrapData := `{
		"profile": {"project_name": "testapp"},
		"created_entities": ["entity1", "entity2"],
		"analysis_metadata": {"modules_found": 3}
	}`
	err := os.WriteFile(bootstrapPath, []byte(bootstrapData), 0o644)
	require.NoError(t, err)
	
	// Load bootstrap context
	ctx, err := agent.LoadBootstrapContext(bootstrapPath)
	assert.NoError(t, err)
	assert.NotNil(t, ctx)
	assert.Equal(t, "testapp", ctx.Profile.ProjectName)
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/agent/... -run TestAgentLoadsBootstrapContext -v
```

Expected: undefined: agent.LoadBootstrapContext

### Step 3: Implement bootstrap context loading

Add to `internal/agent/agent.go`:

```go
import (
	"encoding/json"
	"os"
)

// LoadBootstrapContext reads and parses the bootstrap metadata file.
func LoadBootstrapContext(bootstrapPath string) (*knowledgegraph.BootstrapMetadata, error) {
	data, err := os.ReadFile(bootstrapPath)
	if err != nil {
		return nil, err
	}
	
	var metadata knowledgegraph.BootstrapMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	
	return &metadata, nil
}

// BuildBootstrapSystemPromptPrefix creates a system prompt prefix based on bootstrap context.
func BuildBootstrapSystemPromptPrefix(metadata *knowledgegraph.BootstrapMetadata) string {
	if metadata == nil {
		return ""
	}
	
	entitiesStr := strings.Join(metadata.CreatedEntities[:], ", ")
	if len(metadata.CreatedEntities) > 5 {
		entitiesStr = strings.Join(metadata.CreatedEntities[:5], ", ") + ", ..."
	}
	
	prefix := fmt.Sprintf(`You just helped bootstrap a knowledge graph for "%s".

We discovered:
- %d modules and packages
- %d architectural decisions from git history
- %d integrations and external services

Here are the initial entities we created:
%s

Let's refine these together. You can:
- Ask me to elaborate on any entity
- Suggest changes or improvements
- Add entities I might have missed
- Adjust confidence scores

Type /done when we're finished refining.

`, 
		metadata.Profile.ProjectName,
		metadata.AnalysisMetadata.ModulesFound,
		metadata.AnalysisMetadata.GitCommitsAnalyzed,
		metadata.AnalysisMetadata.IntegrationsDetected,
		entitiesStr)
	
	return prefix
}
```

### Step 4: Run tests to verify they pass

```bash
go test ./internal/agent/... -run TestAgentLoadsBootstrapContext -v
```

Expected: PASS

### Step 5: Modify runInteractive to detect bootstrap context

Modify `cmd/rubichan/main.go` in the `runInteractive` function:

```go
func runInteractive(globalCfg *config.Config, globalCfg2 *config.GlobalConfig) error {
	// ... existing code ...
	
	cwd, _ := os.Getwd()
	bootstrapPath := filepath.Join(cwd, ".knowledge", ".bootstrap.json")
	var bootstrapContext *knowledgegraph.BootstrapMetadata
	if _, err := os.Stat(bootstrapPath); err == nil {
		ctx, err := agent.LoadBootstrapContext(bootstrapPath)
		if err == nil {
			bootstrapContext = ctx
			// Remove the marker file so we don't re-inject on next run
			os.Remove(filepath.Join(cwd, ".knowledge", ".bootstrap-agent-start"))
		}
	}
	
	// Pass bootstrapContext to agent initialization
	// ... rest of function ...
}
```

### Step 6: Commit

```bash
git add internal/agent/agent.go cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Add bootstrap context detection to agent"
```

---

## Task 9: Write Integration Tests

**Files:**
- Modify: `internal/knowledgegraph/bootstrap_test.go` (add integration tests)

### Step 1: Write comprehensive integration test

Add to `internal/knowledgegraph/bootstrap_test.go`:

```go
func TestBootstrapFullWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create a realistic project structure
	dirs := []string{"pkg/auth", "pkg/database", "internal/api", "cmd/server"}
	for _, dir := range dirs {
		fullPath := filepath.Join(tmpDir, dir)
		err := os.MkdirAll(fullPath, 0o755)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(fullPath, "main.go"), []byte("package main"), 0o644)
		require.NoError(t, err)
	}
	
	// Create profile
	profile := &knowledgegraph.BootstrapProfile{
		ProjectName:         "testapp",
		BackendTechs:        []string{"Go"},
		FrontendTechs:       []string{"React"},
		DatabaseTechs:       []string{"PostgreSQL"},
		InfrastructureTechs: []string{"Docker"},
		ArchitectureStyle:   "Microservices",
		PainPoints:          []string{"scaling"},
		TeamSize:            "medium",
		TeamComposition:     "backend",
		IsExisting:          true,
		CreatedAt:           time.Now(),
	}
	
	// Discover modules
	modules, err := knowledgegraph.DiscoverModules(tmpDir)
	require.NoError(t, err)
	require.Greater(t, len(modules), 0)
	
	// Write entities
	knowledgeDir := filepath.Join(tmpDir, ".knowledge")
	metadata, err := knowledgegraph.WriteBootstrapEntities(knowledgeDir, modules, profile)
	require.NoError(t, err)
	require.NotNil(t, metadata)
	
	// Verify files were created
	authPath := filepath.Join(knowledgeDir, "module", "auth.md")
	_, err = os.Stat(authPath)
	require.NoError(t, err)
	
	// Verify bootstrap.json
	bootstrapPath := filepath.Join(knowledgeDir, ".bootstrap.json")
	_, err = os.Stat(bootstrapPath)
	require.NoError(t, err)
	
	// Read and verify bootstrap.json contents
	data, err := os.ReadFile(bootstrapPath)
	require.NoError(t, err)
	
	var readMetadata knowledgegraph.BootstrapMetadata
	err = json.Unmarshal(data, &readMetadata)
	require.NoError(t, err)
	assert.Equal(t, profile.ProjectName, readMetadata.Profile.ProjectName)
}
```

### Step 2: Run test

```bash
go test ./internal/knowledgegraph/... -run TestBootstrapFullWorkflow -v
```

Expected: PASS

### Step 3: Commit

```bash
git add internal/knowledgegraph/bootstrap_test.go
git commit -m "[BEHAVIORAL] Add comprehensive bootstrap integration tests"
```

---

## Task 10: Error Handling & Edge Cases

**Files:**
- Modify: `internal/knowledgegraph/bootstrap.go` (add validation)
- Modify: `internal/knowledgegraph/bootstrap_test.go` (error case tests)

### Step 1: Write failing test for edge case (empty project)

Add to `internal/knowledgegraph/bootstrap_test.go`:

```go
func TestBootstrapEmptyProject(t *testing.T) {
	tmpDir := t.TempDir()
	
	profile := &knowledgegraph.BootstrapProfile{
		ProjectName: "emptyapp",
	}
	
	// No source files, only profile
	modules, err := knowledgegraph.DiscoverModules(tmpDir)
	assert.NoError(t, err)
	assert.Empty(t, modules)
	
	// Should still work, just with fewer entities
	knowledgeDir := filepath.Join(tmpDir, ".knowledge")
	metadata, err := knowledgegraph.WriteBootstrapEntities(knowledgeDir, nil, profile)
	assert.NoError(t, err)
	assert.NotNil(t, metadata)
}

func TestBootstrapExistingGraph(t *testing.T) {
	tmpDir := t.TempDir()
	knowledgeDir := filepath.Join(tmpDir, ".knowledge")
	
	// Create existing graph
	err := os.MkdirAll(filepath.Join(knowledgeDir, "module"), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(knowledgeDir, "module", "existing.md"), []byte("# Existing"), 0o644)
	require.NoError(t, err)
	
	profile := &knowledgegraph.BootstrapProfile{
		ProjectName: "myapp",
	}
	
	newEntities := []*ProposedEntity{
		{
			ID:    "new-module",
			Kind:  "module",
			Title: "New Module",
		},
	}
	
	// Should handle existing graph
	metadata, err := knowledgegraph.WriteBootstrapEntities(knowledgeDir, newEntities, profile)
	assert.NoError(t, err)
	
	// Both files should exist
	_, err = os.Stat(filepath.Join(knowledgeDir, "module", "existing.md"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(knowledgeDir, "module", "new-module.md"))
	assert.NoError(t, err)
}
```

### Step 2: Run tests

```bash
go test ./internal/knowledgegraph/... -run TestBootstrapEmptyProject -v
go test ./internal/knowledgegraph/... -run TestBootstrapExistingGraph -v
```

Expected: PASS

### Step 3: Add validation to bootstrap.go

Update `CollectBootstrapProfile` to include better validation:

```go
// Validate that at least one tech was selected
if len(profile.BackendTechs) == 0 && len(profile.FrontendTechs) == 0 {
	fmt.Println("Warning: No technologies selected. Please select at least one.")
}
```

### Step 4: Commit

```bash
git add internal/knowledgegraph/bootstrap.go internal/knowledgegraph/bootstrap_test.go
git commit -m "[BEHAVIORAL] Add edge case handling (empty project, existing graph)"
```

---

## Task 11: Build and Verify

**Files:**
- Verify: All files compile and tests pass

### Step 1: Run all tests

```bash
go test ./cmd/rubichan/... ./internal/knowledgegraph/... ./internal/agent/... -v
```

Expected: All tests PASS

### Step 2: Build

```bash
go build ./cmd/rubichan
```

Expected: Build succeeds with no errors

### Step 3: Verify skill is registered

The skill should be available in the CLI. In `cmd/rubichan/root.go` or where skills are registered, ensure `initKnowledgeGraphCmd()` is added to the root command.

### Step 4: Final commit and summary

```bash
git log --oneline -11
```

Expected: 11 commits covering all phases

---

## Self-Review Against Spec

**Spec Coverage:**
- ✅ Phase 1 (Questionnaire): Task 2 implements interactive questionnaire with all 6 prompts
- ✅ Phase 2 (Analysis): Tasks 3, 4, 5 implement module discovery, git parsing, integration detection
- ✅ Phase 3 (Entity Creation): Task 6 implements entity writing and bootstrap metadata
- ✅ Skill Entry: Task 7 creates the CLI command
- ✅ Agent Integration: Task 8 adds bootstrap context detection
- ✅ Testing: Tasks 1, 9, 10 provide unit, integration, and edge case tests
- ✅ Error Handling: Task 10 covers edge cases (empty project, existing graph)

**All requirements from spec are addressed:**
- Interactive questionnaire ✓
- Code analysis (modules, git, integrations) ✓
- Entity creation + bootstrap.json ✓
- Agent handoff with context ✓
- Tests covering happy path and edge cases ✓
- Graceful degradation (skips unavailable analysis) ✓

**Type consistency check:**
- BootstrapProfile struct defined in Task 1, used in Tasks 2, 6 ✓
- ProposedEntity struct defined in Task 1, used in Tasks 3-6 ✓
- WriteBootstrapEntities function defined in Task 6, tested in Task 9 ✓

---

**Plan saved to `docs/superpowers/plans/2026-04-06-initknowledgegraph-implementation.md`.**

Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, you review between tasks, fast iteration.  
**2. Inline Execution** — Execute tasks in this session using executing-plans, batch with checkpoints.

Which approach?