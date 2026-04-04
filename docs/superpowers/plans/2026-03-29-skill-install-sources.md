# Skill Install Sources & Discovery Enhancement — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add git/npm/GitHub install sources, category+tags manifest fields, enhanced discovery filters, and a skill update command to rubichan's skill system.

**Architecture:** Extend the existing `isLocalPath()` source resolution in `skill.go` with prefix-based routing (`git:`, `github:`, `npm:`). Add `Category` and `Tags` fields to `SkillManifest`. Extend the SQLite `skill_state` table with source metadata columns. Add `skill update` subcommand that re-fetches from recorded source.

**Tech Stack:** Go, cobra CLI, SQLite (`modernc.org/sqlite`), `os/exec` for git/npm, existing `internal/skills/` and `internal/store/` packages.

**Spec:** `docs/superpowers/specs/2026-03-29-skill-install-sources-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `cmd/rubichan/skill.go` | Modify: source resolution, install handlers, list/info/search formatting, new `skill update` command |
| `internal/skills/manifest.go` | Modify: add `Category` and `Tags` fields to `SkillManifest` |
| `internal/skills/manifest_test.go` | Modify: validation tests for new fields |
| `internal/store/store.go` | Modify: extend `skill_state` schema and `SkillInstallState` struct |
| `internal/store/store_test.go` | Modify: test new schema columns |

---

### Task 1: Add Category and Tags to SkillManifest

**Files:**
- Modify: `internal/skills/manifest.go:118-141`
- Modify: `internal/skills/manifest_test.go`

- [ ] **Step 1: Write failing test for category validation**

```go
// internal/skills/manifest_test.go — add test
func TestManifestValidCategory(t *testing.T) {
	m := SkillManifest{
		Name:     "test-skill",
		Version:  "1.0.0",
		Types:    []SkillType{SkillTypePrompt},
		Category: "development",
	}
	err := m.Validate()
	assert.NoError(t, err)
}

func TestManifestInvalidCategory(t *testing.T) {
	m := SkillManifest{
		Name:     "test-skill",
		Version:  "1.0.0",
		Types:    []SkillType{SkillTypePrompt},
		Category: "invalid-category",
	}
	err := m.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid category")
}

func TestManifestEmptyCategoryAllowed(t *testing.T) {
	m := SkillManifest{
		Name:    "test-skill",
		Version: "1.0.0",
		Types:   []SkillType{SkillTypePrompt},
	}
	err := m.Validate()
	assert.NoError(t, err)
}

func TestManifestTags(t *testing.T) {
	m := SkillManifest{
		Name:     "test-skill",
		Version:  "1.0.0",
		Types:    []SkillType{SkillTypePrompt},
		Category: "development",
		Tags:     []string{"golang", "code-review"},
	}
	err := m.Validate()
	assert.NoError(t, err)
	assert.Equal(t, []string{"golang", "code-review"}, m.Tags)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/... -run "TestManifestValidCategory|TestManifestInvalidCategory|TestManifestEmptyCategory|TestManifestTags" -v`
Expected: FAIL — `Category` field undefined

- [ ] **Step 3: Add fields to SkillManifest**

In `internal/skills/manifest.go`, add to the `SkillManifest` struct (after `Homepage`):

```go
Category string   `yaml:"category"` // development, productivity, learning, security, testing
Tags     []string `yaml:"tags"`     // free-form discovery tags
```

Add valid categories as a package-level variable:

```go
var validCategories = map[string]bool{
	"development":  true,
	"productivity": true,
	"learning":     true,
	"security":     true,
	"testing":      true,
}
```

In the `Validate()` method, add category validation (after existing field checks):

```go
if m.Category != "" {
	if !validCategories[strings.ToLower(m.Category)] {
		return fmt.Errorf("invalid category %q: must be one of development, productivity, learning, security, testing", m.Category)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/skills/... -run "TestManifestValidCategory|TestManifestInvalidCategory|TestManifestEmptyCategory|TestManifestTags" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/skills/manifest.go internal/skills/manifest_test.go
git commit -m "[BEHAVIORAL] Add Category and Tags fields to SkillManifest with validation"
```

---

### Task 2: Extend Skills Database Schema

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

- [ ] **Step 1: Write failing test for new columns**

```go
// internal/store/store_test.go — add test
func TestSkillStatePersistsSourceMetadata(t *testing.T) {
	db := newTestStore(t)
	state := SkillInstallState{
		Name:       "code-reviewer",
		Version:    "1.0.0",
		Source:     "github:user/code-reviewer@v1.0.0",
		SourceType: "github",
		SourceURL:  "https://github.com/user/code-reviewer.git",
		SourceRef:  "v1.0.0",
		Category:   "development",
		Tags:       "golang,code-review",
	}
	require.NoError(t, db.SaveSkillState(state))

	got, err := db.GetSkillState("code-reviewer")
	require.NoError(t, err)
	assert.Equal(t, "github", got.SourceType)
	assert.Equal(t, "https://github.com/user/code-reviewer.git", got.SourceURL)
	assert.Equal(t, "v1.0.0", got.SourceRef)
	assert.Equal(t, "development", got.Category)
	assert.Equal(t, "golang,code-review", got.Tags)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/... -run TestSkillStatePersistsSourceMetadata -v`
Expected: FAIL — `SourceType` field undefined

- [ ] **Step 3: Extend SkillInstallState and schema**

Add fields to `SkillInstallState` struct:

```go
type SkillInstallState struct {
	Name        string
	Version     string
	Source      string
	SourceType  string    // "local", "git", "github", "npm", "registry"
	SourceURL   string    // original URL for git/npm
	SourceRef   string    // tag/branch/version pinned at install
	Category    string    // from manifest
	Tags        string    // comma-separated from manifest
	InstalledAt time.Time
}
```

Add migration in the `ensureSchema()` function (the DB auto-migrates):

```go
// After existing skill_state CREATE TABLE, add migration:
ALTER TABLE skill_state ADD COLUMN source_type TEXT DEFAULT 'local';
ALTER TABLE skill_state ADD COLUMN source_url TEXT DEFAULT '';
ALTER TABLE skill_state ADD COLUMN source_ref TEXT DEFAULT '';
ALTER TABLE skill_state ADD COLUMN category TEXT DEFAULT '';
ALTER TABLE skill_state ADD COLUMN tags TEXT DEFAULT '';
```

Update `SaveSkillState()` and `GetSkillState()` to read/write the new columns.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/... -run TestSkillStatePersistsSourceMetadata -v`
Expected: PASS

- [ ] **Step 5: Run full store tests**

Run: `go test ./internal/store/... -count=1`
Expected: PASS (no regressions)

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "[BEHAVIORAL] Extend skill_state schema with source metadata and category columns"
```

---

### Task 3: Git and GitHub Install Sources

**Files:**
- Modify: `cmd/rubichan/skill.go`

The existing `InstallFromGit()` in `internal/skills/registry.go` handles git cloning. We need to wire it into the install command and add GitHub shorthand expansion.

- [ ] **Step 1: Write failing test for source resolution**

```go
// cmd/rubichan/skill_test.go (or main_test.go, wherever skill tests live)
func TestParseInstallSource(t *testing.T) {
	tests := []struct {
		input      string
		sourceType string
		url        string
		ref        string
	}{
		{"./local-skill", "local", "./local-skill", ""},
		{"/abs/path/skill", "local", "/abs/path/skill", ""},
		{"git:https://github.com/user/skill.git", "git", "https://github.com/user/skill.git", ""},
		{"git:https://github.com/user/skill.git@v1.0", "git", "https://github.com/user/skill.git", "v1.0"},
		{"github:user/skill", "github", "https://github.com/user/skill.git", ""},
		{"github:user/skill@v2.0", "github", "https://github.com/user/skill.git", "v2.0"},
		{"npm:rubichan-skill-review", "npm", "rubichan-skill-review", ""},
		{"npm:rubichan-skill-review@1.0.0", "npm", "rubichan-skill-review", "1.0.0"},
		{"my-skill", "registry", "my-skill", ""},
		{"my-skill@1.0.0", "registry", "my-skill", "1.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			src := parseInstallSource(tt.input)
			assert.Equal(t, tt.sourceType, src.Type)
			assert.Equal(t, tt.url, src.URL)
			assert.Equal(t, tt.ref, src.Ref)
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rubichan/... -run TestParseInstallSource -v`
Expected: FAIL — `parseInstallSource` undefined

- [ ] **Step 3: Implement parseInstallSource**

```go
type installSource struct {
	Type string // "local", "git", "github", "npm", "registry"
	URL  string // path, git URL, npm package name, or registry name
	Ref  string // version/tag/branch (empty = latest/default)
}

func parseInstallSource(source string) installSource {
	switch {
	case strings.HasPrefix(source, "git:"):
		raw := strings.TrimPrefix(source, "git:")
		url, ref := splitAtRef(raw)
		return installSource{Type: "git", URL: url, Ref: ref}

	case strings.HasPrefix(source, "github:"):
		raw := strings.TrimPrefix(source, "github:")
		name, ref := splitAtRef(raw)
		url := "https://github.com/" + name + ".git"
		return installSource{Type: "github", URL: url, Ref: ref}

	case strings.HasPrefix(source, "npm:"):
		raw := strings.TrimPrefix(source, "npm:")
		pkg, ver := splitAtRef(raw)
		return installSource{Type: "npm", URL: pkg, Ref: ver}

	case isLocalPath(source):
		return installSource{Type: "local", URL: source}

	default:
		name, ver := splitAtRef(source)
		return installSource{Type: "registry", URL: name, Ref: ver}
	}
}

func splitAtRef(s string) (string, string) {
	// Split at last "@" that's not part of the URL scheme.
	// "git@github.com:user/repo" should not split at the first @.
	idx := strings.LastIndex(s, "@")
	if idx <= 0 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/rubichan/... -run TestParseInstallSource -v`
Expected: PASS

- [ ] **Step 5: Wire git install into skill install command**

Update the `skillInstallCmd` RunE handler to use `parseInstallSource` and dispatch by type:

```go
RunE: func(cmd *cobra.Command, args []string) error {
	source := args[0]
	src := parseInstallSource(source)

	switch src.Type {
	case "local":
		return installFromLocal(cmd, src.URL, skillsDir, storePath)
	case "git", "github":
		return installFromGit(cmd, src, skillsDir, storePath)
	case "npm":
		return installFromNpm(cmd, src, skillsDir, storePath)
	default:
		return installFromRegistry(cmd, source, skillsDir, storePath)
	}
}
```

Implement `installFromGit`:

```go
func installFromGit(cmd *cobra.Command, src installSource, skillsDir, storePath string) error {
	tmpDir, err := os.MkdirTemp("", "rubichan-git-install-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cloneDir := filepath.Join(tmpDir, "repo")
	gitArgs := []string{"clone", "--depth", "1"}
	if src.Ref != "" {
		gitArgs = append(gitArgs, "--branch", src.Ref)
	}
	gitArgs = append(gitArgs, "--", src.URL, cloneDir)

	gitCmd := exec.Command("git", gitArgs...)
	if out, err := gitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %s: %w", strings.TrimSpace(string(out)), err)
	}

	manifest, err := loadSkillManifest(cloneDir)
	if err != nil {
		return fmt.Errorf("reading skill manifest: %w", err)
	}

	dest := filepath.Join(skillsDir, manifest.Name)
	if err := copyDir(cloneDir, dest); err != nil {
		return fmt.Errorf("copying skill: %w", err)
	}

	// Record source metadata.
	ref := src.Ref
	if ref == "" {
		ref = "HEAD"
	}
	sourceLabel := src.Type + ":" + src.URL
	if src.Ref != "" {
		sourceLabel += "@" + src.Ref
	}

	store, err := openSkillStore(storePath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.SaveSkillState(storemod.SkillInstallState{
		Name:       manifest.Name,
		Version:    manifest.Version,
		Source:     sourceLabel,
		SourceType: src.Type,
		SourceURL:  src.URL,
		SourceRef:  ref,
		Category:   manifest.Category,
		Tags:       strings.Join(manifest.Tags, ","),
	}); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	cmd.Printf("Installed skill %q (v%s) from %s\n", manifest.Name, manifest.Version, sourceLabel)
	return nil
}
```

- [ ] **Step 6: Run build and existing tests**

Run: `go build ./cmd/rubichan && go test ./cmd/rubichan/... -count=1 -timeout 60s 2>&1 | tail -5`
Expected: BUILD OK, PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/rubichan/skill.go cmd/rubichan/skill_test.go
git commit -m "[BEHAVIORAL] Add git: and github: install sources for skill install"
```

---

### Task 4: npm Install Source

**Files:**
- Modify: `cmd/rubichan/skill.go`

- [ ] **Step 1: Write failing test for npm install**

```go
func TestInstallFromNpmRequiresNpm(t *testing.T) {
	// Mock by setting PATH to empty — npm won't be found.
	src := installSource{Type: "npm", URL: "nonexistent-package", Ref: "1.0.0"}
	err := installFromNpm(nil, src, t.TempDir(), filepath.Join(t.TempDir(), "skills.db"))
	assert.Error(t, err)
	// Should fail either with "npm not found" or "npm pack" error.
}
```

- [ ] **Step 2: Implement installFromNpm**

```go
func installFromNpm(cmd *cobra.Command, src installSource, skillsDir, storePath string) error {
	if _, err := exec.LookPath("npm"); err != nil {
		return fmt.Errorf("npm not found — install Node.js to use npm: sources")
	}

	tmpDir, err := os.MkdirTemp("", "rubichan-npm-install-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	pkg := src.URL
	if src.Ref != "" {
		pkg += "@" + src.Ref
	}

	// npm pack downloads the tarball.
	npmCmd := exec.Command("npm", "pack", pkg, "--pack-destination", tmpDir)
	npmCmd.Dir = tmpDir
	out, err := npmCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("npm pack %s: %s: %w", pkg, strings.TrimSpace(string(out)), err)
	}

	// Find the downloaded tarball.
	tarballs, _ := filepath.Glob(filepath.Join(tmpDir, "*.tgz"))
	if len(tarballs) == 0 {
		return fmt.Errorf("npm pack produced no tarball for %s", pkg)
	}

	// Extract tarball.
	extractDir := filepath.Join(tmpDir, "extracted")
	tarCmd := exec.Command("tar", "xzf", tarballs[0], "-C", extractDir)
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return fmt.Errorf("creating extract dir: %w", err)
	}
	if out, err := tarCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extracting tarball: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// npm packs into a "package/" subdirectory.
	pkgDir := filepath.Join(extractDir, "package")
	if _, err := os.Stat(filepath.Join(pkgDir, "SKILL.yaml")); err != nil {
		return fmt.Errorf("no SKILL.yaml found in npm package %s — is this a rubichan skill?", src.URL)
	}

	manifest, err := loadSkillManifest(pkgDir)
	if err != nil {
		return fmt.Errorf("reading skill manifest: %w", err)
	}

	dest := filepath.Join(skillsDir, manifest.Name)
	if err := copyDir(pkgDir, dest); err != nil {
		return fmt.Errorf("copying skill: %w", err)
	}

	sourceLabel := "npm:" + src.URL
	if src.Ref != "" {
		sourceLabel += "@" + src.Ref
	}

	store, err := openSkillStore(storePath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.SaveSkillState(storemod.SkillInstallState{
		Name:       manifest.Name,
		Version:    manifest.Version,
		Source:     sourceLabel,
		SourceType: "npm",
		SourceURL:  src.URL,
		SourceRef:  src.Ref,
		Category:   manifest.Category,
		Tags:       strings.Join(manifest.Tags, ","),
	}); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	if cmd != nil {
		cmd.Printf("Installed skill %q (v%s) from %s\n", manifest.Name, manifest.Version, sourceLabel)
	}
	return nil
}
```

- [ ] **Step 3: Run test and build**

Run: `go build ./cmd/rubichan && go test ./cmd/rubichan/... -run TestInstallFromNpm -v`
Expected: PASS (error expected since no real npm package)

- [ ] **Step 4: Commit**

```bash
git add cmd/rubichan/skill.go cmd/rubichan/skill_test.go
git commit -m "[BEHAVIORAL] Add npm: install source for skill install"
```

---

### Task 5: Enhanced skill list and skill info

**Files:**
- Modify: `cmd/rubichan/skill.go`

- [ ] **Step 1: Update skillListCmd to show category column**

Find the `skillListCmd` function (around line 82) and update the table format to include a `CATEGORY` column. The list handler reads from `store.ListAllSkillStates()` — use the new `Category` field.

```go
// In the list formatter, change the header and row format:
fmt.Fprintf(w, "NAME\tVERSION\tCATEGORY\tSOURCE\tINSTALLED\n")
for _, s := range states {
	cat := s.Category
	if cat == "" {
		cat = "-"
	}
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.Name, s.Version, cat, s.Source, s.InstalledAt.Format("2006-01-02"))
}
```

- [ ] **Step 2: Update skillInfoCmd to show category, tags, and component summary**

Find `skillInfoCmd` (around line 175) and add the new fields to the output:

```go
// After existing fields, add:
if manifest.Category != "" {
	fmt.Fprintf(cmd.OutOrStdout(), "Category:    %s\n", manifest.Category)
}
if len(manifest.Tags) > 0 {
	fmt.Fprintf(cmd.OutOrStdout(), "Tags:        %s\n", strings.Join(manifest.Tags, ", "))
}

// Component summary.
var components []string
if len(manifest.Tools) > 0 {
	components = append(components, fmt.Sprintf("%d tools", len(manifest.Tools)))
}
if len(manifest.Agents) > 0 {
	components = append(components, fmt.Sprintf("%d agents", len(manifest.Agents)))
}
if len(manifest.Commands) > 0 {
	components = append(components, fmt.Sprintf("%d commands", len(manifest.Commands)))
}
if len(components) > 0 {
	fmt.Fprintf(cmd.OutOrStdout(), "Components:  %s\n", strings.Join(components, ", "))
}
```

- [ ] **Step 3: Run build and tests**

Run: `go build ./cmd/rubichan && go test ./cmd/rubichan/... -count=1 -timeout 60s 2>&1 | tail -5`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/rubichan/skill.go
git commit -m "[BEHAVIORAL] Enhance skill list with category column and skill info with component summary"
```

---

### Task 6: skill search Filters

**Files:**
- Modify: `cmd/rubichan/skill.go`

- [ ] **Step 1: Add --category and --tag flags to skillSearchCmd**

Find `skillSearchCmd` and add flags:

```go
cmd.Flags().StringVar(&categoryFilter, "category", "", "filter by category")
cmd.Flags().StringVar(&tagFilter, "tag", "", "filter by tag")
```

- [ ] **Step 2: Update search to filter locally installed skills**

The current search only hits the remote registry. Add local search:

```go
// In the search handler, after registry search:
// Also search locally installed skills.
store, err := openSkillStore(storePath)
if err == nil {
	defer store.Close()
	localStates, _ := store.ListAllSkillStates()
	for _, s := range localStates {
		if matchesSearch(s, query, categoryFilter, tagFilter) {
			// Add to results with [installed] marker.
		}
	}
}
```

Add `matchesSearch` helper:

```go
func matchesSearch(s storemod.SkillInstallState, query, category, tag string) bool {
	if category != "" && !strings.EqualFold(s.Category, category) {
		return false
	}
	if tag != "" && !containsTag(s.Tags, tag) {
		return false
	}
	if query != "" {
		q := strings.ToLower(query)
		return strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Tags), q)
	}
	return true
}

func containsTag(tags, tag string) bool {
	for _, t := range strings.Split(tags, ",") {
		if strings.EqualFold(strings.TrimSpace(t), tag) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Run build and tests**

Run: `go build ./cmd/rubichan`
Expected: BUILD OK

- [ ] **Step 4: Commit**

```bash
git add cmd/rubichan/skill.go
git commit -m "[BEHAVIORAL] Add --category and --tag filters to skill search with local search"
```

---

### Task 7: skill update Command

**Files:**
- Modify: `cmd/rubichan/skill.go`

- [ ] **Step 1: Write failing test for update command registration**

```go
func TestSkillUpdateCommandRegistered(t *testing.T) {
	cmd := skillCmd()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "update" {
			found = true
			break
		}
	}
	assert.True(t, found, "skill update command should be registered")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rubichan/... -run TestSkillUpdateCommandRegistered -v`
Expected: FAIL

- [ ] **Step 3: Implement skillUpdateCmd**

```go
func skillUpdateCmd() *cobra.Command {
	var allFlag bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "update [skill-name]",
		Short: "Update installed skills to latest version",
		Long:  "Re-fetch git/npm/registry-installed skills. Local-path skills are skipped.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, _ := cmd.Flags().GetString("store")
			skillsDir, _ := cmd.Flags().GetString("skills-dir")
			store, err := openSkillStore(storePath)
			if err != nil {
				return err
			}
			defer store.Close()

			var targets []storemod.SkillInstallState
			if allFlag {
				targets, err = store.ListAllSkillStates()
				if err != nil {
					return err
				}
			} else if len(args) == 1 {
				s, err := store.GetSkillState(args[0])
				if err != nil {
					return fmt.Errorf("skill %q not found", args[0])
				}
				targets = []storemod.SkillInstallState{*s}
			} else {
				return fmt.Errorf("specify a skill name or use --all")
			}

			updated, skipped, upToDate := 0, 0, 0
			for _, s := range targets {
				switch s.SourceType {
				case "local":
					cmd.Printf("Checking %s (local path)... skipped (local install)\n", s.Name)
					skipped++
				case "git", "github":
					if dryRun {
						cmd.Printf("Checking %s (%s)... would update\n", s.Name, s.Source)
						updated++
						continue
					}
					src := installSource{Type: s.SourceType, URL: s.SourceURL, Ref: s.SourceRef}
					if err := installFromGit(cmd, src, skillsDir, storePath); err != nil {
						cmd.PrintErrf("Failed to update %s: %v\n", s.Name, err)
						continue
					}
					updated++
				case "npm":
					if dryRun {
						cmd.Printf("Checking %s (%s)... would update\n", s.Name, s.Source)
						updated++
						continue
					}
					src := installSource{Type: "npm", URL: s.SourceURL, Ref: ""}
					if err := installFromNpm(cmd, src, skillsDir, storePath); err != nil {
						cmd.PrintErrf("Failed to update %s: %v\n", s.Name, err)
						continue
					}
					updated++
				default:
					cmd.Printf("Checking %s... up to date (%s)\n", s.Name, s.Version)
					upToDate++
				}
			}

			cmd.Printf("Updated %d, skipped %d, up to date %d.\n", updated, skipped, upToDate)
			return nil
		},
	}

	cmd.Flags().BoolVar(&allFlag, "all", false, "update all installed skills")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without updating")
	return cmd
}
```

Register in `skillCmd()`:

```go
cmd.AddCommand(skillUpdateCmd())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/rubichan/... -run TestSkillUpdateCommandRegistered -v`
Expected: PASS

- [ ] **Step 5: Run full build and tests**

Run: `go build ./cmd/rubichan && go test ./cmd/rubichan/... -count=1 -timeout 60s 2>&1 | tail -5`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/rubichan/skill.go cmd/rubichan/skill_test.go
git commit -m "[BEHAVIORAL] Add skill update command with --all and --dry-run flags"
```

---

### Task 8: Update installFromLocal to Save Source Metadata

**Files:**
- Modify: `cmd/rubichan/skill.go`

The existing `installFromLocal()` saves only name, version, source path. Update it to also save `SourceType`, `Category`, and `Tags` from the manifest.

- [ ] **Step 1: Update installFromLocal**

In the `installFromLocal` function, after loading the manifest and before `SaveSkillState`, populate the new fields:

```go
if err := store.SaveSkillState(storemod.SkillInstallState{
	Name:       manifest.Name,
	Version:    manifest.Version,
	Source:     source,
	SourceType: "local",
	Category:   manifest.Category,
	Tags:       strings.Join(manifest.Tags, ","),
}); err != nil {
	return fmt.Errorf("saving state: %w", err)
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./cmd/rubichan/... -count=1 -timeout 60s 2>&1 | tail -5`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/rubichan/skill.go
git commit -m "[BEHAVIORAL] Save category and tags metadata during local skill install"
```

---

### Task 9: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -count=1 -timeout 300s 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 2: Build binary**

Run: `go build -o rubichan_test ./cmd/rubichan`
Expected: BUILD OK

- [ ] **Step 3: Verify help output**

Run: `./rubichan_test skill install --help`
Expected: Shows install command help

Run: `./rubichan_test skill update --help`
Expected: Shows `--all` and `--dry-run` flags

Run: `./rubichan_test skill search --help`
Expected: Shows `--category` and `--tag` flags

- [ ] **Step 4: Manual smoke test**

```bash
# Create a test skill with category and tags
mkdir -p /tmp/smoke-skill
cat > /tmp/smoke-skill/SKILL.yaml << 'EOF'
name: smoke-test
version: 0.1.0
description: Smoke test skill
types: [prompt]
category: testing
tags: [smoke, ci]
EOF

# Install
./rubichan_test skill install /tmp/smoke-skill

# List (should show category)
./rubichan_test skill list

# Info (should show category, tags, components)
./rubichan_test skill info smoke-test

# Search with filter
./rubichan_test skill search --category testing "smoke"

# Clean up
./rubichan_test skill remove smoke-test
```

- [ ] **Step 5: Commit any fixes**

```bash
git add -A && git commit -m "[STRUCTURAL] Final verification fixes"
```
