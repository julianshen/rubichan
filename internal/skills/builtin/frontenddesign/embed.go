// Package frontenddesign embeds the anthropics/claude-code frontend-design
// prompt skill as a built-in skill. The SKILL.md has YAML frontmatter with
// name and description, and the body becomes the inline system prompt.
package frontenddesign

import (
	"embed"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/skills/builtin/frontmatter"
)

//go:embed content
var content embed.FS

// Register walks the embedded content directory and registers each skill as a
// built-in prompt skill on the loader. Skills auto-activate in interactive mode.
// It panics on embedded content errors since these indicate a build-time bug.
func Register(loader *skills.Loader) {
	if err := frontmatter.RegisterAll(content, loader); err != nil {
		panic("frontenddesign: embedded content error: " + err.Error())
	}
}
