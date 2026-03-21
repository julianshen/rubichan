package codereview

import (
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
)

func TestRegisterPopulatesLoader(t *testing.T) {
	loader := skills.NewLoader("", "")
	Register(loader)

	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(discovered) != 1 {
		t.Fatalf("got %d skills, want 1", len(discovered))
	}

	ds := discovered[0]
	if ds.Manifest.Name != "review-guide" {
		t.Errorf("name = %q, want %q", ds.Manifest.Name, "review-guide")
	}
	if ds.Source != skills.SourceBuiltin {
		t.Errorf("source = %q, want %q", ds.Source, skills.SourceBuiltin)
	}
	if ds.Manifest.Description == "" {
		t.Error("description is empty")
	}
	if len(ds.Manifest.Types) != 1 || ds.Manifest.Types[0] != skills.SkillTypePrompt {
		t.Errorf("types = %v, want [prompt]", ds.Manifest.Types)
	}
	if ds.Manifest.Prompt.SystemPromptFile == "" {
		t.Error("SystemPromptFile is empty")
	}
	if len(ds.Manifest.Prompt.SystemPromptFile) < 100 {
		t.Errorf("content too short (%d bytes)", len(ds.Manifest.Prompt.SystemPromptFile))
	}
}

func TestReviewGuideIncludesAuthoringPatterns(t *testing.T) {
	loader := skills.NewLoader("", "")
	Register(loader)

	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(discovered) == 0 {
		t.Fatal("Discover returned no skills")
	}

	content := discovered[0].Manifest.Prompt.SystemPromptFile

	requiredSections := []string{
		"## Thinking Phase",
		"## Anti-Patterns",
		"## Calibration",
		"## Approaches",
		"## Verification",
	}
	for _, section := range requiredSections {
		if !strings.Contains(content, section) {
			t.Errorf("content missing required section %q", section)
		}
	}
}

func TestReviewGuideIncludesTriggers(t *testing.T) {
	loader := skills.NewLoader("", "")
	Register(loader)

	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(discovered) == 0 {
		t.Fatal("Discover returned no skills")
	}

	ds := discovered[0]
	if len(ds.Manifest.Triggers.Keywords) == 0 {
		t.Fatal("expected keyword triggers")
	}
	if len(ds.Manifest.Triggers.Modes) == 0 {
		t.Fatal("expected mode triggers")
	}
}
