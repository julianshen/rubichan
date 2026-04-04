package wiki

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
)

// SecurityAnalyzer implements SpecializedAnalyzer to produce security-focused
// documentation: authentication & access control, STRIDE threat model, and
// data-flow & compliance analysis.
type SecurityAnalyzer struct {
	llm LLMCompleter
}

// NewSecurityAnalyzer creates a SecurityAnalyzer backed by the given LLM.
func NewSecurityAnalyzer(llm LLMCompleter) *SecurityAnalyzer {
	return &SecurityAnalyzer{llm: llm}
}

// Name returns the analyzer's identifier.
func (a *SecurityAnalyzer) Name() string { return "security" }

// Analyze runs three sequential sub-prompts to produce security documentation.
// Each sub-prompt is independent: if one fails due to an LLM error the others
// still produce output. Context cancellation propagates immediately.
func (a *SecurityAnalyzer) Analyze(ctx context.Context, input AnalyzerInput) (*AnalyzerOutput, error) {
	if input.Architecture == "" {
		return &AnalyzerOutput{}, nil
	}

	summaries := buildSummariesText(input.ModuleAnalyses)

	type subPrompt struct {
		tmpl      *template.Template
		data      interface{}
		docPath   string
		docTitle  string
		diagTitle string
		diagType  string
	}

	prompts := []subPrompt{
		{
			tmpl: securityAuthTmpl,
			data: struct {
				Architecture string
				Summaries    string
			}{Architecture: input.Architecture, Summaries: summaries},
			docPath:   "security/auth-and-access.md",
			docTitle:  "Auth & Access Control",
			diagTitle: "Auth Flow",
			diagType:  "sequence",
		},
		{
			tmpl: securityThreatTmpl,
			data: struct {
				Architecture string
			}{Architecture: input.Architecture},
			docPath:   "security/threat-model.md",
			docTitle:  "Threat Model (STRIDE)",
			diagTitle: "Threat Overview",
			diagType:  "architecture",
		},
		{
			tmpl: securityDataFlowTmpl,
			data: struct {
				Architecture string
				Summaries    string
			}{Architecture: input.Architecture, Summaries: summaries},
			docPath:   "security/data-flow.md",
			docTitle:  "Data Flow & Compliance",
			diagTitle: "Data Flow",
			diagType:  "data-flow",
		},
	}

	out := &AnalyzerOutput{}

	for _, sp := range prompts {
		// Propagate context cancellation immediately.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		doc, diag, err := a.runSubPrompt(ctx, sp.tmpl, sp.data, sp.docPath, sp.docTitle, sp.diagTitle, sp.diagType)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// Non-fatal: skip this sub-prompt and continue.
			continue
		}

		out.Documents = append(out.Documents, *doc)
		if diag != nil {
			out.Diagrams = append(out.Diagrams, *diag)
		}
	}

	return out, nil
}

// runSubPrompt renders a template, calls the LLM, and builds a Document and
// optional Diagram from the response.
func (a *SecurityAnalyzer) runSubPrompt(
	ctx context.Context,
	tmpl *template.Template,
	data interface{},
	docPath, docTitle, diagTitle, diagType string,
) (*Document, *Diagram, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, nil, fmt.Errorf("rendering prompt: %w", err)
	}

	resp, err := a.llm.Complete(ctx, buf.String())
	if err != nil {
		return nil, nil, fmt.Errorf("LLM completion: %w", err)
	}

	resp = strings.TrimSpace(resp)
	if resp == "" {
		return nil, nil, fmt.Errorf("empty LLM response for %s", docPath)
	}

	doc := &Document{
		Path:    docPath,
		Title:   docTitle,
		Content: fmt.Sprintf("# %s\n\n%s\n", docTitle, resp),
	}

	diag := extractMermaidDiagram(resp, diagTitle, diagType)

	return doc, diag, nil
}

// extractMermaidDiagram extracts the first Mermaid fenced block from response.
// Returns nil when no fenced block is present.
func extractMermaidDiagram(response, title, diagramType string) *Diagram {
	start := strings.Index(response, "```mermaid")
	if start == -1 {
		return nil
	}
	end := strings.Index(response[start+10:], "```")
	if end == -1 {
		return nil
	}
	content := strings.TrimSpace(response[start+10 : start+10+end])
	return &Diagram{Title: title, Type: diagramType, Content: content}
}

// ---------- prompt templates ----------

var securityAuthTmpl = template.Must(template.New("security-auth").Parse(
	`Analyze the following software architecture and module summaries for security properties.

Architecture:
{{.Architecture}}

Module Summaries:
{{.Summaries}}

Identify:
- Authentication mechanisms (JWT, OAuth, session tokens, API keys)
- Authorization patterns (RBAC, middleware guards, capability checks)
- Trust boundaries between components

Include a Mermaid sequence diagram showing the authentication flow.`))

var securityThreatTmpl = template.Must(template.New("security-threat").Parse(
	`Perform a STRIDE threat model analysis for the following architecture.

Architecture:
{{.Architecture}}

For each STRIDE category assess the risk:
- Spoofing: Can an attacker impersonate a user or component?
- Tampering: Can data be modified in transit or at rest?
- Repudiation: Are actions auditable and non-repudiable?
- Information Disclosure: Is sensitive data exposed unintentionally?
- Denial of Service: Are there resource-exhaustion attack surfaces?
- Elevation of Privilege: Can a low-privilege actor gain higher access?

Include a Mermaid diagram (attack tree or threat overview).`))

var securityDataFlowTmpl = template.Must(template.New("security-dataflow").Parse(
	`Analyze data flow and compliance properties for the following architecture.

Architecture:
{{.Architecture}}

Module Summaries:
{{.Summaries}}

Describe:
- Sensitive data inventory (PII, credentials, secrets, tokens)
- Encryption at rest and in transit
- Logging hygiene (what is logged, what must not be logged)
- Compliance considerations (GDPR, SOC2, etc. where applicable)

Include a Mermaid data flow diagram showing how sensitive data moves through the system.`))
