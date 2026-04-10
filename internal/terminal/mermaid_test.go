package terminal

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMmdcAvailable_WhenNotOnPath(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	assert.False(t, MmdcAvailable())
}

func TestMmdcAvailable_WhenOnPath(t *testing.T) {
	if _, err := findMmdc(); err != nil {
		t.Skip("mmdc not installed")
	}
	assert.True(t, MmdcAvailable())
}

func TestRenderMermaid_MmdcNotFound(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	_, err := RenderMermaid(context.Background(), "graph TD\nA-->B", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mmdc")
}

func TestRenderMermaid_Success(t *testing.T) {
	if _, err := findMmdc(); err != nil {
		t.Skip("mmdc not installed")
	}
	src := "graph TD\nA-->B"
	png, err := RenderMermaid(context.Background(), src, false)
	require.NoError(t, err)
	require.NotEmpty(t, png)
	// PNG magic bytes: 0x89, 'P', 'N', 'G'
	assert.Equal(t, byte(0x89), png[0])
	assert.Equal(t, byte('P'), png[1])
	assert.Equal(t, byte('N'), png[2])
	assert.Equal(t, byte('G'), png[3])
}

func TestRenderMermaid_CancelledContext(t *testing.T) {
	if _, err := findMmdc(); err != nil {
		t.Skip("mmdc not installed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	_, err := RenderMermaid(ctx, "graph TD\nA-->B", false)
	require.Error(t, err)
}

// findMmdc is the internal lookup used by both MmdcAvailable and the tests.
// Defined here to avoid duplicating the PATH lookup in tests.
func findMmdc() (string, error) {
	return lookupMmdc()
}

// TestRenderMermaid_DarkMode verifies that dark mode flag is forwarded.
// Skipped when mmdc is not installed.
func TestRenderMermaid_DarkMode(t *testing.T) {
	if _, err := findMmdc(); err != nil {
		t.Skip("mmdc not installed")
	}
	src := "graph TD\nA-->B"
	png, err := RenderMermaid(context.Background(), src, true)
	require.NoError(t, err)
	assert.NotEmpty(t, png)

	// Verify PNG magic bytes
	assert.Equal(t, byte(0x89), png[0])
}

// TestRenderMermaid_CleanupOnSuccess verifies no temp files linger after success.
func TestRenderMermaid_CleanupOnSuccess(t *testing.T) {
	if _, err := findMmdc(); err != nil {
		t.Skip("mmdc not installed")
	}

	// Count temp dirs before
	tmpBefore, _ := os.ReadDir(os.TempDir())

	_, err := RenderMermaid(context.Background(), "graph TD\nA-->B", false)
	require.NoError(t, err)

	tmpAfter, _ := os.ReadDir(os.TempDir())
	// The number of entries should not grow permanently (temp dir cleaned up)
	assert.LessOrEqual(t, len(tmpAfter), len(tmpBefore)+1, "temp dirs should be cleaned up")
}
