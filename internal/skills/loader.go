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
}

// Loader discovers skills from multiple sources: built-in registrations,
// a user-level directory, a project-level directory, and MCP server configs.
type Loader struct {
	userDir    string
	projectDir string
	builtins   map[string]*SkillManifest
	mcpServers []config.MCPServerConfig
}

// NewLoader creates a Loader that scans the given user and project directories.
func NewLoader(userDir, projectDir string) *Loader {
	return &Loader{
		userDir:    userDir,
		projectDir: projectDir,
		builtins:   make(map[string]*SkillManifest),
	}
}

// RegisterBuiltin adds a manifest as a built-in skill. Built-in skills have
// the highest priority and override any user or project skill with the same name.
func (l *Loader) RegisterBuiltin(m *SkillManifest) {
	l.builtins[m.Name] = m
}

// AddMCPServers registers MCP server configs for auto-discovery. Each server
// becomes a synthetic skill with BackendMCP and SourceMCP.
func (l *Loader) AddMCPServers(servers []config.MCPServerConfig) {
	l.mcpServers = servers
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

	// 1. Project skills (lowest directory priority).
	projectSkills, err := scanDir(l.projectDir, SourceProject)
	if err != nil {
		return nil, nil, err
	}
	for _, ds := range projectSkills {
		byName[ds.Manifest.Name] = ds
	}

	// 2. User skills override project skills.
	userSkills, err := scanDir(l.userDir, SourceUser)
	if err != nil {
		return nil, nil, err
	}
	for _, ds := range userSkills {
		byName[ds.Manifest.Name] = ds
	}

	// 3. Built-in skills override everything from directories.
	for name, m := range l.builtins {
		byName[name] = DiscoveredSkill{
			Manifest: m,
			Dir:      "",
			Source:   SourceBuiltin,
		}
	}

	// 3.5. MCP servers from config become synthetic skills.
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

	// 4. Mark explicitly requested skills as SourceInline.
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

// scanDir walks a directory looking for <name>/SKILL.yaml files.
// If the directory does not exist, it returns an empty slice (not an error).
func scanDir(dir string, source Source) ([]DiscoveredSkill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan skills dir %q: %w", dir, err)
	}

	var results []DiscoveredSkill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		yamlPath := filepath.Join(dir, entry.Name(), "SKILL.yaml")
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read skill manifest %q: %w", yamlPath, err)
		}

		manifest, err := ParseManifest(data)
		if err != nil {
			return nil, fmt.Errorf("parse skill %q: %w", entry.Name(), err)
		}

		results = append(results, DiscoveredSkill{
			Manifest: manifest,
			Dir:      filepath.Join(dir, entry.Name()),
			Source:   source,
		})
	}

	return results, nil
}
