package uiuxpromax

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/rubichan/internal/skills"
)

//go:embed content
var content embed.FS

const embeddedSkillRoot = "content/ui-ux-pro-max"
const materializeVersion = "1.0.0"

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
		manifest.Version = materializeVersion
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
	versionFile := filepath.Join(destRoot, ".version")

	if data, err := os.ReadFile(versionFile); err == nil && strings.TrimSpace(string(data)) == materializeVersion {
		return destRoot, nil
	}
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
		return os.WriteFile(destPath, data, 0o644)
	})
	if err != nil {
		return "", fmt.Errorf("materialize builtin skill: %w", err)
	}
	if err := os.WriteFile(versionFile, []byte(materializeVersion), 0o644); err != nil {
		return "", fmt.Errorf("write materialize version: %w", err)
	}

	return destRoot, nil
}
