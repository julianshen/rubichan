package knowledgegraph

import "time"

// EntityKind classifies what a knowledge entity describes.
type EntityKind string

const (
	KindArchitecture EntityKind = "architecture"
	KindDecision     EntityKind = "decision"
	KindGotcha       EntityKind = "gotcha"
	KindPattern      EntityKind = "pattern"
	KindModule       EntityKind = "module"
	KindIntegration  EntityKind = "integration"
)

// RelationshipKind describes how two entities relate.
type RelationshipKind string

const (
	RelJustifies  RelationshipKind = "justifies"
	RelRelatesTo  RelationshipKind = "relates-to"
	RelDependsOn  RelationshipKind = "depends-on"
	RelSupersedes RelationshipKind = "supersedes"
	RelConflicts  RelationshipKind = "conflicts-with"
	RelImplements RelationshipKind = "implements"
)

// UpdateSource records how an entity entered the graph.
type UpdateSource string

const (
	SourceLLM    UpdateSource = "llm"
	SourceGit    UpdateSource = "git"
	SourceManual UpdateSource = "manual"
	SourceFile   UpdateSource = "file" // AGENT.md / CLAUDE.md ingest
)

// EntityLayer organizes entities by scope.
type EntityLayer string

const (
	EntityLayerBase    EntityLayer = "base"
	EntityLayerTeam    EntityLayer = "team"
	EntityLayerSession EntityLayer = "session"
)

// Relationship is a directed edge between two entities.
type Relationship struct {
	Kind   RelationshipKind `yaml:"kind"`
	Target string           `yaml:"target"` // target entity ID
}

// Entity is a single node in the knowledge graph.
// It maps directly to one markdown file in .knowledge/.
type Entity struct {
	ID            string
	Kind          EntityKind
	Layer         EntityLayer // "" treated as base; git-committed in frontmatter
	Title         string
	Tags          []string
	Body          string
	Relationships []Relationship
	Source        UpdateSource
	Created       time.Time
	Updated       time.Time
	// Lifecycle fields (user-editable, in frontmatter)
	Version    string  // optional user-set version label
	Confidence float64 // 0.0 = unset, 1.0 = high confidence
	// Runtime metrics (SQLite-only, not committed)
	UsageCount int       // times entity was returned in query results
	LastUsed   time.Time // last time entity was injected into a prompt
}
