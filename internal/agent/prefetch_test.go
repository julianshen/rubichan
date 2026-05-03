package agent

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestPrefetchManagerNilDeps(t *testing.T) {
	pm := NewPrefetchManager(nil, nil)

	ctx := context.Background()
	memHandle := pm.StartMemoryPrefetch(ctx, "test", 1000)
	skillHandle := pm.StartSkillPrefetch(ctx, skills.TriggerContext{})

	// Should complete immediately with nil deps
	entities, err := memHandle.Consume(ctx)
	require.NoError(t, err)
	require.Nil(t, entities)

	_, err = skillHandle.Consume(ctx)
	require.NoError(t, err)
}
