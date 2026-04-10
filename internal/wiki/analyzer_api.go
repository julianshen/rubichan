package wiki

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
)

// APIAnalyzer implements SpecializedAnalyzer to produce API surface documentation.
type APIAnalyzer struct {
	llm LLMCompleter
}

// NewAPIAnalyzer creates an APIAnalyzer backed by the given LLM.
func NewAPIAnalyzer(llm LLMCompleter) *APIAnalyzer {
	return &APIAnalyzer{llm: llm}
}

// Name returns the analyzer's identifier.
func (a *APIAnalyzer) Name() string { return "api" }

// apiGroupConfig maps API pattern kinds to their output file and display title.
type apiGroupConfig struct {
	kind  string
	path  string
	title string
}

var apiGroups = []apiGroupConfig{
	{kind: "http", path: "api/http-endpoints.md", title: "HTTP Endpoints"},
	{kind: "grpc", path: "api/grpc-services.md", title: "gRPC Services"},
	{kind: "cli", path: "api/cli-commands.md", title: "CLI Commands"},
	{kind: "graphql", path: "api/graphql-schema.md", title: "GraphQL Schema"},
	{kind: "export", path: "api/public-interfaces.md", title: "Public Interfaces"},
}

var apiPromptTmpl = template.Must(template.New("api").Parse(
	`Generate structured markdown documentation for the following {{.Title}} found in the codebase.

Patterns:
{{range .Patterns}}  - File: {{.File}}:{{.Line}} | Kind: {{.Kind}}{{if .Method}} | Method: {{.Method}}{{end}} | Path: {{.Path}} | Handler: {{.Handler}}
{{end}}
Produce clear, well-organized markdown documentation. Include a brief description of each endpoint/service/command.`))

// Analyze generates API surface documentation from the detected patterns.
// Always produces api/_index.md when patterns exist. Only generates per-kind
// docs for groups that have at least one pattern. LLM errors are non-fatal.
func (a *APIAnalyzer) Analyze(ctx context.Context, input AnalyzerInput) (*AnalyzerOutput, error) {
	if len(input.APIPatterns) == 0 {
		return &AnalyzerOutput{}, nil
	}

	// Group patterns by kind.
	byKind := make(map[string][]APIPattern)
	for _, p := range input.APIPatterns {
		byKind[p.Kind] = append(byKind[p.Kind], p)
	}

	var docs []Document

	// For each known group with patterns, ask the LLM for structured docs.
	// Track which kinds succeeded so the overview only links to them.
	succeededKinds := make(map[string]bool)
	for _, grp := range apiGroups {
		patterns, ok := byKind[grp.kind]
		if !ok || len(patterns) == 0 {
			continue
		}

		doc, err := a.generateKindDoc(ctx, grp, patterns)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// Non-fatal: skip this kind on LLM error.
			continue
		}
		succeededKinds[grp.kind] = true
		docs = append(docs, doc)
	}

	// Build the overview index AFTER generating per-kind docs, only linking
	// to kinds that actually produced output.
	indexDoc := buildAPIIndexDoc(byKind, succeededKinds)
	docs = append([]Document{indexDoc}, docs...)

	return &AnalyzerOutput{Documents: docs}, nil
}

// generateKindDoc calls the LLM to produce documentation for one API kind group.
func (a *APIAnalyzer) generateKindDoc(ctx context.Context, grp apiGroupConfig, patterns []APIPattern) (Document, error) {
	var buf bytes.Buffer
	err := apiPromptTmpl.Execute(&buf, struct {
		Title    string
		Patterns []APIPattern
	}{
		Title:    grp.title,
		Patterns: patterns,
	})
	if err != nil {
		return Document{}, fmt.Errorf("rendering api prompt: %w", err)
	}

	resp, err := completeLLMResponse(ctx, buf.String(), a.llm, 1)
	if err != nil {
		return Document{}, fmt.Errorf("LLM completion for %s: %w", grp.kind, err)
	}

	resp = strings.TrimSpace(resp)
	if resp == "" {
		return Document{}, fmt.Errorf("empty LLM response for %s", grp.kind)
	}

	return Document{
		Path:    grp.path,
		Title:   grp.title,
		Content: fmt.Sprintf("# %s\n\n%s\n", grp.title, resp),
	}, nil
}

// buildAPIIndexDoc produces the api/_index.md overview document.
// succeededKinds indicates which kinds actually produced per-kind docs;
// only those get hyperlinks in the overview.
func buildAPIIndexDoc(byKind map[string][]APIPattern, succeededKinds map[string]bool) Document {
	var sb strings.Builder
	sb.WriteString("# API Overview\n\n")
	sb.WriteString("The following API surfaces were detected in this codebase:\n\n")

	for _, grp := range apiGroups {
		patterns, ok := byKind[grp.kind]
		if !ok || len(patterns) == 0 {
			continue
		}
		if succeededKinds[grp.kind] {
			fmt.Fprintf(&sb, "- **%s** — %d pattern(s) detected → [%s](%s)\n",
				grp.title, len(patterns), grp.title, grp.path)
		} else {
			fmt.Fprintf(&sb, "- **%s** — %d pattern(s) detected\n",
				grp.title, len(patterns))
		}
	}

	// Include any unrecognized kinds not in the standard list.
	knownKinds := make(map[string]bool)
	for _, grp := range apiGroups {
		knownKinds[grp.kind] = true
	}
	for kind, patterns := range byKind {
		if !knownKinds[kind] {
			fmt.Fprintf(&sb, "- **%s** — %d pattern(s) detected\n", kind, len(patterns))
		}
	}

	return Document{
		Path:    "api/_index.md",
		Title:   "API Overview",
		Content: sb.String(),
	}
}
