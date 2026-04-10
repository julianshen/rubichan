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

// kindDirs lists all entity kind subdirectories in .knowledge/
var kindDirs = []string{"architecture", "decisions", "gotchas", "patterns", "modules", "integrations"}

// kindValues maps kind directories to their SQL enum values (singular forms)
var kindValues = []string{"architecture", "decision", "gotcha", "pattern", "module", "integration"}

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
	for _, subdir := range kindDirs {
		fullDir := filepath.Join(knowledgeDir, subdir)
		if err := os.MkdirAll(fullDir, 0o755); err != nil {
			return nil, fmt.Errorf("Open: creating dir %s: %w", fullDir, err)
		}
	}

	// Create layer subdirectories (team and session layers; base uses flat layout)
	for _, layerDir := range []string{"team", "session"} {
		for _, kind := range kindDirs {
			fullDir := filepath.Join(knowledgeDir, layerDir, kind)
			if err := os.MkdirAll(fullDir, 0o755); err != nil {
				return nil, fmt.Errorf("Open: creating layer dir %s: %w", fullDir, err)
			}
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
layers:
  base: Shared project patterns (git-committed, applies to all contexts)
  team: Team-specific conventions (git-committed, scoped to team)
  session: Ephemeral session findings (local only, not committed)
relationships:
  - justifies
  - relates-to
  - depends-on
  - supersedes
  - conflicts-with
  - implements
fields:
  layer: Entity scope (base|team|session, default: base) - determines visibility and persistence
  confidence: Certainty score 0.0-1.0 where 1.0 is high confidence (0.0 = unset)
  version: Optional user-set version label for tracking changes
  tags: Labels for organizing and filtering entities
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

	// Configure SQLite connection pool
	// SQLite's default journal mode (DELETE) provides sufficient serialization for writes
	// Allow multiple concurrent connections for reads; SQLite handles transaction isolation
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

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
	tagsJSON, err := json.Marshal(e.Tags)
	if err != nil {
		return fmt.Errorf("Put: marshal tags: %w", err)
	}
	stmt := `INSERT OR REPLACE INTO entities(id, kind, layer, title, tags_json, body, source, created_at, updated_at, confidence, usage_count)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = g.db.ExecContext(ctx, stmt,
		e.ID, string(e.Kind), normalizedLayer(e.Layer), e.Title, string(tagsJSON), e.Body, string(e.Source),
		e.Created.Format(time.RFC3339), e.Updated.Format(time.RFC3339), e.Confidence, e.UsageCount,
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

	// Update entity_stats if UsageCount is set
	if e.UsageCount > 0 {
		lastUsedStr := ""
		if !e.LastUsed.IsZero() {
			lastUsedStr = e.LastUsed.Format(time.RFC3339)
		}
		// Use INSERT ... ON CONFLICT to preserve query_hit_count
		if _, err := g.db.ExecContext(ctx,
			`INSERT INTO entity_stats(entity_id, injection_count, last_accessed_at)
			 VALUES(?, ?, ?)
			 ON CONFLICT(entity_id) DO UPDATE SET
			   injection_count=excluded.injection_count,
			   last_accessed_at=excluded.last_accessed_at`,
			e.ID, e.UsageCount, lastUsedStr,
		); err != nil {
			return fmt.Errorf("Put: insert entity_stats: %w", err)
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
		// Even for cached entities, we must read injection metrics from entity_stats
		// to ensure UsageCount and LastUsed are up-to-date for sorting/filtering
		var injectionCount int
		var lastAccessedStr string
		err := g.db.QueryRowContext(ctx,
			`SELECT COALESCE(injection_count, 0), COALESCE(last_accessed_at, '')
			 FROM entity_stats WHERE entity_id = ?`,
			id,
		).Scan(&injectionCount, &lastAccessedStr)

		// Only an error if something other than "no row found"
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("Get: query entity_stats: %w", err)
		}

		// Update runtime metrics on the cached entity
		e.UsageCount = injectionCount
		if lastAccessedStr != "" {
			if parsed, err := time.Parse(time.RFC3339, lastAccessedStr); err == nil {
				e.LastUsed = parsed
			}
		}

		return e, nil
	}
	g.mu.RUnlock()

	// Query from database with LEFT JOIN to entity_stats for injection metrics
	var kind, layer, title, body, source, tagsJSON, createdStr, updatedStr, lastAccessedStr string
	var confidence float64
	var injectionCount int
	err := g.db.QueryRowContext(ctx,
		`SELECT e.kind, e.layer, e.title, e.body, e.source, e.tags_json, e.created_at, e.updated_at, e.confidence,
		        COALESCE(es.injection_count, 0) as injection_count,
		        COALESCE(es.last_accessed_at, '') as last_accessed_at
		 FROM entities e
		 LEFT JOIN entity_stats es ON es.entity_id = e.id
		 WHERE e.id = ?`,
		id,
	).Scan(&kind, &layer, &title, &body, &source, &tagsJSON, &createdStr, &updatedStr, &confidence, &injectionCount, &lastAccessedStr)

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
	if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
		return nil, fmt.Errorf("Get: unmarshal tags: %w", err)
	}

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

	// Parse last_accessed_at into LastUsed, handling NULL/empty case from LEFT JOIN miss
	var lastUsed time.Time
	if lastAccessedStr != "" {
		if parsed, err := time.Parse(time.RFC3339, lastAccessedStr); err == nil {
			lastUsed = parsed
		}
	}

	e := &kg.Entity{
		ID:            id,
		Kind:          kg.EntityKind(kind),
		Layer:         kg.EntityLayer(layer),
		Title:         title,
		Body:          body,
		Source:        kg.UpdateSource(source),
		Tags:          tags,
		Relationships: rels,
		Created:       created,
		Updated:       updated,
		Confidence:    confidence,
		UsageCount:    injectionCount,
		LastUsed:      lastUsed,
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
	var kind, layer string
	err := g.db.QueryRowContext(ctx, `SELECT kind, layer FROM entities WHERE id = ?`, id).Scan(&kind, &layer)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("Delete: query kind/layer: %w", err)
	}

	if err != sql.ErrNoRows {
		// Reconstruct entity to get correct path using entityToPath
		e := &kg.Entity{
			ID:    id,
			Kind:  kg.EntityKind(kind),
			Layer: kg.EntityLayer(layer),
		}
		path := entityToPath(g.knowledgeDir, e)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("deleteEntity: remove file: %w", err)
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

	if len(filter.Layers) > 0 {
		normalized := make([]string, len(filter.Layers))
		for i, l := range filter.Layers {
			normalized[i] = normalizedLayer(l)
		}
		query += ` AND layer IN (` + repeatedPlaceholder(len(normalized)) + `)`
		for _, l := range normalized {
			args = append(args, l)
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
	defer func() { _ = tx.Rollback() }()

	// Clear all tables
	for _, table := range []string{"embeddings", "relationships", "entities_fts", "entities"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table); err != nil {
			return fmt.Errorf("rebuildIndex: delete from %s: %w", table, err)
		}
	}

	// Insert all entities
	for _, e := range entities {
		tagsJSON, err := json.Marshal(e.Tags)
		if err != nil {
			return fmt.Errorf("rebuildIndex: marshal tags for entity %s: %w", e.ID, err)
		}
		_, err = tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO entities(id, kind, layer, title, tags_json, body, source, created_at, updated_at, confidence)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.ID, string(e.Kind), normalizedLayer(e.Layer), e.Title, string(tagsJSON), e.Body, string(e.Source),
			e.Created.Format(time.RFC3339), e.Updated.Format(time.RFC3339), e.Confidence,
		)
		if err != nil {
			return fmt.Errorf("rebuildIndex: insert entity: %w", err)
		}

		// Insert relationships
		for _, rel := range e.Relationships {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR REPLACE INTO relationships(source_id, kind, target_id) VALUES(?, ?, ?)`,
				e.ID, string(rel.Kind), rel.Target,
			); err != nil {
				return fmt.Errorf("rebuildIndex: insert relationship: %w", err)
			}
		}

		// Populate FTS (must do within transaction using same connection)
		tagsStr := strings.Join(e.Tags, " ")
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO entities_fts(id, title, body, tags) VALUES(?, ?, ?, ?)`,
			e.ID, e.Title, e.Body, tagsStr,
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
	var results []kg.ScoredEntity

	// If query text is empty, return all entities matching filters (no full-text search)
	if req.Text == "" {
		// Use List to get all entities with filters applied
		entities, err := g.List(ctx, kg.ListFilter{
			Kinds:  req.KindFilter,
			Layers: req.LayerFilter,
		})
		if err != nil {
			return nil, fmt.Errorf("Query: %w", err)
		}
		// Convert to ScoredEntity with neutral score
		for _, e := range entities {
			results = append(results, kg.ScoredEntity{
				Entity:          e,
				Score:           0.5,
				EstimatedTokens: estimateTokens(e),
			})
		}
	} else {
		// Try vector embedding first
		queryVec, err := g.embedder.Embed(ctx, req.Text)

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

	// Batch missing kinds and empty bodies checks into single query
	// Use UNION to combine two separate checks into one result set with a discriminator
	kindList := "'" + strings.Join(kindValues, "','") + "'"
	batchQuery := fmt.Sprintf(`
		SELECT id, 'missing_kind' as check_type FROM entities
		WHERE kind = '' OR kind NOT IN (%s)
		UNION ALL
		SELECT id, 'empty_body' as check_type FROM entities
		WHERE body = '' OR body IS NULL
	`, kindList)

	rows, err = g.db.QueryContext(ctx, batchQuery)
	if err != nil {
		return nil, fmt.Errorf("LintGraph: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, checkType string
		if err := rows.Scan(&id, &checkType); err != nil {
			return nil, err
		}
		if checkType == "missing_kind" {
			report.MissingKinds = append(report.MissingKinds, id)
		} else if checkType == "empty_body" {
			report.EmptyBodies = append(report.EmptyBodies, id)
		}
	}

	return report, nil
}

// Stats returns knowledge graph metrics and quality indicators.
func (g *KnowledgeGraph) Stats(ctx context.Context) (*kg.KnowledgeStats, error) {
	stats := &kg.KnowledgeStats{
		ByKind: make(map[kg.EntityKind]int),
	}

	// Get total entity count and breakdown by kind
	rows, err := g.db.QueryContext(ctx, `
		SELECT kind, COUNT(*) as count FROM entities GROUP BY kind
	`)
	if err != nil {
		return nil, fmt.Errorf("Stats: counting by kind: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		stats.TotalEntities += count
		stats.ByKind[kg.EntityKind(kind)] = count
	}

	// Get breakdown by layer
	stats.ByLayer = make(map[kg.EntityLayer]int)
	rows, err = g.db.QueryContext(ctx, `
		SELECT layer, COUNT(*) as count FROM entities GROUP BY layer
	`)
	if err != nil {
		return nil, fmt.Errorf("Stats: counting by layer: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var layer string
		var count int
		if err := rows.Scan(&layer, &count); err != nil {
			return nil, err
		}
		stats.ByLayer[kg.EntityLayer(layer)] = count
	}

	// Get orphaned relationships count
	err = g.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM relationships
		WHERE target_id NOT IN (SELECT id FROM entities)
	`).Scan(&stats.OrphanedRels)
	if err != nil {
		return nil, fmt.Errorf("Stats: counting orphaned rels: %w", err)
	}

	// Get confidence metrics (average and high-confidence count)
	var totalConfidence float64
	var confidenceCount int
	err = g.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(confidence), 0), COUNT(*) FROM entities WHERE confidence > 0
	`).Scan(&totalConfidence, &confidenceCount)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("Stats: computing confidence metrics: %w", err)
	}
	if confidenceCount > 0 {
		stats.AvgScore = totalConfidence / float64(confidenceCount)
	}

	// High confidence count (>= 0.8)
	err = g.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM entities WHERE confidence >= 0.8
	`).Scan(&stats.HighConfidenceCount)
	if err != nil {
		return nil, fmt.Errorf("Stats: counting high confidence: %w", err)
	}

	// Total injections, never-used count, and stale count from entity_stats table
	// (injection tracking happens via RecordUsage in entity_stats.injection_count, not entities.usage_count)
	err = g.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(injection_count), 0),
			(SELECT COUNT(*) FROM entities WHERE id NOT IN
				(SELECT entity_id FROM entity_stats WHERE injection_count > 0)),
			COUNT(CASE WHEN last_accessed_at IS NOT NULL
				AND datetime(last_accessed_at) < datetime('now', '-30 days')
				THEN 1 END)
		FROM entity_stats
	`).Scan(&stats.TotalInjections, &stats.NeverUsedCount, &stats.StaleEntityCount)
	if err != nil {
		return nil, fmt.Errorf("Stats: counting usage metrics: %w", err)
	}

	// Total query hits from entity_stats
	err = g.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(query_hit_count), 0) FROM entity_stats
	`).Scan(&stats.TotalQueryHits)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("Stats: counting query hits: %w", err)
	}

	return stats, nil
}

// RecordEntityMentions scans responseText for entity IDs/titles from the knowledge
// graph and increments their query_hit_count. This tracks which entities were
// actually referenced in the assistant's response, not just injected.
// This is a lightweight scan — O(n_entities * response_length).
func (g *KnowledgeGraph) RecordEntityMentions(ctx context.Context, responseText string) error {
	// Query all entity IDs and titles to scan for mentions
	rows, err := g.db.QueryContext(ctx, `SELECT id, title FROM entities`)
	if err != nil {
		return fmt.Errorf("RecordEntityMentions: query entities: %w", err)
	}
	defer rows.Close()

	mentioned := make(map[string]bool) // Track which entities we've already bumped
	responseLower := strings.ToLower(responseText)

	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return err
		}

		// Skip if already recorded for this response
		if mentioned[id] {
			continue
		}

		// Simple substring search for ID (e.g., "arch-001") or title in response
		// Uses case-insensitive search
		idLower := strings.ToLower(id)
		titleLower := strings.ToLower(title)

		if strings.Contains(responseLower, idLower) || strings.Contains(responseLower, titleLower) {
			// Bump query_hit_count for this entity
			_, err := g.db.ExecContext(ctx, `
				INSERT INTO entity_stats(entity_id, query_hit_count)
				VALUES(?, 1)
				ON CONFLICT(entity_id) DO UPDATE SET
					query_hit_count = query_hit_count + 1
			`, id)
			if err != nil {
				return fmt.Errorf("RecordEntityMentions: update %s: %w", id, err)
			}
			mentioned[id] = true
		}
	}

	return rows.Err()
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

	// Convert to ScoredEntity with post-fetch filtering
	var results []kg.ScoredEntity
	for _, s := range scoredResults {
		e, err := g.Get(ctx, s.id)
		if err != nil {
			continue
		}
		if e != nil {
			// Apply KindFilter
			if len(req.KindFilter) > 0 && !containsKind(req.KindFilter, e.Kind) {
				continue
			}
			// Apply LayerFilter
			if len(req.LayerFilter) > 0 && !containsLayer(req.LayerFilter, e.Layer) {
				continue
			}
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
			// Apply KindFilter
			if len(req.KindFilter) > 0 && !containsKind(req.KindFilter, e.Kind) {
				continue
			}
			// Apply LayerFilter
			if len(req.LayerFilter) > 0 && !containsLayer(req.LayerFilter, e.Layer) {
				continue
			}
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

// normalizedLayer converts EntityLayer to normalized string for storage.
// Empty layer is treated as "base" for SQL consistency.
func normalizedLayer(l kg.EntityLayer) string {
	if l == "" {
		return string(kg.EntityLayerBase)
	}
	return string(l)
}

// containsKind checks if a kind is in the filter list.
func containsKind(kinds []kg.EntityKind, k kg.EntityKind) bool {
	for _, kind := range kinds {
		if kind == k {
			return true
		}
	}
	return false
}

// containsLayer checks if a layer is in the filter list.
// Treats empty layer as base for matching purposes.
func containsLayer(layers []kg.EntityLayer, l kg.EntityLayer) bool {
	normalizedL := normalizedLayer(l)
	for _, layer := range layers {
		normalizedFilter := normalizedLayer(layer)
		if normalizedFilter == normalizedL {
			return true
		}
	}
	return false
}

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
