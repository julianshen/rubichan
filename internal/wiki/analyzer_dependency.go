package wiki

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// knownManifests lists dependency manifest filenames to probe in the project directory.
var knownManifests = []string{
	"go.mod",
	"package.json",
	"Cargo.toml",
	"requirements.txt",
	"pyproject.toml",
}

// DependencyAnalyzer implements SpecializedAnalyzer to produce ADR-style design
// decisions inferred from dependency manifests and architecture context.
type DependencyAnalyzer struct {
	llm        LLMCompleter
	projectDir string
}

// NewDependencyAnalyzer creates a DependencyAnalyzer for the given project directory.
func NewDependencyAnalyzer(llm LLMCompleter, projectDir string) *DependencyAnalyzer {
	return &DependencyAnalyzer{llm: llm, projectDir: projectDir}
}

// Name returns the analyzer's identifier.
func (a *DependencyAnalyzer) Name() string { return "dependencies" }

// Analyze scans for dependency manifests, reads their contents, and asks the LLM
// to infer architectural decisions. Returns empty output when no manifests exist
// or when the LLM call fails (non-fatal). Context cancellation is propagated.
func (a *DependencyAnalyzer) Analyze(ctx context.Context, input AnalyzerInput) (*AnalyzerOutput, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	manifests := a.readManifests()
	if len(manifests) == 0 {
		return &AnalyzerOutput{}, nil
	}

	summaries := buildSummariesText(input.ModuleAnalyses)

	var buf bytes.Buffer
	err := dependencyTmpl.Execute(&buf, struct {
		Architecture string
		Summaries    string
		Manifests    []manifestEntry
	}{
		Architecture: input.Architecture,
		Summaries:    summaries,
		Manifests:    manifests,
	})
	if err != nil {
		return &AnalyzerOutput{}, nil // non-fatal
	}

	resp, err := a.llm.Complete(ctx, buf.String())
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return &AnalyzerOutput{}, nil // non-fatal for LLM errors
	}

	resp = strings.TrimSpace(resp)
	if resp == "" {
		return &AnalyzerOutput{}, nil
	}

	doc := Document{
		Path:    "architecture/design-decisions.md",
		Title:   "Design Decisions",
		Content: fmt.Sprintf("# Design Decisions\n\n%s\n", resp),
	}

	return &AnalyzerOutput{Documents: []Document{doc}}, nil
}

// manifestEntry holds a dependency filename and its raw content.
type manifestEntry struct {
	Name    string
	Content string
}

// readManifests probes for known manifest files and returns those that exist.
func (a *DependencyAnalyzer) readManifests() []manifestEntry {
	var entries []manifestEntry
	for _, name := range knownManifests {
		path := filepath.Join(a.projectDir, name)
		if _, err := os.Stat(path); err != nil {
			continue // file does not exist
		}
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("wiki: warning reading manifest %s: %v", name, err)
			continue
		}
		entries = append(entries, manifestEntry{Name: name, Content: string(data)})
	}
	return entries
}

var dependencyTmpl = template.Must(template.New("dependency").Parse(
	`You are analyzing a software project. Based on the architecture description, module summaries, and dependency manifest files below, produce ADR-style (Architecture Decision Record) design decisions.

Architecture:
{{.Architecture}}

Module Summaries:
{{.Summaries}}

Dependency Files:
{{range .Manifests}}
### {{.Name}}
` + "```" + `
{{.Content}}
` + "```" + `
{{end}}

For each inferred architectural decision, produce a section in this format:
## ADR-NNN: <short title>
Decision: <one-paragraph explanation>

Include:
- External dependency inventory with versions where available
- Dependency risk notes (unmaintained packages, security concerns)
- Inferred architectural decisions based on the dependencies chosen`))
