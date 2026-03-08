package uiuxpromax

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/julianshen/rubichan/internal/skills"
)

//go:embed content
var content embed.FS

const embeddedSkillRoot = "content/ui-ux-pro-max"

// Register materializes the embedded ui-ux-pro-max skill into cacheRoot and
// registers it as a built-in instruction skill with its helper files preserved.
func Register(loader *skills.Loader, cacheRoot string) error {
	skillDir, err := materialize(cacheRoot)
	if err != nil {
		return err
	}

	data, err := fs.ReadFile(content, embeddedSkillRoot+"/SKILL.md")
	if err != nil {
		return fmt.Errorf("read embedded skill: %w", err)
	}

	manifest, body, err := skills.ParseInstructionSkill(data)
	if err != nil {
		return fmt.Errorf("parse embedded skill: %w", err)
	}
	if manifest.Version == "" {
		manifest.Version = "1.0.0"
	}

	loader.RegisterBuiltinDiscovered(skills.DiscoveredSkill{
		Manifest:        manifest,
		Dir:             skillDir,
		RootDir:         filepath.Dir(skillDir),
		InstructionBody: body,
	})
	return nil
}

func materialize(cacheRoot string) (string, error) {
	destRoot := filepath.Join(cacheRoot, "builtin-skills", "ui-ux-pro-max")
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return "", fmt.Errorf("create builtin skill directory: %w", err)
	}

	err := fs.WalkDir(content, embeddedSkillRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(embeddedSkillRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		destPath := filepath.Join(destRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		data, err := fs.ReadFile(content, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0o644)
	})
	if err != nil {
		return "", fmt.Errorf("materialize builtin skill: %w", err)
	}

	return destRoot, nil
}
