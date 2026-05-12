package skills

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type BundledContent interface {
	Materialize(cacheDir, skillName string) (string, error)
}

type BundledSkill struct {
	Name        string
	Version     string
	Description string
	Types       []SkillType
	Permissions []Permission
	Triggers    TriggerConfig
	Prompt      PromptConfig
	Content     BundledContent
}

func (bs BundledSkill) ToManifest() *SkillManifest {
	return &SkillManifest{
		Name:        bs.Name,
		Version:     bs.Version,
		Description: bs.Description,
		Types:       bs.Types,
		Permissions: bs.Permissions,
		Triggers:    bs.Triggers,
		Prompt:      bs.Prompt,
	}
}

type InlineContent struct {
	Files map[string]string
}

func (ic *InlineContent) Materialize(cacheDir, skillName string) (string, error) {
	files := make(map[string][]byte, len(ic.Files))
	for name, content := range ic.Files {
		files[name] = []byte(content)
	}
	return writeFileMap(cacheDir, skillName, files)
}

type EmbedContent struct {
	FS     fs.FS
	Prefix string
}

func (ec *EmbedContent) Materialize(cacheDir, skillName string) (string, error) {
	skillDir := filepath.Join(cacheDir, "bundled-skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create bundled skill dir: %w", err)
	}

	// Clear any stale files from previous materialization.
	if err := os.RemoveAll(skillDir); err != nil {
		return "", fmt.Errorf("clear stale bundled skill dir: %w", err)
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create bundled skill dir: %w", err)
	}

	err := fs.WalkDir(ec.FS, ec.Prefix, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk embedded %s: %w", path, walkErr)
		}

		rel := strings.TrimPrefix(path, ec.Prefix)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return nil
		}

		destPath := filepath.Join(skillDir, rel)
		if d.IsDir() {
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return fmt.Errorf("create dir %s: %w", destPath, err)
			}
			return nil
		}

		src, err := ec.FS.Open(path)
		if err != nil {
			return fmt.Errorf("open embedded %s: %w", path, err)
		}
		defer src.Close()

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("create dir for %s: %w", destPath, err)
		}

		dst, err := os.Create(destPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", destPath, err)
		}
		defer dst.Close()

		if _, err := io.Copy(dst, src); err != nil {
			return fmt.Errorf("copy %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("materialize embedded skill: %w", err)
	}

	return skillDir, nil
}

type FileMapContent struct {
	Files map[string][]byte
}

func (fc *FileMapContent) Materialize(cacheDir, skillName string) (string, error) {
	return writeFileMap(cacheDir, skillName, fc.Files)
}

func writeFileMap(cacheDir, skillName string, files map[string][]byte) (string, error) {
	if !filepath.IsLocal(skillName) {
		return "", fmt.Errorf("invalid skill name: %q", skillName)
	}

	skillDir := filepath.Join(cacheDir, "bundled-skills", skillName)
	if err := os.RemoveAll(skillDir); err != nil {
		return "", fmt.Errorf("clear stale bundled skill dir: %w", err)
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create bundled skill dir: %w", err)
	}

	for name, content := range files {
		if !filepath.IsLocal(name) {
			return "", fmt.Errorf("invalid file path in bundle: %q", name)
		}
		path := filepath.Join(skillDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("create dir for %s: %w", name, err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return "", fmt.Errorf("write bundled file %s: %w", name, err)
		}
	}

	return skillDir, nil
}
