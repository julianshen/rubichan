// Package frontmatter parses YAML frontmatter from SKILL.md files used by
// built-in prompt skills and registers them on the skill Loader.
package frontmatter

import (
	"bytes"
	"fmt"
	"io/fs"

	"github.com/julianshen/rubichan/internal/skills"
)

// injectDefaultVersion prepends "version: 1.0.0" to the YAML frontmatter if
// no version field exists. This allows built-in SKILL.md files to omit the
// version field while still passing manifest validation.
func injectDefaultVersion(data []byte) []byte {
	if bytes.Contains(data, []byte("\nversion:")) {
		return data
	}
	// Insert "version: 1.0.0\n" right after the opening "---\n".
	idx := bytes.Index(data, []byte("\n"))
	if idx < 0 {
		return data
	}
	var buf bytes.Buffer
	buf.Write(data[:idx+1])
	buf.WriteString("version: \"1.0.0\"\n")
	buf.Write(data[idx+1:])
	return buf.Bytes()
}

// RegisterAllFull walks an embedded FS for SKILL.md files and registers each
// as a built-in skill using the full instruction skill parser. This supports
// the complete SKILL.md frontmatter schema (commands, agents, triggers, etc.).
func RegisterAllFull(fsys fs.FS, loader *skills.Loader) error {
	return fs.WalkDir(fsys, "content", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || d.Name() != "SKILL.md" {
			return walkErr
		}

		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return readErr
		}

		// Inject a default version if the frontmatter doesn't include one,
		// because ParseInstructionSkill requires version via validateManifest.
		data = injectDefaultVersion(data)

		manifest, body, parseErr := skills.ParseInstructionSkill(data)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", path, parseErr)
		}
		if len(manifest.Triggers.Modes) == 0 {
			manifest.Triggers.Modes = []string{"interactive"}
		}
		manifest.Prompt.SystemPromptFile = body

		loader.RegisterBuiltin(manifest)
		return nil
	})
}
