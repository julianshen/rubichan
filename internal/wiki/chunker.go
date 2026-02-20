package wiki

import (
	"bytes"
	"fmt"
)

// SourceReader abstracts file reading for testability.
type SourceReader interface {
	ReadFile(path string) ([]byte, error)
}

// ChunkerConfig controls the chunking behavior.
type ChunkerConfig struct {
	MaxChunkSize int // maximum byte size of a single chunk's Source
	MaxFileLines int // files with more lines than this omit full source
}

// DefaultChunkerConfig returns sensible defaults for chunking.
func DefaultChunkerConfig() ChunkerConfig {
	return ChunkerConfig{
		MaxChunkSize: 100_000,
		MaxFileLines: 500,
	}
}

// ChunkFiles groups ScannedFile entries by module and builds structured text
// summaries suitable for LLM analysis. If a module's summary exceeds
// MaxChunkSize, it is split into multiple chunks.
// Returns nil for empty input.
func ChunkFiles(files []ScannedFile, reader SourceReader, cfg ChunkerConfig) ([]Chunk, error) {
	if len(files) == 0 {
		return nil, nil
	}

	// Group files by module, preserving discovery order.
	type moduleGroup struct {
		module string
		files  []ScannedFile
	}

	seen := map[string]int{} // module -> index in groups
	var groups []moduleGroup

	for _, f := range files {
		idx, ok := seen[f.Module]
		if !ok {
			idx = len(groups)
			seen[f.Module] = idx
			groups = append(groups, moduleGroup{module: f.Module})
		}
		groups[idx].files = append(groups[idx].files, f)
	}

	// Build chunks for each module group.
	var result []Chunk
	for _, g := range groups {
		chunks, err := buildModuleChunks(g.module, g.files, reader, cfg)
		if err != nil {
			return nil, err
		}
		result = append(result, chunks...)
	}

	return result, nil
}

// buildModuleChunks creates one or more chunks for a single module.
func buildModuleChunks(module string, files []ScannedFile, reader SourceReader, cfg ChunkerConfig) ([]Chunk, error) {
	preamble := fmt.Sprintf("# Module: %s\n\n", module)

	var chunks []Chunk
	var currentBuf bytes.Buffer
	var currentFiles []ScannedFile

	currentBuf.WriteString(preamble)

	for _, f := range files {
		summary, err := buildFileSummary(f, reader, cfg)
		if err != nil {
			return nil, fmt.Errorf("building summary for %s: %w", f.Path, err)
		}

		// If adding this file would exceed MaxChunkSize and we already have
		// content, flush the current chunk first.
		if currentBuf.Len()+len(summary) > cfg.MaxChunkSize && len(currentFiles) > 0 {
			chunks = append(chunks, Chunk{
				Module: module,
				Files:  currentFiles,
				Source: bytes.Clone(currentBuf.Bytes()),
			})
			currentBuf.Reset()
			currentBuf.WriteString(preamble)
			currentFiles = nil
		}

		currentBuf.Write(summary)
		currentFiles = append(currentFiles, f)
	}

	// Flush remaining content.
	if len(currentFiles) > 0 {
		chunks = append(chunks, Chunk{
			Module: module,
			Files:  currentFiles,
			Source: currentBuf.Bytes(),
		})
	}

	return chunks, nil
}

// buildFileSummary constructs a structured text summary for a single file.
func buildFileSummary(f ScannedFile, reader SourceReader, cfg ChunkerConfig) ([]byte, error) {
	var buf bytes.Buffer

	// File header
	buf.WriteString(fmt.Sprintf("## File: %s [%s]\n", f.Path, f.Language))

	// Function signatures
	if len(f.Functions) > 0 {
		buf.WriteString("### Functions\n")
		for _, fn := range f.Functions {
			buf.WriteString(fmt.Sprintf("- %s (lines %d-%d)\n", fn.Name, fn.StartLine, fn.EndLine))
		}
	}

	// Import list
	if len(f.Imports) > 0 {
		buf.WriteString("### Imports\n")
		for _, imp := range f.Imports {
			buf.WriteString(fmt.Sprintf("- %s\n", imp))
		}
	}

	// Full source for small files
	source, err := reader.ReadFile(f.Path)
	if err != nil {
		// If we cannot read the file, just skip the source section.
		buf.WriteString("\n")
		return buf.Bytes(), nil
	}

	lineCount := countLines(source)
	if lineCount < cfg.MaxFileLines {
		buf.WriteString("### Source\n```\n")
		buf.Write(source)
		if len(source) > 0 && source[len(source)-1] != '\n' {
			buf.WriteByte('\n')
		}
		buf.WriteString("```\n")
	}

	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// countLines counts the number of lines in data.
func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	count := 1
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	return count
}
