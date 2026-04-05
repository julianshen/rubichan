package knowledgegraph

import "context"

// Ingestor extracts entities from an external source and writes them to a Graph.
type Ingestor interface {
	Ingest(ctx context.Context, g Graph) error
}

// IngestSource identifies the provenance of an ingest operation.
type IngestSource string

const (
	IngestSourceLLM    IngestSource = "llm"
	IngestSourceGit    IngestSource = "git"
	IngestSourceManual IngestSource = "manual"
	IngestSourceFile   IngestSource = "file"
)
