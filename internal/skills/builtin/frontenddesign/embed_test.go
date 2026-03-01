package frontenddesign

import (
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
	if ds.Manifest.Name != "frontend-design" {
		t.Errorf("name = %q, want %q", ds.Manifest.Name, "frontend-design")
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
