# Bundled Skill Registration API Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `RegisterBundledSkill()` API that allows skills to be registered as lazy-loaded bundles with embedded content, similar to claude-code's `registerBundledSkill()`.

**Architecture:** A `BundledSkill` type encapsulates a skill definition with optional embedded content (FS, files, or inline strings). The `Loader` gains a `RegisterBundled()` method that stores the bundle without immediate materialization. The `Runtime` materializes bundled skills on first activation, extracting embedded content to a cache directory. This enables built-in skills to ship as embedded resources while keeping memory usage low until needed.

**Tech Stack:** Go, `embed` package, `internal/skills` package.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/skills/bundled.go` | New: `BundledSkill` type, `BundledContent` interface, materialization logic |
| `internal/skills/bundled_test.go` | Tests for bundled skill registration and materialization |
| `internal/skills/loader.go` | Modify: add `bundled` map and `RegisterBundled()` method |
| `internal/skills/runtime.go` | Modify: materialize bundled skills on activation |
| `internal/skills/types.go` | Modify: add `SourceBundled` constant |

---

## Chunk 1: Bundled Skill Types and Registration

### Task 1: Create BundledSkill type and Loader integration

**Files:**
- Create: `internal/skills/bundled.go`
- Create: `internal/skills/bundled_test.go`
- Modify: `internal/skills/loader.go`
- Modify: `internal/skills/types.go`

- [ ] **Step 1: Write the failing test**

Create `internal/skills/bundled_test.go`:

```go
package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundledSkillRegistration(t *testing.T) {
	loader := NewLoader("", "")

	bundle := BundledSkill{
		Name:        "test-bundle",
		Version:     "1.0.0",
		Description: "A test bundled skill",
		Types:       []SkillType{SkillTypePrompt},
		Content: &InlineContent{
			Files: map[string]string{
				"SKILL.md": "# Test Bundle\n\nThis is a test skill.",
			},
		},
	}

	loader.RegisterBundled(bundle)

	// Bundled skills should appear in discovery.
	discovered, _, err := loader.Discover(nil)
	require.NoError(t, err)

	var found *DiscoveredSkill
	for i := range discovered {
		if discovered[i].Manifest.Name == "test-bundle" {
			found = &discovered[i]
			break
		}
	}
	require.NotNil(t, found, "bundled skill should be discovered")
	assert.Equal(t, "test-bundle", found.Manifest.Name)
	assert.Equal(t, SourceBundled, found.Source)
	assert.Equal(t, "1.0.0", found.Manifest.Version)
}

func TestBundledSkillMaterialize(t *testing.T) {
	content := &InlineContent{
		Files: map[string]string{
			"SKILL.md": "# Test\n\nContent",
			"helper.txt": "helper content",
		},
	}

	cacheDir := t.TempDir()
	dir, err := content.Materialize(cacheDir, "test-skill")
	require.NoError(t, err)

	// Verify files were written.
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Test\n\nContent", string(data))

	data, err = os.ReadFile(filepath.Join(dir, "helper.txt"))
	require.NoError(t, err)
	assert.Equal(t, "helper content", string(data))
}

func TestBundledSkillPrecedence(t *testing.T) {
	loader := NewLoader("", "")

	// Register a built-in skill.
	loader.RegisterBuiltin(&SkillManifest{
		Name:        "conflict-skill",
		Version:     "1.0.0",
		Description: "builtin",
	})

	// Register a bundled skill with the same name.
	loader.RegisterBundled(BundledSkill{
		Name:        "conflict-skill",
		Version:     "2.0.0",
		Description: "bundled",
		Types:       []SkillType{SkillTypePrompt},
	})

	discovered, _, err := loader.Discover(nil)
	require.NoError(t, err)

	var found *DiscoveredSkill
	for i := range discovered {
		if discovered[i].Manifest.Name == "conflict-skill" {
			found = &discovered[i]
			break
		}
	}
	require.NotNil(t, found)
	// Built-in should override bundled.
	assert.Equal(t, SourceBuiltin, found.Source)
	assert.Equal(t, "builtin", found.Manifest.Description)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/skills/... -run TestBundled -v
```

Expected: FAIL — `BundledSkill`, `InlineContent`, `SourceBundled`, `RegisterBundled` don't exist.

- [ ] **Step 3: Implement types and constants**

Add to `internal/skills/types.go` after `SourceMCP`:

```go
	// SourceBundled is a skill registered as a lazy-loaded bundle.
	SourceBundled Source = "bundled"
```

- [ ] **Step 4: Create bundled.go**

Create `internal/skills/bundled.go`:

```go
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
```

- [ ] **Step 5: Add RegisterBundled to Loader**

Add to `internal/skills/loader.go` in the `Loader` struct:

```go
	bundled    map[string]BundledSkill
```

Add to `NewLoader`:

```go
		bundled:    make(map[string]BundledSkill),
```

Add method after `RegisterBuiltinDiscovered`:

```go
// RegisterBundled adds a bundled skill to the loader. Bundled skills are
// materialized on first activation. They have lower priority than built-in
// skills but higher than directory-discovered skills.
func (l *Loader) RegisterBundled(bundle BundledSkill) {
	l.bundled[bundle.Name] = bundle
}
```

Add to `Discover` after built-in skills (step 4) and before MCP servers (step 4.5):

```go
	// 4.1. Bundled skills override directory-discovered skills but not built-ins.
	for name, bundle := range l.bundled {
		// Skip if a built-in skill already has this name.
		if _, exists := byName[name]; exists {
			continue
		}
		byName[name] = DiscoveredSkill{
			Manifest: &SkillManifest{
				Name:        bundle.Name,
				Version:     bundle.Version,
				Description: bundle.Description,
				Types:       bundle.Types,
				Permissions: bundle.Permissions,
				Triggers:    bundle.Triggers,
				Prompt:      bundle.Prompt,
			},
			Source: SourceBundled,
		}
	}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/skills/... -run TestBundled -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/skills/bundled.go internal/skills/bundled_test.go internal/skills/loader.go internal/skills/types.go
git commit -m "[BEHAVIORAL] Add BundledSkill type with lazy materialization"
```

---

## Chunk 2: Runtime Materialization

### Task 2: Materialize bundled skills on activation

**Files:**
- Modify: `internal/skills/runtime.go`
- Test: `internal/skills/runtime_test.go`

- [ ] **Step 1: Add cacheDir and materialization to Runtime**

Add to `Runtime` struct in `runtime.go`:

```go
	bundledCacheDir     string
```

Add to `NewRuntime`:

```go
		bundledCacheDir:     filepath.Join(os.TempDir(), "rubichan-bundled-skills"),
```

Add import for `path/filepath` and `os` if not present.

- [ ] **Step 2: Add materialization logic in Activate**

In `Activate`, after looking up the skill and before creating the sandbox, check if it's a bundled skill that needs materialization:

```go
	// If this is a bundled skill with content, materialize it first.
	if sk.Source == SourceBundled && sk.Dir == "" {
		// Look up the bundled skill definition.
		var bundle *BundledSkill
		if rt.loader != nil {
			if b, ok := rt.loader.bundled[name]; ok {
				bundle = &b
			}
		}
		if bundle != nil && bundle.Content != nil {
			skillDir, matErr := bundle.Content.Materialize(rt.bundledCacheDir, name)
			if matErr != nil {
				rt.mu.Lock()
				_ = sk.TransitionTo(SkillStateError)
				_ = sk.TransitionTo(SkillStateInactive)
				rt.mu.Unlock()
				rt.emitErrorEvent(name, matErr)
				return fmt.Errorf("materialize bundled skill %q: %w", name, matErr)
			}
			rt.mu.Lock()
			sk.Dir = skillDir
			rt.mu.Unlock()
		}
	}
```

Note: This needs to be placed after the skill lookup but before the `rt.mu.Unlock()` that releases the lock for Phase 2. The exact placement depends on the current `Activate` method structure.

- [ ] **Step 3: Add SetBundledCacheDir setter**

```go
// SetBundledCacheDir sets the directory where bundled skills are materialized.
// Defaults to os.TempDir()/rubichan-bundled-skills.
func (rt *Runtime) SetBundledCacheDir(dir string) {
	rt.bundledCacheDir = dir
}
```

- [ ] **Step 4: Write integration test**

Add to `runtime_test.go`:

```go
func TestRuntimeActivateBundledSkill(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	loader := NewLoader(userDir, projectDir)
	loader.RegisterBundled(BundledSkill{
		Name:        "bundled-test",
		Version:     "1.0.0",
		Description: "A bundled test skill",
		Types:       []SkillType{SkillTypePrompt},
		Content: &InlineContent{
			Files: map[string]string{
				"SKILL.md": "# Bundled Test\n\nThis skill was bundled.",
			},
		},
	})

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	registry := tools.NewRegistry()
	backendFactory := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackend{}, nil
	}
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return &stubPermissionChecker{}
	}

	cacheDir := t.TempDir()
	rt := NewRuntime(loader, s, registry, nil, backendFactory, sandboxFactory)
	rt.SetBundledCacheDir(cacheDir)
	require.NoError(t, rt.Discover(nil))

	require.NoError(t, rt.Activate("bundled-test"))

	// Verify the skill was materialized.
	rt.mu.RLock()
	sk := rt.skills["bundled-test"]
	rt.mu.RUnlock()
	require.NotNil(t, sk)
	assert.NotEmpty(t, sk.Dir, "bundled skill should have a directory after materialization")
	assert.DirExists(t, sk.Dir)

	// Verify the file was written.
	_, err = os.ReadFile(filepath.Join(sk.Dir, "SKILL.md"))
	require.NoError(t, err)
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/skills/... -run TestRuntimeActivateBundled -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/skills/runtime.go internal/skills/runtime_test.go
git commit -m "[BEHAVIORAL] Materialize bundled skills on activation"
```

---

## Chunk 3: Refactor Existing Built-ins to Use Bundled API

### Task 3: Migrate uiuxpromax to use RegisterBundled

**Files:**
- Modify: `internal/skills/builtin/uiuxpromax/embed.go`
- Test: existing tests should still pass

- [ ] **Step 1: Refactor uiuxpromax to use BundledSkill**

Replace the current `Register` function in `embed.go`:

```go
// Register registers the ui-ux-pro-max skill as a bundled skill.
func Register(loader *skills.Loader, cacheRoot string) error {
	bundle := skills.BundledSkill{
		Name:        "ui-ux-pro-max",
		Version:     materializeVersion,
		Description: "UI/UX Pro Max — comprehensive design system skill",
		Types:       []skills.SkillType{skills.SkillTypePrompt},
		Content: &skills.EmbedContent{
			FS:     content,
			Prefix: embeddedSkillRoot,
		},
	}
	loader.RegisterBundled(bundle)
	return nil
}
```

Remove the `materialize` function and its helpers (keep only the `content` embed.FS and constants).

- [ ] **Step 2: Verify tests pass**

```bash
go test ./internal/skills/builtin/uiuxpromax/... -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/skills/builtin/uiuxpromax/embed.go
git commit -m "[BEHAVIORAL] Migrate uiuxpromax to BundledSkill API"
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

**Title:** `[BEHAVIORAL] Skill system: Bundled skill registration API`

**Body:**
- Add `BundledSkill` type with lazy materialization via `BundledContent` interface
- Support three content sources: `InlineContent` (string map), `EmbedContent` (//go:embed), `FileMapContent` (byte map)
- `Loader.RegisterBundled()` stores bundles without immediate I/O
- `Runtime` materializes bundled skills to cache dir on first activation
- Bundled skills have priority between built-in and directory-discovered skills
- Migrate `uiuxpromax` built-in to use new `RegisterBundled` API

**Commit prefix:** `[BEHAVIORAL]`
