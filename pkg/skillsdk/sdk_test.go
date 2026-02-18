package skillsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManifestType(t *testing.T) {
	m := Manifest{
		Name:        "my-skill",
		Version:     "1.0.0",
		Description: "A test skill",
		Author:      "Test Author",
		License:     "MIT",
	}

	assert.Equal(t, "my-skill", m.Name)
	assert.Equal(t, "1.0.0", m.Version)
	assert.Equal(t, "A test skill", m.Description)
	assert.Equal(t, "Test Author", m.Author)
	assert.Equal(t, "MIT", m.License)
}

// TestContextInterface is a compile-time interface check that verifies Context
// has all required methods. If any method is missing, the test will not compile.
func TestContextInterface(t *testing.T) {
	// Compile-time assertion: if Context is missing any method, this fails to build.
	var _ Context = (*mockContext)(nil)
}

// mockContext implements Context for testing purposes.
type mockContext struct{}

func (m *mockContext) ReadFile(path string) (string, error)         { return "", nil }
func (m *mockContext) WriteFile(path, content string) error         { return nil }
func (m *mockContext) ListDir(path string) ([]FileInfo, error)      { return nil, nil }
func (m *mockContext) SearchFiles(pattern string) ([]string, error) { return nil, nil }

func (m *mockContext) Exec(command string, args ...string) (ExecResult, error) {
	return ExecResult{}, nil
}

func (m *mockContext) Complete(prompt string) (string, error)     { return "", nil }
func (m *mockContext) Fetch(url string) (string, error)           { return "", nil }
func (m *mockContext) GitDiff(args ...string) (string, error)     { return "", nil }
func (m *mockContext) GitLog(args ...string) ([]GitCommit, error) { return nil, nil }
func (m *mockContext) GitStatus() ([]GitFileStatus, error)        { return nil, nil }
func (m *mockContext) GetEnv(key string) string                   { return "" }
func (m *mockContext) ProjectRoot() string                        { return "" }

func (m *mockContext) InvokeSkill(name string, input map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestMockContext(t *testing.T) {
	var ctx Context = &mockContext{}

	t.Run("ReadFile", func(t *testing.T) {
		content, err := ctx.ReadFile("/some/path")
		require.NoError(t, err)
		assert.Equal(t, "", content)
	})

	t.Run("WriteFile", func(t *testing.T) {
		err := ctx.WriteFile("/some/path", "content")
		require.NoError(t, err)
	})

	t.Run("ListDir", func(t *testing.T) {
		entries, err := ctx.ListDir("/some/dir")
		require.NoError(t, err)
		assert.Nil(t, entries)
	})

	t.Run("SearchFiles", func(t *testing.T) {
		matches, err := ctx.SearchFiles("*.go")
		require.NoError(t, err)
		assert.Nil(t, matches)
	})

	t.Run("Exec", func(t *testing.T) {
		result, err := ctx.Exec("echo", "hello")
		require.NoError(t, err)
		assert.Equal(t, ExecResult{}, result)
	})

	t.Run("Complete", func(t *testing.T) {
		resp, err := ctx.Complete("test prompt")
		require.NoError(t, err)
		assert.Equal(t, "", resp)
	})

	t.Run("Fetch", func(t *testing.T) {
		body, err := ctx.Fetch("https://example.com")
		require.NoError(t, err)
		assert.Equal(t, "", body)
	})

	t.Run("GitDiff", func(t *testing.T) {
		diff, err := ctx.GitDiff("--cached")
		require.NoError(t, err)
		assert.Equal(t, "", diff)
	})

	t.Run("GitLog", func(t *testing.T) {
		commits, err := ctx.GitLog("--oneline")
		require.NoError(t, err)
		assert.Nil(t, commits)
	})

	t.Run("GitStatus", func(t *testing.T) {
		statuses, err := ctx.GitStatus()
		require.NoError(t, err)
		assert.Nil(t, statuses)
	})

	t.Run("GetEnv", func(t *testing.T) {
		val := ctx.GetEnv("HOME")
		assert.Equal(t, "", val)
	})

	t.Run("ProjectRoot", func(t *testing.T) {
		root := ctx.ProjectRoot()
		assert.Equal(t, "", root)
	})

	t.Run("InvokeSkill", func(t *testing.T) {
		result, err := ctx.InvokeSkill("other-skill", map[string]any{"key": "value"})
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}
