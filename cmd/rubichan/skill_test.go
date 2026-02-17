package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/search", r.URL.Path)
		assert.Equal(t, "code", r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]skills.RegistrySearchResult{})
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
