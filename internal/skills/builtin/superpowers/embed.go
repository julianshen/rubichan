// Package superpowers embeds the obra/superpowers prompt skills as built-in
// skills. Each skill is a markdown file with YAML frontmatter providing the
// name and description. The body becomes the inline system prompt content.
package superpowers

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
		panic("superpowers: embedded content error: " + err.Error())
	}
}
