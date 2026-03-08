package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillListCommand(t *testing.T) {
	// Create a temp store with test data.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)

	require.NoError(t, s.SaveSkillState(store.SkillInstallState{
		Name:    "code-review",
		Version: "1.0.0",
		Source:  "registry",
	}))
	require.NoError(t, s.SaveSkillState(store.SkillInstallState{
		Name:    "formatter",
		Version: "2.1.0",
		Source:  "git",
	}))
	s.Close()

	// Build the skill list command and capture output.
	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--store", dbPath})

	err = cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	// Should contain table headers.
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "VERSION")
	assert.Contains(t, output, "SOURCE")
	// Should contain both skills.
	assert.Contains(t, output, "code-review")
	assert.Contains(t, output, "1.0.0")
	assert.Contains(t, output, "registry")
	assert.Contains(t, output, "formatter")
	assert.Contains(t, output, "2.1.0")
	assert.Contains(t, output, "git")
}

func TestSkillListCommandEmpty(t *testing.T) {
	// Create an empty store.
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	s.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--store", dbPath})

	err = cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "No skills installed")
}

func TestSkillListAvailable(t *testing.T) {
	results := []skills.RegistrySearchResult{
		{Name: "code-review", Version: "1.0.0", Description: "Automated code review"},
		{Name: "formatter", Version: "2.1.0", Description: "Code formatting skill"},
	}

	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/search", r.URL.Path)
		assert.Equal(t, "", r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(results)
	}))
	defer srv.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--available", "--registry", srv.URL})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "VERSION")
	assert.Contains(t, output, "DESCRIPTION")
	assert.Contains(t, output, "code-review")
	assert.Contains(t, output, "1.0.0")
	assert.Contains(t, output, "Automated code review")
	assert.Contains(t, output, "formatter")
}

func TestSkillListAvailableNoResults(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]skills.RegistrySearchResult{})
	}))
	defer srv.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--available", "--registry", srv.URL})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "No skills available")
}

func TestSkillInfoCommand(t *testing.T) {
	// Create a temp directory with a SKILL.yaml.
	skillDir := filepath.Join(t.TempDir(), "skills", "my-tool")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	manifestYAML := `name: my-tool
version: 3.0.0
description: "A test tool skill"
types:
  - tool
author: tester
license: MIT
permissions:
  - file:read
  - shell:exec
implementation:
  backend: starlark
  entrypoint: main.star
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.yaml"), []byte(manifestYAML), 0o644))

	// Create a store that maps the skill name to a source directory.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	require.NoError(t, s.SaveSkillState(store.SkillInstallState{
		Name:    "my-tool",
		Version: "3.0.0",
		Source:  skillDir,
	}))
	s.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"info", "my-tool", "--store", dbPath})

	err = cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "my-tool")
	assert.Contains(t, output, "3.0.0")
	assert.Contains(t, output, "A test tool skill")
	assert.Contains(t, output, "tester")
	assert.Contains(t, output, "MIT")
	assert.Contains(t, output, "tool")
	assert.Contains(t, output, "file:read")
	assert.Contains(t, output, "shell:exec")
	assert.Contains(t, output, "starlark")
	assert.Contains(t, output, "InstalledFrom:")
}

func TestSkillInfoCommandNotFound(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	s.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"info", "nonexistent", "--store", dbPath})

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSkillSearchCommand(t *testing.T) {
	// Mock the registry API.
	results := []skills.RegistrySearchResult{
		{Name: "code-review", Version: "1.0.0", Description: "Automated code review"},
		{Name: "code-format", Version: "2.1.0", Description: "Code formatting skill"},
	}

	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/search", r.URL.Path)
		assert.Equal(t, "code", r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(results)
	}))
	defer srv.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"search", "code", "--registry", srv.URL})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	// Should contain table headers.
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "VERSION")
	assert.Contains(t, output, "DESCRIPTION")
	// Should contain search results.
	assert.Contains(t, output, "code-review")
	assert.Contains(t, output, "1.0.0")
	assert.Contains(t, output, "Automated code review")
	assert.Contains(t, output, "code-format")
	assert.Contains(t, output, "2.1.0")
	assert.Contains(t, output, "Code formatting skill")
}

func TestSkillSearchCommandNoResults(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]skills.RegistrySearchResult{})
	}))
	defer srv.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"search", "nonexistent", "--registry", srv.URL})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "No results found")
}

func TestSkillAddDir(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.toml")
	skillPackDir := filepath.Join(t.TempDir(), "skill-pack")
	require.NoError(t, os.MkdirAll(skillPackDir, 0o755))

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"add-dir", skillPackDir, "--config", configFile})

	err := cmd.Execute()
	require.NoError(t, err)

	cfg, err := config.Load(configFile)
	require.NoError(t, err)
	require.Len(t, cfg.Skills.Dirs, 1)

	absDir, err := filepath.Abs(skillPackDir)
	require.NoError(t, err)
	assert.Equal(t, absDir, cfg.Skills.Dirs[0])
	assert.Contains(t, buf.String(), "Registered skill directory")
}

func TestSkillWhy(t *testing.T) {
	projectDir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(projectDir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module test\n"), 0o644))

	userDir := filepath.Join(t.TempDir(), "skills")
	skillDir := filepath.Join(userDir, "k8s-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: k8s-skill
version: 1.0.0
description: "Kubernetes helper"
triggers:
  keywords:
    - kubernetes
---

Use for Kubernetes work.
`), 0o644))

	configFile := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.DefaultConfig()
	cfg.Skills.UserDir = userDir
	require.NoError(t, config.Save(configFile, cfg))

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"why", "k8s-skill", "--config", configFile, "--message", "deploy to kubernetes"})

	err = cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Name:       k8s-skill")
	assert.Contains(t, output, "Activated:  true")
	assert.Contains(t, output, "Score:")
	assert.Contains(t, output, "Keywords:   kubernetes")
}

func TestSkillTrace(t *testing.T) {
	projectDir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(projectDir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module test\n"), 0o644))

	userDir := filepath.Join(t.TempDir(), "skills")

	alphaDir := filepath.Join(userDir, "alpha-skill")
	require.NoError(t, os.MkdirAll(alphaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(alphaDir, "SKILL.md"), []byte(`---
name: alpha-skill
version: 1.0.0
description: "Alpha helper"
triggers:
  keywords:
    - alpha
---

Alpha guidance repeated. Alpha guidance repeated. Alpha guidance repeated. Alpha guidance repeated.
`), 0o644))

	betaDir := filepath.Join(userDir, "beta-skill")
	require.NoError(t, os.MkdirAll(betaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(betaDir, "SKILL.md"), []byte(`---
name: beta-skill
version: 1.0.0
description: "Beta helper"
triggers:
  keywords:
    - beta
---

Beta guidance repeated. Beta guidance repeated. Beta guidance repeated. Beta guidance repeated.
`), 0o644))

	configFile := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.DefaultConfig()
	cfg.Skills.UserDir = userDir
	require.NoError(t, config.Save(configFile, cfg))

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"trace",
		"--config", configFile,
		"--message", "alpha beta",
		"--max-total-tokens", "12",
		"--max-per-skill-tokens", "12",
	})

	err = cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Activation")
	assert.Contains(t, output, "alpha-skill [activated]")
	assert.Contains(t, output, "beta-skill [activated]")
	assert.Contains(t, output, "Prompt Budget")
	assert.Contains(t, output, "decision=truncated")
	assert.Contains(t, output, "decision=excluded")
}

func TestSkillSearchCommandMissingQuery(t *testing.T) {
	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"search"})

	err := cmd.Execute()
	require.Error(t, err)
}

// TestSkillSearchCommandContextCancellation verifies search handles context cancellation.
func TestSkillSearchCommandContextCancellation(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow server.
		ctx := r.Context()
		<-ctx.Done()
	}))
	defer srv.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Use a cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"search", "code", "--registry", srv.URL})

	err := cmd.Execute()
	require.Error(t, err)
}

// testManifestYAML is a valid SKILL.yaml used across install/add tests.
const testManifestYAML = `name: my-tool
version: 1.0.0
description: "A test tool skill"
types:
  - tool
author: tester
license: MIT
implementation:
  backend: starlark
  entrypoint: main.star
`

// createTestSkillDir sets up a temp directory containing a valid SKILL.yaml
// and an extra file to verify recursive copy.
func createTestSkillDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "src-skill")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.yaml"), []byte(testManifestYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.star"), []byte("print('hello')"), 0o644))

	subDir := filepath.Join(dir, "lib")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "helper.star"), []byte("x = 1"), 0o644))
	return dir
}

// makeTarGz builds an in-memory gzipped tar archive from a map of
// relative-path -> content entries, suitable for the registry download endpoint.
func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

// TestSkillInstallLocal verifies that "skill install <local-path>" copies the
// directory to the skills dir, validates SKILL.yaml, and saves state to store.
func TestSkillInstallLocal(t *testing.T) {
	srcDir := createTestSkillDir(t)

	skillsDir := filepath.Join(t.TempDir(), "installed-skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install", srcDir, "--store", dbPath, "--skills-dir", skillsDir})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify the skill was copied.
	copiedManifest := filepath.Join(skillsDir, "my-tool", "SKILL.yaml")
	_, err = os.Stat(copiedManifest)
	require.NoError(t, err, "SKILL.yaml should exist in installed location")

	// Verify sub-directory was also copied.
	copiedHelper := filepath.Join(skillsDir, "my-tool", "lib", "helper.star")
	_, err = os.Stat(copiedHelper)
	require.NoError(t, err, "nested files should be copied recursively")

	// Verify state was saved in the store.
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	defer s.Close()

	state, err := s.GetSkillState("my-tool")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "my-tool", state.Name)
	assert.Equal(t, "1.0.0", state.Version)
	assert.Contains(t, state.Source, skillsDir)

	// Output should contain confirmation message.
	assert.Contains(t, buf.String(), "Installed skill")
	assert.Contains(t, buf.String(), "my-tool")
}

// TestSkillInstallFromRegistry verifies that "skill install <name>" downloads
// from the registry, extracts the tarball, and saves state.
func TestSkillInstallFromRegistry(t *testing.T) {
	tarData := makeTarGz(t, map[string]string{
		"SKILL.yaml": testManifestYAML,
		"main.star":  "print('hello')",
	})

	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/skills/my-tool/latest/manifest":
			w.Header().Set("Content-Type", "application/x-yaml")
			_, _ = w.Write([]byte(testManifestYAML))
		case r.URL.Path == "/api/v1/skills/my-tool/latest/download":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	skillsDir := filepath.Join(t.TempDir(), "installed-skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install", "my-tool", "--store", dbPath, "--skills-dir", skillsDir, "--registry", srv.URL})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify the skill was extracted.
	copiedManifest := filepath.Join(skillsDir, "my-tool", "SKILL.yaml")
	_, err = os.Stat(copiedManifest)
	require.NoError(t, err, "SKILL.yaml should exist in installed location")

	// Verify state was saved.
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	defer s.Close()

	state, err := s.GetSkillState("my-tool")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "my-tool", state.Name)
	assert.Equal(t, "1.0.0", state.Version)
}

// TestSkillInstallVersion verifies that "skill install name@version" correctly
// parses the name and version and downloads from the registry.
func TestSkillInstallVersion(t *testing.T) {
	manifestVersioned := `name: my-tool
version: 2.3.0
description: "A versioned tool skill"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: main.star
`
	tarData := makeTarGz(t, map[string]string{
		"SKILL.yaml": manifestVersioned,
		"main.star":  "print('v2')",
	})

	var requestedVersion string
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/skills/my-tool/2.3.0/manifest":
			w.Header().Set("Content-Type", "application/x-yaml")
			_, _ = w.Write([]byte(manifestVersioned))
		case r.URL.Path == "/api/v1/skills/my-tool/2.3.0/download":
			requestedVersion = "2.3.0"
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	skillsDir := filepath.Join(t.TempDir(), "installed-skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install", "my-tool@2.3.0", "--store", dbPath, "--skills-dir", skillsDir, "--registry", srv.URL})

	err := cmd.Execute()
	require.NoError(t, err)

	// The server should have received the versioned request.
	assert.Equal(t, "2.3.0", requestedVersion)

	// Verify state shows the correct version.
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	defer s.Close()

	state, err := s.GetSkillState("my-tool")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "2.3.0", state.Version)
}

// TestSkillInstallSemVerRange verifies that "skill install name@^1.0.0"
// resolves the version range against the registry and installs the best match.
func TestSkillInstallSemVerRange(t *testing.T) {
	// The registry has versions 1.0.0, 1.2.0, 2.0.0. ^1.0.0 should resolve to 1.2.0.
	manifestResolved := `name: my-tool
version: 1.2.0
description: "A versioned tool skill"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: main.star
`
	tarData := makeTarGz(t, map[string]string{
		"SKILL.yaml": manifestResolved,
		"main.star":  "print('v1.2')",
	})

	var downloadedVersion string
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/skills/my-tool/versions":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]string{"1.0.0", "1.2.0", "2.0.0"})
		case r.URL.Path == "/api/v1/skills/my-tool/1.2.0/download":
			downloadedVersion = "1.2.0"
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	skillsDir := filepath.Join(t.TempDir(), "installed-skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install", "my-tool@^1.0.0", "--store", dbPath, "--skills-dir", skillsDir, "--registry", srv.URL})

	err := cmd.Execute()
	require.NoError(t, err)

	// The server should have resolved ^1.0.0 to 1.2.0.
	assert.Equal(t, "1.2.0", downloadedVersion)

	// Verify state shows the resolved version.
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	defer s.Close()

	state, err := s.GetSkillState("my-tool")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "1.2.0", state.Version)

	// Output should mention the resolved version.
	assert.Contains(t, buf.String(), "1.2.0")
}

// TestSkillInstallSemVerRangeNoMatch verifies error when no version matches the range.
func TestSkillInstallSemVerRangeNoMatch(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/skills/my-tool/versions":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]string{"1.0.0", "1.1.0"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	skillsDir := filepath.Join(t.TempDir(), "installed-skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install", "my-tool@^3.0.0", "--store", dbPath, "--skills-dir", skillsDir, "--registry", srv.URL})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving version")
}

// TestSkillRemove verifies that "skill remove <name>" deletes the skill
// directory and removes its entry from the store.
func TestSkillRemove(t *testing.T) {
	// Set up: install a skill first.
	srcDir := createTestSkillDir(t)
	skillsDir := filepath.Join(t.TempDir(), "installed-skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Install the skill.
	installCmd := skillCmd()
	installBuf := new(bytes.Buffer)
	installCmd.SetOut(installBuf)
	installCmd.SetErr(installBuf)
	installCmd.SetArgs([]string{"install", srcDir, "--store", dbPath, "--skills-dir", skillsDir})
	require.NoError(t, installCmd.Execute())

	// Verify the skill exists before removal.
	installedDir := filepath.Join(skillsDir, "my-tool")
	_, err := os.Stat(installedDir)
	require.NoError(t, err, "skill directory should exist before removal")

	// Now remove it.
	removeCmd := skillCmd()
	removeBuf := new(bytes.Buffer)
	removeCmd.SetOut(removeBuf)
	removeCmd.SetErr(removeBuf)
	removeCmd.SetArgs([]string{"remove", "my-tool", "--store", dbPath, "--skills-dir", skillsDir})

	err = removeCmd.Execute()
	require.NoError(t, err)

	// Verify directory is gone.
	_, err = os.Stat(installedDir)
	assert.True(t, os.IsNotExist(err), "skill directory should be deleted after removal")

	// Verify store entry is gone.
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	defer s.Close()

	state, err := s.GetSkillState("my-tool")
	require.NoError(t, err)
	assert.Nil(t, state, "store entry should be removed")

	// Output should contain confirmation.
	assert.Contains(t, removeBuf.String(), "Removed skill")
	assert.Contains(t, removeBuf.String(), "my-tool")
}

// TestSkillAdd verifies that "skill add <path>" copies a skill into the
// project's .rubichan/skills/<name>/ directory.
func TestSkillAdd(t *testing.T) {
	srcDir := createTestSkillDir(t)
	projectDir := filepath.Join(t.TempDir(), "my-project")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"add", srcDir, "--project-dir", projectDir})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify the skill was copied to .rubichan/skills/<name>/.
	addedManifest := filepath.Join(projectDir, ".rubichan", "skills", "my-tool", "SKILL.yaml")
	_, err = os.Stat(addedManifest)
	require.NoError(t, err, "SKILL.yaml should exist in project .rubichan/skills/<name>/")

	// Verify sub-directory was also copied.
	addedHelper := filepath.Join(projectDir, ".rubichan", "skills", "my-tool", "lib", "helper.star")
	_, err = os.Stat(addedHelper)
	require.NoError(t, err, "nested files should be copied recursively")

	// Output should contain confirmation.
	output := buf.String()
	assert.Contains(t, output, "Added skill")
	assert.Contains(t, output, "my-tool")
}

func TestSkillInfoInstructionSkill(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "skills", "react-guide")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillMD := `---
name: react-guide
version: 1.2.3
description: "React guidance"
types:
  - prompt
permissions:
  - file:read
---

Use this skill when editing React files.
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644))

	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	require.NoError(t, s.SaveSkillState(store.SkillInstallState{
		Name:    "react-guide",
		Version: "1.2.3",
		Source:  skillDir,
	}))
	s.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"info", "react-guide", "--store", dbPath})

	err = cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "react-guide")
	assert.Contains(t, output, "1.2.3")
	assert.Contains(t, output, "React guidance")
	assert.Contains(t, output, "prompt")
	assert.Contains(t, output, "file:read")
	assert.NotContains(t, output, "Backend:")
}

// TestSkillInstallLocalInvalidManifest verifies install fails when SKILL.yaml
// is missing or invalid.
func TestSkillInstallLocalInvalidManifest(t *testing.T) {
	// Case 1: directory without SKILL.yaml.
	emptyDir := t.TempDir()

	skillsDir := filepath.Join(t.TempDir(), "skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install", emptyDir, "--store", dbPath, "--skills-dir", skillsDir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SKILL.yaml")

	// Case 2: directory with invalid SKILL.yaml.
	badDir := filepath.Join(t.TempDir(), "bad-skill")
	require.NoError(t, os.MkdirAll(badDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "SKILL.yaml"), []byte("not: valid: skill"), 0o644))

	cmd2 := skillCmd()
	buf2 := new(bytes.Buffer)
	cmd2.SetOut(buf2)
	cmd2.SetErr(buf2)
	cmd2.SetArgs([]string{"install", badDir, "--store", dbPath, "--skills-dir", skillsDir})

	err = cmd2.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid manifest")
}

func TestSkillInstallLocalInstructionSkill(t *testing.T) {
	source := filepath.Join(t.TempDir(), "instruction-skill")
	require.NoError(t, os.MkdirAll(source, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(`---
name: docs-guide
version: 0.3.0
description: "Documentation guidance"
---

Write concise docs.
`), 0o644))

	skillsDir := filepath.Join(t.TempDir(), "skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install", source, "--store", dbPath, "--skills-dir", skillsDir})

	err := cmd.Execute()
	require.NoError(t, err)

	installedManifest := filepath.Join(skillsDir, "docs-guide", "SKILL.md")
	_, err = os.Stat(installedManifest)
	require.NoError(t, err)

	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	defer s.Close()

	state, err := s.GetSkillState("docs-guide")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "0.3.0", state.Version)
	assert.Equal(t, filepath.Join(skillsDir, "docs-guide"), state.Source)
}

// TestSkillInstallRegistryDownloadError verifies install fails on registry errors.
func TestSkillInstallRegistryDownloadError(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	skillsDir := filepath.Join(t.TempDir(), "skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install", "nonexistent-skill", "--store", dbPath, "--skills-dir", skillsDir, "--registry", srv.URL})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "downloading skill")
}

// TestSkillRemoveNotInstalled verifies remove handles a non-existent skill.
func TestSkillRemoveNotInstalled(t *testing.T) {
	skillsDir := filepath.Join(t.TempDir(), "skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"remove", "nonexistent", "--store", dbPath, "--skills-dir", skillsDir})

	// remove should return an error for nonexistent skills.
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

// TestSkillAddInvalidManifest verifies add fails when SKILL.yaml is missing.
func TestSkillAddInvalidManifest(t *testing.T) {
	emptyDir := t.TempDir()
	projectDir := filepath.Join(t.TempDir(), "project")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"add", emptyDir, "--project-dir", projectDir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SKILL.yaml")
}

// TestSkillInstallMissingArgs verifies commands require arguments.
func TestSkillInstallMissingArgs(t *testing.T) {
	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install"})

	err := cmd.Execute()
	require.Error(t, err)
}

// TestSkillRemoveMissingArgs verifies remove requires a name argument.
func TestSkillRemoveMissingArgs(t *testing.T) {
	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"remove"})

	err := cmd.Execute()
	require.Error(t, err)
}

// TestSkillAddMissingArgs verifies add requires a path argument.
func TestSkillAddMissingArgs(t *testing.T) {
	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"add"})

	err := cmd.Execute()
	require.Error(t, err)
}

// TestIsLocalPath verifies local path detection logic.
func TestIsLocalPath(t *testing.T) {
	assert.True(t, isLocalPath("./my-skill"))
	assert.True(t, isLocalPath("../my-skill"))
	assert.True(t, isLocalPath("/tmp/my-skill"))
	assert.True(t, isLocalPath("path/to/skill"))
	assert.False(t, isLocalPath("my-skill"))
	assert.False(t, isLocalPath("my-skill@1.0.0"))
}

// TestParseNameVersion verifies name@version parsing.
func TestParseNameVersion(t *testing.T) {
	name, version := parseNameVersion("my-skill@1.2.3")
	assert.Equal(t, "my-skill", name)
	assert.Equal(t, "1.2.3", version)

	name, version = parseNameVersion("my-skill")
	assert.Equal(t, "my-skill", name)
	assert.Equal(t, "latest", version)

	name, version = parseNameVersion("my-skill@")
	assert.Equal(t, "my-skill", name)
	assert.Equal(t, "", version)
}

// TestValidateSkillName verifies skill name validation rejects path traversal.
func TestValidateSkillName(t *testing.T) {
	// Valid names.
	assert.NoError(t, validateSkillName("my-skill"))
	assert.NoError(t, validateSkillName("code_review"))
	assert.NoError(t, validateSkillName("kubernetes"))
	assert.NoError(t, validateSkillName("skill123"))

	// Invalid names.
	assert.Error(t, validateSkillName("../../admin"))
	assert.Error(t, validateSkillName("my skill"))
	assert.Error(t, validateSkillName("my/skill"))
	assert.Error(t, validateSkillName(""))
	assert.Error(t, validateSkillName("-starts-with-dash"))
	assert.Error(t, validateSkillName("_starts-with-underscore"))
	assert.Error(t, validateSkillName("."))
	assert.Error(t, validateSkillName(".."))
	assert.Error(t, validateSkillName(".hidden"))

	// Max length.
	longName := strings.Repeat("a", 129)
	assert.Error(t, validateSkillName(longName))
	assert.NoError(t, validateSkillName(longName[:128]))
}

// TestSkillInstallInvalidName verifies install rejects invalid skill names
// that would be routed through installFromRegistry (not isLocalPath).
func TestSkillInstallInvalidName(t *testing.T) {
	skillsDir := filepath.Join(t.TempDir(), "skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Use name@version syntax with an invalid name containing a space.
	// parseNameVersion splits on @ so name="bad name", which fails validation.
	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install", "bad name@1.0.0", "--store", dbPath, "--skills-dir", skillsDir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid skill name")
}

// TestSkillListAvailableError verifies --available handles registry errors.
func TestSkillListAvailableError(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--available", "--registry", srv.URL})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching available skills")
}

// TestSkillInstallSemVerRangeListVersionsError verifies that a registry error
// during version listing propagates correctly.
func TestSkillInstallSemVerRangeListVersionsError(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	skillsDir := filepath.Join(t.TempDir(), "skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install", "my-tool@^1.0.0", "--store", dbPath, "--skills-dir", skillsDir, "--registry", srv.URL})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing versions")
}

// TestSkillInstallVersionMismatch verifies that a registry serving a manifest
// with a different version than requested is rejected.
func TestSkillInstallVersionMismatch(t *testing.T) {
	mismatchManifest := `name: my-tool
version: 9.9.9
description: "A test tool skill"
types:
  - tool
author: tester
license: MIT
implementation:
  backend: starlark
  entrypoint: main.star
`
	tarData := makeTarGz(t, map[string]string{
		"SKILL.yaml": mismatchManifest,
		"main.star":  "print('hello')",
	})

	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/manifest"):
			w.Header().Set("Content-Type", "application/x-yaml")
			_, _ = w.Write([]byte(mismatchManifest))
		case strings.HasSuffix(r.URL.Path, "/download"):
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	skillsDir := filepath.Join(t.TempDir(), "skills")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"install", "my-tool@1.0.0", "--store", dbPath, "--skills-dir", skillsDir, "--registry", srv.URL})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "downloaded skill declares version")
}

// TestSkillCreate verifies that "skill create <name>" scaffolds a directory
// with a SKILL.yaml template and a skill.star template.
func TestSkillCreate(t *testing.T) {
	parentDir := t.TempDir()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "my-greeter", "--dir", parentDir})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify directory was created.
	skillDir := filepath.Join(parentDir, "my-greeter")
	info, err := os.Stat(skillDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify SKILL.yaml exists and parses correctly.
	manifestPath := filepath.Join(skillDir, "SKILL.yaml")
	data, err := os.ReadFile(manifestPath)
	require.NoError(t, err)

	manifest, err := skills.ParseManifest(data)
	require.NoError(t, err)
	assert.Equal(t, "my-greeter", manifest.Name)
	assert.Equal(t, "0.1.0", manifest.Version)
	assert.Contains(t, manifest.Types, skills.SkillTypeTool)
	assert.Equal(t, skills.BackendStarlark, manifest.Implementation.Backend)

	// Verify skill.star template exists.
	starPath := filepath.Join(skillDir, "skill.star")
	starData, err := os.ReadFile(starPath)
	require.NoError(t, err)
	assert.Contains(t, string(starData), "register_tool")

	// Output should contain confirmation.
	output := buf.String()
	assert.Contains(t, output, "my-greeter")
	assert.Contains(t, output, "Created skill")
}

func TestSkillCreateInstruction(t *testing.T) {
	parentDir := t.TempDir()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "react-guide", "--dir", parentDir, "--type", "instruction"})

	err := cmd.Execute()
	require.NoError(t, err)

	skillDir := filepath.Join(parentDir, "react-guide")
	info, err := os.Stat(skillDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	require.NoError(t, err)

	manifest, body, err := skills.ParseInstructionSkill(data)
	require.NoError(t, err)
	assert.Equal(t, "react-guide", manifest.Name)
	assert.Equal(t, "0.1.0", manifest.Version)
	assert.Equal(t, "A new instruction skill", manifest.Description)
	assert.Contains(t, body, "Add concise guidance")

	_, err = os.Stat(filepath.Join(skillDir, "skill.star"))
	assert.True(t, os.IsNotExist(err))
}

// TestSkillTest verifies that "skill test <path>" loads and validates
// a SKILL.yaml manifest from the given directory.
func TestSkillTest(t *testing.T) {
	// Create a temp dir with a valid SKILL.yaml.
	skillDir := filepath.Join(t.TempDir(), "test-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	manifestYAML := `name: test-skill
version: 2.0.0
description: "A test skill for validation"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: main.star
`
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.yaml"),
		[]byte(manifestYAML), 0o644,
	))

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"test", skillDir})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test-skill")
	assert.Contains(t, output, "2.0.0")
	assert.Contains(t, output, "validated successfully")
}

func TestSkillTestInstructionSkill(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "instruction-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillMD := `---
name: test-instruction
version: 2.0.0
description: "An instruction skill for validation"
---

Apply this guidance when editing docs.
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644))

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"test", skillDir})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test-instruction")
	assert.Contains(t, output, "2.0.0")
	assert.Contains(t, output, "validated successfully")
}

// TestSkillTestInvalidManifest verifies that "skill test <path>" reports
// validation errors for an invalid manifest.
func TestSkillTestInvalidManifest(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "bad-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	// Missing required fields (description, types).
	badYAML := `name: bad-skill
version: 1.0.0
`
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.yaml"),
		[]byte(badYAML), 0o644,
	))

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"test", skillDir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation")
}

// TestSkillTestMissingManifest verifies "skill test" fails when SKILL.yaml is absent.
func TestSkillTestMissingManifest(t *testing.T) {
	emptyDir := t.TempDir()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"test", emptyDir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SKILL.yaml")
}

// TestSkillPermissions verifies that "skill permissions <name>" lists
// permission approvals for a skill.
func TestSkillPermissions(t *testing.T) {
	// Create a store with some approvals.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)

	require.NoError(t, s.Approve("my-tool", "file:read", "always"))
	require.NoError(t, s.Approve("my-tool", "shell:exec", "always"))
	s.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"permissions", "my-tool", "--store", dbPath})

	err = cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	// Should contain table headers.
	assert.Contains(t, output, "PERMISSION")
	assert.Contains(t, output, "SCOPE")
	assert.Contains(t, output, "APPROVED_AT")
	// Should contain both approvals.
	assert.Contains(t, output, "file:read")
	assert.Contains(t, output, "shell:exec")
	assert.Contains(t, output, "always")
}

// TestSkillPermissionsEmpty verifies the empty message when no approvals exist.
func TestSkillPermissionsEmpty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	s.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"permissions", "unknown-skill", "--store", dbPath})

	err = cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "No permission approvals")
	assert.Contains(t, output, "unknown-skill")
}

// TestSkillPermissionsRevoke verifies that "skill permissions <name> --revoke"
// clears all permission approvals for a skill.
func TestSkillPermissionsRevoke(t *testing.T) {
	// Create a store with some approvals.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)

	require.NoError(t, s.Approve("my-tool", "file:read", "always"))
	require.NoError(t, s.Approve("my-tool", "shell:exec", "always"))
	s.Close()

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"permissions", "my-tool", "--revoke", "--store", dbPath})

	err = cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "All permissions revoked")
	assert.Contains(t, output, "my-tool")

	// Verify approvals are actually gone.
	s2, err := store.NewStore(dbPath)
	require.NoError(t, err)
	defer s2.Close()

	approvals, err := s2.ListApprovals("my-tool")
	require.NoError(t, err)
	assert.Empty(t, approvals)
}

func TestSkillLintCommandPasses(t *testing.T) {
	skillDir := t.TempDir()
	content := `---
name: lint-ok
version: 1.0.0
description: Clean instruction skill
references:
  - path: references/checklist.md
---
Keep output concise.
`
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "references"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "references", "checklist.md"), []byte("checklist"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"lint", skillDir})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "passed lint")
}

func TestSkillLintCommandReportsIssues(t *testing.T) {
	skillDir := t.TempDir()
	content := `---
name: lint-bad
version: 1.0.0
description: Broken instruction skill
references:
  - path: refs/missing.md
commands:
  - name: repeat
    description: first
  - name: repeat
    description: second
---
Body.
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"lint", skillDir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skill lint failed")
	assert.Contains(t, buf.String(), `duplicate command name "repeat"`)
	assert.Contains(t, buf.String(), `missing reference file "refs/missing.md"`)
}

func TestSkillDevCommandOncePasses(t *testing.T) {
	skillDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: dev-ok
version: 1.0.0
description: Clean dev skill
---
Keep things simple.
`), 0o644))

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dev", skillDir, "--once"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "[manifest] ok: dev-ok v1.0.0 (instruction)")
	assert.Contains(t, output, "[lint] ok")
}

func TestSkillDevCommandOnceReportsIssues(t *testing.T) {
	skillDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: dev-bad
version: 1.0.0
description: Broken dev skill
unknown_field: nope
---
Body.
`), 0o644))

	cmd := skillCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dev", skillDir, "--once"})

	err := cmd.Execute()
	require.Error(t, err)
	output := buf.String()
	assert.Contains(t, output, "[manifest] ok: dev-bad v1.0.0 (instruction)")
	assert.Contains(t, output, "[lint] 1 issue(s)")
	assert.Contains(t, output, "unknown_field")
}

// --- skillListerAdapter tests ---

// newTestRuntime creates a Runtime with a single registered built-in skill
// for testing the skillListerAdapter field mapping.
func newTestRuntime(t *testing.T) *skills.Runtime {
	t.Helper()

	loader := skills.NewLoader("", "")

	manifest := &skills.SkillManifest{
		Name:        "test-skill",
		Description: "A test skill",
		Version:     "1.0.0",
		Types:       []skills.SkillType{skills.SkillTypePrompt},
		Triggers:    skills.TriggerConfig{Modes: []string{"interactive"}},
	}
	loader.RegisterBuiltin(manifest)

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	rt := skills.NewRuntime(loader, s, tools.NewRegistry(), nil,
		func(m skills.SkillManifest, dir string) (skills.SkillBackend, error) {
			return &stubBackend{}, nil
		},
		func(name string, perms []skills.Permission) skills.PermissionChecker {
			return &stubPermChecker{}
		},
	)
	require.NoError(t, rt.Discover(nil))
	return rt
}

// stubBackend satisfies SkillBackend for adapter tests.
type stubBackend struct{}

func (b *stubBackend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error { return nil }
func (b *stubBackend) Unload() error                                                 { return nil }
func (b *stubBackend) Tools() []tools.Tool                                           { return nil }
func (b *stubBackend) Commands() []commands.SlashCommand                             { return nil }
func (b *stubBackend) Agents() []*skills.AgentDefinition                             { return nil }
func (b *stubBackend) Hooks() map[skills.HookPhase]skills.HookHandler                { return nil }

// stubPermChecker satisfies PermissionChecker for adapter tests.
type stubPermChecker struct{}

func (c *stubPermChecker) CheckPermission(_ skills.Permission) error { return nil }
func (c *stubPermChecker) CheckRateLimit(_ string) error             { return nil }
func (c *stubPermChecker) ResetTurnLimits()                          {}

func TestSkillListerAdapterListSkills(t *testing.T) {
	rt := newTestRuntime(t)
	adapter := &skillListerAdapter{rt: rt}

	infos := adapter.ListSkills()
	require.Len(t, infos, 1)

	info := infos[0]
	assert.Equal(t, "test-skill", info.Name)
	assert.Equal(t, "A test skill", info.Description)
	assert.Equal(t, "builtin", info.Source)
	assert.Equal(t, "Inactive", info.State)
}

func TestSkillListerAdapterActivateAndDeactivate(t *testing.T) {
	rt := newTestRuntime(t)
	adapter := &skillListerAdapter{rt: rt}

	// Activate.
	require.NoError(t, adapter.ActivateSkill("test-skill"))

	infos := adapter.ListSkills()
	require.Len(t, infos, 1)
	assert.Equal(t, "Active", infos[0].State)

	// Deactivate.
	require.NoError(t, adapter.DeactivateSkill("test-skill"))

	infos = adapter.ListSkills()
	require.Len(t, infos, 1)
	assert.Equal(t, "Inactive", infos[0].State)
}

func TestSkillListerAdapterActivateNotFound(t *testing.T) {
	rt := newTestRuntime(t)
	adapter := &skillListerAdapter{rt: rt}

	err := adapter.ActivateSkill("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSkillListerAdapterDeactivateNotActive(t *testing.T) {
	rt := newTestRuntime(t)
	adapter := &skillListerAdapter{rt: rt}

	err := adapter.DeactivateSkill("test-skill")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not active")
}
