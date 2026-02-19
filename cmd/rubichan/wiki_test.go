// cmd/rubichan/wiki_test.go
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWikiCmdExists(t *testing.T) {
	cmd := wikiCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "wiki [path]", cmd.Use)
}

func TestWikiCmdDefaultFlags(t *testing.T) {
	cmd := wikiCmd()

	format, _ := cmd.Flags().GetString("format")
	assert.Equal(t, "raw-md", format)

	output, _ := cmd.Flags().GetString("output")
	assert.Equal(t, "docs/wiki", output)

	diagrams, _ := cmd.Flags().GetString("diagrams")
	assert.Equal(t, "mermaid", diagrams)

	concurrency, _ := cmd.Flags().GetInt("concurrency")
	assert.Equal(t, 5, concurrency)
}
