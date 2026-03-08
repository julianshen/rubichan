package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/julianshen/rubichan/internal/config"
)

// Source indicates where a discovered skill was loaded from.
type Source string

const (
	// SourceConfigured is a skill found in an extra configured skills directory.
	SourceConfigured Source = "configured"
	// SourceBuiltin is a skill registered in code via RegisterBuiltin.
	SourceBuiltin Source = "builtin"
	// SourceUser is a skill found in the user-level skills directory.
	SourceUser Source = "user"
	// SourceProject is a skill found in the project-level skills directory.
	SourceProject Source = "project"
	// SourceInline is a skill explicitly requested via the --skills flag.
	SourceInline Source = "inline"
	// SourceMCP is a skill auto-discovered from a configured MCP server.
	SourceMCP Source = "mcp"
)

// DiscoveredSkill represents a skill that was found by the loader, together
// with its parsed manifest, directory on disk, and the source it came from.
type DiscoveredSkill struct {
	Manifest *SkillManifest
	Dir      string
	Source   Source
	RootDir  string

	// InstructionBody holds the markdown body for instruction skills (SKILL.md).
	// Empty for regular SKILL.yaml skills.
	InstructionBody string
}

// Loader discovers skills from multiple sources: built-in registrations,
// a user-level directory, a project-level directory, and MCP server configs.
type Loader struct {
	userDir    string
	projectDir string
	skillDirs  []string
	builtins   map[string]DiscoveredSkill
	mcpServers []config.MCPServerConfig
}

// NewLoader creates a Loader that scans the given user and project directories.
func NewLoader(userDir, projectDir string) *Loader {
	return &Loader{
		userDir:    userDir,
		projectDir: projectDir,
		builtins:   make(map[string]DiscoveredSkill),
	}
}

// RegisterBuiltin adds a manifest as a built-in skill. Built-in skills have
// the highest priority and override any user or project skill with the same name.
func (l *Loader) RegisterBuiltin(m *SkillManifest) {
	l.RegisterBuiltinDiscovered(DiscoveredSkill{
		Manifest: m,
	})
}

// RegisterBuiltinDiscovered adds a fully-populated built-in skill, preserving
// any associated on-disk directory and instruction body.
func (l *Loader) RegisterBuiltinDiscovered(ds DiscoveredSkill) {
	ds.Source = SourceBuiltin
	l.builtins[ds.Manifest.Name] = ds
}

// AddMCPServers registers MCP server configs for auto-discovery. Each server
// becomes a synthetic skill with BackendMCP and SourceMCP.
func (l *Loader) AddMCPServers(servers []config.MCPServerConfig) {
	l.mcpServers = servers
}

// AddSkillDirs registers extra configured skill roots. These are scanned
// recursively and have lower precedence than project and user skill dirs.
func (l *Loader) AddSkillDirs(dirs []string) {
	l.skillDirs = append(l.skillDirs[:0], dirs...)
}

// Discover finds all available skills and returns them in a deduplicated list.
// The explicit parameter lists skill names explicitly requested (e.g. via --skills flag);
// these are marked as SourceInline if found.
//
// It returns:
//   - the list of discovered skills (deduplicated by name, highest priority wins)
//   - a list of warning strings (e.g. missing optional dependencies)
//   - an error if a required dependency is missing or a manifest can't be parsed
func (l *Loader) Discover(explicit []string) ([]DiscoveredSkill, []string, error) {
	explicitSet := make(map[string]bool, len(explicit))
	for _, name := range explicit {
		explicitSet[name] = true
	}

	// Collect skills from all directory sources.
	// We build a map keyed by skill name; higher-priority sources overwrite lower ones.
	byName := make(map[string]DiscoveredSkill)

	// 1. Configured skill roots (lowest directory priority).
	for _, dir := range l.skillDirs {
		configuredSkills, err := scanDir(dir, SourceConfigured)
		if err != nil {
			return nil, nil, err
		}
		for _, ds := range configuredSkills {
			if _, exists := byName[ds.Manifest.Name]; !exists {
				byName[ds.Manifest.Name] = ds
			}
		}
	}

	// 2. Project skills override configured skill roots.
	projectSkills, err := scanDir(l.projectDir, SourceProject)
	if err != nil {
		return nil, nil, err
	}
	for _, ds := range projectSkills {
		byName[ds.Manifest.Name] = ds
	}

	// 3. User skills override project skills.
	userSkills, err := scanDir(l.userDir, SourceUser)
	if err != nil {
		return nil, nil, err
	}
	for _, ds := range userSkills {
		byName[ds.Manifest.Name] = ds
	}

	// 4. Built-in skills override everything from directories.
	for name, ds := range l.builtins {
		byName[name] = ds
	}

	// 4.5. MCP servers from config become synthetic skills.
	for _, srv := range l.mcpServers {
		name := "mcp-" + srv.Name
		// Skip if a higher-priority skill already has this name.
		if _, exists := byName[name]; exists {
			continue
		}
		// Only stdio transport spawns a child process — grant shell:exec accordingly.
		var perms []Permission
		if srv.Transport == "stdio" {
			perms = []Permission{PermShellExec}
		}
		byName[name] = DiscoveredSkill{
			Manifest: &SkillManifest{
				Name:        name,
				Version:     "0.0.0",
				Description: fmt.Sprintf("MCP server: %s", srv.Name),
				Types:       []SkillType{SkillTypeTool},
				Permissions: perms,
				Implementation: ImplementationConfig{
					Backend:      BackendMCP,
					MCPTransport: srv.Transport,
					MCPCommand:   srv.Command,
					MCPArgs:      srv.Args,
					MCPURL:       srv.URL,
				},
			},
			Dir:    "",
			Source: SourceMCP,
		}
	}

	// 5. Mark explicitly requested skills as SourceInline.
	for name := range explicitSet {
		ds, ok := byName[name]
		if !ok {
			return nil, nil, fmt.Errorf("explicit skill %q not found in any source", name)
		}
		ds.Source = SourceInline
		byName[name] = ds
	}

	// Build sorted result slice for deterministic output.
	result := make([]DiscoveredSkill, 0, len(byName))
	for _, ds := range byName {
		result = append(result, ds)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Manifest.Name < result[j].Manifest.Name
	})

	// Validate dependencies.
	nameSet := make(map[string]bool, len(result))
	for _, ds := range result {
		nameSet[ds.Manifest.Name] = true
	}

	var warnings []string
	for _, ds := range result {
		for _, dep := range ds.Manifest.Dependencies {
			if nameSet[dep.Name] {
				continue
			}
			if dep.Optional {
				warnings = append(warnings, fmt.Sprintf(
					"skill %q: optional dependency %q not found",
					ds.Manifest.Name, dep.Name,
				))
			} else {
				return nil, nil, fmt.Errorf(
					"skill %q: required dependency %q not found",
					ds.Manifest.Name, dep.Name,
				)
			}
		}
	}

	return result, warnings, nil
}

// scanDir walks a directory tree recursively looking for directories that
// contain SKILL.yaml files (and SKILL.md instruction skills as a fallback).
// If a directory contains a skill manifest, it is treated as a skill root and
// its descendants are not scanned for nested skills. If the directory does not
// exist, it returns an empty slice (not an error).
func scanDir(dir string, source Source) ([]DiscoveredSkill, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan skills dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, nil
	}

	var results []DiscoveredSkill
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		if path == dir {
			return nil
		}

		// Try SKILL.yaml first.
		yamlPath := filepath.Join(path, "SKILL.yaml")
		data, err := os.ReadFile(yamlPath)
		if err == nil {
			manifest, parseErr := ParseManifest(data)
			if parseErr != nil {
				return fmt.Errorf("parse skill %q: %w", filepath.Base(path), parseErr)
			}
			results = append(results, DiscoveredSkill{
				Manifest: manifest,
				Dir:      path,
				Source:   source,
				RootDir:  dir,
			})
			return filepath.SkipDir
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("read skill manifest %q: %w", yamlPath, err)
		}

		// Fall back to SKILL.md (instruction skill).
		mdPath := filepath.Join(path, "SKILL.md")
		mdData, err := os.ReadFile(mdPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("read instruction skill %q: %w", mdPath, err)
		}

		manifest, body, parseErr := ParseInstructionSkill(mdData)
		if parseErr != nil {
			return fmt.Errorf("parse instruction skill %q: %w", filepath.Base(path), parseErr)
		}
		results = append(results, DiscoveredSkill{
			Manifest:        manifest,
			Dir:             path,
			Source:          source,
			RootDir:         dir,
			InstructionBody: body,
		})
		return filepath.SkipDir
	})
	if err != nil {
		return nil, fmt.Errorf("scan skills dir %q: %w", dir, err)
	}

	return results, nil
}
