package wiki

import (
	"context"

	"github.com/julianshen/rubichan/internal/parser"
)

// ScannedFile represents a source file discovered by the scanner stage.
type ScannedFile struct {
	Path      string
	Language  string
	Functions []parser.FunctionDef
	Imports   []string
	Size      int64
	Module    string
}

// Chunk groups related files for LLM analysis.
type Chunk struct {
	Module string
	Files  []ScannedFile
	Source []byte
}

// AnalysisResult holds the complete output of the LLM analyzer stage.
type AnalysisResult struct {
	Modules         []ModuleAnalysis
	Architecture    string
	KeyAbstractions string
	Suggestions     []string
}

// ModuleAnalysis summarizes a single module from the LLM.
type ModuleAnalysis struct {
	Module   string
	Summary  string
	KeyTypes string
	Patterns string
	Concerns string
}

// Diagram holds a generated Mermaid diagram.
type Diagram struct {
	Title   string
	Type    string // "architecture", "dependency", "data-flow", "sequence"
	Content string // Mermaid source
}

// Document represents a single output page in the wiki.
type Document struct {
	Path    string
	Title   string
	Content string
}

// WikiResult summarises the outcome of a successful wiki generation run.
type WikiResult struct {
	OutputDir          string   `json:"output_dir"`
	Format             string   `json:"format"`
	Documents          int      `json:"documents"`
	NewDocuments       int      `json:"new_documents"`
	UpdatedDocuments   int      `json:"updated_documents"`
	UnchangedDocuments int      `json:"unchanged_documents"`
	Diagrams           int      `json:"diagrams"`
	DurationMs         int64    `json:"duration_ms"`
	APISurfaces        []string `json:"api_surfaces,omitempty"`
	SecurityDepth      []string `json:"security_depth,omitempty"`
}

// SkillWikiSection holds a wiki contribution from a skill.
type SkillWikiSection struct {
	SkillName string
	Title     string
	Content   string
	Diagrams  []Diagram
}

// SpecializedAnalyzer produces documents and diagrams for a specific domain.
type SpecializedAnalyzer interface {
	Name() string
	Analyze(ctx context.Context, input AnalyzerInput) (*AnalyzerOutput, error)
}

// APIPattern represents a detected API registration point in source code.
type APIPattern struct {
	Kind     string // "http", "grpc", "cli", "graphql", "websocket", "export"
	Method   string // "GET", "POST" etc. (HTTP only, empty for others)
	Path     string // route path, command name, or service name
	Handler  string // function/method name handling this
	File     string // source file path
	Line     int    // line number (1-based)
	Language string // detected language
}

// AnalyzerInput provides shared context from the base analysis pass.
type AnalyzerInput struct {
	Chunks         []Chunk
	Files          []ScannedFile
	ModuleAnalyses []ModuleAnalysis
	Architecture   string
	APIPatterns    []APIPattern
}

// ValidationWarning represents a factual claim in wiki output that couldn't
// be verified against the actual codebase.
type ValidationWarning struct {
	Document string // path of document containing the claim
	Line     int    // approximate line number
	Claim    string // the unverified claim text
	Check    string // what was checked
	Result   string // what was actually found
}

// AnalyzerOutput holds documents and diagrams from a specialized analyzer.
type AnalyzerOutput struct {
	Documents []Document
	Diagrams  []Diagram
}
