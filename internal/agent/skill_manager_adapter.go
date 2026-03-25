package agent

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
)

const defaultRegistryURL = "https://registry.rubichan.dev"

// skillManagerAdapter implements tools.SkillManagerAccess by delegating to
// RegistryClient, Store, and the skill loader for filesystem operations.
type skillManagerAdapter struct {
	registry  *skills.RegistryClient
	store     *store.Store
	skillsDir string
}

func (a *skillManagerAdapter) Search(ctx context.Context, query string) ([]tools.SkillSearchResult, error) {
	results, err := a.registry.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]tools.SkillSearchResult, len(results))
	for i, r := range results {
		out[i] = tools.SkillSearchResult{
			Name:        r.Name,
			Version:     r.Version,
			Description: r.Description,
		}
	}
	return out, nil
}

func (a *skillManagerAdapter) Install(ctx context.Context, source string) (tools.SkillInstallResult, error) {
	if isLocalPathAdapter(source) {
		return a.installFromLocal(source)
	}
	if isGitURL(source) {
		return a.installFromGit(ctx, source)
	}
	return a.installFromRegistry(ctx, source)
}

func (a *skillManagerAdapter) installFromLocal(source string) (tools.SkillInstallResult, error) {
	absSource, err := filepath.Abs(source)
	if err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("resolve path: %w", err)
	}

	manifest, err := loadManifest(absSource)
	if err != nil {
		return tools.SkillInstallResult{}, err
	}

	dest := filepath.Join(a.skillsDir, manifest.Name)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("create skill directory: %w", err)
	}

	if err := copyDirAdapter(absSource, dest); err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("copy skill: %w", err)
	}

	if err := a.store.SaveSkillState(store.SkillInstallState{
		Name:    manifest.Name,
		Version: manifest.Version,
		Source:  dest,
	}); err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("save skill state: %w", err)
	}

	return tools.SkillInstallResult{
		Name:    manifest.Name,
		Version: manifest.Version,
	}, nil
}

func (a *skillManagerAdapter) installFromGit(ctx context.Context, gitURL string) (tools.SkillInstallResult, error) {
	// Clone to a temp directory first, then move to skills dir after validation.
	tmpDir, err := os.MkdirTemp("", "rubichan-skill-git-*")
	if err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cloneDest := filepath.Join(tmpDir, "skill")
	if err := a.registry.InstallFromGit(ctx, gitURL, cloneDest); err != nil {
		return tools.SkillInstallResult{}, err
	}

	manifest, err := loadManifest(cloneDest)
	if err != nil {
		return tools.SkillInstallResult{}, err
	}

	dest := filepath.Join(a.skillsDir, manifest.Name)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("create skill directory: %w", err)
	}

	if err := copyDirAdapter(cloneDest, dest); err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("copy skill: %w", err)
	}

	if err := a.store.SaveSkillState(store.SkillInstallState{
		Name:    manifest.Name,
		Version: manifest.Version,
		Source:  dest,
	}); err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("save skill state: %w", err)
	}

	return tools.SkillInstallResult{
		Name:    manifest.Name,
		Version: manifest.Version,
	}, nil
}

func (a *skillManagerAdapter) installFromRegistry(ctx context.Context, source string) (tools.SkillInstallResult, error) {
	name, version := parseNameVersionAdapter(source)

	if err := validateSkillNameAdapter(name); err != nil {
		return tools.SkillInstallResult{}, err
	}

	// Resolve SemVer ranges.
	if skills.IsSemVerRange(version) {
		available, err := a.registry.ListVersions(ctx, name)
		if err != nil {
			return tools.SkillInstallResult{}, fmt.Errorf("list versions: %w", err)
		}
		resolved, err := skills.ResolveVersion(version, available)
		if err != nil {
			return tools.SkillInstallResult{}, fmt.Errorf("resolve version %q: %w", version, err)
		}
		version = resolved
	}

	dest := filepath.Join(a.skillsDir, name)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("create skill directory: %w", err)
	}

	if err := a.registry.Download(ctx, name, version, dest); err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("download skill: %w", err)
	}

	manifest, err := loadManifest(dest)
	if err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("invalid downloaded manifest: %w", err)
	}

	if err := a.store.SaveSkillState(store.SkillInstallState{
		Name:    manifest.Name,
		Version: manifest.Version,
		Source:  dest,
	}); err != nil {
		return tools.SkillInstallResult{}, fmt.Errorf("save skill state: %w", err)
	}

	return tools.SkillInstallResult{
		Name:    manifest.Name,
		Version: manifest.Version,
	}, nil
}

func (a *skillManagerAdapter) List() ([]tools.SkillListEntry, error) {
	states, err := a.store.ListAllSkillStates()
	if err != nil {
		return nil, err
	}
	entries := make([]tools.SkillListEntry, len(states))
	for i, s := range states {
		entries[i] = tools.SkillListEntry{
			Name:        s.Name,
			Version:     s.Version,
			Source:      s.Source,
			InstalledAt: s.InstalledAt.Format(time.RFC3339),
		}
	}
	return entries, nil
}

func (a *skillManagerAdapter) Remove(name string) error {
	if err := validateSkillNameAdapter(name); err != nil {
		return err
	}

	existing, err := a.store.GetSkillState(name)
	if err != nil {
		return fmt.Errorf("check skill state: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("skill %q is not installed", name)
	}

	// Remove from filesystem.
	skillDir := filepath.Join(a.skillsDir, name)
	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("remove skill directory: %w", err)
	}

	// Remove from store.
	if err := a.store.DeleteSkillState(name); err != nil {
		return fmt.Errorf("delete skill state: %w", err)
	}

	return nil
}

// --- helpers (duplicated from cmd/rubichan/skill.go to avoid import cycle) ---

func isLocalPathAdapter(source string) bool {
	return strings.Contains(source, "/") || strings.HasPrefix(source, ".")
}

func isGitURL(source string) bool {
	return strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "ssh://") ||
		strings.HasPrefix(source, "git@") ||
		strings.HasPrefix(source, "github.com/")
}

func parseNameVersionAdapter(source string) (name, version string) {
	if idx := strings.LastIndex(source, "@"); idx > 0 {
		return source[:idx], source[idx+1:]
	}
	return source, "latest"
}

var validSkillNameAdapterPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func validateSkillNameAdapter(name string) error {
	const maxLen = 128
	if len(name) > maxLen {
		return fmt.Errorf("invalid skill name %q: exceeds maximum length of %d characters", name, maxLen)
	}
	if !validSkillNameAdapterPattern.MatchString(name) {
		return fmt.Errorf("invalid skill name %q: must contain only letters, digits, hyphens, and underscores", name)
	}
	return nil
}

func loadManifest(dir string) (*skills.SkillManifest, error) {
	yamlPath := filepath.Join(dir, "SKILL.yaml")
	data, err := os.ReadFile(yamlPath)
	if err == nil {
		return skills.ParseManifest(data)
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	mdPath := filepath.Join(dir, "SKILL.md")
	mdData, err := os.ReadFile(mdPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no SKILL.yaml or SKILL.md found in %s", dir)
		}
		return nil, fmt.Errorf("read instruction skill: %w", err)
	}

	manifest, _, err := skills.ParseInstructionSkill(mdData)
	return manifest, err
}

func copyDirAdapter(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, relPath)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFileAdapter(path, target)
	})
}

func copyFileAdapter(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode())
}
