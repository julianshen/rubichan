package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectRelevantMemories(t *testing.T) {
	memories := []MemoryEntry{
		{Tag: "react", Content: "Use functional components with hooks"},
		{Tag: "go", Content: "Prefer interfaces for testability"},
		{Tag: "css", Content: "Use CSS modules for scoping"},
		{Tag: "react-testing", Content: "Use React Testing Library"},
	}

	t.Run("filters by keyword overlap", func(t *testing.T) {
		result := SelectRelevantMemories(memories, "how do I test react components", 2)
		assert.Len(t, result, 2)
		// Both "react" and "react-testing" match "react" and "components"/"test".
		// They tie on score; original order is preserved ("react" first).
		assert.Equal(t, "react", result[0].Tag)
		assert.Equal(t, "react-testing", result[1].Tag)
	})

	t.Run("returns all when maxResults is zero", func(t *testing.T) {
		result := SelectRelevantMemories(memories, "go interfaces", 0)
		assert.Len(t, result, 4)
		// go should be first.
		assert.Equal(t, "go", result[0].Tag)
	})

	t.Run("returns nil for empty memories", func(t *testing.T) {
		result := SelectRelevantMemories(nil, "query", 5)
		assert.Nil(t, result)
	})

	t.Run("returns first N when query is empty", func(t *testing.T) {
		result := SelectRelevantMemories(memories, "", 2)
		assert.Len(t, result, 2)
		assert.Equal(t, "react", result[0].Tag)
		assert.Equal(t, "go", result[1].Tag)
	})

	t.Run("limits to maxResults", func(t *testing.T) {
		result := SelectRelevantMemories(memories, "react", 1)
		assert.Len(t, result, 1)
		// "react" comes first in original slice and ties on score.
		assert.Equal(t, "react", result[0].Tag)
	})
}

func TestExtractWords(t *testing.T) {
	words := extractWords("Hello, world! This is a test.")
	assert.Equal(t, []string{"hello", "world", "this", "test"}, words)
}

func TestExtractWords_FiltersShortWords(t *testing.T) {
	words := extractWords("a an the go")
	assert.Equal(t, []string{"the"}, words)
}
