package skills

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// BundledContent is the interface for lazy materialization of bundled skill
// files into a cache directory at activation time.
type BundledContent interface {
	Materialize(cacheDir, skillName string) (string, error)
}

// BundledSkill represents a skill whose content is embedded in the binary
// and materialized on demand when activated.
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

// ToManifest converts a BundledSkill into a SkillManifest for discovery.
func (bs BundledSkill) ToManifest() SkillManifest {
	return SkillManifest{
		Name:        bs.Name,
		Version:     bs.Version,
		Description: bs.Description,
		Types:       bs.Types,
		Permissions: bs.Permissions,
		Triggers:    bs.Triggers,
		Prompt:      bs.Prompt,
	}
}

// InlineContent provides bundled skill files as a map of filename to content.
type InlineContent struct {
	Files map[string]string
}

// Materialize writes the inline files to the cache directory.
func (ic *InlineContent) Materialize(cacheDir, skillName string) (string, error) {
	return writeFileMap(cacheDir, skillName, ic.Files)
}

// EmbedContent provides bundled skill files via an embedded fs.FS.
type EmbedContent struct {
	FS     fs.FS
	Prefix string
}

// Materialize walks the embedded FS and writes all files to the cache directory.
func (ec *EmbedContent) Materialize(cacheDir, skillName string) (string, error) {
	skillDir, err := prepareSkillDir(cacheDir, skillName)
	if err != nil {
		return "", err
	}

	err = fs.WalkDir(ec.FS, ec.Prefix, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk embedded %s: %w", path, walkErr)
		}

		rel, err := filepath.Rel(ec.Prefix, path)
		if err != nil {
			return fmt.Errorf("rel path for %s: %w", path, err)
		}
		if rel == "." {
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

// FileMapContent provides bundled skill files as a map of filename to raw bytes.
type FileMapContent struct {
	Files map[string][]byte
}

// Materialize writes the file map to the cache directory.
func (fc *FileMapContent) Materialize(cacheDir, skillName string) (string, error) {
	return writeByteMap(cacheDir, skillName, fc.Files)
}

// prepareSkillDir validates the skill name, removes any stale cached directory,
// and creates a fresh one. It returns the skill directory path.
func prepareSkillDir(cacheDir, skillName string) (string, error) {
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
	return skillDir, nil
}

// writeFileMap writes a map of string content to files under the skill directory.
func writeFileMap(cacheDir, skillName string, files map[string]string) (string, error) {
	skillDir, err := prepareSkillDir(cacheDir, skillName)
	if err != nil {
		return "", err
	}

	for name, content := range files {
		if !filepath.IsLocal(name) {
			return "", fmt.Errorf("invalid file path in bundle: %q", name)
		}
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

// writeByteMap writes a map of byte content to files under the skill directory.
func writeByteMap(cacheDir, skillName string, files map[string][]byte) (string, error) {
	skillDir, err := prepareSkillDir(cacheDir, skillName)
	if err != nil {
		return "", err
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
