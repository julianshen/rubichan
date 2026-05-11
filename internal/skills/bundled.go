package skills

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// BundledContent is the interface for skill content that can be materialized
// to disk on demand. Implementations include embedded FS, inline strings, and
// file maps.
type BundledContent interface {
	// Materialize extracts the bundled content to the given cache directory.
	// The skillName is used to create a unique subdirectory. Returns the path
	// to the materialized skill directory.
	Materialize(cacheDir, skillName string) (string, error)
}

// BundledSkill represents a skill that is registered in code but whose content
// is loaded lazily. This enables built-in skills to ship as embedded resources
// without keeping them in memory until needed.
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

// InlineContent implements BundledContent using an in-memory map of filenames
// to content strings. This is the simplest bundled content type, suitable for
// small skills with a few files.
type InlineContent struct {
	Files map[string]string
}

// Materialize writes all files to a subdirectory under cacheDir.
func (ic *InlineContent) Materialize(cacheDir, skillName string) (string, error) {
	skillDir := filepath.Join(cacheDir, "bundled-skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create bundled skill dir: %w", err)
	}

	for name, content := range ic.Files {
		path := filepath.Join(skillDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("create dir for %s: %w", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write bundled file %s: %w", name, err)
		}
	}

	return skillDir, nil
}

// EmbedContent implements BundledContent using an embedded fs.FS. This is
// suitable for skills that ship with the binary via //go:embed directives.
type EmbedContent struct {
	FS     fs.FS
	Prefix string // Path prefix within the FS to the skill content
}

// Materialize walks the embedded FS and copies all files to cacheDir.
func (ec *EmbedContent) Materialize(cacheDir, skillName string) (string, error) {
	skillDir := filepath.Join(cacheDir, "bundled-skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create bundled skill dir: %w", err)
	}

	err := fs.WalkDir(ec.FS, ec.Prefix, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(ec.Prefix, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		destPath := filepath.Join(skillDir, rel)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		src, err := ec.FS.Open(path)
		if err != nil {
			return fmt.Errorf("open embedded %s: %w", path, err)
		}
		defer src.Close()

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
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

// FileMapContent implements BundledContent using a map of filenames to byte
// slices. Similar to InlineContent but for binary data.
type FileMapContent struct {
	Files map[string][]byte
}

// Materialize writes all files to a subdirectory under cacheDir.
func (fc *FileMapContent) Materialize(cacheDir, skillName string) (string, error) {
	skillDir := filepath.Join(cacheDir, "bundled-skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create bundled skill dir: %w", err)
	}

	for name, content := range fc.Files {
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
