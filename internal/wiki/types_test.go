package wiki

import (
	"testing"

	"github.com/julianshen/rubichan/internal/parser"
	"github.com/stretchr/testify/assert"
)

func TestScannedFile_ZeroValue(t *testing.T) {
	var sf ScannedFile
	assert.Equal(t, "", sf.Path)
	assert.Equal(t, "", sf.Language)
	assert.Nil(t, sf.Functions)
	assert.Nil(t, sf.Imports)
	assert.Equal(t, int64(0), sf.Size)
	assert.Equal(t, "", sf.Module)
}

func TestScannedFile_FieldAccess(t *testing.T) {
	sf := ScannedFile{
		Path:     "internal/wiki/types.go",
		Language: "go",
		Functions: []parser.FunctionDef{
			{Name: "NewScanner", StartLine: 10, EndLine: 25},
		},
		Imports: []string{"fmt", "os"},
		Size:    1024,
		Module:  "github.com/julianshen/rubichan",
	}

	assert.Equal(t, "internal/wiki/types.go", sf.Path)
	assert.Equal(t, "go", sf.Language)
	assert.Len(t, sf.Functions, 1)
	assert.Equal(t, "NewScanner", sf.Functions[0].Name)
	assert.Equal(t, 10, sf.Functions[0].StartLine)
	assert.Equal(t, 25, sf.Functions[0].EndLine)
	assert.Equal(t, []string{"fmt", "os"}, sf.Imports)
	assert.Equal(t, int64(1024), sf.Size)
	assert.Equal(t, "github.com/julianshen/rubichan", sf.Module)
}

func TestChunk_ZeroValue(t *testing.T) {
	var c Chunk
	assert.Equal(t, "", c.Module)
	assert.Nil(t, c.Files)
	assert.Nil(t, c.Source)
}

func TestChunk_FieldAccess(t *testing.T) {
	c := Chunk{
		Module: "internal/wiki",
		Files: []ScannedFile{
			{Path: "types.go", Language: "go"},
			{Path: "scanner.go", Language: "go"},
		},
		Source: []byte("package wiki\n"),
	}

	assert.Equal(t, "internal/wiki", c.Module)
	assert.Len(t, c.Files, 2)
	assert.Equal(t, "types.go", c.Files[0].Path)
	assert.Equal(t, "scanner.go", c.Files[1].Path)
	assert.Equal(t, []byte("package wiki\n"), c.Source)
}

func TestAnalysisResult_ZeroValue(t *testing.T) {
	var ar AnalysisResult
	assert.Nil(t, ar.Modules)
	assert.Equal(t, "", ar.Architecture)
	assert.Equal(t, "", ar.KeyAbstractions)
	assert.Nil(t, ar.Suggestions)
}

func TestAnalysisResult_FieldAccess(t *testing.T) {
	ar := AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "parser", Summary: "Parses source code"},
		},
		Architecture:    "Layered architecture",
		KeyAbstractions: "Parser, Tree, FunctionDef",
		Suggestions:     []string{"Add caching", "Improve error handling"},
	}

	assert.Len(t, ar.Modules, 1)
	assert.Equal(t, "parser", ar.Modules[0].Module)
	assert.Equal(t, "Layered architecture", ar.Architecture)
	assert.Equal(t, "Parser, Tree, FunctionDef", ar.KeyAbstractions)
	assert.Equal(t, []string{"Add caching", "Improve error handling"}, ar.Suggestions)
}

func TestModuleAnalysis_ZeroValue(t *testing.T) {
	var ma ModuleAnalysis
	assert.Equal(t, "", ma.Module)
	assert.Equal(t, "", ma.Summary)
	assert.Equal(t, "", ma.KeyTypes)
	assert.Equal(t, "", ma.Patterns)
	assert.Equal(t, "", ma.Concerns)
}

func TestModuleAnalysis_FieldAccess(t *testing.T) {
	ma := ModuleAnalysis{
		Module:   "internal/parser",
		Summary:  "Multi-language source code parser",
		KeyTypes: "Parser, Tree, FunctionDef",
		Patterns: "Registry pattern for language support",
		Concerns: "Memory management with tree-sitter C bindings",
	}

	assert.Equal(t, "internal/parser", ma.Module)
	assert.Equal(t, "Multi-language source code parser", ma.Summary)
	assert.Equal(t, "Parser, Tree, FunctionDef", ma.KeyTypes)
	assert.Equal(t, "Registry pattern for language support", ma.Patterns)
	assert.Equal(t, "Memory management with tree-sitter C bindings", ma.Concerns)
}

func TestDiagram_ZeroValue(t *testing.T) {
	var d Diagram
	assert.Equal(t, "", d.Title)
	assert.Equal(t, "", d.Type)
	assert.Equal(t, "", d.Content)
}

func TestDiagram_Type(t *testing.T) {
	tests := []struct {
		name     string
		diagType string
	}{
		{name: "architecture type", diagType: "architecture"},
		{name: "dependency type", diagType: "dependency"},
		{name: "data-flow type", diagType: "data-flow"},
		{name: "sequence type", diagType: "sequence"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Diagram{
				Title:   "Test Diagram",
				Type:    tt.diagType,
				Content: "graph TD\n  A-->B",
			}
			assert.Equal(t, tt.diagType, d.Type)
			assert.Equal(t, "Test Diagram", d.Title)
			assert.Equal(t, "graph TD\n  A-->B", d.Content)
		})
	}
}

func TestDocument_ZeroValue(t *testing.T) {
	var d Document
	assert.Equal(t, "", d.Path)
	assert.Equal(t, "", d.Title)
	assert.Equal(t, "", d.Content)
}

func TestDocument_FieldAccess(t *testing.T) {
	d := Document{
		Path:    "wiki/architecture.md",
		Title:   "Architecture Overview",
		Content: "# Architecture\n\nThis document describes...",
	}

	assert.Equal(t, "wiki/architecture.md", d.Path)
	assert.Equal(t, "Architecture Overview", d.Title)
	assert.Equal(t, "# Architecture\n\nThis document describes...", d.Content)
}

func TestSkillWikiSection_ZeroValue(t *testing.T) {
	var s SkillWikiSection
	assert.Equal(t, "", s.SkillName)
	assert.Equal(t, "", s.Title)
	assert.Equal(t, "", s.Content)
	assert.Nil(t, s.Diagrams)
}

func TestSkillWikiSection_FieldAccess(t *testing.T) {
	s := SkillWikiSection{
		SkillName: "security-scanner",
		Title:     "Security Scanner",
		Content:   "Scans for vulnerabilities...",
		Diagrams: []Diagram{
			{Title: "Scan Flow", Type: "sequence", Content: "sequenceDiagram\n  A->>B: scan"},
			{Title: "Architecture", Type: "architecture", Content: "graph TD\n  S-->A"},
		},
	}

	assert.Equal(t, "security-scanner", s.SkillName)
	assert.Equal(t, "Security Scanner", s.Title)
	assert.Equal(t, "Scans for vulnerabilities...", s.Content)
	assert.Len(t, s.Diagrams, 2)
	assert.Equal(t, "Scan Flow", s.Diagrams[0].Title)
	assert.Equal(t, "sequence", s.Diagrams[0].Type)
	assert.Equal(t, "Architecture", s.Diagrams[1].Title)
	assert.Equal(t, "architecture", s.Diagrams[1].Type)
}

func TestSkillWikiSection_DiagramsHoldsDiagramSlice(t *testing.T) {
	diagrams := []Diagram{
		{Title: "D1", Type: "dependency", Content: "graph LR\n  A-->B"},
		{Title: "D2", Type: "data-flow", Content: "graph TD\n  C-->D"},
		{Title: "D3", Type: "architecture", Content: "graph TD\n  E-->F"},
	}

	s := SkillWikiSection{
		SkillName: "test-skill",
		Diagrams:  diagrams,
	}

	assert.IsType(t, []Diagram{}, s.Diagrams)
	assert.Len(t, s.Diagrams, 3)
	for i, d := range s.Diagrams {
		assert.Equal(t, diagrams[i].Title, d.Title)
		assert.Equal(t, diagrams[i].Type, d.Type)
		assert.Equal(t, diagrams[i].Content, d.Content)
	}
}
