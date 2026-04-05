package knowledgegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

// LLMIngestor extracts entities from raw text using an LLM.
// The LLM is prompted to return a YAML-formatted list of entities.
type LLMIngestor struct {
	completer LLMCompleter
}

// NewLLMIngestor creates an ingestor that uses the given LLM for extraction.
func NewLLMIngestor(completer LLMCompleter) *LLMIngestor {
	return &LLMIngestor{completer: completer}
}

// Ingest extracts entities from the given text and stores them in the graph.
// The source parameter indicates how the text was obtained (LLM, git, manual, file).
func (i *LLMIngestor) Ingest(ctx context.Context, g *KnowledgeGraph, text string, source kg.UpdateSource) (int, error) {
	if text == "" {
		return 0, nil
	}

	prompt := i.extractionPrompt(text)
	response, err := i.completer.Complete(ctx, prompt)
	if err != nil {
		return 0, fmt.Errorf("LLMIngestor: LLM error: %w", err)
	}

	// Parse YAML response into entities
	entities, err := i.parseYAMLResponse(response, source)
	if err != nil {
		return 0, fmt.Errorf("LLMIngestor: parse error: %w", err)
	}

	// Store entities in graph
	count := 0
	now := time.Now()
	for _, e := range entities {
		e.Source = source
		if e.Created.IsZero() {
			e.Created = now
		}
		if e.Updated.IsZero() {
			e.Updated = now
		}

		if err := g.Put(ctx, e); err != nil {
			return count, fmt.Errorf("LLMIngestor: store error: %w", err)
		}
		count++
	}

	return count, nil
}

func (i *LLMIngestor) extractionPrompt(text string) string {
	return fmt.Sprintf(`You are a knowledge extraction expert. Analyze the following text and extract key knowledge entities.

For each entity, provide:
- id: unique identifier (kebab-case, e.g., adr-001-go-language)
- kind: one of: architecture, decision, gotcha, pattern, module, integration
- layer: optional scope - "base" (shared), "team" (team-specific), or "session" (ephemeral). Defaults to "base"
- title: short descriptive title
- tags: list of relevant tags
- body: detailed content (2-3 sentences)
- relationships (optional): list of connections to other entities

Return a JSON array of entities with this structure:
[
  {
    "id": "string",
    "kind": "string",
    "layer": "base|team|session",
    "title": "string",
    "tags": ["string"],
    "body": "string",
    "relationships": [
      {
        "kind": "justifies|relates-to|depends-on|supersedes|conflicts-with|implements",
        "target": "entity-id"
      }
    ]
  }
]

Text to analyze:
%s

Extract only entities that are explicitly mentioned or strongly implied. Return empty array if no entities found.`, text)
}

func (i *LLMIngestor) parseYAMLResponse(response string, source kg.UpdateSource) ([]*kg.Entity, error) {
	var parsed []struct {
		ID            string   `json:"id"`
		Kind          string   `json:"kind"`
		Layer         string   `json:"layer"`
		Title         string   `json:"title"`
		Tags          []string `json:"tags"`
		Body          string   `json:"body"`
		Relationships []struct {
			Kind   string `json:"kind"`
			Target string `json:"target"`
		} `json:"relationships"`
	}

	if err := json.Unmarshal([]byte(response), &parsed); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}

	var entities []*kg.Entity
	for _, p := range parsed {
		if p.ID == "" || p.Kind == "" {
			continue
		}

		var rels []kg.Relationship
		for _, r := range p.Relationships {
			if r.Kind != "" && r.Target != "" {
				rels = append(rels, kg.Relationship{
					Kind:   kg.RelationshipKind(r.Kind),
					Target: r.Target,
				})
			}
		}

		entities = append(entities, &kg.Entity{
			ID:            p.ID,
			Kind:          kg.EntityKind(p.Kind),
			Layer:         kg.EntityLayer(p.Layer),
			Title:         p.Title,
			Tags:          p.Tags,
			Body:          p.Body,
			Relationships: rels,
			Source:        source,
			Created:       time.Now(),
			Updated:       time.Now(),
		})
	}

	return entities, nil
}

// GitIngestor extracts entities from git history since a given time.
type GitIngestor struct {
	llmIngestor *LLMIngestor
}

// NewGitIngestor creates an ingestor that analyzes git history.
func NewGitIngestor(completer LLMCompleter) *GitIngestor {
	return &GitIngestor{
		llmIngestor: NewLLMIngestor(completer),
	}
}

// Ingest reads git history since `since` (e.g., "1w" for one week) and extracts entities.
func (i *GitIngestor) Ingest(ctx context.Context, g *KnowledgeGraph, projectRoot string, since string) (int, error) {
	if projectRoot == "" {
		return 0, fmt.Errorf("GitIngestor: projectRoot required")
	}

	// Build git log command
	cmd := exec.CommandContext(ctx, "git", "log", "--since="+since, "--format=%H %s%n%b")
	cmd.Dir = projectRoot

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("GitIngestor: git log failed: %w", err)
	}

	if len(output) == 0 {
		return 0, nil
	}

	// Feed to LLM ingestor
	return i.llmIngestor.Ingest(ctx, g, string(output), kg.SourceGit)
}

// ManualIngestor reads entities from a file with YAML frontmatter.
type ManualIngestor struct{}

// NewManualIngestor creates an ingestor for manual YAML files.
func NewManualIngestor() *ManualIngestor {
	return &ManualIngestor{}
}

// Ingest reads a markdown file and extracts entities from its frontmatter.
func (i *ManualIngestor) Ingest(ctx context.Context, g *KnowledgeGraph, filePath string) (int, error) {
	if filePath == "" {
		return 0, fmt.Errorf("ManualIngestor: filePath required")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("ManualIngestor: read file: %w", err)
	}

	e, err := readEntityFromBytes(content)
	if err != nil {
		return 0, fmt.Errorf("ManualIngestor: parse entity: %w", err)
	}

	if e == nil {
		return 0, nil
	}

	e.Source = kg.SourceManual
	if e.Created.IsZero() {
		e.Created = time.Now()
	}
	if e.Updated.IsZero() {
		e.Updated = time.Now()
	}

	if err := g.Put(ctx, e); err != nil {
		return 0, fmt.Errorf("ManualIngestor: store error: %w", err)
	}

	return 1, nil
}

// FileIngestor extracts entities from file content using an LLM.
type FileIngestor struct {
	llmIngestor *LLMIngestor
}

// NewFileIngestor creates an ingestor that analyzes file content.
func NewFileIngestor(completer LLMCompleter) *FileIngestor {
	return &FileIngestor{
		llmIngestor: NewLLMIngestor(completer),
	}
}

// Ingest reads a file and extracts entities from its content.
func (i *FileIngestor) Ingest(ctx context.Context, g *KnowledgeGraph, filePath string) (int, error) {
	if filePath == "" {
		return 0, fmt.Errorf("FileIngestor: filePath required")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("FileIngestor: read file: %w", err)
	}

	return i.llmIngestor.Ingest(ctx, g, string(content), kg.SourceFile)
}

// readEntityFromBytes parses a single entity from markdown with YAML frontmatter.
// Returns nil if no frontmatter found.
func readEntityFromBytes(data []byte) (*kg.Entity, error) {
	if len(data) == 0 {
		return nil, nil
	}

	// Look for --- delimiters
	var frontmatterStart, frontmatterEnd int
	if len(data) > 3 && data[0] == '-' && data[1] == '-' && data[2] == '-' {
		frontmatterStart = 3
	} else {
		return nil, nil // No frontmatter
	}

	// Find closing delimiter
	for i := frontmatterStart + 1; i < len(data)-2; i++ {
		if data[i] == '\n' && data[i+1] == '-' && data[i+2] == '-' && data[i+3] == '-' {
			frontmatterEnd = i
			break
		}
	}

	if frontmatterEnd == 0 {
		return nil, fmt.Errorf("malformed frontmatter: no closing delimiter")
	}

	frontmatterStr := string(data[frontmatterStart:frontmatterEnd])
	bodyStart := frontmatterEnd + 5 // Skip "\n---\n"
	bodyStr := string(data[bodyStart:])

	// Parse YAML frontmatter (simplified; assumes key: value format)
	var e kg.Entity
	e.Body = bodyStr

	// Quick YAML parsing for entity fields
	lines := splitLines(frontmatterStr)
	for _, line := range lines {
		if key, val, ok := parseYAMLLine(line); ok {
			switch key {
			case "id":
				e.ID = val
			case "kind":
				e.Kind = kg.EntityKind(val)
			case "layer":
				e.Layer = kg.EntityLayer(val)
			case "title":
				e.Title = val
			case "source":
				e.Source = kg.UpdateSource(val)
			}
		}
	}

	if e.ID == "" {
		return nil, fmt.Errorf("entity missing required id")
	}

	return &e, nil
}

func splitLines(s string) []string {
	var lines []string
	var current string
	for _, ch := range s {
		if ch == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func parseYAMLLine(line string) (key, val string, ok bool) {
	for i, ch := range line {
		if ch == ':' {
			key = line[:i]
			val = line[i+1:]
			// Trim spaces from both sides
			for len(key) > 0 && key[0] == ' ' {
				key = key[1:]
			}
			for len(key) > 0 && key[len(key)-1] == ' ' {
				key = key[:len(key)-1]
			}
			for len(val) > 0 && val[0] == ' ' {
				val = val[1:]
			}
			for len(val) > 0 && val[len(val)-1] == ' ' {
				val = val[:len(val)-1]
			}
			return key, val, true
		}
	}
	return "", "", false
}
