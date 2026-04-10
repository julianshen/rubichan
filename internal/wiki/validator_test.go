package wiki

import (
	"testing"
)

func TestValidateDocs_FlagsFalseDependencyConflict(t *testing.T) {
	docs := []Document{
		{
			Path:  "architecture.md",
			Title: "Architecture",
			Content: `# Architecture
The project has conflicting SQLite drivers mattn/go-sqlite3 causing build issues.
This needs to be resolved.`,
		},
	}
	projectFiles := map[string]string{
		"go.mod": `module example.com/myproject

go 1.21

require (
	modernc.org/sqlite v1.25.0
)`,
	}

	warnings := ValidateDocs(docs, projectFiles)

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	w := warnings[0]
	if w.Document != "architecture.md" {
		t.Errorf("expected document architecture.md, got %s", w.Document)
	}
	if w.Line != 2 {
		t.Errorf("expected line 2, got %d", w.Line)
	}
	if w.Result != "modernc.org/sqlite only" {
		t.Errorf("expected result 'modernc.org/sqlite only', got %q", w.Result)
	}
}

func TestValidateDocs_FlagsFalseBrokenSQL(t *testing.T) {
	docs := []Document{
		{
			Path:  "security.md",
			Title: "Security",
			Content: `# Security Issues
Found broken SQL string concatenation in the data layer.
Recommend using prepared statements.`,
		},
	}
	projectFiles := map[string]string{
		"store/users.go": `package store

func (s *Store) GetUser(id int) (*User, error) {
	return s.db.QueryRow("SELECT * FROM users WHERE id = ?", id)
}`,
	}

	warnings := ValidateDocs(docs, projectFiles)

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	w := warnings[0]
	if w.Document != "security.md" {
		t.Errorf("expected document security.md, got %s", w.Document)
	}
	if w.Line != 2 {
		t.Errorf("expected line 2, got %d", w.Line)
	}
	if w.Result != "parameterized queries found" {
		t.Errorf("expected result about parameterized queries, got %q", w.Result)
	}
}

func TestValidateDocs_FlagsFalseMissingSource(t *testing.T) {
	docs := []Document{
		{
			Path:  "overview.md",
			Title: "Overview",
			Content: `# Overview
Warning: missing source code in ` + "`bin`" + ` directory.
No implementation found.`,
		},
	}
	projectFiles := map[string]string{
		"bin/server": "",
	}

	warnings := ValidateDocs(docs, projectFiles)

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	w := warnings[0]
	if w.Document != "overview.md" {
		t.Errorf("expected document overview.md, got %s", w.Document)
	}
	if w.Line != 2 {
		t.Errorf("expected line 2, got %d", w.Line)
	}
	if w.Result != "files found in bin" {
		t.Errorf("expected result about files found, got %q", w.Result)
	}
}

func TestValidateDocs_NoFalsePositiveOnRealIssue(t *testing.T) {
	docs := []Document{
		{
			Path:  "security.md",
			Title: "Security",
			Content: `# Security
The API has no authentication middleware protecting sensitive endpoints.
This should be addressed before production.`,
		},
	}
	projectFiles := map[string]string{
		"go.mod":           `module example.com/myproject`,
		"internal/api.go":  "",
		"internal/main.go": "",
	}

	warnings := ValidateDocs(docs, projectFiles)

	if len(warnings) != 0 {
		t.Fatalf("expected 0 warnings for real issues, got %d: %+v", len(warnings), warnings)
	}
}

func TestValidateDocs_NoClaims(t *testing.T) {
	docs := []Document{
		{
			Path:  "readme.md",
			Title: "README",
			Content: `# My Project
This is a well-structured Go project with clean architecture.
It uses the repository pattern for data access.`,
		},
	}
	projectFiles := map[string]string{
		"go.mod":     `module example.com/myproject`,
		"main.go":    "",
		"handler.go": "",
	}

	warnings := ValidateDocs(docs, projectFiles)

	if len(warnings) != 0 {
		t.Fatalf("expected 0 warnings, got %d: %+v", len(warnings), warnings)
	}
}

func TestStripFalseClaims_RemovesFlaggedLines(t *testing.T) {
	docs := []Document{
		{
			Path:  "doc.md",
			Title: "Doc",
			Content: `line one
line two is false
line three
line four is also false
line five`,
		},
	}
	warnings := []ValidationWarning{
		{Document: "doc.md", Line: 2, Claim: "line two is false", Check: "test", Result: "false"},
		{Document: "doc.md", Line: 4, Claim: "line four is also false", Check: "test", Result: "false"},
	}

	result := stripFalseClaims(docs, warnings)

	if len(result) != 1 {
		t.Fatalf("expected 1 document, got %d", len(result))
	}

	expected := "line one\nline three\nline five"
	if result[0].Content != expected {
		t.Errorf("expected content:\n%s\ngot:\n%s", expected, result[0].Content)
	}
	if result[0].Path != "doc.md" {
		t.Errorf("expected path doc.md, got %s", result[0].Path)
	}
	if result[0].Title != "Doc" {
		t.Errorf("expected title Doc, got %s", result[0].Title)
	}
}

func TestStripFalseClaims_NoWarnings(t *testing.T) {
	docs := []Document{
		{
			Path:    "doc.md",
			Title:   "Doc",
			Content: "line one\nline two\nline three",
		},
	}

	result := stripFalseClaims(docs, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 document, got %d", len(result))
	}
	if result[0].Content != docs[0].Content {
		t.Errorf("expected content unchanged, got %q", result[0].Content)
	}
}
