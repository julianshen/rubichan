package wiki

import (
	"fmt"
	"strings"
)

// Assemble combines analysis results, diagrams, and skill sections into a set
// of wiki documents. It always produces at least the _index.md page.
func Assemble(analysis *AnalysisResult, diagrams []Diagram, skillSections []SkillWikiSection) ([]Document, error) {
	var docs []Document

	docs = append(docs, buildIndexPage(analysis))
	docs = append(docs, buildArchitecturePages(analysis, diagrams)...)
	docs = append(docs, buildModulePages(analysis)...)
	docs = append(docs, buildCodeStructurePage(analysis))
	docs = append(docs, buildSecurityPage())

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

// buildSecurityPage creates security/overview.md with placeholder text.
func buildSecurityPage() Document {
	return Document{
		Path:    "security/overview.md",
		Title:   "Security",
		Content: "# Security\n\nSecurity analysis pending...\n",
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

// titleSlug converts a title to a URL-friendly slug: lowercased with spaces replaced by hyphens.
func titleSlug(title string) string {
	return strings.ReplaceAll(strings.ToLower(title), " ", "-")
}
