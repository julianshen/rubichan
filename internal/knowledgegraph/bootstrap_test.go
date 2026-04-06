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
