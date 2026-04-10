package knowledgegraph

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
	"gopkg.in/yaml.v3"
)

// ValidationError indicates a frontmatter field failed validation.
// Distinct from I/O or parse errors so callers can skip bad files
// without swallowing infrastructure failures.
type ValidationError struct {
	Path string
	Msg  string
}

func (e *ValidationError) Error() string { return e.Msg }

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

// validKinds enumerates accepted EntityKind values for frontmatter validation.
var validKinds = map[string]bool{
	string(kg.KindArchitecture): true,
	string(kg.KindDecision):     true,
	string(kg.KindGotcha):       true,
	string(kg.KindPattern):      true,
	string(kg.KindModule):       true,
	string(kg.KindIntegration):  true,
}

// validateFrontmatter checks required fields and value constraints.
// Returns *ValidationError for field-level issues so callers can
// distinguish validation failures from I/O or parse errors.
func validateFrontmatter(fm *frontmatter, path string) error {
	if strings.TrimSpace(fm.ID) == "" {
		return &ValidationError{Path: path, Msg: fmt.Sprintf("readEntityFile: %s: missing required field 'id'", path)}
	}
	if strings.TrimSpace(fm.Kind) == "" {
		return &ValidationError{Path: path, Msg: fmt.Sprintf("readEntityFile: %s: missing required field 'kind'", path)}
	}
	if !validKinds[fm.Kind] {
		return &ValidationError{Path: path, Msg: fmt.Sprintf("readEntityFile: %s: invalid kind %q (valid: architecture, decision, gotcha, pattern, module, integration)", path, fm.Kind)}
	}
	if fm.Confidence < 0 || fm.Confidence > 1 {
		return &ValidationError{Path: path, Msg: fmt.Sprintf("readEntityFile: %s: confidence must be between 0.0 and 1.0, got %g", path, fm.Confidence)}
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

	// Validate required fields
	if err := validateFrontmatter(fm, path); err != nil {
		return nil, err
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
// Files with validation errors (missing ID, invalid kind, etc.) are skipped with a
// log warning rather than aborting the entire walk.
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

		// Parse the markdown file; skip validation errors but propagate
		// I/O and parse errors which indicate infrastructure problems.
		e, err := readEntityFile(path)
		if err != nil {
			var valErr *ValidationError
			if errors.As(err, &valErr) {
				log.Printf("walkKnowledgeDir: skipping %s: %v", path, err)
				return nil
			}
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
