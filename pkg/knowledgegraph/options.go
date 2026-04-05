package knowledgegraph

// Option configures a Graph during construction.
type Option func(*config)

type config struct {
	knowledgeDir string
	dbPath       string
	embedder     Embedder
}

// WithKnowledgeDir sets the directory where .knowledge/ files are stored.
// Default: ".knowledge"
func WithKnowledgeDir(dir string) Option {
	return func(c *config) {
		c.knowledgeDir = dir
	}
}

// WithDBPath sets the path to the SQLite index database.
// Default: <projectRoot>/.knowledge/.index.db
func WithDBPath(path string) Option {
	return func(c *config) {
		c.dbPath = path
	}
}

// WithEmbedder sets the embedder for semantic search.
// Default: NullEmbedder (triggers FTS5 fallback)
func WithEmbedder(e Embedder) Option {
	return func(c *config) {
		c.embedder = e
	}
}
