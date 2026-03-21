// Package codereview embeds the review-guide prompt skill as a built-in skill.
// The SKILL.md has YAML frontmatter with name, description, and triggers, and
// the body becomes the inline system prompt. This skill demonstrates the five
// authoring patterns defined in spec section 4.13.
package codereview

import (
	"embed"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/skills/builtin/frontmatter"
)

//go:embed content
var content embed.FS

// Register walks the embedded content directory and registers each skill as a
// built-in prompt skill on the loader. It panics on embedded content errors
// since these indicate a build-time bug.
func Register(loader *skills.Loader) {
	if err := frontmatter.RegisterAllFull(content, loader); err != nil {
		panic("codereview: embedded content error: " + err.Error())
	}
}
