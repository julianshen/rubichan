// Package analyzer provides LLM-powered security analyzers that run in the
// second phase of the two-phase security engine on prioritized code segments.
package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/security"
)

// analyzedFinding is the expected JSON structure from the LLM response.
type analyzedFinding struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Location    struct {
		File      string `json:"file"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		Function  string `json:"function"`
	} `json:"location"`
	CWE         string `json:"cwe"`
	Confidence  string `json:"confidence"`
	Remediation string `json:"remediation"`
}

// baseAnalyzer provides shared LLM analysis logic for all security analyzers.
// Each concrete analyzer differs only in name, category, and system prompt.
type baseAnalyzer struct {
	name         string
	category     security.Category
	systemPrompt string
	provider     provider.LLMProvider
}

// Name returns the analyzer name.
func (b *baseAnalyzer) Name() string {
	return b.name
}

// Category returns the security category this analyzer focuses on.
func (b *baseAnalyzer) Category() security.Category {
	return b.category
}

// Analyze sends code chunks to the LLM and parses the response into findings.
func (b *baseAnalyzer) Analyze(ctx context.Context, chunks []security.AnalysisChunk) ([]security.Finding, error) {
	if len(chunks) == 0 {
		return nil, nil
	}

	userContent := buildUserMessage(chunks)

	req := provider.CompletionRequest{
		System: b.systemPrompt,
		Messages: []provider.Message{
			provider.NewUserMessage(userContent),
		},
		MaxTokens: 4096,
	}

	ch, err := b.provider.Stream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s analyzer: stream failed: %w", b.name, err)
	}

	response := collectStreamResponse(ch)

	parsed, parseErr := parseFindings(response)
	if parseErr != nil {
		return []security.Finding{
			{
				ID:          fmt.Sprintf("%s-unparsed-001", b.name),
				Scanner:     b.name,
				Severity:    security.SeverityInfo,
				Category:    b.category,
				Title:       "Unparseable LLM response",
				Description: "The LLM response could not be parsed as JSON findings.",
				Evidence:    response,
				Confidence:  security.ConfidenceLow,
			},
		}, nil
	}

	return b.mapFindings(parsed), nil
}

// buildUserMessage formats all chunks into a single user message for the LLM.
func buildUserMessage(chunks []security.AnalysisChunk) string {
	var sb strings.Builder
	sb.WriteString("Analyze the following code segments for security issues. Return findings as a JSON array.\n\n")
	for _, c := range chunks {
		fmt.Fprintf(&sb, "// File: %s:%d-%d\n%s\n\n", c.File, c.StartLine, c.EndLine, c.Content)
	}
	return sb.String()
}

// collectStreamResponse reads all text_delta events from a stream channel
// and concatenates them into a single response string.
func collectStreamResponse(ch <-chan provider.StreamEvent) string {
	var sb strings.Builder
	for event := range ch {
		if event.Type == "text_delta" {
			sb.WriteString(event.Text)
		}
	}
	return sb.String()
}

// parseFindings attempts to parse the LLM response as a JSON array of findings.
// It handles responses that may have markdown code fences around the JSON.
func parseFindings(response string) ([]analyzedFinding, error) {
	trimmed := strings.TrimSpace(response)

	// Strip markdown code fences if present.
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 2 {
			// Remove first line (```json) and last line (```)
			start := 1
			end := len(lines)
			if strings.TrimSpace(lines[end-1]) == "```" {
				end--
			}
			trimmed = strings.Join(lines[start:end], "\n")
		}
	}

	var findings []analyzedFinding
	if err := json.Unmarshal([]byte(trimmed), &findings); err != nil {
		return nil, fmt.Errorf("parse findings JSON: %w", err)
	}
	return findings, nil
}

// mapFindings converts parsed LLM findings to security.Finding types.
func (b *baseAnalyzer) mapFindings(parsed []analyzedFinding) []security.Finding {
	findings := make([]security.Finding, 0, len(parsed))
	for _, p := range parsed {
		f := security.Finding{
			ID:          p.ID,
			Scanner:     b.name,
			Severity:    mapSeverity(p.Severity),
			Category:    b.category,
			Title:       p.Title,
			Description: p.Description,
			Location: security.Location{
				File:      p.Location.File,
				StartLine: p.Location.StartLine,
				EndLine:   p.Location.EndLine,
				Function:  p.Location.Function,
			},
			CWE:         p.CWE,
			Confidence:  mapConfidence(p.Confidence),
			Remediation: p.Remediation,
		}
		findings = append(findings, f)
	}
	return findings
}

// mapSeverity converts a string severity from the LLM to the typed Severity.
func mapSeverity(s string) security.Severity {
	switch strings.ToLower(s) {
	case "critical":
		return security.SeverityCritical
	case "high":
		return security.SeverityHigh
	case "medium":
		return security.SeverityMedium
	case "low":
		return security.SeverityLow
	case "info":
		return security.SeverityInfo
	default:
		return security.SeverityInfo
	}
}

// mapConfidence converts a string confidence from the LLM to the typed Confidence.
func mapConfidence(c string) security.Confidence {
	switch strings.ToLower(c) {
	case "high":
		return security.ConfidenceHigh
	case "medium":
		return security.ConfidenceMedium
	case "low":
		return security.ConfidenceLow
	default:
		return security.ConfidenceLow
	}
}
