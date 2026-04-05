package knowledgegraph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

// KnowledgeGraph implements kg.Graph backed by SQLite + markdown files.
// It is safe for concurrent use.
type KnowledgeGraph struct {
	mu           sync.RWMutex          // guards cache only; DB uses MaxOpenConns(1)
	db           *sql.DB               // SQLite index (MaxOpenConns=1)
	knowledgeDir string                // path to .knowledge/
	embedder     kg.Embedder           // vector embedder (may be NullEmbedder)
	fts          *ftsSearcher          // FTS5 search helper
	cache        map[string]*kg.Entity // hot entity cache
}

// ftsSearcher wraps FTS5 search operations.
type ftsSearcher struct {
	db *sql.DB
}

// openGraph creates or opens the knowledge graph at the given project root.
// The SQLite index is stored at <projectRoot>/.knowledge/.index.db (gitignored).
// Markdown files are stored at <projectRoot>/.knowledge/ (committed).
func openGraph(ctx context.Context, projectRoot string, opts []kg.Option) (kg.Graph, error) {
	// Apply options to config
	c := &kg.OpenConfig{
		KnowledgeDir: ".knowledge",
		Embedder:     kg.NullEmbedder{},
	}
	for _, opt := range opts {
		opt.ApplyOption(c)
	}

	// Resolve knowledge directory
	knowledgeDir := c.KnowledgeDir
	if !filepath.IsAbs(knowledgeDir) {
		knowledgeDir = filepath.Join(projectRoot, knowledgeDir)
	}

	// Create knowledge directory structure if needed
	for _, subdir := range []string{"architecture", "decisions", "gotchas", "patterns", "modules", "integrations"} {
		fullDir := filepath.Join(knowledgeDir, subdir)
		if err := os.MkdirAll(fullDir, 0o755); err != nil {
			return nil, fmt.Errorf("Open: creating dir %s: %w", fullDir, err)
		}
	}

	// Create .knowledge/.gitignore
	gitignorePath := filepath.Join(knowledgeDir, ".gitignore")
	gitignoreContent := ".index.db\n"
	if _, err := os.Stat(gitignorePath); err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0o644); err != nil {
				return nil, fmt.Errorf("Open: writing .gitignore: %w", err)
			}
		}
	}

	// Create schema.yaml if it doesn't exist
	schemaPath := filepath.Join(knowledgeDir, "schema.yaml")
	if _, err := os.Stat(schemaPath); err != nil {
		if os.IsNotExist(err) {
			schemaContent := `version: 1
kinds:
  - architecture
  - decision
  - gotcha
  - pattern
  - module
  - integration
relationships:
  - justifies
  - relates-to
  - depends-on
  - supersedes
  - conflicts-with
  - implements
`
			if err := os.WriteFile(schemaPath, []byte(schemaContent), 0o644); err != nil {
				return nil, fmt.Errorf("Open: writing schema.yaml: %w", err)
			}
		}
	}

	// Open SQLite database
	dbPath := c.DBPath
	if dbPath == "" {
		dbPath = filepath.Join(knowledgeDir, ".index.db")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("Open: sql.Open: %w", err)
	}

	// Configure SQLite for serialized writes (mirrors internal/store)
	db.SetMaxOpenConns(1)

	// Create tables
	if err := createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("Open: createTables: %w", err)
	}

	g := &KnowledgeGraph{
		db:           db,
		knowledgeDir: knowledgeDir,
		embedder:     c.Embedder,
		fts:          &ftsSearcher{db: db},
		cache:        make(map[string]*kg.Entity),
	}

	// Load existing entities from markdown into cache and DB
	if err := g.rebuildIndexInternal(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("Open: rebuildIndex: %w", err)
	}

	return g, nil
}

// Put writes an entity to both markdown and SQLite index.
func (g *KnowledgeGraph) Put(ctx context.Context, e *kg.Entity) error {
	if e.ID == "" || e.Kind == "" {
		return fmt.Errorf("Put: entity requires ID and Kind")
	}

	// Set timestamps if not already set
	now := time.Now()
	if e.Created.IsZero() {
		e.Created = now
	}
	if e.Updated.IsZero() {
		e.Updated = now
	}

	// Write markdown file first
	if err := writeEntityFile(g.knowledgeDir, e); err != nil {
		return fmt.Errorf("Put: writeEntityFile: %w", err)
	}

	// Update cache (write mutex)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cache[e.ID] = e

	// Upsert into SQLite
	tagsJSON, _ := json.Marshal(e.Tags)
	stmt := `INSERT OR REPLACE INTO entities(id, kind, title, tags_json, body, source, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := g.db.ExecContext(ctx, stmt,
		e.ID, string(e.Kind), e.Title, string(tagsJSON), e.Body, string(e.Source),
		e.Created.Format(time.RFC3339), e.Updated.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("Put: insert entity: %w", err)
	}

	// Delete existing relationships for this entity
	if _, err := g.db.ExecContext(ctx, `DELETE FROM relationships WHERE source_id = ?`, e.ID); err != nil {
		return fmt.Errorf("Put: delete relationships: %w", err)
	}

	// Insert new relationships
	for _, rel := range e.Relationships {
		if _, err := g.db.ExecContext(ctx,
			`INSERT INTO relationships(source_id, kind, target_id) VALUES(?, ?, ?)`,
			e.ID, string(rel.Kind), rel.Target,
		); err != nil {
			return fmt.Errorf("Put: insert relationship: %w", err)
		}
	}

	// Update FTS index
	if err := populateFTS(g.db, e.ID, e.Title, e.Body, string(tagsJSON)); err != nil {
		return fmt.Errorf("Put: populateFTS: %w", err)
	}

	// Embed and store vector (async, non-blocking)
	go g.embedAndStore(context.Background(), e.ID, e.Title)

	return nil
}

// Get retrieves an entity by ID from cache or database.
func (g *KnowledgeGraph) Get(ctx context.Context, id string) (*kg.Entity, error) {
	// Check cache first (read mutex)
	g.mu.RLock()
	if e, ok := g.cache[id]; ok {
		g.mu.RUnlock()
		return e, nil
	}
	g.mu.RUnlock()

	// Query from database
	var kind, title, body, source, tagsJSON, createdStr, updatedStr string
	err := g.db.QueryRowContext(ctx,
		`SELECT kind, title, body, source, tags_json, created_at, updated_at FROM entities WHERE id = ?`,
		id,
	).Scan(&kind, &title, &body, &source, &tagsJSON, &createdStr, &updatedStr)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("Get: query: %w", err)
	}

	// Parse timestamps
	created, err := time.Parse(time.RFC3339, createdStr)
	if err != nil {
		created = time.Now()
	}
	updated, err := time.Parse(time.RFC3339, updatedStr)
	if err != nil {
		updated = time.Now()
	}

	// Parse tags
	var tags []string
	json.Unmarshal([]byte(tagsJSON), &tags)

	// Load relationships
	rows, err := g.db.QueryContext(ctx,
		`SELECT kind, target_id FROM relationships WHERE source_id = ?`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("Get: query relationships: %w", err)
	}
	defer rows.Close()

	var rels []kg.Relationship
	for rows.Next() {
		var relKind, targetID string
		if err := rows.Scan(&relKind, &targetID); err != nil {
			return nil, fmt.Errorf("Get: scan relationship: %w", err)
		}
		rels = append(rels, kg.Relationship{Kind: kg.RelationshipKind(relKind), Target: targetID})
	}

	e := &kg.Entity{
		ID:            id,
		Kind:          kg.EntityKind(kind),
		Title:         title,
		Body:          body,
		Source:        kg.UpdateSource(source),
		Tags:          tags,
		Relationships: rels,
		Created:       created,
		Updated:       updated,
	}

	// Update cache
	g.mu.Lock()
	g.cache[id] = e
	g.mu.Unlock()

	return e, nil
}

// Delete removes an entity from markdown and database.
func (g *KnowledgeGraph) Delete(ctx context.Context, id string) error {
	// Delete markdown file
	var kind string
	err := g.db.QueryRowContext(ctx, `SELECT kind FROM entities WHERE id = ?`, id).Scan(&kind)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("Delete: query kind: %w", err)
	}

	if err != sql.ErrNoRows {
		path := filepath.Join(g.knowledgeDir, kind, id+".md")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			// Log but don't fail if file doesn't exist
		}
	}

	// Delete from SQLite (CASCADE handles relationships + embeddings)
	if _, err := g.db.ExecContext(ctx, `DELETE FROM entities WHERE id = ?`, id); err != nil {
		return fmt.Errorf("Delete: %w", err)
	}

	// Delete from cache
	g.mu.Lock()
	delete(g.cache, id)
	g.mu.Unlock()

	return nil
}

// List retrieves entities matching the filter.
func (g *KnowledgeGraph) List(ctx context.Context, filter kg.ListFilter) ([]*kg.Entity, error) {
	query := `SELECT id FROM entities WHERE 1=1`
	var args []interface{}

	if len(filter.Kinds) > 0 {
		query += ` AND kind IN (` + repeatedPlaceholder(len(filter.Kinds)) + `)`
		for _, k := range filter.Kinds {
			args = append(args, string(k))
		}
	}

	rows, err := g.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("List: %w", err)
	}
	defer rows.Close()

	var entities []*kg.Entity
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("List: scan: %w", err)
		}

		// Load full entity
		e, err := g.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if e != nil {
			// Filter by tags (must have ALL listed tags)
			if len(filter.Tags) > 0 && !hasAllTags(e.Tags, filter.Tags) {
				continue
			}
			entities = append(entities, e)
		}
	}

	return entities, nil
}

// RebuildIndex scans .knowledge/ and repopulates the SQLite index.
func (g *KnowledgeGraph) RebuildIndex(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rebuildIndexInternal(ctx)
}

// rebuildIndexInternal does the actual rebuild (must be called with mu held for write).
func (g *KnowledgeGraph) rebuildIndexInternal(ctx context.Context) error {
	// Walk markdown directory
	entities, err := walkKnowledgeDir(g.knowledgeDir)
	if err != nil {
		return fmt.Errorf("rebuildIndex: walkKnowledgeDir: %w", err)
	}

	// Start transaction
	tx, err := g.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rebuildIndex: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Clear all tables
	for _, table := range []string{"embeddings", "relationships", "entities_fts", "entities"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table); err != nil {
			return fmt.Errorf("rebuildIndex: delete from %s: %w", table, err)
		}
	}

	// Insert all entities
	for _, e := range entities {
		tagsJSON, _ := json.Marshal(e.Tags)
		_, err := tx.ExecContext(ctx,
			`INSERT INTO entities(id, kind, title, tags_json, body, source, created_at, updated_at)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
			e.ID, string(e.Kind), e.Title, string(tagsJSON), e.Body, string(e.Source),
			e.Created.Format(time.RFC3339), e.Updated.Format(time.RFC3339),
		)
		if err != nil {
			return fmt.Errorf("rebuildIndex: insert entity: %w", err)
		}

		// Insert relationships
		for _, rel := range e.Relationships {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO relationships(source_id, kind, target_id) VALUES(?, ?, ?)`,
				e.ID, string(rel.Kind), rel.Target,
			); err != nil {
				return fmt.Errorf("rebuildIndex: insert relationship: %w", err)
			}
		}

		// Populate FTS (must do within transaction using same connection)
		tags := ""
		json.Unmarshal([]byte(string(tagsJSON)), &tags)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO entities_fts(id, title, body, tags) VALUES(?, ?, ?, ?)`,
			e.ID, e.Title, e.Body, tags,
		); err != nil {
			return fmt.Errorf("rebuildIndex: insert FTS: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rebuildIndex: commit: %w", err)
	}

	// Update cache
	g.cache = make(map[string]*kg.Entity)
	for _, e := range entities {
		g.cache[e.ID] = e
	}

	return nil
}

// Query performs semantic or keyword search.
func (g *KnowledgeGraph) Query(ctx context.Context, req kg.QueryRequest) ([]kg.ScoredEntity, error) {
	// Try vector embedding first
	queryVec, err := g.embedder.Embed(ctx, req.Text)

	var results []kg.ScoredEntity

	if err == nil && len(queryVec) > 0 {
		// Vector search
		results, err = g.vectorSearch(ctx, queryVec, req)
		if err != nil {
			// Fall through to FTS
			results = nil
		}
	}

	if len(results) == 0 {
		// FTS5 fallback or primary search
		results, err = g.ftsSearch(ctx, req.Text, req)
		if err != nil {
			return nil, fmt.Errorf("Query: %w", err)
		}
	}

	// Apply budget trimming
	if req.TokenBudget > 0 {
		results = trimByBudget(results, req.TokenBudget)
	}

	// Apply limit
	if req.Limit > 0 && len(results) > req.Limit {
		results = results[:req.Limit]
	}

	return results, nil
}

// LintGraph checks for structural issues.
func (g *KnowledgeGraph) LintGraph(ctx context.Context) (*kg.LintReport, error) {
	report := &kg.LintReport{}

	// Check for orphaned relationships
	rows, err := g.db.QueryContext(ctx, `
		SELECT source_id, kind, target_id FROM relationships
		WHERE target_id NOT IN (SELECT id FROM entities)
	`)
	if err != nil {
		return nil, fmt.Errorf("LintGraph: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sourceID, kind, targetID string
		if err := rows.Scan(&sourceID, &kind, &targetID); err != nil {
			return nil, err
		}
		report.OrphanedRelationships = append(report.OrphanedRelationships, kg.OrphanedRelationship{
			SourceID: sourceID,
			Kind:     kg.RelationshipKind(kind),
			TargetID: targetID,
		})
	}

	// Check for duplicate titles
	rows, err = g.db.QueryContext(ctx, `
		SELECT title, GROUP_CONCAT(id) as ids FROM entities
		GROUP BY title HAVING COUNT(*) > 1
	`)
	if err != nil {
		return nil, fmt.Errorf("LintGraph: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var title, idsStr string
		if err := rows.Scan(&title, &idsStr); err != nil {
			return nil, err
		}
		ids := strings.Split(idsStr, ",")
		report.DuplicateTitles = append(report.DuplicateTitles, kg.DuplicateTitle{
			Title: title,
			IDs:   ids,
		})
	}

	return report, nil
}

// Close closes the database connection.
func (g *KnowledgeGraph) Close() error {
	return g.db.Close()
}

// Helper functions

func (g *KnowledgeGraph) vectorSearch(ctx context.Context, queryVec []float32, req kg.QueryRequest) ([]kg.ScoredEntity, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT entity_id, vector, dims FROM embeddings
	`)
	if err != nil {
		return nil, fmt.Errorf("vectorSearch: %w", err)
	}
	defer rows.Close()

	type scoredResult struct {
		id    string
		score float64
	}
	var scoredResults []scoredResult

	for rows.Next() {
		var id string
		var vecBlob []byte
		var dims int
		if err := rows.Scan(&id, &vecBlob, &dims); err != nil {
			continue
		}

		vec := decodeVector(vecBlob)
		if len(vec) != len(queryVec) {
			continue
		}

		sim := cosineSimilarity(queryVec, vec)
		scoredResults = append(scoredResults, scoredResult{id: id, score: sim})
	}

	// Sort by score descending
	sort.Slice(scoredResults, func(i, j int) bool {
		return scoredResults[i].score > scoredResults[j].score
	})

	// Convert to ScoredEntity
	var results []kg.ScoredEntity
	for _, s := range scoredResults {
		e, err := g.Get(ctx, s.id)
		if err != nil {
			continue
		}
		if e != nil {
			results = append(results, kg.ScoredEntity{
				Entity:          e,
				Score:           s.score,
				EstimatedTokens: estimateTokens(e),
			})
		}
	}

	return results, nil
}

func (g *KnowledgeGraph) ftsSearch(ctx context.Context, query string, req kg.QueryRequest) ([]kg.ScoredEntity, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT id FROM entities_fts WHERE entities_fts MATCH ? ORDER BY rank LIMIT 100
	`, query)
	if err != nil {
		return nil, fmt.Errorf("ftsSearch: %w", err)
	}
	defer rows.Close()

	var results []kg.ScoredEntity
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}

		e, err := g.Get(ctx, id)
		if err != nil {
			continue
		}
		if e != nil {
			results = append(results, kg.ScoredEntity{
				Entity:          e,
				Score:           0.5, // FTS doesn't provide scores; use neutral value
				EstimatedTokens: estimateTokens(e),
			})
		}
	}

	return results, nil
}

func (g *KnowledgeGraph) embedAndStore(ctx context.Context, entityID string, text string) {
	vec, err := g.embedder.Embed(ctx, text)
	if err != nil {
		return // Silent fail; embeddings are optional
	}

	vecBlob := encodeVector(vec)
	_, _ = g.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO embeddings(entity_id, vector, dims) VALUES(?, ?, ?)`,
		entityID, vecBlob, g.embedder.Dims(),
	)
}

// Utility functions

func repeatedPlaceholder(count int) string {
	result := ""
	for i := 0; i < count; i++ {
		if i > 0 {
			result += ", "
		}
		result += "?"
	}
	return result
}

func hasAllTags(entityTags, filterTags []string) bool {
	for _, filter := range filterTags {
		found := false
		for _, tag := range entityTags {
			if tag == filter {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func trimByBudget(results []kg.ScoredEntity, budget int) []kg.ScoredEntity {
	var trimmed []kg.ScoredEntity
	accumulated := 0
	for _, r := range results {
		if accumulated+r.EstimatedTokens > budget {
			break
		}
		accumulated += r.EstimatedTokens
		trimmed = append(trimmed, r)
	}
	return trimmed
}

func estimateTokens(e *kg.Entity) int {
	return (len(e.Title) + len(e.Body) + len(e.ID) + 100 + 3) / 4
}
