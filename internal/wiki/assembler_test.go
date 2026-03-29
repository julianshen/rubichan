package wiki

import (
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/parser"
	"github.com/julianshen/rubichan/internal/security"
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

	docs, err := Assemble(analysis, nil, nil, nil, nil)
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

	docs, err := Assemble(analysis, nil, nil, nil, nil)
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

	docs, err := Assemble(analysis, diagrams, nil, nil, nil)
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

	docs, err := Assemble(analysis, nil, skillSections, nil, nil)
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

	docs, err := Assemble(analysis, nil, nil, nil, nil)
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

	docs, err := Assemble(analysis, nil, nil, nil, nil)
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

	docs, err := Assemble(analysis, nil, nil, nil, nil)
	require.NoError(t, err)

	for _, doc := range docs {
		assert.False(t, strings.Contains(doc.Path, "\\"),
			"document path %q should not contain backslashes", doc.Path)
	}
}

func TestAssembleWithSecurityFindings(t *testing.T) {
	analysis := &AnalysisResult{}
	findings := []security.Finding{
		{
			Title:    "Hardcoded API Key",
			Scanner:  "secrets",
			Severity: security.SeverityHigh,
			Location: security.Location{
				File:      "config/settings.go",
				StartLine: 42,
			},
			Description: "API key found in source code.",
		},
		{
			Title:    "SQL Injection Risk",
			Scanner:  "sast",
			Severity: security.SeverityCritical,
			Location: security.Location{
				File:      "internal/db/query.go",
				StartLine: 15,
			},
			Description: "Unsanitized input used in SQL query.",
		},
	}

	docs, err := Assemble(analysis, nil, nil, findings, nil)
	require.NoError(t, err)

	var secDoc *Document
	for i := range docs {
		if docs[i].Path == "security/overview.md" {
			secDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, secDoc, "security/overview.md should exist")

	// Finding titles are present.
	assert.Contains(t, secDoc.Content, "Hardcoded API Key")
	assert.Contains(t, secDoc.Content, "SQL Injection Risk")

	// File:line references are present.
	assert.Contains(t, secDoc.Content, "config/settings.go:42")
	assert.Contains(t, secDoc.Content, "internal/db/query.go:15")

	// Severity summary counts.
	assert.Contains(t, secDoc.Content, "| critical | 1 |")
	assert.Contains(t, secDoc.Content, "| high | 1 |")

	// Must NOT contain the placeholder text.
	assert.NotContains(t, secDoc.Content, "pending")
}

func TestAssembleWithNoSecurityFindings(t *testing.T) {
	analysis := &AnalysisResult{}

	// nil findings
	docs, err := Assemble(analysis, nil, nil, nil, nil)
	require.NoError(t, err)

	var secDoc *Document
	for i := range docs {
		if docs[i].Path == "security/overview.md" {
			secDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, secDoc, "security/overview.md should exist")
	assert.Contains(t, secDoc.Content, "Security analysis pending...")

	// empty slice findings
	docs2, err := Assemble(analysis, nil, nil, []security.Finding{}, nil)
	require.NoError(t, err)

	var secDoc2 *Document
	for i := range docs2 {
		if docs2[i].Path == "security/overview.md" {
			secDoc2 = &docs2[i]
			break
		}
	}
	require.NotNil(t, secDoc2, "security/overview.md should exist")
	assert.Contains(t, secDoc2.Content, "Security analysis pending...")
}

func TestModulePageIncludesTestingSection(t *testing.T) {
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "internal/foo", Summary: "Foo module"},
			{Module: "internal/bar", Summary: "Bar module"},
		},
	}

	// internal/foo has a test file; internal/bar does not.
	files := []ScannedFile{
		{Path: "internal/foo/foo.go", Module: "internal/foo"},
		{Path: "internal/foo/foo_test.go", Module: "internal/foo"},
		{Path: "internal/bar/bar.go", Module: "internal/bar"},
	}

	docs, err := Assemble(analysis, nil, nil, nil, files)
	require.NoError(t, err)

	fooPath := "modules/" + sanitizeID("internal/foo") + ".md"
	barPath := "modules/" + sanitizeID("internal/bar") + ".md"

	var fooDoc, barDoc *Document
	for i := range docs {
		switch docs[i].Path {
		case fooPath:
			fooDoc = &docs[i]
		case barPath:
			barDoc = &docs[i]
		}
	}

	require.NotNil(t, fooDoc, "foo module page should exist")
	assert.Contains(t, fooDoc.Content, "## Testing")
	assert.Contains(t, fooDoc.Content, "Test files detected")

	require.NotNil(t, barDoc, "bar module page should exist")
	assert.Contains(t, barDoc.Content, "## Testing")
	assert.Contains(t, barDoc.Content, "No test files detected")
}

func TestModulePageIncludesPublicInterface(t *testing.T) {
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "internal/mymod", Summary: "My module"},
		},
	}

	files := []ScannedFile{
		{
			Path:   "internal/mymod/mymod.go",
			Module: "internal/mymod",
			Functions: []parser.FunctionDef{
				{Name: "ExportedFunc"},
				{Name: "unexportedFunc"},
				{Name: "AnotherExported"},
			},
		},
	}

	docs, err := Assemble(analysis, nil, nil, nil, files)
	require.NoError(t, err)

	modPath := "modules/" + sanitizeID("internal/mymod") + ".md"
	var modDoc *Document
	for i := range docs {
		if docs[i].Path == modPath {
			modDoc = &docs[i]
			break
		}
	}

	require.NotNil(t, modDoc, "module page should exist")
	assert.Contains(t, modDoc.Content, "## Public Interface")
	assert.Contains(t, modDoc.Content, "`ExportedFunc`")
	assert.Contains(t, modDoc.Content, "`AnotherExported`")
	assert.NotContains(t, modDoc.Content, "`unexportedFunc`")
}

func TestModulePageNoPublicInterfaceSectionWhenAllUnexported(t *testing.T) {
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "internal/internal", Summary: "Internal only"},
		},
	}

	files := []ScannedFile{
		{
			Path:   "internal/internal/impl.go",
			Module: "internal/internal",
			Functions: []parser.FunctionDef{
				{Name: "doSomething"},
				{Name: "helper"},
			},
		},
	}

	docs, err := Assemble(analysis, nil, nil, nil, files)
	require.NoError(t, err)

	modPath := "modules/" + sanitizeID("internal/internal") + ".md"
	var modDoc *Document
	for i := range docs {
		if docs[i].Path == modPath {
			modDoc = &docs[i]
			break
		}
	}

	require.NotNil(t, modDoc, "module page should exist")
	assert.NotContains(t, modDoc.Content, "## Public Interface")
}

func TestHasTestFiles(t *testing.T) {
	tests := []struct {
		name  string
		files []ScannedFile
		want  bool
	}{
		{"go test file (suffix)", []ScannedFile{{Path: "pkg/foo_test.go"}}, true},
		{"ts test file (suffix)", []ScannedFile{{Path: "src/foo.test.ts"}}, true},
		{"ts spec file (suffix)", []ScannedFile{{Path: "src/foo.spec.ts"}}, true},
		{"py test file (prefix)", []ScannedFile{{Path: "tests/test_foo.py"}}, true},
		{"go test file (prefix)", []ScannedFile{{Path: "internal/test_helper.go"}}, true},
		{"no test files", []ScannedFile{{Path: "pkg/foo.go"}, {Path: "src/bar.ts"}}, false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasTestFiles(tt.files))
		})
	}
}

func TestExportedFunctions(t *testing.T) {
	files := []ScannedFile{
		{
			Functions: []parser.FunctionDef{
				{Name: "PublicA"},
				{Name: "privateB"},
				{Name: "PublicC"},
			},
		},
		{
			Functions: []parser.FunctionDef{
				{Name: "PublicA"}, // duplicate — should appear only once
				{Name: "PublicD"},
			},
		},
	}
	got := exportedFunctions(files)
	assert.Equal(t, []string{"PublicA", "PublicC", "PublicD"}, got)
}

func TestIsExported(t *testing.T) {
	assert.True(t, isExported("Foo"))
	assert.True(t, isExported("FooBar"))
	assert.False(t, isExported("foo"))
	assert.False(t, isExported(""))
}

func TestSanitizeMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"escapes script tags", "hello <script>alert('xss')</script>", "hello &lt;script&gt;alert('xss')&lt;/script&gt;"},
		{"escapes angle brackets", "a < b > c", "a &lt; b &gt; c"},
		{"escapes ampersands", "a & b", "a &amp; b"},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeMarkdown(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
