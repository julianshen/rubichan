// Package frontenddesign embeds the anthropics/claude-code frontend-design
// prompt skill as a built-in skill. The SKILL.md has YAML frontmatter with
// name and description, and the body becomes the inline system prompt.
package frontenddesign

import (
	"embed"
	"io/fs"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/skills/builtin/frontmatter"
)

//go:embed content
var content embed.FS

// Register walks the embedded content directory and registers each skill as a
// built-in prompt skill on the loader. Skills auto-activate in interactive mode.
// It panics on embedded content errors since these indicate a build-time bug.
func Register(loader *skills.Loader) {
	err := fs.WalkDir(content, "content", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || d.Name() != "SKILL.md" {
			return walkErr
		}

		data, readErr := content.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		name, description, body, parseErr := frontmatter.Parse(string(data))
		if parseErr != nil {
			return parseErr
		}

		m := &skills.SkillManifest{
			Name:        name,
			Version:     "1.0.0",
			Description: description,
			Types:       []skills.SkillType{skills.SkillTypePrompt},
			Prompt: skills.PromptConfig{
				SystemPromptFile: body,
			},
			Triggers: skills.TriggerConfig{
				Modes: []string{"interactive"},
			},
		}
		loader.RegisterBuiltin(m)
		return nil
	})
	if err != nil {
		panic("frontenddesign: embedded content error: " + err.Error())
	}
}
