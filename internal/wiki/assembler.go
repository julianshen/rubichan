package wiki

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/julianshen/rubichan/internal/security"
)

// Assemble combines analysis results, diagrams, skill sections, and security
// findings into a set of wiki documents. It always produces at least the
// _index.md page.
func Assemble(analysis *AnalysisResult, diagrams []Diagram, skillSections []SkillWikiSection, findings []security.Finding) ([]Document, error) {
	var docs []Document

	docs = append(docs, buildIndexPage(analysis))
	docs = append(docs, buildArchitecturePages(analysis, diagrams)...)
	docs = append(docs, buildModulePages(analysis)...)
	docs = append(docs, buildCodeStructurePage(analysis))
	docs = append(docs, buildSecurityPage(findings))

	if len(analysis.Suggestions) > 0 {
		docs = append(docs, buildSuggestionsPage(analysis.Suggestions))
	}

	docs = append(docs, buildSkillPages(skillSections)...)

	return docs, nil
}

// buildIndexPage creates _index.md with architecture text and a bullet list of modules.
func buildIndexPage(analysis *AnalysisResult) Document {
	var b strings.Builder
	b.WriteString("# Project Overview\n\n")

	if analysis.Architecture != "" {
		b.WriteString("## Architecture\n\n")
		b.WriteString(analysis.Architecture)
		b.WriteString("\n\n")
	}

	if len(analysis.Modules) > 0 {
		b.WriteString("## Modules\n\n")
		for _, m := range analysis.Modules {
			fmt.Fprintf(&b, "- **%s**: %s\n", m.Module, m.Summary)
		}
		b.WriteString("\n")
	}

	return Document{
		Path:    "_index.md",
		Title:   "Project Overview",
		Content: b.String(),
	}
}

// buildArchitecturePages creates architecture/overview.md, architecture/dependencies.md,
// and architecture/data-flow.md, embedding relevant diagrams.
func buildArchitecturePages(analysis *AnalysisResult, diagrams []Diagram) []Document {
	var docs []Document

	// Group diagrams by type for easy lookup.
	diagramsByType := make(map[string][]Diagram)
	for _, d := range diagrams {
		diagramsByType[d.Type] = append(diagramsByType[d.Type], d)
	}

	// architecture/overview.md — architecture diagrams + description
	{
		var b strings.Builder
		b.WriteString("# Architecture Overview\n\n")
		if analysis.Architecture != "" {
			b.WriteString(analysis.Architecture)
			b.WriteString("\n\n")
		}
		for _, d := range diagramsByType["architecture"] {
			writeMermaidBlock(&b, d)
		}
		docs = append(docs, Document{
			Path:    "architecture/overview.md",
			Title:   "Architecture Overview",
			Content: b.String(),
		})
	}

	// architecture/dependencies.md — dependency diagrams
	{
		var b strings.Builder
		b.WriteString("# Dependencies\n\n")
		for _, d := range diagramsByType["dependency"] {
			writeMermaidBlock(&b, d)
		}
		docs = append(docs, Document{
			Path:    "architecture/dependencies.md",
			Title:   "Dependencies",
			Content: b.String(),
		})
	}

	// architecture/data-flow.md — data-flow + sequence diagrams
	{
		var b strings.Builder
		b.WriteString("# Data Flow\n\n")
		for _, d := range diagramsByType["data-flow"] {
			writeMermaidBlock(&b, d)
		}
		for _, d := range diagramsByType["sequence"] {
			writeMermaidBlock(&b, d)
		}
		docs = append(docs, Document{
			Path:    "architecture/data-flow.md",
			Title:   "Data Flow",
			Content: b.String(),
		})
	}

	return docs
}

// buildModulePages creates modules/_index.md and one page per module.
func buildModulePages(analysis *AnalysisResult) []Document {
	if len(analysis.Modules) == 0 {
		return nil
	}

	var docs []Document

	// modules/_index.md — listing with links
	{
		var b strings.Builder
		b.WriteString("# Modules\n\n")
		for _, m := range analysis.Modules {
			slug := sanitizeID(m.Module)
			fmt.Fprintf(&b, "- [%s](%s.md): %s\n", m.Module, slug, m.Summary)
		}
		b.WriteString("\n")
		docs = append(docs, Document{
			Path:    "modules/_index.md",
			Title:   "Modules",
			Content: b.String(),
		})
	}

	// One page per module
	for _, m := range analysis.Modules {
		slug := sanitizeID(m.Module)
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n", m.Module)

		if m.Summary != "" {
			b.WriteString("## Summary\n\n")
			b.WriteString(m.Summary)
			b.WriteString("\n\n")
		}
		if m.KeyTypes != "" {
			b.WriteString("## Key Types\n\n")
			b.WriteString(m.KeyTypes)
			b.WriteString("\n\n")
		}
		if m.Patterns != "" {
			b.WriteString("## Patterns\n\n")
			b.WriteString(m.Patterns)
			b.WriteString("\n\n")
		}
		if m.Concerns != "" {
			b.WriteString("## Concerns\n\n")
			b.WriteString(m.Concerns)
			b.WriteString("\n\n")
		}

		docs = append(docs, Document{
			Path:    "modules/" + slug + ".md",
			Title:   m.Module,
			Content: b.String(),
		})
	}

	return docs
}

// buildCodeStructurePage creates code-structure/overview.md with key abstractions.
func buildCodeStructurePage(analysis *AnalysisResult) Document {
	var b strings.Builder
	b.WriteString("# Code Structure\n\n")
	if analysis.KeyAbstractions != "" {
		b.WriteString("## Key Abstractions\n\n")
		b.WriteString(analysis.KeyAbstractions)
		b.WriteString("\n\n")
	}

	return Document{
		Path:    "code-structure/overview.md",
		Title:   "Code Structure",
		Content: b.String(),
	}
}

// buildSecurityPage creates security/overview.md. When findings are provided,
// it renders a severity summary table and per-finding details. Otherwise it
// shows placeholder text.
func buildSecurityPage(findings []security.Finding) Document {
	if len(findings) == 0 {
		return Document{
			Path:    "security/overview.md",
			Title:   "Security",
			Content: "# Security\n\nSecurity analysis pending...\n",
		}
	}

	var b strings.Builder
	b.WriteString("# Security\n\n")

	// Severity summary counts.
	counts := map[security.Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}

	b.WriteString("## Summary\n\n")
	b.WriteString("| Severity | Count |\n")
	b.WriteString("|----------|-------|\n")
	for _, sev := range []security.Severity{
		security.SeverityCritical,
		security.SeverityHigh,
		security.SeverityMedium,
		security.SeverityLow,
		security.SeverityInfo,
	} {
		if c, ok := counts[sev]; ok && c > 0 {
			fmt.Fprintf(&b, "| %s | %d |\n", sev, c)
		}
	}
	fmt.Fprintf(&b, "| **Total** | **%d** |\n\n", len(findings))

	// Per-finding details.
	b.WriteString("## Findings\n\n")
	for _, f := range findings {
		fmt.Fprintf(&b, "### %s\n\n", sanitizeMarkdown(f.Title))
		fmt.Fprintf(&b, "- **Severity**: %s\n", sanitizeMarkdown(string(f.Severity)))
		fmt.Fprintf(&b, "- **Scanner**: %s\n", sanitizeMarkdown(f.Scanner))
		if f.Location.File != "" {
			if f.Location.StartLine > 0 {
				fmt.Fprintf(&b, "- **Location**: `%s:%d`\n", sanitizeMarkdown(f.Location.File), f.Location.StartLine)
			} else {
				fmt.Fprintf(&b, "- **Location**: `%s`\n", sanitizeMarkdown(f.Location.File))
			}
		}
		if f.Description != "" {
			fmt.Fprintf(&b, "\n%s\n", sanitizeMarkdown(f.Description))
		}
		b.WriteString("\n")
	}

	return Document{
		Path:    "security/overview.md",
		Title:   "Security",
		Content: b.String(),
	}
}

// buildSuggestionsPage creates suggestions/improvements.md with bullet points.
func buildSuggestionsPage(suggestions []string) Document {
	var b strings.Builder
	b.WriteString("# Suggestions for Improvement\n\n")
	for _, s := range suggestions {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	b.WriteString("\n")

	return Document{
		Path:    "suggestions/improvements.md",
		Title:   "Suggestions",
		Content: b.String(),
	}
}

// buildSkillPages creates skill-contributed/<title-slug>.md for each skill section.
func buildSkillPages(sections []SkillWikiSection) []Document {
	var docs []Document
	for _, s := range sections {
		slug := titleSlug(s.Title)
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n", s.Title)
		b.WriteString(s.Content)
		b.WriteString("\n\n")

		for _, d := range s.Diagrams {
			writeMermaidBlock(&b, d)
		}

		docs = append(docs, Document{
			Path:    "skill-contributed/" + slug + ".md",
			Title:   s.Title,
			Content: b.String(),
		})
	}
	return docs
}

// writeMermaidBlock appends a fenced mermaid code block for a diagram.
func writeMermaidBlock(b *strings.Builder, d Diagram) {
	if d.Title != "" {
		fmt.Fprintf(b, "### %s\n\n", d.Title)
	}
	b.WriteString("```mermaid\n")
	b.WriteString(d.Content)
	if !strings.HasSuffix(d.Content, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```\n\n")
}

var (
	nonAlphanumRe = regexp.MustCompile(`[^a-z0-9-]`)
	multiHyphenRe = regexp.MustCompile(`-{2,}`)
)

// sanitizeMarkdown escapes HTML-significant characters from untrusted text to
// prevent XSS when the generated Markdown is rendered to HTML.
func sanitizeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// titleSlug converts a title to a URL-friendly slug: lowercased, spaces to hyphens,
// non-alphanumeric characters (except hyphens) stripped.
func titleSlug(title string) string {
	slug := strings.ReplaceAll(strings.ToLower(title), " ", "-")
	slug = nonAlphanumRe.ReplaceAllString(slug, "")
	slug = multiHyphenRe.ReplaceAllString(slug, "-")
	return strings.Trim(slug, "-")
}
