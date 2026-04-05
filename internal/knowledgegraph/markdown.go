package knowledgegraph

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
	"gopkg.in/yaml.v3"
)

// frontmatter is the YAML structure parsed from each .md file's --- block.
type frontmatter struct {
	ID            string                `yaml:"id"`
	Kind          string                `yaml:"kind"`
	Layer         string                `yaml:"layer,omitempty"` // empty = base layer
	Title         string                `yaml:"title"`
	Tags          []string              `yaml:"tags"`
	Created       time.Time             `yaml:"created"`
	Updated       time.Time             `yaml:"updated"`
	Source        string                `yaml:"source"`
	Relationships []frontmatterRelation `yaml:"relationships"`
	// Lifecycle fields (user-editable in markdown)
	Version    string  `yaml:"version"`
	Confidence float64 `yaml:"confidence"`
}

type frontmatterRelation struct {
	Kind   string `yaml:"kind"`
	Target string `yaml:"target"`
}

// entityToPath returns the canonical file path for an entity.
// Base layer (layer="" or layer="base"): .knowledge/<kind>/<id>.md
// Team/Session layers: .knowledge/<layer>/<kind>/<id>.md
func entityToPath(knowledgeDir string, e *kg.Entity) string {
	layer := string(e.Layer)
	if layer == "" || layer == string(kg.EntityLayerBase) {
		// Base layer uses flat path (backward compatible)
		return filepath.Join(knowledgeDir, string(e.Kind), e.ID+".md")
	}
	// Team/Session layers use prefixed path
	return filepath.Join(knowledgeDir, layer, string(e.Kind), e.ID+".md")
}

// writeEntityFile serializes an entity to its canonical markdown file.
// Creates directories as needed.
func writeEntityFile(knowledgeDir string, e *kg.Entity) error {
	path := entityToPath(knowledgeDir, e)

	// Create directory structure
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("writeEntityFile: creating dir %s: %w", dir, err)
	}

	// Build frontmatter
	fm := frontmatter{
		ID:         e.ID,
		Kind:       string(e.Kind),
		Layer:      string(e.Layer),
		Title:      e.Title,
		Tags:       e.Tags,
		Created:    e.Created,
		Updated:    e.Updated,
		Source:     string(e.Source),
		Version:    e.Version,
		Confidence: e.Confidence,
	}

	// Convert relationships
	for _, rel := range e.Relationships {
		fm.Relationships = append(fm.Relationships, frontmatterRelation{
			Kind:   string(rel.Kind),
			Target: rel.Target,
		})
	}

	// Serialize frontmatter
	var fmBuf bytes.Buffer
	if err := yaml.NewEncoder(&fmBuf).Encode(fm); err != nil {
		return fmt.Errorf("writeEntityFile: encoding frontmatter: %w", err)
	}

	// Build complete markdown: --- + YAML + --- + body
	var content bytes.Buffer
	content.WriteString("---\n")
	content.Write(fmBuf.Bytes())
	content.WriteString("---\n")
	content.WriteString(e.Body)
	if e.Body != "" && !strings.HasSuffix(e.Body, "\n") {
		content.WriteString("\n")
	}

	// Write file
	if err := os.WriteFile(path, content.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writeEntityFile: writing file %s: %w", path, err)
	}

	return nil
}

// readEntityFile parses a markdown file into an Entity.
// Expects YAML frontmatter between --- markers, followed by markdown body.
func readEntityFile(path string) (*kg.Entity, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("readEntityFile: reading %s: %w", path, err)
	}

	// Split frontmatter and body
	parts := bytes.SplitN(content, []byte("---"), 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("readEntityFile: %s: missing frontmatter delimiters", path)
	}

	// Parse YAML frontmatter (parts[1])
	fm := &frontmatter{}
	if err := yaml.Unmarshal(parts[1], fm); err != nil {
		return nil, fmt.Errorf("readEntityFile: parsing YAML from %s: %w", path, err)
	}

	// Body is parts[2], trimming leading newline
	body := string(bytes.TrimLeft(parts[2], "\n"))
	body = strings.TrimRight(body, "\n")

	// Convert relationships
	rels := make([]kg.Relationship, len(fm.Relationships))
	for i, r := range fm.Relationships {
		rels[i] = kg.Relationship{
			Kind:   kg.RelationshipKind(r.Kind),
			Target: r.Target,
		}
	}

	// Convert tags from slice (or nil to empty slice)
	tags := fm.Tags
	if tags == nil {
		tags = []string{}
	}

	return &kg.Entity{
		ID:            fm.ID,
		Kind:          kg.EntityKind(fm.Kind),
		Layer:         kg.EntityLayer(fm.Layer),
		Title:         fm.Title,
		Tags:          tags,
		Body:          body,
		Relationships: rels,
		Source:        kg.UpdateSource(fm.Source),
		Created:       fm.Created,
		Updated:       fm.Updated,
		Version:       fm.Version,
		Confidence:    fm.Confidence,
	}, nil
}

// walkKnowledgeDir walks all .md files in knowledgeDir and returns parsed entities.
// Uses filepath.WalkDir for efficiency. Returns an error if any file fails to parse,
// but accumulated entities up to that point are lost (transaction-like semantics).
func walkKnowledgeDir(knowledgeDir string) ([]*kg.Entity, error) {
	var entities []*kg.Entity

	// Check if knowledgeDir exists
	info, err := os.Stat(knowledgeDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist yet; return empty list
			return entities, nil
		}
		return nil, fmt.Errorf("walkKnowledgeDir: stat %s: %w", knowledgeDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("walkKnowledgeDir: %s is not a directory", knowledgeDir)
	}

	err = filepath.WalkDir(knowledgeDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-.md files
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		// Parse the markdown file
		e, err := readEntityFile(path)
		if err != nil {
			return err
		}

		entities = append(entities, e)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walkKnowledgeDir: %w", err)
	}

	return entities, nil
}
