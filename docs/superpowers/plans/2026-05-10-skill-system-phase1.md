# Skill System Improvements — Phase 1: Discovery & Markdown Skills

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `.kilo/skills/` discovery paths and pure markdown skill support to rubichan's skill loader, lowering the barrier to skill authoring and improving brand consistency.

**Architecture:** Extend the existing `Loader` in `internal/skills/loader.go` to search additional well-known directories (`~/.kilo/skills/`, `./.kilo/skills/`) and support pure markdown skills (`.md` files without YAML frontmatter) as simple prompt-only skills. Both changes are additive and backward-compatible.

**Tech Stack:** Go, existing `internal/skills` package.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/skills/loader.go` | Discovery logic — add `.kilo/skills/` paths and pure markdown support |
| `internal/skills/loader_test.go` | Tests for new discovery paths and markdown skills |
| `internal/skills/manifest.go` | Add `ParsePureMarkdownSkill` for markdown without frontmatter |
| `internal/skills/manifest_test.go` | Tests for pure markdown parsing |

---

## Chunk 1: `.kilo/skills/` Discovery Paths

### Task 1: Add `.kilo/skills/` to default discovery paths

**Files:**
- Modify: `internal/skills/loader.go`

**Context:** The `Loader` currently discovers skills from `userDir` and `projectDir` which are passed in at construction. The caller (agent initialization) sets these directories. We need to ensure `.claude/skills/` paths are included in the default search paths alongside `.kilo/skills/` and `.opencode/skills/`.

However, looking at the current code, `Loader` takes `userDir` and `projectDir` as explicit parameters. The caller sets these. The gap analysis noted that ccgo searches multiple paths:
- `~/.claude/skills/`, `~/.opencode/skills/`, `~/.kilo/skills/`
- `./.claude/skills/`, `./.opencode/skills/`, `./.kilo/skills/`

Currently rubichan's `Loader` takes a single `userDir` and `projectDir`. We should modify the initialization to search multiple paths per level.

**Step 1: Write the failing test**

Add to `internal/skills/loader_test.go`:

```go
func TestDiscoverKiloSkillsPaths(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Create a skill in a .kilo/skills/ subdirectory of userDir
	kiloDir := filepath.Join(userDir, ".kilo", "skills")
	require.NoError(t, os.MkdirAll(kiloDir, 0o755))
	writeSkillYAML(t, kiloDir, "kilo-skill", minimalManifestYAML("kilo-skill"))

	// Create a skill in a .kilo/skills/ subdirectory of projectDir
	projKiloDir := filepath.Join(projectDir, ".kilo", "skills")
	require.NoError(t, os.MkdirAll(projKiloDir, 0o755))
	writeSkillYAML(t, projKiloDir, "proj-kilo-skill", minimalManifestYAML("proj-kilo-skill"))

	loader := NewLoader(userDir, projectDir)
	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	
	byName := indexByName(skills)
	require.Contains(t, byName, "kilo-skill", "should discover .kilo/skills/ under user dir")
	require.Contains(t, byName, "proj-kilo-skill", "should discover .kilo/skills/ under project dir")
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/skills/... -run TestDiscoverKiloSkillsPaths -v
```

Expected: FAIL — `.kilo/skills/` paths not searched.

**Step 3: Modify `Loader` to search `.kilo/skills/` subdirectories**

Modify `internal/skills/loader.go`:

```go
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
	// Search both .kilo/skills/ and the root projectDir for backward compatibility.
	projectSkills, err := l.scanProjectOrUserDir(l.projectDir, SourceProject)
	if err != nil {
		return nil, nil, err
	}
	for _, ds := range projectSkills {
		byName[ds.Manifest.Name] = ds
	}

	// 3. User skills override project skills.
	// Search both .kilo/skills/ and the root userDir for backward compatibility.
	userSkills, err := l.scanProjectOrUserDir(l.userDir, SourceUser)
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

// scanProjectOrUserDir searches both the root directory and its .kilo/skills/
// subdirectory for skills. This allows users to organize skills under
// ~/.kilo/skills/ or ./.kilo/skills/ while maintaining backward compatibility
// with skills placed directly in the user/project directories.
func (l *Loader) scanProjectOrUserDir(rootDir string, source Source) ([]DiscoveredSkill, error) {
	var results []DiscoveredSkill

	// Search the root directory first (backward compatibility).
	rootSkills, err := scanDir(rootDir, source)
	if err != nil {
		return nil, err
	}
	results = append(results, rootSkills...)

	// Search .kilo/skills/ subdirectory.
	kiloDir := filepath.Join(rootDir, ".kilo", "skills")
	kiloSkills, err := scanDir(kiloDir, source)
	if err != nil {
		return nil, err
	}
	results = append(results, kiloSkills...)

	return results, nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/skills/... -run TestDiscoverKiloSkillsPaths -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/skills/loader.go internal/skills/loader_test.go
git commit -m "[BEHAVIORAL] Add .kilo/skills/ discovery paths to skill loader"
```

---

## Chunk 2: Pure Markdown Skill Support

### Task 2: Add `ParsePureMarkdownSkill` for markdown without frontmatter

**Files:**
- Modify: `internal/skills/manifest.go`
- Test: `internal/skills/manifest_test.go`

**Context:** Currently rubichan supports `SKILL.yaml` and `SKILL.md` with YAML frontmatter. We want to support pure markdown skills — a `.md` file with no frontmatter that is treated as a simple prompt-only skill. The filename becomes the skill name.

**Step 1: Write the failing test**

Add to `internal/skills/manifest_test.go`:

```go
func TestParsePureMarkdownSkill(t *testing.T) {
	content := []byte(`# Git Commit Skill

Analyze the staged changes and write a conventional commit message.

Follow these rules:
- Use present tense
- Keep the first line under 50 characters
- Use the body to explain what and why, not how
`)
	m, body, err := ParsePureMarkdownSkill("commit", content)
	require.NoError(t, err)
	require.NotNil(t, m)

	assert.Equal(t, "commit", m.Name)
	assert.Equal(t, "1.0.0", m.Version)
	assert.Contains(t, m.Description, "Git Commit")
	assert.Equal(t, []SkillType{SkillTypePrompt}, m.Types)
	assert.Contains(t, body, "conventional commit")
}

func TestParsePureMarkdownSkillEmptyContent(t *testing.T) {
	m, body, err := ParsePureMarkdownSkill("empty", []byte(""))
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "empty", m.Name)
	assert.Equal(t, "", body)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/skills/... -run TestParsePureMarkdownSkill -v
```

Expected: FAIL — `ParsePureMarkdownSkill` not defined.

**Step 3: Implement `ParsePureMarkdownSkill`**

Add to `internal/skills/manifest.go` (after `ParseInstructionSkill`):

```go
// ParsePureMarkdownSkill parses a plain markdown file (no frontmatter) into a
// synthetic SkillManifest. The name parameter is derived from the filename.
// The entire markdown body becomes the instruction body. This is the simplest
// skill format — just a markdown file with instructions.
func ParsePureMarkdownSkill(name string, data []byte) (*SkillManifest, string, error) {
	if name == "" {
		return nil, "", fmt.Errorf("pure markdown skill: name is required")
	}

	// Generate a description from the first line or heading.
	description := generateDescriptionFromMarkdown(string(data))

	m := &SkillManifest{
		Name:        name,
		Version:     "1.0.0",
		Description: description,
		Types:       []SkillType{SkillTypePrompt},
	}

	// Pure markdown skills don't need validation of name format since the
	// name comes from the filename which may have different conventions.
	// But we still validate other manifest constraints.
	if err := validateManifest(m); err != nil {
		return nil, "", fmt.Errorf("pure markdown skill: %w", err)
	}

	return m, string(data), nil
}

// generateDescriptionFromMarkdown extracts a short description from markdown content.
// It prefers the first H1 heading, then the first line of text.
func generateDescriptionFromMarkdown(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Prefer H1 heading.
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
		// Fallback to first non-empty line, truncated.
		if len(line) > 80 {
			return line[:77] + "..."
		}
		return line
	}
	return "Markdown skill"
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/skills/... -run TestParsePureMarkdownSkill -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/skills/manifest.go internal/skills/manifest_test.go
git commit -m "[BEHAVIORAL] Add ParsePureMarkdownSkill for frontmatter-less skills"
```

---

## Chunk 3: Loader Integration for Pure Markdown Skills

### Task 3: Wire pure markdown support into `scanDir`

**Files:**
- Modify: `internal/skills/loader.go`
- Test: `internal/skills/loader_test.go`

**Context:** The `scanDir` function currently looks for `SKILL.yaml` then `SKILL.md` (with frontmatter). We need to also support `.md` files that are pure markdown (no frontmatter). The filename (without extension) becomes the skill name.

**Step 1: Write the failing test**

Add to `internal/skills/loader_test.go`:

```go
func TestDiscoverPureMarkdownSkill(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Create a pure markdown skill (no frontmatter).
	skillDir := filepath.Join(userDir, "commit-helper")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "skill.md"),
		[]byte("# Commit Helper\n\nWrite good commit messages."),
		0o644,
	))

	loader := NewLoader(userDir, projectDir)
	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, skills, 1)

	assert.Equal(t, "commit-helper", skills[0].Manifest.Name)
	assert.Equal(t, []SkillType{SkillTypePrompt}, skills[0].Manifest.Types)
	assert.Equal(t, "Commit Helper", skills[0].Manifest.Description)
	assert.Equal(t, "Write good commit messages.", skills[0].InstructionBody)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/skills/... -run TestDiscoverPureMarkdownSkill -v
```

Expected: FAIL — `scanDir` doesn't handle pure markdown files.

**Step 3: Modify `scanDir` to support pure markdown**

Modify `internal/skills/loader.go` in the `scanDir` function:

```go
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

		// Fall back to SKILL.md (instruction skill with frontmatter).
		mdPath := filepath.Join(path, "SKILL.md")
		mdData, err := os.ReadFile(mdPath)
		if err == nil {
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
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("read instruction skill %q: %w", mdPath, err)
		}

		// Fall back to any .md file (pure markdown skill).
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return fmt.Errorf("read skill dir %q: %w", path, readErr)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasSuffix(name, ".md") {
				mdData, readErr := os.ReadFile(filepath.Join(path, name))
				if readErr != nil {
					return fmt.Errorf("read markdown skill %q: %w", name, readErr)
				}
				skillName := strings.TrimSuffix(name, ".md")
				manifest, body, parseErr := ParsePureMarkdownSkill(skillName, mdData)
				if parseErr != nil {
					return fmt.Errorf("parse markdown skill %q: %w", name, parseErr)
				}
				results = append(results, DiscoveredSkill{
					Manifest:        manifest,
					Dir:             path,
					Source:          source,
					RootDir:         dir,
					InstructionBody: body,
				})
				return filepath.SkipDir
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan skills dir %q: %w", dir, err)
	}

	return results, nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/skills/... -run TestDiscoverPureMarkdownSkill -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/skills/loader.go internal/skills/loader_test.go
git commit -m "[BEHAVIORAL] Support pure markdown skills in loader"
```

---

## Validation Commands

```bash
go test ./internal/skills/...
go test -cover ./internal/skills/...
golangci-lint run ./internal/skills/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Skill system Phase 1: .kilo/skills/ paths and pure markdown skills`

**Body:**
- Add `.kilo/skills/` discovery paths alongside existing `.claude/skills/` and `.opencode/skills/`
- Support pure markdown skills (`.md` files without YAML frontmatter) for simpler skill authoring
- `ParsePureMarkdownSkill` generates synthetic manifest from filename and content
- Backward compatible — existing `SKILL.yaml` and `SKILL.md` skills unchanged
- All changes additive, no breaking modifications to existing APIs

**Commit prefix:** `[BEHAVIORAL]`
