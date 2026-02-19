package wiki

import (
	"fmt"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- mock ----------

type mockSourceReader struct {
	files map[string][]byte
}

func (m *mockSourceReader) ReadFile(path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return data, nil
}

// ---------- tests ----------

func TestDefaultChunkerConfig(t *testing.T) {
	cfg := DefaultChunkerConfig()
	assert.Equal(t, 100_000, cfg.MaxChunkSize)
	assert.Equal(t, 500, cfg.MaxFileLines)
}

func TestChunkEmptyFiles(t *testing.T) {
	reader := &mockSourceReader{files: map[string][]byte{}}
	cfg := DefaultChunkerConfig()

	chunks, err := ChunkFiles(nil, reader, cfg)
	require.NoError(t, err)
	assert.Nil(t, chunks)
}

func TestChunkGroupsByModule(t *testing.T) {
	files := []ScannedFile{
		{Path: "cmd/main.go", Language: "go", Module: "cmd"},
		{Path: "internal/lib.go", Language: "go", Module: "internal"},
		{Path: "cmd/util.go", Language: "go", Module: "cmd"},
	}

	reader := &mockSourceReader{
		files: map[string][]byte{
			"cmd/main.go":     []byte("package main\n\nfunc Main() {}\n"),
			"internal/lib.go": []byte("package internal\n\nfunc Lib() {}\n"),
			"cmd/util.go":     []byte("package main\n\nfunc Util() {}\n"),
		},
	}
	cfg := DefaultChunkerConfig()

	chunks, err := ChunkFiles(files, reader, cfg)
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	// First chunk should be "cmd" (discovered first)
	assert.Equal(t, "cmd", chunks[0].Module)
	assert.Len(t, chunks[0].Files, 2)

	// Second chunk should be "internal"
	assert.Equal(t, "internal", chunks[1].Module)
	assert.Len(t, chunks[1].Files, 1)
}

func TestChunkSplitsLargeModules(t *testing.T) {
	// Use a very small MaxChunkSize to force splitting within a single module
	files := []ScannedFile{
		{Path: "mod/a.go", Language: "go", Module: "mod"},
		{Path: "mod/b.go", Language: "go", Module: "mod"},
		{Path: "mod/c.go", Language: "go", Module: "mod"},
	}

	reader := &mockSourceReader{
		files: map[string][]byte{
			"mod/a.go": []byte("package mod\n\nfunc A() {}\n"),
			"mod/b.go": []byte("package mod\n\nfunc B() {}\n"),
			"mod/c.go": []byte("package mod\n\nfunc C() {}\n"),
		},
	}

	cfg := ChunkerConfig{
		MaxChunkSize: 50, // Very small to force splits
		MaxFileLines: 500,
	}

	chunks, err := ChunkFiles(files, reader, cfg)
	require.NoError(t, err)
	require.Greater(t, len(chunks), 1, "expected multiple chunks due to small MaxChunkSize")

	// All chunks should belong to the same module
	for _, c := range chunks {
		assert.Equal(t, "mod", c.Module)
	}

	// Total files across all chunks should equal input count
	totalFiles := 0
	for _, c := range chunks {
		totalFiles += len(c.Files)
	}
	assert.Equal(t, 3, totalFiles)
}

func TestChunkSourceContainsFunctionSignatures(t *testing.T) {
	files := []ScannedFile{
		{
			Path:     "pkg/handler.go",
			Language: "go",
			Module:   "pkg",
			Functions: []parser.FunctionDef{
				{Name: "HandleRequest", StartLine: 10, EndLine: 25},
				{Name: "ValidateInput", StartLine: 27, EndLine: 40},
			},
			Imports: []string{"fmt", "net/http"},
		},
	}

	reader := &mockSourceReader{
		files: map[string][]byte{
			"pkg/handler.go": []byte("package pkg\n\nimport \"fmt\"\nimport \"net/http\"\n\nfunc HandleRequest() {}\nfunc ValidateInput() {}\n"),
		},
	}
	cfg := DefaultChunkerConfig()

	chunks, err := ChunkFiles(files, reader, cfg)
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	source := string(chunks[0].Source)

	// Should contain function names
	assert.True(t, strings.Contains(source, "HandleRequest"), "source should contain function name HandleRequest")
	assert.True(t, strings.Contains(source, "ValidateInput"), "source should contain function name ValidateInput")

	// Should contain import names
	assert.True(t, strings.Contains(source, "fmt"), "source should contain import fmt")
	assert.True(t, strings.Contains(source, "net/http"), "source should contain import net/http")

	// Should contain module preamble
	assert.True(t, strings.Contains(source, "pkg"), "source should contain module name")
}
