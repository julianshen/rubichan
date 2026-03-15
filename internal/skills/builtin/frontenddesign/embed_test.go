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

func TestFrontendDesignRegisterDesignCommand(t *testing.T) {
	loader := skills.NewLoader("", "")
	Register(loader)

	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(discovered) == 0 {
		t.Fatalf("Discover returned no skills")
	}

	cmds := discovered[0].Manifest.Commands
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1", len(cmds))
	}
	if cmds[0].Name != "design" {
		t.Errorf("command name = %q, want %q", cmds[0].Name, "design")
	}
}

func TestFrontendDesignIncludesAutoTriggers(t *testing.T) {
	loader := skills.NewLoader("", "")
	Register(loader)

	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(discovered) == 0 {
		t.Fatalf("Discover returned no skills")
	}

	ds := discovered[0]
	if len(ds.Manifest.Triggers.Files) == 0 {
		t.Fatal("expected file triggers")
	}
	if len(ds.Manifest.Triggers.Languages) == 0 {
		t.Fatal("expected language triggers")
	}
	if len(ds.Manifest.Triggers.Modes) == 0 {
		t.Fatal("expected mode triggers")
	}
	if len(ds.Manifest.Triggers.Keywords) == 0 {
		t.Fatal("expected keyword triggers")
	}
}
