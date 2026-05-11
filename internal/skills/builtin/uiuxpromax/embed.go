package uiuxpromax

import (
	"embed"

	"github.com/julianshen/rubichan/internal/skills"
)

//go:embed content
var content embed.FS

const embeddedSkillRoot = "content/ui-ux-pro-max"
const materializeVersion = "1.0.0"

// Register registers the ui-ux-pro-max skill as a bundled skill.
func Register(loader *skills.Loader, cacheRoot string) error {
	bundle := skills.BundledSkill{
		Name:        "ui-ux-pro-max",
		Version:     materializeVersion,
		Description: "UI/UX Pro Max — comprehensive design system skill",
		Types:       []skills.SkillType{skills.SkillTypePrompt},
		Content: &skills.EmbedContent{
			FS:     content,
			Prefix: embeddedSkillRoot,
		},
	}
	loader.RegisterBundled(bundle)
	return nil
}
