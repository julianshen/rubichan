package wiki

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// DiagramConfig controls diagram generation behavior.
type DiagramConfig struct {
	Format string // diagram format (only "mermaid" supported)
}

// DefaultDiagramConfig returns sensible defaults for diagram generation.
func DefaultDiagramConfig() DiagramConfig {
	return DiagramConfig{
		Format: "mermaid",
	}
}

// GenerateDiagrams produces Mermaid diagrams from scanned files and analysis results.
// It generates up to 3 programmatic diagrams and 1 LLM-generated sequence diagram.
func GenerateDiagrams(ctx context.Context, files []ScannedFile, analysis *AnalysisResult, llm LLMCompleter, cfg DiagramConfig) ([]Diagram, error) {
	if cfg.Format != "mermaid" {
		return nil, fmt.Errorf("unsupported diagram format: %s", cfg.Format)
	}

	var diagrams []Diagram

	// 1. Architecture overview (programmatic).
	diagrams = append(diagrams, generateArchitectureDiagram(analysis.Modules))

	// 2. Dependency graph (programmatic).
	diagrams = append(diagrams, generateDependencyDiagram(files, analysis.Modules))

	// 3. Data flow (programmatic) — only if 2+ modules.
	if len(analysis.Modules) >= 2 {
		diagrams = append(diagrams, generateDataFlowDiagram(analysis.Modules))
	}

	// 4. Sequence diagram (LLM-generated) — only if llm is non-nil and architecture is non-empty.
	if llm != nil && analysis.Architecture != "" {
		seq, err := generateSequenceDiagram(ctx, analysis.Architecture, llm)
		if err != nil {
			log.Printf("WARNING: sequence diagram generation failed: %v", err)
		} else {
			diagrams = append(diagrams, seq)
		}
	}

	return diagrams, nil
}

// generateArchitectureDiagram creates a graph TD showing modules with their summaries.
func generateArchitectureDiagram(modules []ModuleAnalysis) Diagram {
	var b strings.Builder
	b.WriteString("graph TD\n")

	for _, m := range modules {
		id := sanitizeID(m.Module)
		summary := truncateUTF8(m.Summary, 40)
		fmt.Fprintf(&b, "    %s[\"%s\\n%s\"]\n", id, escapeMermaid(m.Module), escapeMermaid(summary))
	}

	return Diagram{
		Title:   "Architecture Overview",
		Type:    "architecture",
		Content: b.String(),
	}
}

// generateDependencyDiagram creates a graph LR showing import relationships between known modules.
func generateDependencyDiagram(files []ScannedFile, modules []ModuleAnalysis) Diagram {
	// Build a set of known module names for matching.
	knownModules := make(map[string]bool, len(modules))
	for _, m := range modules {
		knownModules[m.Module] = true
	}

	var b strings.Builder
	b.WriteString("graph LR\n")

	// For each file, check if any import path contains a known module name.
	seen := make(map[string]bool)
	for _, f := range files {
		if !knownModules[f.Module] {
			continue
		}
		for _, imp := range f.Imports {
			for mod := range knownModules {
				if mod == f.Module {
					continue
				}
				if strings.Contains(imp, mod) {
					edge := f.Module + " -> " + mod
					if !seen[edge] {
						seen[edge] = true
						fromID := sanitizeID(f.Module)
						toID := sanitizeID(mod)
						fmt.Fprintf(&b, "    %s --> %s\n", fromID, toID)
					}
				}
			}
		}
	}

	return Diagram{
		Title:   "Module Dependencies",
		Type:    "dependency",
		Content: b.String(),
	}
}

// generateDataFlowDiagram creates a flowchart LR showing modules connected sequentially.
func generateDataFlowDiagram(modules []ModuleAnalysis) Diagram {
	var b strings.Builder
	b.WriteString("flowchart LR\n")

	for i, m := range modules {
		id := sanitizeID(m.Module)
		fmt.Fprintf(&b, "    %s[\"%s\"]\n", id, escapeMermaid(m.Module))
		if i > 0 {
			prevID := sanitizeID(modules[i-1].Module)
			fmt.Fprintf(&b, "    %s --> %s\n", prevID, id)
		}
	}

	return Diagram{
		Title:   "Data Flow",
		Type:    "data-flow",
		Content: b.String(),
	}
}

// generateSequenceDiagram uses the LLM to generate a Mermaid sequence diagram.
func generateSequenceDiagram(ctx context.Context, architecture string, llm LLMCompleter) (Diagram, error) {
	prompt := fmt.Sprintf(`Given the following architecture overview, generate a Mermaid sequenceDiagram showing the key interactions between components.

Architecture:
%s

Respond with ONLY the Mermaid diagram code starting with "sequenceDiagram".`, architecture)

	response, err := llm.Complete(ctx, prompt)
	if err != nil {
		return Diagram{}, fmt.Errorf("LLM completion: %w", err)
	}

	return Diagram{
		Title:   "Key Sequences",
		Type:    "sequence",
		Content: strings.TrimSpace(response),
	}, nil
}

// truncateUTF8 truncates s to at most maxRunes Unicode code points,
// avoiding corruption of multi-byte characters.
func truncateUTF8(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return s
}

// escapeMermaid replaces characters that would break Mermaid label syntax.
func escapeMermaid(s string) string {
	s = strings.ReplaceAll(s, "\"", "#quot;")
	return s
}

// sanitizeID converts a string into a safe Mermaid node identifier.
func sanitizeID(s string) string {
	r := strings.NewReplacer("/", "_", ".", "_", "-", "_", " ", "_")
	return r.Replace(s)
}
