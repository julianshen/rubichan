package knowledgegraph

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestBootstrapProfileMarshaling(t *testing.T) {
	profile := BootstrapProfile{
		ProjectName:         "myapp",
		BackendTechs:        []string{"Go", "PostgreSQL"},
		FrontendTechs:       []string{"React", "TypeScript"},
		DatabaseTechs:       []string{"PostgreSQL", "Redis"},
		InfrastructureTechs: []string{"Kubernetes", "Docker"},
		ArchitectureStyle:   "Microservices",
		PainPoints:          []string{"scaling", "monitoring"},
		TeamSize:            "medium",
		TeamComposition:     "fullstack",
		IsExisting:          true,
		CreatedAt:           time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC),
	}

	// Marshal to JSON
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("failed to marshal BootstrapProfile: %v", err)
	}

	// Unmarshal back
	var unmarshaled BootstrapProfile
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal BootstrapProfile: %v", err)
	}

	// Verify round-trip
	if unmarshaled.ProjectName != profile.ProjectName {
		t.Errorf("ProjectName mismatch: got %q, want %q", unmarshaled.ProjectName, profile.ProjectName)
	}
	if len(unmarshaled.BackendTechs) != len(profile.BackendTechs) {
		t.Errorf("BackendTechs length mismatch: got %d, want %d", len(unmarshaled.BackendTechs), len(profile.BackendTechs))
	}
	if unmarshaled.ArchitectureStyle != profile.ArchitectureStyle {
		t.Errorf("ArchitectureStyle mismatch: got %q, want %q", unmarshaled.ArchitectureStyle, profile.ArchitectureStyle)
	}
	if unmarshaled.IsExisting != profile.IsExisting {
		t.Errorf("IsExisting mismatch: got %v, want %v", unmarshaled.IsExisting, profile.IsExisting)
	}
	if !unmarshaled.CreatedAt.Equal(profile.CreatedAt) {
		t.Errorf("CreatedAt mismatch: got %v, want %v", unmarshaled.CreatedAt, profile.CreatedAt)
	}
}

func TestProposedEntityMarshaling(t *testing.T) {
	entity := ProposedEntity{
		ID:         "myapp-auth-module",
		Kind:       "module",
		Title:      "Authentication Module",
		Body:       "Handles user authentication and JWT token management",
		SourceType: "ast",
		Confidence: 0.85,
		Tags:       []string{"security", "auth", "jwt"},
	}

	// Marshal to JSON
	data, err := json.Marshal(entity)
	if err != nil {
		t.Fatalf("failed to marshal ProposedEntity: %v", err)
	}

	// Unmarshal back
	var unmarshaled ProposedEntity
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal ProposedEntity: %v", err)
	}

	// Verify round-trip
	if unmarshaled.ID != entity.ID {
		t.Errorf("ID mismatch: got %q, want %q", unmarshaled.ID, entity.ID)
	}
	if unmarshaled.Kind != entity.Kind {
		t.Errorf("Kind mismatch: got %q, want %q", unmarshaled.Kind, entity.Kind)
	}
	if unmarshaled.Title != entity.Title {
		t.Errorf("Title mismatch: got %q, want %q", unmarshaled.Title, entity.Title)
	}
	if unmarshaled.Confidence != entity.Confidence {
		t.Errorf("Confidence mismatch: got %f, want %f", unmarshaled.Confidence, entity.Confidence)
	}
	if len(unmarshaled.Tags) != len(entity.Tags) {
		t.Errorf("Tags length mismatch: got %d, want %d", len(unmarshaled.Tags), len(entity.Tags))
	}
}

func TestBootstrapMetadataMarshaling(t *testing.T) {
	profile := BootstrapProfile{
		ProjectName:         "myapp",
		BackendTechs:        []string{"Go"},
		FrontendTechs:       []string{"React"},
		DatabaseTechs:       []string{"PostgreSQL"},
		InfrastructureTechs: []string{"Kubernetes"},
		ArchitectureStyle:   "Microservices",
		PainPoints:          []string{"scaling"},
		TeamSize:            "medium",
		TeamComposition:     "fullstack",
		IsExisting:          true,
		CreatedAt:           time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC),
	}

	metadata := BootstrapMetadata{
		Profile:         profile,
		CreatedEntities: []string{"entity-1", "entity-2", "entity-3"},
		AnalysisMetadata: AnalysisMetadata{
			ModulesFound:         15,
			GitCommitsAnalyzed:   250,
			IntegrationsDetected: 8,
			AnalysisTimestamp:    time.Date(2026, 4, 6, 10, 30, 0, 0, time.UTC),
		},
		BootstrappedAt: time.Date(2026, 4, 6, 10, 45, 0, 0, time.UTC),
	}

	// Marshal to JSON
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("failed to marshal BootstrapMetadata: %v", err)
	}

	// Unmarshal back
	var unmarshaled BootstrapMetadata
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal BootstrapMetadata: %v", err)
	}

	// Verify Profile round-trip
	if unmarshaled.Profile.ProjectName != profile.ProjectName {
		t.Errorf("Profile.ProjectName mismatch: got %q, want %q", unmarshaled.Profile.ProjectName, profile.ProjectName)
	}

	// Verify CreatedEntities round-trip
	if len(unmarshaled.CreatedEntities) != len(metadata.CreatedEntities) {
		t.Errorf("CreatedEntities length mismatch: got %d, want %d", len(unmarshaled.CreatedEntities), len(metadata.CreatedEntities))
	}
	for i, id := range unmarshaled.CreatedEntities {
		if id != metadata.CreatedEntities[i] {
			t.Errorf("CreatedEntities[%d] mismatch: got %q, want %q", i, id, metadata.CreatedEntities[i])
		}
	}

	// Verify AnalysisMetadata round-trip
	if unmarshaled.AnalysisMetadata.ModulesFound != metadata.AnalysisMetadata.ModulesFound {
		t.Errorf("ModulesFound mismatch: got %d, want %d", unmarshaled.AnalysisMetadata.ModulesFound, metadata.AnalysisMetadata.ModulesFound)
	}
	if unmarshaled.AnalysisMetadata.GitCommitsAnalyzed != metadata.AnalysisMetadata.GitCommitsAnalyzed {
		t.Errorf("GitCommitsAnalyzed mismatch: got %d, want %d", unmarshaled.AnalysisMetadata.GitCommitsAnalyzed, metadata.AnalysisMetadata.GitCommitsAnalyzed)
	}
	if unmarshaled.AnalysisMetadata.IntegrationsDetected != metadata.AnalysisMetadata.IntegrationsDetected {
		t.Errorf("IntegrationsDetected mismatch: got %d, want %d", unmarshaled.AnalysisMetadata.IntegrationsDetected, metadata.AnalysisMetadata.IntegrationsDetected)
	}
	if !unmarshaled.AnalysisMetadata.AnalysisTimestamp.Equal(metadata.AnalysisMetadata.AnalysisTimestamp) {
		t.Errorf("AnalysisTimestamp mismatch: got %v, want %v", unmarshaled.AnalysisMetadata.AnalysisTimestamp, metadata.AnalysisMetadata.AnalysisTimestamp)
	}

	// Verify BootstrappedAt round-trip
	if !unmarshaled.BootstrappedAt.Equal(metadata.BootstrappedAt) {
		t.Errorf("BootstrappedAt mismatch: got %v, want %v", unmarshaled.BootstrappedAt, metadata.BootstrappedAt)
	}
}

// MockQuestioner implements Questioner interface for testing.
type MockQuestioner struct {
	responses map[string]interface{}
}

// AskString returns a string response from the mock responses.
func (m *MockQuestioner) AskString(prompt string) (string, error) {
	if val, ok := m.responses[prompt]; ok {
		return val.(string), nil
	}
	return "", nil
}

// AskChoice returns a single choice response from the mock responses.
func (m *MockQuestioner) AskChoice(prompt string, options []string) (string, error) {
	if val, ok := m.responses[prompt]; ok {
		return val.(string), nil
	}
	return "", nil
}

// AskMultiSelect returns multiple choice responses from the mock responses.
func (m *MockQuestioner) AskMultiSelect(prompt string, options []string) ([]string, error) {
	if val, ok := m.responses[prompt]; ok {
		return val.([]string), nil
	}
	return []string{}, nil
}

func TestCollectBootstrapProfile(t *testing.T) {
	mockQ := &MockQuestioner{
		responses: map[string]interface{}{
			"What is your project name?":                   "myapp",
			"Select backend technologies:":                 []string{"Go", "Python"},
			"Select frontend technologies:":                []string{"React", "Vue"},
			"Select database technologies:":                []string{"PostgreSQL", "MongoDB"},
			"Select infrastructure technologies:":          []string{"Kubernetes", "Docker"},
			"What is your architecture style?":             "Microservices",
			"Describe your pain points (comma-separated):": "scaling, monitoring, documentation",
			"What is your team size?":                      "medium",
			"What is your team composition?":               "fullstack",
			"Is this an existing project?":                 "yes",
		},
	}

	profile, err := CollectBootstrapProfile(mockQ)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if profile.ProjectName != "myapp" {
		t.Errorf("ProjectName: got %q, want %q", profile.ProjectName, "myapp")
	}

	if len(profile.BackendTechs) != 2 || profile.BackendTechs[0] != "Go" {
		t.Errorf("BackendTechs: got %v, want [Go Python]", profile.BackendTechs)
	}

	if len(profile.FrontendTechs) != 2 || profile.FrontendTechs[0] != "React" {
		t.Errorf("FrontendTechs: got %v, want [React Vue]", profile.FrontendTechs)
	}

	if profile.ArchitectureStyle != "Microservices" {
		t.Errorf("ArchitectureStyle: got %q, want %q", profile.ArchitectureStyle, "Microservices")
	}

	if len(profile.PainPoints) != 3 || profile.PainPoints[0] != "scaling" {
		t.Errorf("PainPoints: got %v, want [scaling monitoring documentation]", profile.PainPoints)
	}

	if profile.TeamSize != "medium" {
		t.Errorf("TeamSize: got %q, want %q", profile.TeamSize, "medium")
	}

	if profile.TeamComposition != "fullstack" {
		t.Errorf("TeamComposition: got %q, want %q", profile.TeamComposition, "fullstack")
	}

	if !profile.IsExisting {
		t.Errorf("IsExisting: got %v, want true", profile.IsExisting)
	}

	if profile.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestBootstrapProfileValidation(t *testing.T) {
	mockQ := &MockQuestioner{
		responses: map[string]interface{}{
			"What is your project name?": "",
		},
	}

	_, err := CollectBootstrapProfile(mockQ)
	if err == nil {
		t.Error("expected error for empty project name, got nil")
	}
}

func TestPainPointsParsing(t *testing.T) {
	mockQ := &MockQuestioner{
		responses: map[string]interface{}{
			"What is your project name?":                   "test",
			"Select backend technologies:":                 []string{},
			"Select frontend technologies:":                []string{},
			"Select database technologies:":                []string{},
			"Select infrastructure technologies:":          []string{},
			"What is your architecture style?":             "Monolithic",
			"Describe your pain points (comma-separated):": "  scaling  ,  monitoring  , documentation  ",
			"What is your team size?":                      "small",
			"What is your team composition?":               "backend",
			"Is this an existing project?":                 "no",
		},
	}

	profile, err := CollectBootstrapProfile(mockQ)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(profile.PainPoints) != 3 {
		t.Fatalf("expected 3 pain points, got %d", len(profile.PainPoints))
	}

	expected := []string{"scaling", "monitoring", "documentation"}
	for i, pp := range profile.PainPoints {
		if pp != expected[i] {
			t.Errorf("PainPoint[%d]: got %q, want %q", i, pp, expected[i])
		}
	}
}

func TestExistingProjectDetection(t *testing.T) {
	mockQ := &MockQuestioner{
		responses: map[string]interface{}{
			"What is your project name?":                   "test",
			"Select backend technologies:":                 []string{},
			"Select frontend technologies:":                []string{},
			"Select database technologies:":                []string{},
			"Select infrastructure technologies:":          []string{},
			"What is your architecture style?":             "Monolithic",
			"Describe your pain points (comma-separated):": "none",
			"What is your team size?":                      "small",
			"What is your team composition?":               "backend",
			"Is this an existing project?":                 "yes",
		},
	}

	profile, err := CollectBootstrapProfile(mockQ)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !profile.IsExisting {
		t.Error("IsExisting should be true when answer is 'yes'")
	}

	// Test "no" response
	mockQ.responses["Is this an existing project?"] = "no"
	profile, err = CollectBootstrapProfile(mockQ)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if profile.IsExisting {
		t.Error("IsExisting should be false when answer is 'no'")
	}
}

func TestDiscoverModules(t *testing.T) {
	// Create a temporary project structure with modules
	tmpDir := t.TempDir()

	// Create pkg/ directory with auth module
	pkgAuthDir := tmpDir + "/pkg/auth"
	if err := os.MkdirAll(pkgAuthDir, 0755); err != nil {
		t.Fatalf("failed to create pkg/auth directory: %v", err)
	}

	// Create pkg/ directory with database module
	pkgDatabaseDir := tmpDir + "/pkg/database"
	if err := os.MkdirAll(pkgDatabaseDir, 0755); err != nil {
		t.Fatalf("failed to create pkg/database directory: %v", err)
	}

	// Create internal/ directory with config module
	internalConfigDir := tmpDir + "/internal/config"
	if err := os.MkdirAll(internalConfigDir, 0755); err != nil {
		t.Fatalf("failed to create internal/config directory: %v", err)
	}

	// Run DiscoverModules
	entities, err := DiscoverModules(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverModules failed: %v", err)
	}

	// Verify entities were found
	if len(entities) < 3 {
		t.Errorf("expected at least 3 modules, got %d", len(entities))
	}

	// Verify module names
	moduleNames := make(map[string]bool)
	for _, entity := range entities {
		if entity.Kind != "module" {
			t.Errorf("expected kind 'module', got %q", entity.Kind)
		}
		if entity.Confidence != 0.9 {
			t.Errorf("expected confidence 0.9, got %f", entity.Confidence)
		}
		moduleNames[entity.ID] = true
	}

	if !moduleNames["auth"] {
		t.Error("expected 'auth' module not found")
	}
	if !moduleNames["database"] {
		t.Error("expected 'database' module not found")
	}
	if !moduleNames["config"] {
		t.Error("expected 'config' module not found")
	}
}

func TestFormatModuleTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"auth", "Auth"},
		{"user_management", "User Management"},
		{"api_handler", "Api Handler"},
		{"database", "Database"},
		{"a", "A"},
	}

	for _, tt := range tests {
		result := formatModuleTitle(tt.input)
		if result != tt.expected {
			t.Errorf("formatModuleTitle(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestDiscoverDecisionsFromGit(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init", tmpDir)
	if err := cmd.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}

	// Configure git user for commits
	configNameCmd := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User")
	if err := configNameCmd.Run(); err != nil {
		t.Fatalf("failed to configure git user name: %v", err)
	}

	configEmailCmd := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com")
	if err := configEmailCmd.Run(); err != nil {
		t.Fatalf("failed to configure git user email: %v", err)
	}

	// Create test file and commit with architecture keyword
	testFile := tmpDir + "/test.txt"
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	addCmd := exec.Command("git", "-C", tmpDir, "add", "test.txt")
	if err := addCmd.Run(); err != nil {
		t.Fatalf("failed to git add: %v", err)
	}

	commitCmd := exec.Command("git", "-C", tmpDir, "commit", "-m", "[STRUCTURAL] architecture: refactor to microservices")
	if err := commitCmd.Run(); err != nil {
		t.Fatalf("failed to git commit: %v", err)
	}

	// Run DiscoverDecisionsFromGit
	profile := &BootstrapProfile{}
	entities, err := DiscoverDecisionsFromGit(tmpDir, profile)
	if err != nil {
		t.Fatalf("DiscoverDecisionsFromGit failed: %v", err)
	}

	// Verify decision entity was found
	if len(entities) < 1 {
		t.Error("expected at least 1 decision entity, got 0")
	}

	if len(entities) > 0 {
		entity := entities[0]
		if entity.Kind != "decision" {
			t.Errorf("expected kind 'decision', got %q", entity.Kind)
		}
		if entity.SourceType != "git" {
			t.Errorf("expected source_type 'git', got %q", entity.SourceType)
		}
		if entity.Confidence <= 0 || entity.Confidence > 1 {
			t.Errorf("expected confidence between 0 and 1, got %f", entity.Confidence)
		}
	}
}

func TestDiscoverIntegrations(t *testing.T) {
	tmpDir := t.TempDir()

	// Create cmd/ directory
	cmdDir := tmpDir + "/cmd/myapp"
	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		t.Fatalf("failed to create cmd directory: %v", err)
	}

	// Create a Go file with pq and redis imports
	mainFile := cmdDir + "/main.go"
	mainContent := `package main

import (
	"github.com/lib/pq"
	"github.com/redis/go-redis/v8"
	"fmt"
)

func main() {
	fmt.Println("hello")
}
`
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to create main.go: %v", err)
	}

	// Run DiscoverIntegrations
	entities, err := DiscoverIntegrations(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverIntegrations failed: %v", err)
	}

	// Verify integration entities were found
	if len(entities) < 2 {
		t.Errorf("expected at least 2 integration entities, got %d", len(entities))
	}

	// Check for PostgreSQL and Redis integrations
	integrationTitles := make(map[string]bool)
	for _, entity := range entities {
		if entity.Kind != "integration" {
			t.Errorf("expected kind 'integration', got %q", entity.Kind)
		}
		if entity.SourceType != "integration" {
			t.Errorf("expected source_type 'integration', got %q", entity.SourceType)
		}
		if entity.Confidence != 0.85 {
			t.Errorf("expected confidence 0.85, got %f", entity.Confidence)
		}
		integrationTitles[entity.Title] = true
	}

	if !integrationTitles["PostgreSQL (pq driver)"] {
		t.Error("expected PostgreSQL integration not found")
	}
	if !integrationTitles["Redis"] {
		t.Error("expected Redis integration not found")
	}
}
