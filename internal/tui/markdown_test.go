package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderMarkdown(t *testing.T) {
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	result, err := r.Render("Hello **world**")
	require.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "world")
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	md := "```go\nfmt.Println(\"hello\")\n```"
	result, err := r.Render(md)
	require.NoError(t, err)
	assert.Contains(t, result, "Println")
}

func TestRenderMarkdownBoldStripsMarkers(t *testing.T) {
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	result, err := r.Render("Hello **world**")
	require.NoError(t, err)
	// Glamour should strip the ** markdown markers and apply ANSI styling
	assert.NotContains(t, result, "**world**")
	assert.Contains(t, result, "world")
}

func TestRenderMarkdownEmpty(t *testing.T) {
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	result, err := r.Render("")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestRenderMarkdownNilRenderer(t *testing.T) {
	// A nil renderer should fall back to returning raw markdown.
	r := &MarkdownRenderer{}
	result, err := r.Render("Hello **world**")
	require.NoError(t, err)
	assert.Equal(t, "Hello **world**", result)
}

func TestIsMarkdownBreakpointDoubleNewline(t *testing.T) {
	assert.True(t, IsMarkdownBreakpoint("some text\n\n"))
}

func TestIsMarkdownBreakpointCodeFenceClosing(t *testing.T) {
	assert.True(t, IsMarkdownBreakpoint("fmt.Println()\n```\n"))
}

func TestIsMarkdownBreakpointHeading(t *testing.T) {
	assert.True(t, IsMarkdownBreakpoint("some text\n## Section\n"))
}

func TestIsMarkdownBreakpointH1(t *testing.T) {
	assert.True(t, IsMarkdownBreakpoint("intro\n# Title\n"))
}

func TestIsMarkdownBreakpointSingleNewline(t *testing.T) {
	assert.False(t, IsMarkdownBreakpoint("some text\n"))
}

func TestIsMarkdownBreakpointMidWord(t *testing.T) {
	assert.False(t, IsMarkdownBreakpoint("some text"))
}

func TestIsMarkdownBreakpointEmpty(t *testing.T) {
	assert.False(t, IsMarkdownBreakpoint(""))
}

func TestIsMarkdownBreakpointCodeFenceOpening(t *testing.T) {
	// Opening fence is NOT a breakpoint — only closing fences are.
	assert.False(t, IsMarkdownBreakpoint("text\n```go\n"))
}

func TestSanitizeAssistantOutputRemovesProtocolSections(t *testing.T) {
	raw := "assistantanalysisWe need to inspect files." +
		"assistantcommentary to=functions.shell json{\"command\":\"ls -R\"}" +
		"assistantfinalHere is the readable answer."

	result := SanitizeAssistantOutput(raw)

	assert.Equal(t, "Here is the readable answer.", result)
}

func TestSanitizeAssistantOutputHandlesLeadingAnalysisToken(t *testing.T) {
	raw := "analysisNeed to think first.assistantfinalReadable answer."

	result := SanitizeAssistantOutput(raw)

	assert.Equal(t, "Readable answer.", result)
}

func TestSanitizeAssistantOutputHandlesBareFinalMidString(t *testing.T) {
	raw := "analysisThinking...finalReadable answer."

	result := SanitizeAssistantOutput(raw)

	assert.Equal(t, "Readable answer.", result)
}

func TestSanitizeAssistantOutputPreservesVisibleCommandSentence(t *testing.T) {
	raw := "assistantfinalYou can run the command.assistantanalysishidden"

	result := SanitizeAssistantOutput(raw)

	assert.Equal(t, "You can run the command.", result)
}
