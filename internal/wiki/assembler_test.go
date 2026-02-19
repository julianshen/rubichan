package wiki

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssembleCreatesIndexPage(t *testing.T) {
	analysis := &AnalysisResult{
		Architecture: "Layered architecture with HTTP handlers and a persistence layer.",
		Modules: []ModuleAnalysis{
			{Module: "internal/handler", Summary: "Handles HTTP requests"},
		},
	}

	docs, err := Assemble(analysis, nil, nil)
	require.NoError(t, err)

	var indexDoc *Document
	for i := range docs {
		if docs[i].Path == "_index.md" {
			indexDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, indexDoc, "_index.md should exist")
	assert.Contains(t, indexDoc.Content, "Layered architecture with HTTP handlers")
	assert.Contains(t, indexDoc.Content, "internal/handler")
}

func TestAssembleCreatesModulePages(t *testing.T) {
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "internal/handler", Summary: "Handles HTTP requests", KeyTypes: "Handler, Router", Patterns: "MVC", Concerns: ""},
			{Module: "internal/store", Summary: "Persistence layer", KeyTypes: "", Patterns: "Repository", Concerns: "No connection pooling"},
		},
	}

	docs, err := Assemble(analysis, nil, nil)
	require.NoError(t, err)

	// Find modules/_index.md
	var modulesIndex *Document
	for i := range docs {
		if docs[i].Path == "modules/_index.md" {
			modulesIndex = &docs[i]
			break
		}
	}
	require.NotNil(t, modulesIndex, "modules/_index.md should exist")
	assert.Contains(t, modulesIndex.Content, "internal/handler")
	assert.Contains(t, modulesIndex.Content, "internal/store")

	// Find individual module pages
	handlerPath := "modules/" + sanitizeID("internal/handler") + ".md"
	storePath := "modules/" + sanitizeID("internal/store") + ".md"

	var handlerDoc, storeDoc *Document
	for i := range docs {
		switch docs[i].Path {
		case handlerPath:
			handlerDoc = &docs[i]
		case storePath:
			storeDoc = &docs[i]
		}
	}

	require.NotNil(t, handlerDoc, "handler module page should exist at %s", handlerPath)
	assert.Contains(t, handlerDoc.Content, "Handles HTTP requests")
	assert.Contains(t, handlerDoc.Content, "Handler, Router")
	assert.Contains(t, handlerDoc.Content, "MVC")
	// Concerns is empty for handler, should not appear
	assert.NotContains(t, handlerDoc.Content, "## Concerns")

	require.NotNil(t, storeDoc, "store module page should exist at %s", storePath)
	assert.Contains(t, storeDoc.Content, "Persistence layer")
	assert.Contains(t, storeDoc.Content, "No connection pooling")
	// KeyTypes is empty for store, should not appear
	assert.NotContains(t, storeDoc.Content, "## Key Types")
}

func TestAssembleIncludesDiagrams(t *testing.T) {
	analysis := &AnalysisResult{
		Architecture: "Layered architecture.",
		Modules: []ModuleAnalysis{
			{Module: "internal/handler", Summary: "HTTP handler"},
		},
	}
	diagrams := []Diagram{
		{Title: "Architecture Overview", Type: "architecture", Content: "graph TD\n    A-->B"},
		{Title: "Module Dependencies", Type: "dependency", Content: "graph LR\n    A-->B"},
		{Title: "Data Flow", Type: "data-flow", Content: "flowchart LR\n    A-->B"},
		{Title: "Key Sequences", Type: "sequence", Content: "sequenceDiagram\n    A->>B: call"},
	}

	docs, err := Assemble(analysis, diagrams, nil)
	require.NoError(t, err)

	var archDoc *Document
	for i := range docs {
		if docs[i].Path == "architecture/overview.md" {
			archDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, archDoc, "architecture/overview.md should exist")
	assert.Contains(t, archDoc.Content, "```mermaid")
	assert.Contains(t, archDoc.Content, "graph TD")

	// Check dependency page
	var depDoc *Document
	for i := range docs {
		if docs[i].Path == "architecture/dependencies.md" {
			depDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, depDoc, "architecture/dependencies.md should exist")
	assert.Contains(t, depDoc.Content, "```mermaid")
	assert.Contains(t, depDoc.Content, "graph LR")

	// Check data-flow page
	var dataFlowDoc *Document
	for i := range docs {
		if docs[i].Path == "architecture/data-flow.md" {
			dataFlowDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, dataFlowDoc, "architecture/data-flow.md should exist")
	assert.Contains(t, dataFlowDoc.Content, "```mermaid")
	// Should contain data-flow and sequence diagrams
	assert.Contains(t, dataFlowDoc.Content, "flowchart LR")
	assert.Contains(t, dataFlowDoc.Content, "sequenceDiagram")
}

func TestAssembleIncludesSkillSections(t *testing.T) {
	analysis := &AnalysisResult{}
	skillSections := []SkillWikiSection{
		{
			SkillName: "test-skill",
			Title:     "Security Analysis",
			Content:   "This is the security analysis content.",
			Diagrams: []Diagram{
				{Title: "Threat Model", Type: "architecture", Content: "graph TD\n    Threat-->App"},
			},
		},
	}

	docs, err := Assemble(analysis, nil, skillSections)
	require.NoError(t, err)

	expectedPath := "skill-contributed/security-analysis.md"
	var skillDoc *Document
	for i := range docs {
		if docs[i].Path == expectedPath {
			skillDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, skillDoc, "skill-contributed page should exist at %s", expectedPath)
	assert.Contains(t, skillDoc.Content, "Security Analysis")
	assert.Contains(t, skillDoc.Content, "This is the security analysis content.")
	assert.Contains(t, skillDoc.Content, "```mermaid")
	assert.Contains(t, skillDoc.Content, "graph TD")
}

func TestAssembleCreatesSuggestionsPage(t *testing.T) {
	analysis := &AnalysisResult{
		Suggestions: []string{
			"Add more unit tests",
			"Improve error handling in the store package",
		},
	}

	docs, err := Assemble(analysis, nil, nil)
	require.NoError(t, err)

	var suggestionsDoc *Document
	for i := range docs {
		if docs[i].Path == "suggestions/improvements.md" {
			suggestionsDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, suggestionsDoc, "suggestions/improvements.md should exist")
	assert.Contains(t, suggestionsDoc.Content, "Add more unit tests")
	assert.Contains(t, suggestionsDoc.Content, "Improve error handling in the store package")
	// Verify they are bullet points
	assert.Contains(t, suggestionsDoc.Content, "- Add more unit tests")
	assert.Contains(t, suggestionsDoc.Content, "- Improve error handling in the store package")
}

func TestAssembleEmptyAnalysis(t *testing.T) {
	analysis := &AnalysisResult{}

	docs, err := Assemble(analysis, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, docs, "should produce at least one document")

	// _index.md must always exist
	var indexDoc *Document
	for i := range docs {
		if docs[i].Path == "_index.md" {
			indexDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, indexDoc, "_index.md should exist even with empty analysis")

	// Suggestions page should NOT exist when there are no suggestions
	for _, doc := range docs {
		assert.NotEqual(t, "suggestions/improvements.md", doc.Path,
			"suggestions page should not exist when there are no suggestions")
	}

	// Security page should still exist as placeholder
	var securityDoc *Document
	for i := range docs {
		if docs[i].Path == "security/overview.md" {
			securityDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, securityDoc, "security/overview.md should exist")
	assert.Contains(t, securityDoc.Content, "Security analysis pending...")
}

// TestAssembleModulePagesUseForwardSlashes verifies paths use forward slashes.
func TestAssembleModulePagesUseForwardSlashes(t *testing.T) {
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "internal/handler", Summary: "Handler"},
		},
	}

	docs, err := Assemble(analysis, nil, nil)
	require.NoError(t, err)

	for _, doc := range docs {
		assert.False(t, strings.Contains(doc.Path, "\\"),
			"document path %q should not contain backslashes", doc.Path)
	}
}
