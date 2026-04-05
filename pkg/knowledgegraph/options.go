package knowledgegraph

// Option configures a Graph during construction.
type Option interface {
	ApplyOption(*OpenConfig)
}

// OpenConfig holds configuration for Open. This is exported only for use by Option.
type OpenConfig struct {
	KnowledgeDir string
	DBPath       string
	Embedder     Embedder
}

type withKnowledgeDirOpt string

func (d withKnowledgeDirOpt) ApplyOption(c *OpenConfig) {
	c.KnowledgeDir = string(d)
}

// WithKnowledgeDir sets the directory where .knowledge/ files are stored.
// Default: ".knowledge"
func WithKnowledgeDir(dir string) Option {
	return withKnowledgeDirOpt(dir)
}

type withDBPathOpt string

func (d withDBPathOpt) ApplyOption(c *OpenConfig) {
	c.DBPath = string(d)
}

// WithDBPath sets the path to the SQLite index database.
// Default: <projectRoot>/.knowledge/.index.db
func WithDBPath(path string) Option {
	return withDBPathOpt(path)
}

type withEmbedderOpt struct {
	embedder Embedder
}

func (e withEmbedderOpt) ApplyOption(c *OpenConfig) {
	c.Embedder = e.embedder
}

// WithEmbedder sets the embedder for semantic search.
// Default: NullEmbedder (triggers FTS5 fallback)
func WithEmbedder(e Embedder) Option {
	return withEmbedderOpt{embedder: e}
}
