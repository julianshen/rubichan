package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestColorizeContent_DiffDetected(t *testing.T) {
	content := "@@ -1,3 +1,4 @@\n-old line\n+new line\n context"
	result := ColorizeContent(content, "file")
	// Content should be preserved (colorization may be no-op in test env).
	assert.Contains(t, result, "new line")
	assert.Contains(t, result, "old line")
}

func TestColorizeContent_JSONDetected(t *testing.T) {
	content := `{"name": "rubichan", "version": 1, "active": true}`
	result := ColorizeContent(content, "file")
	// Chroma adds ANSI codes even in test environments.
	assert.Contains(t, result, "rubichan")
}

func TestColorizeContent_XMLDetected(t *testing.T) {
	content := `<project><name>rubichan</name><version>1.0</version></project>`
	result := ColorizeContent(content, "file")
	assert.Contains(t, result, "rubichan")
}

func TestColorizeContent_MarkdownDetected(t *testing.T) {
	content := "# Heading\n\nSome text.\n\n```go\nfunc main() {}\n```"
	result := ColorizeContent(content, "file")
	assert.Contains(t, result, "Heading")
	assert.Contains(t, result, "func main")
}

func TestColorizeContent_PlainTextPassthrough(t *testing.T) {
	content := "just some plain output with no special format"
	result := ColorizeContent(content, "shell")
	assert.Equal(t, content, result)
}

func TestColorizeContent_Empty(t *testing.T) {
	assert.Equal(t, "", ColorizeContent("", "shell"))
}

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		toolName string
		want     string
	}{
		{"json object", `{"key": "value"}`, "file", "json"},
		{"json array", `[1, 2, 3]`, "file", "json"},
		{"xml", `<root><child>text</child></root>`, "file", "xml"},
		{"markdown heading", "# Title\n\nParagraph", "file", "markdown"},
		{"markdown code fence", "```go\nfunc main() {}\n```", "file", "markdown"},
		{"plain text", "just text", "shell", ""},
		{"empty", "", "shell", ""},
		{"short json-like", "{}", "file", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, detectContentType(tt.content, tt.toolName))
		})
	}
}

func TestColorizeWithChroma_JSON(t *testing.T) {
	content := `{"key": "value", "num": 42}`
	result := colorizeWithChroma(content, "json")
	// Chroma should produce different output (ANSI codes).
	assert.Contains(t, result, "key")
	assert.Contains(t, result, "value")
}

func TestColorizeWithChroma_UnknownLanguage(t *testing.T) {
	content := "some content"
	result := colorizeWithChroma(content, "nonexistent-language")
	// Unknown language should return content unchanged.
	assert.Equal(t, content, result)
}
