package tui

import (
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

var (
	styleMDHeading = lipgloss.NewStyle().Bold(true).Foreground(colorPrimaryLight)
	styleMDCode    = lipgloss.NewStyle().Foreground(colorSuccess)
	styleMDBold    = lipgloss.NewStyle().Bold(true)
)

// Cached Chroma resources — looked up once, reused across renders.
var (
	chromaStyle      *chroma.Style
	chromaFormatter  chroma.Formatter
	chromaLexerMu    sync.Mutex
	chromaLexerCache = map[string]chroma.Lexer{}
)

func init() {
	chromaStyle = styles.Get("monokai")
	if chromaStyle == nil {
		chromaStyle = styles.Fallback
	}
	chromaFormatter = formatters.Get("terminal256")
}

// ColorizeContent applies syntax-aware colorization to tool result content.
// It detects the content type and applies appropriate styling:
//   - Diffs: green/red/cyan for +/-/@@ lines
//   - JSON: keys in blue, strings in green, numbers in yellow
//   - XML/HTML: tags in blue, attributes in yellow
//   - Markdown: headings bold, code in green
//   - Code: Chroma syntax highlighting via terminal256 formatter
//
// Falls back to plain text when content type cannot be determined.
func ColorizeContent(content string, toolName string) string {
	if content == "" {
		return content
	}

	// Diffs get priority — most common and well-tested.
	if isDiffContent(content) {
		return colorizeDiffContent(content)
	}

	// Detect content type from structure.
	contentType := detectContentType(content, toolName)
	switch contentType {
	case "json":
		return colorizeJSON(content)
	case "xml":
		return colorizeXML(content)
	case "markdown":
		return colorizeMarkdown(content)
	default:
		return content
	}
}

// detectContentType guesses the format of tool result content.
func detectContentType(content string, toolName string) string {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) == 0 {
		return ""
	}

	// JSON: starts with { or [
	if (trimmed[0] == '{' || trimmed[0] == '[') && len(trimmed) > 2 {
		return "json"
	}

	// XML/HTML: starts with < and has closing tags.
	if trimmed[0] == '<' && strings.Contains(trimmed, "</") {
		return "xml"
	}

	// Markdown: has heading lines or code fences.
	// Skip markdown detection for shell results — output lines starting
	// with # are comments, not headings.
	if toolName != "shell" && hasMarkdownIndicators(trimmed) {
		return "markdown"
	}

	return ""
}

func hasMarkdownIndicators(s string) bool {
	lines := strings.SplitN(s, "\n", 10)
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "# ") ||
			strings.HasPrefix(stripped, "## ") ||
			strings.HasPrefix(stripped, "### ") ||
			strings.HasPrefix(stripped, "```") ||
			strings.HasPrefix(stripped, "- [") {
			return true
		}
	}
	return false
}

// colorizeJSON applies lightweight JSON syntax coloring.
func colorizeJSON(content string) string {
	return colorizeWithChroma(content, "json")
}

// colorizeXML applies lightweight XML/HTML syntax coloring.
func colorizeXML(content string) string {
	return colorizeWithChroma(content, "xml")
}

// colorizeMarkdown applies lightweight markdown syntax coloring.
func colorizeMarkdown(content string) string {
	lines := strings.Split(content, "\n")
	inCodeBlock := false
	for i, line := range lines {
		stripped := strings.TrimSpace(line)

		// Code fences.
		if strings.HasPrefix(stripped, "```") {
			inCodeBlock = !inCodeBlock
			lines[i] = styleMDCode.Render(line)
			continue
		}
		if inCodeBlock {
			lines[i] = styleMDCode.Render(line)
			continue
		}

		// Headings.
		if strings.HasPrefix(stripped, "#") {
			lines[i] = styleMDHeading.Render(line)
			continue
		}

		// Bold text (simple inline **...**).
		if strings.Contains(line, "**") {
			lines[i] = colorizeBold(line)
		}
	}
	return strings.Join(lines, "\n")
}

func colorizeBold(line string) string {
	var result strings.Builder
	for {
		start := strings.Index(line, "**")
		if start == -1 {
			result.WriteString(line)
			break
		}
		end := strings.Index(line[start+2:], "**")
		if end == -1 {
			result.WriteString(line)
			break
		}
		end += start + 2
		result.WriteString(line[:start])
		result.WriteString(styleMDBold.Render(line[start : end+2]))
		line = line[end+2:]
	}
	return result.String()
}

// colorizeDiffContent applies diff-specific coloring (extracted from ColorizeDiffLines).
func colorizeDiffContent(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "@@ "):
			lines[i] = diffHunkStyle.Render(line)
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			lines[i] = diffAddedStyle.Render(line)
		case strings.HasPrefix(line, "-"):
			lines[i] = diffRemovedStyle.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

// colorizeWithChroma uses the Chroma library for syntax highlighting.
// Lexers are cached per language to avoid repeated lookups.
// Falls back to plain text on any error.
func colorizeWithChroma(content, language string) string {
	if chromaFormatter == nil {
		return content
	}

	lexer := getCachedLexer(language)
	if lexer == nil {
		return content
	}

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return content
	}

	var buf strings.Builder
	if err := chromaFormatter.Format(&buf, chromaStyle, iterator); err != nil {
		return content
	}
	return buf.String()
}

func getCachedLexer(language string) chroma.Lexer {
	chromaLexerMu.Lock()
	defer chromaLexerMu.Unlock()

	if l, ok := chromaLexerCache[language]; ok {
		return l
	}
	l := lexers.Get(language)
	if l == nil {
		chromaLexerCache[language] = nil
		return nil
	}
	l = chroma.Coalesce(l)
	chromaLexerCache[language] = l
	return l
}
