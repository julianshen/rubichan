package knowledgegraph

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptions(t *testing.T) {
	c := &config{}

	// Apply options
	WithKnowledgeDir("/custom/path")(c)
	require.Equal(t, "/custom/path", c.knowledgeDir)

	WithDBPath("/db/path")(c)
	require.Equal(t, "/db/path", c.dbPath)

	e := &NullEmbedder{}
	WithEmbedder(e)(c)
	require.Equal(t, e, c.embedder)
}
