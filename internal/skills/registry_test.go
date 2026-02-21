package skills

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistrySearch(t *testing.T) {
	results := []RegistrySearchResult{
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

	client := NewRegistryClient(srv.URL, nil, 0)
	got, err := client.Search(context.Background(), "code")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "code-review", got[0].Name)
	assert.Equal(t, "1.0.0", got[0].Version)
	assert.Equal(t, "Automated code review", got[0].Description)
	assert.Equal(t, "code-format", got[1].Name)
	assert.Equal(t, "2.1.0", got[1].Version)
}

func TestRegistryGetManifest(t *testing.T) {
	manifestYAML := `name: code-review
version: 1.0.0
description: "Automated code review"
types:
  - prompt
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/skills/code-review/1.0.0/manifest", r.URL.Path)
		w.Header().Set("Content-Type", "application/x-yaml")
		w.Write([]byte(manifestYAML))
	}))
	defer srv.Close()

	client := NewRegistryClient(srv.URL, nil, 0)
	m, err := client.GetManifest(context.Background(), "code-review", "1.0.0")
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "code-review", m.Name)
	assert.Equal(t, "1.0.0", m.Version)
	assert.Equal(t, "Automated code review", m.Description)
	assert.Equal(t, []SkillType{SkillTypePrompt}, m.Types)
}

func TestRegistryDownload(t *testing.T) {
	// Build a gzip tarball in memory containing a SKILL.yaml and a README.
	skillContent := `name: dl-skill
version: 1.0.0
description: "Downloaded skill"
types:
  - prompt
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/skills/dl-skill/1.0.0/download", r.URL.Path)
		w.Header().Set("Content-Type", "application/gzip")

		gw := gzip.NewWriter(w)
		tw := tar.NewWriter(gw)

		// Add SKILL.yaml to tarball.
		data := []byte(skillContent)
		err := tw.WriteHeader(&tar.Header{
			Name: "SKILL.yaml",
			Mode: 0o644,
			Size: int64(len(data)),
		})
		require.NoError(t, err)
		_, err = tw.Write(data)
		require.NoError(t, err)

		// Add a README file.
		readme := []byte("# dl-skill\nDownloaded skill.")
		err = tw.WriteHeader(&tar.Header{
			Name: "README.md",
			Mode: 0o644,
			Size: int64(len(readme)),
		})
		require.NoError(t, err)
		_, err = tw.Write(readme)
		require.NoError(t, err)

		require.NoError(t, tw.Close())
		require.NoError(t, gw.Close())
	}))
	defer srv.Close()

	dest := t.TempDir()
	client := NewRegistryClient(srv.URL, nil, 0)
	err := client.Download(context.Background(), "dl-skill", "1.0.0", dest)
	require.NoError(t, err)

	// Verify extracted files exist.
	skillYAML, err := os.ReadFile(filepath.Join(dest, "SKILL.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(skillYAML), "dl-skill")

	readmeData, err := os.ReadFile(filepath.Join(dest, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readmeData), "dl-skill")
}

func TestRegistryCachingHit(t *testing.T) {
	manifestYAML := `name: cached-skill
version: 2.0.0
description: "A cached skill"
types:
  - prompt
`
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/x-yaml")
		w.Write([]byte(manifestYAML))
	}))
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	client := NewRegistryClient(srv.URL, s, 10*time.Minute)

	// First call should hit the server.
	m1, err := client.GetManifest(context.Background(), "cached-skill", "2.0.0")
	require.NoError(t, err)
	require.NotNil(t, m1)
	assert.Equal(t, "cached-skill", m1.Name)
	assert.Equal(t, int32(1), requestCount.Load())

	// Second call should use cache, no additional server request.
	m2, err := client.GetManifest(context.Background(), "cached-skill", "2.0.0")
	require.NoError(t, err)
	require.NotNil(t, m2)
	assert.Equal(t, "cached-skill", m2.Name)
	assert.Equal(t, int32(1), requestCount.Load(), "second call should use cache, not hit server")
}

func TestRegistryCachingExpired(t *testing.T) {
	manifestYAML := `name: expired-skill
version: 1.0.0
description: "An expired skill"
types:
  - prompt
`
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/x-yaml")
		w.Write([]byte(manifestYAML))
	}))
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Use a negative TTL so cache is always expired.
	client := NewRegistryClient(srv.URL, s, -1*time.Second)

	// First call hits server.
	m1, err := client.GetManifest(context.Background(), "expired-skill", "1.0.0")
	require.NoError(t, err)
	require.NotNil(t, m1)
	assert.Equal(t, int32(1), requestCount.Load())

	// Second call should also hit the server because cache is expired.
	m2, err := client.GetManifest(context.Background(), "expired-skill", "1.0.0")
	require.NoError(t, err)
	require.NotNil(t, m2)
	assert.Equal(t, int32(2), requestCount.Load(), "expired cache should cause refetch")
}

func TestRegistryListVersions(t *testing.T) {
	versions := []string{"1.0.0", "1.1.0", "1.2.0", "2.0.0"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/skills/my-tool/versions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(versions)
	}))
	defer srv.Close()

	client := NewRegistryClient(srv.URL, nil, 0)
	got, err := client.ListVersions(context.Background(), "my-tool")
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, "1.0.0", got[0])
	assert.Equal(t, "2.0.0", got[3])
}

func TestRegistryListVersionsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewRegistryClient(srv.URL, nil, 0)
	_, err := client.ListVersions(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestRegistryGitInstall(t *testing.T) {
	// Create a bare git repo with a SKILL.yaml committed.
	repoDir := t.TempDir()
	workDir := t.TempDir()

	// Initialize a bare repo.
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}

	// Create a normal repo, add SKILL.yaml, then clone from it.
	run(workDir, "git", "init")
	skillContent := fmt.Sprintf("name: git-skill\nversion: 1.0.0\ndescription: \"Git skill\"\ntypes:\n  - prompt\n")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "SKILL.yaml"), []byte(skillContent), 0o644))
	run(workDir, "git", "add", "SKILL.yaml")
	run(workDir, "git", "commit", "-m", "initial")

	// Create a bare clone to serve as the remote.
	run(repoDir, "git", "clone", "--bare", workDir, filepath.Join(repoDir, "repo.git"))

	dest := t.TempDir()
	client := NewRegistryClient("", nil, 0)
	err := client.InstallFromGit(context.Background(), filepath.Join(repoDir, "repo.git"), filepath.Join(dest, "git-skill"))
	require.NoError(t, err)

	// Verify SKILL.yaml exists in the cloned directory.
	_, err = os.Stat(filepath.Join(dest, "git-skill", "SKILL.yaml"))
	require.NoError(t, err, "SKILL.yaml should exist in cloned dir")

	// Verify it fails when SKILL.yaml is missing.
	emptyRepo := t.TempDir()
	emptyWork := t.TempDir()
	run(emptyWork, "git", "init")
	require.NoError(t, os.WriteFile(filepath.Join(emptyWork, "README.md"), []byte("no skill"), 0o644))
	run(emptyWork, "git", "add", "README.md")
	run(emptyWork, "git", "commit", "-m", "no skill")
	run(emptyRepo, "git", "clone", "--bare", emptyWork, filepath.Join(emptyRepo, "empty.git"))

	dest2 := t.TempDir()
	err = client.InstallFromGit(context.Background(), filepath.Join(emptyRepo, "empty.git"), filepath.Join(dest2, "empty-skill"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SKILL.yaml")
}
