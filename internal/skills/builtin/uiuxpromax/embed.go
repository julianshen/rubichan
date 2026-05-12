package uiuxpromax

import (
	"embed"
	"io/fs"

	"github.com/julianshen/rubichan/internal/skills"
)

//go:embed content
var content embed.FS

const embeddedSkillRoot = "content/ui-ux-pro-max"
const materializeVersion = "1.0.0"

// Register registers the ui-ux-pro-max skill as a bundled skill.
func Register(loader *skills.Loader, cacheRoot string) error {
	// Parse the embedded SKILL.md to extract manifest and instruction body.
	data, err := fs.ReadFile(content, embeddedSkillRoot+"/SKILL.md")
	if err != nil {
		return err
	}
	manifest, body, err := skills.ParseInstructionSkill(data)
	if err != nil {
		return err
	}
	if manifest.Version == "" {
		manifest.Version = materializeVersion
	}

	bundle := skills.BundledSkill{
		Name:        manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		Types:       manifest.Types,
		Permissions: manifest.Permissions,
		Triggers:    manifest.Triggers,
		Prompt:      manifest.Prompt,
		Content: &skills.EmbedContent{
			FS:     content,
			Prefix: embeddedSkillRoot,
		},
	}
	loader.RegisterBundled(bundle)

	// Also register as a built-in discovered skill so that InstructionBody
	// is available during discovery (for prompt collection).
	loader.RegisterBuiltinDiscovered(skills.DiscoveredSkill{
		Manifest:        manifest,
		Source:          skills.SourceBuiltin,
		InstructionBody: body,
	})
	return nil
}
