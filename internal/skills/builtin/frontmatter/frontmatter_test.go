package frontmatter

import (
	"testing"
)

func TestParse(t *testing.T) {
	input := "---\nname: brainstorming\ndescription: \"A creative skill\"\n---\n\n# Body content\nHello world"
	name, desc, body, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "brainstorming" {
		t.Errorf("name = %q, want %q", name, "brainstorming")
	}
	if desc != "A creative skill" {
		t.Errorf("description = %q, want %q", desc, "A creative skill")
	}
	if body != "# Body content\nHello world" {
		t.Errorf("body = %q, want %q", body, "# Body content\nHello world")
	}
}

func TestParseMissingOpeningDelimiter(t *testing.T) {
	_, _, _, err := Parse("no frontmatter here")
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestParseMissingClosingDelimiter(t *testing.T) {
	_, _, _, err := Parse("---\nname: test\ndescription: foo\n")
	if err == nil {
		t.Fatal("expected error for missing closing delimiter")
	}
}

func TestParseNoNewlineAfterOpening(t *testing.T) {
	_, _, _, err := Parse("---")
	if err == nil {
		t.Fatal("expected error for no newline after opening delimiter")
	}
}

func TestParseIgnoresSubstringDashes(t *testing.T) {
	// Description containing "---" inline should not be treated as the closing delimiter.
	input := "---\nname: tricky\ndescription: \"uses --- in value\"\n---\n\nBody"
	name, desc, body, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "tricky" {
		t.Errorf("name = %q, want %q", name, "tricky")
	}
	if desc != "uses --- in value" {
		t.Errorf("description = %q, want %q", desc, "uses --- in value")
	}
	if body != "Body" {
		t.Errorf("body = %q, want %q", body, "Body")
	}
}

func TestParseUnquotedDescription(t *testing.T) {
	input := "---\nname: test-skill\ndescription: A test skill\n---\n\nBody here"
	name, desc, body, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "test-skill" {
		t.Errorf("name = %q, want %q", name, "test-skill")
	}
	if desc != "A test skill" {
		t.Errorf("description = %q, want %q", desc, "A test skill")
	}
	if body != "Body here" {
		t.Errorf("body = %q, want %q", body, "Body here")
	}
}
