package wiki

import "github.com/julianshen/rubichan/internal/parser"

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

// SkillWikiSection holds a wiki contribution from a skill.
type SkillWikiSection struct {
	SkillName string
	Title     string
	Content   string
	Diagrams  []Diagram
}
