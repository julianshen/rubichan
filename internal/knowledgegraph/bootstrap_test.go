package knowledgegraph

import (
	"encoding/json"
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
