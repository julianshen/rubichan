package knowledgegraph

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptions(t *testing.T) {
	c := &OpenConfig{}

	// Apply options
	WithKnowledgeDir("/custom/path").ApplyOption(c)
	require.Equal(t, "/custom/path", c.KnowledgeDir)

	WithDBPath("/db/path").ApplyOption(c)
	require.Equal(t, "/db/path", c.DBPath)

	e := &NullEmbedder{}
	WithEmbedder(e).ApplyOption(c)
	require.Equal(t, e, c.Embedder)
}
