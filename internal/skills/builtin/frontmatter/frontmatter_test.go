package frontmatter

import (
	"testing"
	"testing/fstest"

	"github.com/julianshen/rubichan/internal/skills"
)

func TestRegisterAllFullParsesBasicFrontmatter(t *testing.T) {
	fsys := fstest.MapFS{
		"content/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\n\n# Body\nHello"),
		},
	}

	loader := skills.NewLoader("", "")
	if err := RegisterAllFull(fsys, loader); err != nil {
		t.Fatalf("RegisterAllFull: %v", err)
	}

	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(discovered) != 1 {
		t.Fatalf("got %d skills, want 1", len(discovered))
	}

	ds := discovered[0]
	if ds.Manifest.Name != "my-skill" {
		t.Errorf("name = %q, want %q", ds.Manifest.Name, "my-skill")
	}
	if ds.Manifest.Description != "A test skill" {
		t.Errorf("description = %q, want %q", ds.Manifest.Description, "A test skill")
	}
	if ds.Manifest.Prompt.SystemPromptFile != "# Body\nHello" {
		t.Errorf("body = %q, want %q", ds.Manifest.Prompt.SystemPromptFile, "# Body\nHello")
	}
}

func TestRegisterAllFullSetsDefaultVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"content/no-version/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: no-version\ndescription: Missing version field\n---\n\nBody"),
		},
	}

	loader := skills.NewLoader("", "")
	if err := RegisterAllFull(fsys, loader); err != nil {
		t.Fatalf("RegisterAllFull: %v", err)
	}

	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(discovered) != 1 {
		t.Fatalf("got %d skills, want 1", len(discovered))
	}

	if discovered[0].Manifest.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", discovered[0].Manifest.Version, "1.0.0")
	}
}

func TestRegisterAllFullSetsInteractiveTrigger(t *testing.T) {
	fsys := fstest.MapFS{
		"content/interactive/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: interactive-skill\ndescription: Interactive skill\n---\n\nBody"),
		},
	}

	loader := skills.NewLoader("", "")
	if err := RegisterAllFull(fsys, loader); err != nil {
		t.Fatalf("RegisterAllFull: %v", err)
	}

	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	modes := discovered[0].Manifest.Triggers.Modes
	if len(modes) != 1 || modes[0] != "interactive" {
		t.Errorf("triggers.modes = %v, want [interactive]", modes)
	}
}

func TestRegisterAllFullParsesCommands(t *testing.T) {
	fsys := fstest.MapFS{
		"content/cmd-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: cmd-skill\ndescription: Skill with commands\ncommands:\n  - name: plan\n    description: Start planning\n---\n\nBody"),
		},
	}

	loader := skills.NewLoader("", "")
	if err := RegisterAllFull(fsys, loader); err != nil {
		t.Fatalf("RegisterAllFull: %v", err)
	}

	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	cmds := discovered[0].Manifest.Commands
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1", len(cmds))
	}
	if cmds[0].Name != "plan" {
		t.Errorf("command name = %q, want %q", cmds[0].Name, "plan")
	}
	if cmds[0].Description != "Start planning" {
		t.Errorf("command description = %q, want %q", cmds[0].Description, "Start planning")
	}
}

func TestRegisterAllFullIgnoresUnknownFields(t *testing.T) {
	fsys := fstest.MapFS{
		"content/extra-fields/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: extra-fields\ndescription: Has unknown fields\nlicense: MIT\n---\n\nBody"),
		},
	}

	loader := skills.NewLoader("", "")
	if err := RegisterAllFull(fsys, loader); err != nil {
		t.Fatalf("RegisterAllFull: %v", err)
	}

	discovered, _, err := loader.Discover(nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(discovered) != 1 {
		t.Fatalf("got %d skills, want 1", len(discovered))
	}
	if discovered[0].Manifest.Name != "extra-fields" {
		t.Errorf("name = %q, want %q", discovered[0].Manifest.Name, "extra-fields")
	}
}
