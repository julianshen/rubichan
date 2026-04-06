package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/julianshen/rubichan/internal/agent"
)

// TestAppendKnowledgeGraphOptionReturnsLongerSlice verifies that appendKnowledgeGraphOption
// appends a knowledge graph option to the opts slice.
func TestAppendKnowledgeGraphOptionReturnsLongerSlice(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a .knowledge directory structure
	knowledgeDir := filepath.Join(tmpDir, ".knowledge")
	for _, subdir := range []string{"architecture", "decisions", "gotchas", "patterns", "modules", "integrations"} {
		if err := os.MkdirAll(filepath.Join(knowledgeDir, subdir), 0o755); err != nil {
			t.Fatalf("creating subdir: %v", err)
		}
	}

	// Empty opts slice
	var opts []agent.AgentOption

	// Call appendKnowledgeGraphOption
	newOpts := appendKnowledgeGraphOption(context.Background(), opts, tmpDir)

	// Should return a slice with at least one more option
	require.Greater(t, len(newOpts), len(opts), "appendKnowledgeGraphOption should append a knowledge graph option")
}

// TestAppendKnowledgeGraphOptionGracefulDegradation verifies graceful
// degradation when knowledge graph creation fails. This test uses /dev/null
// as a path which will cause openGraph to fail when trying to create directories.
func TestAppendKnowledgeGraphOptionGracefulDegradation(t *testing.T) {
	// Use /dev/null which will fail when trying to create subdirectories
	invalidPath := "/dev/null"

	var opts []agent.AgentOption
	initialLen := len(opts)

	// Call appendKnowledgeGraphOption
	newOpts := appendKnowledgeGraphOption(context.Background(), opts, invalidPath)

	// Should return unchanged (graceful degradation)
	require.Equal(t, initialLen, len(newOpts), "appendKnowledgeGraphOption should return original opts on error")
}
