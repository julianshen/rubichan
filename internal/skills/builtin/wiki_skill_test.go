package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWikiManifest(t *testing.T) {
	m := WikiManifest()

	assert.Equal(t, "wiki", m.Name)
	assert.Equal(t, "1.0.0", m.Version)
	assert.Contains(t, m.Description, "wiki")
	require.Len(t, m.Types, 1)
	assert.Equal(t, skills.SkillTypeTool, m.Types[0])
	assert.Empty(t, m.Permissions, "built-in wiki needs no declared permissions")
	assert.Empty(t, string(m.Implementation.Backend), "built-in skills should not set a backend")
}

func TestWikiBackendLoad(t *testing.T) {
	backend := &WikiBackend{WorkDir: t.TempDir()}
	m := WikiManifest()

	err := backend.Load(m, noopChecker{})
	require.NoError(t, err)

	toolList := backend.Tools()
	require.Len(t, toolList, 1, "wiki skill should expose exactly 1 tool")
	assert.Equal(t, "generate_wiki", toolList[0].Name())

	// Description should be non-empty.
	assert.NotEmpty(t, toolList[0].Description())

	// InputSchema should be valid JSON.
	var schema map[string]interface{}
	err = json.Unmarshal(toolList[0].InputSchema(), &schema)
	require.NoError(t, err)
	assert.Equal(t, "object", schema["type"])

	// Hooks should be empty.
	assert.Empty(t, backend.Hooks())

	// Unload should succeed.
	assert.NoError(t, backend.Unload())
}

// mockLLMCompleter returns a canned response for any prompt.
type mockLLMCompleter struct {
	response string
}

func (m *mockLLMCompleter) Complete(_ context.Context, _ string) (string, error) {
	return m.response, nil
}

func TestWikiBackendToolExecute(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a minimal Go file for the wiki pipeline to scan.
	if err := os.MkdirAll(filepath.Join(tmpDir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "pkg", "main.go"), []byte(`package main

func main() {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(tmpDir, "wiki-out")

	mock := &mockLLMCompleter{
		response: `{"summary":"A test project","key_types":"none","patterns":"none","concerns":"none"}`,
	}

	backend := &WikiBackend{WorkDir: tmpDir, LLM: mock}
	err := backend.Load(WikiManifest(), noopChecker{})
	require.NoError(t, err)

	tool := backend.Tools()[0]

	input, _ := json.Marshal(map[string]interface{}{
		"path":   tmpDir,
		"outdir": outDir,
		"format": "raw-md",
	})

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError, "tool should not return an error result")
	assert.Contains(t, result.Content, outDir)

	// Verify output directory was created.
	_, statErr := os.Stat(outDir)
	assert.NoError(t, statErr, "output directory should exist after execution")
}
