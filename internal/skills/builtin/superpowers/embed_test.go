package superpowers

import (
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
)

func TestParseFrontmatter(t *testing.T) {
	input := "---\nname: brainstorming\ndescription: \"A creative skill\"\n---\n\n# Body content\nHello world"
	name, desc, body, err := parseFrontmatter(input)
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

func TestParseFrontmatterMissingDelimiter(t *testing.T) {
	_, _, _, err := parseFrontmatter("no frontmatter here")
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestRegisterPopulatesLoader(t *testing.T) {
	loader := skills.NewLoader("", "")
	Register(loader)

	// Discover all registered builtins.
	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	expectedSkills := []string{
		"brainstorming",
		"dispatching-parallel-agents",
		"executing-plans",
		"finishing-a-development-branch",
		"receiving-code-review",
		"requesting-code-review",
		"subagent-driven-development",
		"systematic-debugging",
		"test-driven-development",
		"using-git-worktrees",
		"using-superpowers",
		"verification-before-completion",
		"writing-plans",
		"writing-skills",
	}

	if len(discovered) != len(expectedSkills) {
		t.Fatalf("got %d skills, want %d", len(discovered), len(expectedSkills))
	}

	byName := make(map[string]skills.DiscoveredSkill)
	for _, ds := range discovered {
		byName[ds.Manifest.Name] = ds
	}

	for _, name := range expectedSkills {
		ds, ok := byName[name]
		if !ok {
			t.Errorf("skill %q not found", name)
			continue
		}
		if ds.Source != skills.SourceBuiltin {
			t.Errorf("skill %q: source = %q, want %q", name, ds.Source, skills.SourceBuiltin)
		}
		if ds.Dir != "" {
			t.Errorf("skill %q: dir = %q, want empty", name, ds.Dir)
		}
		if ds.Manifest.Description == "" {
			t.Errorf("skill %q: description is empty", name)
		}
		if len(ds.Manifest.Types) != 1 || ds.Manifest.Types[0] != skills.SkillTypePrompt {
			t.Errorf("skill %q: types = %v, want [prompt]", name, ds.Manifest.Types)
		}
		if ds.Manifest.Prompt.SystemPromptFile == "" {
			t.Errorf("skill %q: SystemPromptFile is empty", name)
		}
		if len(ds.Manifest.Triggers.Modes) == 0 {
			t.Errorf("skill %q: no mode triggers set", name)
		}
	}
}

func TestSkillContentNotEmpty(t *testing.T) {
	loader := skills.NewLoader("", "")
	Register(loader)

	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	for _, ds := range discovered {
		// SystemPromptFile holds inline content for builtins.
		if len(ds.Manifest.Prompt.SystemPromptFile) < 100 {
			t.Errorf("skill %q: content too short (%d bytes)", ds.Manifest.Name, len(ds.Manifest.Prompt.SystemPromptFile))
		}
	}
}
