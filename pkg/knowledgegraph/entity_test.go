package knowledgegraph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEntity(t *testing.T) {
	e := &Entity{
		ID:      "test-001",
		Kind:    KindArchitecture,
		Title:   "Test Architecture",
		Tags:    []string{"test", "architecture"},
		Body:    "This is a test entity body.",
		Source:  SourceManual,
		Created: time.Now(),
		Updated: time.Now(),
		Relationships: []Relationship{
			{Kind: RelJustifies, Target: "test-002"},
		},
	}

	require.Equal(t, "test-001", e.ID)
	require.Equal(t, KindArchitecture, e.Kind)
	require.Equal(t, "Test Architecture", e.Title)
	require.Len(t, e.Tags, 2)
	require.Len(t, e.Relationships, 1)
	require.Equal(t, RelJustifies, e.Relationships[0].Kind)
	require.Equal(t, "test-002", e.Relationships[0].Target)
}

func TestEntityKinds(t *testing.T) {
	kinds := []EntityKind{
		KindArchitecture,
		KindDecision,
		KindGotcha,
		KindPattern,
		KindModule,
		KindIntegration,
	}
	require.Len(t, kinds, 6)
}

func TestRelationshipKinds(t *testing.T) {
	kinds := []RelationshipKind{
		RelJustifies,
		RelRelatesTo,
		RelDependsOn,
		RelSupersedes,
		RelConflicts,
		RelImplements,
	}
	require.Len(t, kinds, 6)
}

func TestUpdateSources(t *testing.T) {
	sources := []UpdateSource{
		SourceLLM,
		SourceGit,
		SourceManual,
		SourceFile,
	}
	require.Len(t, sources, 4)
}

func TestEntityLayerConstants(t *testing.T) {
	// Verify all layer constants are defined and non-empty
	layers := []EntityLayer{
		EntityLayerBase,
		EntityLayerTeam,
		EntityLayerSession,
	}

	for _, layer := range layers {
		require.NotEmpty(t, layer)
	}

	// Verify specific values
	require.Equal(t, EntityLayer("base"), EntityLayerBase)
	require.Equal(t, EntityLayer("team"), EntityLayerTeam)
	require.Equal(t, EntityLayer("session"), EntityLayerSession)
}

func TestEntityHasLayerField(t *testing.T) {
	e := &Entity{
		ID:    "test-001",
		Kind:  KindArchitecture,
		Title: "Test Architecture",
		Layer: EntityLayerBase,
	}

	require.Equal(t, EntityLayerBase, e.Layer)

	e.Layer = EntityLayerTeam
	require.Equal(t, EntityLayerTeam, e.Layer)

	e.Layer = EntityLayerSession
	require.Equal(t, EntityLayerSession, e.Layer)
}
